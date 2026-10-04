package ctrl

import (
	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

type DeviceParams struct {
	Backend interfaces.Backend

	DeviceID         int32
	QueueDepth       int
	NumQueues        int
	LogicalBlockSize int
	MaxIOSize        int

	EnableZeroCopy     bool
	EnableUnprivileged bool
	EnableUserCopy     bool
	EnableZoned        bool
	EnableIoctlEncode  bool

	ReadOnly      bool
	Rotational    bool
	VolatileCache bool
	EnableFUA     bool

	DiscardAlignment   uint32
	DiscardGranularity uint32
	MaxDiscardSectors  uint32
	MaxDiscardSegments uint16

	DeviceName  string
	CPUAffinity []int

	// Size is the device size in bytes; 0 means Backend.Size().
	Size int64
	// CanDiscard and CanWriteZeroes say which range operations the server
	// handles; only those get advertised limits. DeviceParamsFor sets them
	// from the backend's optional interfaces.
	CanDiscard, CanWriteZeroes bool

	// Flags are additional UBLK_F_* features to request (recovery,
	// NEED_GET_DATA, NO_AUTO_PART_SCAN, QUIESCE, UPDATE_SIZE, ...).
	Flags uint64
	// UblksrvFlags is stored by the kernel and returned by GET_DEV_INFO.
	UblksrvFlags uint64

	// Optional geometry; zero means the default derived from LogicalBlockSize.
	PhysicalBlockSize   int
	IOMinSize           int
	IOOptSize           int
	ChunkSectors        uint32
	VirtBoundaryMask    uint64
	DMAAlignment        uint32 // sent as UBLK_PARAM_TYPE_DMA_ALIGN when nonzero
	SegmentBoundaryMask uint64 // the three segment limits are sent together
	MaxSegmentSize      uint32 // as UBLK_PARAM_TYPE_SEGMENT when any is nonzero
	MaxSegments         uint16

	// Zoned devices (EnableZoned): zone size in sectors (sent as
	// chunk_sectors) and the UBLK_PARAM_TYPE_ZONED limits.
	ZoneSectors          uint32
	MaxOpenZones         uint32
	MaxActiveZones       uint32
	MaxZoneAppendSectors uint32

	// Integrity, if set, is sent as UBLK_PARAM_TYPE_INTEGRITY with
	// UBLK_F_INTEGRITY (which needs USER_COPY).
	Integrity *uapi.UblkParamIntegrity
}

// DeviceSize is the device size in bytes.
func (p *DeviceParams) DeviceSize() int64 {
	if p.Size > 0 || p.Backend == nil {
		return p.Size
	}
	return p.Backend.Size()
}

func DefaultDeviceParams(backend interfaces.Backend) DeviceParams {
	return DeviceParams{
		Backend:          backend,
		DeviceID:         -1,
		QueueDepth:       128,
		NumQueues:        0,
		LogicalBlockSize: 512,
		MaxIOSize:        1 << 20,

		EnableZeroCopy:     false,
		EnableUnprivileged: false,
		EnableUserCopy:     false,
		EnableZoned:        false,
		EnableIoctlEncode:  false, // redundant: AddDev always sets UBLK_F_CMD_IOCTL_ENCODE

		ReadOnly:      false,
		Rotational:    false,
		VolatileCache: false,
		EnableFUA:     false,

		DiscardAlignment:   4096,
		DiscardGranularity: 4096,
		MaxDiscardSectors:  0xffffffff,
		MaxDiscardSegments: 1, // ublk_validate_params only accepts single-segment discard
	}
}

type DeviceInfo struct {
	ID         uint32
	State      uint32
	NumQueues  uint16
	QueueDepth uint16
	BlockSize  uint16
	MaxIOSize  uint32
	DevSectors uint64
	Features   uint64
	CharPath   string
	BlockPath  string
}

func (d *DeviceInfo) Size() int64 {
	// dev_sectors counts 512-byte sectors, not logical blocks.
	return int64(d.DevSectors) * uapi.SectorSize
}
