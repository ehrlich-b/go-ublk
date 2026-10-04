// Package uapi provides Linux kernel UAPI definitions for ublk.
//
// It mirrors include/uapi/linux/ublk_cmd.h as of v7.3-rc5, plus the LBMD_PI_*
// constants from include/uapi/linux/fs.h that ublk_param_integrity refers to.
// scripts/uapi-fixtures.c compiles against the real header and
// TestKernelCFixtures checks every constant, struct size, field offset and
// helper here against its output.
package uapi

// A "sector" is always 512 bytes in the block layer and therefore in every
// ublk structure that counts them: ublk_param_basic.dev_sectors and max_sectors,
// and ublksrv_io_desc.start_sector and nr_sectors. This is independent of the
// device's logical block size, which is carried separately as logical_bs_shift.
// Conflating the two reports the wrong capacity and does I/O at the wrong offset.
const (
	SectorSize  = 512
	SectorShift = 9
)

// Control command numbers (the _IOC_NR of each control opcode). The header
// calls these "legacy": a kernel built without CONFIG_BLKDEV_UBLK_LEGACY_OPCODES
// rejects them as raw opcodes, so they are only sent ioctl-encoded (see
// UBLK_U_CMD_*). Newer commands have no raw form at all.
const (
	UBLK_CMD_GET_QUEUE_AFFINITY  = 0x01 // v6.0
	UBLK_CMD_GET_DEV_INFO        = 0x02 // v6.0
	UBLK_CMD_ADD_DEV             = 0x04 // v6.0
	UBLK_CMD_DEL_DEV             = 0x05 // v6.0
	UBLK_CMD_START_DEV           = 0x06 // v6.0
	UBLK_CMD_STOP_DEV            = 0x07 // v6.0
	UBLK_CMD_SET_PARAMS          = 0x08 // v6.0
	UBLK_CMD_GET_PARAMS          = 0x09 // v6.0
	UBLK_CMD_START_USER_RECOVERY = 0x10 // v6.1
	UBLK_CMD_END_USER_RECOVERY   = 0x11 // v6.1
	UBLK_CMD_GET_DEV_INFO2       = 0x12 // v6.3

	// The header defines no UBLK_CMD_* names for these; the driver mirrors
	// them privately with _IOC_NR(). Named here so code can switch on them.
	UBLK_CMD_GET_FEATURES  = 0x13 // v6.5
	UBLK_CMD_DEL_DEV_ASYNC = 0x14 // v6.9 (dispatch broken until v6.11)
	UBLK_CMD_UPDATE_SIZE   = 0x15 // v6.16
	UBLK_CMD_QUIESCE_DEV   = 0x16 // v6.16
	UBLK_CMD_TRY_STOP_DEV  = 0x17 // v7.0
	UBLK_CMD_REG_BUF       = 0x18 // v7.1
	UBLK_CMD_UNREG_BUF     = 0x19 // v7.1
)

const (
	ublkCtrlCmdStructSize = 32 // sizeof(struct ublksrv_ctrl_cmd)
	ublkIOCmdStructSize   = 16 // sizeof(struct ublksrv_io_cmd)
	ublkBatchIOStructSize = 16 // sizeof(struct ublk_batch_io)
	ublkIoctlType         = 'u'
	ublkIocR              = _IOC_READ << _IOC_DIRSHIFT
	ublkIocRW             = (_IOC_READ | _IOC_WRITE) << _IOC_DIRSHIFT
	ublkCtrlIoc           = ublkCtrlCmdStructSize<<_IOC_SIZESHIFT | ublkIoctlType<<_IOC_TYPESHIFT
	ublkIOIoc             = ublkIOCmdStructSize<<_IOC_SIZESHIFT | ublkIoctlType<<_IOC_TYPESHIFT
	ublkBatchIoc          = ublkBatchIOStructSize<<_IOC_SIZESHIFT | ublkIoctlType<<_IOC_TYPESHIFT
)

