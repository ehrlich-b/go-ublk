// Package ublk provides the main API for creating userspace block devices
package ublk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/ctrl"
	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/queue"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// Device represents a ublk block device
type Device struct {
	// ID is the device ID assigned by the kernel
	ID uint32

	// Path is the path to the block device (e.g., "/dev/ublkb0")
	Path string

	// CharPath is the path to the character device (e.g., "/dev/ublkc0")
	CharPath string

	// Backend is the backend implementation (nil when the device was
	// created with a Handler)
	Backend Backend

	mu        sync.Mutex
	state     DeviceState
	queues    int
	depth     int
	blockSize int
	maxIO     int
	flags     uint64 // negotiated UBLK_F_* features
	charFd    int
	runners   []*queue.Queue
	handler   Handler
	unwatch   chan struct{} // closed to stop the supervisors
	// leaving is set before an orderly stop or detach is asked of the
	// kernel, whose queues may then exit before the request returns; the
	// supervisors must not mistake that for a failure.
	leaving atomic.Bool

	// done is closed and err set (at most once) when serving ends.
	doneOnce sync.Once
	done     chan struct{}
	err      error

	params  DeviceParams
	options *Options

	metrics  *Metrics
	observer Observer
}

// DeviceParams contains parameters for creating a ublk device
type DeviceParams struct {
	// Backend provides the storage implementation. Exactly one of Backend and
	// Handler must be set.
	Backend Backend

	// Handler serves raw requests instead of a Backend, with access to every
	// operation and flag the kernel sends (zoned operations, FUA, NOUNMAP, ...)
	// and the ability to complete asynchronously. Size must be set with it.
	Handler Handler

	// Size is the device size in bytes. Required with Handler; with a Backend,
	// zero means Backend.Size().
	Size int64

	// Device configuration
	QueueDepth       int // Queue depth per queue (default: 128)
	NumQueues        int // Number of queues (default: number of CPUs)
	LogicalBlockSize int // Logical block size in bytes (default: 512)

	// MaxIOSize is the maximum request size in bytes (default: 1 MiB).
	// It must be page-aligned, at least one page, a multiple of LogicalBlockSize,
	// and no larger than math.MaxInt32. Each queue maps QueueDepth * MaxIOSize
	// bytes, using the capacity returned by the kernel at device creation.
	MaxIOSize int

	// Inline calls the backend on each queue's own I/O thread instead of a
	// goroutine per request. It has the lowest latency, but a queue then
	// serves one request at a time, so it suits only backends that never block
	// (RAM). The default runs every request on its own goroutine, so up to
	// QueueDepth requests per queue are in flight at once.
	Inline bool

	// Recovery makes the block device survive its server: see RecoveryMode,
	// Device.Detach and Recover.
	Recovery RecoveryMode

	// Feature flags
	EnableZeroCopy     bool // Serve requests zero-copy against a ZeroCopyBackend's file (kernel 6.15+)
	EnableUnprivileged bool // Create the device as an unprivileged user (UBLK_F_UNPRIVILEGED_DEV)
	EnableUserCopy     bool // Move data with pread/pwrite on /dev/ublkcN (UBLK_F_USER_COPY)
	EnableZoned        bool // A zoned block device served by a Handler (kernel 6.6+); see Zoned
	// Deprecated: ioctl-encoded commands are always used; this has no effect.
	EnableIoctlEncode bool

	// Zoned configures a zoned device (EnableZoned). The Handler serves the
	// zone operations (OpZoneOpen ... OpZoneReset, OpZoneAppend with
	// CompleteZoneAppend, OpReportZones with Request.ReportZones). Zoned
	// devices always use user copy.
	Zoned ZonedParams

	// NeedGetData makes the kernel ask for a write's buffer before copying its
	// data (UBLK_F_NEED_GET_DATA). Supported for completeness; it costs a round
	// trip per write and buys nothing with go-ublk's fixed per-tag buffers.
	NeedGetData bool

	// NoPartitionScan stops the kernel scanning the device for a partition
	// table when it starts (UBLK_F_NO_AUTO_PART_SCAN, kernel 7.0+).
	NoPartitionScan bool

	// ThreadsPerQueue splits each queue's tags across this many I/O threads,
	// each with its own io_uring (UBLK_F_PER_IO_DAEMON, kernel 6.16+). 0 or 1
	// means one thread per queue.
	ThreadsPerQueue int

	// SafeStop makes Stop and Close use TRY_STOP_DEV, which refuses
	// (ErrDeviceBusy) while the block device is open, instead of STOP_DEV,
	// which stops it regardless (kernel 7.0+).
	SafeStop bool

	// Tag is stored by the kernel with the device (ublksrv_flags) and returned
	// by GetDeviceInfo, so a restarted process can find its devices with
	// FindDevices. The kernel never interprets it.
	Tag uint64

	// Device attributes
	ReadOnly      bool // Make device read-only
	Rotational    bool // Device is rotational (HDD-like)
	VolatileCache bool // Device has volatile cache
	// EnableFUA advertises Force Unit Access. It is honored only with a
	// Handler (which must act on FlagFUA) or a backend implementing
	// FUABackend; with a plain Backend it is ignored and the block layer
	// emulates FUA with a flush instead. Requires VolatileCache.
	EnableFUA bool

	// Discard parameters (only used if backend implements DiscardBackend)
	DiscardAlignment   uint32 // Discard alignment
	DiscardGranularity uint32 // Discard granularity
	MaxDiscardSectors  uint32 // Max sectors per discard
	MaxDiscardSegments uint16 // Max segments per discard

	// HandlerDiscard and HandlerWriteZeroes advertise discard and
	// write-zeroes for a Handler (a Backend advertises them by implementing
	// DiscardBackend and WriteZeroesBackend).
	HandlerDiscard     bool
	HandlerWriteZeroes bool

	// Optional geometry hints, in bytes; zero derives them from
	// LogicalBlockSize.
	PhysicalBlockSize int
	IOMinSize         int
	IOOptSize         int
	// DMAAlignment is the buffer alignment mask the device needs (kernel
	// 6.15+), e.g. 511 for 512-byte alignment. Zero leaves the default.
	DMAAlignment uint32

	// Advanced options
	DeviceID int32 // Specific device ID to request (-1 for auto)
	// Deprecated: ublk devices have no names; this has no effect.
	DeviceName  string
	CPUAffinity []int // CPU affinity mask for queue threads
}

