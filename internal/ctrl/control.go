package ctrl

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

const (
	UblkControlPath = "/dev/ublk-control"
)

type Controller struct {
	controlFd int
	ring      uring.Ring
	logger    *logging.Logger
}

func NewController() (*Controller, error) {
	fd, err := syscall.Open(UblkControlPath, syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %v", UblkControlPath, err)
	}

	config := uring.Config{
		Entries: 32,
		FD:      int32(fd),
		Flags:   0,
	}

	ring, err := uring.NewRing(config)
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to create io_uring: %v", err)
	}

	return &Controller{
		controlFd: fd,
		ring:      ring,
		logger:    logging.Default(),
	}, nil
}

func (c *Controller) Close() error {
	if c.ring != nil {
		c.ring.Close()
	}
	if c.controlFd >= 0 {
		return syscall.Close(c.controlFd)
	}
	return nil
}

func (c *Controller) AddDevice(params *DeviceParams) (uint32, error) {
	// Auto-detect number of queues if not specified
	numQueues := params.NumQueues
	if numQueues <= 0 {
		numQueues = 1 // Start with 1 queue for simplicity
	}

	// Create and populate device info structure
	devInfo := &uapi.UblksrvCtrlDevInfo{
		NrHwQueues:    uint16(numQueues),
		QueueDepth:    uint16(params.QueueDepth),
		State:         0, // UBLK_S_DEV_INIT
		MaxIOBufBytes: uint32(params.MaxIOSize),
		DevID:         uint32(params.DeviceID),
		UblksrvPID:    int32(os.Getpid()),
		// Negotiate features up front
		Flags:        c.buildFeatureFlags(params),
		UblksrvFlags: 0,
		OwnerUID:     uint32(os.Getuid()),
		OwnerGID:     uint32(os.Getgid()),
	}

	c.logger.Debug("submitting ADD_DEV",
		"queues", devInfo.NrHwQueues,
		"depth", devInfo.QueueDepth,
		"max_io", devInfo.MaxIOBufBytes,
		"flags", fmt.Sprintf("0x%x", devInfo.UblksrvFlags),
		"dev_id", devInfo.DevID)

	// Marshal device info (64-byte format matches kernel 6.6+)
	deviceInfoBytes := uapi.Marshal(devInfo)

	// Build control header (48-byte variant)
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      devInfo.DevID,
		QueueID:    0xFFFF,
		Len:        uint16(len(deviceInfoBytes)),
		Addr:       uint64(uintptr(unsafe.Pointer(&deviceInfoBytes[0]))),
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}

	c.logger.Debug("submitting control command",
		"dev_id", cmd.DevID,
		"queue_id", cmd.QueueID,
		"len", cmd.Len,
		"addr", fmt.Sprintf("0x%x", cmd.Addr))

	c.logger.Debug("device info buffer", "size", len(deviceInfoBytes), "data", fmt.Sprintf("%x", deviceInfoBytes))

	// Use ioctl encoding - required by modern kernels (6.11+)
	op := uapi.UblkCtrlCmd(uapi.UBLK_CMD_ADD_DEV)
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return 0, fmt.Errorf("ADD_DEV submit failed: %v", err)
	}

	c.logger.Info("ADD_DEV completed", "result", result.Value())

	if result.Value() < 0 {
		return 0, fmt.Errorf("ADD_DEV failed with error: %d", result.Value())
	}

	// Ensure device info buffer stays alive until after kernel copies it
	runtime.KeepAlive(deviceInfoBytes)

	info := uapi.UnmarshalCtrlDevInfo(deviceInfoBytes)
	c.logger.Info("device created", "dev_id", info.DevID)
	return info.DevID, nil
}

// basicAttrs maps the caller-visible device attributes onto UBLK_ATTR_* bits.
// The kernel only honors what we actually send here: read-only, for example, is
// applied by ublk_dev_param_basic_apply -> set_disk_ro(), so dropping the bit
// silently hands the caller a writable device.
//
// EnableFUA is deliberately NOT advertised. Nothing consumes the per-IO
// UBLK_IO_F_FUA flag yet, and claiming FUA support we do not honor would turn a
// power cut into silent corruption. Advertising a volatile cache without FUA is
// safe: the block layer then emulates FUA as write + post-flush, so a caller
// asking for FUA still gets those semantics, just via a flush we do implement.
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
	}
	return attrs
}

