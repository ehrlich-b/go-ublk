# io_uring URING_CMD Encoding for ublk

## Overview

ublk uses `IORING_OP_URING_CMD` to pass commands between userspace and kernel. This document details the exact encoding format.

## SQE Structure with URING_CMD

### Standard SQE (64 bytes)
```c
struct io_uring_sqe {
    __u8   opcode;         // = IORING_OP_URING_CMD (33)
    __u8   flags;          // SQE flags
    __u16  ioprio;         // Priority (unused)
    __s32  fd;             // File descriptor
    union {
        __u64 off;
        __u64 addr2;
        struct {
            __u32 cmd_op;  // Command opcode
            __u32 __pad1;
        };
    };
    union {
        __u64 addr;        // Usually unused for URING_CMD
        __u64 splice_off_in;
    };
    __u32  len;            // Usually 0 for URING_CMD
    union {
        // Various other fields...
    };
    __u64  user_data;      // Passed back in CQE
    // More fields...
};
```

### Extended SQE128 (128 bytes)
When using `IORING_SETUP_SQE128`:
```c
struct io_uring_sqe {
    // First 64 bytes as above
    __u8 cmd[0];           // Start of extended area
    // Additional 64 bytes available
    // Total cmd area: 80 bytes usable
};
```

## Command Area Layout

### Available Space
- Without SQE128: 16 bytes (limited)
- With SQE128: 80 bytes (full command data)

### ublk Command Encoding

#### For Control Commands
The `cmd[80]` area contains the command-specific data:

```c
// In SQE setup:
sqe->opcode = IORING_OP_URING_CMD;
sqe->fd = control_fd;  // /dev/ublk-control
sqe->cmd_op = UBLK_CMD_* or ioctl_encode(UBLK_U_CMD_*);

// Command data in cmd area depends on the command
```

#### For I/O Commands
```c
// In SQE setup:
sqe->opcode = IORING_OP_URING_CMD;
sqe->fd = ublkc_fd;    // /dev/ublkc<ID>
sqe->cmd_op = UBLK_IO_* or ioctl_encode(UBLK_U_IO_*);
sqe->user_data = tag;   // IO tag

// The cmd area contains ublksrv_io_cmd data
```

## Encoding Methods

### Method 1: Legacy (Raw Command)
```c
sqe->cmd_op = UBLK_CMD_ADD_DEV;  // Direct command value
```

### Method 2: ioctl Encoding (Recommended)
```c
// When UBLK_F_CMD_IOCTL_ENCODE is set
sqe->cmd_op = _IOC(_IOC_READ|_IOC_WRITE, 'u', UBLK_CMD_ADD_DEV, 
                    sizeof(struct ublksrv_ctrl_cmd));
```

## Command-Specific Encoding

### ADD_DEV Command
```c
// SQE setup
sqe->opcode = IORING_OP_URING_CMD;
sqe->fd = control_fd;
sqe->cmd_op = UBLK_CMD_ADD_DEV;  // or ioctl encoded

// In cmd[80] area:
struct ublksrv_ctrl_cmd cmd = {
    .dev_id = -1,           // Let kernel assign
    .queue_id = -1,         // Not queue-specific
    .len = sizeof(struct ublksrv_ctrl_dev_info),
    .addr = (__u64)&dev_info,  // Pointer to dev_info struct
    .data[0] = 0,           // Optional inline data
};
// Copy cmd to sqe->cmd area
```

### FETCH_REQ Command
```c
// SQE setup
sqe->opcode = IORING_OP_URING_CMD;
sqe->fd = ublkc_fd;
sqe->cmd_op = UBLK_IO_FETCH_REQ;
sqe->user_data = tag;

// In cmd[80] area:
struct ublksrv_io_cmd io_cmd = {
    .q_id = queue_id,
    .tag = tag,
    .result = 0,            // Not used for FETCH
    .addr = buffer_addr,    // User buffer address
};
// Copy io_cmd to sqe->cmd area
```

### COMMIT_AND_FETCH_REQ Command
```c
// SQE setup
sqe->opcode = IORING_OP_URING_CMD;
sqe->fd = ublkc_fd;
sqe->cmd_op = UBLK_IO_COMMIT_AND_FETCH_REQ;
sqe->user_data = tag;

// In cmd[80] area:
struct ublksrv_io_cmd io_cmd = {
    .q_id = queue_id,
    .tag = tag,
    .result = io_result,    // Result of completed IO
    .addr = buffer_addr,    // Buffer for next request
};
// Copy io_cmd to sqe->cmd area
```

