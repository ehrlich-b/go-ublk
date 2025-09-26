package ctrl

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"

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
	useIoctl  bool
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
		useIoctl:  true,
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

	// Marshal device info and optionally pad to requested length (64 or 80)
	infoBuf := uapi.Marshal(devInfo)
	if v := os.Getenv("UBLK_DEVINFO_LEN"); v != "" {
		if want, err := strconv.Atoi(v); err == nil {
			if want == 80 && len(infoBuf) == 64 {
				padded := make([]byte, 80)
				copy(padded, infoBuf)
				infoBuf = padded
				c.logger.Debug("using padded dev_info payload", "size", 80)
			} else if want == 64 && len(infoBuf) != 64 {
				// Not expected today; keep as-is
			}
		}
	}

	// Build control header (48-byte variant)
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      devInfo.DevID,
		QueueID:    0xFFFF,
		Len:        uint16(len(infoBuf)),
		Addr:       uint64(uintptr(unsafe.Pointer(&infoBuf[0]))),
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

	c.logger.Debug("device info buffer", "size", len(infoBuf), "data", fmt.Sprintf("%x", infoBuf))

	// ALWAYS use ioctl encoding - kernel 6.11+ requires it
	c.useIoctl = true
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
	runtime.KeepAlive(infoBuf)

	info := uapi.UnmarshalCtrlDevInfo(infoBuf)
	c.logger.Info("device created", "dev_id", info.DevID)
	return info.DevID, nil
}

func (c *Controller) SetParams(devID uint32, params *DeviceParams) error {
	c.logger.Debug("setting device parameters",
		"logical_bs", params.LogicalBlockSize,
		"max_io", params.MaxIOSize,
		"backend_size", params.Backend.Size())

	ublkParams := &uapi.UblkParams{
		Types: uapi.UBLK_PARAM_TYPE_BASIC,
		Basic: uapi.UblkParamBasic{
			Attrs:            0,
			LogicalBSShift:   uint8(sizeToShift(params.LogicalBlockSize)),
			PhysicalBSShift:  uint8(sizeToShift(params.LogicalBlockSize)),
			IOOptShift:       0,
			IOMinShift:       uint8(sizeToShift(params.LogicalBlockSize)),
			MaxSectors:       uint32(params.MaxIOSize / params.LogicalBlockSize),
			ChunkSectors:     0,
			DevSectors:       uint64(params.Backend.Size() / int64(params.LogicalBlockSize)),
			VirtBoundaryMask: 0,
		},
	}

	c.logger.Debug("calculated basic parameters",
		"logical_bs_shift", ublkParams.Basic.LogicalBSShift,
		"max_sectors", ublkParams.Basic.MaxSectors,
		"dev_sectors", ublkParams.Basic.DevSectors)

	// TODO: Add discard parameters if backend supports it

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
		DevID:      devID,
		QueueID:    0xFFFF,
		Len:        uint16(len(buf)),
		Addr:       uint64(uintptr(unsafe.Pointer(&buf[0]))),
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}

	var op uint32 = uapi.UBLK_CMD_SET_PARAMS
	if c.useIoctl {
		op = uapi.UblkCtrlCmd(op)
	}
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

func (c *Controller) StartDevice(devID uint32) error {
	c.logger.Debug("starting device", "dev_id", devID)
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      devID,
		QueueID:    0xFFFF,
		Len:        0,
		Addr:       0,
		Data:       uint64(os.Getpid()),
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}
	var op uint32 = uapi.UBLK_CMD_START_DEV
	if c.useIoctl {
		op = uapi.UblkCtrlCmd(op)
	}
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

// AsyncStartHandle wraps the async START_DEV operation
type AsyncStartHandle struct {
	handle *uring.AsyncHandle
	devID  uint32
}

// Wait waits for START_DEV completion
func (h *AsyncStartHandle) Wait(timeout time.Duration) error {
	result, err := h.handle.Wait(timeout)
	if err != nil {
		return fmt.Errorf("START_DEV timeout for device %d: %v", h.devID, err)
	}

	if result.Value() < 0 {
		return fmt.Errorf("START_DEV failed with error: %d", result.Value())
	}

	return nil
}

// StartDeviceAsync initiates START_DEV without blocking
func (c *Controller) StartDeviceAsync(devID uint32) (*AsyncStartHandle, error) {
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      devID,
		QueueID:    0xFFFF,
		Len:        0,
		Addr:       0,
		Data:       uint64(os.Getpid()),
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}

	var op uint32 = uapi.UBLK_CMD_START_DEV
	if c.useIoctl {
		op = uapi.UblkCtrlCmd(op)
	}

	// Submit asynchronously
	handle, err := c.ring.SubmitCtrlCmdAsync(op, cmd, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to submit START_DEV: %v", err)
	}

	return &AsyncStartHandle{
		handle: handle,
		devID:  devID,
	}, nil
}

// StartDataPlane is deprecated - queue runners handle FETCH_REQ directly
func (c *Controller) StartDataPlane(devID uint32, numQueues, queueDepth int) error {
	c.logger.Warn("StartDataPlane is deprecated", "dev_id", devID)
	return nil
}

func (c *Controller) StopDevice(devID uint32) error {
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      devID,
		QueueID:    0xFFFF,
		Len:        0,
		Addr:       0,
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}
	var op uint32 = uapi.UBLK_CMD_STOP_DEV
	if c.useIoctl {
		op = uapi.UblkCtrlCmd(op)
	}
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return fmt.Errorf("STOP_DEV failed: %v", err)
	}

	if result.Value() < 0 {
		return fmt.Errorf("STOP_DEV failed with error: %d", result.Value())
	}

	return nil
}

func (c *Controller) DeleteDevice(devID uint32) error {
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      devID,
		QueueID:    0xFFFF,
		Len:        0,
		Addr:       0,
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}
	var op uint32 = uapi.UBLK_CMD_DEL_DEV
	if c.useIoctl {
		op = uapi.UblkCtrlCmd(op)
	}
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return fmt.Errorf("DEL_DEV failed: %v", err)
	}

	if result.Value() < 0 {
		return fmt.Errorf("DEL_DEV failed with error: %d", result.Value())
	}

	return nil
}

func (c *Controller) GetDeviceInfo(devID uint32) (*uapi.UblksrvCtrlDevInfo, error) {
	buf := make([]byte, 80)

	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      devID,
		QueueID:    0xFFFF,
		Len:        uint16(len(buf)),
		Addr:       uint64(uintptr(unsafe.Pointer(&buf[0]))),
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}

	var op uint32 = uapi.UBLK_CMD_GET_DEV_INFO
	if c.useIoctl {
		op = uapi.UblkCtrlCmd(op)
	}
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
func (c *Controller) GetParams(devID uint32) (*uapi.UblkParams, error) {
	// Allocate a buffer big enough for common parameter sets (basic + devt)
	buf := make([]byte, 128)

	cmd := &uapi.UblksrvCtrlCmd{
		DevID:      devID,
		QueueID:    0xFFFF,
		Len:        uint16(len(buf)),
		Addr:       uint64(uintptr(unsafe.Pointer(&buf[0]))),
		Data:       0,
		DevPathLen: 0,
		Pad:        0,
		Reserved:   0,
	}

	var op uint32 = uapi.UBLK_CMD_GET_PARAMS
	if c.useIoctl {
		op = uapi.UblkCtrlCmd(op)
	}
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
