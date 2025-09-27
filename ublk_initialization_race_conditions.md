# UBLK Initialization Race Conditions & Concurrency Issues

## Executive Summary

Our Go ublk implementation has **fundamental race conditions and concurrency bugs** during device initialization that cause intermittent failures, segfaults, and hangs. While we successfully identified and fixed an artificial 250ms delay, this revealed deeper issues in the control plane sequencing, queue runner initialization, and error handling.

## The Problem Manifestation

### Symptoms
1. **Intermittent "no completions available" errors** during START_DEV
2. **Segmentation faults** in `processIOAndCommit` due to nil pointer dereference
3. **Inconsistent reliability** - same code works sometimes, fails other times
4. **Timing-dependent behavior** - small delays (1ms) improve reliability but don't eliminate issues

### Evidence from Logs
```
# Successful run:
time=2025-09-26T22:30:53.565-04:00 level=INFO msg="START_DEV completed" result=0
time=2025-09-26T22:30:53.566-04:00 level=INFO msg="device initialization complete"

# Failed run (same code):
time=2025-09-26T22:31:06.199-04:00 level=ERROR msg="submitAndWait failed" error="no completions available"
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x1 addr=0x4 pc=0x4e3328]
```

## Root Cause Analysis

### The Initialization Sequence

The current device creation follows this sequence:

```go
// backend.go - CreateAndServe()
func CreateAndServe(ctx context.Context, params DeviceParams, options *Options) (*Device, error) {
    // 1. Create control plane
    ctrl, err := createController()

    // 2. ADD_DEV (fast)
    devID, err := ctrl.AddDevice(&ctrlParams)

    // 3. SET_PARAMS (fast)
    err = ctrl.SetParams(devID, &ctrlParams)

    // 4. Create queue runners BEFORE START_DEV
    device.runners = make([]*queue.Runner, numQueues)
    for i := 0; i < numQueues; i++ {
        runner, err := queue.NewRunner(device.ctx, runnerConfig)
        device.runners[i] = runner
    }

    // 5. Start queue runners and submit FETCH_REQs
    for i := 0; i < numQueues; i++ {
        if err := device.runners[i].Start(); err != nil {
            // Error handling...
        }
    }

    // 6. Small delay (was 250ms, now 1ms)
    time.Sleep(constants.QueueInitDelay)

    // 7. START_DEV (this is where races occur)
    err = ctrl.StartDevice(devID)

    // 8. Another delay (current workaround)
    time.Sleep(1 * time.Millisecond)

    return device, nil
}
```

### Critical Race Window: Queue Runner Start

The `runner.Start()` method does this:

```go
// internal/queue/runner.go
func (r *Runner) Start() error {
    // Start background goroutine
    go r.ioLoop()

    // Submit initial FETCH_REQs
    return r.Prime()
}

func (r *Runner) Prime() error {
    // Submit initial FETCH_REQ for each tag (ONLY ONCE at startup)
    for tag := 0; tag < r.depth; tag++ {
        if err := r.submitInitialFetchReq(uint16(tag)); err != nil {
            // If we get EOPNOTSUPP, START_DEV might not be ready yet
            if errno, ok := err.(syscall.Errno); ok && errno == syscall.EOPNOTSUPP {
                return fmt.Errorf("device not ready (START_DEV pending): %w", err)
            }
            return fmt.Errorf("submit initial FETCH_REQ[%d]: %w", tag, err)
        }
        // Set initial state: FETCH_REQ is now in flight
        r.tagStates[tag] = TagStateInFlightFetch
    }
    return nil
}
```

### Race Condition #1: FETCH_REQ vs START_DEV Timing

**The Problem**: FETCH_REQs are submitted BEFORE START_DEV, but the kernel may not be ready to accept them.

```go
func (r *Runner) submitInitialFetchReq(tag uint16) error {
    // This happens BEFORE START_DEV
    cmd := uapi.UblkIOCmd(uapi.UBLK_IO_FETCH_REQ)
    _, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)  // MAY FAIL

    // Set state assuming success
    r.tagStates[tag] = TagStateInFlightFetch  // WRONG if submission failed
    return err
}
```

**What happens**:
1. `Prime()` submits FETCH_REQs to kernel
2. Kernel may reject them (device not started yet)
3. State machine assumes they succeeded
4. `START_DEV` happens later
5. Queue runner expects FETCH_REQs to be in flight, but they're not

