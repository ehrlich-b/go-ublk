# TODO.md - Current Status

## Progress: Fixed -EINVAL, Now Process Crashes

**PARTIAL FIX (2025-09-25):**
- ✅ **Fixed -EINVAL**: SQE layout corrected - cmd area now at bytes 48-127 (80 bytes)
- ✅ **FETCH_REQ returns 0**: No longer getting -EINVAL
- ❌ **Process crashes**: ublk-mem becomes zombie after first FETCH completion
- ❌ **Empty descriptors**: FETCH returns with NrSectors=0, OpFlags=0x0
- ❌ **I/O still hangs**: dd stuck in D state

**Current issue:**
- FETCH_REQ completes with result=0 but empty descriptor
- Process crashes immediately after (zombie state)
- Likely a segfault or panic in the Go code

## What Was Fixed:

### The START_DEV Hang Issue (SOLVED)
- **Root cause**: Kernel waits for FETCH_REQs before completing START_DEV
- **Solution**: Implemented async START_DEV with fire-and-forget submission
- **Result**: Device creation now works reliably

### The IOCTL Encoding Issue (SOLVED)
- **Root cause**: Modern kernels require IOCTL-encoded commands for ublk
- **Solution**: Added proper IOCTL encoding for all queue commands
- **Result**: FETCH_REQ commands now accepted by kernel

## Current Issue: Phantom Completions

### CRITICAL: FETCH_REQ Completes Without Waiting for I/O

**Key findings (2025-09-25):**

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

### The Paradox:
- **FETCH_REQ returns success (result=0) but with empty descriptors**
- This suggests FETCH is "accepted" but not "activated"
- Like a successful no-op - kernel says "OK" but doesn't wait for I/O
- Pattern: Every fix for I/O breaks START_DEV and vice versa
- See `einval_issue.md` for 100+ questions that need answering

## Next Steps:

### Test the fix:
1. Build and deploy to VM
2. Run simple e2e test
3. Verify FETCH_REQ no longer returns -EINVAL
4. Check if I/O operations work

### Key changes made:
1. ✅ Fixed sqe128 struct - cmd area now at bytes 48-127 (80 bytes)
2. ✅ Fixed SubmitIOCmd - writes ublksrv_io_cmd to bytes 48-63
3. ✅ Fixed SubmitCtrlCmd - writes control cmd to correct location
4. ✅ Already had thread locking (runtime.LockOSThread) per queue

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