# TODO.md - Current Status

## Device Creation Works, But I/O Processing Broken

Current status:
- ✅ ADD_DEV works (returns device ID)
- ✅ SET_PARAMS works (returns 0)
- ✅ START_DEV completes successfully (async implementation working)
- ✅ Block device `/dev/ublkb*` is created
- ❌ **I/O operations hang** - kernel not sending I/O requests to our queues
- ❌ Queue runners stuck in infinite loop re-submitting FETCH_REQs

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

### CRITICAL: I/O Processing Flow Broken

**Deep dive findings (2025-09-23):**

1. **Polling mechanism works!**
   - Successfully finding I/O via descriptor polling
   - Kernel DOES write descriptors when I/O arrives
   - We can detect new I/O (NrSectors=8 for 4KB writes)

2. **COMMIT_AND_FETCH_REQ issue**
   - We submit COMMIT_AND_FETCH but descriptor stays populated
   - This causes infinite loop processing same I/O
   - Fixed by tracking descriptor changes

3. **But I/O still hangs**
   - Even though we find and process I/O, dd still hangs
   - Possibly COMMIT_AND_FETCH_REQ not actually completing the I/O
   - Or we're not handling the flow correctly

### Root Cause (Hypothesis):
- The kernel might be expecting a different flow or setup
- Possible missing step in queue initialization
- May need to wait for kernel to be ready before submitting FETCH_REQs
- Or the descriptor mmap might not be set up correctly

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