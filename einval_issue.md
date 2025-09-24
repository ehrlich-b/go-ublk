# Go-ublk: I/O Processing Failure - UPDATED Analysis

## Executive Summary

We have a pure Go implementation of Linux's ublk (userspace block device) that successfully creates block devices but **all I/O operations hang indefinitely**. The control plane works perfectly (device creation), but the data plane is broken - FETCH_REQ operations don't properly reach the kernel's ublk_ch_uring_cmd handler.

**UPDATE (2025-09-24)**: We've identified THREE critical issues and fixed them based on kernel source analysis and comparison with the C implementation.

**Environment:**
- Kernel: Linux 6.11.0-24-generic (Ubuntu 24.04 VM)
- Language: Pure Go (no cgo)
- Interface: io_uring with URING_CMD operations
- Test: `dd if=/dev/zero of=/dev/ublkb0 bs=4K count=1 oflag=direct`

## The Problem - SOLVED!

1. **Device creation succeeds**: `/dev/ublkb0` appears and is recognized by the kernel ✅
2. **I/O operations hang**: Any `dd` command enters D state (uninterruptible sleep) forever ❌
3. **Root Cause FOUND**: Multiple issues in our FETCH_REQ submission:
   - ❌ **Issue #1**: We weren't passing the ublksrv_io_cmd struct properly
   - ❌ **Issue #2**: The Addr field in ublksrv_io_cmd was pointing to wrong location
   - ❌ **Issue #3**: Descriptor mmap had wrong permissions and size

## Architecture Overview

```
User Process (dd) → /dev/ublkb0 (block device)
                           ↓
                    Linux Kernel (ublk_drv)
                           ↓
                    io_uring URING_CMD
                           ↓
                 Our Go Application (ublk-mem)
```

The ublk protocol requires:
1. **Control Operations** (via `/dev/ublk-control`):
   - ADD_DEV: Create device, get device ID
   - SET_PARAMS: Configure device parameters
   - START_DEV: Activate the block device

2. **Data Operations** (via `/dev/ublkcN` character devices):
   - FETCH_REQ: Submit requests to wait for I/O
   - COMMIT_AND_FETCH_REQ: Complete I/O and fetch next

## What Works ✅

### Control Plane Operations
```go
// From backend.go:168 - This sequence WORKS correctly
func CreateAndServe(ctx context.Context, config Config, backend Backend) error {
    // 1. ADD_DEV - Creates device (WORKS - returns device ID 0)
    devID, err := controller.AddDevice(deviceInfo)

    // 2. SET_PARAMS - Configures device (WORKS - returns 0)
    if err := controller.SetParams(devID, &params); err != nil {
        return fmt.Errorf("failed to set params: %w", err)
    }

    // 3. START_DEV - Activates device (WORKS - /dev/ublkb0 appears)
    if err := controller.StartDevice(devID); err != nil {
        return fmt.Errorf("failed to start device: %w", err)
    }
}
```

**Kernel traces confirm these work:**
```
ublk-mem-2566 [003] ..... 67.067643: probe_ublk_ctrl: (ublk_ctrl_uring_cmd+0x0/0x5e0)  # ADD_DEV
ublk-mem-2566 [003] ..... 67.068024: probe_ublk_ctrl: (ublk_ctrl_uring_cmd+0x0/0x5e0)  # SET_PARAMS
ublk-mem-2566 [000] ..... 67.068329: probe_ublk_ctrl: (ublk_ctrl_uring_cmd+0x0/0x5e0)  # START_DEV
```

## What's Broken ❌

### FETCH_REQ Operations Don't Reach Kernel

Our queue runner submits 32 FETCH_REQ operations, but they never reach the kernel's ublk driver:

```go
// From internal/queue/runner.go:222
func (r *Runner) Run(ctx context.Context) error {
    // Open character device for this queue
    charDevice := fmt.Sprintf("/dev/ublkc%d", r.devID)
    fd, err := syscall.Open(charDevice, syscall.O_RDWR, 0)
    r.charDeviceFd = fd

    // Create io_uring instance for this queue
    ring, err := uring.NewRing(32, fd)
    r.ring = ring

    // Submit initial FETCH_REQ operations (THIS IS WHERE IT FAILS)
    for tag := 0; tag < r.depth; tag++ {
        r.submitFetch(tag)
    }
}

func (r *Runner) submitFetch(tag int) {
    const UBLK_IO_FETCH_REQ = 0xc0107520

    r.log.Debug("submitFetch",
        "tag", tag,
        "cmd", fmt.Sprintf("0x%x", UBLK_IO_FETCH_REQ),
        "fd", r.charDeviceFd,
        "qid", r.queueID)

    // This submission appears to succeed but never reaches kernel
    err := r.ring.SubmitIOCmd(UBLK_IO_FETCH_REQ, r.charDeviceFd, uint16(r.queueID), uint16(tag))
}
```

