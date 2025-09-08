# ublk Technical Reference

## Quick Reference Card

### Device Files
```
/dev/ublk-control    # Control device (misc)
/dev/ublkc<ID>       # Character device per ublk device
/dev/ublkb<ID>       # Block device (after START_DEV)
```

### Control Commands
```c
UBLK_CMD_ADD_DEV            0x04
UBLK_CMD_DEL_DEV            0x05
UBLK_CMD_START_DEV          0x06
UBLK_CMD_STOP_DEV           0x07
UBLK_CMD_SET_PARAMS         0x08
UBLK_CMD_GET_PARAMS         0x09
UBLK_CMD_GET_DEV_INFO       0x0a
UBLK_CMD_GET_QUEUE_AFFINITY 0x0b
UBLK_CMD_GET_DEV_INFO2      0x0c
UBLK_CMD_START_USER_RECOVERY 0x10
UBLK_CMD_END_USER_RECOVERY  0x11
```

### I/O Commands
```c
UBLK_IO_FETCH_REQ           0x20
UBLK_IO_COMMIT_AND_FETCH_REQ 0x21
UBLK_IO_NEED_GET_DATA       0x22
```

### Feature Flags
```c
UBLK_F_NEED_GET_DATA        (1UL << 0)
UBLK_F_PER_IO_DAEMON        (1UL << 1)
UBLK_F_UNPRIVILEGED_DEV     (1UL << 2)
UBLK_F_CMD_IOCTL_ENCODE     (1UL << 3)
UBLK_F_AUTO_BUF_REG         (1UL << 4)
UBLK_F_USER_RECOVERY        (1UL << 5)
UBLK_F_USER_RECOVERY_REISSUE (1UL << 6)
```

## Data Structure Layouts

### ublksrv_ctrl_dev_info (64 bytes)
```c
struct ublksrv_ctrl_dev_info {
    __u16 nr_hw_queues;       // 0-1
    __u16 queue_depth;        // 2-3
    __u16 state;             // 4-5
    __u16 pad0;              // 6-7
    __u32 max_io_buf_bytes;  // 8-11
    __u32 dev_id;            // 12-15
    __s32 ublksrv_pid;       // 16-19
    __u32 pad1;              // 20-23
    __u64 flags;             // 24-31
    __u64 ublksrv_flags;     // 32-39
    __u64 reserved[2];       // 40-55
};
```

### ublksrv_io_desc (32 bytes)
```c
struct ublksrv_io_desc {
    __u32 op_flags;          // 0-3   (op | flags)
    __u32 nr_sectors;        // 4-7
    __u64 start_sector;      // 8-15
    __u64 addr;             // 16-23
    union {
        __s32 res;          // 24-27 (result/errno)
        __u32 buf_off;      // 24-27 (buffer offset)
    };
    __u32 pad;              // 28-31
};
```

### ublksrv_ctrl_cmd (48 bytes)
```c
struct ublksrv_ctrl_cmd {
    __u32 cmd;              // 0-3
    __u32 len;              // 4-7
    __u64 addr;             // 8-15
    __u64 data[2];          // 16-31
    __u32 dev_id;           // 32-35
    __u16 queue_id;         // 36-37
    __u16 pad;              // 38-39
    __u64 reserved;         // 40-47
};
```

## Memory Management

### Descriptor Array mmap
```
Offset: UBLKSRV_IO_DESC_MMAP_OFFSET (0x80000000)
Size: nr_queues * queue_depth * sizeof(ublksrv_io_desc)
Access: [queue_id * queue_depth + tag]
```

### Buffer Layout (Default Mode)
```
Pre-allocated buffer per descriptor
Address in desc->addr
Size: max_io_buf_bytes
```

### Buffer Layout (NEED_GET_DATA Mode)
```
Phase 1: No buffer
Phase 2: User provides buffer address
Phase 3: Kernel copies data to user buffer
```

## io_uring Setup

### Control Plane Ring
```c
struct io_uring_params params = {
    .flags = IORING_SETUP_SQE128 | IORING_SETUP_CQE32,
    .sq_entries = 16,  // Small, low traffic
};
```

### Data Plane Ring (per queue)
```c
struct io_uring_params params = {
    .flags = IORING_SETUP_SQE128 | IORING_SETUP_CQE32,
    .sq_entries = queue_depth * 2,  // Allow pipelining
};
```

### URING_CMD SQE Layout
```c
struct io_uring_sqe {
    __u8  opcode;        // = IORING_OP_URING_CMD (33)
    __u8  flags;
    __u16 ioprio;
    __s32 fd;            // control_fd or ublkc_fd
    union {
        struct {
            __u32 cmd_op;  // UBLK command
            __u32 __pad1;
        };
    };
    __u64 user_data;     // tag for data plane
    // ... other fields ...
    __u8 cmd[80];        // Command payload (with SQE128)
};
```

