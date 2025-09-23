# TODO.md - Current Status

## MAJOR BREAKTHROUGH: Device Creation Works!

Current status:
- ✅ ADD_DEV works (returns device ID)
- ✅ SET_PARAMS works (returns 0)
- ✅ START_DEV completes successfully (async implementation working)
- ✅ Block device `/dev/ublkb*` is created
- ✅ Basic I/O operations work (can write/read small amounts)
- ❌ Large I/O operations fail (need proper handling of I/O > 64KB)

## What Was Fixed:

### The START_DEV Hang Issue (SOLVED)
- **Root cause**: Kernel waits for FETCH_REQs before completing START_DEV
- **Solution**: Implemented async START_DEV with fire-and-forget submission
- **Result**: Device creation now works reliably

### The IOCTL Encoding Issue (SOLVED)
- **Root cause**: Modern kernels require IOCTL-encoded commands for ublk
- **Solution**: Added proper IOCTL encoding for all queue commands
- **Result**: FETCH_REQ commands now accepted by kernel

## Current Issues:

### 1. Initial 0-length I/O Operations
- After FETCH_REQ completes, we get descriptors with 0 sectors
- These are just initial completions, not real I/O
- Currently handled by returning success immediately

### 2. Large I/O Handling
- Kernel sometimes sends I/O requests larger than our 64KB buffer
- Need to either:
  - Increase buffer size to match max_sectors setting
  - Properly handle multi-buffer I/O operations
  - Or ensure kernel respects our max_sectors limit

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