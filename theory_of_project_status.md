# Theory of Project Status: Deep Dive Analysis

**Date**: 2025-09-29
**Status**: Critical analysis after days of I/O hang issues
**Purpose**: Comprehensive analysis comparing our implementation vs working reference implementations

---

## Executive Summary

After deep analysis of reference implementations (ublksrv C and libublk-rs), I've identified several potential issues causing intermittent I/O hangs. The implementations work ~50% of the time, suggesting **race conditions or memory ordering issues** rather than fundamental logic errors.

**Key Findings**:
1. **Per-SQE syscall overhead**: We call `io_uring_enter` for each FETCH_REQ submission (32 syscalls) instead of batching
2. **Context cancellation**: Blocking `io_uring_enter` syscall cannot be interrupted by Go context cancellation
3. **Memory ordering concerns**: SQE field writes may not be visible to kernel without proper barriers
4. **Tight select loop**: Non-blocking ctx.Done() check with blocking processRequests() creates complex interaction

---

## Project Context

### Current Status
- ✅ Device creation (ADD_DEV, SET_PARAMS, START_DEV) works reliably
- ⚠️  I/O operations work **intermittently** (~50% success rate)
- ✅ When working: Excellent performance (504k IOPS)
- ❌ When failing: Complete I/O hang, tests timeout

### Symptoms When Hanging
- FETCH_REQs submitted successfully
- START_DEV completes successfully
- `WaitForCompletion` blocks forever on `io_uring_enter`
- No completions arrive from kernel
- Process cannot be interrupted (syscall is blocking)

---

## Reference Implementation: ublksrv C

### Main I/O Loop (`ublksrv_process_io`)

**Location**: `.gitignored-repos/ublksrv-c/lib/ublksrv.c:1121-1167`

```c
int ublksrv_process_io(const struct ublksrv_queue *tq) {
    struct _ublksrv_queue *q = tq_to_local(tq);
    int ret, reapped;
    struct __kernel_timespec ts = {
        .tv_sec = UBLKSRV_IO_IDLE_SECS,  // 20 seconds
        .tv_nsec = 0
    };
    struct __kernel_timespec *tsp = (q->state & UBLKSRV_QUEUE_IDLE) ?
        NULL : &ts;
    struct io_uring_cqe *cqe;

    if (__ublksrv_queue_is_done(q))
        return -ENODEV;

    // KEY: Submit pending SQEs and wait for AT LEAST 1 completion
    ret = io_uring_submit_and_wait_timeout(&q->ring, &cqe, 1, tsp, NULL);
    //                                                      ^ minComplete=1

    ublksrv_reset_aio_batch(q);
    reapped = ublksrv_reap_events_uring(&q->ring);  // Process ALL completions
    ublksrv_submit_aio_batch(q);

    // ... idle handling ...

    return reapped;
}
```

**Critical Observations**:
1. **Always waits for ≥1 completion**: `minComplete=1` parameter
2. **Has timeout**: 20 seconds (or NULL=infinite when idle)
3. **Processes ALL available completions**: `ublksrv_reap_events_uring`
4. **Returns completion count**: Useful for debugging/monitoring

### Submission Strategy

```c
static void ublksrv_submit_fetch_commands(struct _ublksrv_queue *q) {
    int i = 0;

    // Queue all SQEs WITHOUT calling io_uring_enter
    for (i = 0; i < q->q_depth; i++)
        ublksrv_queue_io_cmd(q, &q->ios[i], i);

    __ublksrv_queue_event(q);
}

static inline int ublksrv_queue_io_cmd(struct _ublksrv_queue *q,
        struct ublk_io *io, unsigned tag) {
    // ...
    sqe = ublksrv_alloc_sqe(&q->ring);  // Just gets SQE pointer
    // ... fill SQE fields ...

    // NO io_uring_enter here! Just increments tail pointer
    q->cmd_inflight += 1;
    return 1;
}

// Later, ONE io_uring_enter call submits ALL queued SQEs:
io_uring_submit_and_wait_timeout(&q->ring, &cqe, 1, tsp, NULL);
```

**Key Point**: C implementation batches all SQEs, then makes **ONE** `io_uring_enter` syscall to submit them all.

### Completion Handling