### Race Condition #2: Concurrent ioLoop Startup

The `ioLoop` goroutine starts immediately but may process completions before FETCH_REQs are submitted:

```go
func (r *Runner) ioLoop() {
    defer r.cleanup()

    for {
        select {
        case <-r.ctx.Done():
            return
        default:
            if err := r.processRequests(); err != nil {
                return  // Exits on any error
            }
        }
    }
}

func (r *Runner) processRequests() error {
    // Wait for completions (may timeout)
    results, err := r.ring.WaitForCompletion(constants.IOTimeout)
    if err != nil {
        return err  // EXITS THE ENTIRE LOOP
    }

    for _, result := range results {
        if err := r.handleCompletion(result); err != nil {
            return err  // ALSO EXITS ON SINGLE COMPLETION ERROR
        }
    }
    return nil
}
```

**The Problem**: `ioLoop` can exit permanently on the first error, but `Prime()` may still be submitting FETCH_REQs.

### Race Condition #3: Tag State Machine Corruption

Tags have states managed with per-tag mutexes:

```go
type TagState int

const (
    TagStateIdle TagState = iota
    TagStateInFlightFetch
    TagStateProcessing
    TagStateInFlightCommit
)

// Per-tag state and mutex
r.tagStates[tag] = TagStateInFlightFetch
r.tagMutexes[tag].Lock()
```

**The Problem**: State transitions can be inconsistent if FETCH_REQ submission fails but state is still updated.

### The Null Pointer Dereference

From the crash log, the segfault happens here:

```go
func (r *Runner) processIOAndCommit(result Result) error {
    // result can be nil if handleCompletion gets a nil result
    userData := result.UserData()  // SEGFAULT: result is nil

    // Extract operation info
    op := userData & udOpMask
    // ...
}

func (r *Runner) handleCompletion(result Result) error {
    // This can pass nil to processIOAndCommit
    if result == nil {
        return r.processIOAndCommit(nil)  // BUG: passes nil
    }
    // ...
}
```

## The Deeper io_uring Issues

### Control Plane Completion Handling

The control operations use `submitAndWait` which expects exactly one completion:

```go
// internal/uring/minimal.go
func (r *minimalRing) submitAndWait(sqe *sqe128) (Result, error) {
    // Submit SQE and wait for exactly one completion
    submitted, completed, errno := r.submitAndWaitRing(1, 1)
    if errno != 0 {
        return nil, fmt.Errorf("io_uring_enter failed: %v", errno)
    }

    // Process the one expected completion
    return r.processCompletion()  // MAY RETURN "no completions available"
}

func (r *minimalRing) processCompletion() (Result, error) {
    // Check completion queue
    cqHead := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.head))
    cqTail := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.tail))

    if *cqHead == *cqTail {
        return nil, fmt.Errorf("no completions available")  // RACE: completion not ready yet
    }

    // Process completion...
}
```

**The Problem**: `io_uring_enter` may return saying completions are ready, but when we check the completion queue, nothing is there due to memory ordering or timing.

### Data Plane Completion Handling

The data plane uses `WaitForCompletion` with timeouts:

```go
func (r *minimalRing) WaitForCompletion(timeout int) ([]Result, error) {
    // Drain CQ if anything is already there
    results := make([]Result, 0, 8)

    if timeout > 0 {
        // Don't wait, just check
        _, _, _ = r.submitAndWaitRing(0, 0)
        drain()
        return results, nil
    }

    // Block for at least one completion
    _, _, errno := r.submitAndWaitRing(0, 1)  // BLOCKS
    if errno != 0 {
        return nil, fmt.Errorf("io_uring_enter wait failed: %v", errno)
    }

    drain()
    return results, nil
}
```

**The Problem**: The `drain()` function can return empty results even after `io_uring_enter` claims completions are available.

## Specific Failure Modes

### Mode 1: "no completions available"

1. Control operation (ADD_DEV/SET_PARAMS/START_DEV) submitted
2. `io_uring_enter` returns success with `completed=1`
3. `processCompletion()` checks completion queue
4. Queue is empty due to memory ordering/timing
5. Returns "no completions available" error
6. Control operation fails, device creation aborts

