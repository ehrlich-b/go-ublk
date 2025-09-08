# ublk Kernel Interface Documentation

## Overview

The ublk (userspace block device) driver was introduced in Linux 6.1 as a high-performance alternative to NBD (Network Block Device). It uses io_uring for communication between kernel and userspace.

## Kernel Requirements

### Minimum Version
- **Linux 6.1**: Initial ublk support
- **Linux 6.2**: Adds unprivileged device support (UBLK_F_UNPRIVILEGED_DEV)
- **Linux 6.3**: Performance improvements, bug fixes
- **Linux 6.4**: Additional features, stability
- **Linux 6.5+**: Recommended for production use

### Kernel Configuration
Required kernel config options:
```
CONFIG_BLK_DEV_UBLK=m      # ublk module
CONFIG_IO_URING=y          # io_uring support
CONFIG_BLOCK=y             # Block layer
CONFIG_BLK_MQ=y            # Multi-queue block layer
```

### Module Loading
```bash
# Check if module exists
modinfo ublk_drv

# Load module
sudo modprobe ublk_drv

# Verify loaded
lsmod | grep ublk
```

## Device Nodes

### Control Device
- **Path**: `/dev/ublk-control`
- **Type**: Misc character device
- **Purpose**: Device management (add, delete, configure)
- **Major/Minor**: Dynamic (misc device)

### Character Devices
- **Path**: `/dev/ublkc<ID>` (e.g., /dev/ublkc0)
- **Type**: Character device per ublk device
- **Purpose**: Data plane communication
- **Created**: After ADD_DEV command

### Block Devices
- **Path**: `/dev/ublkb<ID>` (e.g., /dev/ublkb0)
- **Type**: Block device
- **Purpose**: Actual block device for user applications
- **Created**: After START_DEV command

## Control Commands

### Command List
```c
enum {
    UBLK_CMD_ADD_DEV = 0x04,
    UBLK_CMD_DEL_DEV = 0x05,
    UBLK_CMD_START_DEV = 0x06,
    UBLK_CMD_STOP_DEV = 0x07,
    UBLK_CMD_SET_PARAMS = 0x08,
    UBLK_CMD_GET_PARAMS = 0x09,
    UBLK_CMD_GET_DEV_INFO = 0x0a,
    UBLK_CMD_GET_QUEUE_AFFINITY = 0x0b,
    UBLK_CMD_GET_DEV_INFO2 = 0x0c,
    UBLK_CMD_START_USER_RECOVERY = 0x10,
    UBLK_CMD_END_USER_RECOVERY = 0x11,
};
```

### Command Sequence
```
1. ADD_DEV       → Creates /dev/ublkc<ID>
2. SET_PARAMS    → Configure device parameters (optional)
3. START_DEV     → Creates /dev/ublkb<ID>, device is ready
4. [Device serves I/O]
5. STOP_DEV      → Stops I/O, removes /dev/ublkb<ID>
6. DEL_DEV       → Removes /dev/ublkc<ID>, cleanup
```

## Feature Flags

### Core Features
```c
#define UBLK_F_NEED_GET_DATA        (1UL << 0)  // Two-phase write
#define UBLK_F_PER_IO_DAEMON        (1UL << 1)  // Per-queue daemon
#define UBLK_F_UNPRIVILEGED_DEV     (1UL << 2)  // Non-root operation
#define UBLK_F_CMD_IOCTL_ENCODE     (1UL << 3)  // ioctl encoding
#define UBLK_F_AUTO_BUF_REG         (1UL << 4)  // Auto buffer reg
#define UBLK_F_USER_RECOVERY        (1UL << 5)  // Recovery support
#define UBLK_F_USER_RECOVERY_REISSUE (1UL << 6) // Reissue on recovery
```

### Feature Negotiation
- Features requested in ADD_DEV
- Kernel returns negotiated features
- Must respect kernel's decision

## Data Structures

### Control Device Info
```c
struct ublksrv_ctrl_dev_info {
    __u16 nr_hw_queues;      // Number of hardware queues
    __u16 queue_depth;       // Depth per queue
    __u16 state;            // Device state
    __u16 pad0;
    __u32 max_io_buf_bytes; // Max I/O buffer size
    __u32 dev_id;          // Device ID
    __s32 ublksrv_pid;     // Server process ID
    __u32 pad1;
    __u64 flags;           // Feature flags
    __u64 ublksrv_flags;   // Server flags
    __u64 reserved[2];
};
```