```c
static void ublksrv_handle_cqe(struct io_uring *r,
        struct io_uring_cqe *cqe, void *data) {
    // ...
    struct ublk_io *io = &q->ios[tag];
    q->cmd_inflight--;

    if (cqe->res == UBLK_IO_RES_OK) {
        // Synchronously handle I/O (calls backend ReadAt/WriteAt)
        q->tgt_ops->handle_io_async(local_to_tq(q), &io->data);
        // handle_io_async internally calls ublksrv_complete_io which:
        //   1. Marks IO done with result
        //   2. Immediately submits COMMIT_AND_FETCH_REQ
    } else if (cqe->res == UBLK_IO_RES_NEED_GET_DATA) {
        io->flags |= UBLKSRV_NEED_GET_DATA | UBLKSRV_IO_FREE;
        ublksrv_queue_io_cmd(q, io, tag);
    } else {
        io->flags = UBLKSRV_IO_FREE;
    }
}
```

**Key Points**:
- Handles I/O **synchronously** from completion handler
- Immediately queues COMMIT_AND_FETCH_REQ (no async gap)
- Simple flag-based state machine

---

## Our Implementation Analysis

### Queue Runner I/O Loop

**Location**: `internal/queue/runner.go:230-274`

```go
func (r *Runner) ioLoop() {
    runtime.LockOSThread()  // ✅ CORRECT
    defer runtime.UnlockOSThread()

    for {
        select {
        case <-r.ctx.Done():  // Non-blocking check!
            return
        default:
            err := r.processRequests()  // Blocks in io_uring_enter
            if err != nil {
                r.logger.Printf("Queue %d: Error: %v", r.queueID, err)
                return
            }
        }
    }
}
```

**Issue #1: Context Cancellation Cannot Interrupt Blocking Syscall**

The `select` with `default` is non-blocking. When ctx is not done, we immediately call `processRequests()` which blocks in `io_uring_enter`. **If the kernel never sends a completion, we hang forever and cannot be interrupted.**

C implementation doesn't have this problem because it uses a timeout (20 seconds).

### Process Requests

**Location**: `internal/queue/runner.go:415-460`

```go
func (r *Runner) processRequests() error {
    // Wait for completion events from io_uring - this blocks until events arrive
    completions, err := r.ring.WaitForCompletion(0) // 0 = block forever
    if err != nil {
        return fmt.Errorf("failed to wait for completions: %w", err)
    }

    // Handle empty completions as no-work, not an error
    if len(completions) == 0 {
        return nil  // Should not happen if blocking properly
    }

    // Process each completion event
    for _, completion := range completions {
        if completion == nil {
            continue
        }

        userData := completion.UserData()
        tag := uint16(userData & 0xFFFF)
        isCommit := (userData & udOpCommit) != 0
        result := completion.Value()

        if tag >= uint16(r.depth) {
            continue
        }

        if err := r.handleCompletion(tag, isCommit, result); err != nil {
            return err
        }
    }

    return nil
}
```

**Analysis**: Logic looks correct. When `WaitForCompletion(0)` is called:
- `timeout=0` means "block forever"
- Should call `submitAndWaitRing(0, 1)` with `minComplete=1`
- This matches C implementation behavior

So the blocking logic is **correct**.

### WaitForCompletion Implementation

**Location**: `internal/uring/minimal.go:591-647`

```go
func (r *minimalRing) WaitForCompletion(timeout int) ([]Result, error) {
    results := make([]Result, 0, 8)

    drain := func() {
        // ... atomically read CQ head/tail and collect completions ...
    }

    // First, non-blocking drain of any existing completions
    drain()
    if len(results) > 0 {
        return results, nil
    }

    // If timeout > 0, do quick non-blocking check
    if timeout > 0 {
        _, _, _ = r.submitAndWaitRing(0, 0)  // minComplete=0
        drain()
        return results, nil
    }

    // timeout=0: Block for at least one completion
    _, _, errno := r.submitAndWaitRing(0, 1)  // ✅ minComplete=1
    if errno != 0 {
        return nil, fmt.Errorf("io_uring_enter wait failed: %v", errno)
    }

    drain()
    return results, nil
}
```