// ZonedParams describes a zoned device's zones.
type ZonedParams struct {
	// ZoneSize is the size of every zone in bytes: a power of two and a
	// multiple of LogicalBlockSize. The device size must be a multiple of it.
	ZoneSize int64
	// MaxOpenZones and MaxActiveZones limit open and active zones (0: no
	// limit).
	MaxOpenZones, MaxActiveZones uint32
	// MaxZoneAppendSize is the largest zone append in bytes (0: MaxIOSize).
	MaxZoneAppendSize int
}

// RecoveryMode selects what happens to a device whose server exits without
// deleting it (a crash, or Device.Detach). With any mode but RecoveryNone the
// block device stays, and a new server process takes it over with Recover.
type RecoveryMode int

const (
	// RecoveryNone deletes the device's disk when its server exits; I/O fails.
	RecoveryNone RecoveryMode = iota
	// RecoveryReissue requeues the server's outstanding I/O and holds new I/O
	// until Recover; outstanding requests are reissued to the new server
	// (UBLK_F_USER_RECOVERY | UBLK_F_USER_RECOVERY_REISSUE). The backend may
	// see a write twice, which block semantics allow.
	RecoveryReissue
	// RecoveryQueue fails the server's outstanding I/O but holds new I/O until
	// Recover (UBLK_F_USER_RECOVERY).
	RecoveryQueue
	// RecoveryFailIO fails outstanding and new I/O until Recover
	// (UBLK_F_USER_RECOVERY | UBLK_F_USER_RECOVERY_FAIL_IO, kernel 6.13+).
	RecoveryFailIO
)

func (m RecoveryMode) flags() uint64 {
	switch m {
	case RecoveryReissue:
		return uapi.UBLK_F_USER_RECOVERY | uapi.UBLK_F_USER_RECOVERY_REISSUE
	case RecoveryQueue:
		return uapi.UBLK_F_USER_RECOVERY
	case RecoveryFailIO:
		return uapi.UBLK_F_USER_RECOVERY | uapi.UBLK_F_USER_RECOVERY_FAIL_IO
	}
	return 0
}

func (p *DeviceParams) size() int64 {
	if p.Size > 0 || p.Backend == nil {
		return p.Size
	}
	return p.Backend.Size()
}