### Mode 2: Segmentation Fault

1. FETCH_REQ submitted during `Prime()`
2. Kernel rejects it (device not ready) but no error returned
3. Tag state set to `TagStateInFlightFetch` anyway
4. `ioLoop` starts and calls `WaitForCompletion()`
5. Gets empty results array or nil result
6. Calls `handleCompletion(nil)`
7. `processIOAndCommit(nil)` dereferences null pointer
8. Segfault

### Mode 3: Timing-Dependent Success

1. Small delay allows kernel to process commands before next step
2. Memory barriers and cache coherency settle
3. Completions are properly available when checked
4. Everything works correctly

## Impact Assessment

### Current Reliability
- **Success rate**: ~66% (2 out of 3 tests pass)
- **Failure modes**: Control plane hangs, data plane crashes
- **User impact**: Unusable for production due to intermittent failures

### Performance Impact
- **Minimal delay workaround**: Fast (~100ms initialization)
- **No impact on steady-state**: 500k+ IOPS maintained
- **Race-dependent**: May require larger delays under load

## Required Fixes

### IMMEDIATE (Critical Safety)

1. **Fix null pointer dereference**:
```go
func (r *Runner) processIOAndCommit(result Result) error {
    if result == nil {
        return fmt.Errorf("received nil completion result")
    }
    // ... rest of function
}

func (r *Runner) handleCompletion(result Result) error {
    if result == nil {
        return fmt.Errorf("handleCompletion called with nil result")
    }
    // ... rest of function
}
```

2. **Guard against empty completion arrays**:
```go
func (r *Runner) processRequests() error {
    results, err := r.ring.WaitForCompletion(constants.IOTimeout)
    if err != nil {
        return err
    }

    // Guard against empty results
    if len(results) == 0 {
        return nil  // Not an error, just no work
    }

    for _, result := range results {
        if result == nil {
            continue  // Skip nil results
        }
        if err := r.handleCompletion(result); err != nil {
            return err
        }
    }
    return nil
}
```

### ARCHITECTURAL (Race Condition Fixes)

1. **Separate FETCH_REQ submission from START_DEV**:
```go
// Instead of submitting FETCH_REQs before START_DEV:
func CreateAndServe() {
    // 1. Control plane setup
    ctrl.AddDevice()
    ctrl.SetParams()

    // 2. Create runners but DON'T start them
    for i := 0; i < numQueues; i++ {
        runner := queue.NewRunner(config)
        device.runners[i] = runner
    }

    // 3. START_DEV first
    ctrl.StartDevice()

    // 4. THEN start runners and submit FETCH_REQs
    for i := 0; i < numQueues; i++ {
        device.runners[i].Start()  // Now kernel is ready
    }
}
```

2. **Retry mechanism for control operations**:
```go
func (r *minimalRing) submitAndWaitWithRetry(sqe *sqe128, maxRetries int) (Result, error) {
    for attempt := 0; attempt < maxRetries; attempt++ {
        result, err := r.submitAndWait(sqe)
        if err == nil {
            return result, nil
        }

        if strings.Contains(err.Error(), "no completions available") {
            time.Sleep(time.Millisecond * time.Duration(1<<attempt))  // Exponential backoff
            continue
        }

        return nil, err  // Non-retryable error
    }
    return nil, fmt.Errorf("submitAndWait failed after %d retries", maxRetries)
}
```

3. **Memory barriers in completion checking**:
```go
func (r *minimalRing) processCompletion() (Result, error) {
    // Force memory barrier before checking completion queue
    runtime.Gosched()

    cqHead := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.head))
    cqTail := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.tail))

    // Double-check with small delay
    if *cqHead == *cqTail {
        time.Sleep(100 * time.Microsecond)  // Allow completion to appear
        if *cqHead == *cqTail {
            return nil, fmt.Errorf("no completions available after delay")
        }
    }

    // Process completion...
}
```

### DESIGN IMPROVEMENTS

1. **State machine validation**:
```go
func (r *Runner) validateTagState(tag uint16, expectedState TagState) error {
    r.tagMutexes[tag].Lock()
    defer r.tagMutexes[tag].Unlock()

    if r.tagStates[tag] != expectedState {
        return fmt.Errorf("tag %d in state %d, expected %d", tag, r.tagStates[tag], expectedState)
    }
    return nil
}
```

