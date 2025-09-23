# UBLK I/O Processing Issues - INVESTIGATION

## Current Status (2025-09-22)

**Device creation works, but I/O processing is broken**

### Latest Investigation:
- FETCH_REQs are being submitted successfully (32 of them for queue depth 32)
- The kernel is NOT sending any I/O completions back
- Queue runner is stuck in a polling loop waiting for completions
- When timeout is added to avoid blocking, it causes 100% CPU usage

## The 14MB Mystery

We're getting I/O requests for 14751744 bytes (14MB) when we set MaxIOSize to 64KB.

### Suspicious Numbers:
- 14751744 bytes = 28808 sectors (0x7088)
- 14751744 in hex = 0xE14000
- Our max_sectors setting = 128 (0x80)
- Actual sectors received = 28808 (225x larger!)

### Possible Causes:
1. **Endianness issue** - Are we reading NrSectors with wrong byte order?
2. **Struct alignment** - Is the descriptor struct layout different than expected?
3. **Memory corruption** - Is something overwriting the descriptor?
4. **Wrong descriptor offset** - Are we reading from the wrong memory location?
5. **Kernel bug** - Is the kernel ignoring our max_sectors?

## The Zero-Length I/O Issue

Initial FETCH_REQ completions have NrSectors=0. This might be normal:
- FETCH_REQ completes immediately when submitted (no I/O yet)
- Real I/O comes later as separate completion
- We might be handling the flow wrong

## What We Found:

### The Flow Issue - CONFIRMED
1. Initial FETCH_REQ completions have zeros (just ACKs)
2. We were treating them as real I/O - WRONG
3. Real I/O comes later as the SAME completion when kernel has work

### The Real Problem
- After FETCH_REQ is submitted, kernel acknowledges with completion
- Descriptor stays at zeros until real I/O arrives
- We need to KEEP POLLING for the descriptor to change
- Once it has data, process it and submit COMMIT_AND_FETCH_REQ

### Current Issue
- We're ignoring zero descriptors but not handling the flow correctly
- Queue runner gets stuck waiting for completions that don't come
- Need to keep the FETCH_REQs "live" and poll for descriptor changes

## Root Cause Analysis:

The kernel is not sending I/O requests to our queue. Possible reasons:

1. **FETCH_REQ submission issue**: The commands might not be properly formatted
2. **io_uring setup issue**: The ring might not be properly configured for URING_CMD
3. **Missing step**: We might be missing a step to enable I/O flow
4. **Wrong FD**: We might be using the wrong file descriptor for I/O commands

## Action Plan:

### 1. Verify FETCH_REQ Submission
- Check that all FETCH_REQs are actually being sent to kernel (not just queued)
- Verify the command encoding matches what kernel expects
- Check if we need to call io_uring_enter after batching

### 2. Check io_uring Configuration
- Ensure the ring is set up correctly for the character device
- Verify SQE128 is enabled if needed
- Check if we're missing any required flags

### 3. Debug Kernel Communication
- Add more logging to see what's happening with submissions
- Check dmesg for any kernel errors
- Compare with working C implementation

## Implementation:

```go
// TEMPORARY: Handle any size I/O by dynamic allocation
if length > maxBufferSize {
    fmt.Printf("LARGE I/O: %d bytes requested, dynamically allocating\n", length)
    tempBuf := make([]byte, length)
    // Do I/O with tempBuf
    // Copy back to our buffer if needed
}
```

## Previous Fix (IOCTL Encoding)

Change queue commands from raw opcodes to IOCTL-encoded versions:

```go
// WRONG (returns -95):
_, err := r.ring.SubmitIOCmd(uapi.UBLK_IO_FETCH_REQ, ioCmd, userData)  // 0x20

// CORRECT:
cmd := uapi.UblkIOCmd(uapi.UBLK_IO_FETCH_REQ)  // _IOWR('u', 0x20, struct)
_, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
```

## Why It Failed

Modern kernels (6.1+) enforce IOCTL encoding for ublk commands:
- The kernel's `ublk_check_cmd_op()` checks if `_IOC_TYPE(cmd_op) == 'u'`
- Raw opcodes like `0x20` don't have the IOCTL type bits set
- Without `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES=y`, raw opcodes return `-EOPNOTSUPP`

## What Actually Happens

The correct sequence is:

1. **Queue Setup**: Create io_uring for each queue
2. **Submit FETCH_REQs**: Submit `queue_depth` FETCH_REQ commands per queue (with IOCTL encoding)
3. **Kernel Accepts**: Kernel validates and accepts the FETCH_REQs (no error)
4. **START_DEV**: Submit START_DEV command
5. **Kernel Waits**: START_DEV blocks until all queues have accepted FETCH_REQs
6. **Completion**: Once all queues are "ready", START_DEV completes and `/dev/ublkb*` appears

There's no deadlock - the kernel just needs to see valid FETCH_REQs before START_DEV completes.

## Current Status

After fixing the opcode encoding:

✅ **WORKING:**
- ADD_DEV succeeds (creates `/dev/ublkc*`)
- SET_PARAMS succeeds
- FETCH_REQ commands are accepted (no more -95 errors)
- START_DEV completes successfully
- Block device `/dev/ublkb*` is created

❌ **STILL BROKEN:**
- I/O operations (read/write) fail after device creation
- This appears to be a separate data plane issue

## Test Results

```bash
# After the fix:
sudo ./ublk-mem --size=16M

# Output:
*** ASYNC: Submitting START_DEV asynchronously
*** QUEUE 0: Prime() succeeded after 1 attempts
*** SUCCESS: Device /dev/ublkb34 created!

# Verification:
ls -la /dev/ublkb*
brw-rw---- 1 root disk 259, 0 Sep 21 19:24 /dev/ublkb35
brw-rw---- 1 root disk 259, 1 Sep 21 19:48 /dev/ublkb36
# Block devices successfully created!
```

## Lessons Learned

1. **Always check encoding requirements**: Modern Linux subsystems often require IOCTL encoding
2. **-EOPNOTSUPP is a hint**: This error often means "wrong command format" not "unsupported feature"
3. **Read kernel source carefully**: The check for `_IOC_TYPE(cmd_op)` was the key clue
4. **Legacy support is optional**: Most distros don't enable `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES`

## Implementation Details

The IOCTL encoding helper:
```go
func UblkIOCmd(cmd uint32) uint32 {
    // _IOWR('u', cmd, sizeof(struct ublksrv_io_cmd))
    return IoctlEncode(_IOC_READ|_IOC_WRITE, 'u', cmd, 16)
}
```

This sets the proper type bits that the kernel checks for.

## Next Steps

Now that device creation works, we need to debug why I/O operations fail. This is likely related to:
- Incorrect I/O command handling in the data plane
- Buffer management issues
- Missing or incorrect response format

But the fundamental "START_DEV never completes" issue is **SOLVED**!

## References

- Kernel check that was failing: `ublk_check_cmd_op()` in `drivers/block/ublk_drv.c`
- UAPI header: `include/uapi/linux/ublk_cmd.h` defines IOCTL encodings
- Config option: `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES` (usually disabled)