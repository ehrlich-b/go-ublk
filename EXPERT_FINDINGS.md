# EXPERT FINDINGS - UBLK Implementation Issues

## Three Critical Bugs Identified and Fixed

### BUG #1: Missing SQE Length Field ✅ FIXED
**Location**: `internal/uring/minimal.go:613`
- **Was**: `sqe.len = 0`
- **Fixed**: `sqe.len = 16`
- **Why it matters**: Without len=16, kernel doesn't know there's a 16-byte `ublksrv_io_cmd` in the cmd area

### BUG #2: Wrong mmap Offset ✅ FIXED
**Location**: `internal/queue/runner.go:737`
- **Was**: `offset = 0` for all queues
- **Fixed**: `offset = queueID * round_up(queue_depth * sizeof(desc), PAGE_SIZE)`
- **Why it matters**: All queues were looking at queue 0's descriptors

### BUG #3: Wrong FETCH_REQ Timing ✅ FIXED
**Location**: `backend.go:280`
- **Was**: Submit FETCH_REQs AFTER waiting for device LIVE
- **Fixed**: Submit FETCH_REQs BEFORE START_DEV
- **Why it matters**: Kernel expects FETCHes armed before device starts

## Key Insights from Expert Analysis

### FETCH_REQ Semantics
- Should **block** until I/O arrives (not complete immediately)
- Posted once per (qid,tag) initially
- Completes only when kernel has I/O to deliver
- If getting immediate completion with empty descriptors = not reaching ublk handler

### Correct Initialization Sequence
1. **Prepare**: Create io_uring, open `/dev/ublkcN`, mmap descriptors
2. **Arm**: Submit FETCH_REQ for every tag
3. **Expose**: Issue START_DEV
4. **Serve**: On completion, read descriptor, process I/O, COMMIT_AND_FETCH

### SQE Requirements for URING_CMD
- `sqe->opcode = 46` (IORING_OP_URING_CMD)
- `sqe->fd = fd` of `/dev/ublkcN` (char device, NOT block device)
- `sqe->len = 16` (MUST be set!)
- 16-byte `ublksrv_io_cmd` goes in cmd area (bytes 48-63)
- `cmd_op = 0xC0107520` (IOCTL-encoded FETCH_REQ)

## Still Not Working After Fixes

Despite fixing all three identified bugs, I/O still hangs. Possible remaining issues:

1. **SQE cmd area placement**: We're using bytes 48-63, which should be correct for SQE128
2. **userData encoding**: Using `opFetch | (qid<<16) | tag`
3. **Descriptor reading**: After FETCH completes, how we read the descriptor
4. **Some other subtle issue** we haven't identified yet

## What Would Help

1. **Kernel debug output** showing what happens to our FETCH_REQ
2. **strace comparison** between working C and our Go implementation
3. **Confirmation** that bytes 48-63 is correct cmd area for SQE128
4. **Verification** of our userData encoding format

## References
- Expert analysis provided comprehensive understanding of ublk semantics
- Linux kernel docs specify control/data plane separation
- UAPI values confirmed: FETCH=0x20, IOCTL-encoded=0xC0107520
- Per-queue mmap with vm_pgoff selection critical for multi-queue