2. **Graceful error recovery**:
```go
func (r *Runner) ioLoop() {
    defer r.cleanup()

    consecutiveErrors := 0
    for {
        select {
        case <-r.ctx.Done():
            return
        default:
            if err := r.processRequests(); err != nil {
                consecutiveErrors++
                if consecutiveErrors > 10 {
                    return  // Give up after many consecutive errors
                }
                time.Sleep(time.Millisecond * time.Duration(consecutiveErrors))
                continue
            }
            consecutiveErrors = 0  // Reset on success
        }
    }
}
```

## Testing Strategy

### Reproduce the Race Conditions
```bash
# Rapid repeated testing to trigger races
for i in {1..50}; do
    echo "Test $i"
    timeout 5 ./ublk-mem --size=16M --minimal || echo "FAILED"
done
```

### Stress Testing
```bash
# Concurrent device creation
for i in {1..5}; do
    ./ublk-mem --size=16M --minimal &
done
wait
```

### Race Detection
```bash
go build -race ./cmd/ublk-mem
./ublk-mem --size=16M --minimal
```

## Conclusion

The 250ms delay was not entirely artificial - it was masking **real race conditions** in our initialization sequence. While we correctly identified that FETCH_REQ submission itself is fast, the interaction between:

1. **Control plane sequencing** (ADD_DEV → SET_PARAMS → START_DEV)
2. **Queue runner initialization** (goroutine startup, FETCH_REQ submission)
3. **io_uring completion handling** (memory ordering, timing dependencies)

Creates a complex web of race conditions that require systematic fixes, not just timing adjustments.

The immediate priority is **fixing the segfault** to prevent crashes, followed by **architectural changes** to eliminate the race conditions entirely.

---

# Expert Kernel Analysis Questions for UBLK Race Conditions (Linux 6.11)

## Context for Kernel Expert
We have a Go implementation of Linux ublk that exhibits **intermittent race conditions** during device initialization. Our analysis shows the issue occurs in the control plane sequencing and completion handling, but we need deep kernel-side insights to understand the root cause.

## Critical Questions for Kernel Expert

### 1. UBLK Control Command Sequencing & Timing

**Question**: In Linux 6.11, what is the **exact kernel state machine** for ublk device initialization? Specifically:

- After `ADD_DEV` completes successfully, is the `/dev/ublkc*` character device **immediately ready** for FETCH_REQ commands?
- Is there a **grace period** or **asynchronous setup** after `ADD_DEV`/`SET_PARAMS` where FETCH_REQs might be rejected?
- Does `START_DEV` have **ordering requirements** - must all queues have at least one FETCH_REQ submitted before it will complete?

**Our Evidence**: We see intermittent `-EOPNOTSUPP` when submitting FETCH_REQs before START_DEV, but inconsistently.

### 2. FETCH_REQ Submission Before START_DEV

**Question**: Is it **safe and intended** to submit `UBLK_IO_FETCH_REQ` commands to `/dev/ublkc*` **before** calling `START_DEV`?

Our current sequence:
```
1. ADD_DEV → success
2. SET_PARAMS → success
3. Submit FETCH_REQs to /dev/ublkc* (sometimes fails)
4. START_DEV (sometimes hangs waiting for FETCH_REQs)
```

**Kernel-side question**: Does the ublk driver **queue/buffer** FETCH_REQs submitted before START_DEV, or does it require START_DEV to complete first?

### 3. io_uring URING_CMD Completion Ordering

**Question**: With `IORING_OP_URING_CMD` on `/dev/ublk-control` and `/dev/ublkc*`, what are the **memory ordering guarantees** for completion visibility?

**Specific issue**: We see this sequence:
```c
// Control plane
io_uring_enter(fd, to_submit=1, min_complete=1, flags=IORING_ENTER_GETEVENTS)
// Returns: submitted=1, completed=1, errno=0

// But immediately after:
if (*cq_head == *cq_tail) {
    // THIS HAPPENS: no completions visible despite kernel claiming completed=1
}
```

**Questions**:
- Is there a **memory barrier requirement** between `io_uring_enter` return and CQ head/tail checking?
- Can `io_uring_enter` return `completed=1` but the CQE not be visible yet due to CPU cache coherency?
- Does ublk's URING_CMD implementation have any **special ordering requirements**?

