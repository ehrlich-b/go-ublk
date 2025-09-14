// Package uapi provides Linux kernel UAPI definitions for ublk
package uapi

// Control Commands (Legacy - don't use in new applications)
const (
	UBLK_CMD_GET_QUEUE_AFFINITY  = 0x01
	UBLK_CMD_GET_DEV_INFO        = 0x02
	UBLK_CMD_ADD_DEV             = 0x04
	UBLK_CMD_DEL_DEV             = 0x05
	UBLK_CMD_START_DEV           = 0x06
	UBLK_CMD_STOP_DEV            = 0x07
	UBLK_CMD_SET_PARAMS          = 0x08
	UBLK_CMD_GET_PARAMS          = 0x09
	UBLK_CMD_START_USER_RECOVERY = 0x10
	UBLK_CMD_END_USER_RECOVERY   = 0x11
	UBLK_CMD_GET_DEV_INFO2       = 0x12
)

// I/O Commands (Legacy)
const (
	UBLK_IO_FETCH_REQ            = 0x20
	UBLK_IO_COMMIT_AND_FETCH_REQ = 0x21
	UBLK_IO_NEED_GET_DATA        = 0x22
)

// I/O Result Codes
const (
	UBLK_IO_RES_OK           = 0
	UBLK_IO_RES_NEED_GET_DATA = 1
	UBLK_IO_RES_ABORT        = -19 // -ENODEV
)

// Feature Flags (64-bit)
const (
	UBLK_F_SUPPORT_ZERO_COPY      = 1 << 0  // Zero copy with 4k blocks
	UBLK_F_URING_CMD_COMP_IN_TASK = 1 << 1  // Force task_work completion
	UBLK_F_NEED_GET_DATA          = 1 << 2  // Two-phase write support
	UBLK_F_USER_RECOVERY          = 1 << 3  // User recovery support
	UBLK_F_USER_RECOVERY_REISSUE  = 1 << 4  // Reissue on recovery
	UBLK_F_UNPRIVILEGED_DEV       = 1 << 5  // Unprivileged device creation
	UBLK_F_CMD_IOCTL_ENCODE       = 1 << 6  // Use ioctl encoding
	UBLK_F_USER_COPY              = 1 << 7  // pread/pwrite for data
	UBLK_F_ZONED                  = 1 << 8  // Zoned storage support
)

// Device States
const (
	UBLK_S_DEV_DEAD     = 0
	UBLK_S_DEV_LIVE     = 1
	UBLK_S_DEV_QUIESCED = 2
)

// I/O Operations
const (
	UBLK_IO_OP_READ          = 0
	UBLK_IO_OP_WRITE         = 1
	UBLK_IO_OP_FLUSH         = 2
	UBLK_IO_OP_DISCARD       = 3
	UBLK_IO_OP_WRITE_SAME    = 4
	UBLK_IO_OP_WRITE_ZEROES  = 5
	UBLK_IO_OP_ZONE_OPEN     = 10
	UBLK_IO_OP_ZONE_CLOSE    = 11
	UBLK_IO_OP_ZONE_FINISH   = 12
	UBLK_IO_OP_ZONE_APPEND   = 13
	UBLK_IO_OP_ZONE_RESET_ALL = 14
	UBLK_IO_OP_ZONE_RESET    = 15
	UBLK_IO_OP_REPORT_ZONES  = 18
)

// I/O Flags
const (
	UBLK_IO_F_FAILFAST_DEV       = 1 << 8
	UBLK_IO_F_FAILFAST_TRANSPORT = 1 << 9
	UBLK_IO_F_FAILFAST_DRIVER    = 1 << 10
	UBLK_IO_F_META               = 1 << 11
	UBLK_IO_F_FUA                = 1 << 13
	UBLK_IO_F_NOUNMAP            = 1 << 15
	UBLK_IO_F_SWAP               = 1 << 16
)

// Limits and Constants
const (
	UBLK_MAX_QUEUE_DEPTH = 4096 // Max IOs per queue
	UBLK_MAX_NR_QUEUES   = 4096 // Max queues per device
	UBLK_FEATURES_LEN    = 8    // Feature flags length (bytes)

	// Buffer offsets
	UBLKSRV_CMD_BUF_OFFSET = 0
	UBLKSRV_IO_BUF_OFFSET  = 0x80000000

	// IO buffer encoding
	UBLK_IO_BUF_OFF       = 0
	UBLK_IO_BUF_BITS      = 25 // 32MB max per IO
	UBLK_IO_BUF_BITS_MASK = (1 << UBLK_IO_BUF_BITS) - 1

	// Tag encoding
	UBLK_TAG_OFF       = UBLK_IO_BUF_BITS
	UBLK_TAG_BITS      = 16 // 64K IOs max
	UBLK_TAG_BITS_MASK = (1 << UBLK_TAG_BITS) - 1

	// Queue ID encoding
	UBLK_QID_OFF       = UBLK_TAG_OFF + UBLK_TAG_BITS
	UBLK_QID_BITS      = 12
	UBLK_QID_BITS_MASK = (1 << UBLK_QID_BITS) - 1

	// Total buffer size
	UBLKSRV_IO_BUF_TOTAL_BITS = UBLK_QID_OFF + UBLK_QID_BITS
	UBLKSRV_IO_BUF_TOTAL_SIZE = 1 << UBLKSRV_IO_BUF_TOTAL_BITS
)

// Device Attribute Flags
const (
	UBLK_ATTR_READ_ONLY      = 1 << 0
	UBLK_ATTR_ROTATIONAL     = 1 << 1
	UBLK_ATTR_VOLATILE_CACHE = 1 << 2
	UBLK_ATTR_FUA            = 1 << 3
)

// Parameter Type Flags
const (
	UBLK_PARAM_TYPE_BASIC   = 1 << 0
	UBLK_PARAM_TYPE_DISCARD = 1 << 1
	UBLK_PARAM_TYPE_DEVT    = 1 << 2
	UBLK_PARAM_TYPE_ZONED   = 1 << 3
)

// ioctl encoding constants
const (
	_IOC_WRITE     = 1
	_IOC_READ      = 2
	_IOC_SIZEBITS  = 14
	_IOC_DIRBITS   = 2
	_IOC_TYPEBITS  = 8
	_IOC_NRBITS    = 8
	_IOC_NRSHIFT   = 0
	_IOC_TYPESHIFT = _IOC_NRSHIFT + _IOC_NRBITS
	_IOC_SIZESHIFT = _IOC_TYPESHIFT + _IOC_TYPEBITS
	_IOC_DIRSHIFT  = _IOC_SIZESHIFT + _IOC_SIZEBITS
)

// IoctlEncode creates an ioctl command number
func IoctlEncode(dir, typ, nr, size uint32) uint32 {
	return (dir << _IOC_DIRSHIFT) |
		(size << _IOC_SIZESHIFT) |
		(typ << _IOC_TYPESHIFT) |
		(nr << _IOC_NRSHIFT)
}

// Helper function to create ublk ioctl commands
func UblkCtrlCmd(cmd uint32) uint32 {
    // sizeof(UblksrvCtrlCmd) = 48 bytes (variant used by this project)
    return IoctlEncode(_IOC_READ|_IOC_WRITE, 'u', cmd, 48)
}

func UblkIOCmd(cmd uint32) uint32 {
	return IoctlEncode(_IOC_READ|_IOC_WRITE, 'u', cmd, 16) // sizeof(UblksrvIOCmd)
}