**Analysis**: When called with `timeout=0` (our case):
- Skips the `if timeout > 0` branch
- Calls `submitAndWaitRing(0, 1)` which waits for ≥1 completion
- **This is correct!**

### Submission Strategy

**Location**: `internal/queue/runner.go:277-315`, `internal/uring/minimal.go:546-589`

```go
// In Runner.Prime()
for tag := 0; tag < r.depth; tag++ {
    if err := r.submitInitialFetchReq(uint16(tag)); err != nil {
        return err
    }
}

func (r *Runner) submitInitialFetchReq(tag uint16) error {
    // ...
    ioCmd := &uapi.UblksrvIOCmd{
        QID:    r.queueID,
        Tag:    tag,
        Result: 0,
        Addr:   uint64(bufferAddr),
    }

    userData := udOpFetch | (uint64(r.queueID) << 16) | uint64(tag)
    cmd := uapi.UblkIOCmd(uapi.UBLK_IO_FETCH_REQ)

    // This calls SubmitIOCmd → submitOnlyCmd → io_uring_enter
    _, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
    // ...
}

// In minimal.go
func (r *minimalRing) SubmitIOCmd(...) (Result, error) {
    // ...
    if _, err := r.submitOnlyCmd(sqe); err != nil {
        return nil, err
    }
    return &minimalResult{userData: userData, value: 0, err: nil}, nil
}

func (r *minimalRing) submitOnlyCmd(sqe *sqe128) (uint32, error) {
    // ...
    // Update SQ tail
    atomic.StoreUint32(sqTail, newTail)

    // Submit WITHOUT waiting
    submitted, errno := r.submitOnly(1)  // io_uring_enter(fd, 1, 0, 0)
    // ...
}
```

**Issue #2: Per-SQE Syscall Overhead**

We call `io_uring_enter(fd, 1, 0, 0)` **for each tag** during Prime():
- Queue depth = 32 → **32 syscalls**
- C implementation: **1 syscall** (batched submission)

**Performance impact**: ~32x more syscalls during initialization
**Correctness impact**: Potentially creates race window where START_DEV sees incomplete submissions?

---

## Key Differences Summary

| Aspect | C Implementation | Our Implementation | Impact |
|--------|-----------------|-------------------|---------|
| **Submission batching** | Batch all SQEs, 1 syscall | 1 syscall per SQE | 32x syscall overhead |
| **Completion waiting** | `minComplete=1` always | `minComplete=1` (correct) | ✅ Same |
| **Timeout** | 20 seconds | Infinite (0) | Cannot recover from hangs |
| **Context cancellation** | N/A (C) | Cannot interrupt syscall | ❌ Process hangs forever |
| **Memory barriers** | Implicit (C compiler) | Explicit atomic.Store | ✅ Should be okay |
| **Thread affinity** | `sched_setaffinity` | `runtime.LockOSThread()` | ✅ Equivalent |

---

## Theories for Intermittent Hangs

### Theory 1: Memory Ordering / Visibility Race

**Hypothesis**: Kernel reads stale SQE data due to insufficient memory barriers.

**Evidence**:
- Works ~50% of the time (classic race condition symptom)
- We use `atomic.StoreUint32` for tail pointer
- But other SQE fields are written with normal stores

**Code Location**: `internal/uring/minimal.go:806-820`

```go
// Update array entry
*(*uint32)(unsafe.Add(unsafe.Pointer(sqArray), ...)) = sqIndex

// Update tail
oldTail := *sqTail
newTail := oldTail + 1

runtime.KeepAlive(sqe)  // Not a memory barrier!
atomic.StoreUint32(sqTail, newTail)
runtime.KeepAlive(sqTail)
```

**Problem**: `runtime.KeepAlive` prevents GC but **doesn't enforce memory ordering**. The kernel might see:
- Updated tail pointer (atomic store)
- But stale SQE data (normal stores not yet visible)

**Fix**: Add `atomic.LoadUint32(sqTail)` or similar before the atomic store to force synchronization.

### Theory 2: START_DEV / FETCH_REQ Race

**Hypothesis**: START_DEV completes before kernel fully processes all FETCH_REQs.

**Evidence**:
- We submit FETCH_REQs one-by-one with individual syscalls
- Small time window between each submission
- START_DEV might see incomplete initialization

