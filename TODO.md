# TODO.md - Fix START_DEV Hang

## THE ONE PROBLEM: START_DEV hangs forever

Current status:
- ADD_DEV works (returns device ID)
- SET_PARAMS works (returns 0)
- START_DEV hangs in io_uring_enter syscall (never completes)

## Root Cause Analysis:

**The kernel waits for queue FETCH_REQ commands before completing START_DEV**

This creates a chicken-and-egg problem:
1. We can't wait for START_DEV completion - it hangs waiting for queue FETCH_REQs
2. We can't submit FETCH_REQs before START_DEV - kernel returns -95 (EOPNOTSUPP)
3. Fire-and-forget START_DEV doesn't hang but doesn't complete either

## Progress Made:
- ✅ File registration added to queue io_urings (matches C code)
- ✅ Fire-and-forget START_DEV prevents hanging
- ✅ Queue runners start goroutines before START_DEV
- ❌ Block device still not created (START_DEV not actually completing)

## Solution Required:

**See [async_refactor.md](./async_refactor.md) for complete design**

Need to implement async START_DEV flow:
1. Submit START_DEV without waiting (fire-and-forget)
2. Prime all queues with initial FETCH_REQ commands
3. Poll for START_DEV completion in CQ
4. Only then will /dev/ublkb<N> be created

This requires refactoring the control flow to handle START_DEV asynchronously.

## Implementation Tasks:

### Phase 1: Core Async Support
- [ ] Add `SubmitCtrlCmdAsync` to minimalRing
- [ ] Add `AsyncHandle` type for pending operations
- [ ] Implement `tryGetCompletion` for polling CQ
- [ ] Add timeout support to async wait

### Phase 2: Controller Changes
- [ ] Create `StartDeviceAsync` method
- [ ] Return `AsyncStartHandle` instead of blocking
- [ ] Keep synchronous version for other commands

### Phase 3: Backend Orchestration
- [ ] Submit START_DEV asynchronously
- [ ] Prime queues while START_DEV pending
- [ ] Wait for START_DEV completion with timeout
- [ ] Handle partial failures gracefully

### Phase 4: Queue Runner Updates
- [ ] Add retry logic for EOPNOTSUPP errors
- [ ] Handle START_DEV-in-progress state
- [ ] Ensure proper synchronization

## Files to modify:
- `/internal/uring/minimal.go` - Add async primitives
- `/internal/ctrl/control.go` - Add StartDeviceAsync
- `/backend.go` - Orchestrate async flow
- `/internal/queue/runner.go` - Add retry logic

## Testing:
```bash
# After implementation, test on VM:
make build && make vm-copy
./vm-ssh.sh "cd ~/ublk-test && sudo ./ublk-mem --size=16M"

# Success indicators:
# - No hang at START_DEV
# - /dev/ublkb0 created
# - Can perform I/O operations
```

## Test command:
```bash
make build && make vm-copy
./vm-ssh.sh "cd ~/ublk-test && sudo timeout 5 ./ublk-mem --size=16M -v"
```

## Success criteria:
START_DEV returns without hanging. That's it. Nothing else matters until this works.