package uapi

import (
	"strconv"
	"unsafe"
)

// UblksrvCtrlCmd must match kernel struct exactly (32 bytes):
// This structure gets placed directly in the SQE cmd area (bytes 32-63)
//
//	struct ublksrv_ctrl_cmd {
//	  __u32 dev_id;        // device id (0xFFFFFFFF for new device)
//	  __u16 queue_id;      // 0xFFFF for control ops
//	  __u16 len;           // data length for buffer at addr
//	  __u64 addr;          // userspace buffer address (IN/OUT depending on op)
//	  __u64 data[1];       // inline payload (op-specific)
//	  __u16 dev_path_len;  // unprivileged devices and GET_DEV_INFO2 only
//	  __u16 pad;           // reserved/padding
//	  __u32 reserved;      // must be zero
//	};
type UblksrvCtrlCmd struct {
	DevID      uint32 // device id (0xFFFFFFFF for new device)
	QueueID    uint16 // 0xFFFF for control ops
	Len        uint16 // data length for buffer at addr
	Addr       uint64 // userspace buffer address
	Data       uint64 // inline payload (single uint64)
	DevPathLen uint16 // length of the char-device path prefixed at addr, NUL included
	Pad        uint16 // padding
	Reserved   uint32 // must be zero
}

// Compile-time size check - must be exactly 32 bytes to fit in SQE cmd area
var _ [32]byte = [unsafe.Sizeof(UblksrvCtrlCmd{})]byte{}

// UblksrvCtrlDevInfo is struct ublksrv_ctrl_dev_info (64 bytes since v6.0):
// ADD_DEV's input and output, and GET_DEV_INFO/GET_DEV_INFO2's output.
type UblksrvCtrlDevInfo struct {
	NrHwQueues    uint16 // number of hardware queues
	QueueDepth    uint16 // depth per queue
	State         uint16 // device state (UBLK_S_*)
	IODescSize    uint16 // ublksrv_io_desc stride; was pad0 before v7.3 (UBLK_F_IO_DESC_SIZE)
	MaxIOBufBytes uint32 // max I/O buffer size
	DevID         uint32 // device ID
	UblksrvPID    int32  // server process ID
	Pad1          uint32 // padding
	Flags         uint64 // feature flags
	UblksrvFlags  uint64 // server-private; the kernel stores and returns it untouched
	OwnerUID      uint32 // owner UID (set by kernel)
	OwnerGID      uint32 // owner GID (set by kernel)
	Reserved1     uint64 // reserved
	Reserved2     uint64 // reserved
}

// Compile-time size check - 64 bytes since v6.0
var _ [64]byte = [unsafe.Sizeof(UblksrvCtrlDevInfo{})]byte{}

// UblksrvIODesc describes each I/O operation (stored in shared memory).
// Layout must match Linux's struct ublksrv_io_desc exactly (24 bytes).
type UblksrvIODesc struct {
	OpFlags     uint32 // op: bits 0-7, flags: bits 8-31
	NrSectors   uint32 // number of sectors (or nr_zones for REPORT_ZONES)
	StartSector uint64 // starting sector
	Addr        uint64 // buffer address in userspace (or ShmemZCAddr with UBLK_IO_F_SHMEM_ZC)
}

// Compile-time size check - kernel struct is 24 bytes.
var _ [24]byte = [unsafe.Sizeof(UblksrvIODesc{})]byte{}

// GetOp extracts the operation code from OpFlags
func (d *UblksrvIODesc) GetOp() uint8 {
	return uint8(d.OpFlags & 0xff)
}

// GetFlags extracts the flags from OpFlags
func (d *UblksrvIODesc) GetFlags() uint32 {
	return d.OpFlags >> 8
}

// UblksrvIOCmd is issued to ublk driver via /dev/ublkcN
type UblksrvIOCmd struct {
	QID    uint16 // queue ID
	Tag    uint16 // request tag
	Result int32  // I/O result (valid for COMMIT* commands only)
	// Union field - either buffer address or zone append LBA. With
	// UBLK_F_AUTO_BUF_REG the SQE's addr (not this field) carries the
	// AutoBufReg encoding; REGISTER/UNREGISTER_IO_BUF put the buffer index here.
	Addr uint64
}