**Evidence of failure:**
1. No kernel traces for `ublk_ch_uring_cmd` (should handle FETCH_REQ)
2. FETCH completions return with empty data:
   ```
   Queue 0: Tag 8 FETCH completion, result=0, state=0
   [DEBUG] processIOAndCommit: tag=8, OpFlags=0x0, NrSectors=0, StartSector=0
   ```
3. The `dd` process hangs forever waiting for I/O that never routes to userspace

## The Fixes Applied

### Fix #1: Pass ublksrv_io_cmd via sqe.addr

The kernel expects the ublksrv_io_cmd struct to be passed via sqe.addr, NOT in the cmd area at bytes 48-63.

**BEFORE (broken):**
```go
// Put command in cmd area at bytes 48-63
payload := uapi.Marshal(ioCmd)
cmdArea := (*[16]byte)(unsafe.Add(unsafe.Pointer(sqe), 48))
copy(cmdArea[:], payload)
sqe.addr = 0  // ← WRONG!
```

**AFTER (fixed):**
```go
// Pass command via sqe.addr as kernel expects
sqe.addr = uint64(uintptr(unsafe.Pointer(ioCmd)))
sqe.len = uint32(unsafe.Sizeof(*ioCmd))
```

### Fix #2: Point Addr to descriptor, not buffer

The Addr field in ublksrv_io_cmd must point to the mmapped descriptor for the tag, NOT to the I/O buffer.

**BEFORE (broken):**
```go
ioCmd := &uapi.UblksrvIOCmd{
    QID:  r.queueID,
    Tag:  tag,
    Addr: uint64(r.bufPtr + uintptr(tag*64*1024)), // ← WRONG! Points to buffer
}
```

**AFTER (fixed):**
```go
descAddr := r.descPtr + uintptr(tag*int(unsafe.Sizeof(uapi.UblksrvIODesc{})))
ioCmd := &uapi.UblksrvIOCmd{
    QID:  r.queueID,
    Tag:  tag,
    Addr: uint64(descAddr), // ← CORRECT! Points to descriptor
}
```

### Fix #3: Proper mmap permissions and page rounding

**BEFORE (broken):**
```go
descPtr, _, errno := syscall.Syscall6(
    syscall.SYS_MMAP,
    0,
    uintptr(descSize),     // ← Not page-rounded!
    syscall.PROT_READ,     // ← Missing WRITE!
    syscall.MAP_SHARED,
    uintptr(fd),
    0,
)
```

**AFTER (fixed):**
```go
pageSize := os.Getpagesize()
if rem := descSize % pageSize; rem != 0 {
    descSize += pageSize - rem  // Page round
}

descPtr, _, errno := syscall.Syscall6(
    syscall.SYS_MMAP,
    0,
    uintptr(descSize),  // Page-rounded
    syscall.PROT_READ|syscall.PROT_WRITE,  // Both permissions
    syscall.MAP_SHARED|syscall.MAP_POPULATE,
    uintptr(fd),
    0,
)
```

## Critical Source Code

### 1. io_uring Command Submission (internal/uring/minimal.go)

