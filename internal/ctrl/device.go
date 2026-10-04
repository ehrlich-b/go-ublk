package ctrl

import (
	"context"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// This file maps the library's DeviceParams onto the typed commands.

// AddDevice is AddDev for a DeviceParams.
func (c *Controller) AddDevice(ctx context.Context, params *DeviceParams) (*uapi.UblksrvCtrlDevInfo, error) {
	numQueues := params.NumQueues
	if numQueues <= 0 {
		numQueues = 1
	}
	return c.AddDev(ctx, AddDevOptions{
		DevID:         uint32(params.DeviceID),
		NrHwQueues:    uint16(numQueues),
		QueueDepth:    uint16(params.QueueDepth),
		MaxIOBufBytes: uint32(params.MaxIOSize),
		Flags:         c.buildFeatureFlags(params),
		UblksrvFlags:  params.UblksrvFlags,
	})
}

// SetDeviceParams is SetParams with the parameters a DeviceParams implies.
func (c *Controller) SetDeviceParams(ctx context.Context, id uint32, params *DeviceParams) error {
	p := deviceUblkParams(params)
	if p.HasDiscard() && params.MaxDiscardSegments != 1 && p.Discard.MaxDiscardSectors != 0 {
		c.logger.Warn("clamping MaxDiscardSegments to 1: ublk only supports single-segment discard",
			"requested", params.MaxDiscardSegments)
	}
	c.logger.Debug("setting device parameters",
		"logical_bs_shift", p.Basic.LogicalBSShift,
		"max_sectors", p.Basic.MaxSectors,
		"dev_sectors", p.Basic.DevSectors,
		"types", ParamTypeNames(p.Types))
	return c.SetParams(ctx, id, p)
}

// deviceUblkParams builds the SET_PARAMS payload for a DeviceParams.
func deviceUblkParams(params *DeviceParams) *uapi.UblkParams {
	orLBS := func(v int) int {
		if v > 0 {
			return v
		}
		return params.LogicalBlockSize
	}
	p := &uapi.UblkParams{
		Types: uapi.UBLK_PARAM_TYPE_BASIC,
		Basic: uapi.UblkParamBasic{
			Attrs:           basicAttrs(params),
			LogicalBSShift:  uint8(sizeToShift(params.LogicalBlockSize)),
			PhysicalBSShift: uint8(sizeToShift(orLBS(params.PhysicalBlockSize))),
			IOMinShift:      uint8(sizeToShift(orLBS(params.IOMinSize))),
			IOOptShift:      uint8(sizeToShift(max(params.IOOptSize, 1))),
			// Both of these count 512-byte sectors, NOT logical blocks: the
			// kernel checks max_sectors against max_io_buf_bytes >> 9 and
			// derives capacity from dev_sectors << 9.
			MaxSectors:       uint32(params.MaxIOSize / uapi.SectorSize),
			ChunkSectors:     params.ChunkSectors,
			DevSectors:       uint64(params.DeviceSize() / uapi.SectorSize),
			VirtBoundaryMask: params.VirtBoundaryMask,
		},
	}
	if params.EnableZoned {
		p.Types |= uapi.UBLK_PARAM_TYPE_ZONED
		p.Basic.ChunkSectors = params.ZoneSectors
		p.Zoned.MaxOpenZones = params.MaxOpenZones
		p.Zoned.MaxActiveZones = params.MaxActiveZones
		p.Zoned.MaxZoneAppendSectors = params.MaxZoneAppendSectors
	}
	if params.Integrity != nil {
		p.Types |= uapi.UBLK_PARAM_TYPE_INTEGRITY
		p.Integrity = *params.Integrity
	}
	if params.DMAAlignment != 0 {
		p.Types |= uapi.UBLK_PARAM_TYPE_DMA_ALIGN
		p.DMA.Alignment = params.DMAAlignment
	}
	if params.SegmentBoundaryMask != 0 || params.MaxSegmentSize != 0 || params.MaxSegments != 0 {
		p.Types |= uapi.UBLK_PARAM_TYPE_SEGMENT
		p.Seg.SegBoundaryMask = params.SegmentBoundaryMask
		p.Seg.MaxSegmentSize = params.MaxSegmentSize
		p.Seg.MaxSegments = params.MaxSegments
	}
	// Limits are only advertised for operations the backend can actually
	// service, since advertising one we drop invites silent data loss.
	// MaxDiscardSectors == 0 means the caller opted out of both.
	if discard, ok := discardParams(params); ok {
		p.Types |= uapi.UBLK_PARAM_TYPE_DISCARD
		p.Discard = discard
	}
	return p
}

// basicAttrs maps the caller-visible device attributes onto UBLK_ATTR_* bits.
// The kernel only honors what we actually send here: read-only, for example, is
// applied by ublk_dev_param_basic_apply -> set_disk_ro(), so dropping the bit
// silently hands the caller a writable device.
//
// EnableFUA is only set by the public layer when the server honors the
// per-I/O UBLK_IO_F_FUA flag (a FUABackend, or a Handler that says so), and
// only takes effect with a volatile cache. Without it the block layer emulates
// FUA as write + post-flush, which is always safe.
func basicAttrs(params *DeviceParams) uint32 {
	var attrs uint32
	if params.ReadOnly {
		attrs |= uapi.UBLK_ATTR_READ_ONLY
	}
	if params.Rotational {
		attrs |= uapi.UBLK_ATTR_ROTATIONAL
	}
	if params.VolatileCache {
		attrs |= uapi.UBLK_ATTR_VOLATILE_CACHE
		if params.EnableFUA {
			attrs |= uapi.UBLK_ATTR_FUA
		}
	}
	return attrs
}

// discardParams builds the discard limits to advertise, and reports whether
// they should be sent at all. Discard and write-zeroes share one param block
// but are separate capabilities, and each is advertised only if the backend
// can service it — the runner dispatches them through these two interfaces.
// MaxDiscardSectors doubles as the write-zeroes limit; nothing has yet needed
// them to differ.
func discardParams(params *DeviceParams) (uapi.UblkParamDiscard, bool) {
	canDiscard, canWriteZeroes := params.CanDiscard, params.CanWriteZeroes
	if params.Backend != nil {
		_, d := params.Backend.(interfaces.DiscardBackend)
		_, z := params.Backend.(interfaces.WriteZeroesBackend)
		canDiscard, canWriteZeroes = canDiscard || d, canWriteZeroes || z
	}
	if params.MaxDiscardSectors == 0 || (!canDiscard && !canWriteZeroes) {
		return uapi.UblkParamDiscard{}, false
	}

	// ublk_validate_params() rejects the whole SET_PARAMS with -EINVAL unless
	// max_discard_segments is exactly 1 ("So far, only support single segment
	// discard") and discard_granularity is non-zero. Neither is expressible any
	// other way, so normalize rather than let a caller's value fail device
	// creation outright. Granularity is required even when only write-zeroes is
	// advertised.
	granularity := params.DiscardGranularity
	if granularity == 0 {
		granularity = uint32(params.LogicalBlockSize)
	}

	discard := uapi.UblkParamDiscard{
		DiscardAlignment:   params.DiscardAlignment,
		DiscardGranularity: granularity,
	}
	if canDiscard {
		discard.MaxDiscardSectors = params.MaxDiscardSectors
		discard.MaxDiscardSegments = 1
	}
	if canWriteZeroes {
		discard.MaxWriteZeroesSectors = params.MaxDiscardSectors
	}
	return discard, true
}

// buildFeatureFlags maps the DeviceParams feature switches onto UBLK_F_*.
// AddDev adds UBLK_F_CMD_IOCTL_ENCODE itself; URING_CMD_COMP_IN_TASK is no
// longer requested because every supported kernel forces it on and ignores it.
func (c *Controller) buildFeatureFlags(params *DeviceParams) uint64 {
	var flags uint64

	if params.EnableZeroCopy {
		flags |= uapi.UBLK_F_SUPPORT_ZERO_COPY
	}

	if params.EnableUnprivileged {
		flags |= uapi.UBLK_F_UNPRIVILEGED_DEV
	}

	if params.EnableUserCopy {
		flags |= uapi.UBLK_F_USER_COPY
	}

	if params.EnableIoctlEncode {
		flags |= uapi.UBLK_F_CMD_IOCTL_ENCODE
	}

	if params.EnableZoned {
		flags |= uapi.UBLK_F_ZONED
	}

	if params.Integrity != nil {
		flags |= uapi.UBLK_F_INTEGRITY
	}

	return flags | params.Flags
}

// sizeToShift converts a size to its shift value (log2)
func sizeToShift(size int) int {
	shift := 0
	for s := size; s > 1; s >>= 1 {
		shift++
	}
	return shift
}