**Our flow**:
```
1. Submit FETCH_REQ tag=0  (io_uring_enter)
2. Submit FETCH_REQ tag=1  (io_uring_enter)
3. ... (tiny race window here)
4. Submit FETCH_REQ tag=31 (io_uring_enter)
5. START_DEV
```

**C flow**:
```
1. Queue all FETCH_REQs in SQ (no syscalls)
2. io_uring_submit_and_wait_timeout (ONE syscall, waits for processing)
3. START_DEV
```

**Fix**: Batch all FETCH_REQ submissions into one `io_uring_enter` call.

### Theory 3: The 250ms Mystery

**From TODO.md**:
> Each FETCH_REQ takes exactly 250ms to process (kernel issue?)

**Hypothesis**: Kernel has 250ms timer/timeout for FETCH_REQ processing. If we hit some edge case (wrong thread, wrong timing, etc.), kernel waits for timer instead of processing immediately.

**Evidence**:
- Consistent 250ms delay per FETCH_REQ during initialization
- `queue_depth * 250ms` initialization time
- Too consistent to be coincidence

**Investigation needed**:
- Check kernel ublk source for timers around 250ms or HZ/4
- Test with different queue depths to confirm linearity
- Compare with C implementation timing

### Theory 4: Goroutine Scheduling / Thread Confusion

**Hypothesis**: Despite `LockOSThread()`, goroutine scheduler causes issues.

**Evidence**:
- ublk kernel driver requires **same OS thread** for all queue operations
- We call `LockOSThread()` but goroutine might have moved before then
- Prime() is called from main goroutine, ioLoop() from new goroutine

**Problem**: Initial FETCH_REQs submitted from **different OS thread** than ioLoop?

```go
// CreateAndServe path:
func (d *Device) Start() error {
    for i := 0; i < d.numQueues; i++ {
        runner := d.runners[i]
        if err := runner.Start(); err != nil {  // Calls Prime() HERE
            return err
        }
    }
    // ...
}

func (r *Runner) Start() error {
    if err := r.Prime(); err != nil {  // Main goroutine thread
        return err
    }
    go r.ioLoop()  // NEW goroutine, NEW thread after LockOSThread
    return nil
}
```

**Fix**: Call `LockOSThread` BEFORE Prime(), or submit initial FETCH_REQs from within ioLoop.

### Theory 5: Fixed File Index Issue

**Hypothesis**: We register char device fd with io_uring but don't use IOSQE_FIXED_FILE flag consistently.

**C implementation** (`.gitignored-repos/ublksrv-c/lib/ublksrv.c:200`):
```c
sqe->flags = IOSQE_FIXED_FILE;
sqe->fd = 0;  // Index into registered files array
```

**Our implementation**:
```go
sqe.flags = 0  // ❌ Not using IOSQE_FIXED_FILE
sqe.fd = int32(r.targetFd)  // Real fd, not index
```

**Investigation needed**: Check if kernel expects IOSQE_FIXED_FILE for ublk queue operations.

---

## What We're Doing Right

### ✅ Correct Architecture

- Clean separation: control plane / data plane / io_uring abstraction
- Per-tag state machine prevents double-submission
- Proper SQE128/CQE32 structure layout (48-byte cmd offset)
- IOCTL encoding for modern kernels

### ✅ Correct I/O Processing

- Read descriptor → process I/O → submit COMMIT_AND_FETCH
- Proper buffer address passing
- Result code handling (bytes on success, -errno on error)

### ✅ Correct Memory Management

- mmap descriptor array read-only
- Separate anonymous mmap for I/O buffers
- Proper cleanup in Close()

### ✅ Thread Affinity

- `runtime.LockOSThread()` equivalent to `sched_setaffinity`
- Kernel sees consistent OS thread per queue

---

## Recommended Fixes (Priority Order)

### 1. Add Memory Barriers (HIGH PRIORITY)

```go
// In submitOnlyCmd, BEFORE atomic tail update:
runtime.KeepAlive(sqe)
_ = atomic.LoadUint32(sqTail)  // Force memory sync
atomic.StoreUint32(sqTail, newTail)
```

### 2. Add Timeout to WaitForCompletion (HIGH PRIORITY)

