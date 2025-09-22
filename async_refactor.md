# Async START_DEV Refactor Design

## Problem Statement

The ublk START_DEV command hangs indefinitely because of a synchronization deadlock between the kernel and userspace:

1. **Kernel behavior**: START_DEV waits for all queue io_urings to submit initial FETCH_REQ commands before completing
2. **Current code**: Waits synchronously for START_DEV to complete before priming queues
3. **Result**: Deadlock - kernel waits for queues, userspace waits for kernel

## Root Cause Analysis

### Why the C implementation works
```c
// C implementation flow:
1. Create queue threads
2. Each thread:
   - Opens /dev/ublkcN
   - Creates io_uring
   - Registers files with io_uring_register_files()
   - Enters event loop (io_uring_submit_and_wait_timeout)
3. Main thread calls START_DEV
4. Kernel sees queue rings are ready
5. Queue threads submit FETCH_REQ when kernel signals
6. START_DEV completes
```

### Why our Go implementation hangs
```go
// Current Go flow:
1. Create queue runners (open /dev/ublkcN, create rings)
2. Start goroutines (but they don't submit anything yet)
3. Call START_DEV and wait synchronously // HANGS HERE
4. [Never reached] Prime queues with FETCH_REQ
```

## Solution: Async START_DEV Architecture

### Core Design Principles

1. **START_DEV must be non-blocking**: Submit without waiting for completion
2. **Queue priming happens after submission**: Once START_DEV is in flight, submit FETCH_REQs
3. **Completion polling**: Check START_DEV completion after queues are primed
4. **Error handling**: Properly handle partial initialization failures

### Implementation Architecture

```
┌─────────────┐
│  backend.go │
└──────┬──────┘
       │
       ▼
   ASYNC FLOW
       │
   ┌───▼───┐     1. Submit START_DEV
   │ Submit ├────────────────────┐
   └───┬───┘                     │
       │                         ▼
   ┌───▼────┐            ┌──────────────┐
   │ Prime  │            │ Control Ring │
   │ Queues │            │   (io_uring) │
   └───┬────┘            └──────┬───────┘
       │                         │
   ┌───▼───┐     3. Poll CQ     │
   │ Wait  ├─────────────────────┘
   │  CQ   │
   └───────┘
```

## Detailed Implementation Plan

### Phase 1: Add Async Primitives to minimalRing

```go
// internal/uring/minimal.go

// AsyncHandle represents a pending io_uring operation
type AsyncHandle struct {
    userData uint64
    ring     *minimalRing
}

// SubmitCtrlCmdAsync submits command without waiting
func (r *minimalRing) SubmitCtrlCmdAsync(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (*AsyncHandle, error) {
    // Prepare SQE as before
    sqe := prepareSQE(cmd, ctrlCmd, userData)

    // Submit without waiting
    if err := r.submitToRing(sqe); err != nil {
        return nil, err
    }

    // Call io_uring_enter to submit but don't wait
    submitted, err := r.submitOnly(1)
    if err != nil || submitted != 1 {
        return nil, fmt.Errorf("failed to submit: %v", err)
    }

    // Return handle for later polling
    return &AsyncHandle{
        userData: userData,
        ring:     r,
    }, nil
}

// Wait polls for completion of async operation
func (h *AsyncHandle) Wait(timeout time.Duration) (Result, error) {
    deadline := time.Now().Add(timeout)

    for time.Now().Before(deadline) {
        // Try to get completion without blocking
        result, err := h.ring.tryGetCompletion(h.userData)
        if err == nil {
            return result, nil
        }

        // Not ready yet, sleep briefly
        time.Sleep(10 * time.Millisecond)
    }

    return nil, fmt.Errorf("timeout waiting for completion")
}

// tryGetCompletion checks CQ for a specific completion
func (r *minimalRing) tryGetCompletion(userData uint64) (Result, error) {
    // Check CQ head/tail
    cqHead := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.head))
    cqTail := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.tail))

    if *cqHead == *cqTail {
        return nil, fmt.Errorf("no completions available")
    }

    // Process completions looking for our userData
    cqMask := r.params.cqEntries - 1
    for *cqHead != *cqTail {
        index := *cqHead & cqMask
        cqe := (*io_uring_cqe)(unsafe.Add(r.cqesAddr, unsafe.Sizeof(io_uring_cqe{})*uintptr(index)))

        if cqe.userData == userData {
            // Found our completion
            result := &minimalResult{
                userData: cqe.userData,
                value:    cqe.res,
                err:      nil,
            }

            // Advance head
            *cqHead++

            return result, nil
        }

        *cqHead++
    }

    return nil, fmt.Errorf("completion not found")
}
```