### 4. Queue Readiness and FETCH_REQ Processing

**Question**: How does the kernel determine when a ublk queue is **"ready"** for I/O?

**Specific questions**:
- Does each queue need **at least one FETCH_REQ** in flight before the device becomes visible as `/dev/ublkb*`?
- Is there a **per-queue vs global** readiness state in the ublk driver?
- What happens if a queue **runs out of FETCH_REQs** during operation - does it block new I/O or return errors?

### 5. Error Conditions and Recovery

**Question**: Under what conditions will ublk control operations return specific errors?

**Our observations**:
- Sometimes `ADD_DEV`/`SET_PARAMS` work but `START_DEV` hangs indefinitely
- Sometimes FETCH_REQ submission returns success but no completion event arrives
- Occasionally see `-EINVAL` or `-EOPNOTSUPP` errors that are timing-dependent

**Questions**:
- What are the **valid state transitions** for ublk devices, and what errors occur for invalid transitions?
- Is there **kernel-side logging** (printk, tracepoints) we should enable to debug these issues?
- Are there **sysfs attributes** or `/proc` files that expose ublk device internal state?

### 6. Memory Management and Buffer Lifecycle

**Question**: What are the **buffer ownership semantics** for FETCH_REQ operations?

```c
struct ublksrv_io_cmd {
    __u16 q_id;
    __u16 tag;
    __u32 result;
    __u64 addr;  // User buffer address
};
```

**Questions**:
- When we submit FETCH_REQ with `addr` pointing to userspace memory, when does the kernel **pin/access** this memory?
- Is it safe to **submit multiple FETCH_REQs** for the same queue with different `addr` values before any complete?
- Does the kernel **validate** the `addr` immediately on submission or only when I/O arrives?

### 7. Kernel Version Differences

**Question**: Have there been **recent changes** to ublk initialization timing or URING_CMD handling between kernel versions?

**Context**: We're testing primarily on **6.11.0-24-generic**, but the code needs to work on 6.1+ (minimum ublk support).

**Questions**:
- Are there **known timing changes** or race condition fixes in recent ublk versions?
- Did io_uring URING_CMD implementation change completion semantics between 6.1 and 6.11?
- Should we expect **different behavior** on different kernel versions?

### 8. Debugging and Tracing

**Question**: What is the **best way to trace ublk kernel operations** to debug these race conditions?

**Current approach**:
```bash
# We're using these, but getting limited insight:
echo 1 > /sys/kernel/tracing/events/block/block_rq_insert/enable
echo 'p:probe_ublk_ctrl ublk_ctrl_uring_cmd' > /sys/kernel/tracing/kprobe_events
```

**Questions**:
- Are there **ublk-specific tracepoints** beyond generic block layer events?
- What **dynamic debug** categories exist for ublk (`pr_debug`, `dev_dbg`)?
- Is there a way to trace **per-queue state transitions** and FETCH_REQ lifecycle?

### 9. Race Condition Diagnosis

**Question**: Based on our symptom description, what are the **most likely kernel-side race conditions** we should investigate?

**Our symptoms summary**:
- ~66% success rate for device creation
- "no completions available" after successful `io_uring_enter`
- Timing-dependent behavior (1ms delay helps)
- Segfaults when completion arrays are empty

**Expert assessment request**: Given these symptoms and our initialization sequence, what would you investigate first on the kernel side?

### 10. Production Recommendations

**Question**: For a **production ublk implementation**, what is the **recommended initialization sequence** that avoids these race conditions?

**Specific asks**:
- Should FETCH_REQs be submitted **before** or **after** START_DEV?
- Is there a **standard retry mechanism** for control operations?
- What **timeouts and delays** are reasonable vs signs of bugs?
- Are there **kernel patches** or workarounds we should be aware of?

## Additional Context for Expert

**Our Environment**:
- Kernel version: `6.11.0-24-generic` (Ubuntu 24.04)
- io_uring: Using `IORING_OP_URING_CMD` with SQE128/CQE32 when available
- Implementation: Pure Go (no cgo) using syscalls directly

**Primary goal**: Understand if these are **userspace bugs** (our implementation), **kernel race conditions** (ublk driver), or **io_uring timing issues** (completion visibility), so we can implement the correct fix strategy.