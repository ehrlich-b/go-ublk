# go-ublk Architecture

## Overview

go-ublk implements Linux ublk (userspace block driver) in pure Go, providing a high-performance interface for creating block devices in userspace.

## System Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    User Applications                     │
│                  (filesystem, database)                  │
└────────────────────┬────────────────────────────────────┘
                     │ Block I/O
┌────────────────────▼────────────────────────────────────┐
│                    Block Device Layer                    │
│                     /dev/ublkb[0-N]                     │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│                  Linux Kernel ublk_drv                   │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │ blk-mq queue │  │ blk-mq queue │  │ blk-mq queue │ │
│  │      0       │  │      1       │  │      N       │ │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘ │
└─────────┼──────────────────┼──────────────────┼─────────┘
          │ io_uring         │ io_uring         │ io_uring
┌─────────▼──────────────────▼──────────────────▼─────────┐
│                     go-ublk Library                      │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │Queue Runner 0│  │Queue Runner 1│  │Queue Runner N│ │
│  │ (goroutine)  │  │ (goroutine)  │  │ (goroutine)  │ │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘ │
│         └──────────────────┼──────────────────┘         │
│                            ▼                             │
│                    Backend Interface                     │
└────────────────────────────┬─────────────────────────────┘
                             │
          ┌──────────────────┼──────────────────┐
          │                  │                  │
    ┌─────▼─────┐     ┌──────▼──────┐   ┌──────▼──────┐
    │  Memory   │     │    File     │   │   Custom    │
    │  Backend  │     │   Backend   │   │   Backend   │
    └───────────┘     └─────────────┘   └─────────────┘
```

## Component Details

### Control Plane (/internal/ctrl)

Manages device lifecycle through `/dev/ublk-control`:

```
┌─────────────┐
│ Application │
└──────┬──────┘
       │ CreateAndServe()
┌──────▼──────┐
│ Control API │
└──────┬──────┘
       │ io_uring URING_CMD
┌──────▼──────────┐
│ /dev/ublk-control│
└──────┬──────────┘
       │
┌──────▼──────┐
│ Kernel ublk │
└─────────────┘
```

**Operations Sequence:**
1. `ADD_DEV` - Allocate device, negotiate features
2. `SET_PARAMS` - Configure block parameters
3. `GET_QUEUE_AFFINITY` - Get CPU bindings
4. `START_DEV` - Activate block device
5. (Device serves I/O)
6. `STOP_DEV` - Quiesce I/O
7. `DEL_DEV` - Remove device

### Data Plane (/internal/queue)

Per-queue I/O handling with dedicated goroutines:

```
┌──────────────────────────────────┐
│        Queue Runner Loop         │
│         (per goroutine)          │
└──────────┬───────────────────────┘
           │
    ┌──────▼──────┐
    │ Setup Phase │
    ├─────────────┤
    │ 1. Open /dev/ublkc<ID>      │
    │ 2. mmap descriptor array    │
    │ 3. Create io_uring          │
    │ 4. Submit FETCH_REQ for     │
    │    each tag [0, depth)      │
    └──────┬──────┘
           │
    ┌──────▼──────┐
    │  Main Loop  │
    ├─────────────┤
    │ for each CQE:              │
    │ 1. Extract tag from CQE    │
    │ 2. Read descriptor[tag]    │
    │ 3. Decode operation        │
    │ 4. Call backend method     │
    │ 5. Submit COMMIT_AND_FETCH│
    └─────────────┘
```

**I/O Request Flow:**

```
Kernel                go-ublk              Backend
  │                      │                    │
  │   Block I/O request  │                    │
  ├─────────────────────►│                    │
  │                      │                    │
  │  Complete FETCH_REQ  │                    │
  │◄─────────────────────┤                    │
  │                      │                    │
  │                      │  ReadAt/WriteAt    │
  │                      ├───────────────────►│
  │                      │                    │
  │                      │     Data/Error     │
  │                      │◄───────────────────┤
  │                      │                    │
  │  COMMIT_AND_FETCH    │                    │
  ├─────────────────────►│                    │
  │                      │                    │
  │  Complete Block I/O  │                    │
  │◄─────────────────────┤                    │