// discardParams builds the discard limits to advertise, and reports whether
// they should be sent at all. See the call site in SetParams for why this is
// gated on the backend implementing DiscardBackend.
func discardParams(params *DeviceParams) (uapi.UblkParamDiscard, bool) {
	// Discard and write-zeroes share one param block but are separate
	// capabilities, and each is advertised only if the backend can service it —
	// the runner dispatches them through these two interfaces. MaxDiscardSectors
	// doubles as the write-zeroes limit; nothing has yet needed them to differ.
	_, canDiscard := params.Backend.(interfaces.DiscardBackend)
	_, canWriteZeroes := params.Backend.(interfaces.WriteZeroesBackend)
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

func (c *Controller) SetParams(deviceID uint32, params *DeviceParams) error {
	c.logger.Debug("setting device parameters",
		"logical_bs", params.LogicalBlockSize,
		"max_io", params.MaxIOSize,
		"backend_size", params.Backend.Size())

	if params.EnableFUA {
		c.logger.Warn("EnableFUA requested but not advertised: per-IO FUA is not implemented; " +
			"set VolatileCache to get FUA semantics via block-layer post-flush emulation")
	}

	ublkParams := &uapi.UblkParams{
		Types: uapi.UBLK_PARAM_TYPE_BASIC,
		Basic: uapi.UblkParamBasic{
			Attrs:           basicAttrs(params),
			LogicalBSShift:  uint8(sizeToShift(params.LogicalBlockSize)),
			PhysicalBSShift: uint8(sizeToShift(params.LogicalBlockSize)),
			IOOptShift:      0,
			IOMinShift:      uint8(sizeToShift(params.LogicalBlockSize)),
			// Both of these count 512-byte sectors, NOT logical blocks: the
			// kernel checks max_sectors against max_io_buf_bytes >> 9 and
			// derives capacity from dev_sectors << 9.
			MaxSectors:       uint32(params.MaxIOSize / uapi.SectorSize),
			ChunkSectors:     0,
			DevSectors:       uint64(params.Backend.Size() / uapi.SectorSize),
			VirtBoundaryMask: 0,
		},
	}

	c.logger.Debug("calculated basic parameters",
		"logical_bs_shift", ublkParams.Basic.LogicalBSShift,
		"max_sectors", ublkParams.Basic.MaxSectors,
		"dev_sectors", ublkParams.Basic.DevSectors)

	// Limits are only advertised for operations the backend can actually
	// service, since advertising one we drop invites silent data loss.
	// MaxDiscardSectors == 0 means the caller opted out of both.
	if discard, ok := discardParams(params); ok {
		if params.MaxDiscardSegments != 1 && discard.MaxDiscardSectors != 0 {
			c.logger.Warn("clamping MaxDiscardSegments to 1: ublk only supports single-segment discard",
				"requested", params.MaxDiscardSegments)
		}
		ublkParams.Types |= uapi.UBLK_PARAM_TYPE_DISCARD
		ublkParams.Discard = discard
		c.logger.Debug("advertising discard limits",
			"max_discard_sectors", params.MaxDiscardSectors,
			"granularity", params.DiscardGranularity,
			"max_segments", params.MaxDiscardSegments)
	}

	// Marshal params - the Len field is set automatically by the marshal function
	buf := uapi.Marshal(ublkParams)

	// Pad buffer to minimum 128 bytes if needed
	if len(buf) < 128 {
		padded := make([]byte, 128)
		copy(padded, buf)
		buf = padded
		binary.LittleEndian.PutUint32(buf[0:4], 128)
		c.logger.Debug("padded parameter buffer", "size", 128)
	}

	c.logger.Debug("parameter buffer prepared",
		"size", len(buf),
		"addr", fmt.Sprintf("%p", &buf[0]),
		"first_16_bytes", fmt.Sprintf("%x", buf[:16]))

	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      deviceID,
		QueueID:    0xFFFF,
		Len:        uint16(len(buf)),
		Addr:       uint64(uintptr(unsafe.Pointer(&buf[0]))),
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}

	op := uapi.UblkCtrlCmd(uapi.UBLK_CMD_SET_PARAMS)
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return fmt.Errorf("SET_PARAMS failed: %v", err)
	}

	c.logger.Info("SET_PARAMS completed", "result", result.Value())

	if result.Value() < 0 {
		return fmt.Errorf("SET_PARAMS failed with error: %d", result.Value())
	}

	return nil
}

