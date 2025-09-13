// Package ublk provides the main API for creating userspace block devices
package ublk

import (
    "context"
    "fmt"

    "github.com/ehrlich-b/go-ublk/internal/ctrl"
    "github.com/ehrlich-b/go-ublk/internal/interfaces"
    "github.com/ehrlich-b/go-ublk/internal/queue"
)

// Re-export interfaces from internal package
type Backend = interfaces.Backend
type DiscardBackend = interfaces.DiscardBackend
type WriteZeroesBackend = interfaces.WriteZeroesBackend
type SyncBackend = interfaces.SyncBackend
type StatBackend = interfaces.StatBackend
type ResizeBackend = interfaces.ResizeBackend

// Device represents a ublk block device
type Device struct {
	// ID is the device ID assigned by the kernel
	ID uint32

	// Path is the path to the block device (e.g., "/dev/ublkb0")  
	Path string

	// CharPath is the path to the character device (e.g., "/dev/ublkc0")
	CharPath string

	// Backend is the backend implementation
	Backend Backend

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Internal state
	queues   int
	depth    int
	blockSize int
	started  bool
	runners  []*queue.Runner
}

// DeviceParams contains parameters for creating a ublk device
type DeviceParams struct {
	// Backend provides the storage implementation
	Backend Backend

	// Device configuration
	QueueDepth       int    // Queue depth per queue (default: 128)
	NumQueues        int    // Number of queues (default: number of CPUs)
	LogicalBlockSize int    // Logical block size in bytes (default: 512)
	MaxIOSize        int    // Maximum I/O size in bytes (default: 1MB)

	// Feature flags
	EnableZeroCopy     bool // Enable zero-copy if supported
	EnableUnprivileged bool // Allow unprivileged operation
	EnableUserCopy     bool // Use user-copy mode
	EnableZoned        bool // Enable zoned storage support
	EnableIoctlEncode  bool // Use ioctl encoding instead of URING_CMD

	// Device attributes
	ReadOnly        bool // Make device read-only
	Rotational      bool // Device is rotational (HDD-like)
	VolatileCache   bool // Device has volatile cache
	EnableFUA       bool // Enable Force Unit Access

	// Discard parameters (only used if backend implements DiscardBackend)
	DiscardAlignment    uint32 // Discard alignment
	DiscardGranularity  uint32 // Discard granularity
	MaxDiscardSectors   uint32 // Max sectors per discard
	MaxDiscardSegments  uint16 // Max segments per discard

	// Advanced options
	DeviceID      int32  // Specific device ID to request (-1 for auto)
	DeviceName    string // Optional device name
	CPUAffinity   []int  // CPU affinity mask for queue threads
}

// DefaultParams returns default device parameters
func DefaultParams(backend Backend) DeviceParams {
	return DeviceParams{
		Backend:          backend,
		QueueDepth:       128,
		NumQueues:        0, // 0 means auto-detect based on CPUs
		LogicalBlockSize: 512,
		MaxIOSize:        1 << 20, // 1MB

		// Sensible defaults
		EnableZeroCopy:     false, // Requires 4K blocks
		EnableUnprivileged: false, // Requires root by default
		EnableUserCopy:     false, // Direct mode by default
		EnableZoned:        false, // Regular block device
		EnableIoctlEncode:  false, // Use URING_CMD (modern approach)

		ReadOnly:      false,
		Rotational:    false, // SSD-like by default
		VolatileCache: false,
		EnableFUA:     false,

		// Discard defaults
		DiscardAlignment:   4096,
		DiscardGranularity: 4096,
		MaxDiscardSectors:  0xffffffff,
		MaxDiscardSegments: 256,

		DeviceID: -1, // Auto-assign
	}
}

// Options contains additional options for device creation
type Options struct {
	// Context for cancellation (if nil, uses context.Background())
	Context context.Context

	// Logger for debug/info messages (if nil, no logging)
	Logger Logger
}

// Logger interface for optional logging
type Logger interface {
	Printf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}