// Compile-time size check
var _ [16]byte = [unsafe.Sizeof(UblksrvIOCmd{})]byte{}

// SetZoneAppendLBA sets the zone append LBA (reuses Addr field)
func (c *UblksrvIOCmd) SetZoneAppendLBA(lba uint64) {
	c.Addr = lba
}

// GetZoneAppendLBA gets the zone append LBA (from Addr field)
func (c *UblksrvIOCmd) GetZoneAppendLBA() uint64 {
	return c.Addr
}

// UblkElemHeader is struct ublk_elem_header, the mandatory 8-byte head of each
// element in a UBLK_F_BATCH_IO command buffer (v7.0).
type UblkElemHeader struct {
	Tag      uint16 // I/O tag
	BufIndex uint16 // buffer index, only with UBLK_F_AUTO_BUF_REG
	Result   int32  // completion result (commit only)
}

var _ [8]byte = [unsafe.Sizeof(UblkElemHeader{})]byte{}

// UblkBatchIO is struct ublk_batch_io, the uring_cmd payload of the batch
// I/O commands (UBLK_U_IO_PREP_IO_CMDS/COMMIT_IO_CMDS/FETCH_IO_CMDS, v7.0).
// Each element is a UblkElemHeader followed by an optional 8-byte buffer
// address (UBLK_BATCH_F_HAS_BUF_ADDR) and zone LBA (UBLK_BATCH_F_HAS_ZONE_LBA);
// ElemBytes is the resulting per-element size.
type UblkBatchIO struct {
	QID       uint16
	Flags     uint16 // UBLK_BATCH_F_*
	NrElem    uint16
	ElemBytes uint8
	Reserved  uint8
	Reserved2 uint64
}

var _ [16]byte = [unsafe.Sizeof(UblkBatchIO{})]byte{}

// BatchElemBytes returns the per-element size implied by batch flags: the
// 8-byte header plus 8 bytes for each of HAS_BUF_ADDR and HAS_ZONE_LBA.
func BatchElemBytes(flags uint16) uint8 {
	n := uint8(unsafe.Sizeof(UblkElemHeader{}))
	if flags&UBLK_BATCH_F_HAS_BUF_ADDR != 0 {
		n += 8
	}
	if flags&UBLK_BATCH_F_HAS_ZONE_LBA != 0 {
		n += 8
	}
	return n
}

// UblkAutoBufReg is struct ublk_auto_buf_reg (UBLK_F_AUTO_BUF_REG, v6.16). It
// is not sent as memory: it travels packed in the I/O SQE's addr field.
type UblkAutoBufReg struct {
	Index     uint16 // registered-buffer table slot for the request buffer
	Flags     uint8  // UBLK_AUTO_BUF_REG_*
	Reserved0 uint8  // must be zero
	Reserved1 uint32 // must be zero (future: external io_uring fd)
}

var _ [8]byte = [unsafe.Sizeof(UblkAutoBufReg{})]byte{}

// SQEAddr packs r the way ublk_auto_buf_reg_to_sqe_addr() does: index in bits
// 0-15, flags 16-23, reserved0 24-31, reserved1 32-63.
func (r UblkAutoBufReg) SQEAddr() uint64 {
	return uint64(r.Index) | uint64(r.Flags)<<16 | uint64(r.Reserved0)<<24 | uint64(r.Reserved1)<<32
}

// AutoBufRegFromSQEAddr is ublk_sqe_addr_to_auto_buf_reg().
func AutoBufRegFromSQEAddr(addr uint64) UblkAutoBufReg {
	return UblkAutoBufReg{
		Index:     uint16(addr),
		Flags:     uint8(addr >> 16),
		Reserved0: uint8(addr >> 24),
		Reserved1: uint32(addr >> 32),
	}
}