// validateParams rejects parameters the kernel or the data plane cannot honor,
// so a caller finds out at creation time rather than from a device that fails
// in a confusing way later.
func validateParams(params *DeviceParams) error {
	switch {
	case params.Backend == nil && params.Handler == nil:
		return fmt.Errorf("Backend is nil")
	case params.Backend != nil && params.Handler != nil:
		return fmt.Errorf("set Backend or Handler, not both")
	}
	if params.EnableZeroCopy {
		zc, ok := params.Backend.(ZeroCopyBackend)
		if !ok {
			return fmt.Errorf("EnableZeroCopy needs a Backend that implements ZeroCopyBackend")
		}
		if params.EnableUserCopy || params.NeedGetData || params.EnableUnprivileged {
			return fmt.Errorf("EnableZeroCopy cannot be combined with EnableUserCopy, NeedGetData or EnableUnprivileged")
		}
		fd, base := zc.ZeroCopyFile()
		var st syscall.Stat_t
		if err := syscall.Fstat(fd, &st); err != nil {
			return fmt.Errorf("ZeroCopyFile: %w", err)
		}
		if st.Mode&syscall.S_IFMT == syscall.S_IFREG && st.Size < base+params.size() {
			return fmt.Errorf("ZeroCopyFile is %d bytes; it must cover base %d + size %d", st.Size, base, params.size())
		}
	}
	if params.EnableZoned {
		z := params.Zoned
		if params.Handler == nil {
			return fmt.Errorf("EnableZoned needs a Handler: a Backend cannot serve zone operations")
		}
		if z.ZoneSize <= 0 || z.ZoneSize&(z.ZoneSize-1) != 0 || z.ZoneSize%int64(params.LogicalBlockSize) != 0 {
			return fmt.Errorf("Zoned.ZoneSize is %d; must be a power of two and a multiple of LogicalBlockSize", z.ZoneSize)
		}
		if params.Size%z.ZoneSize != 0 {
			return fmt.Errorf("Size %d is not a multiple of Zoned.ZoneSize %d", params.Size, z.ZoneSize)
		}
		if params.EnableZeroCopy || params.EnableUnprivileged {
			return fmt.Errorf("EnableZoned cannot be combined with EnableZeroCopy or EnableUnprivileged")
		}
	}
	if params.Recovery < RecoveryNone || params.Recovery > RecoveryFailIO {
		return fmt.Errorf("Recovery is %d; not a RecoveryMode", params.Recovery)
	}
	if params.ThreadsPerQueue < 0 {
		return fmt.Errorf("ThreadsPerQueue is %d; must not be negative", params.ThreadsPerQueue)
	}

	// The kernel requires 9 <= logical_bs_shift <= PAGE_SHIFT, i.e. a power of
	// two from one sector up to one page.
	page := os.Getpagesize()
	bs := params.LogicalBlockSize
	if bs < uapi.SectorSize || bs > page || bs&(bs-1) != 0 {
		return fmt.Errorf("LogicalBlockSize is %d; must be a power of two from %d to the page size (%d)",
			bs, uapi.SectorSize, page)
	}
	for _, g := range []struct {
		name string
		v    int
	}{{"PhysicalBlockSize", params.PhysicalBlockSize}, {"IOMinSize", params.IOMinSize}, {"IOOptSize", params.IOOptSize}} {
		if g.v != 0 && (g.v < bs || g.v&(g.v-1) != 0) {
			return fmt.Errorf("%s is %d; must be 0 or a power of two no smaller than LogicalBlockSize %d", g.name, g.v, bs)
		}
	}

	if params.QueueDepth < 1 || params.QueueDepth > uapi.UBLK_MAX_QUEUE_DEPTH {
		return fmt.Errorf("QueueDepth is %d; must be from 1 to %d",
			params.QueueDepth, uapi.UBLK_MAX_QUEUE_DEPTH)
	}
	if params.NumQueues < 0 || params.NumQueues > uapi.UBLK_MAX_NR_QUEUES {
		return fmt.Errorf("NumQueues is %d; must be from 0 to %d",
			params.NumQueues, uapi.UBLK_MAX_NR_QUEUES)
	}

	// max_sectors is derived from MaxIOSize. ADD_DEV rounds the advertised
	// buffer size down to a page, while completions return a signed byte count.
	if params.MaxIOSize < page {
		return fmt.Errorf("MaxIOSize is %d; must be at least the page size (%d)", params.MaxIOSize, page)
	}
	if params.MaxIOSize%page != 0 {
		return fmt.Errorf("MaxIOSize %d is not a multiple of the page size %d", params.MaxIOSize, page)
	}
	if params.MaxIOSize > math.MaxInt32 {
		return fmt.Errorf("MaxIOSize %d exceeds the signed completion result limit %d",
			params.MaxIOSize, math.MaxInt32)
	}
	if params.MaxIOSize%bs != 0 {
		return fmt.Errorf("MaxIOSize %d is not a multiple of LogicalBlockSize %d", params.MaxIOSize, bs)
	}
	maxInt := int(^uint(0) >> 1)
	if params.MaxIOSize > maxInt/params.QueueDepth {
		return fmt.Errorf("queue buffer allocation overflows int: depth %d, MaxIOSize %d",
			params.QueueDepth, params.MaxIOSize)
	}

	// Capacity is reported in whole sectors, so a tail shorter than a block
	// would be addressable by the kernel but outside the backend.
	if params.Handler != nil && params.Size <= 0 {
		return fmt.Errorf("Size is %d; a Handler device needs a positive Size", params.Size)
	}
	if size := params.size(); size <= 0 || size%int64(bs) != 0 {
		return fmt.Errorf("Backend.Size() is %d; must be positive and a multiple of LogicalBlockSize %d",
			size, bs)
	}

	return nil
}