// Ioctl-encoded control opcodes, exactly as the header defines them. The
// driver only inspects _IOC_TYPE and _IOC_NR of a control opcode — with one
// exception: GET_FEATURES is compared against this full value
// (ublk_ctrl_uring_cmd: "cmd_op == UBLK_U_CMD_GET_FEATURES"), so sending it
// _IOWR-encoded falls through to a device lookup and fails with ENODEV.
const (
	UBLK_U_CMD_GET_QUEUE_AFFINITY  = ublkIocR | ublkCtrlIoc | UBLK_CMD_GET_QUEUE_AFFINITY
	UBLK_U_CMD_GET_DEV_INFO        = ublkIocR | ublkCtrlIoc | UBLK_CMD_GET_DEV_INFO
	UBLK_U_CMD_ADD_DEV             = ublkIocRW | ublkCtrlIoc | UBLK_CMD_ADD_DEV
	UBLK_U_CMD_DEL_DEV             = ublkIocRW | ublkCtrlIoc | UBLK_CMD_DEL_DEV
	UBLK_U_CMD_START_DEV           = ublkIocRW | ublkCtrlIoc | UBLK_CMD_START_DEV
	UBLK_U_CMD_STOP_DEV            = ublkIocRW | ublkCtrlIoc | UBLK_CMD_STOP_DEV
	UBLK_U_CMD_SET_PARAMS          = ublkIocRW | ublkCtrlIoc | UBLK_CMD_SET_PARAMS
	UBLK_U_CMD_GET_PARAMS          = ublkIocR | ublkCtrlIoc | UBLK_CMD_GET_PARAMS
	UBLK_U_CMD_START_USER_RECOVERY = ublkIocRW | ublkCtrlIoc | UBLK_CMD_START_USER_RECOVERY
	UBLK_U_CMD_END_USER_RECOVERY   = ublkIocRW | ublkCtrlIoc | UBLK_CMD_END_USER_RECOVERY
	UBLK_U_CMD_GET_DEV_INFO2       = ublkIocR | ublkCtrlIoc | UBLK_CMD_GET_DEV_INFO2
	UBLK_U_CMD_GET_FEATURES        = ublkIocR | ublkCtrlIoc | UBLK_CMD_GET_FEATURES
	UBLK_U_CMD_DEL_DEV_ASYNC       = ublkIocR | ublkCtrlIoc | UBLK_CMD_DEL_DEV_ASYNC
	UBLK_U_CMD_UPDATE_SIZE         = ublkIocRW | ublkCtrlIoc | UBLK_CMD_UPDATE_SIZE
	UBLK_U_CMD_QUIESCE_DEV         = ublkIocRW | ublkCtrlIoc | UBLK_CMD_QUIESCE_DEV
	UBLK_U_CMD_TRY_STOP_DEV        = ublkIocRW | ublkCtrlIoc | UBLK_CMD_TRY_STOP_DEV
	UBLK_U_CMD_REG_BUF             = ublkIocRW | ublkCtrlIoc | UBLK_CMD_REG_BUF
	UBLK_U_CMD_UNREG_BUF           = ublkIocRW | ublkCtrlIoc | UBLK_CMD_UNREG_BUF
)

// Flags for struct ublk_shmem_buf_reg (UBLK_U_CMD_REG_BUF).
const (
	UBLK_SHMEM_BUF_READ_ONLY = 1 << 0 // pin without FOLL_WRITE; usable with write-sealed memfd
)

// I/O command numbers (raw/legacy forms of the first three I/O opcodes).
const (
	UBLK_IO_FETCH_REQ            = 0x20
	UBLK_IO_COMMIT_AND_FETCH_REQ = 0x21
	UBLK_IO_NEED_GET_DATA        = 0x22
	UBLK_IO_REGISTER_IO_BUF      = 0x23 // driver-private name, v6.15
	UBLK_IO_UNREGISTER_IO_BUF    = 0x24 // driver-private name, v6.15
	UBLK_IO_PREP_IO_CMDS         = 0x25 // no header name, v7.0
	UBLK_IO_COMMIT_IO_CMDS       = 0x26 // no header name, v7.0
	UBLK_IO_FETCH_IO_CMDS        = 0x27 // no header name, v7.0
)