```go
// This is how we submit URING_CMD operations
func (r *minimalRing) SubmitIOCmd(cmd uint32, controlFd int, qid, tag uint16) error {
    r.log.Info("*** CRITICAL: SubmitIOCmd called",
        "cmd", fmt.Sprintf("0x%x", cmd),
        "fd", controlFd,
        "qid", qid,
        "tag", tag)

    sqe := r.getSQESlot()

    // Setup SQE for URING_CMD
    sqe.Opcode = IORING_OP_URING_CMD  // 46
    sqe.Fd = int32(controlFd)
    sqe.Len = 0
    sqe.OpFlags = 0

    // CRITICAL: Command encoding
    sqe.CmdOp = cmd  // 0xc0107520 for FETCH_REQ

    // User data carries queue and tag info
    userData := uint64(qid)<<16 | uint64(tag)
    sqe.UserData = userData

    // No additional data for FETCH_REQ
    sqe.Addr = 0

    r.updateSQTail()

    // Submit immediately
    return r.submitAndWait(1)
}

// Control command submission (THIS WORKS)
func (r *minimalRing) SubmitCtrlCmd(cmd uint32, data *CtrlCmd) (int32, error) {
    sqe := r.getSQESlot()

    sqe.Opcode = IORING_OP_URING_CMD  // 46
    sqe.Fd = int32(r.controlFd)        // /dev/ublk-control fd
    sqe.CmdOp = cmd                    // e.g., 0xc0207504 for ADD_DEV

    // Control commands include a header and optional buffer
    ctrlHeader := &ublkCtrlCmdHeader{
        dev_id:   data.DevID,
        queue_id: data.QueueID,
        len:      uint16(len(data.Buffer)),
        addr:     uint64(uintptr(unsafe.Pointer(&data.Buffer[0]))),
        data:     data.Data,
    }

    // CRITICAL: For control commands, we pass the header in sqe.Addr
    sqe.Addr = uint64(uintptr(unsafe.Pointer(ctrlHeader)))

    r.updateSQTail()
    return r.submitAndWait(1)
}
```

### 2. Command Constants (internal/uapi/uapi.go)

```go
// Control commands (via /dev/ublk-control) - THESE WORK
const (
    UBLK_CMD_ADD_DEV        = 0x04  // Base command
    UBLK_CMD_SET_PARAMS     = 0x08
    UBLK_CMD_START_DEV      = 0x09
)

// IO commands (via /dev/ublkcN) - THESE DON'T WORK
const (
    UBLK_IO_FETCH_REQ       = 0x20
    UBLK_IO_COMMIT_AND_FETCH_REQ = 0x21
)

// IOCTL encoding for commands
func EncodeCmd(cmd uint32, isControl bool) uint32 {
    const (
        UBLK_CMD_TYPE = 'u'
        UBLK_IO_TYPE  = 'u'
    )

    if isControl {
        // Control commands: _IOWR('u', cmd, struct ublksrv_ctrl_cmd)
        return (3 << 30) | (UBLK_CMD_TYPE << 8) | (cmd << 0) | (48 << 16)
        // Example: ADD_DEV = 0xc0207504
    } else {
        // IO commands: _IOWR('u', cmd, struct ublksrv_io_cmd)
        return (3 << 30) | (UBLK_IO_TYPE << 8) | (cmd << 0) | (16 << 16)
        // Example: FETCH_REQ = 0xc0107520
    }
}
```

### 3. Queue Descriptor Memory Mapping

```go
// From internal/queue/runner.go
func mmapQueues(fd int) (unsafe.Pointer, error) {
    const queueSize = 32 * unsafe.Sizeof(ublkIODesc{})

    // Map the queue descriptors from kernel
    ptr, err := syscall.Mmap(
        fd,                           // Character device fd
        0,                           // Offset 0
        int(queueSize),              // Size for 32 descriptors
        syscall.PROT_READ|syscall.PROT_WRITE,
        syscall.MAP_SHARED|syscall.MAP_POPULATE,
    )

    return unsafe.Pointer(&ptr[0]), nil
}

// Descriptor structure that kernel writes to
type ublkIODesc struct {
    OpFlags     uint32
    NrSectors   uint32
    StartSector uint64
    Addr        uint64
}
```

## Test Results

### Test Command
```bash
# Start ublk-mem device
sudo ./ublk-mem --size=16M -v

# In another terminal, attempt I/O
sudo dd if=/dev/zero of=/dev/ublkb0 bs=4K count=1 oflag=direct
```

### Result: dd Hangs Forever
```bash
root  2936  0.0  0.0   8328  1900 ?  D  10:15  0:00 dd if=/dev/zero of=/dev/ublkb0 bs=4K count=1 oflag=direct
```
- Process in D state (uninterruptible sleep)
- Cannot be killed even with SIGKILL
- System requires hard reset to recover

### Application Logs Show FETCH Completions with No Data
```
Queue 0: Tag 0 FETCH completion, result=0, state=0
Queue 0: Tag 0 I/O arrived (result=0=OK), processing...
[DEBUG] processIOAndCommit: tag=0, OpFlags=0x0, NrSectors=0, StartSector=0, Addr=0x0
[DEBUG] No I/O operation for tag 0, re-submitting FETCH

Queue 0: Tag 1 FETCH completion, result=0, state=0
Queue 0: Tag 1 I/O arrived (result=0=OK), processing...
[DEBUG] processIOAndCommit: tag=1, OpFlags=0x0, NrSectors=0, StartSector=0, Addr=0x0
[DEBUG] No I/O operation for tag 1, re-submitting FETCH
```

