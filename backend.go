// Package ublk provides the main API for creating userspace block devices
package ublk

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/ctrl"
	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/queue"
)

// waitLive waits for a ublk device to transition to LIVE state
func waitLive(devID uint32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Give kernel time to process START_DEV
	time.Sleep(constants.DeviceStartupDelay)

	// Check if block device exists
	blockPath := fmt.Sprintf("/dev/ublkb%d", devID)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(blockPath); err == nil {
			return nil
		}
		time.Sleep(constants.DevicePollingInterval)
	}

	// Timeout waiting for device
	return fmt.Errorf("timeout waiting for device %s to appear", blockPath)
}

// Backend interfaces are now defined in interfaces.go

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
	queues    int
	depth     int
	blockSize int
	started   bool
	runners   []*queue.Runner

	// Metrics and observability
	metrics  *Metrics
	observer Observer
}

// DeviceParams contains parameters for creating a ublk device
type DeviceParams struct {
	// Backend provides the storage implementation
	Backend Backend

	// Device configuration
	QueueDepth       int // Queue depth per queue (default: 128)
	NumQueues        int // Number of queues (default: number of CPUs)
	LogicalBlockSize int // Logical block size in bytes (default: 512)
	MaxIOSize        int // Maximum I/O size in bytes (default: 1MB)

	// Feature flags
	EnableZeroCopy     bool // Enable zero-copy if supported
	EnableUnprivileged bool // Allow unprivileged operation
	EnableUserCopy     bool // Use user-copy mode
	EnableZoned        bool // Enable zoned storage support
	EnableIoctlEncode  bool // Use ioctl encoding instead of URING_CMD

	// Device attributes
	ReadOnly      bool // Make device read-only
	Rotational    bool // Device is rotational (HDD-like)
	VolatileCache bool // Device has volatile cache
	EnableFUA     bool // Enable Force Unit Access

	// Discard parameters (only used if backend implements DiscardBackend)
	DiscardAlignment   uint32 // Discard alignment
	DiscardGranularity uint32 // Discard granularity
	MaxDiscardSectors  uint32 // Max sectors per discard
	MaxDiscardSegments uint16 // Max segments per discard

	// Advanced options
	DeviceID    int32  // Specific device ID to request (-1 for auto)
	DeviceName  string // Optional device name
	CPUAffinity []int  // CPU affinity mask for queue threads
}

// DefaultParams returns default device parameters
func DefaultParams(backend Backend) DeviceParams {
	return DeviceParams{
		Backend:          backend,
		QueueDepth:       constants.DefaultQueueDepth,
		NumQueues:        0, // 0 means auto-detect based on CPUs
		LogicalBlockSize: constants.DefaultLogicalBlockSize,
		MaxIOSize:        constants.DefaultMaxIOSize,

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
		DiscardAlignment:   constants.DefaultDiscardAlignment,
		DiscardGranularity: constants.DefaultDiscardGranularity,
		MaxDiscardSectors:  constants.DefaultMaxDiscardSectors,
		MaxDiscardSegments: constants.DefaultMaxDiscardSegments,

		DeviceID: constants.AutoAssignDeviceID,
	}
}

// Options contains additional options for device creation
type Options struct {
	// Context for cancellation (if nil, uses context.Background())
	Context context.Context

	// Logger for debug/info messages (if nil, no logging)
	Logger Logger

	// Observer for metrics collection (if nil, uses no-op observer)
	Observer Observer
}

// Logger interface is now defined in interfaces.go

