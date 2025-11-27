# go-ublk Internals

Project-specific reference for ublk and io_uring structures used in this codebase.

## Device Lifecycle

```
1. ADD_DEV       → Creates /dev/ublkcN (character device)
2. SET_PARAMS    → Configure size, block size, etc.
3. START_DEV     → Creates /dev/ublkbN (block device), starts I/O
4. [I/O loop runs]
5. STOP_DEV      → Stops I/O, removes /dev/ublkbN
6. DEL_DEV       → Removes /dev/ublkcN
```

## io_uring Setup

We use extended SQE/CQE sizes for ublk's URING_CMD operations:

```go
flags := IORING_SETUP_SQE128 | IORING_SETUP_CQE32
```

- `IORING_SETUP_SQE128` (1 << 10): 128-byte SQEs (standard 64 + 64 extra for cmd data)
- `IORING_SETUP_CQE32` (1 << 11): 32-byte CQEs (standard 16 + 16 extra)

### mmap Regions

Three memory regions are mapped from the ring fd:

| Offset | Content | Size |
|--------|---------|------|
| `0x00000000` | SQ ring (head, tail, array) | ~1 page |
| `0x08000000` | CQ ring (head, tail, cqes) | ~1 page |
| `0x10000000` | SQE array | entries × 128 bytes |

### SQE Layout (128 bytes total)

```
Bytes 0-63:   Standard io_uring_sqe fields
Bytes 64-79:  ublksrv_ctrl_cmd OR ublksrv_io_cmd (16 bytes used)
Bytes 80-127: Padding (48 bytes, zeroed)
```

For URING_CMD, the key fields are:
- `opcode` (byte 0): `IORING_OP_URING_CMD` = 46
- `fd` (bytes 4-7): `/dev/ublk-control` or `/dev/ublkcN` fd
- `cmd_op` (bytes 8-11): ioctl-encoded command
- `user_data` (bytes 24-31): tag identifier (returned in CQE)
- `cmd[80]` (bytes 48-127): command-specific data

## Kernel Structures

### UblksrvCtrlCmd (32 bytes)

Used in SQE cmd area for control operations:

```go
type UblksrvCtrlCmd struct {
    DevID      uint32  // 0xFFFFFFFF for new device
    QueueID    uint16  // 0xFFFF for control ops
    Len        uint16  // data length at Addr
    Addr       uint64  // userspace buffer address
    Data       uint64  // inline payload
    DevPathLen uint16  // unprivileged mode only
    Pad        uint16
    Reserved   uint32
}
```

### UblksrvCtrlDevInfo (64 bytes)

Returned by ADD_DEV, describes the created device:

```go
type UblksrvCtrlDevInfo struct {
    NrHwQueues    uint16  // number of queues
    QueueDepth    uint16  // depth per queue
    State         uint16  // UBLK_S_DEV_*
    Pad0          uint16
    MaxIOBufBytes uint32  // max I/O buffer
    DevID         uint32  // assigned device ID
    UblksrvPID    int32   // our PID
    Pad1          uint32
    Flags         uint64  // negotiated feature flags
    UblksrvFlags  uint64
    OwnerUID      uint32
    OwnerGID      uint32
    Reserved1     uint64
    Reserved2     uint64
}
```

### UblksrvIODesc (24 bytes)

Per-tag I/O descriptor, lives in shared memory (mmap'd from `/dev/ublkcN`):

```go
type UblksrvIODesc struct {
    OpFlags     uint32  // op in bits 0-7, flags in bits 8-31
    NrSectors   uint32  // sector count
    StartSector uint64  // starting sector
    Addr        uint64  // buffer address (writes only)
}
```

Memory offset: `UBLKSRV_IO_BUF_OFFSET` (0x80000000)

Indexing: `desc = base + (queueID * queueDepth + tag) * 24`

### UblksrvIOCmd (16 bytes)

Used in SQE cmd area for I/O operations:

```go
type UblksrvIOCmd struct {
    QID    uint16  // queue ID
    Tag    uint16  // request tag
    Result int32   // I/O result (COMMIT ops)
    Addr   uint64  // buffer address (FETCH ops)
}
```

## I/O Flow

### Initial Setup (per tag)

Submit `UBLK_IO_FETCH_REQ` for each tag to prime the queue:

```
SQE:
  opcode = IORING_OP_URING_CMD (46)
  fd = ublkc_fd
  cmd_op = ioctl_encode(UBLK_IO_FETCH_REQ)
  user_data = tag
  cmd[0:16] = UblksrvIOCmd{QID, Tag, 0, buffer_addr}
```

### Main Loop

1. **Wait for CQE** - kernel signals request available
2. **Read descriptor** - `atomic.LoadUint32` on OpFlags, then read full descriptor
3. **Process I/O** - read/write backend based on op
4. **Submit COMMIT_AND_FETCH** - complete request, fetch next

```
SQE:
  opcode = IORING_OP_URING_CMD (46)
  fd = ublkc_fd
  cmd_op = ioctl_encode(UBLK_IO_COMMIT_AND_FETCH_REQ)
  user_data = tag
  cmd[0:16] = UblksrvIOCmd{QID, Tag, result, buffer_addr}
```

Result encoding: 0 for success, negative errno for failure.

## Feature Flags

Requested in ADD_DEV, kernel returns negotiated set:

| Flag | Value | Purpose |
|------|-------|---------|
| `UBLK_F_URING_CMD_COMP_IN_TASK` | 1 << 1 | Force task_work completion |
| `UBLK_F_USER_COPY` | 1 << 7 | Use pread/pwrite for data |

We currently request: `UBLK_F_URING_CMD_COMP_IN_TASK`

## ioctl Encoding

Commands sent via `cmd_op` field are ioctl-encoded:

```go
func IoctlEncode(dir, typ, nr, size uint32) uint32 {
    return (dir << 30) | (size << 16) | (typ << 8) | nr
}

// Control commands: type='u', size=32
cmd_op = IoctlEncode(3, 'u', UBLK_CMD_*, 32)

// I/O commands: type='u', size=16
cmd_op = IoctlEncode(3, 'u', UBLK_IO_*, 16)
```

Direction bits: `_IOC_READ=2, _IOC_WRITE=1, both=3`

## Memory Barriers

Critical for shared memory correctness:

```go
// Before reading descriptor (after CQE received)
atomic.LoadUint32(&desc.OpFlags)  // acquire semantics

// Before updating SQ tail
Sfence()  // store fence
atomic.StoreUint32(sqTail, newTail)
```

## Key Files

| File | Purpose |
|------|---------|
| `internal/uapi/structs.go` | Kernel struct definitions with size checks |
| `internal/uapi/constants.go` | Command codes, flags, limits |
| `internal/uring/minimal.go` | io_uring ring setup and operations |
| `internal/queue/runner.go` | I/O loop state machine |
| `internal/ctrl/control.go` | Device lifecycle (ADD, START, STOP, DEL) |

## References

- Kernel source: `include/uapi/linux/ublk_cmd.h`
- Kernel docs: `docs.kernel.org/block/ublk.html`
