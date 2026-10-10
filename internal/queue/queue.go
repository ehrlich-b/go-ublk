package queue

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
	"github.com/ehrlich-b/go-ublk/internal/validation"
)

// ErrStillInUse is returned by Queue.Close when an engine has not exited or a
// handler still holds a request. The queue's memory is then deliberately
// leaked: unmapping a buffer the kernel or a handler may still touch would be
// a use-after-free (TODO Critical Bug #19).
var ErrStillInUse = errors.New("queue still in use; its memory was leaked rather than freed")

// QueueConfig configures one ublk hardware queue.
type QueueConfig struct {
	NumQueues        int   // accepted device queue count
	Capacity         int64 // accepted device capacity in bytes
	LogicalBlockSize int
	QueueID          uint16
	Depth            int
	MaxIOSize        int
	CharFd           int    // /dev/ublkcN; shared by every queue, not owned
	Flags            uint64 // negotiated UBLK_F_* features
	DescSize         int    // bytes per I/O descriptor (dev_info.io_desc_size); 0 means 24
	Handler          Handler
	Inline           bool
	Dispatch         DispatchMode
	Threads          int   // engines (OS threads) per queue; >1 needs UBLK_F_PER_IO_DAEMON
	CPUs             []int // CPUs to pin the queue's threads to; empty: no affinity
	// ZeroCopyFile, if >= 0, serves every request zero-copy against this
	// file descriptor (device offset 0 = file offset ZeroCopyBase); Handler
	// is then unused. Needs UBLK_F_SUPPORT_ZERO_COPY in Flags, and uses
	// automatic buffer registration when Flags has UBLK_F_AUTO_BUF_REG.
	ZeroCopyFile int
	ZeroCopyBase int64
	// IntegrityInterval and IntegrityMetadata describe UBLK_F_INTEGRITY
	// metadata: bytes of metadata per interval of data. Zero: none.
	IntegrityInterval int
	IntegrityMetadata int
	// SharedMemory holds the regions registered for UBLK_F_SHMEM_ZC.
	SharedMemory *SharedMemory
	Logger       interfaces.Logger

	newRing func(entries uint32) (ring, error) // tests substitute a fake kernel
}

// Queue serves one ublk hardware queue: it maps the queue's descriptor array
// and data buffers and runs one or more engines over its tags.
type Queue struct {
	capacity atomic.Int64
	cfg      QueueConfig
	desc     []byte
	bufs     []byte
	integ    []byte
	engines  []*engine
	done     chan struct{}
	closed   bool
}

// ioRing is the production ring: uring.IoUring plus the provided-buffer ring
// registration batch I/O needs.
type ioRing struct{ *uring.IoUring }

func (r ioRing) NewTagRing(bgid uint16, entries uint32) (tagRing, error) {
	br, err := r.RegisterBufRing(bgid, entries)
	if err != nil {
		return nil, err
	}
	return br, nil
}

func defaultRing(entries uint32) (ring, error) {
	u, err := newIoUring(entries)
	if err != nil {
		return nil, err
	}
	return ioRing{u}, nil
}

func newIoUring(entries uint32) (*uring.IoUring, error) {
	return uring.NewIoUring(uring.SetupOptions{
		Entries:   entries,
		CQEntries: 2 * entries,
		// What the kernel's own ublk selftest server uses; each is dropped if
		// the running kernel predates it.
		OptionalFlags: uring.IORING_SETUP_COOP_TASKRUN | uring.IORING_SETUP_SINGLE_ISSUER |
			uring.IORING_SETUP_DEFER_TASKRUN,
	})
}

