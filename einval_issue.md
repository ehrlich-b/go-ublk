# Linux ublk FETCH_REQ -EINVAL Issue - Kernel Investigation Needed

## Summary
Pure Go ublk implementation gets `-EINVAL` for FETCH_REQ operations on Linux 6.11. Need kernel source analysis to determine correct command format.

## Environment
- **Kernel**: Linux 6.11.0-24-generic (Ubuntu 24.04)
- **Language**: Pure Go (no cgo)
- **Interface**: io_uring URING_CMD

## Current Status

### What Works ✅
- Device creation: `/dev/ublkb0` created successfully
- Control operations: ADD_DEV, SET_PARAMS, START_DEV all return 0
- Queue initialization: mmap succeeds, io_uring setup works

### What Fails ❌
- **All FETCH_REQ operations return -EINVAL (-22)**
- I/O hangs because kernel can't dispatch to userspace

## The Core Problem

We submit FETCH_REQ via io_uring URING_CMD but get -EINVAL. There's conflicting information about where to place the `ublksrv_io_cmd` struct:

### Approach 1: Via sqe->addr (Our Current Code - FAILS)
```go
func (r *minimalRing) SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) (Result, error) {
    sqe := &sqe128{}
    sqe.opcode = kernelUringCmdOpcode()  // 46 (IORING_OP_URING_CMD)
    sqe.fd = int32(r.controlFd)          // /dev/ublkcN fd
    sqe.setCmdOp(cmd)                    // 0xc0107520 for FETCH_REQ

    // Pass struct via sqe.addr
    sqe.addr = uint64(uintptr(unsafe.Pointer(ioCmd)))
    sqe.len = uint32(unsafe.Sizeof(*ioCmd))  // 16 bytes

    // Submit...
}
```

### Approach 2: Via cmd area at sqe->addr3 (C ublksrv does this)
```c
// From ublksrv C implementation
static inline void *ublksrv_get_sqe_cmd(struct io_uring_sqe *sqe) {
    return (void *)&sqe->addr3;  // Points to bytes 48-63 of SQE
}

// Usage:
cmd = (struct ublksrv_io_cmd *)ublksrv_get_sqe_cmd(sqe);
cmd->q_id = q->q_id;
cmd->tag = tag;
cmd->addr = (__u64)io->buf_addr;
```

## Test Output Showing -EINVAL

```
time=2025-09-24T10:41:58.383-04:00 msg="*** CRITICAL: SubmitIOCmd called"
    cmd=0xc0107520 controlFd=5 qid=0 tag=0 ioCmd.Addr=0x763a5261e000

Queue 0: Tag 0 FETCH completion, result=-22, state=0
Queue 0: Tag 0 unexpected result=-22
```

All 32 tags return -EINVAL immediately upon submission.

## The ublksrv_io_cmd Structure

```go
// 16 bytes total
type UblksrvIOCmd struct {
    QID    uint16  // Queue ID
    Tag    uint16  // Request tag
    Result int32   // For COMMIT operations
    Addr   uint64  // Points to mmapped descriptor for this tag
}
```

For FETCH_REQ, we set:
- QID: 0 (queue 0)
- Tag: 0-31
- Result: 0
- Addr: Points to mmapped descriptor (`0x763a5261e000` + tag*32)

## What We Need From Kernel Investigation

**Please examine Linux kernel 6.11 source: `drivers/block/ublk_drv.c`**

### Specific Questions:

1. **In `ublk_ch_uring_cmd()` for UBLK_IO_FETCH_REQ:**
   - Does it read the struct from `io_uring_sqe_cmd()` (cmd area)?
   - Or from `u64_to_user_ptr(sqe->addr)`?

2. **What validation causes -EINVAL?**
   - Is it the struct location?
   - Is it the struct contents?
   - Is it missing some required field?

3. **Command encoding:**
   - We use `0xc0107520` (_IOWR('u', 0x20, 16))
   - Is the size parameter (16) correct?

## Key Code Paths to Check

```c
// In kernel source, need to find:
static int ublk_ch_uring_cmd(struct io_uring_cmd *cmd, unsigned int issue_flags)
{
    // How does it get ublksrv_io_cmd?
    // Option A: From cmd area
    const struct ublksrv_io_cmd *ub_cmd = io_uring_sqe_cmd(cmd->sqe);

    // Option B: From sqe->addr
    const struct ublksrv_io_cmd __user *ub_cmd = u64_to_user_ptr(sqe->addr);

    // What causes -EINVAL for FETCH_REQ?
}
```

## Additional Context

- Control operations (via `/dev/ublk-control`) work perfectly using similar URING_CMD submission
- The C ublksrv implementation works on this kernel
- Our Go implementation mimics C but clearly has wrong format for FETCH_REQ
- The -EINVAL happens immediately, suggesting early validation failure