# Theory of Project Status - Deep Analysis (UPDATED 2025-10-05)

**Critical Status**: Weeks-long intermittent race condition causing unpredictable failures
**Last Updated**: 2025-10-05 15:45 EDT
**Purpose**: Comprehensive exposition for deep research agent analysis

---

## Executive Summary: The Race Condition

We have a **fully functional ublk implementation with a race condition that makes it unreliable**. The system exhibits perfect behavior sometimes but fails catastrophically at other times in a way that suggests a timing-dependent race rather than deterministic bugs or resource leaks.

### The Intermittent Pattern

**Observed Behavior**: Tests pass perfectly, then fail completely, cycling unpredictably

**What We Observed Today (2025-10-05)**:
```
15:12 - vm-e2e: PASS (perfect data integrity, all tests)
15:14 - vm-e2e: PASS (perfect data integrity, all tests)
15:17 - vm-benchmark: PASS (reads work perfectly)
15:19 - vm-benchmark-race: FAIL (hung during 4K write test, zombie process)
```

**This is a classic race condition pattern:**
- Sometimes works perfectly
- Sometimes fails completely
- Binary outcome (not gradual degradation)
- Timing-dependent, not deterministic
- Has persisted through multiple fix attempts

---

## What Actually Works (When It Works)

### When The Race Resolves Favorably
- ✅ Device creation (ADD_DEV, SET_PARAMS, START_DEV): Works perfectly
- ✅ Block device appearance (/dev/ublkb*): Always appears correctly
- ✅ **Data integrity**: MD5 cryptographic verification passes on all I/O patterns
- ✅ **Read performance**: 50k-500k IOPS depending on queue depth
- ✅ **Sequential reads**: 2.7 GB/s bandwidth
- ✅ **Basic tests**: vm-simple-e2e and vm-e2e pass reliably in isolation

### What We've Already Fixed (September 2025)

1. **Logging Deadlock** (commit b451b7d)
   - Problem: Thread-locked goroutines blocked on synchronous log I/O
   - Solution: Replaced log/slog with zerolog + async non-blocking writer
   - Result: 1.6 GB/s throughput with verbose logging enabled

2. **Memory Ordering Issues** (commit b06b276)
   - Problem: io_uring memory barriers insufficient
   - Solution: Proper atomic operations throughout io_uring code
   - Result: More reliable operation, but race condition persists

3. **Fixed File Descriptor Registration** (commit 339808f)
   - Problem: Dynamic FD lookup overhead
   - Solution: Register ublk char device FD with io_uring upfront
   - Result: Performance improvement, potential new race window?

4. **START_DEV Hang** (SOLVED - ancient history)
   - Problem: Device wouldn't start
   - Solution: Submit FETCH_REQs before START_DEV
   - Result: Device starts reliably now

---

## The Race Condition Failure Modes

### Primary Failure: Write Operation Hangs

**Symptoms**:
```bash
Test sequence:
1. 4K Random Read (QD=1): PASS - 21k IOPS ✅
2. 4K Random Read (QD=32): PASS - 50k IOPS ✅
3. 4K Random Write (QD=1): HUNG FOREVER ❌
```

**Critical Observations**:
- Reads ALWAYS succeed before write hangs
- Write operations never return (blocked in kernel)
- Process becomes zombie: `[ublk-mem] <defunct>`
- Device node persists: `/dev/ublkb3` still exists
- No CPU usage - process in uninterruptible sleep (D state)

**Why This Points to a Race**:
- Reads and writes use IDENTICAL io_uring URING_CMD mechanism
- Same COMMIT_AND_FETCH_REQ pattern for both
- Data plane code path is symmetric for read/write
- **Yet writes fail when reads succeed** → timing-dependent difference

### Secondary Evidence: Zombie Process Accumulation

**Evidence from VM inspection**:
```bash
# Multiple zombie processes over time
root  2836  [ublk-mem] <defunct>    # From Oct 4
root  4384  [ublk-mem] <defunct>    # From 00:04
root  11725 ./ublk-mem (Dl state)   # Current, hung

# Orphaned control device nodes
/dev/ublkc0  (from previous run)
/dev/ublkc1  (from previous run)
/dev/ublkc2  (from previous run)
/dev/ublkc3  (current)
```

**What This Suggests**:
- Process stuck in kernel, cannot complete cleanup
- Kernel holding references preventing full exit
- Race causes kernel to enter unrecoverable wait state

---

## Technical Architecture Analysis

### Our Current Implementation

