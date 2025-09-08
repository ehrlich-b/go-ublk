package ctrl

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

const (
	UblkControlPath = "/dev/ublk-control"
)

type Controller struct {
	controlFd int
	ring      uring.Ring
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
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:   uint32(params.DeviceID),
		QueueID: 0xFFFF,
		Len:     0,
		Addr:    0,
	}

	result, err := c.ring.SubmitCtrlCmd(uapi.UBLK_CMD_ADD_DEV, cmd, 0)
	if err != nil {
		return 0, fmt.Errorf("ADD_DEV failed: %v", err)
	}

	if result.Value() < 0 {
		return 0, fmt.Errorf("ADD_DEV failed with error: %d", result.Value())
	}

	devID := uint32(result.Value())
	return devID, nil
}

func (c *Controller) SetParams(devID uint32, params *DeviceParams) error {
	ublkParams := &uapi.UblkParams{
		Types: uapi.UBLK_PARAM_TYPE_BASIC,
		Basic: uapi.UblkParamBasic{
			Attrs:              0,
			LogicalBSShift:     uint8(sizeToShift(params.LogicalBlockSize)),
			PhysicalBSShift:    uint8(sizeToShift(params.LogicalBlockSize)),
			IOOptShift:         0,
			IOMinShift:         uint8(sizeToShift(params.LogicalBlockSize)),
			MaxSectors:         uint32(params.MaxIOSize / params.LogicalBlockSize),
			ChunkSectors:       0,
			DevSectors:         uint64(params.Backend.Size() / int64(params.LogicalBlockSize)),
			VirtBoundaryMask:   0,
		},
	}

	// Add discard params if supported
	if _, ok := params.Backend.(interface{ Discard(int64, int64) error }); ok {
		ublkParams.Types |= uapi.UBLK_PARAM_TYPE_DISCARD
		ublkParams.Discard = uapi.UblkParamDiscard{
			DiscardAlignment:      params.DiscardAlignment,
			DiscardGranularity:   params.DiscardGranularity,
			MaxDiscardSectors:    params.MaxDiscardSectors,
			MaxDiscardSegments:   params.MaxDiscardSegments,
		}
	}

	buf := uapi.Marshal(ublkParams)
	ublkParams.Len = uint32(len(buf))

	cmd := &uapi.UblksrvCtrlCmd{
		DevID:   devID,
		QueueID: 0xFFFF,
		Len:     uint16(len(buf)),
		Addr:    uint64(uintptr(unsafe.Pointer(&buf[0]))),
	}

	result, err := c.ring.SubmitCtrlCmd(uapi.UBLK_CMD_SET_PARAMS, cmd, 0)
	if err != nil {
		return fmt.Errorf("SET_PARAMS failed: %v", err)
	}

	if result.Value() < 0 {
		return fmt.Errorf("SET_PARAMS failed with error: %d", result.Value())
	}

	return nil
}

func (c *Controller) StartDevice(devID uint32) error {
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:   devID,
		QueueID: 0xFFFF,
		Len:     0,
		Addr:    0,
	}

	result, err := c.ring.SubmitCtrlCmd(uapi.UBLK_CMD_START_DEV, cmd, 0)
	if err != nil {
		return fmt.Errorf("START_DEV failed: %v", err)
	}

	if result.Value() < 0 {
		return fmt.Errorf("START_DEV failed with error: %d", result.Value())
	}

	return nil
}

func (c *Controller) StopDevice(devID uint32) error {
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:   devID,
		QueueID: 0xFFFF,
		Len:     0,
		Addr:    0,
	}

	result, err := c.ring.SubmitCtrlCmd(uapi.UBLK_CMD_STOP_DEV, cmd, 0)
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
		DevID:   devID,
		QueueID: 0xFFFF,
		Len:     0,
		Addr:    0,
	}

	result, err := c.ring.SubmitCtrlCmd(uapi.UBLK_CMD_DEL_DEV, cmd, 0)
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
		DevID:   devID,
		QueueID: 0xFFFF,
		Len:     uint16(len(buf)),
		Addr:    uint64(uintptr(unsafe.Pointer(&buf[0]))),
	}

	result, err := c.ring.SubmitCtrlCmd(uapi.UBLK_CMD_GET_DEV_INFO, cmd, 0)
	if err != nil {
		return nil, fmt.Errorf("GET_DEV_INFO failed: %v", err)
	}

	if result.Value() < 0 {
		return nil, fmt.Errorf("GET_DEV_INFO failed with error: %d", result.Value())
	}

	devInfo := uapi.UnmarshalCtrlDevInfo(buf)
	return devInfo, nil
}

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

	return flags
}

// sizeToShift converts a size to its shift value (log2)
func sizeToShift(size int) int {
	shift := 0
	for s := size; s > 1; s >>= 1 {
		shift++
	}
	return shift
}