// UblkShmemBufReg is struct ublk_shmem_buf_reg, the buffer UBLK_U_CMD_REG_BUF
// reads from ctrl_cmd.addr (UBLK_F_SHMEM_ZC, v7.1).
type UblkShmemBufReg struct {
	Addr     uint64 // page-aligned virtual address of the shared memory
	Len      uint64 // page-aligned length, at most 4 GiB
	Flags    uint32 // UBLK_SHMEM_BUF_*
	Reserved uint32 // must be zero
}

var _ [24]byte = [unsafe.Sizeof(UblkShmemBufReg{})]byte{}

// ShmemZCAddr is ublk_shmem_zc_addr(): the ublksrv_io_desc.addr encoding of a
// request whose pages lie in registered shared-memory buffer index.
func ShmemZCAddr(index uint16, offset uint32) uint64 {
	return uint64(index)<<UBLK_SHMEM_ZC_IDX_OFF | uint64(offset)
}

// ShmemZCIndex is ublk_shmem_zc_index().
func ShmemZCIndex(addr uint64) uint16 {
	return uint16((addr >> UBLK_SHMEM_ZC_IDX_OFF) & UBLK_SHMEM_ZC_IDX_MASK)
}

// ShmemZCOffset is ublk_shmem_zc_offset().
func ShmemZCOffset(addr uint64) uint32 {
	return uint32(addr & UBLK_SHMEM_ZC_OFF_MASK)
}

// UserCopyOffset returns the pread/pwrite position on /dev/ublkcN that
// addresses byte offset of the buffer of request (qid, tag) under
// UBLK_F_USER_COPY. ok is false if a field does not fit its bit width.
func UserCopyOffset(qid, tag uint16, offset uint32) (pos int64, ok bool) {
	if uint64(qid) > UBLK_QID_BITS_MASK || uint64(offset) > UBLK_IO_BUF_BITS_MASK {
		return 0, false
	}
	return UBLKSRV_IO_BUF_OFFSET + int64(qid)<<UBLK_QID_OFF + int64(tag)<<UBLK_TAG_OFF + int64(offset), true
}

// UserCopyIntegrityOffset is UserCopyOffset for the request's integrity
// (metadata) buffer: the same position with UBLKSRV_IO_INTEGRITY_FLAG set.
// The kernel fails it with EINVAL unless the device has UBLK_F_INTEGRITY.
func UserCopyIntegrityOffset(qid, tag uint16, offset uint32) (pos int64, ok bool) {
	if pos, ok = UserCopyOffset(qid, tag, offset); !ok {
		return 0, false
	}
	return pos | UBLKSRV_IO_INTEGRITY_FLAG, true
}

// DecodeUserCopyOffset splits a user-copy position the way the driver does
// (ublk_pos_to_hwq/tag/buf_off plus the integrity bit).
func DecodeUserCopyOffset(pos int64) (qid, tag uint16, offset uint32, integrity bool) {
	integrity = pos&UBLKSRV_IO_INTEGRITY_FLAG != 0
	rel := uint64(pos) - UBLKSRV_IO_BUF_OFFSET
	return uint16((rel >> UBLK_QID_OFF) & UBLK_QID_BITS_MASK),
		uint16((rel >> UBLK_TAG_OFF) & UBLK_TAG_BITS_MASK),
		uint32(rel & UBLK_IO_BUF_BITS_MASK), integrity
}

// CmdBufStride is the distance between per-queue descriptor arrays in the
// /dev/ublkcN mmap space: round_up(UBLK_MAX_QUEUE_DEPTH * ioDescSize,
// pageSize), keyed on the maximum depth, not the device's (ublk_ch_mmap).
// ioDescSize is dev_info.io_desc_size, or 24 before UBLK_F_IO_DESC_SIZE.
func CmdBufStride(ioDescSize uint16, pageSize int) int64 {
	return roundUp(int64(UBLK_MAX_QUEUE_DEPTH)*int64(ioDescSize), int64(pageSize))
}

