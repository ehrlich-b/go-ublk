# io_uring for ublk

## Overview

ublk uses io_uring as its primary communication mechanism between kernel and userspace. This document covers io_uring concepts and usage specific to ublk.

## io_uring Basics

### Ring Structure
```
┌─────────────────────────────────────┐
│          Submission Queue (SQ)       │
│  ┌───┬───┬───┬───┬───┬───┬───┬───┐ │
│  │   │   │   │   │   │   │   │   │ │ ← Producer (userspace)
│  └───┴───┴───┴───┴───┴───┴───┴───┘ │
└─────────────────────────────────────┘
                    ↓
              Kernel Processing
                    ↓
┌─────────────────────────────────────┐
│         Completion Queue (CQ)        │
│  ┌───┬───┬───┬───┬───┬───┬───┬───┐ │
│  │   │   │   │   │   │   │   │   │ │ ← Consumer (userspace)
│  └───┴───┴───┴───┴───┴───┴───┴───┘ │
└─────────────────────────────────────┘
```

### Key Components
- **SQE**: Submission Queue Entry (request)
- **CQE**: Completion Queue Entry (response)
- **SQ Ring**: Array of indices into SQE array
- **CQ Ring**: Array of CQEs

## ublk-Specific io_uring Usage

### Required Setup Flags

```c
// For ublk control and data operations
#define IORING_SETUP_SQE128    (1U << 10)  // 128-byte SQEs
#define IORING_SETUP_CQE32     (1U << 11)  // 32-byte CQEs
```

These are required because:
- `IORING_OP_URING_CMD` needs extra space in SQE
- Passthrough commands return extended data in CQE

### Ring Creation for ublk

```go
// Control plane ring (small, low traffic)
controlRing := CreateRing(16, IORING_SETUP_SQE128 | IORING_SETUP_CQE32)

// Data plane ring (per queue, high traffic)
dataRing := CreateRing(queueDepth * 2, IORING_SETUP_SQE128 | IORING_SETUP_CQE32)
```

## URING_CMD Operation

### SQE Structure for URING_CMD

```c
struct io_uring_sqe {
    __u8  opcode;        // IORING_OP_URING_CMD
    __u8  flags;         
    __u16 ioprio;        
    __s32 fd;            // /dev/ublk-control or /dev/ublkc<ID>
    union {
        __u64 off;       
        __u64 addr2;     
        struct {
            __u32 cmd_op;  // ublk command
            __u32 __pad1;
        };
    };
    union {
        __u64 addr;      // Not used for URING_CMD
        __u64 splice_off_in;
    };
    __u32 len;           
    union {
        // ... other fields ...
    };
    __u64 user_data;     // Tag/identifier
    
    // Extended area (with SQE128)
    __u8 cmd[80];        // Command-specific data
};
```

### Encoding ublk Commands

#### Control Commands
```go
type UringCmdSQE struct {
    opcode    uint8  // = IORING_OP_URING_CMD
    fd        int32  // = control_fd
    cmd_op    uint32 // = UBLK_CMD_*
    user_data uint64 // = unique_id
    cmd       [80]byte // ublksrv_ctrl_cmd encoded here
}
```

#### Data Commands
```go
type DataCmdSQE struct {
    opcode    uint8  // = IORING_OP_URING_CMD
    fd        int32  // = ublkc_fd
    cmd_op    uint32 // = UBLK_IO_*
    user_data uint64 // = tag (maps to descriptor)
    cmd       [80]byte // io-specific data
}
```

## Control Plane Operations

### ADD_DEV Example

```go
func submitAddDev(ring *Ring, params DeviceParams) {
    sqe := ring.GetSQE()
    
    // Setup SQE
    sqe.opcode = IORING_OP_URING_CMD
    sqe.fd = controlFD
    sqe.cmd_op = UBLK_CMD_ADD_DEV
    
    // Encode control command in cmd area
    cmd := &ublksrv_ctrl_cmd{
        cmd: UBLK_CMD_ADD_DEV,
        dev_id: -1, // Let kernel assign
        // ... other fields
    }
    encodeToCmdArea(sqe.cmd[:], cmd)
    
    ring.Submit()
}
```

### Processing Control Response

```go
func processControlCQE(cqe *CQE) {
    if cqe.res < 0 {
        // Error: -errno
        return syscall.Errno(-cqe.res)
    }
    
    // Success, extract data from extended CQE area
    // For ADD_DEV, device info is returned
}
```

## Data Plane Operations

### I/O Request Flow

```
1. Initial Setup (once per tag):
   → Submit UBLK_IO_FETCH_REQ for tag

2. Main Loop:
   a. Receive CQE (request available)
   b. Read descriptor[tag]
   c. Process I/O operation
   d. Submit UBLK_IO_COMMIT_AND_FETCH_REQ
   → Loop back to 2a
```

### FETCH_REQ Submission

```go
func submitFetchReq(ring *Ring, qid, tag uint16) {
    sqe := ring.GetSQE()
    
    sqe.opcode = IORING_OP_URING_CMD
    sqe.fd = ublkcFD
    sqe.cmd_op = UBLK_IO_FETCH_REQ
    sqe.user_data = uint64(tag)
    
    // Encode queue_id and tag in cmd area
    cmd := struct {
        queue_id uint16
        tag      uint16
        // ...
    }{qid, tag}
    encodeToCmdArea(sqe.cmd[:], cmd)
    
    ring.Submit()
}
```