// CreateAndServe creates a ublk device with the given parameters and starts serving I/O.
// This is the main entry point for creating ublk devices.
//
// The device will continue serving I/O until:
// - The context is cancelled
// - StopAndDelete is called
// - An unrecoverable error occurs
//
// Example:
//
//	backend := mem.New(64 << 20) // 64MB RAM disk
//	params := ublk.DefaultParams(backend)
//	device, err := ublk.CreateAndServe(context.Background(), params, nil)
func CreateAndServe(ctx context.Context, params DeviceParams, options *Options) (*Device, error) {
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

	// Initialize metrics and observer
	metrics := NewMetrics()
	var observer Observer = &NoOpObserver{}
	if options.Observer != nil {
		observer = options.Observer
	} else {
		// Default to metrics observer if no custom observer provided
		observer = NewMetricsObserver(metrics)
	}

	// Determine actual number of queues (default to 1 if not specified)
	numQueues := params.NumQueues
	if numQueues == 0 {
		numQueues = 1 // Single queue for minimal implementation
	}

	// Create Device struct
	device := &Device{
		ID:        devID,
		Path:      fmt.Sprintf("/dev/ublkb%d", devID),
		CharPath:  fmt.Sprintf("/dev/ublkc%d", devID),
		Backend:   params.Backend,
		queues:    numQueues, // Store actual queue count, not params value
		depth:     params.QueueDepth,
		blockSize: params.LogicalBlockSize,
		started:   false, // Not started yet
		metrics:   metrics,
		observer:  observer,
	}

	device.ctx, device.cancel = context.WithCancel(ctx)

	// Initialize queue runners before START_DEV
	// The kernel waits for initial FETCH_REQ commands from all queues
	device.runners = make([]*queue.Runner, numQueues)
	for i := 0; i < numQueues; i++ {
		runnerConfig := queue.Config{
			DevID:    devID,
			QueueID:  uint16(i),
			Depth:    params.QueueDepth,
			Backend:  params.Backend,
			Logger:   options.Logger,
			Observer: observer,
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
		device.runners[i] = runner
	}

	// Start queue runners and submit FETCH_REQs before START_DEV
	for i := 0; i < numQueues; i++ {
		if err := device.runners[i].Start(); err != nil {
			for j := 0; j < len(device.runners); j++ {
				if device.runners[j] != nil {
					device.runners[j].Close()
				}
			}
			ctrl.DeleteDevice(devID)
			return nil, fmt.Errorf("failed to start queue runner %d: %v", i, err)
		}
	}

	// Give kernel time to see FETCH_REQs
	time.Sleep(constants.QueueInitDelay)

	// Submit START_DEV after FETCH_REQs are in place
	err = ctrl.StartDevice(devID)
	if err != nil {
		for j := 0; j < len(device.runners); j++ {
			if device.runners[j] != nil {
				device.runners[j].Close()
			}
		}
		ctrl.DeleteDevice(devID)
		return nil, fmt.Errorf("failed to START_DEV: %v", err)
	}

	device.started = true

	// Small delay to ensure kernel has processed FETCH_REQs before declaring ready
	// The 250ms was too long, but there's a real race condition that needs timing
	logger := logging.Default()
	time.Sleep(1 * time.Millisecond) // Minimal delay instead of 250ms * queue_depth
	logger.Info("device initialization complete")

	if options.Logger != nil {
		options.Logger.Printf("Device created: %s (ID: %d) with %d queues", device.Path, device.ID, numQueues)
	}

	return device, nil
}

// DeviceState represents the current state of a ublk device
type DeviceState string

const (
	// DeviceStateCreated indicates the device has been created but not started
	DeviceStateCreated DeviceState = "created"
	// DeviceStateRunning indicates the device is actively serving I/O
	DeviceStateRunning DeviceState = "running"
	// DeviceStateStopped indicates the device has been stopped
	DeviceStateStopped DeviceState = "stopped"
)

// State returns the current state of the device
func (d *Device) State() DeviceState {
	if d == nil {
		return DeviceStateStopped
	}

	if !d.started {
		return DeviceStateCreated
	}

	// Check if context is canceled (but only if context exists)
	if d.ctx != nil {
		select {
		case <-d.ctx.Done():
			return DeviceStateStopped
		default:
			return DeviceStateRunning
		}
	}

	return DeviceStateRunning
}

// IsRunning returns true if the device is currently serving I/O
func (d *Device) IsRunning() bool {
	return d.State() == DeviceStateRunning
}

// NumQueues returns the number of I/O queues configured for this device
func (d *Device) NumQueues() int {
	return d.queues
}

// QueueDepth returns the queue depth configured for this device
func (d *Device) QueueDepth() int {
	return d.depth
}

// BlockSize returns the logical block size of this device
func (d *Device) BlockSize() int {
	return d.blockSize
}

// BlockPath returns the path to the block device (e.g., "/dev/ublkb0")
func (d *Device) BlockPath() string {
	return d.Path
}

// CharDevicePath returns the path to the character device (e.g., "/dev/ublkc0")
func (d *Device) CharDevicePath() string {
	return d.CharPath
}

// DeviceID returns the kernel-assigned device ID
func (d *Device) DeviceID() uint32 {
	return d.ID
}

// Size returns the size of the device in bytes
func (d *Device) Size() int64 {
	if d.Backend == nil {
		return 0
	}
	return d.Backend.Size()
}

// DeviceInfo contains comprehensive information about a ublk device
type DeviceInfo struct {
	ID         uint32      `json:"id"`
	BlockPath  string      `json:"block_path"`
	CharPath   string      `json:"char_path"`
	State      DeviceState `json:"state"`
	NumQueues  int         `json:"num_queues"`
	QueueDepth int         `json:"queue_depth"`
	BlockSize  int         `json:"block_size"`
	Size       int64       `json:"size"`
	Running    bool        `json:"running"`
}

// Info returns comprehensive information about the device
func (d *Device) Info() DeviceInfo {
	if d == nil {
		return DeviceInfo{}
	}

	state := d.State()
	return DeviceInfo{
		ID:         d.ID,
		BlockPath:  d.Path,
		CharPath:   d.CharPath,
		State:      state,
		NumQueues:  d.queues,
		QueueDepth: d.depth,
		BlockSize:  d.blockSize,
		Size:       d.Size(),
		Running:    state == DeviceStateRunning,
	}
}

// Metrics returns the current metrics for the device
func (d *Device) Metrics() *Metrics {
	if d == nil {
		return nil
	}
	return d.metrics
}

// MetricsSnapshot returns a point-in-time snapshot of device metrics
func (d *Device) MetricsSnapshot() MetricsSnapshot {
	if d == nil || d.metrics == nil {
		return MetricsSnapshot{}
	}
	return d.metrics.Snapshot()
}

// StopAndDelete stops the device and removes it from the system.
// This should be called to cleanly shut down a ublk device.
func StopAndDelete(ctx context.Context, device *Device) error {
	if device == nil {
		return ErrInvalidParameters
	}

	// Cancel context first to signal all goroutines to stop
	if device.cancel != nil {
		device.cancel()
	}

	// Mark metrics as stopped
	if device.metrics != nil {
		device.metrics.Stop()
	}

	// Give goroutines a moment to see the cancellation
	time.Sleep(10 * time.Millisecond)

	// Stop and cleanup queue runners
	for _, runner := range device.runners {
		if runner != nil {
			runner.Close()
		}
	}
	device.runners = nil

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

// Error definitions moved to errors.go