func applyNegotiatedDeviceInfo(
	params *DeviceParams, ctrlParams *ctrl.DeviceParams, info *uapi.UblksrvCtrlDevInfo,
) error {
	if info.NrHwQueues == 0 || info.QueueDepth == 0 {
		return fmt.Errorf("ADD_DEV returned unusable queue configuration: %d queues, depth %d",
			info.NrHwQueues, info.QueueDepth)
	}
	if info.MaxIOBufBytes > math.MaxInt32 {
		return fmt.Errorf("ADD_DEV returned MaxIOBufBytes %d above signed completion limit %d",
			info.MaxIOBufBytes, math.MaxInt32)
	}
	params.NumQueues = int(info.NrHwQueues)
	params.QueueDepth = int(info.QueueDepth)
	params.MaxIOSize = int(info.MaxIOBufBytes)
	if err := validateParams(params); err != nil {
		return fmt.Errorf("ADD_DEV returned unusable parameters: %w", err)
	}
	ctrlParams.NumQueues = params.NumQueues
	ctrlParams.QueueDepth = params.QueueDepth
	ctrlParams.MaxIOSize = params.MaxIOSize
	return nil
}

// DefaultParams returns default device parameters
func DefaultParams(backend Backend) DeviceParams {
	return DeviceParams{
		Backend:          backend,
		QueueDepth:       constants.DefaultQueueDepth,
		NumQueues:        0, // 0 means auto-detect based on CPUs
		LogicalBlockSize: constants.DefaultLogicalBlockSize,
		MaxIOSize:        constants.DefaultMaxIOSize,

		ReadOnly:   false,
		Rotational: false, // SSD-like by default
		// Default to advertising a volatile write cache. The library cannot know
		// whether a backend's completed write is durable, and the two mistakes
		// are not symmetric: claiming a cache that does not exist costs a no-op
		// flush round-trip, while hiding one that does exist loses data on power
		// failure with no error anywhere. A backend that makes every write
		// durable before returning should set this to false, which also stops
		// the kernel from sending flushes it does not need.
		VolatileCache: true,

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
	// Context, if set, is used in place of the context passed to
	// CreateAndServe.
	Context context.Context

	// Logger receives the library's log output, including the internal
	// control-plane and queue diagnostics. If nil the library logs nothing;
	// every failure is reported through returned errors and Device.Err.
	Logger Logger

	// Debug enables debug-level logging to Logger. The data plane's hot loop
	// does not log at all, but debug logging elsewhere affects timing.
	Debug bool

	// Observer for metrics collection (if nil, Device.Metrics is fed)
	Observer Observer

	// StopTimeout bounds how long Stop and Close wait for STOP_DEV, which
	// first drains in-flight I/O through the backend, and for DEL_DEV. Zero
	// means one minute.
	StopTimeout time.Duration
}

// loggerWriter adapts a caller's Logger to the io.Writer the internal logger
// writes lines to, so Options.Logger receives the internal diagnostics instead
// of only the handful of messages this file emits directly.
type loggerWriter struct{ l Logger }

func (w loggerWriter) Write(p []byte) (int, error) {
	w.l.Printf("%s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func init() {
	// Silent unless a caller supplies a Logger.
	cfg := logging.DefaultConfig()
	cfg.Output = io.Discard
	logging.SetDefault(logging.NewLogger(cfg))
}

// configureLogging points the library's package-level logger at the caller's
// Logger and level. It is process-wide: with several devices, the most recent
// Options.Logger wins for control-plane diagnostics. Each device's queues log
// to their own Options.Logger.
func configureLogging(options *Options) {
	if options.Logger == nil {
		return
	}
	config := logging.DefaultConfig()
	if options.Debug {
		config.Level = logging.LevelDebug
	}
	config.Output = loggerWriter{options.Logger}
	config.NoTimestamp = true // the caller's logger stamps its own lines
	logging.SetDefault(logging.NewLogger(config))
}

// startTimeout bounds START_DEV and END_USER_RECOVERY, which wait for every
// queue's FETCH_REQs; the engines submit those before the command is sent.
const startTimeout = 30 * time.Second

// CreateAndServe creates a ublk device with the given parameters and starts serving I/O.
// This is the main entry point for creating ublk devices.
//
// The device serves I/O until Close (or Stop) is called or ctx is cancelled.
// Cancelling ctx stops the device gracefully, exactly like Stop: in-flight I/O
// is completed first. Call Close afterwards to delete it.
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
	if options != nil && options.Context != nil {
		ctx = options.Context
	}
	d, err := Create(params, options)
	if err != nil {
		return nil, err
	}
	if err := d.Start(ctx); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

// Create creates a ublk device without starting I/O processing.
// Use this when you need more control over the device lifecycle.
// After Create, call Start() to begin serving I/O and Close() for full
// cleanup.
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
	if err := validateParams(&params); err != nil {
		return nil, err
	}
	configureLogging(options)

	controller, err := createController()
	if err != nil {
		return nil, fmt.Errorf("failed to create controller: %w", err)
	}
	defer controller.Close()

	ctx := context.Background()
	ctrlParams := convertToCtrlParams(params)
	// Opportunistic features: requested only when the kernel says it has
	// them, so asking never makes creation fail. UPDATE_SIZE enables Resize;
	// QUIESCE lets Detach drain in-flight I/O first (it needs USER_RECOVERY);
	// AUTO_BUF_REG saves zero copy two commands per request.
	if fs, err := controller.Features(ctx); err == nil && fs.Known {
		if fs.Has(uapi.UBLK_F_UPDATE_SIZE) {
			ctrlParams.Flags |= uapi.UBLK_F_UPDATE_SIZE
		}
		if params.Recovery != RecoveryNone && fs.Has(uapi.UBLK_F_QUIESCE) {
			ctrlParams.Flags |= uapi.UBLK_F_QUIESCE
		}
		if params.EnableZeroCopy && fs.Has(uapi.UBLK_F_AUTO_BUF_REG) {
			ctrlParams.Flags |= uapi.UBLK_F_AUTO_BUF_REG
		}
	}
	deviceInfo, err := controller.AddDevice(ctx, &ctrlParams)
	if err != nil {
		return nil, fmt.Errorf("failed to add device: %w", err)
	}
	deviceID := deviceInfo.DevID
	if err := applyNegotiatedDeviceInfo(&params, &ctrlParams, deviceInfo); err != nil {
		_ = controller.DelDev(ctx, deviceID)
		return nil, err
	}
	if err := controller.SetDeviceParams(ctx, deviceID, &ctrlParams); err != nil {
		_ = controller.DelDev(ctx, deviceID)
		return nil, fmt.Errorf("failed to set parameters: %w", err)
	}

	d := newDevice(deviceID, params, options, deviceInfo.Flags)
	if options.Logger != nil {
		options.Logger.Printf("Device created: %s (ID: %d) - call Start() to begin I/O", d.Path, d.ID)
	}
	return d, nil
}

func newDevice(id uint32, params DeviceParams, options *Options, flags uint64) *Device {
	metrics := NewMetrics()
	observer := options.Observer
	if observer == nil {
		observer = NewMetricsObserver(metrics)
	}
	handler := params.Handler
	if handler == nil {
		handler = queue.BackendHandler(params.Backend, observer)
	}
	return &Device{
		ID:        id,
		Path:      fmt.Sprintf("/dev/ublkb%d", id),
		CharPath:  fmt.Sprintf("/dev/ublkc%d", id),
		Backend:   params.Backend,
		state:     DeviceStateCreated,
		queues:    params.NumQueues,
		depth:     params.QueueDepth,
		blockSize: params.LogicalBlockSize,
		maxIO:     params.MaxIOSize,
		flags:     flags,
		charFd:    -1,
		handler:   handler,
		done:      make(chan struct{}),
		params:    params,
		options:   options,
		metrics:   metrics,
		observer:  observer,
	}
}

// ErrStopped is returned by Start for a device that has been stopped: the
// kernel cannot reliably restart a stopped ublk device (some kernels oops), so
// a stopped device must be closed and a new one created.
var ErrStopped = errors.New("ublk: device was stopped; close it and create a new one")

// Start begins serving I/O requests for a device created with Create().
// Cancelling ctx later stops the device gracefully, like Stop.
// Returns an error if the device is already started, stopped or closed.
func (d *Device) Start(ctx context.Context) error {
	if d == nil {
		return ErrInvalidParameters
	}
	if ctx == nil {
		ctx = context.Background()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch d.state {
	case DeviceStateCreated:
	case DeviceStateRunning:
		return fmt.Errorf("device is already started")
	case DeviceStateStopped:
		return ErrStopped
	default:
		return fmt.Errorf("device is %s", d.state)
	}
	if d.runners != nil {
		return fmt.Errorf("a previous Start failed; close the device and create a new one")
	}

	controller, err := createController()
	if err != nil {
		return fmt.Errorf("failed to create controller for start: %w", err)
	}
	defer controller.Close()

	if err := d.startQueues(); err != nil {
		return err
	}
	sctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	if err := controller.StartDev(sctx, d.ID, os.Getpid()); err != nil {
		d.abandonQueues()
		return fmt.Errorf("failed to START_DEV: %w", err)
	}
	d.state = DeviceStateRunning
	d.watch(ctx)
	if d.options.Logger != nil {
		d.options.Logger.Printf("Device %s started with %d queues", d.Path, d.queues)
	}
	return nil
}

// startQueues opens /dev/ublkcN and starts every queue's engines, each of
// which has submitted a FETCH_REQ for every tag when this returns.
func (d *Device) startQueues() error {
	fd, err := openCharDevice(d.CharPath)
	if err != nil {
		return err
	}
	d.charFd = fd
	threads := d.params.ThreadsPerQueue
	if threads > 1 && d.flags&uapi.UBLK_F_PER_IO_DAEMON == 0 {
		threads = 1
	}
	for i := 0; i < d.queues; i++ {
		cpu := -1
		if n := len(d.params.CPUAffinity); n > 0 {
			cpu = d.params.CPUAffinity[i%n]
		}
		zcFile, zcBase := -1, int64(0)
		if zc, ok := d.params.Backend.(ZeroCopyBackend); ok && d.flags&uapi.UBLK_F_SUPPORT_ZERO_COPY != 0 {
			zcFile, zcBase = zc.ZeroCopyFile()
		}
		q, err := queue.NewQueue(queue.QueueConfig{
			QueueID:      uint16(i),
			Depth:        d.depth,
			MaxIOSize:    d.maxIO,
			CharFd:       fd,
			Flags:        d.flags,
			Handler:      d.handler,
			Inline:       d.params.Inline,
			Threads:      threads,
			CPU:          cpu,
			Logger:       d.options.Logger,
			ZeroCopyFile: zcFile,
			ZeroCopyBase: zcBase,
		})
		if err == nil {
			d.runners = append(d.runners, q)
			err = q.Start()
		}
		if err != nil {
			d.abandonQueues()
			return fmt.Errorf("failed to start queue %d: %w", i, err)
		}
	}
	return nil
}

// openCharDevice opens /dev/ublkcN, waiting briefly for the node to appear and
// for a previous holder's release to finish (the kernel releases the device
// asynchronously after the last close).
func openCharDevice(path string) (int, error) {
	var err error
	for i := 0; i < constants.CharDeviceOpenRetries; i++ {
		var fd int
		fd, err = syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
		if err != syscall.ENOENT && err != syscall.EBUSY {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return -1, fmt.Errorf("failed to open %s: %w", path, err)
}

// abandonQueues tears down the queues of a device that never went live, or
// whose server is leaving without stopping it. Engines exit after handing
// back what their handlers hold; their memory is freed only if they exited.
func (d *Device) abandonQueues() error {
	for _, q := range d.runners {
		q.Abandon()
	}
	return d.releaseQueues(10 * time.Second)
}

// releaseQueues waits for every queue to finish and frees what it safely can.
// It reports an error if a queue did not exit; that queue's memory is leaked
// rather than freed under a live engine or handler (Critical Bug #19).
func (d *Device) releaseQueues(timeout time.Duration) error {
	var errs []error
	deadline := time.Now().Add(timeout)
	for i, q := range d.runners {
		if !q.Wait(time.Until(deadline)) {
			q.Abandon()
			if !q.Wait(5 * time.Second) {
				errs = append(errs, fmt.Errorf("queue %d did not exit", i))
				continue
			}
		}
		if err := q.Close(); err != nil {
			errs = append(errs, fmt.Errorf("queue %d: %w", i, err))
		}
	}
	if len(errs) == 0 {
		d.runners = nil
		if d.charFd >= 0 {
			// Every engine has closed its ring, so this is the last reference:
			// the kernel now releases the device.
			_ = syscall.Close(d.charFd)
			d.charFd = -1
		}
	}
	return errors.Join(errs...)
}

// watch supervises a live device: a queue that dies makes the device fail
// (Err, Done), and cancelling ctx stops it gracefully.
func (d *Device) watch(ctx context.Context) {
	d.unwatch = make(chan struct{})
	unwatch := d.unwatch
	for i, q := range d.runners {
		go func(i int, q *queue.Queue) {
			select {
			case <-q.Done():
			case <-unwatch:
				return
			}
			select {
			case <-unwatch: // an orderly Stop or Detach is under way
			default:
				if d.leaving.Load() && q.Err() == nil {
					return
				}
				err := q.Err()
				if err == nil {
					err = fmt.Errorf("queue %d stopped unexpectedly", i)
				}
				d.finish(fmt.Errorf("ublk device %d failed: %w", d.ID, err))
			}
		}(i, q)
	}
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = d.Stop()
			case <-unwatch:
			}
		}()
	}
}

// finish records why serving ended and closes Done, once.
func (d *Device) finish(err error) {
	d.doneOnce.Do(func() {
		d.err = err
		close(d.done)
	})
}

func (d *Device) stopTimeout() time.Duration {
	if d.options != nil && d.options.StopTimeout > 0 {
		return d.options.StopTimeout
	}
	return time.Minute
}

// Stop stops the device: the kernel drains in-flight I/O through the still
// running queues, removes the block device, and the queues exit. The device
// stays registered until Close. A stopped device cannot be started again.
// If the kernel refuses to stop (SafeStop with the device open, or a timeout)
// the error is returned and the device keeps serving.
func (d *Device) Stop() error {
	if d == nil {
		return ErrInvalidParameters
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopLocked()
}

func (d *Device) stopLocked() error {
	switch d.state {
	case DeviceStateRunning:
	case DeviceStateStopped:
		return nil
	case DeviceStateClosed:
		return fmt.Errorf("device is closed")
	default:
		return fmt.Errorf("device is not started")
	}
	controller, err := createController()
	if err != nil {
		return fmt.Errorf("failed to create controller for stop: %w", err)
	}
	defer controller.Close()

	// STOP_DEV before anything else: the kernel's stop path (del_gendisk)
	// waits for in-flight requests, which only the running queues can
	// complete (Critical Bug #8). If it fails, nothing has been torn down and
	// the device is still serving (Critical Bug #18).
	ctx, cancel := context.WithTimeout(context.Background(), d.stopTimeout())
	defer cancel()
	d.leaving.Store(true)
	if d.params.SafeStop {
		err = controller.TryStopDev(ctx, d.ID)
		if errors.Is(err, syscall.EBUSY) {
			d.leaving.Store(false)
			return fmt.Errorf("%w: %s is open", ErrDeviceBusy, d.Path)
		}
	} else {
		err = controller.StopDev(ctx, d.ID)
	}
	if err != nil {
		d.leaving.Store(false)
		return fmt.Errorf("failed to stop device: %w", err)
	}

	close(d.unwatch)
	if d.metrics != nil {
		d.metrics.Stop()
	}
	// STOP_DEV aborted every outstanding fetch, so the engines exit on their
	// own once they have committed what their handlers hold.
	relErr := d.releaseQueues(30 * time.Second)
	d.state = DeviceStateStopped
	d.finish(nil)
	if relErr != nil {
		return fmt.Errorf("device stopped, but %w", relErr)
	}
	if d.options != nil && d.options.Logger != nil {
		d.options.Logger.Printf("Device %s stopped", d.Path)
	}
	return nil
}

// Close performs full cleanup: stops the device if it is running, then deletes
// it. After Close the device cannot be reused. Close on a detached device only
// releases this process's handle; the kernel device stays for Recover.
func (d *Device) Close() error {
	if d == nil {
		return ErrInvalidParameters
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch d.state {
	case DeviceStateClosed:
		return nil
	case DeviceStateDetached:
		d.state = DeviceStateClosed
		return nil
	case DeviceStateRunning:
		if err := d.stopLocked(); err != nil {
			return err
		}
	}
	if d.runners != nil {
		// A Start that failed half way, or queues that did not exit.
		if err := d.abandonQueues(); err != nil {
			return fmt.Errorf("failed to delete device: %w; /dev/ublkc%d is still held", err, d.ID)
		}
	}

	controller, err := createController()
	if err != nil {
		return fmt.Errorf("failed to create controller for close: %w", err)
	}
	defer controller.Close()
	// DEL_DEV waits until the last /dev/ublkcN reference is gone, which the
	// queue teardown above guarantees.
	ctx, cancel := context.WithTimeout(context.Background(), d.stopTimeout())
	defer cancel()
	if err := controller.DelDev(ctx, d.ID); err != nil {
		return fmt.Errorf("failed to delete device: %w", err)
	}
	d.state = DeviceStateClosed
	d.finish(nil)
	if d.options != nil && d.options.Logger != nil {
		d.options.Logger.Printf("Device %s closed", d.Path)
	}
	return nil
}

// Done is closed when the device stops serving: after Stop, Close or Detach,
// when the context given to Start is cancelled, or when a queue fails.
func (d *Device) Done() <-chan struct{} { return d.done }

// Err reports why the device stopped serving: nil after an orderly Stop,
// Close or Detach, or the queue failure otherwise. It is nil while the device
// is running.
func (d *Device) Err() error {
	select {
	case <-d.done:
		return d.err
	default:
		return nil
	}
}

// Wait blocks until the device stops serving or ctx ends, and returns Err (or
// ctx's error).
func (d *Device) Wait(ctx context.Context) error {
	select {
	case <-d.done:
		return d.err
	case <-ctx.Done():
		return ctx.Err()
	}
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
	// DeviceStateDetached indicates this process let go of the device with
	// Detach; the kernel keeps it for Recover
	DeviceStateDetached DeviceState = "detached"
	// DeviceStateFailed indicates a queue failed while the device was running
	// (see Err); Close it
	DeviceStateFailed DeviceState = "failed"
)

// State returns the current state of the device
func (d *Device) State() DeviceState {
	if d == nil {
		return DeviceStateClosed
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state == DeviceStateRunning && d.Err() != nil {
		return DeviceStateFailed
	}
	return d.state
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
	return d.params.size()
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

// createController opens the control plane. Tests replace it to inject failures
// without opening a kernel device; production callers never change it.
var createController = ctrl.NewController

// convertToCtrlParams converts public DeviceParams to internal ctrl.DeviceParams
func convertToCtrlParams(params DeviceParams) ctrl.DeviceParams {
	ctrlParams := ctrl.DefaultDeviceParams(params.Backend)

	// Copy all fields
	ctrlParams.DeviceID = params.DeviceID
	ctrlParams.QueueDepth = params.QueueDepth
	ctrlParams.NumQueues = params.NumQueues
	if ctrlParams.NumQueues == 0 {
		ctrlParams.NumQueues = runtime.NumCPU()
	}
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
	ctrlParams.EnableFUA = params.EnableFUA && fuaHonored(params)

	ctrlParams.DiscardAlignment = params.DiscardAlignment
	ctrlParams.DiscardGranularity = params.DiscardGranularity
	ctrlParams.MaxDiscardSectors = params.MaxDiscardSectors
	ctrlParams.MaxDiscardSegments = params.MaxDiscardSegments

	ctrlParams.DeviceName = params.DeviceName
	ctrlParams.CPUAffinity = params.CPUAffinity

	ctrlParams.Size = params.Size
	ctrlParams.CanDiscard = params.Handler != nil && params.HandlerDiscard
	ctrlParams.CanWriteZeroes = params.Handler != nil && params.HandlerWriteZeroes
	if _, ok := params.Backend.(ZeroCopyBackend); ok && params.EnableZeroCopy {
		// Served in the kernel with fallocate, whatever the backend implements.
		ctrlParams.CanDiscard, ctrlParams.CanWriteZeroes = true, true
	}
	ctrlParams.UblksrvFlags = params.Tag
	ctrlParams.PhysicalBlockSize = params.PhysicalBlockSize
	ctrlParams.IOMinSize = params.IOMinSize
	ctrlParams.IOOptSize = params.IOOptSize
	ctrlParams.DMAAlignment = params.DMAAlignment

	if params.EnableZoned {
		ctrlParams.EnableUserCopy = true // the kernel requires it for zoned devices
		ctrlParams.ZoneSectors = uint32(params.Zoned.ZoneSize / uapi.SectorSize)
		ctrlParams.MaxOpenZones = params.Zoned.MaxOpenZones
		ctrlParams.MaxActiveZones = params.Zoned.MaxActiveZones
		appendMax := params.Zoned.MaxZoneAppendSize
		if appendMax <= 0 {
			appendMax = params.MaxIOSize
		}
		ctrlParams.MaxZoneAppendSectors = uint32(appendMax / uapi.SectorSize)
	}

	flags := params.Recovery.flags()
	if params.NeedGetData {
		flags |= uapi.UBLK_F_NEED_GET_DATA
	}
	if params.NoPartitionScan {
		flags |= uapi.UBLK_F_NO_AUTO_PART_SCAN
	}
	if params.ThreadsPerQueue > 1 {
		flags |= uapi.UBLK_F_PER_IO_DAEMON
	}
	ctrlParams.Flags = flags
	return ctrlParams
}

// fuaHonored reports whether the server acts on per-I/O FUA: a Handler is
// trusted to (it sees FlagFUA), a Backend only if it implements FUABackend.
func fuaHonored(params DeviceParams) bool {
	if params.Handler != nil {
		return true
	}
	if _, ok := params.Backend.(ZeroCopyBackend); ok && params.EnableZeroCopy {
		return true // zero-copy writes with FUA use RWF_DSYNC
	}
	_, ok := params.Backend.(FUABackend)
	return ok
}