### COMMIT_AND_FETCH_REQ

```go
func submitCommitAndFetch(ring *Ring, tag uint16, result int32) {
    sqe := ring.GetSQE()
    
    sqe.opcode = IORING_OP_URING_CMD
    sqe.fd = ublkcFD
    sqe.cmd_op = UBLK_IO_COMMIT_AND_FETCH_REQ
    sqe.user_data = uint64(tag)
    
    // Encode result in cmd area
    cmd := struct {
        tag    uint16
        result int32
        // ...
    }{tag, result}
    encodeToCmdArea(sqe.cmd[:], cmd)
    
    ring.Submit()
}
```

## Ring Management

### Submission Strategies

#### Batch Submission
```go
// Prepare multiple SQEs
for _, tag := range tags {
    sqe := ring.GetSQE()
    prepareFetchReq(sqe, tag)
}

// Submit all at once
ring.Submit()
```

#### Continuous Processing
```go
for {
    // Wait for completions
    cqe := ring.WaitCQE()
    
    // Process completion
    tag := cqe.user_data
    processIO(tag)
    
    // Immediately resubmit
    submitCommitAndFetch(ring, tag, result)
    
    // Check for more completions without blocking
    for cqe := ring.PeekCQE(); cqe != nil; cqe = ring.PeekCQE() {
        // Process additional completions
    }
}
```

## Memory Management

### Buffer Registration

```go
// Register I/O buffers with io_uring
buffers := make([][]byte, queueDepth)
for i := range buffers {
    buffers[i] = make([]byte, maxIOSize)
}

ring.RegisterBuffers(buffers)
```

### Using Registered Buffers

```go
// In SQE, use buffer index instead of address
sqe.buf_index = bufferIndex
sqe.flags |= IOSQE_BUFFER_SELECT
```

## Feature Detection

### Probing Support

```go
func probeFeatures() Features {
    probe := io_uring_get_probe()
    
    features := Features{}
    
    // Check for URING_CMD support
    if probe.ops[IORING_OP_URING_CMD].flags & IO_URING_OP_SUPPORTED {
        features.UringCmd = true
    }
    
    // Check for SQE128/CQE32
    params := io_uring_params{}
    io_uring_queue_init_params(1, &ring, &params)
    if params.features & IORING_FEAT_EXT_ARG {
        features.ExtendedSQE = true
    }
    
    return features
}
```

## Error Handling

### CQE Error Codes

```go
func handleCQE(cqe *CQE) error {
    if cqe.res < 0 {
        errno := syscall.Errno(-cqe.res)
        switch errno {
        case syscall.EAGAIN:
            // Retry
        case syscall.EINVAL:
            // Invalid operation
        case syscall.EIO:
            // I/O error
        default:
            return errno
        }
    }
    return nil
}
```

## Performance Considerations

### Ring Sizing
- Control ring: Small (16-32 entries)
- Data ring: 2x queue_depth for pipelining
- Consider memory vs performance trade-off

### CPU Affinity
```go
// Bind ring thread to CPU
runtime.LockOSThread()
syscall.SchedSetaffinity(0, cpuset)
```

### Polling vs Interrupt
- Default: Interrupt-driven (io_uring_enter with min_complete)
- Optional: SQPOLL for kernel-side polling (higher CPU, lower latency)

## Debugging

### Tracing io_uring Operations

```bash
# Enable io_uring tracepoints
echo 1 > /sys/kernel/debug/tracing/events/io_uring/enable

# View trace
cat /sys/kernel/debug/tracing/trace
```

### Common Issues

1. **SQE/CQE Size Mismatch**
   - Symptom: EINVAL on ring creation
   - Fix: Ensure kernel supports extended sizes

2. **Ring Overflow**
   - Symptom: Dropped completions
   - Fix: Process CQEs more frequently

3. **Memory Limits**
   - Symptom: ENOMEM on registration
   - Fix: Increase RLIMIT_MEMLOCK

## Go-Specific Considerations

### goroutine Safety
- One goroutine per ring (no sharing)
- Use channels for coordination
- Lock OS thread for CPU affinity

### Memory Pinning
```go
// Prevent GC from moving buffers
runtime.KeepAlive(buffer)

// Or use C.malloc equivalent
buffer := directAlloc(size)
defer directFree(buffer)
```

### Syscall Overhead
- Batch operations to reduce syscalls
- Use io_uring_enter only when necessary
- Consider SQPOLL for high-frequency operations

## Testing io_uring

### Unit Tests
```go
func TestRingCreation(t *testing.T) {
    ring, err := NewRing(32, IORING_SETUP_SQE128)
    if err != nil {
        t.Skip("SQE128 not supported")
    }
    // Test operations
}
```

### Integration Tests
```go
func TestUringCmd(t *testing.T) {
    requireRoot(t)
    requireKernel(t, "6.1")
    
    // Test URING_CMD operations
}
```

## References

- [io_uring man pages](https://man7.org/linux/man-pages/man2/io_uring_enter.2.html)
- [Kernel io_uring documentation](https://kernel.dk/io_uring.pdf)
- [liburing source](https://github.com/axboe/liburing)