// CmdBufMmapOffset is the mmap offset of queue qid's descriptor array.
func CmdBufMmapOffset(qid uint16, ioDescSize uint16, pageSize int) int64 {
	return UBLKSRV_CMD_BUF_OFFSET + int64(qid)*CmdBufStride(ioDescSize, pageSize)
}

// CmdBufSize is the exact mmap length the driver accepts for one queue:
// round_up(depth * ioDescSize, pageSize).
func CmdBufSize(depth, ioDescSize uint16, pageSize int) int64 {
	return roundUp(int64(depth)*int64(ioDescSize), int64(pageSize))
}

func roundUp(n, to int64) int64 {
	return (n + to - 1) / to * to
}

// UblkParamBasic contains basic device parameters
type UblkParamBasic struct {
	Attrs            uint32 // attribute flags (UBLK_ATTR_*)
	LogicalBSShift   uint8  // logical block size shift
	PhysicalBSShift  uint8  // physical block size shift
	IOOptShift       uint8  // optimal I/O size shift
	IOMinShift       uint8  // minimum I/O size shift
	MaxSectors       uint32 // max sectors per request
	ChunkSectors     uint32 // chunk size in sectors
	DevSectors       uint64 // device size in sectors
	VirtBoundaryMask uint64 // virtual boundary mask
}

// UblkParamDiscard contains discard-related parameters
type UblkParamDiscard struct {
	DiscardAlignment      uint32 // discard alignment
	DiscardGranularity    uint32 // discard granularity
	MaxDiscardSectors     uint32 // max discard sectors
	MaxWriteZeroesSectors uint32 // max write zeroes sectors
	MaxDiscardSegments    uint16 // max discard segments
	Reserved0             uint16 // reserved
}

// UblkParamDevt contains device numbers (read-only)
type UblkParamDevt struct {
	CharMajor uint32 // character device major
	CharMinor uint32 // character device minor
	DiskMajor uint32 // disk device major
	DiskMinor uint32 // disk device minor
}

// UblkParamZoned contains zoned device parameters
type UblkParamZoned struct {
	MaxOpenZones         uint32    // max open zones
	MaxActiveZones       uint32    // max active zones
	MaxZoneAppendSectors uint32    // max zone append sectors
	Reserved             [20]uint8 // reserved for future use
}

// UblkParamDMAAlign is struct ublk_param_dma_align (v6.15). Alignment is a
// mask: alignment+1 must be a power of two below PAGE_SIZE.
type UblkParamDMAAlign struct {
	Alignment uint32
	Pad       [4]uint8
}

// UblkParamSegment is struct ublk_param_segment (v6.15). seg_boundary_mask+1
// must be a power of two >= UBLK_MIN_SEGMENT_SIZE, max_segment_size >=
// UBLK_MIN_SEGMENT_SIZE; a zero in any field is undefined behavior.
type UblkParamSegment struct {
	SegBoundaryMask uint64
	MaxSegmentSize  uint32
	MaxSegments     uint16
	Pad             [2]uint8
}

// UblkParamIntegrity is struct ublk_param_integrity (v7.0), valid only on a
// device created with UBLK_F_INTEGRITY.
type UblkParamIntegrity struct {
	Flags                uint32 // LBMD_PI_CAP_*
	MaxIntegritySegments uint16 // 0 means no limit
	IntervalExp          uint8  // log2 protection interval, 9..logical_bs_shift
	MetadataSize         uint8  // per-interval metadata bytes, must be nonzero
	PIOffset             uint8  // offset of the PI tuple within the metadata
	CsumType             uint8  // LBMD_PI_CSUM_*
	TagSize              uint8
	Pad                  [5]uint8
}

var (
	_ [32]byte = [unsafe.Sizeof(UblkParamBasic{})]byte{}
	_ [20]byte = [unsafe.Sizeof(UblkParamDiscard{})]byte{}
	_ [16]byte = [unsafe.Sizeof(UblkParamDevt{})]byte{}
	_ [32]byte = [unsafe.Sizeof(UblkParamZoned{})]byte{}
	_ [8]byte  = [unsafe.Sizeof(UblkParamDMAAlign{})]byte{}
	_ [16]byte = [unsafe.Sizeof(UblkParamSegment{})]byte{}
	_ [16]byte = [unsafe.Sizeof(UblkParamIntegrity{})]byte{}
)

