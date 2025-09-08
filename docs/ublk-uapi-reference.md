# ublk UAPI Reference (Linux Kernel 6.11)

## Control Commands

### Legacy Commands (Don't use in new applications)
```c
#define UBLK_CMD_GET_QUEUE_AFFINITY  0x01
#define UBLK_CMD_GET_DEV_INFO        0x02
#define UBLK_CMD_ADD_DEV             0x04
#define UBLK_CMD_DEL_DEV             0x05
#define UBLK_CMD_START_DEV           0x06
#define UBLK_CMD_STOP_DEV            0x07
#define UBLK_CMD_SET_PARAMS          0x08
#define UBLK_CMD_GET_PARAMS          0x09
#define UBLK_CMD_START_USER_RECOVERY 0x10
#define UBLK_CMD_END_USER_RECOVERY   0x11
#define UBLK_CMD_GET_DEV_INFO2       0x12
```

### ioctl-Encoded Commands (Preferred)
```c
#define UBLK_U_CMD_GET_QUEUE_AFFINITY _IOR('u', UBLK_CMD_GET_QUEUE_AFFINITY, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_GET_DEV_INFO       _IOR('u', UBLK_CMD_GET_DEV_INFO, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_ADD_DEV            _IOWR('u', UBLK_CMD_ADD_DEV, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_DEL_DEV            _IOWR('u', UBLK_CMD_DEL_DEV, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_START_DEV          _IOWR('u', UBLK_CMD_START_DEV, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_STOP_DEV           _IOWR('u', UBLK_CMD_STOP_DEV, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_SET_PARAMS         _IOWR('u', UBLK_CMD_SET_PARAMS, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_GET_PARAMS         _IOR('u', UBLK_CMD_GET_PARAMS, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_START_USER_RECOVERY _IOWR('u', UBLK_CMD_START_USER_RECOVERY, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_END_USER_RECOVERY  _IOWR('u', UBLK_CMD_END_USER_RECOVERY, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_GET_DEV_INFO2      _IOR('u', UBLK_CMD_GET_DEV_INFO2, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_GET_FEATURES       _IOR('u', 0x13, struct ublksrv_ctrl_cmd)
#define UBLK_U_CMD_DEL_DEV_ASYNC      _IOR('u', 0x14, struct ublksrv_ctrl_cmd)
```

## I/O Commands

### Legacy I/O Commands
```c
#define UBLK_IO_FETCH_REQ            0x20
#define UBLK_IO_COMMIT_AND_FETCH_REQ 0x21
#define UBLK_IO_NEED_GET_DATA        0x22
```

### ioctl-Encoded I/O Commands
```c
#define UBLK_U_IO_FETCH_REQ           _IOWR('u', UBLK_IO_FETCH_REQ, struct ublksrv_io_cmd)
#define UBLK_U_IO_COMMIT_AND_FETCH_REQ _IOWR('u', UBLK_IO_COMMIT_AND_FETCH_REQ, struct ublksrv_io_cmd)
#define UBLK_U_IO_NEED_GET_DATA       _IOWR('u', UBLK_IO_NEED_GET_DATA, struct ublksrv_io_cmd)
```

### I/O Result Codes
```c
#define UBLK_IO_RES_OK           0
#define UBLK_IO_RES_NEED_GET_DATA 1
#define UBLK_IO_RES_ABORT        (-ENODEV)
```

## Feature Flags

```c
#define UBLK_F_SUPPORT_ZERO_COPY      (1ULL << 0)  // Zero copy with 4k blocks
#define UBLK_F_URING_CMD_COMP_IN_TASK (1ULL << 1)  // Force task_work completion
#define UBLK_F_NEED_GET_DATA          (1UL << 2)   // Two-phase write support
#define UBLK_F_USER_RECOVERY          (1UL << 3)   // User recovery support
#define UBLK_F_USER_RECOVERY_REISSUE  (1UL << 4)   // Reissue on recovery
#define UBLK_F_UNPRIVILEGED_DEV       (1UL << 5)   // Unprivileged device creation
#define UBLK_F_CMD_IOCTL_ENCODE       (1UL << 6)   // Use ioctl encoding
#define UBLK_F_USER_COPY              (1UL << 7)   // pread/pwrite for data
#define UBLK_F_ZONED                  (1ULL << 8)  // Zoned storage support
```

## Device States

```c
#define UBLK_S_DEV_DEAD     0
#define UBLK_S_DEV_LIVE     1
#define UBLK_S_DEV_QUIESCED 2
```