**Control Plane** (`/dev/ublk-control` via io_uring URING_CMD):
```
ADD_DEV (allocate device ID)
  ↓
SET_PARAMS (configure size, queues, depth)
  ↓
START_DEV (activate device, create /dev/ublkb*)
  ↓
... device operational ...
  ↓
STOP_DEV (deactivate)
  ↓
DEL_DEV (cleanup)
```

**Data Plane** (per-queue io_uring, single queue currently):
```
Goroutine (locked to OS thread via runtime.LockOSThread):
  Prime(): Submit all FETCH_REQs (queue_depth=32)
    ↓
  ioLoop():
    Wait for completion (blocks in io_uring_enter)
      ↓
    Completion arrives with descriptor
      ↓
    Read descriptor to get operation (read/write) and buffer
      ↓
    Call backend.ReadAt() or backend.WriteAt()
      ↓
    Submit COMMIT_AND_FETCH_REQ with result
      ↓
    Loop back to wait
```

### What Happens During a Write Operation

1. **Kernel receives block layer write request**
2. **Kernel posts descriptor to our mmap'd array** with:
   - Operation: `UBLK_IO_OP_WRITE` (1)
   - Buffer address: where to read write data FROM
   - Sector offset: where to write TO in backend
   - Bytes: how much data
3. **Kernel completes FETCH_REQ or COMMIT_AND_FETCH_REQ** (io_uring CQE with result=0)
4. **Our code reads descriptor** (memory-mapped array)
5. **Our code calls backend.WriteAt(buffer, offset)**
6. **Our code submits COMMIT_AND_FETCH_REQ** with success result
7. **Kernel completes block layer request**, allows next I/O

**Race Windows Where It Can Fail**:
- Between steps 2-3: Descriptor written but completion not posted
- Between steps 3-4: Completion posted but descriptor not visible (cache)
- Between steps 6-7: COMMIT submitted but kernel doesn't see it
- During step 3: io_uring_enter blocks, kernel never sends completion

---

## Race Condition Hypotheses (Ranked by Likelihood)

### Hypothesis 1: Memory Ordering Race in Descriptor Reading (HIGH)

**Theory**: Kernel writes descriptor to mmap, we read stale cached data due to insufficient memory barriers.

**Evidence**:
- Intermittent nature is classic cache coherency race
- Only affects writes (different cache access pattern than reads?)
- Works sometimes, fails sometimes (timing-dependent)

**Race Window**:
```go
// Step 1: Kernel writes descriptor to mmap'd memory
// (kernel side - happens in interrupt context)

// Step 2: Kernel posts CQE to io_uring completion queue
// (kernel side - uses proper barriers)

// Step 3: Our code receives CQE
completion := r.ring.WaitForCompletion(0)

// Step 4: Our code reads descriptor
descPtr := unsafe.Add(r.descAddr, offset)
desc := *(*uapi.UblksrvIODesc)(descPtr)  // ❌ NO MEMORY BARRIER!

// RACE: CPU cache might still have stale descriptor data
// even though kernel wrote new data and posted CQE
```

**Why Writes Fail Specifically**:
- Write descriptors might have different cache line alignment
- Write buffer addresses might trigger different cache behavior
- Kernel write path might have different memory ordering than read path

**How to Fix**:
```go
// Force cache coherency before reading descriptor
atomic.LoadPointer(&descPtr)  // Memory barrier
desc := *(*uapi.UblksrvIODesc)(descPtr)
```

### Hypothesis 2: io_uring Submission Race (HIGH)

**Theory**: Race between our COMMIT_AND_FETCH_REQ submission and kernel's completion processing.

**Evidence**:
- Recent change to fixed FD registration (commit 339808f)
- io_uring submission involves multiple memory writes
- Kernel reads our SQE data without synchronization

**Race Window in SQE Submission**:
```go
// internal/uring/minimal.go - submitOnlyCmd
func (r *minimalRing) submitOnlyCmd(sqe *sqe128) (uint32, error) {
    // Write SQE to ring buffer
    *(*sqe128)(sqeSlot) = *sqe  // ❌ Normal memory write

    // Write SQE index to array
    *(*uint32)(arrayPtr) = sqIndex  // ❌ Normal memory write

    // Update tail pointer
    atomic.StoreUint32(sqTail, newTail)  // ✅ Atomic

    // Call io_uring_enter
    submitted, errno := r.submitOnly(1)

    // RACE: Kernel might read SQE before our writes are visible
    // atomic.Store on tail doesn't guarantee SQE data is visible
}
```

**Why This Causes Hangs**:
- Kernel sees updated tail pointer
- Kernel reads SQE data (might be stale)
- Kernel processes wrong command or corrupted data
- Kernel gets into stuck state