```go
// In processRequests:
completions, err := r.ring.WaitForCompletion(20)  // 20 second timeout like C
```

This allows recovery from hangs and proper context cancellation.

### 3. Batch FETCH_REQ Submissions (MEDIUM PRIORITY)

Create a `SubmitBatch()` method that queues multiple SQEs and calls `io_uring_enter` once:

```go
func (r *Runner) Prime() error {
    batch := make([]*sqe128, r.depth)
    for tag := 0; tag < r.depth; tag++ {
        sqe := prepareFetchReqSQE(tag)
        batch[tag] = sqe
        r.tagStates[tag] = TagStateInFlightFetch
    }

    // ONE syscall to submit all
    return r.ring.SubmitBatch(batch)
}
```

### 4. Fix Thread Initialization (MEDIUM PRIORITY)

Submit initial FETCH_REQs from the ioLoop goroutine AFTER LockOSThread:

```go
func (r *Runner) Start() error {
    go func() {
        runtime.LockOSThread()
        defer runtime.UnlockOSThread()

        // Prime from the locked thread
        if err := r.Prime(); err != nil {
            r.logger.Printf("Prime failed: %v", err)
            return
        }

        r.ioLoop()
    }()
    return nil
}
```

### 5. Use Fixed File Index (LOW PRIORITY)

```go
sqe.flags = IOSQE_FIXED_FILE
sqe.fd = 0  // Index 0 in registered files array
```

---

## Alternate Theories for SOTA LLM Analysis

### Theory A: io_uring Kernel Bug

**Premise**: Our Go code is correct, but we're hitting a kernel bug in io_uring or ublk interaction.

**Evidence to check**:
- Kernel version specific behavior (we use 6.6.87)
- Known io_uring bugs around SQE128/CQE32
- ublk driver bugs with io_uring submission patterns

**Test**: Try on different kernel versions (6.1, 6.8, 6.11)

### Theory B: giouring Library Issue

**Premise**: iceber/iouring-go library has bugs we're not using it correctly.

**Evidence**: We switched to minimal custom io_uring wrapper, issues persist.

**Conclusion**: Unlikely, since we wrote our own io_uring code.

### Theory C: Initialization Ordering

**Premise**: Device/queue/ring initialization order matters more than we think.

**Check**:
- C: Open char device → create ring → register fd → submit FETCH → START_DEV
- Us: Create ring → open char device → register fd → submit FETCH → START_DEV

**Test**: Match C initialization order exactly.

### Theory D: Descriptor Array Reading

**Premise**: We're reading stale descriptors due to mmap cache coherency.

**Evidence**: Descriptors are mmap'd read-only, kernel writes them.

**Fix**: Add memory barrier before reading descriptors:
```go
atomic.LoadUintptr(&r.descPtr)  // Force cache coherency
desc := *(*uapi.UblksrvIODesc)(descPtr)
```

### Theory E: Buffer Alignment

**Premise**: I/O buffers need specific alignment (page, cache line, etc.).

**Check C implementation**:
```c
// Allocate with page alignment?
max_io_sz = round_up(max_io_sz, page_sz);
```

**Our implementation**: Uses anonymous mmap (page-aligned by default) ✅

### Theory F: Completion Queue Overflow

**Premise**: CQ fills up and kernel can't post completions.

**Evidence**: CQ size is 2x SQ size (standard), should be sufficient.

**Check**: Monitor CQ overflow counter in `io_uring_params`.

---

## Code Snippets for Expert Review

### Our SQE Preparation (minimal.go:790-830)

```go
func (r *minimalRing) submitOnlyCmd(sqe *sqe128) (uint32, error) {
    sqHead := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.head))
    sqTail := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.tail))
    sqMask := r.params.sqEntries - 1

    if (*sqTail - *sqHead) >= r.params.sqEntries {
        return 0, fmt.Errorf("submission queue full")
    }

    sqArray := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.array))
    sqIndex := *sqTail & sqMask
    sqeSlot := unsafe.Add(r.sqesAddr, 128*uintptr(sqIndex))
    *(*sqe128)(sqeSlot) = *sqe

    *(*uint32)(unsafe.Add(unsafe.Pointer(sqArray), unsafe.Sizeof(uint32(0))*uintptr(sqIndex))) = sqIndex

    oldTail := *sqTail
    newTail := oldTail + 1

    runtime.KeepAlive(sqe)
    atomic.StoreUint32(sqTail, newTail)
    runtime.KeepAlive(sqTail)

    submitted, errno := r.submitOnly(1)
    if errno != 0 {
        return 0, fmt.Errorf("io_uring_enter failed: %v", errno)
    }

    return submitted, nil
}
```

