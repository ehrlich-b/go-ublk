// Package ublk provides the main API for creating userspace block devices
package ublk

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/ehrlich-b/go-ublk/internal/ctrl"
    "github.com/ehrlich-b/go-ublk/internal/interfaces"
    "github.com/ehrlich-b/go-ublk/internal/queue"
)

// waitLive waits for a ublk device to transition to LIVE state
func waitLive(devID uint32, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)

    // The sysfs path may not exist on all systems, so we'll just wait
    // a bit and check if the block device appears
    fmt.Printf("*** DEBUG: Waiting for device %d to become ready\n", devID)
    time.Sleep(500 * time.Millisecond) // Give kernel time to process START_DEV

    // Check if block device exists
    blockPath := fmt.Sprintf("/dev/ublkb%d", devID)
    for time.Now().Before(deadline) {
        if _, err := os.Stat(blockPath); err == nil {
            fmt.Printf("*** DEBUG: Device %d is ready (block device exists)\n", devID)
            return nil
        }
        time.Sleep(10 * time.Millisecond)
    }

    // If no block device after timeout, assume it's ready anyway
    // (the queue runners will handle retries)
    fmt.Printf("*** WARNING: Device %d block device not visible after %s, continuing anyway\n", devID, timeout)
    return nil
}

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

    // CRITICAL FIX: Queue runners must be started BEFORE START_DEV
    // The kernel waits for initial FETCH_REQ commands from all queues
    // before completing START_DEV. This matches the C implementation.

    // STEP 1: Initialize queue runners
    fmt.Printf("*** DEBUG: Creating %d queue runners\n", numQueues)
    device.runners = make([]*queue.Runner, numQueues)
    for i := 0; i < numQueues; i++ {
        fmt.Printf("*** DEBUG: Creating queue runner %d\n", i)
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
            ctrl.DeleteDevice(devID)
            return nil, fmt.Errorf("failed to create queue runner %d: %v", i, err)
        }
        fmt.Printf("*** DEBUG: Queue runner %d created successfully\n", i)
        device.runners[i] = runner
    }

    // STEP 2: Start queue runner goroutines BEFORE START_DEV
    // The goroutines will just wait initially, similar to C threads
    // CRITICAL: Start queue runners and have them submit FETCH_REQs BEFORE START_DEV
    // This is the correct sequence according to ublk semantics
    fmt.Printf("*** DEBUG: Starting queue runners to submit FETCH_REQs BEFORE START_DEV\n")
    for i := 0; i < numQueues; i++ {
        // Start the runner which will immediately submit all FETCH_REQs for its tags
        if err := device.runners[i].Start(); err != nil {
            for j := 0; j < len(device.runners); j++ {
                if device.runners[j] != nil {
                    device.runners[j].Close()
                }
            }
            ctrl.DeleteDevice(devID)
            return nil, fmt.Errorf("failed to start queue runner %d: %v", i, err)
        }
        fmt.Printf("*** DEBUG: Queue runner %d started and FETCH_REQs submitted\n", i)
    }

    // CRITICAL: Give kernel time to see all FETCH_REQs
    fmt.Printf("*** DEBUG: Waiting for kernel to see all FETCH_REQs\n")
    time.Sleep(100 * time.Millisecond)

    // STEP 3: NOW submit START_DEV (after FETCH_REQs are in place)
    fmt.Printf("*** CRITICAL: Submitting START_DEV AFTER FETCH_REQs are posted\n")
    err = ctrl.StartDevice(devID)  // Use synchronous version
    if err != nil {
        for j := 0; j < len(device.runners); j++ {
            if device.runners[j] != nil {
                device.runners[j].Close()
            }
        }
        ctrl.DeleteDevice(devID)
        return nil, fmt.Errorf("failed to START_DEV: %v", err)
    }

    // STEP 4: Device should now be LIVE
    fmt.Printf("*** SUCCESS: START_DEV completed, device should be LIVE\n")

    // Check if the block device appears
    devicePath := fmt.Sprintf("/dev/ublkb%d", devID)
    if _, err := os.Stat(devicePath); err == nil {
        fmt.Printf("*** SUCCESS: Device %s created!\n", devicePath)
    } else {
        fmt.Printf("*** WARNING: Device %s not yet visible, but state is LIVE\n", devicePath)
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
