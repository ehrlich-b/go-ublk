# TODO.md - Current Status

## Device Creation Works, But I/O Processing Broken

Current status (2025-09-24):
- ✅ ADD_DEV works (returns device ID)
- ✅ SET_PARAMS works (returns 0)
- ✅ START_DEV completes successfully
- ✅ Block device `/dev/ublkb0` is created
- ❌ **FETCH_REQ operations don't reach kernel** - `ublk_ch_uring_cmd` never called
- ❌ **I/O operations hang** - dd enters D state forever
- ❌ Queue runners receive empty FETCH completions (NrSectors=0)

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

### Root Cause (Confirmed by Testing):
- **FETCH_REQ operations are not reaching the kernel's ublk driver**
- Control operations work (`ublk_ctrl_uring_cmd` traced successfully)
- But I/O operations fail (`ublk_ch_uring_cmd` never called)
- This means our FETCH_REQ SQE format is likely incorrect
- See `einval_issue.md` for comprehensive analysis and code

## Next Steps to Fix:

### 1. Debug why FETCH_REQ doesn't reach kernel
- [ ] Compare our SQE format with ublksrv C implementation
- [ ] Check if we need to pass a header struct in sqe.Addr for FETCH_REQ
- [ ] Verify the IOCTL encoding (0xc0107520) is correct
- [ ] Test if we need to register the `/dev/ublkcN` fd with io_uring

### 2. Alternative approaches to test
- [ ] Try using the non-encoded command value (just 0x20)
- [ ] Test with sqe.Addr pointing to a header struct
- [ ] Try different sqe.OpFlags values
- [ ] Check if queue_id/tag should be in sqe.Addr instead of UserData

### 3. Kernel investigation needed
- [ ] Why can't we trace `ublk_ch_uring_cmd` with kprobes?
- [ ] What's the exact SQE format the kernel expects for FETCH_REQ?
- [ ] Is there a state machine we're not following correctly?

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