## I/O Operations

```c
#define UBLK_IO_OP_READ          0
#define UBLK_IO_OP_WRITE         1
#define UBLK_IO_OP_FLUSH         2
#define UBLK_IO_OP_DISCARD       3
#define UBLK_IO_OP_WRITE_SAME    4
#define UBLK_IO_OP_WRITE_ZEROES  5
#define UBLK_IO_OP_ZONE_OPEN     10
#define UBLK_IO_OP_ZONE_CLOSE    11
#define UBLK_IO_OP_ZONE_FINISH   12
#define UBLK_IO_OP_ZONE_APPEND   13
#define UBLK_IO_OP_ZONE_RESET_ALL 14
#define UBLK_IO_OP_ZONE_RESET    15
#define UBLK_IO_OP_REPORT_ZONES  18
```

## I/O Flags

```c
#define UBLK_IO_F_FAILFAST_DEV       (1U << 8)
#define UBLK_IO_F_FAILFAST_TRANSPORT (1U << 9)
#define UBLK_IO_F_FAILFAST_DRIVER    (1U << 10)
#define UBLK_IO_F_META               (1U << 11)
#define UBLK_IO_F_FUA                (1U << 13)
#define UBLK_IO_F_NOUNMAP            (1U << 15)
#define UBLK_IO_F_SWAP               (1U << 16)
```

## Limits and Constants

```c
#define UBLK_MAX_QUEUE_DEPTH      4096    // Max IOs per queue
#define UBLK_MAX_NR_QUEUES        4096    // Max queues per device
#define UBLK_FEATURES_LEN         8       // Feature flags length

// Buffer offsets
#define UBLKSRV_CMD_BUF_OFFSET    0
#define UBLKSRV_IO_BUF_OFFSET     0x80000000

// IO buffer encoding
#define UBLK_IO_BUF_OFF           0
#define UBLK_IO_BUF_BITS          25      // 32MB max per IO
#define UBLK_IO_BUF_BITS_MASK     ((1ULL << UBLK_IO_BUF_BITS) - 1)

// Tag encoding
#define UBLK_TAG_OFF              UBLK_IO_BUF_BITS
#define UBLK_TAG_BITS             16      // 64K IOs max
#define UBLK_TAG_BITS_MASK        ((1ULL << UBLK_TAG_BITS) - 1)

// Queue ID encoding
#define UBLK_QID_OFF              (UBLK_TAG_OFF + UBLK_TAG_BITS)
#define UBLK_QID_BITS             12
#define UBLK_QID_BITS_MASK        ((1ULL << UBLK_QID_BITS) - 1)

// Total buffer size
#define UBLKSRV_IO_BUF_TOTAL_BITS (UBLK_QID_OFF + UBLK_QID_BITS)
#define UBLKSRV_IO_BUF_TOTAL_SIZE (1ULL << UBLKSRV_IO_BUF_TOTAL_BITS)
```

## Data Structures

### ublksrv_ctrl_cmd
```c
struct ublksrv_ctrl_cmd {
    __u32 dev_id;         // Target device ID (must be valid)
    __u16 queue_id;       // Target queue (-1 if not queue-specific)
    __u16 len;            // Buffer length
    __u64 addr;           // Buffer address (IN or OUT)
    __u64 data[1];        // Inline data
    __u16 dev_path_len;   // For UNPRIVILEGED_DEV (includes null)
    __u16 pad;
    __u32 reserved;
};
```

### ublksrv_ctrl_dev_info
```c
struct ublksrv_ctrl_dev_info {
    __u16 nr_hw_queues;       // Number of hardware queues
    __u16 queue_depth;        // Depth per queue
    __u16 state;              // Device state (UBLK_S_*)
    __u16 pad0;
    __u32 max_io_buf_bytes;   // Max IO buffer size
    __u32 dev_id;             // Device ID
    __s32 ublksrv_pid;        // Server process ID
    __u32 pad1;
    __u64 flags;              // Feature flags
    __u64 ublksrv_flags;      // Server-internal flags
    __u32 owner_uid;          // Owner UID (kernel-set)
    __u32 owner_gid;          // Owner GID (kernel-set)
    __u64 reserved1;
    __u64 reserved2;
};
```

