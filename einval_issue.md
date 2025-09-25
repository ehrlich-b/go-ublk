# UBLK Go Implementation - FETCH_REQ Returns -EINVAL

## Executive Summary
We have a Go implementation of ublk that successfully creates devices but FETCH_REQ commands immediately return -EINVAL. Device creation works perfectly, but I/O processing fails at the first step.

## Test Environment
- **Kernel: 6.11** (Debian VM)
- **go-ublk commit**: 3049986
- **Test command**: `make vm-simple-e2e`

## What Works
- ✅ Device creation (ADD_DEV → SET_PARAMS → START_DEV)
- ✅ Block device appears at `/dev/ublkb2`
- ✅ Character device opens successfully at `/dev/ublkc2`
- ✅ io_uring setup with SQE128/CQE32
- ✅ mmap of descriptor regions
- ✅ Control commands through `/dev/ublk-control`

## The Problem

FETCH_REQ commands return -22 (EINVAL) immediately. The sequence is:
1. Submit FETCH_REQs (before START_DEV)
2. Submit START_DEV
3. START_DEV hangs waiting for completion
4. FETCH_REQs complete with result=-22

```
Queue 0: Tag 0 FETCH completion, result=-22, state=0
Queue 0: Tag 0 unexpected result=-22
```

## Our Implementation

### SQE Structure (128 bytes)
```go
type sqe128 struct {
    // 0..31: base SQE
    opcode      uint8    // 0: set to 46 (IORING_OP_URING_CMD)
    flags       uint8    // 1
    ioprio      uint16   // 2-3
    fd          int32    // 4-7: fd from /dev/ublkcN
    union0      [8]byte  // 8-15: cmd_op goes here (0xC0107520 for FETCH_REQ)
    addr        uint64   // 16-23
    len         uint32   // 24-27: set to 16
    opcodeFlags uint32   // 28-31

    // 32..63: extended fields
    userData    uint64   // 32-39
    bufIndex    uint16   // 40-41
    personality uint16   // 42-43
    spliceFdIn  int32    // 44-47
    fileIndex   uint32   // 48-51
    _pad64      [12]byte // 52-63

    // 64..127: cmd area for SQE128
    cmd [64]byte // 64-127
}
```

### How We Submit FETCH_REQ
```go
sqe := &sqe128{}
sqe.opcode = 46  // IORING_OP_URING_CMD
sqe.fd = int32(charDeviceFd)  // fd from /dev/ublkcN
sqe.setCmdOp(0xC0107520)  // UBLK_U_IO_FETCH_REQ
sqe.len = 16
sqe.userData = (opFetch << 48) | (uint64(queueID) << 16) | uint64(tag)

// ublksrv_io_cmd (16 bytes)
ioCmd := &UblksrvIOCmd{
    QID:    queueID,  // 2 bytes
    Tag:    tag,      // 2 bytes
    Result: 0,        // 4 bytes
    Addr:   bufferAddr, // 8 bytes - points to 64KB I/O buffer
}
```

## What We've Tried

### 1. Placement of ublksrv_io_cmd (all result in -EINVAL)
- **Bytes 16-31**: Overwrites addr/len fields
- **Bytes 48-63**: In the extended area
- **Bytes 64-79**: In the cmd field for SQE128

### 2. Fixed Issues
- ✅ Set `sqe.len = 16` (was 0)
- ✅ Fixed mmap offset to `queueID * descSize`
- ✅ Submit FETCH_REQs BEFORE START_DEV
- ✅ Wait for START_DEV completion

### 3. Actual SQE Bytes Submitted
```
Example FETCH_REQ SQE (tag 0, queue 0):
Bytes 0-31:  2e00000005000000207510c0000000000000e2fd967a00000010000000000000
Bytes 32-63: 0000000000010000000000000000000000000000010000000000e2fd967a0000
Bytes 64-95: 0000000000000000000000000000000000000000000000000000000000000000
```

## Critical Questions

### 1. Where exactly does ublksrv_io_cmd go in SQE128?
For IORING_OP_URING_CMD with SQE128:
- The standard says cmd area is bytes 64-127
- But we get -EINVAL when placing the 16-byte struct there
- Control commands work when placed at bytes 48-63
- **What's the correct location for I/O commands?**

### 2. What validates the ublksrv_io_cmd structure?
The kernel returns -EINVAL for FETCH_REQ. What is it checking?
- Queue ID bounds? (we use qid=0)
- Tag bounds? (we use tag=0..31 for depth=32)
- Buffer address alignment? (we use mmap'd addresses)
- Something else?

### 3. Is our ioctl encoding correct?
- We use `0xC0107520` for UBLK_U_IO_FETCH_REQ
- Calculated as: `_IOC(_IOC_READ|_IOC_WRITE, 'u', 0x20, 16)`
- Is this the correct encoding for kernel 6.11?

### 4. Any prerequisites before FETCH_REQ?
- Do we need to register buffers with io_uring first?
- Is there a specific state the device needs to be in?
- Any initialization we're missing between opening /dev/ublkcN and submitting FETCH_REQ?

## Request for Help

We need:
1. **The exact SQE128 layout for UBLK_IO_FETCH_REQ** - which bytes contain what
2. **What causes -EINVAL in ublk_ch_uring_cmd** for FETCH_REQ
3. **A minimal working example** of FETCH_REQ submission in C

The full source is at https://github.com/ehrlich-b/go-ublk

Thank you for your patience and expertise!