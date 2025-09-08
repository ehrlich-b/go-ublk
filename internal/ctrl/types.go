package ctrl

import "github.com/ehrlich-b/go-ublk/internal/interfaces"

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

	ReadOnly        bool
	Rotational      bool
	VolatileCache   bool
	EnableFUA       bool

	DiscardAlignment    uint32
	DiscardGranularity  uint32
	MaxDiscardSectors   uint32
	MaxDiscardSegments  uint16

	DeviceName  string
	CPUAffinity []int
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
		EnableIoctlEncode:  false, // Disable ioctl mode, use URING_CMD

		ReadOnly:      false,
		Rotational:    false,
		VolatileCache: false,
		EnableFUA:     false,

		DiscardAlignment:   4096,
		DiscardGranularity: 4096,
		MaxDiscardSectors:  0xffffffff,
		MaxDiscardSegments: 256,
	}
}

type DeviceInfo struct {
	ID           uint32
	State        uint32
	NumQueues    uint16
	QueueDepth   uint16
	BlockSize    uint16
	MaxIOSize    uint32
	DevSectors   uint64
	Features     uint64
	CharPath     string
	BlockPath    string
}

func (d *DeviceInfo) Size() int64 {
	return int64(d.DevSectors) * int64(d.BlockSize)
}