func (c *Controller) StartDevice(deviceID uint32) error {
	c.logger.Debug("starting device", "dev_id", deviceID)
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      deviceID,
		QueueID:    0xFFFF,
		Len:        0,
		Addr:       0,
		Data:       uint64(os.Getpid()),
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}
	op := uapi.UblkCtrlCmd(uapi.UBLK_CMD_START_DEV)
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return fmt.Errorf("START_DEV failed: %v", err)
	}

	c.logger.Info("START_DEV completed", "result", result.Value())

	if result.Value() < 0 {
		return fmt.Errorf("START_DEV failed with error: %d", result.Value())
	}

	return nil
}

func (c *Controller) StopDevice(deviceID uint32) error {
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      deviceID,
		QueueID:    0xFFFF,
		Len:        0,
		Addr:       0,
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}
	op := uapi.UblkCtrlCmd(uapi.UBLK_CMD_STOP_DEV)
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return fmt.Errorf("STOP_DEV failed: %v", err)
	}

	if result.Value() < 0 {
		return fmt.Errorf("STOP_DEV failed with error: %d", result.Value())
	}

	return nil
}

func (c *Controller) DeleteDevice(deviceID uint32) error {
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      deviceID,
		QueueID:    0xFFFF,
		Len:        0,
		Addr:       0,
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}
	op := uapi.UblkCtrlCmd(uapi.UBLK_CMD_DEL_DEV)
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return fmt.Errorf("DEL_DEV failed: %v", err)
	}

	if result.Value() < 0 {
		return fmt.Errorf("DEL_DEV failed with error: %d", result.Value())
	}

	return nil
}

func (c *Controller) GetDeviceInfo(deviceID uint32) (*uapi.UblksrvCtrlDevInfo, error) {
	buf := make([]byte, 80)

	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      deviceID,
		QueueID:    0xFFFF,
		Len:        uint16(len(buf)),
		Addr:       uint64(uintptr(unsafe.Pointer(&buf[0]))),
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}

	op := uapi.UblkCtrlCmd(uapi.UBLK_CMD_GET_DEV_INFO)
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return nil, fmt.Errorf("GET_DEV_INFO failed: %v", err)
	}

	if result.Value() < 0 {
		return nil, fmt.Errorf("GET_DEV_INFO failed with error: %d", result.Value())
	}

	devInfo := uapi.UnmarshalCtrlDevInfo(buf)
	return devInfo, nil
}

// GetParams retrieves current device parameters (including devt majors/minors when available)
func (c *Controller) GetParams(deviceID uint32) (*uapi.UblkParams, error) {
	// Allocate a buffer big enough for common parameter sets (basic + devt)
	buf := make([]byte, 128)

	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      deviceID,
		QueueID:    0xFFFF,
		Len:        uint16(len(buf)),
		Addr:       uint64(uintptr(unsafe.Pointer(&buf[0]))),
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}

	op := uapi.UblkCtrlCmd(uapi.UBLK_CMD_GET_PARAMS)
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return nil, fmt.Errorf("GET_PARAMS failed: %v", err)
	}
	if result.Value() < 0 {
		return nil, fmt.Errorf("GET_PARAMS failed with error: %d", result.Value())
	}
	params := &uapi.UblkParams{}
	if err := uapi.Unmarshal(buf, params); err != nil {
		params.Len = uint32(len(buf))
	}
	return params, nil
}

func (c *Controller) buildFeatureFlags(params *DeviceParams) uint64 {
	var flags uint64

	// Prefer completions in task context for control plane, as seen in
	// working reference setups (flags 0x42 = COMP_IN_TASK | IOCTL_ENCODE).
	// This is generally safe for control cmds and improves compatibility.
	flags |= uapi.UBLK_F_URING_CMD_COMP_IN_TASK

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

	return flags
}

// SetLogger sets the logger for this controller
func (c *Controller) SetLogger(logger *logging.Logger) {
	if logger != nil {
		c.logger = logger
	}
}

// sizeToShift converts a size to its shift value (log2)
func sizeToShift(size int) int {
	shift := 0
	for s := size; s > 1; s >>= 1 {
		shift++
	}
	return shift
}
