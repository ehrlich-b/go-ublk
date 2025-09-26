# UBLK Go Implementation - Status Update (2025-09-25)

## Executive Summary
**MAJOR PROGRESS**: Fixed -EINVAL issue by correcting SQE layout. Now facing empty descriptor issue.

## Test Environment
- **Kernel: 6.11** (Debian VM)
- **go-ublk commit**: Latest with SQE fixes
- **Test command**: `make vm-simple-e2e`

## Current Status

### ✅ FIXED Issues
1. **-EINVAL on FETCH_REQ**: SOLVED by fixing SQE128 layout
   - **Root cause**: `ublksrv_io_cmd` was placed at bytes 64-79 instead of 48-63
   - **Fix**: Corrected sqe128 struct - cmd area now starts at byte 48 (80 bytes total)

2. **Submission order**: SOLVED by submitting FETCH_REQs before START_DEV
   - Moved Prime() call to Start() method
   - Ensures all tags have FETCH_REQs submitted before START_DEV

### ❌ Current Issues
1. **Empty descriptors**: FETCH_REQ returns with NrSectors=0, OpFlags=0
2. **Process crashes**: Becomes zombie after processing empty descriptor
3. **Only partial tags complete**: Only seeing tag 8 complete out of 32
4. **-EBUSY on resubmit**: Can't resubmit FETCH_REQ for busy tag

```
Queue 0: Tag 0 FETCH completion, result=-22, state=0
Queue 0: Tag 0 unexpected result=-22
```

## Fixed Implementation

### CORRECTED SQE Structure (128 bytes)
```go
type sqe128 struct {
    // 0..31: base SQE
    opcode      uint8    // 0: set to 46 (IORING_OP_URING_CMD)
    flags       uint8    // 1
    ioprio      uint16   // 2-3
    fd          int32    // 4-7: fd from /dev/ublkcN
    union0      [8]byte  // 8-15: cmd_op goes here (ioctl-encoded)
    addr        uint64   // 16-23
    len         uint32   // 24-27: MUST be 16
    opcodeFlags uint32   // 28-31

    // 32..47: rest of base SQE
    userData    uint64   // 32-39
    bufIndex    uint16   // 40-41
    personality uint16   // 42-43
    spliceFdIn  int32    // 44-47

    // 48..127: cmd area for URING_CMD (80 bytes with SQE128)
    // THIS IS THE KEY FIX - cmd starts at byte 48, not 64!
    cmd [80]byte // 48-127
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

## Key Discoveries

### 1. SQE128 cmd area location (CRITICAL)
- **WRONG**: cmd at bytes 64-127 (our original struct)
- **CORRECT**: cmd at bytes 48-127 (80 bytes total)
- Kernel reads `ublksrv_io_cmd` from byte 48, not 64
- With SQE128 enabled, URING_CMD has 80-byte payload area

### 2. Proper ioctl encoding (REQUIRED)
- Must use `UBLK_U_IO_FETCH_REQ` (ioctl-encoded: 0xC0107520)
- Not legacy `UBLK_IO_FETCH_REQ` (raw value: 0x20)
- Formula: `_IOWR('u', 0x20, struct ublksrv_io_cmd)`

### 3. Thread affinity (MANDATORY)
- Each queue needs dedicated OS thread
- Must call `runtime.LockOSThread()` in queue worker
- Kernel binds first FETCH to thread as queue daemon
- Different thread for same queue causes -EINVAL

### 4. Current behavior
```
Queue 0: Tag 8 FETCH completion, result=0, state=0
Queue 0: Tag 8 I/O arrived (result=0=OK), processing...
[DEBUG] processIOAndCommit: tag=8, OpFlags=0x0, NrSectors=0, StartSector=0, Addr=0x0
Queue 0: Tag 8 FETCH completion, result=-16, state=0  // -EBUSY on resubmit
```

## Remaining Issues to Solve

### 1. Why empty descriptors?
- FETCH_REQ completes with result=0 (success)
- But descriptor has NrSectors=0, OpFlags=0
- Possible causes:
  - Descriptor mmap wrong offset?
  - Kernel hasn't written descriptor yet?
  - Need to wait for actual I/O arrival?

### 2. Handling empty descriptor completions
- Current approach causes -EBUSY when resubmitting
- Options:
  - Submit COMMIT_AND_FETCH with result=0?
  - Just wait for real I/O?
  - Poll descriptor until non-empty?

### 3. Why only partial tags complete?
- Only seeing tag 8 out of 32
- Are other FETCH_REQs stuck?
- Is the submission batching working?

### 4. Process crash (zombie)
- Process becomes defunct after handling empty descriptor
- Likely segfault or panic in Go code
- Need to add better error handling

## Request for Help

We need:
1. **The exact SQE128 layout for UBLK_IO_FETCH_REQ** - which bytes contain what
2. **What causes -EINVAL in ublk_ch_uring_cmd** for FETCH_REQ
3. **A minimal working example** of FETCH_REQ submission in C

The full source is at https://github.com/ehrlich-b/go-ublk

Thank you for your patience and expertise!