// NewQueue maps the queue's memory and prepares its engines; Start runs them.
func NewQueue(cfg QueueConfig) (*Queue, error) {
	if cfg.Dispatch > DispatchAdaptive {
		return nil, fmt.Errorf("invalid dispatch mode %d", cfg.Dispatch)
	}
	if cfg.Depth < 1 || cfg.Depth > uapi.UBLK_MAX_QUEUE_DEPTH {
		return nil, fmt.Errorf("queue depth %d outside 1..%d", cfg.Depth, uapi.UBLK_MAX_QUEUE_DEPTH)
	}
	if cfg.MaxIOSize < 1 {
		return nil, fmt.Errorf("max I/O size must be positive, got %d", cfg.MaxIOSize)
	}
	zeroCopy := cfg.ZeroCopyFile >= 0 && cfg.Flags&uapi.UBLK_F_SUPPORT_ZERO_COPY != 0
	if cfg.Handler == nil && !zeroCopy {
		return nil, errors.New("nil handler")
	}
	if cfg.DescSize == 0 && cfg.Flags&uapi.UBLK_F_IO_DESC_SIZE == 0 {
		cfg.DescSize = 24
	}
	if cfg.Threads < 1 {
		cfg.Threads = 1
	}
	if cfg.Threads > cfg.Depth {
		cfg.Threads = cfg.Depth
	}
	if cfg.Flags&uapi.UBLK_F_BATCH_IO != 0 {
		cfg.Threads = 1 // one PREP and one multishot fetch per queue
	}
	if cfg.newRing == nil {
		cfg.newRing = defaultRing
	}

	base := int64(0)
	if zeroCopy {
		base = cfg.ZeroCopyBase
	}
	limits := validation.RequestLimits{Capacity: cfg.Capacity, FileBase: base,
		LogicalBlockSize: uint64(cfg.LogicalBlockSize), MaxPayload: uint64(cfg.MaxIOSize)}
	if err := limits.Validate(); err != nil {
		return nil, fmt.Errorf("queue request limits: %w", err)
	}
	// This gate checks descriptor stride, queue ownership, page rounding and
	// the fixed maximum-depth mmap offset before any mapping or pointer.
	dl, err := validation.ValidateDescriptorLayout(validation.DescriptorParams{
		QueueID: uint64(cfg.QueueID), Queues: uint64(cfg.NumQueues), Depth: uint64(cfg.Depth),
		DescSize: uint64(cfg.DescSize), PageSize: uint64(os.Getpagesize()),
	})
	if err != nil {
		return nil, fmt.Errorf("queue descriptor layout: %w", err)
	}
	bufBytes, err := validation.BufferSize(uint64(cfg.Depth), uint64(cfg.MaxIOSize), 1)
	if err != nil {
		return nil, fmt.Errorf("queue buffer layout: %w", err)
	}
	integSize, integBytes := 0, 0
	if cfg.IntegrityInterval < 0 || cfg.IntegrityMetadata < 0 || (cfg.IntegrityInterval == 0) != (cfg.IntegrityMetadata == 0) {
		return nil, errors.New("invalid integrity buffer geometry")
	}
	if cfg.IntegrityInterval > 0 {
		integSize, err = validation.BufferSize(uint64(cfg.MaxIOSize/cfg.IntegrityInterval)+1, uint64(cfg.IntegrityMetadata), 1)
		if err != nil {
			return nil, fmt.Errorf("integrity tag layout: %w", err)
		}
		integBytes, err = validation.BufferSize(uint64(cfg.Depth), uint64(integSize), 1)
		if err != nil {
			return nil, fmt.Errorf("integrity queue layout: %w", err)
		}
	}
	desc, err := unix.Mmap(cfg.CharFd, dl.Offset, dl.Size, unix.PROT_READ, unix.MAP_SHARED|unix.MAP_POPULATE)
	if err != nil {
		return nil, fmt.Errorf("queue %d: mmap descriptors: %w", cfg.QueueID, err)
	}
	var bufs, integ []byte
	cleanup := func() {
		_ = unix.Munmap(desc)
		if bufs != nil {
			_ = unix.Munmap(bufs)
		}
		if integ != nil {
			_ = unix.Munmap(integ)
		}
	}
	var zc *zeroCopyConfig
	if zeroCopy {
		zc = &zeroCopyConfig{fd: cfg.ZeroCopyFile, base: cfg.ZeroCopyBase,
			auto: cfg.Flags&uapi.UBLK_F_AUTO_BUF_REG != 0}
	} else {
		bufs, err = unix.Mmap(-1, 0, bufBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("queue %d: allocate data buffers: %w", cfg.QueueID, err)
		}
	}
	if integBytes > 0 {
		integ, err = unix.Mmap(-1, 0, integBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("queue %d: allocate integrity buffers: %w", cfg.QueueID, err)
		}
	}
	maps := []validation.Mapping{{Size: uint64(len(desc)), Regions: []validation.Region{
		{Name: "descriptors", Count: uint64(cfg.Depth), Stride: dl.Stride, Align: 8},
	}}}
	if bufs != nil {
		maps = append(maps, validation.Mapping{Size: uint64(len(bufs)), Regions: []validation.Region{
			{Name: "data buffers", Count: uint64(cfg.Depth), Stride: uint64(cfg.MaxIOSize), Align: 1},
		}})
	}
	if integ != nil {
		maps = append(maps, validation.Mapping{Size: uint64(len(integ)), Regions: []validation.Region{
			{Name: "integrity buffers", Count: uint64(cfg.Depth), Stride: uint64(integSize), Align: 1},
		}})
	}
	if _, err := validation.ValidateLayout(maps); err != nil {
		cleanup()
		return nil, fmt.Errorf("mapped queue layout: %w", err)
	}
	// Only validated spans become engine pointers.
	var bufPtr, integPtr unsafe.Pointer
	if bufs != nil {
		bufPtr = unsafe.Pointer(&bufs[0])
	}
	if integ != nil {
		integPtr = unsafe.Pointer(&integ[0])
	}

	q := &Queue{cfg: cfg, desc: desc, bufs: bufs, integ: integ, done: make(chan struct{})}
	q.capacity.Store(cfg.Capacity)
	per := (cfg.Depth + cfg.Threads - 1) / cfg.Threads
	for lo := 0; lo < cfg.Depth; lo += per {
		q.engines = append(q.engines, newEngine(engineConfig{
			queueID:          cfg.QueueID,
			capacity:         q.capacity.Load,
			logicalBlockSize: cfg.LogicalBlockSize,
			tagLo:            lo,
			tagHi:            min(lo+per, cfg.Depth),
			charFd:           cfg.CharFd,
			desc:             unsafe.Pointer(&desc[0]),
			descStride:       uintptr(cfg.DescSize),
			bufs:             bufPtr,
			bufSize:          cfg.MaxIOSize,
			userCopy:         cfg.Flags&uapi.UBLK_F_USER_COPY != 0,
			zeroCopy:         zc,
			batch:            cfg.Flags&uapi.UBLK_F_BATCH_IO != 0,
			zoned:            cfg.Flags&uapi.UBLK_F_ZONED != 0,
			shmem:            cfg.SharedMemory,
			integ:            integPtr,
			integSize:        integSize,
			integInterval:    cfg.IntegrityInterval,
			integMeta:        cfg.IntegrityMetadata,
			handler:          cfg.Handler,
			inline:           cfg.Inline,
			dispatch:         cfg.Dispatch,
			cpus:             cfg.CPUs,
			logger:           cfg.Logger,
			newRing:          cfg.newRing,
		}))
	}
	return q, nil
}

