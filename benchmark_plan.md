# io_uring Submission Batching Plan

## Root Cause Identified

**Problem**: We're calling `io_uring_enter()` for every single I/O instead of batching.

### CPU Profile Evidence
```
53.64%  syscall.Syscall6          (io_uring_enter syscalls)
50.91%  submitOnlyCmd             (called once per I/O!)
```

### Current Flow (WRONG)
```
WaitForCompletion() → returns N completions
For each of N completions:
    handleCompletion()
    processIOAndCommit()
    submitCommitAndFetch()
        → SubmitIOCmd()
            → submitOnlyCmd()
                → io_uring_enter(1)  ← SYSCALL FOR EVERY I/O!
```

If we get 64 completions, we make **64 syscalls** instead of **1**.

### Correct Flow (BATCHED)
```
WaitForCompletion() → returns N completions
For each of N completions:
    handleCompletion()
    processIOAndCommit()
    prepareSQE()          ← Just writes to ring memory, no syscall

flushSubmissions()        ← ONE io_uring_enter(N) for entire batch
```

## Design

This is the standard io_uring batching pattern used by liburing, SPDK, and all
production io_uring implementations. The key insight:

1. **Prepare phase**: Write SQEs to ring memory, increment local tail counter
2. **Submit phase**: Atomic store local tail to shared tail, one syscall

The kernel only sees submissions when we update the shared tail pointer.

### Key Insight: Ring Full Cannot Happen

In normal operation, ring full is impossible:
- We receive N completions from CQ (those SQEs were already consumed by kernel)
- We prepare N new SQEs (replacing the consumed ones)
- SQ depth = queue depth, state machine guarantees ≤depth in-flight operations
- Therefore: always have exactly the slots we need

Ring full would indicate a bug in the state machine.

### 1. New Ring Field

```go
type minimalRing struct {
    // ... existing fields ...

    // Batching: local tail tracks prepared-but-not-submitted SQEs
    // The kernel sees submissions only when we store this to shared sqTail
    sqTailLocal uint32
}
```

Only one field needed - pending count is implicit: `sqTailLocal - *sqTail`

### 2. prepareSQE - Write to Ring Without Syscall

```go
func (r *minimalRing) prepareSQE(sqe *sqe128) error {
    sqHead := atomic.LoadUint32(sqHeadPtr)

    // Check capacity (should never fail in normal operation)
    if r.sqTailLocal - sqHead >= r.params.sqEntries {
        return ErrRingFull
    }

    // Write SQE to slot
    sqIndex := r.sqTailLocal & sqMask
    sqeSlot := r.sqesAddr + 128*sqIndex
    *sqeSlot = *sqe

    // Update array entry
    sqArray[sqIndex] = sqIndex

    // Increment LOCAL tail only - kernel doesn't see this yet
    r.sqTailLocal++

    // NO memory barrier here
    // NO atomic store to shared tail
    // NO syscall
    return nil
}
```

### 3. flushSubmissions - One Syscall for All

```go
func (r *minimalRing) flushSubmissions() (uint32, error) {
    currentTail := atomic.LoadUint32(sqTailPtr)
    pending := r.sqTailLocal - currentTail

    if pending == 0 {
        return 0, nil  // Nothing to submit
    }

    // CRITICAL: Memory barrier ensures all SQE writes are visible
    // before kernel sees the new tail value
    Sfence()

    // Publish new tail to kernel
    atomic.StoreUint32(sqTailPtr, r.sqTailLocal)

    // ONE syscall for entire batch
    submitted, errno := io_uring_enter(pending, 0, 0)
    if errno != 0 {
        return 0, fmt.Errorf("io_uring_enter: %v", errno)
    }

    return submitted, nil
}
```

### 4. Interface Changes

```go
type Ring interface {
    // ... existing methods ...

    // PrepareIOCmd prepares an I/O command without submitting
    // Call FlushSubmissions() to submit all prepared commands
    PrepareIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error

    // FlushSubmissions submits all prepared SQEs with one syscall
    FlushSubmissions() (uint32, error)
}
```

### 5. Runner Changes

```go
func (r *Runner) processRequests() error {
    completions, err := r.ring.WaitForCompletion(0)
    if err != nil {
        return err
    }
    if len(completions) == 0 {
        return nil
    }

    // Process all completions - each prepares an SQE
    for _, completion := range completions {
        // ... validate ...
        if err := r.handleCompletion(tag, isCommit, result); err != nil {
            return err
        }
    }

    // ONE syscall to submit all prepared SQEs
    _, err = r.ring.FlushSubmissions()
    return err
}

func (r *Runner) submitCommitAndFetch(...) error {
    // ... prepare ioCmd ...

    // Prepare SQE (no syscall)
    if err := r.ring.PrepareIOCmd(cmd, ioCmd, userData); err != nil {
        return err
    }

    r.tagStates[tag] = TagStateInFlightCommit
    return nil
}
```

## Implementation Steps

1. Add `sqTailLocal` field to `minimalRing`, initialize from shared tail
2. Add `ErrRingFull` sentinel error
3. Implement `prepareSQE()` - copies SQE, increments local tail
4. Implement `flushSubmissions()` - barrier, store tail, one syscall
5. Add `PrepareIOCmd()` and `FlushSubmissions()` to Ring interface
6. Update `SubmitIOCmd()` to call prepare + flush (backward compat)
7. Update runner `submitCommitAndFetch()` to use `PrepareIOCmd()`
8. Add `FlushSubmissions()` call at end of `processRequests()`

## Expected Impact

**Before**: N completions → N syscalls
**After**: N completions → 1 syscall

With queue_depth=64:
- 64x reduction in syscall overhead
- Target: 100k+ IOPS with 4 jobs (vs current 17k)

## Files to Modify

| File | Changes |
|------|---------|
| `internal/uring/interface.go` | Add PrepareIOCmd, FlushSubmissions to interface |
| `internal/uring/minimal.go` | Add sqTailLocal, prepareSQE, flushSubmissions |
| `internal/queue/runner.go` | Use PrepareIOCmd, add flush call |