// CreateAndServe creates a ublk device with the given parameters and starts serving I/O.
// This is the main entry point for creating ublk devices.
// 
// The device will continue serving I/O until:
// - The context is cancelled
// - StopAndDelete is called
// - An unrecoverable error occurs
//
// Example:
//   backend := mem.New(64 << 20) // 64MB RAM disk
//   params := ublk.DefaultParams(backend)
//   device, err := ublk.CreateAndServe(context.Background(), params, nil)
func CreateAndServe(ctx context.Context, params DeviceParams, options *Options) (*Device, error) {
	fmt.Printf("*** CRITICAL: CreateAndServe starting with new code\n")
	if ctx == nil {
		ctx = context.Background()
	}
	
	if options == nil {
		options = &Options{}
	}
	
	if options.Context != nil {
		ctx = options.Context
	}

	// Create controller
	ctrl, err := createController()
	if err != nil {
		return nil, fmt.Errorf("failed to create controller: %v", err)
	}
	defer ctrl.Close()

	// Convert params to internal format
	ctrlParams := convertToCtrlParams(params)

	// Create device using control plane
	devID, err := ctrl.AddDevice(&ctrlParams)
	if err != nil {
		return nil, fmt.Errorf("failed to add device: %v", err)
	}

	// Set parameters
	err = ctrl.SetParams(devID, &ctrlParams)
	if err != nil {
		ctrl.DeleteDevice(devID)
		return nil, fmt.Errorf("failed to set parameters: %v", err)
	}

	// Create Device struct
	device := &Device{
		ID:        devID,
		Path:      fmt.Sprintf("/dev/ublkb%d", devID),
		CharPath:  fmt.Sprintf("/dev/ublkc%d", devID),
		Backend:   params.Backend,
		queues:    params.NumQueues,
		depth:     params.QueueDepth,
		blockSize: params.LogicalBlockSize,
		started:   false, // Not started yet
	}

	device.ctx, device.cancel = context.WithCancel(ctx)

	numQueues := params.NumQueues
	if numQueues == 0 {
		numQueues = 1 // Single queue for minimal implementation
	}

    // STEP 1: Start device first
    err = ctrl.StartDevice(devID)
    if err != nil {
        ctrl.DeleteDevice(devID)
        return nil, fmt.Errorf("failed to start device: %v", err)
    }

    // STEP 2: Proceed to queue runners - kernel should create device nodes

    // STEP 3: Now start queue runners - device nodes should exist
	device.runners = make([]*queue.Runner, numQueues)
	for i := 0; i < numQueues; i++ {
		runnerConfig := queue.Config{
			DevID:   devID,
			QueueID: uint16(i),
			Depth:   params.QueueDepth,
			Backend: params.Backend,
			Logger:  options.Logger,
		}

		runner, err := queue.NewRunner(device.ctx, runnerConfig)
		if err != nil {
			// Cleanup already created runners
			for j := 0; j < i; j++ {
				if device.runners[j] != nil {
					device.runners[j].Close()
				}
			}
			ctrl.StopDevice(devID)
			ctrl.DeleteDevice(devID)
			return nil, fmt.Errorf("failed to create queue runner %d: %v", i, err)
		}

		device.runners[i] = runner

		// Start the runner
		err = runner.Start()
		if err != nil {
			// Cleanup
			for j := 0; j <= i; j++ {
				if device.runners[j] != nil {
					device.runners[j].Close()
				}
			}
			ctrl.StopDevice(devID)
			ctrl.DeleteDevice(devID)
			return nil, fmt.Errorf("failed to start queue runner %d: %v", i, err)
		}
	}

	device.started = true

	if options.Logger != nil {
		options.Logger.Printf("Device created: %s (ID: %d) with %d queues", device.Path, device.ID, numQueues)
	}

	return device, nil
}

// StopAndDelete stops the device and removes it from the system.
// This should be called to cleanly shut down a ublk device.
func StopAndDelete(ctx context.Context, device *Device) error {
	if device == nil {
		return ErrInvalidParameters
	}

	// Stop and cleanup queue runners
	for _, runner := range device.runners {
		if runner != nil {
			runner.Close()
		}
	}
	device.runners = nil

	// Cancel context to stop any remaining goroutines
	if device.cancel != nil {
		device.cancel()
	}

	// Create controller for cleanup
	ctrl, err := createController()
	if err != nil {
		return fmt.Errorf("failed to create controller for cleanup: %v", err)
	}
	defer ctrl.Close()

	// Stop device
	err = ctrl.StopDevice(device.ID)
	if err != nil {
		return fmt.Errorf("failed to stop device: %v", err)
	}

	// Delete device
	err = ctrl.DeleteDevice(device.ID)
	if err != nil {
		return fmt.Errorf("failed to delete device: %v", err)
	}

	device.started = false
	return nil
}

// createController creates a new control plane controller
func createController() (*ctrl.Controller, error) {
	return ctrl.NewController()
}

// convertToCtrlParams converts public DeviceParams to internal ctrl.DeviceParams
func convertToCtrlParams(params DeviceParams) ctrl.DeviceParams {
	ctrlParams := ctrl.DefaultDeviceParams(params.Backend)
	
	// Copy all fields
	ctrlParams.DeviceID = params.DeviceID
	ctrlParams.QueueDepth = params.QueueDepth
	ctrlParams.NumQueues = params.NumQueues
	ctrlParams.LogicalBlockSize = params.LogicalBlockSize
	ctrlParams.MaxIOSize = params.MaxIOSize

	ctrlParams.EnableZeroCopy = params.EnableZeroCopy
	ctrlParams.EnableUnprivileged = params.EnableUnprivileged
	ctrlParams.EnableUserCopy = params.EnableUserCopy
	ctrlParams.EnableZoned = params.EnableZoned
	ctrlParams.EnableIoctlEncode = params.EnableIoctlEncode

	ctrlParams.ReadOnly = params.ReadOnly
	ctrlParams.Rotational = params.Rotational
	ctrlParams.VolatileCache = params.VolatileCache
	ctrlParams.EnableFUA = params.EnableFUA

	ctrlParams.DiscardAlignment = params.DiscardAlignment
	ctrlParams.DiscardGranularity = params.DiscardGranularity
	ctrlParams.MaxDiscardSectors = params.MaxDiscardSectors
	ctrlParams.MaxDiscardSegments = params.MaxDiscardSegments

	ctrlParams.DeviceName = params.DeviceName
	ctrlParams.CPUAffinity = params.CPUAffinity

	return ctrlParams
}

// Error definitions
type UblkError string

func (e UblkError) Error() string {
	return string(e)
}

const (
	ErrNotImplemented    UblkError = "not implemented"
	ErrDeviceNotFound    UblkError = "device not found"
	ErrDeviceBusy        UblkError = "device busy"
	ErrInvalidParameters UblkError = "invalid parameters"
	ErrKernelNotSupported UblkError = "kernel does not support ublk"
	ErrPermissionDenied  UblkError = "permission denied"
	ErrInsufficientMemory UblkError = "insufficient memory"
)