// UblkParams contains all device parameters. On the wire each block sits at
// its fixed C offset (see marshal.go); this Go struct's own layout matches C
// on 64-bit targets only and is never copied as a whole.
type UblkParams struct {
	Len       uint32             // total length of parameters
	Types     uint32             // types of parameters included (UBLK_PARAM_TYPE_*)
	Basic     UblkParamBasic     // basic parameters
	Discard   UblkParamDiscard   // discard parameters
	Devt      UblkParamDevt      // device numbers (read-only)
	Zoned     UblkParamZoned     // zoned device parameters
	DMA       UblkParamDMAAlign  // DMA alignment
	Seg       UblkParamSegment   // segment limits
	Integrity UblkParamIntegrity // integrity / protection information
}

// Helper methods for UblkParams

// HasBasic returns true if basic parameters are included
func (p *UblkParams) HasBasic() bool {
	return (p.Types & UBLK_PARAM_TYPE_BASIC) != 0
}

// HasDiscard returns true if discard parameters are included
func (p *UblkParams) HasDiscard() bool {
	return (p.Types & UBLK_PARAM_TYPE_DISCARD) != 0
}

// HasDevt returns true if device number parameters are included
func (p *UblkParams) HasDevt() bool {
	return (p.Types & UBLK_PARAM_TYPE_DEVT) != 0
}

// HasZoned returns true if zoned parameters are included
func (p *UblkParams) HasZoned() bool {
	return (p.Types & UBLK_PARAM_TYPE_ZONED) != 0
}

// HasDMAAlign returns true if DMA alignment parameters are included
func (p *UblkParams) HasDMAAlign() bool {
	return (p.Types & UBLK_PARAM_TYPE_DMA_ALIGN) != 0
}

// HasSegment returns true if segment parameters are included
func (p *UblkParams) HasSegment() bool {
	return (p.Types & UBLK_PARAM_TYPE_SEGMENT) != 0
}

// HasIntegrity returns true if integrity parameters are included
func (p *UblkParams) HasIntegrity() bool {
	return (p.Types & UBLK_PARAM_TYPE_INTEGRITY) != 0
}

// SetBasic enables basic parameters
func (p *UblkParams) SetBasic() {
	p.Types |= UBLK_PARAM_TYPE_BASIC
}

// SetDiscard enables discard parameters
func (p *UblkParams) SetDiscard() {
	p.Types |= UBLK_PARAM_TYPE_DISCARD
}

// SetDevt enables device number parameters (usually set by kernel)
func (p *UblkParams) SetDevt() {
	p.Types |= UBLK_PARAM_TYPE_DEVT
}

// SetZoned enables zoned parameters
func (p *UblkParams) SetZoned() {
	p.Types |= UBLK_PARAM_TYPE_ZONED
}

// SetDMAAlign enables DMA alignment parameters
func (p *UblkParams) SetDMAAlign() {
	p.Types |= UBLK_PARAM_TYPE_DMA_ALIGN
}

// SetSegment enables segment parameters
func (p *UblkParams) SetSegment() {
	p.Types |= UBLK_PARAM_TYPE_SEGMENT
}

// SetIntegrity enables integrity parameters
func (p *UblkParams) SetIntegrity() {
	p.Types |= UBLK_PARAM_TYPE_INTEGRITY
}

// Device file paths
const (
	UBLK_CONTROL_DEV = "/dev/ublk-control"
)

// UblkDevicePath returns the path to the character device
func UblkDevicePath(devID uint32) string {
	return "/dev/ublkc" + strconv.FormatUint(uint64(devID), 10)
}

// UblkBlockDevicePath returns the path to the block device
func UblkBlockDevicePath(devID uint32) string {
	return "/dev/ublkb" + strconv.FormatUint(uint64(devID), 10)
}