### Phase 2: Modify Controller for Async START_DEV

```go
// internal/ctrl/control.go

// StartDeviceAsync initiates START_DEV without blocking
func (c *Controller) StartDeviceAsync(devID uint32) (*AsyncStartHandle, error) {
    cmd := &uapi.UblksrvCtrlCmd{
        DevID:   devID,
        QueueID: 0xFFFF,
        Data:    uint64(os.Getpid()),
    }

    var op uint32 = uapi.UBLK_CMD_START_DEV
    if c.useIoctl {
        op = uapi.UblkCtrlCmd(op)
    }

    // Submit asynchronously
    handle, err := c.ring.SubmitCtrlCmdAsync(op, cmd, 0)
    if err != nil {
        return nil, fmt.Errorf("failed to submit START_DEV: %v", err)
    }

    return &AsyncStartHandle{
        handle: handle,
        devID:  devID,
    }, nil
}

// AsyncStartHandle wraps the async operation
type AsyncStartHandle struct {
    handle *uring.AsyncHandle
    devID  uint32
}

// Wait waits for START_DEV completion
func (h *AsyncStartHandle) Wait(timeout time.Duration) error {
    result, err := h.handle.Wait(timeout)
    if err != nil {
        return fmt.Errorf("START_DEV timeout for device %d: %v", h.devID, err)
    }

    if result.Value() < 0 {
        return fmt.Errorf("START_DEV failed with error: %d", result.Value())
    }

    return nil
}
```

### Phase 3: Orchestrate Async Flow in Backend

```go
// backend.go

func CreateAndServe(ctx context.Context, params DeviceParams, options *Options) (*Device, error) {
    // ... (device creation, ADD_DEV, SET_PARAMS as before) ...

    // STEP 1: Create and initialize queue runners
    for i := 0; i < numQueues; i++ {
        runner, err := queue.NewRunner(device.ctx, runnerConfig)
        if err != nil {
            // cleanup...
            return nil, err
        }
        device.runners[i] = runner
    }

    // STEP 2: Start queue runner goroutines (they wait initially)
    for i := 0; i < numQueues; i++ {
        if err := device.runners[i].Start(); err != nil {
            // cleanup...
            return nil, err
        }
    }

    // STEP 3: Submit START_DEV asynchronously
    fmt.Printf("*** Submitting START_DEV asynchronously\n")
    startHandle, err := ctrl.StartDeviceAsync(devID)
    if err != nil {
        // cleanup...
        return nil, fmt.Errorf("failed to submit START_DEV: %v", err)
    }

    // STEP 4: Prime queues while START_DEV is in flight
    fmt.Printf("*** Priming queues while START_DEV is pending\n")
    for i := 0; i < numQueues; i++ {
        if err := device.runners[i].Prime(); err != nil {
            // Log but continue - some queues might fail initially
            fmt.Printf("*** Warning: Failed to prime queue %d: %v\n", i, err)
        }
    }

    // STEP 5: Wait for START_DEV completion
    fmt.Printf("*** Waiting for START_DEV completion\n")
    if err := startHandle.Wait(5 * time.Second); err != nil {
        // cleanup...
        return nil, fmt.Errorf("START_DEV failed: %v", err)
    }

    fmt.Printf("*** START_DEV completed successfully\n")
    device.started = true

    return device, nil
}
```

### Phase 4: Queue Runner Adjustments