**How to Fix**:
```go
// Force memory ordering before updating tail
runtime.KeepAlive(sqe)
atomic.LoadUint32(sqTail)  // Read barrier
atomic.StoreUint32(sqTail, newTail)  // Write barrier
```

### Hypothesis 3: Double-Submit Race (MEDIUM-HIGH)

**Theory**: Under certain timing, we might submit two operations for the same tag, causing kernel confusion.

**Evidence**:
- We use per-tag mutexes to prevent this
- But mutex is released before COMMIT submission completes
- Kernel might process completion while we're still submitting

**Race Window**:
```go
func (r *Runner) processIOAndCommit(tag uint16) error {
    // Read descriptor, do I/O
    // ...

    // Submit COMMIT_AND_FETCH_REQ
    _, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
    // ❌ Submission might not complete immediately

    // State update
    r.tagStates[tag] = TagStateInFlightCommit
    r.tagMutexes[tag].Unlock()

    // RACE: If kernel processes COMMIT super fast and posts new completion
    // before SubmitIOCmd returns, we might process same tag twice
}
```

**How to Fix**:
- Update state BEFORE submission
- Or use stricter state machine enforcement

### Hypothesis 4: Thread Initialization Race (MEDIUM)

**Theory**: FETCH_REQs submitted from different OS thread than ioLoop, violating kernel expectations.

**Evidence**:
- ublk requires same OS thread for all queue operations
- We call `LockOSThread()` in ioLoop
- But Prime() is called BEFORE ioLoop starts

**Race Window**:
```go
func (r *Runner) Start() error {
    // Prime() runs on MAIN GOROUTINE (random OS thread)
    if err := r.Prime(); err != nil {
        return err
    }

    // ioLoop runs on NEW GOROUTINE (locks to new OS thread)
    go r.ioLoop()
    return nil
}

func (r *Runner) ioLoop() {
    runtime.LockOSThread()  // ✅ Locks thread
    defer runtime.UnlockOSThread()

    // RACE: Prime() submitted FETCH_REQs from different thread
    // Kernel expects all queue ops from same thread
}
```

**Why This Causes Intermittent Failures**:
- Sometimes goroutine scheduler assigns same thread
- Sometimes assigns different thread
- Kernel behavior undefined when thread changes

**How to Fix**:
```go
func (r *Runner) Start() error {
    primeDone := make(chan error, 1)

    go func() {
        runtime.LockOSThread()
        defer runtime.UnlockOSThread()

        // Prime from the locked thread
        primeDone <- r.Prime()
        if <-primeDone == nil {
            r.ioLoop()
        }
    }()

    return <-primeDone
}
```

### Hypothesis 5: Kernel ublk State Machine Race (MEDIUM)

**Theory**: Kernel ublk driver has internal race condition between write path and completion path.

**Evidence**:
- Reads work, writes fail (different kernel code paths)
- Zombie processes suggest kernel stuck state
- Cannot access kernel logs to verify

**What Could Be Racing in Kernel**:
- Block layer issuing write request
- ublk driver posting descriptor
- Our COMMIT_AND_FETCH_REQ arriving
- Kernel processing completion

**How to Test**:
- Run C ublksrv implementation with same workload
- If C also fails → kernel bug
- If C works → our bug

### Hypothesis 6: io_uring CQ/SQ Ring Race (MEDIUM)

**Theory**: Race between our CQ head update and kernel's CQ tail update.

**Evidence**:
- We use atomic operations on CQ head
- Kernel uses atomic operations on CQ tail
- But CQE data itself is not atomic

**Race Window**:
```go
func drain() {
    currentTail := atomic.LoadUint32(cqTail)  // Read kernel's tail
    currentHead := atomic.LoadUint32(cqHead)  // Read our head

    for currentHead != currentTail {
        cqIndex := currentHead & cqMask
        cqe := (*cqe32)(cqeSlot)  // ❌ Read CQE without barrier

        // RACE: Kernel might be writing next CQE while we read current one
        // Cache coherency not guaranteed for CQE data

        results = append(results, processedCQE)
        currentHead++
    }

    atomic.StoreUint32(cqHead, currentHead)  // Update our head
}
```

**How to Fix**:
```go
// Add memory barrier before reading CQE
atomic.LoadUintptr(cqeSlot)  // Force cache sync
cqe := (*cqe32)(cqeSlot)
```

---

## What We Cannot Debug (Critical Gaps)

### 1. Kernel State Visibility