### ublksrv_io_desc
```c
struct ublksrv_io_desc {
    __u32 op_flags;       // op: bits 0-7, flags: bits 8-31
    union {
        __u32 nr_sectors;
        __u32 nr_zones;   // For REPORT_ZONES
    };
    __u64 start_sector;   // Starting sector
    __u64 addr;          // Buffer address in userspace
};

// Helper functions
static inline __u8 ublksrv_get_op(const struct ublksrv_io_desc *iod) {
    return iod->op_flags & 0xff;
}

static inline __u32 ublksrv_get_flags(const struct ublksrv_io_desc *iod) {
    return iod->op_flags >> 8;
}
```

### ublksrv_io_cmd
```c
struct ublksrv_io_cmd {
    __u16 q_id;           // Queue ID
    __u16 tag;            // Request tag
    __s32 result;         // IO result (COMMIT* only)
    union {
        __u64 addr;              // Buffer address (FETCH* only)
        __u64 zone_append_lba;   // For zone append
    };
};
```

## Device Parameters

### ublk_param_basic
```c
struct ublk_param_basic {
    __u32 attrs;                  // Attribute flags
    __u8  logical_bs_shift;       // Logical block size shift
    __u8  physical_bs_shift;      // Physical block size shift
    __u8  io_opt_shift;           // Optimal IO size shift
    __u8  io_min_shift;           // Minimum IO size shift
    __u32 max_sectors;            // Max sectors per request
    __u32 chunk_sectors;          // Chunk size in sectors
    __u64 dev_sectors;            // Device size in sectors
    __u64 virt_boundary_mask;     // Virtual boundary mask
};

// Attribute flags
#define UBLK_ATTR_READ_ONLY      (1 << 0)
#define UBLK_ATTR_ROTATIONAL     (1 << 1)
#define UBLK_ATTR_VOLATILE_CACHE (1 << 2)
#define UBLK_ATTR_FUA            (1 << 3)
```

### ublk_param_discard
```c
struct ublk_param_discard {
    __u32 discard_alignment;
    __u32 discard_granularity;
    __u32 max_discard_sectors;
    __u32 max_write_zeroes_sectors;
    __u16 max_discard_segments;
    __u16 reserved0;
};
```

### ublk_param_devt
```c
struct ublk_param_devt {
    __u32 char_major;    // Character device major
    __u32 char_minor;    // Character device minor
    __u32 disk_major;    // Disk device major
    __u32 disk_minor;    // Disk device minor
};
```

### ublk_param_zoned
```c
struct ublk_param_zoned {
    __u32 max_open_zones;
    __u32 max_active_zones;
    __u32 max_zone_append_sectors;
    __u8  reserved[20];
};
```

### ublk_params
```c
struct ublk_params {
    __u32 len;    // Total length of parameters
    __u32 types;  // Types of parameters included
    
    struct ublk_param_basic   basic;
    struct ublk_param_discard discard;
    struct ublk_param_devt    devt;
    struct ublk_param_zoned   zoned;
};

// Parameter type flags
#define UBLK_PARAM_TYPE_BASIC    (1 << 0)
#define UBLK_PARAM_TYPE_DISCARD  (1 << 1)
#define UBLK_PARAM_TYPE_DEVT     (1 << 2)
#define UBLK_PARAM_TYPE_ZONED    (1 << 3)
```

## Usage Notes

### Command Encoding
- Legacy commands use raw command codes
- New applications should use ioctl-encoded commands (_IOR/_IOWR macros)
- When UBLK_F_CMD_IOCTL_ENCODE is set, use ioctl encoding

### Memory Layout
- I/O descriptors are mmap'd at offset 0x80000000
- Index calculation: `desc = array[queue_id * queue_depth + tag]`
- Each descriptor is 32 bytes (ublksrv_io_desc)

### Two-Phase Write (NEED_GET_DATA)
1. Write request arrives without data
2. Server returns UBLK_IO_RES_NEED_GET_DATA
3. Server issues UBLK_IO_NEED_GET_DATA with buffer address
4. Kernel copies data to user buffer
5. Server processes write and commits result

### Unprivileged Operation
- Requires UBLK_F_UNPRIVILEGED_DEV flag
- Device owner is set to current user's uid/gid
- Udev rules needed to set proper permissions
- All commands except ADD_DEV restricted to owner

### Zero Copy Support
- Requires UBLK_F_SUPPORT_ZERO_COPY flag
- Requires 4K block size
- Remaps kernel IO buffers into userspace

### User Copy Mode
- Enabled with UBLK_F_USER_COPY flag
- Data transfer via pread()/pwrite() on /dev/ublkcN
- Avoids direct buffer addressing