```go
// internal/queue/runner.go

// Prime can now handle START_DEV in progress
func (r *Runner) Prime() error {
    if r.charFd < 0 || r.ring == nil {
        return fmt.Errorf("runner not initialized")
    }

    // Submit FETCH_REQ for each tag
    for tag := 0; tag < r.depth; tag++ {
        if err := r.submitFetchReq(uint16(tag)); err != nil {
            // If we get EOPNOTSUPP, START_DEV might not be ready yet
            if errno, ok := err.(syscall.Errno); ok && errno == syscall.EOPNOTSUPP {
                // This is expected if START_DEV hasn't been processed yet
                // The queue runner loop will retry
                return fmt.Errorf("device not ready (START_DEV pending): %w", err)
            }
            return fmt.Errorf("submit initial FETCH_REQ[%d]: %w", tag, err)
        }
    }
    return nil
}

// Add retry logic to ioLoop for initial priming
func (r *Runner) ioLoop() {
    primed := false
    retryCount := 0

    for !primed && retryCount < 50 { // Try for up to 5 seconds
        if err := r.Prime(); err != nil {
            if strings.Contains(err.Error(), "START_DEV pending") {
                time.Sleep(100 * time.Millisecond)
                retryCount++
                continue
            }
            r.logger.Printf("Failed to prime queue: %v", err)
            return
        }
        primed = true
    }

    if !primed {
        r.logger.Printf("Failed to prime queue after retries")
        return
    }

    // Continue with normal I/O processing loop
    for {
        select {
        case <-r.ctx.Done():
            return
        default:
            r.processRequests()
        }
    }
}
```

## Verification Strategy

### Test Plan

1. **Unit Test**: Async handle wait/timeout behavior
2. **Integration Test**: Full async START_DEV flow
3. **Stress Test**: Multiple devices starting concurrently
4. **Error Test**: Handle partial failures (some queues fail to prime)

### Success Criteria

1. ✅ START_DEV does not hang
2. ✅ /dev/ublkb<N> device appears after completion
3. ✅ All queues successfully submit FETCH_REQ
4. ✅ I/O operations work after device starts
5. ✅ Proper cleanup on failure

### Debug Verification

```bash
# On VM, after implementation:
sudo ./ublk-mem --size=16M

# Should see:
*** Submitting START_DEV asynchronously
*** Priming queues while START_DEV is pending
*** Waiting for START_DEV completion
*** START_DEV completed successfully

# Verify device exists:
ls -la /dev/ublkb*

# Test I/O:
sudo dd if=/dev/zero of=/dev/ublkb0 bs=4k count=100
```

## Risk Assessment

### Potential Issues

1. **Race Conditions**: START_DEV might complete before all queues prime
   - **Mitigation**: Queue runners retry on EOPNOTSUPP

2. **Timeout Selection**: How long to wait for START_DEV?
   - **Mitigation**: Configurable timeout, default 5 seconds

3. **Partial Success**: Some queues prime, others fail
   - **Mitigation**: Log warnings but continue if majority succeed

4. **Memory Ordering**: CQ updates might not be immediately visible
   - **Mitigation**: Use memory barriers if needed

## Implementation Timeline

1. **Hour 1-2**: Implement async primitives in minimalRing
2. **Hour 3**: Update Controller with async START_DEV
3. **Hour 4**: Refactor backend.go orchestration
4. **Hour 5**: Update queue runners with retry logic
5. **Hour 6**: Testing and debugging
6. **Hour 7**: Documentation and cleanup

## Alternative Approaches Considered

### Option 1: Full io_uring Library
- **Pros**: More features, battle-tested
- **Cons**: Requires cgo or complex pure-Go library
- **Decision**: Stay with minimal implementation for now

### Option 2: Kernel Module Changes
- **Pros**: Could make START_DEV truly synchronous
- **Cons**: Requires kernel changes, not portable
- **Decision**: Work with existing kernel behavior

### Option 3: Pre-submit FETCH_REQ with Deferred Processing
- **Pros**: Simpler control flow
- **Cons**: Kernel rejects with EOPNOTSUPP before START_DEV
- **Decision**: Not viable with current kernel

## Conclusion

The async refactor is necessary because the kernel's START_DEV behavior requires userspace to have queue rings ready and waiting. The solution maintains compatibility with the kernel's expectations while avoiding the deadlock through asynchronous execution and careful orchestration of the initialization sequence.