## Command Sequences

### Device Creation
```
1. Open /dev/ublk-control
2. ADD_DEV with params
3. Parse returned dev_info
4. Open /dev/ublkc<ID>
5. mmap descriptor array
6. SET_PARAMS (optional)
7. Create per-queue rings
8. Submit initial FETCH_REQs
9. START_DEV
10. Device ready at /dev/ublkb<ID>
```

### I/O Processing Loop
```
for each tag in [0, queue_depth):
    submit FETCH_REQ(tag)

while running:
    cqe = wait_for_completion()
    tag = cqe.user_data
    desc = descriptors[queue_id * queue_depth + tag]
    
    switch desc.op_flags & OP_MASK:
        case READ:
            backend.ReadAt(desc.addr, desc.start_sector * 512)
        case WRITE:
            backend.WriteAt(desc.addr, desc.start_sector * 512)
        case FLUSH:
            backend.Flush()
        case DISCARD:
            backend.Trim(desc.start_sector * 512, desc.nr_sectors * 512)
    
    submit COMMIT_AND_FETCH_REQ(tag, result)
```

### Device Teardown
```
1. Cancel context / signal stop
2. Wait for queue runners to finish
3. STOP_DEV
4. munmap descriptor array
5. Close /dev/ublkc<ID>
6. DEL_DEV
7. Close /dev/ublk-control
```

## Error Handling

### Control Plane Errors
```c
-ENODEV     // Device not found
-EINVAL     // Invalid parameters
-EBUSY      // Device busy/in use
-EPERM      // Permission denied
-ENOMEM     // Out of memory
-ENOTSUPP   // Feature not supported
```

### Data Plane Errors
```c
0           // Success
-EIO        // I/O error
-ENOMEM     // Memory allocation failed
-EINVAL     // Invalid request
-ENOTSUPP   // Operation not supported
-EROFS      // Read-only filesystem
```

## Performance Parameters

### Recommended Settings
```
Queue Depth: 128-256
Num Queues: Number of CPUs
Block Size: 512 or 4096
Max I/O Size: 1MB
CPU Affinity: Enabled
```

### Memory Requirements
```
Per Device:
  Control: ~1KB
  Descriptors: nr_queues * queue_depth * 32 bytes
  Buffers: nr_queues * queue_depth * max_io_size
  
Example (4 queues, depth 128, 1MB max I/O):
  Descriptors: 4 * 128 * 32 = 16KB
  Buffers: 4 * 128 * 1MB = 512MB
  Total: ~512MB
```

## Kernel Source References

### Key Files
```
drivers/block/ublk_drv.c       # Main driver
include/uapi/linux/ublk_cmd.h  # UAPI definitions
block/blk-mq.c                 # Multi-queue infrastructure
fs/io_uring.c                  # io_uring implementation
```

### Key Functions
```c
ublk_ctrl_add_dev()      # Handle ADD_DEV
ublk_ctrl_start_dev()    # Handle START_DEV
ublk_ctrl_stop_dev()     # Handle STOP_DEV
ublk_ctrl_del_dev()      # Handle DEL_DEV
ublk_queue_rq()          # Queue block request
ublk_commit_completion() # Complete I/O request
```

## Debugging Commands

### Check Module
```bash
lsmod | grep ublk
modinfo ublk_drv
```

### Check Devices
```bash
ls -la /dev/ublk*
cat /sys/class/ublk/ublkb*/dev_info
```

### Monitor I/O
```bash
iostat -x 1 /dev/ublkb0
blktrace -d /dev/ublkb0
```

### Kernel Messages
```bash
dmesg | grep ublk
journalctl -f | grep ublk
```

### Trace Points
```bash
echo 1 > /sys/kernel/debug/tracing/events/ublk/enable
cat /sys/kernel/debug/tracing/trace
```

## Common Issues and Solutions

### Issue: EINVAL on ADD_DEV
**Cause**: Invalid parameters
**Solution**: Check queue_depth (power of 2), nr_queues (> 0)

### Issue: ENOMEM on buffer registration
**Cause**: RLIMIT_MEMLOCK too low
**Solution**: Increase ulimit -l or use CAP_IPC_LOCK

### Issue: Poor performance
**Causes**:
- No CPU affinity
- Queue depth too low
- Single queue
- Debug build

**Solutions**:
- Enable CPU affinity
- Increase queue depth
- Use multiple queues
- Build with optimizations

### Issue: Device not appearing
**Cause**: START_DEV not called or failed
**Solution**: Check return value of START_DEV, check dmesg

## Version History

### Linux 6.1
- Initial ublk support
- Basic functionality

### Linux 6.2
- UBLK_F_UNPRIVILEGED_DEV
- Performance improvements

### Linux 6.3
- Bug fixes
- Stability improvements

### Linux 6.4
- User recovery support
- Additional features

### Linux 6.5+
- Continued improvements
- Production ready