### I/O Descriptor
```c
struct ublksrv_io_desc {
    __u32 op_flags;        // Operation and flags
    __u32 nr_sectors;      // Number of sectors
    __u64 start_sector;    // Starting sector
    __u64 addr;           // Buffer address
    // Result fields (after completion)
    __s32 res;            // Result/errno
    __u32 pad;
};
```

### Control Command
```c
struct ublksrv_ctrl_cmd {
    __u32 cmd;            // Command code
    __u32 len;            // Data length
    __u64 addr;           // Data address
    __u64 data[2];        // Inline data
    __u32 dev_id;         // Device ID
    __u16 queue_id;       // Queue ID
    __u16 pad;
};
```

## Memory Layout

### Descriptor Array
- Location: mmap from `/dev/ublkc<ID>`
- Size: `nr_queues * queue_depth * sizeof(ublksrv_io_desc)`
- Indexing: `desc = array[queue_id * queue_depth + tag]`
- Alignment: Page-aligned

### mmap Offsets
```c
#define UBLKSRV_IO_DESC_MMAP_OFFSET 0x80000000
```

## I/O Commands

### Data Plane Commands
```c
#define UBLK_IO_FETCH_REQ           0x20
#define UBLK_IO_COMMIT_AND_FETCH_REQ 0x21
#define UBLK_IO_NEED_GET_DATA       0x22
```

### Operation Types
```c
#define UBLK_IO_OP_READ             0
#define UBLK_IO_OP_WRITE            1
#define UBLK_IO_OP_FLUSH            2
#define UBLK_IO_OP_DISCARD          3
#define UBLK_IO_OP_WRITE_SAME       4
#define UBLK_IO_OP_WRITE_ZEROES     5
```

## State Machine

### Device States
```
UBLK_DEV_S_INIT    → After ADD_DEV
UBLK_DEV_S_LIVE    → After START_DEV
UBLK_DEV_S_STOPPING → During STOP_DEV
UBLK_DEV_S_STOPPED → After STOP_DEV
```

### I/O Request States
```
1. IDLE       → Request slot available
2. FETCHING   → FETCH_REQ submitted
3. ACTIVE     → Request received, processing
4. COMMITTING → COMMIT submitted
→ back to IDLE
```

## Error Codes

### Common Errors
- `-ENODEV`: Device not found
- `-EINVAL`: Invalid parameters
- `-EBUSY`: Device busy
- `-EPERM`: Permission denied
- `-ENOMEM`: Out of memory
- `-ENOTSUPP`: Feature not supported

## Sysfs Interface

### Device Attributes
```
/sys/block/ublkb<ID>/
├── queue/
│   ├── nr_hw_queues
│   ├── queue_depth
│   └── ...
└── ublk/
    ├── dev_id
    ├── state
    └── features
```

## Debugging

### Enable Debug Messages
```bash
echo 'module ublk_drv +p' > /sys/kernel/debug/dynamic_debug/control
```

### Trace Events
```bash
# Available events
ls /sys/kernel/debug/tracing/events/ublk/

# Enable all ublk events
echo 1 > /sys/kernel/debug/tracing/events/ublk/enable
```

### Key Trace Points
- `ublk_add_dev`
- `ublk_del_dev`
- `ublk_start_dev`
- `ublk_stop_dev`
- `ublk_io_fetch`
- `ublk_io_commit`

## Performance Tuning

### Queue Configuration
- Multiple queues for parallelism
- Queue depth affects latency/throughput trade-off
- CPU affinity for queue threads

### Memory Considerations
- Pre-allocated buffers reduce allocation overhead
- RLIMIT_MEMLOCK may need adjustment
- Huge pages can improve TLB efficiency

### io_uring Optimizations
- Batch submissions/completions
- Use SQE128/CQE32 for extended features
- Consider SQPOLL for reduced syscalls (advanced)

## Compatibility Notes

### Kernel Version Features
- 6.1: Basic ublk support
- 6.2: Unprivileged devices
- 6.3: Performance improvements
- 6.4: Recovery enhancements
- 6.5+: Stability and features

### Distribution Support
- Ubuntu 23.04+: Native support
- Fedora 38+: Module available
- RHEL 9.3+: Tech preview
- Debian 12+: Backported

## References

- [Kernel Documentation](https://docs.kernel.org/block/ublk.html)
- [UAPI Header](https://github.com/torvalds/linux/blob/master/include/uapi/linux/ublk_cmd.h)
- [Implementation](https://github.com/torvalds/linux/blob/master/drivers/block/ublk_drv.c)