## CQE Processing

### Standard CQE (16 bytes)
```c
struct io_uring_cqe {
    __u64 user_data;        // Tag from SQE
    __s32 res;              // Result (0 or -errno)
    __u32 flags;
};
```

### Extended CQE32 (32 bytes)
With `IORING_SETUP_CQE32`:
```c
struct io_uring_cqe {
    __u64 user_data;
    __s32 res;
    __u32 flags;
    __u64 extra1;           // Additional data
    __u64 extra2;           // Additional data
};
```

## Go Implementation Considerations

### Unsafe Memory Access
```go
// Encoding command in SQE
func encodeCommand(sqe *SQE, cmd interface{}) {
    cmdBytes := (*[80]byte)(unsafe.Pointer(&sqe.cmd[0]))
    
    switch v := cmd.(type) {
    case *UblksrvCtrlCmd:
        copy(cmdBytes[:], (*[unsafe.Sizeof(*v)]byte)(unsafe.Pointer(v))[:])
    case *UblksrvIoCmd:
        copy(cmdBytes[:], (*[unsafe.Sizeof(*v)]byte)(unsafe.Pointer(v))[:])
    }
}
```

### ioctl Encoding Helper
```go
const (
    _IOC_WRITE = 1
    _IOC_READ  = 2
)

func ioctlEncode(dir, typ, nr, size uint32) uint32 {
    return (dir << 30) | (size << 16) | (typ << 8) | nr
}

func ublkCmdEncode(cmd uint32) uint32 {
    if useIoctlEncoding {
        return ioctlEncode(_IOC_READ|_IOC_WRITE, 'u', cmd, 
                          uint32(unsafe.Sizeof(UblksrvCtrlCmd{})))
    }
    return cmd
}
```

## Memory Layout in cmd Area

### Byte-by-byte Layout for ublksrv_ctrl_cmd
```
Offset  Size  Field
0       4     dev_id
4       2     queue_id  
6       2     len
8       8     addr
16      8     data[0]
24      2     dev_path_len
26      2     pad
28      4     reserved
Total: 32 bytes (fits in 80-byte cmd area)
```

### Byte-by-byte Layout for ublksrv_io_cmd
```
Offset  Size  Field
0       2     q_id
2       2     tag
4       4     result
8       8     addr/zone_append_lba
Total: 16 bytes (fits in 80-byte cmd area)
```

## Alignment and Padding

- Structures should be naturally aligned
- Use compiler directives to ensure no padding:
  ```go
  type UblksrvIoCmd struct {
      QID    uint16
      Tag    uint16
      Result int32
      Addr   uint64
  } // Naturally aligned, no padding needed
  ```

## Error Handling

### Command Submission Errors
- `-EINVAL`: Invalid command or parameters
- `-ENODEV`: Device not found
- `-EBUSY`: Device busy
- `-ENOMEM`: Out of memory

### CQE Result Interpretation
```go
func processCQE(cqe *CQE) error {
    if cqe.Res < 0 {
        return syscall.Errno(-cqe.Res)
    }
    // Success, process based on command type
    return nil
}
```

## Testing Command Encoding

### Verification Steps
1. Use strace to capture actual SQE contents:
   ```bash
   strace -e io_uring_enter ./ublk-test
   ```

2. Compare with C implementation:
   ```bash
   # Run ublksrv and capture syscalls
   strace -o ublksrv.trace ublksrv ...
   # Run Go implementation
   strace -o go-ublk.trace ./go-ublk ...
   # Compare the traces
   ```

3. Use kernel tracing:
   ```bash
   echo 1 > /sys/kernel/debug/tracing/events/io_uring/enable
   cat /sys/kernel/debug/tracing/trace
   ```

## Key Insights

1. **Always use SQE128**: ublk requires the extended command area
2. **ioctl encoding**: Prefer ioctl-encoded commands when supported
3. **Alignment matters**: Ensure structures are properly aligned
4. **Tag in user_data**: For I/O commands, tag goes in user_data field
5. **cmd_op field**: Command opcode goes in the cmd_op union field

## References

- Linux kernel: `drivers/block/ublk_drv.c`
- io_uring: `fs/io_uring.c`
- ublksrv: `https://github.com/ublk-org/ublksrv`