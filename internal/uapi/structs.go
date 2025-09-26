package uapi

import (
	"fmt"
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
//	  __u16 dev_path_len;  // for unprivileged mode only
//	  __u16 pad;           // reserved/padding
//	  __u32 reserved;      // must be zero
//	};
type UblksrvCtrlCmd struct {
	DevID      uint32 // device id (0xFFFFFFFF for new device)
	QueueID    uint16 // 0xFFFF for control ops
	Len        uint16 // data length for buffer at addr
	Addr       uint64 // userspace buffer address
	Data       uint64 // inline payload (single uint64)
	DevPathLen uint16 // for unprivileged mode
	Pad        uint16 // padding
	Reserved   uint32 // must be zero
}

// Compile-time size check - must be exactly 32 bytes to fit in SQE cmd area
var _ [32]byte = [unsafe.Sizeof(UblksrvCtrlCmd{})]byte{}

// UblksrvCtrlDevInfo contains device information
type UblksrvCtrlDevInfo struct {
	NrHwQueues    uint16 // number of hardware queues
	QueueDepth    uint16 // depth per queue
	State         uint16 // device state (UBLK_S_*)
	Pad0          uint16 // padding
	MaxIOBufBytes uint32 // max I/O buffer size
	DevID         uint32 // device ID
	UblksrvPID    int32  // server process ID
	Pad1          uint32 // padding
	Flags         uint64 // feature flags
	UblksrvFlags  uint64 // server-internal flags (invisible to driver)
	OwnerUID      uint32 // owner UID (set by kernel)
	OwnerGID      uint32 // owner GID (set by kernel)
	Reserved1     uint64 // reserved
	Reserved2     uint64 // reserved
}

// Compile-time size check - 64 bytes as per kernel 6.6+
var _ [64]byte = [unsafe.Sizeof(UblksrvCtrlDevInfo{})]byte{}

// UblksrvIODesc describes each I/O operation (stored in shared memory).
// Layout must match Linux's struct ublksrv_io_desc exactly (24 bytes).
type UblksrvIODesc struct {
	OpFlags     uint32 // op: bits 0-7, flags: bits 8-31
	NrSectors   uint32 // number of sectors (or nr_zones for REPORT_ZONES)
	StartSector uint64 // starting sector
	Addr        uint64 // buffer address in userspace
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
	// Union field - either buffer address or zone append LBA
	Addr uint64 // userspace buffer address (FETCH* commands)
	// OR
	// ZoneAppendLBA uint64 // for UBLK_IO_OP_ZONE_APPEND with UBLK_F_ZONED
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

// UblkParams contains all device parameters
type UblkParams struct {
	Len     uint32           // total length of parameters
	Types   uint32           // types of parameters included (UBLK_PARAM_TYPE_*)
	Basic   UblkParamBasic   // basic parameters
	Discard UblkParamDiscard // discard parameters
	Devt    UblkParamDevt    // device numbers (read-only)
	Zoned   UblkParamZoned   // zoned device parameters
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

// Device file paths
const (
	UBLK_CONTROL_DEV = "/dev/ublk-control"
)

// UblkDevicePath returns the path to the character device
func UblkDevicePath(devID uint32) string {
	return "/dev/ublkc" + fmt.Sprintf("%d", devID)
}

// UblkBlockDevicePath returns the path to the block device
func UblkBlockDevicePath(devID uint32) string {
	return "/dev/ublkb" + fmt.Sprintf("%d", devID)
}