// Ioctl-encoded I/O opcodes. The driver matches FETCH/COMMIT/NEED_GET_DATA/
// REGISTER/UNREGISTER by _IOC_NR on the per-I/O path, but the batch path
// (UBLK_F_BATCH_IO) compares full values, so always send these exact encodings.
const (
	UBLK_U_IO_FETCH_REQ            = ublkIocRW | ublkIOIoc | UBLK_IO_FETCH_REQ
	UBLK_U_IO_COMMIT_AND_FETCH_REQ = ublkIocRW | ublkIOIoc | UBLK_IO_COMMIT_AND_FETCH_REQ
	UBLK_U_IO_NEED_GET_DATA        = ublkIocRW | ublkIOIoc | UBLK_IO_NEED_GET_DATA
	UBLK_U_IO_REGISTER_IO_BUF      = ublkIocRW | ublkIOIoc | UBLK_IO_REGISTER_IO_BUF
	UBLK_U_IO_UNREGISTER_IO_BUF    = ublkIocRW | ublkIOIoc | UBLK_IO_UNREGISTER_IO_BUF
	UBLK_U_IO_PREP_IO_CMDS         = ublkIocRW | ublkBatchIoc | UBLK_IO_PREP_IO_CMDS
	UBLK_U_IO_COMMIT_IO_CMDS       = ublkIocRW | ublkBatchIoc | UBLK_IO_COMMIT_IO_CMDS
	UBLK_U_IO_FETCH_IO_CMDS        = ublkIocRW | ublkBatchIoc | UBLK_IO_FETCH_IO_CMDS
)

// I/O Result Codes
const (
	UBLK_IO_RES_OK            = 0
	UBLK_IO_RES_NEED_GET_DATA = 1
	UBLK_IO_RES_ABORT         = -19 // -ENODEV; only ABORT means "do not re-fetch"
)

// Feature flags (ublksrv_ctrl_dev_info.flags, 64-bit). The version is the
// first kernel whose UBLK_F_ALL accepts the flag.
const (
	UBLK_F_SUPPORT_ZERO_COPY      = 1 << 0  // v6.15 functional (accepted but stripped before)
	UBLK_F_URING_CMD_COMP_IN_TASK = 1 << 1  // v6.0; forced on and inert since v6.5
	UBLK_F_NEED_GET_DATA          = 1 << 2  // v6.0
	UBLK_F_USER_RECOVERY          = 1 << 3  // v6.1
	UBLK_F_USER_RECOVERY_REISSUE  = 1 << 4  // v6.1
	UBLK_F_UNPRIVILEGED_DEV       = 1 << 5  // v6.3
	UBLK_F_CMD_IOCTL_ENCODE       = 1 << 6  // v6.4; always reported, never consulted
	UBLK_F_USER_COPY              = 1 << 7  // v6.5
	UBLK_F_ZONED                  = 1 << 8  // v6.6
	UBLK_F_USER_RECOVERY_FAIL_IO  = 1 << 9  // v6.13
	UBLK_F_UPDATE_SIZE            = 1 << 10 // v6.16
	UBLK_F_AUTO_BUF_REG           = 1 << 11 // v6.16
	UBLK_F_QUIESCE                = 1 << 12 // v6.16
	UBLK_F_PER_IO_DAEMON          = 1 << 13 // v6.16; always reported (except with BATCH_IO)
	UBLK_F_BUF_REG_OFF_DAEMON     = 1 << 14 // v6.17; always reported
	UBLK_F_BATCH_IO               = 1 << 15 // v7.0
	UBLK_F_INTEGRITY              = 1 << 16 // v7.0; needs CONFIG_BLK_DEV_INTEGRITY
	UBLK_F_SAFE_STOP_DEV          = 1 << 17 // v7.0; always reported
	UBLK_F_NO_AUTO_PART_SCAN      = 1 << 18 // v7.0
	UBLK_F_SHMEM_ZC               = 1 << 19 // v7.1
	UBLK_F_IO_DESC_SIZE           = 1 << 20 // v7.3
)