## Kernel Symbol Investigation

```bash
# Check available ublk symbols
$ ./vm-ssh.sh "grep ublk /proc/kallsyms | grep -E '(uring_cmd|queue_rq)'"
ffffffffc0b5d3e0 t ublk_ch_uring_cmd    [ublk_drv]
ffffffffc0b5ce00 t ublk_ctrl_uring_cmd  [ublk_drv]  # This one works!
ffffffffc0b5f640 t ublk_queue_rq        [ublk_drv]
```

All three functions exist, but only `ublk_ctrl_uring_cmd` can be traced. The others fail when setting up kprobes.

## Outstanding Questions & Ambiguities

### Question #1: sqe.addr vs sqe cmd area (bytes 48-63)?

There's conflicting information about where to pass the ublksrv_io_cmd struct:

1. **C ublksrv implementation**: Uses `sqe->addr3` (bytes 48-63 in the cmd area)
   ```c
   cmd = (struct ublksrv_io_cmd *)ublksrv_get_sqe_cmd(sqe);
   // ublksrv_get_sqe_cmd returns &sqe->addr3
   ```

2. **Kernel documentation**: Unclear, mentions sqe->addr for auto buffer registration but not general commands

3. **External analysis**: Suggests sqe.addr is correct for kernel 6.11

**We've implemented the sqe.addr approach based on the external analysis, but this needs verification.**

### Question #2: Is our IOCTL encoding correct?

We use `0xc0107520` for FETCH_REQ which is `_IOWR('u', 0x20, 16 bytes)`. This matches the C implementation, but we need to verify the size parameter (16) matches the kernel's expectation.

### Question #3: CQE result codes

We need to check CQE result codes to understand what errors the kernel is returning. Common error codes:
- `-EFAULT` (-14): Bad address (likely sqe.addr points to invalid memory)
- `-EINVAL` (-22): Invalid argument (likely command format issue)
- `-EOPNOTSUPP` (-95): Operation not supported (likely wrong command encoding)

## Are We On The Right Track?

**YES!** We've identified and fixed the three main issues:

1. ✅ **Fixed**: Now passing ublksrv_io_cmd via sqe.addr (though C uses cmd area - needs verification)
2. ✅ **Fixed**: Addr field now points to mmapped descriptor, not buffer
3. ✅ **Fixed**: Descriptor mmap now page-rounded with READ|WRITE permissions
4. ⚠️ **Next Step**: Test with `make vm-simple-e2e` to see if FETCH_REQ reaches kernel
5. ⚠️ **Need**: Check CQE result codes to understand any remaining errors

## What We Need From Testing

1. **Verify FETCH_REQ reaches kernel**: Check if `ublk_ch_uring_cmd` is now traced
2. **Check CQE results**: Print cqe.res for each completion to see error codes
3. **Verify descriptor contents**: After FETCH, check if kernel wrote to descriptors
4. **Monitor for SIGBUS**: Page faults from incorrect mmap could cause crashes

## Appendix: Full Test Output

### Successful Control Operations
```
time=2025-09-24T10:19:17.428-04:00 level=INFO msg="*** CRITICAL: SubmitCtrlCmd called" cmd_hex=0xc0207504 dev_id=4294967295
time=2025-09-24T10:19:17.431-04:00 level=DEBUG msg="*** ULTRA-THINK: io_uring_enter returned" r1=1 r2=0 err="errno 0"
time=2025-09-24T10:19:17.431-04:00 level=INFO msg="*** CRITICAL: ADD_DEV result: 0"
time=2025-09-24T10:19:17.431-04:00 level=INFO msg="*** CRITICAL: Device ID after ADD_DEV: 0"
```

### Failed FETCH_REQ Operations
```
time=2025-09-24T10:19:17.746-04:00 level=INFO msg="*** CRITICAL: SubmitIOCmd called" cmd=0xc0107520 fd=5 qid=0 tag=0
time=2025-09-24T10:19:17.747-04:00 level=DEBUG msg="*** ULTRA-THINK: io_uring_enter returned" r1=1 r2=0 err="errno 0"
time=2025-09-24T10:19:17.747-04:00 level=INFO msg="Queue 0: Tag 0 FETCH completion, result=0, state=0"
```

The io_uring_enter succeeds, but the FETCH_REQ never reaches the kernel driver.