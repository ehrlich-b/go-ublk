// Package ublk provides the main API for creating userspace block devices
package ublk

import (
	"context"
	"fmt"
	"runtime"
	"syscall"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/ctrl"
	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/queue"
)

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
	closed    bool
	runners   []*queue.Runner

	// Configuration preserved for Start()
	params  DeviceParams
	options *Options

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
	deviceID, err := ctrl.AddDevice(&ctrlParams)
	if err != nil {
		return nil, fmt.Errorf("failed to add device: %v", err)
	}

	// Set parameters
	err = ctrl.SetParams(deviceID, &ctrlParams)
	if err != nil {
		_ = ctrl.DeleteDevice(deviceID) // Cleanup, ignore error
		return nil, fmt.Errorf("failed to set parameters: %v", err)
	}

	// Initialize metrics and observer
	metrics := NewMetrics()
	var observer Observer
	if options.Observer != nil {
		observer = options.Observer
	} else {
		// Default to metrics observer if no custom observer provided
		observer = NewMetricsObserver(metrics)
	}

	// Determine actual number of queues (default to number of CPUs)
	numQueues := params.NumQueues
	if numQueues == 0 {
		numQueues = runtime.NumCPU()
	}

	// Create Device struct
	device := &Device{
		ID:        deviceID,
		Path:      fmt.Sprintf("/dev/ublkb%d", deviceID),
		CharPath:  fmt.Sprintf("/dev/ublkc%d", deviceID),
		Backend:   params.Backend,
		queues:    numQueues, // Store actual queue count, not params value
		depth:     params.QueueDepth,
		blockSize: params.LogicalBlockSize,
		started:   false, // Not started yet
		metrics:   metrics,
		observer:  observer,
	}

	device.ctx, device.cancel = context.WithCancel(ctx)

	// Initialize and start queue runners before START_DEV
	// The kernel waits for initial FETCH_REQ commands from all queues
	// NOTE: The ublk character device can only be opened once (kernel enforces this)
	// so we open it once and share the fd among all queues (each queue dups it)
	logger := logging.Default()

	// Open character device once (kernel only allows single open)
	charPath := fmt.Sprintf("/dev/ublkc%d", deviceID)
	charDeviceFd := -1
	for i := 0; i < constants.CharDeviceOpenRetries; i++ { // Retry for up to 5s waiting for udev
		var err error
		charDeviceFd, err = syscall.Open(charPath, syscall.O_RDWR, 0)
		if err == nil {
			logger.Info("opened char device for multi-queue", "fd", charDeviceFd, "path", charPath)
			break
		}
		if err != syscall.ENOENT {
			return nil, fmt.Errorf("failed to open %s: %v", charPath, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if charDeviceFd < 0 {
		_ = ctrl.DeleteDevice(deviceID) // Cleanup, ignore error
		return nil, fmt.Errorf("character device did not appear: %s", charPath)
	}

	device.runners = make([]*queue.Runner, numQueues)
	for i := 0; i < numQueues; i++ {
		runnerConfig := queue.Config{
			DevID:       deviceID,
			QueueID:     uint16(i),
			Depth:       params.QueueDepth,
			BlockSize:   params.LogicalBlockSize,
			Backend:     params.Backend,
			Logger:      options.Logger,
			Observer:    observer,
			CPUAffinity: params.CPUAffinity,
			CharFd:      charDeviceFd, // Share the fd (runner will dup it)
		}

		runner, err := queue.NewRunner(device.ctx, runnerConfig)
		if err != nil {
			// Cleanup already created runners
			for j := 0; j < i; j++ {
				if device.runners[j] != nil {
					device.runners[j].Close()
				}
			}
			_ = ctrl.DeleteDevice(deviceID) // Cleanup, ignore error
			return nil, fmt.Errorf("failed to create queue runner %d: %v", i, err)
		}
		device.runners[i] = runner

		// Start this runner immediately (submit FETCH_REQs)
		// This must happen before creating the next queue
		if err := runner.Start(); err != nil {
			for j := 0; j <= i; j++ {
				if device.runners[j] != nil {
					device.runners[j].Close()
				}
			}
			_ = ctrl.DeleteDevice(deviceID) // Cleanup, ignore error
			return nil, fmt.Errorf("failed to start queue runner %d: %v", i, err)
		}
	}

	// Give kernel time to see FETCH_REQs
	time.Sleep(constants.QueueInitDelay)

	// Submit START_DEV after FETCH_REQs are in place
	err = ctrl.StartDevice(deviceID)
	if err != nil {
		for j := 0; j < len(device.runners); j++ {
			if device.runners[j] != nil {
				device.runners[j].Close()
			}
		}
		_ = ctrl.DeleteDevice(deviceID) // Cleanup, ignore error
		return nil, fmt.Errorf("failed to START_DEV: %v", err)
	}

	device.started = true

	// Small delay to ensure kernel has processed FETCH_REQs before declaring ready
	// The 250ms was too long, but there's a real race condition that needs timing
	time.Sleep(1 * time.Millisecond) // Minimal delay instead of 250ms * queue_depth
	logger.Info("device initialization complete")

	if options.Logger != nil {
		options.Logger.Printf("Device created: %s (ID: %d) with %d queues", device.Path, device.ID, numQueues)
	}

	return device, nil
}

// Create creates a ublk device without starting I/O processing.
// Use this when you need more control over the device lifecycle.
// After Create, call Start() to begin serving I/O, Stop() to pause,
// and Close() for full cleanup.
//
// Example:
//
//	device, err := ublk.Create(params, options)
//	if err != nil {
//	    return err
//	}
//	defer device.Close()
//
//	if err := device.Start(ctx); err != nil {
//	    return err
//	}
//	// Device is now serving I/O
func Create(params DeviceParams, options *Options) (*Device, error) {
	if options == nil {
		options = &Options{}
	}

	// Create controller
	controller, err := createController()
	if err != nil {
		return nil, fmt.Errorf("failed to create controller: %v", err)
	}
	defer controller.Close()

	// Convert params to internal format
	ctrlParams := convertToCtrlParams(params)

	// Create device using control plane
	deviceID, err := controller.AddDevice(&ctrlParams)
	if err != nil {
		return nil, fmt.Errorf("failed to add device: %v", err)
	}

	// Set parameters
	err = controller.SetParams(deviceID, &ctrlParams)
	if err != nil {
		_ = controller.DeleteDevice(deviceID) // Cleanup, ignore error
		return nil, fmt.Errorf("failed to set parameters: %v", err)
	}

	// Initialize metrics and observer
	metrics := NewMetrics()
	var observer Observer
	if options.Observer != nil {
		observer = options.Observer
	} else {
		observer = NewMetricsObserver(metrics)
	}

	// Determine actual number of queues (default to number of CPUs)
	numQueues := params.NumQueues
	if numQueues == 0 {
		numQueues = runtime.NumCPU()
	}

	// Create Device struct
	device := &Device{
		ID:        deviceID,
		Path:      fmt.Sprintf("/dev/ublkb%d", deviceID),
		CharPath:  fmt.Sprintf("/dev/ublkc%d", deviceID),
		Backend:   params.Backend,
		queues:    numQueues,
		depth:     params.QueueDepth,
		blockSize: params.LogicalBlockSize,
		started:   false,
		closed:    false,
		params:    params,
		options:   options,
		metrics:   metrics,
		observer:  observer,
	}

	if options.Logger != nil {
		options.Logger.Printf("Device created: %s (ID: %d) - call Start() to begin I/O", device.Path, device.ID)
	}

	return device, nil
}

// Start begins serving I/O requests for a device created with Create().
// The context controls the lifetime of I/O processing.
// Returns an error if the device is already started or has been closed.
func (d *Device) Start(ctx context.Context) error {
	if d == nil {
		return ErrInvalidParameters
	}
	if d.closed {
		return fmt.Errorf("device is closed")
	}
	if d.started {
		return fmt.Errorf("device is already started")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	d.ctx, d.cancel = context.WithCancel(ctx)

	// Open character device once (kernel only allows single open)
	// Share the fd among all queues (each queue dups it)
	logger := logging.Default()
	charPath := fmt.Sprintf("/dev/ublkc%d", d.ID)
	charDeviceFd := -1
	for i := 0; i < constants.CharDeviceOpenRetries; i++ {
		var err error
		charDeviceFd, err = syscall.Open(charPath, syscall.O_RDWR, 0)
		if err == nil {
			logger.Info("opened char device for multi-queue", "fd", charDeviceFd, "path", charPath)
			break
		}
		if err != syscall.ENOENT {
			return fmt.Errorf("failed to open %s: %v", charPath, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if charDeviceFd < 0 {
		return fmt.Errorf("character device did not appear: %s", charPath)
	}

	// Initialize queue runners
	d.runners = make([]*queue.Runner, d.queues)
	for i := 0; i < d.queues; i++ {
		runnerConfig := queue.Config{
			DevID:       d.ID,
			QueueID:     uint16(i),
			Depth:       d.depth,
			BlockSize:   d.blockSize,
			Backend:     d.Backend,
			Logger:      d.options.Logger,
			Observer:    d.observer,
			CPUAffinity: d.params.CPUAffinity,
			CharFd:      charDeviceFd, // Share the fd (runner will dup it)
		}

		runner, err := queue.NewRunner(d.ctx, runnerConfig)
		if err != nil {
			// Cleanup already created runners
			for j := 0; j < i; j++ {
				if d.runners[j] != nil {
					d.runners[j].Close()
				}
			}
			d.runners = nil
			return fmt.Errorf("failed to create queue runner %d: %v", i, err)
		}
		d.runners[i] = runner
	}

	// Start queue runners and submit FETCH_REQs before START_DEV
	for i := 0; i < d.queues; i++ {
		if err := d.runners[i].Start(); err != nil {
			for j := 0; j < len(d.runners); j++ {
				if d.runners[j] != nil {
					d.runners[j].Close()
				}
			}
			d.runners = nil
			return fmt.Errorf("failed to start queue runner %d: %v", i, err)
		}
	}

	// Give kernel time to see FETCH_REQs
	time.Sleep(constants.QueueInitDelay)

	// Create temporary controller for START_DEV
	controller, err := createController()
	if err != nil {
		for j := 0; j < len(d.runners); j++ {
			if d.runners[j] != nil {
				d.runners[j].Close()
			}
		}
		d.runners = nil
		return fmt.Errorf("failed to create controller for start: %v", err)
	}
	defer controller.Close()

	// Submit START_DEV after FETCH_REQs are in place
	err = controller.StartDevice(d.ID)
	if err != nil {
		for j := 0; j < len(d.runners); j++ {
			if d.runners[j] != nil {
				d.runners[j].Close()
			}
		}
		d.runners = nil
		return fmt.Errorf("failed to START_DEV: %v", err)
	}

	d.started = true

	// Small delay to ensure kernel has processed FETCH_REQs
	time.Sleep(1 * time.Millisecond)
	logger.Info("device started")

	if d.options.Logger != nil {
		d.options.Logger.Printf("Device %s started with %d queues", d.Path, d.queues)
	}

	return nil
}

// Stop stops I/O processing but keeps the device registered with the kernel.
// Call Close() for full cleanup, or Start() to resume I/O processing.
// Returns an error if the device is not started or has been closed.
func (d *Device) Stop() error {
	if d == nil {
		return ErrInvalidParameters
	}
	if d.closed {
		return fmt.Errorf("device is closed")
	}
	if !d.started {
		return fmt.Errorf("device is not started")
	}

	// Cancel context to signal goroutines to stop
	if d.cancel != nil {
		d.cancel()
	}

	// Mark metrics as stopped
	if d.metrics != nil {
		d.metrics.Stop()
	}

	// Give goroutines a moment to see the cancellation
	time.Sleep(10 * time.Millisecond)

	// Stop queue runners
	for _, runner := range d.runners {
		if runner != nil {
			runner.Close()
		}
	}
	d.runners = nil

	// Create controller to stop device
	controller, err := createController()
	if err != nil {
		return fmt.Errorf("failed to create controller for stop: %v", err)
	}
	defer controller.Close()

	// Stop device in kernel (device stays registered)
	err = controller.StopDevice(d.ID)
	if err != nil {
		return fmt.Errorf("failed to stop device: %v", err)
	}

	d.started = false

	if d.options != nil && d.options.Logger != nil {
		d.options.Logger.Printf("Device %s stopped", d.Path)
	}

	return nil
}

// Close performs full cleanup: stops I/O (if running) and removes the device.
// After Close(), the device cannot be reused.
func (d *Device) Close() error {
	if d == nil {
		return ErrInvalidParameters
	}
	if d.closed {
		return nil // Already closed, idempotent
	}

	// Stop first if running
	if d.started {
		// Cancel context
		if d.cancel != nil {
			d.cancel()
		}

		// Mark metrics as stopped
		if d.metrics != nil {
			d.metrics.Stop()
		}

		time.Sleep(10 * time.Millisecond)

		// Stop queue runners
		for _, runner := range d.runners {
			if runner != nil {
				runner.Close()
			}
		}
		d.runners = nil
		d.started = false
	}

	// Create controller for cleanup
	controller, err := createController()
	if err != nil {
		return fmt.Errorf("failed to create controller for close: %v", err)
	}
	defer controller.Close()

	// Stop device if not already stopped
	// Ignore error here - device might already be stopped
	_ = controller.StopDevice(d.ID)

	// Delete device from kernel
	err = controller.DeleteDevice(d.ID)
	if err != nil {
		return fmt.Errorf("failed to delete device: %v", err)
	}

	d.closed = true

	if d.options != nil && d.options.Logger != nil {
		d.options.Logger.Printf("Device %s closed", d.Path)
	}

	return nil
}

// DeviceState represents the current state of a ublk device
type DeviceState string

const (
	// DeviceStateCreated indicates the device has been created but not started
	DeviceStateCreated DeviceState = "created"
	// DeviceStateRunning indicates the device is actively serving I/O
	DeviceStateRunning DeviceState = "running"
	// DeviceStateStopped indicates the device has been stopped but is still registered
	DeviceStateStopped DeviceState = "stopped"
	// DeviceStateClosed indicates the device has been fully closed and removed
	DeviceStateClosed DeviceState = "closed"
)

// State returns the current state of the device
func (d *Device) State() DeviceState {
	if d == nil {
		return DeviceStateClosed
	}

	if d.closed {
		return DeviceStateClosed
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