// SetCapacity updates request validation after a successful UPDATE_SIZE.
func (q *Queue) SetCapacity(size int64) { q.capacity.Store(size) }

// Start runs the queue's engines and returns once each has submitted a FETCH
// for every tag it serves. On error the engines that did start are abandoned;
// the caller must still Wait and Close.
func (q *Queue) Start() error {
	var err error
	started := 0
	for _, e := range q.engines {
		if err = e.start(); err != nil {
			break
		}
		started++
	}
	if err != nil {
		for _, e := range q.engines[:started] {
			e.abandon()
		}
		// The failed engine already tore down before reporting its error.
		// Later engines never ran, but NewQueue created their pool workers.
		for _, e := range q.engines[started+1:] {
			e.stopDispatch()
		}
		q.engines = q.engines[:started]
	}
	go func() {
		for _, e := range q.engines {
			<-e.done
		}
		close(q.done)
	}()
	return err
}

// Done is closed once every engine has exited: normally after STOP_DEV aborts
// every tag, or after Abandon, or on a fatal error.
func (q *Queue) Done() <-chan struct{} { return q.done }

// Err returns the first fatal engine error, or nil.
func (q *Queue) Err() error {
	for _, e := range q.engines {
		select {
		case <-e.done:
			if e.err != nil {
				return e.err
			}
		default:
		}
	}
	return nil
}

// Abandon makes the engines exit without the kernel stopping the device: they
// stop dispatching, commit what handlers still hold, and close their rings.
func (q *Queue) Abandon() {
	for _, e := range q.engines {
		e.abandon()
	}
}

// Wait blocks until the queue is done or the timeout passes.
func (q *Queue) Wait(timeout time.Duration) bool {
	select {
	case <-q.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Close frees the queue's memory. It refuses — leaking the memory and
// returning ErrStillInUse — unless every engine has exited and no handler
// still holds a request.
func (q *Queue) Close() error {
	if q.closed {
		return nil
	}
	select {
	case <-q.done:
	default:
		return ErrStillInUse
	}
	for _, e := range q.engines {
		if e.handlers.Load() != 0 {
			return ErrStillInUse
		}
	}
	q.closed = true
	var bufErr, integErr error
	if q.bufs != nil {
		bufErr = unix.Munmap(q.bufs)
	}
	if q.integ != nil {
		integErr = unix.Munmap(q.integ)
	}
	return errors.Join(unix.Munmap(q.desc), bufErr, integErr)
}