**Question for experts**: Are the memory barriers sufficient? Should we use atomic stores for the SQE fields and array entry?

### Our Completion Draining (minimal.go:595-622)

```go
drain := func() {
    cqHead := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.head))
    cqTail := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.tail))

    currentTail := atomic.LoadUint32(cqTail)
    currentHead := atomic.LoadUint32(cqHead)

    for currentHead != currentTail {
        cqMask := r.params.cqEntries - 1
        cqIndex := currentHead & cqMask
        cqeSlot := unsafe.Add(r.cqAddr, uintptr(r.params.cqOff.cqes)+uintptr(unsafe.Sizeof(cqe32{})*uintptr(cqIndex)))
        cqe := (*cqe32)(cqeSlot)

        res := &minimalResult{userData: cqe.userData, value: cqe.res}
        if cqe.res < 0 {
            res.err = fmt.Errorf("operation failed with result: %d", cqe.res)
        }
        results = append(results, res)
        currentHead++
    }

    if currentHead != atomic.LoadUint32(cqHead) {
        atomic.StoreUint32(cqHead, currentHead)
    }
}
```

**Question for experts**: Should we use `atomic.LoadUintptr` on cqeSlot before dereferencing to force cache coherency?

### Our State Machine (runner.go:463-541)

```go
func (r *Runner) handleCompletion(tag uint16, isCommit bool, result int32) error {
    r.tagMutexes[tag].Lock()
    defer r.tagMutexes[tag].Unlock()

    currentState := r.tagStates[tag]

    switch currentState {
    case TagStateInFlightFetch:
        if result == 0 {
            r.tagStates[tag] = TagStateOwned
            return r.processIOAndCommit(tag)
        } else if result == 1 {
            r.tagStates[tag] = TagStateOwned
            return fmt.Errorf("NEED_GET_DATA not implemented")
        } else {
            return fmt.Errorf("unexpected FETCH result: %d", result)
        }

    case TagStateInFlightCommit:
        if result == 0 {
            r.tagStates[tag] = TagStateOwned
            return r.processIOAndCommit(tag)
        } else if result == 1 {
            r.tagStates[tag] = TagStateOwned
            return fmt.Errorf("NEED_GET_DATA not implemented")
        } else if result < 0 {
            r.tagStates[tag] = TagStateOwned
            return fmt.Errorf("COMMIT_AND_FETCH error: %d", result)
        } else {
            return fmt.Errorf("unexpected COMMIT result: %d", result)
        }

    case TagStateOwned:
        return fmt.Errorf("unexpected completion for tag %d in Owned state", tag)

    default:
        return fmt.Errorf("invalid state %d for tag %d", currentState, tag)
    }
}
```

**Question for experts**: Is this state machine correct? C implementation uses simpler flags.

---

## Next Steps

1. **Implement memory barrier fix** (30 minutes)
2. **Add 20-second timeout** (15 minutes)
3. **Test on clean VM** - see if fixes improve reliability
4. **If still hanging**: Implement batch submission
5. **If still hanging**: Move Prime() into ioLoop after LockOSThread
6. **If still hanging**: Deep kernel debugging with bpftrace/ftrace

---

## Summary

Our implementation is **architecturally sound** but has **subtle bugs** causing intermittent hangs:

**Most Likely Culprits** (in order):
1. Memory ordering/visibility race (missing barriers)
2. Per-SQE syscall overhead creating race window
3. Thread initialization order (Prime before LockOSThread)
4. Missing timeout (makes debugging impossible)

**Less Likely But Possible**:
5. Fixed file index not being used
6. Descriptor reading cache coherency
7. Kernel bug (io_uring or ublk)

The fact that it works ~50% suggests a **race condition with timing-dependent winner**. Adding proper memory barriers and batched submission should significantly improve reliability.