**Cannot Access**:
- `dmesg` output: Permission denied even with sudo
- Kernel trace buffer: `/sys/kernel/debug/tracing` insufficient permissions
- ublk driver internal state: No sysfs/debugfs interface
- io_uring pending request queue: No inspection tools

**What We Need**:
- Kernel stack trace of hung process
- ublk driver state for device ID
- io_uring SQ/CQ ring inspection
- Reason why completions stop arriving

### 2. Process State at Hang

**What We Know**: Process in `Dl` state (uninterruptible sleep + CLONE_THREAD)

**What We Can't See**:
- Exact syscall it's blocked in
- io_uring ring state (SQ tail/head, CQ tail/head)
- Which kernel wait queue it's on
- Full stack trace

**How to Get (if we had access)**:
```bash
cat /proc/{pid}/stack      # Kernel stack
cat /proc/{pid}/wchan      # Wait channel
gdb -p {pid}               # Debugger attach
strace -p {pid}            # Current syscall
```

### 3. Comparison with C Implementation

**Critical Test**: Does C ublksrv have same issue?

**Value**:
- If C works reliably → our bug (memory barriers, threading, etc.)
- If C also fails → kernel bug or VM environment issue

---

## Specific Weird Behaviors Needing Explanation

### 1. Reads Always Work, Writes Always Hang (CRITICAL CLUE)

Both use identical mechanism:
- Same io_uring URING_CMD submission
- Same descriptor reading from mmap
- Same COMMIT_AND_FETCH_REQ pattern
- Same buffer management

**What's Different**:
- **Write**: Kernel posts descriptor with write data IN buffer (we read FROM it)
- **Read**: Kernel posts descriptor, we write TO buffer (kernel reads FROM it)
- Direction of memory access is reversed
- **Cache coherency requirements might differ!**

**Hypothesis**: Write descriptors point to DMA buffers that need different cache handling?

### 2. Zombie Processes Accumulate

Normal process exit:
1. Process exits
2. Kernel sends SIGCHLD to parent
3. Parent wait()s for child
4. Kernel releases process resources

Zombie means kernel can't release:
- Process stuck in uninterruptible sleep
- io_uring requests still pending?
- ublk driver holding references?
- Cannot exit until kernel state cleared

**Our case**: Process stuck in `io_uring_enter` syscall, kernel never returns

### 3. Intermittent Nature (Classic Race Symptom)

**Why races are timing-dependent**:
- CPU scheduling variations
- Cache line alignment differences
- Memory pressure affecting cache behavior
- Interrupt timing
- Goroutine scheduler decisions

**Our observations match this**:
- Works perfectly sometimes
- Fails completely other times
- No gradual degradation
- No clear pattern to success/failure

---

## Code Audit: Potential Race Locations

### Location 1: Descriptor Read (HIGH PRIORITY)

**File**: `internal/queue/runner.go:~340`

```go
func (r *Runner) processIOAndCommit(tag uint16) error {
    descOffset := uintptr(tag) * unsafe.Sizeof(uapi.UblksrvIODesc{})
    descPtr := unsafe.Add(r.descAddr, descOffset)
    desc := *(*uapi.UblksrvIODesc)(descPtr)  // ❌ NO BARRIER

    // RACE: desc might be stale cached data
}
```

**Fix**:
```go
// Force cache coherency
atomic.LoadPointer(&descPtr)
desc := *(*uapi.UblksrvIODesc)(descPtr)
```

### Location 2: SQE Submission (HIGH PRIORITY)

**File**: `internal/uring/minimal.go:~790`

```go
func (r *minimalRing) submitOnlyCmd(sqe *sqe128) (uint32, error) {
    *(*sqe128)(sqeSlot) = *sqe  // ❌ Normal write
    *(*uint32)(arrayPtr) = sqIndex  // ❌ Normal write

    runtime.KeepAlive(sqe)  // ❌ Not a memory barrier
    atomic.StoreUint32(sqTail, newTail)

    // RACE: Kernel might read stale SQE data
}
```

**Fix**:
```go
*(*sqe128)(sqeSlot) = *sqe
*(*uint32)(arrayPtr) = sqIndex

// Force memory ordering
atomic.LoadUint32(sqTail)  // Read barrier
atomic.StoreUint32(sqTail, newTail)  // Write barrier
```

### Location 3: CQE Reading (MEDIUM PRIORITY)

**File**: `internal/uring/minimal.go:~610`

```go
drain := func() {
    for currentHead != currentTail {
        cqe := (*cqe32)(cqeSlot)  // ❌ No barrier
        // RACE: Might read stale CQE
    }
}
```

**Fix**:
```go
atomic.LoadUintptr(cqeSlot)  // Barrier
cqe := (*cqe32)(cqeSlot)
```