```

### io_uring Integration (/internal/uring)

Abstraction layer over pure-Go io_uring implementation:

**Ring Setup:**
- `IORING_SETUP_SQE128` - Extended SQE for URING_CMD
- `IORING_SETUP_CQE32` - Extended CQE for passthrough
- Probe kernel features before enabling

**Command Encoding:**
- Control commands via `IORING_OP_URING_CMD`
- Pack command structures in SQE cmd area
- Handle both ioctl and native encoding

### Memory Management

**Descriptor Array:**
- mmap'd from `/dev/ublkc<ID>`
- Indexed by `(queue_id * queue_depth + tag)`
- Contains `ublksrv_io_desc` structures
- Shared between kernel and userspace

**Buffer Management:**
- Default: Pre-allocated buffers in descriptor
- Optional: `NEED_GET_DATA` for lazy allocation
- Zero-copy where possible

## Key Data Structures

### ublksrv_io_desc
```go
type UblksrvIoDesc struct {
    Op        uint32  // READ, WRITE, FLUSH, DISCARD
    Flags     uint32  // Operation flags
    Sector    uint64  // Start sector
    NrSectors uint32  // Number of sectors
    Addr      uint64  // Buffer address
    // ... result fields
}
```

### Backend Interface
```go
type Backend interface {
    ReadAt(p []byte, off int64) (int, error)
    WriteAt(p []byte, off int64) (int, error)
    Flush() error
    Trim(off, length int64) error
    Size() int64
    Close() error
}
```

## Performance Considerations

### CPU Affinity
- Bind queue runners to specific CPUs
- Match kernel's blk-mq queue mapping
- Reduce cross-CPU traffic

### Memory Allocation
- Pre-allocate buffers where possible
- Reuse buffers across operations
- Minimize GC pressure

### Batching
- Submit multiple SQEs before io_uring_enter
- Process multiple CQEs per wake
- Amortize syscall overhead

### Zero-Copy Paths
- Direct I/O for file backend
- mmap for read-only backends
- Avoid unnecessary copies

## Error Handling

### Graceful Degradation
- Feature not available → fall back
- Resource exhausted → queue/retry
- Backend error → return errno to kernel

### Cleanup Sequence
1. Cancel context → stop queue runners
2. Wait for in-flight I/O completion
3. STOP_DEV command
4. Unmap memory regions
5. Close file descriptors
6. DEL_DEV command

## Feature Negotiation

### Kernel Features
- `UBLK_F_PER_IO_DAEMON` - Per-queue tag ownership
- `UBLK_F_NEED_GET_DATA` - Two-phase write
- `UBLK_F_UNPRIVILEGED_DEV` - Non-root operation
- `UBLK_F_AUTO_BUF_REG` - Automatic buffer registration
- `UBLK_F_CMD_IOCTL_ENCODE` - ioctl command encoding

### Adaptive Behavior
- Probe kernel capabilities at startup
- Enable features when available
- Provide fallback paths

## Security Model

### Privilege Requirements
- `CAP_SYS_ADMIN` for ADD_DEV
- Optional unprivileged mode (kernel ≥ 6.2)
- Per-user device limits

### Resource Limits
- `RLIMIT_MEMLOCK` for locked memory
- Queue depth limits
- Number of queues limits

## Testing Strategy

### Unit Tests
- Backend implementations
- Control command encoding
- Descriptor array operations

### Integration Tests
- Full device lifecycle
- I/O operations
- Error conditions
- Resource cleanup

### Performance Tests
- Throughput benchmarks
- Latency measurements
- CPU utilization
- Memory usage

## Future Enhancements

### Planned Features
- User recovery support
- Zoned block devices
- Multi-queue optimization
- Advanced caching

### Extension Points
- Backend plugin system
- Custom I/O schedulers
- Monitoring/metrics hooks
- Tracing integration