// Device states (ublksrv_ctrl_dev_info.state)
const (
	UBLK_S_DEV_DEAD     = 0
	UBLK_S_DEV_LIVE     = 1
	UBLK_S_DEV_QUIESCED = 2
	UBLK_S_DEV_FAIL_IO  = 3 // v6.13
)

// I/O Operations
const (
	UBLK_IO_OP_READ           = 0
	UBLK_IO_OP_WRITE          = 1
	UBLK_IO_OP_FLUSH          = 2
	UBLK_IO_OP_DISCARD        = 3
	UBLK_IO_OP_WRITE_SAME     = 4
	UBLK_IO_OP_WRITE_ZEROES   = 5
	UBLK_IO_OP_ZONE_OPEN      = 10
	UBLK_IO_OP_ZONE_CLOSE     = 11
	UBLK_IO_OP_ZONE_FINISH    = 12
	UBLK_IO_OP_ZONE_APPEND    = 13
	UBLK_IO_OP_ZONE_RESET_ALL = 14
	UBLK_IO_OP_ZONE_RESET     = 15
	UBLK_IO_OP_REPORT_ZONES   = 18
)

// I/O Flags (bits 8-31 of ublksrv_io_desc.op_flags)
const (
	UBLK_IO_F_FAILFAST_DEV       = 1 << 8
	UBLK_IO_F_FAILFAST_TRANSPORT = 1 << 9
	UBLK_IO_F_FAILFAST_DRIVER    = 1 << 10
	UBLK_IO_F_META               = 1 << 11
	UBLK_IO_F_FUA                = 1 << 13
	UBLK_IO_F_NOUNMAP            = 1 << 15
	UBLK_IO_F_SWAP               = 1 << 16
	UBLK_IO_F_NEED_REG_BUF       = 1 << 17 // v6.16, AUTO_BUF_REG fallback: register manually
	UBLK_IO_F_INTEGRITY          = 1 << 18 // v7.0, request carries an integrity buffer
	UBLK_IO_F_SHMEM_ZC           = 1 << 19 // v7.1, addr encodes a shmem buffer index+offset
)

// Auto buffer registration (UBLK_F_AUTO_BUF_REG) flags, carried in
// ublk_auto_buf_reg.flags.
const (
	UBLK_AUTO_BUF_REG_FALLBACK = 1 << 0
	UBLK_AUTO_BUF_REG_F_MASK   = UBLK_AUTO_BUF_REG_FALLBACK
)

// struct ublk_batch_io flags (UBLK_F_BATCH_IO).
const (
	UBLK_BATCH_F_HAS_ZONE_LBA          = 1 << 0
	UBLK_BATCH_F_HAS_BUF_ADDR          = 1 << 1
	UBLK_BATCH_F_AUTO_BUF_REG_FALLBACK = 1 << 2
)

// Limits and Constants
const (
	UBLK_MAX_QUEUE_DEPTH = 4096 // Max IOs per queue
	UBLK_MAX_NR_QUEUES   = 1 << UBLK_QID_BITS
	UBLK_FEATURES_LEN    = 8 // GET_FEATURES buffer length (bytes)

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

	// User-copy position bit selecting the request's integrity buffer instead
	// of its data buffer (UBLK_F_INTEGRITY, v7.0).
	UBLK_INTEGRITY_FLAG_OFF   = 62
	UBLKSRV_IO_INTEGRITY_FLAG = 1 << UBLK_INTEGRITY_FLAG_OFF

	// Minimum seg_boundary_mask+1 and max_segment_size (UBLK_PARAM_TYPE_SEGMENT).
	UBLK_MIN_SEGMENT_SIZE = 4096
)

