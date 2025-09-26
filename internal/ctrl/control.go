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
		// Modern kernels expect ioctl-encoded control commands. Default to true
		// so STOP/DEL issued by a fresh controller use the same encoding path.
		useIoctl: true,
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

	// Debug: log the device parameters being sent
	fmt.Printf("*** DEBUG: ADD_DEV device info: queues=%d, depth=%d, maxIO=%d, flags=0x%x, devID=%d\n",
		devInfo.NrHwQueues, devInfo.QueueDepth, devInfo.MaxIOBufBytes, devInfo.UblksrvFlags, devInfo.DevID)

	// Marshal device info and optionally pad to requested length (64 or 80)
	infoBuf := uapi.Marshal(devInfo)
	if v := os.Getenv("UBLK_DEVINFO_LEN"); v != "" {
		if want, err := strconv.Atoi(v); err == nil {
			if want == 80 && len(infoBuf) == 64 {
				padded := make([]byte, 80)
				copy(padded, infoBuf)
				infoBuf = padded
				fmt.Printf("*** DEBUG: Using 80-byte dev_info payload (padded)\n")
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

	// Debug: log the control command being sent
	fmt.Printf("*** DEBUG: ADD_DEV ctrl cmd: devID=%d, queueID=%d, len=%d, addr=0x%x\n",
		cmd.DevID, cmd.QueueID, cmd.Len, cmd.Addr)

	// Debug: dump the actual buffer bytes being sent
	fmt.Printf("*** DEBUG: Device info buffer (%d bytes): %x\n", len(infoBuf), infoBuf)

	// ALWAYS use ioctl encoding - kernel 6.11+ requires it
	c.useIoctl = true
	op := uapi.UblkCtrlCmd(uapi.UBLK_CMD_ADD_DEV)
	result, err := c.ring.SubmitCtrlCmd(op, cmd, 0)
	if err != nil {
		return 0, fmt.Errorf("ADD_DEV submit failed: %v", err)
	}

	fmt.Printf("*** CRITICAL: ADD_DEV result: %d\n", result.Value())

	if result.Value() < 0 {
		return 0, fmt.Errorf("ADD_DEV failed with error: %d", result.Value())
	}

	// Ensure device info buffer stays alive until after kernel copies it
	runtime.KeepAlive(infoBuf)

	info := uapi.UnmarshalCtrlDevInfo(infoBuf)
	fmt.Printf("*** CRITICAL: Device ID after ADD_DEV: %d\n", info.DevID)
	return info.DevID, nil
}

func (c *Controller) SetParams(devID uint32, params *DeviceParams) error {
	// Debug: log the input params
	fmt.Printf("*** DEBUG SET_PARAMS input: LogicalBS=%d, MaxIO=%d, Backend.Size=%d\n",
		params.LogicalBlockSize, params.MaxIOSize, params.Backend.Size())

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

	fmt.Printf("*** DEBUG SET_PARAMS Basic: LogicalBSShift=%d, MaxSectors=%d, DevSectors=%d\n",
		ublkParams.Basic.LogicalBSShift, ublkParams.Basic.MaxSectors, ublkParams.Basic.DevSectors)

	// Add discard params if supported - DISABLED TO DEBUG
	// TODO: Re-enable after fixing basic params
	/*
		if _, ok := params.Backend.(interface{ Discard(int64, int64) error }); ok {
			ublkParams.Types |= uapi.UBLK_PARAM_TYPE_DISCARD
			ublkParams.Discard = uapi.UblkParamDiscard{
				DiscardAlignment:      params.DiscardAlignment,
				DiscardGranularity:   params.DiscardGranularity,
				MaxDiscardSectors:    params.MaxDiscardSectors,
				MaxDiscardSegments:   params.MaxDiscardSegments,
			}
		}
	*/

	// Marshal params - the Len field is set automatically by the marshal function
	buf := uapi.Marshal(ublkParams)

	// CRITICAL: Kernel might expect minimum 128 byte buffer
	if len(buf) < 128 {
		padded := make([]byte, 128)
		copy(padded, buf)
		buf = padded
		// Update the Len field in the buffer to match actual size
		binary.LittleEndian.PutUint32(buf[0:4], 128)
		fmt.Printf("*** DEBUG SET_PARAMS: Padded buffer to 128 bytes and updated Len field\n")
	}

	fmt.Printf("*** DEBUG SET_PARAMS: params buffer len=%d, addr=%p\n", len(buf), &buf[0])
	fmt.Printf("*** DEBUG SET_PARAMS: first 16 bytes: %x\n", buf[:16])

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

	fmt.Printf("*** CRITICAL: SET_PARAMS result: %d\n", result.Value())

	if result.Value() < 0 {
		return fmt.Errorf("SET_PARAMS failed with error: %d", result.Value())
	}

	return nil
}

func (c *Controller) StartDevice(devID uint32) error {
	// IMPORTANT: pass daemon PID in ctrl cmd data[0], like ublksrv does.
	// The kernel uses this to signal the userspace queue threads.
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

	fmt.Printf("*** CRITICAL: START_DEV result: %d\n", result.Value())

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

// StartDataPlane is removed - FETCH_REQ must be done by per-queue runners
// Device nodes are created by the kernel after START_DEV, not by FETCH_REQ
func (c *Controller) StartDataPlane(devID uint32, numQueues, queueDepth int) error {
	fmt.Printf("*** CRITICAL: StartDataPlane - FETCH_REQ approach was wrong!\n")
	fmt.Printf("*** Device nodes should appear after START_DEV, not after FETCH_REQ\n")
	fmt.Printf("*** FETCH_REQ must be done by queue runners on /dev/ublkc%d fds\n", devID)

	// The correct sequence is:
	// 1. ADD_DEV (done)
	// 2. SET_PARAMS (done)
	// 3. START_DEV (done)
	// 4. Device nodes /dev/ublkb<ID> and /dev/ublkc<ID> should now exist
	// 5. Queue runners open /dev/ublkc<ID> and submit FETCH_REQ on those fds

	// For now, just return success - device creation should already have triggered node creation
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

// sizeToShift converts a size to its shift value (log2)
func sizeToShift(size int) int {
	shift := 0
	for s := size; s > 1; s >>= 1 {
		shift++
	}
	return shift
}