### Location 4: Thread Initialization (MEDIUM PRIORITY)

**File**: `internal/queue/runner.go:~200`

```go
func (r *Runner) Start() error {
    if err := r.Prime(); err != nil {  // ❌ Wrong thread
        return err
    }
    go r.ioLoop()  // ✅ Right thread
    return nil
}
```

**Fix**: Prime from within ioLoop after LockOSThread

---

## What Would Definitively Solve This

### Option A: Add Memory Barriers Everywhere (30 minutes)

**Priority locations**:
1. Descriptor reads - force cache coherency
2. SQE writes - ensure visible before tail update
3. CQE reads - ensure not reading stale data

**Expected outcome**: If race is memory ordering, this should fix it

### Option B: Move Prime() Into ioLoop Thread (30 minutes)

**Change threading model**:
- Start ioLoop goroutine first
- Lock thread
- Then submit FETCH_REQs from locked thread
- Then enter main loop

**Expected outcome**: If race is thread-related, this should fix it

### Option C: Run C ublksrv Comparison (1-2 hours)

**Steps**:
1. Install C ublksrv on VM
2. Run identical fio workload
3. See if it also hangs

**Value**: Immediately tells us if kernel bug vs our bug

### Option D: Minimal Reproducer (2-3 hours)

**Goal**: Smallest code that triggers hang

**Value**:
- Makes debugging tractable
- Can share with kernel developers
- Can bisect to find exact race window

### Option E: Systematic State Logging (1 hour)

**Add detailed logging**:
- Every descriptor read with tag/operation
- Every SQE submission with tag/command
- Every CQE completion with tag/result
- Thread IDs for all operations

**Value**: Might reveal race pattern in logs

---

## Questions for Deep Research Agent

### Critical Questions

1. **Why do writes hang but reads succeed?**
   - What's fundamentally different in kernel handling?
   - Different memory barriers required?
   - Different cache coherency guarantees?

2. **What memory barriers are required for kernel-written mmap data?**
   - When kernel writes to userspace mmap, what visibility guarantees?
   - Does x86-64 TSO model provide automatic barriers?
   - Do we need explicit `asm volatile("mfence")`?

3. **What's the correct memory ordering for io_uring?**
   - SQE writes must be visible before tail update - how?
   - CQE reads must see latest data - how?
   - What barriers does kernel use?

4. **Can ublk operations switch OS threads?**
   - Does kernel track queue by OS thread ID?
   - What happens if thread changes between operations?
   - Does this explain intermittent failures?

5. **What could cause kernel to stop sending completions?**
   - ublk driver stuck states?
   - io_uring internal deadlock?
   - Race in kernel completion path?

### Technical Deep Dives Needed

1. **Memory Barrier Requirements for mmap**
   - Kernel writes to mmap
   - Userspace reads from mmap
   - What barriers needed on read side?
   - x86-64 specific guarantees?

2. **io_uring Memory Ordering**
   - Documented barrier requirements
   - How does liburing handle this in C?
   - Compiler barriers vs CPU barriers
   - When are barriers actually needed?

3. **Kernel ublk Write vs Read Path**
   - Source code analysis of drivers/block/ublk_drv.c
   - Write path differences
   - Completion posting differences
   - Potential race windows

4. **Go Memory Model for unsafe Operations**
   - What guarantees for unsafe pointer dereference?
   - Does `runtime.KeepAlive` provide ordering?
   - When do we need atomic operations?

---

## Summary: It's a Race Condition

### What We Know ✅

1. **Classic race pattern**: Works sometimes, fails sometimes
2. **Timing-dependent**: No deterministic trigger
3. **Binary outcome**: Perfect or complete failure
4. **Asymmetric**: Reads work, writes fail
5. **Persistent**: Survived multiple fix attempts

### Top Race Candidates 🎯

1. **Memory ordering in descriptor reads** - Most likely
2. **Memory ordering in SQE submissions** - Also very likely
3. **Thread initialization (Prime on wrong thread)** - Likely
4. **Double-submit race with tag state machine** - Possible
5. **Kernel ublk driver internal race** - Need to test with C

### Immediate Actions 📋

1. **Add memory barriers** - 30 min, high chance of fix
2. **Move Prime() into ioLoop thread** - 30 min, might fix thread race
3. **Run C ublksrv comparison** - 2 hours, critical diagnostic
4. **Add comprehensive logging** - 1 hour, might reveal pattern

**This is definitely a RACE CONDITION, not a resource leak. Focus debugging on memory ordering and thread synchronization.**