// Shared-memory zero-copy address encoding (UBLK_IO_F_SHMEM_ZC): bits 0-31
// are the byte offset in the buffer, bits 32-47 the buffer index.
const (
	UBLK_SHMEM_ZC_OFF_MASK = 0xffffffff
	UBLK_SHMEM_ZC_IDX_OFF  = 32
	UBLK_SHMEM_ZC_IDX_MASK = 0xffff
)

// Device Attribute Flags
const (
	UBLK_ATTR_READ_ONLY      = 1 << 0
	UBLK_ATTR_ROTATIONAL     = 1 << 1
	UBLK_ATTR_VOLATILE_CACHE = 1 << 2
	UBLK_ATTR_FUA            = 1 << 3
)

// Parameter Type Flags (ublk_params.types). Unknown types are silently
// masked off by the kernel's SET_PARAMS (types &= UBLK_PARAM_TYPE_ALL).
const (
	UBLK_PARAM_TYPE_BASIC     = 1 << 0 // v6.0
	UBLK_PARAM_TYPE_DISCARD   = 1 << 1 // v6.0
	UBLK_PARAM_TYPE_DEVT      = 1 << 2 // v6.3, read-only
	UBLK_PARAM_TYPE_ZONED     = 1 << 3 // v6.6
	UBLK_PARAM_TYPE_DMA_ALIGN = 1 << 4 // v6.15
	UBLK_PARAM_TYPE_SEGMENT   = 1 << 5 // v6.15
	UBLK_PARAM_TYPE_INTEGRITY = 1 << 6 // v7.0, requires UBLK_F_INTEGRITY
)

// Logical block metadata protection-information constants from
// include/uapi/linux/fs.h, used by ublk_param_integrity.flags and .csum_type.
const (
	LBMD_PI_CAP_INTEGRITY     = 1 << 0
	LBMD_PI_CAP_REFTAG        = 1 << 1
	LBMD_PI_CSUM_NONE         = 0
	LBMD_PI_CSUM_IP           = 1
	LBMD_PI_CSUM_CRC16_T10DIF = 2
	LBMD_PI_CSUM_CRC64_NVME   = 4
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

// UblkCtrlCmd returns the header's ioctl encoding for control command number
// nr: _IOR for the read-only commands (GET_QUEUE_AFFINITY, GET_DEV_INFO,
// GET_PARAMS, GET_DEV_INFO2, GET_FEATURES, DEL_DEV_ASYNC), _IOWR otherwise.
// Unknown numbers get _IOWR, the header's rule for new commands.
func UblkCtrlCmd(nr uint32) uint32 {
	switch nr {
	case UBLK_CMD_GET_QUEUE_AFFINITY, UBLK_CMD_GET_DEV_INFO, UBLK_CMD_GET_PARAMS,
		UBLK_CMD_GET_DEV_INFO2, UBLK_CMD_GET_FEATURES, UBLK_CMD_DEL_DEV_ASYNC:
		return IoctlEncode(_IOC_READ, ublkIoctlType, nr, ublkCtrlCmdStructSize)
	}
	return IoctlEncode(_IOC_READ|_IOC_WRITE, ublkIoctlType, nr, ublkCtrlCmdStructSize)
}

// UblkIOCmd returns the _IOWR encoding of I/O command number nr, sized by
// struct ublk_batch_io for the batch commands and struct ublksrv_io_cmd
// otherwise (both are 16 bytes, so the values only differ in nr).
func UblkIOCmd(nr uint32) uint32 {
	size := uint32(ublkIOCmdStructSize)
	switch nr {
	case UBLK_IO_PREP_IO_CMDS, UBLK_IO_COMMIT_IO_CMDS, UBLK_IO_FETCH_IO_CMDS:
		size = ublkBatchIOStructSize
	}
	return IoctlEncode(_IOC_READ|_IOC_WRITE, ublkIoctlType, nr, size)
}
