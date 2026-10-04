package queue

import (
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// ErrStillInUse is returned by Queue.Close when an engine has not exited or a
// handler still holds a request. The queue's memory is then deliberately
// leaked: unmapping a buffer the kernel or a handler may still touch would be
// a use-after-free (TODO Critical Bug #19).
var ErrStillInUse = errors.New("queue still in use; its memory was leaked rather than freed")

// QueueConfig configures one ublk hardware queue.
type QueueConfig struct {
	QueueID   uint16
	Depth     int
	MaxIOSize int
	CharFd    int    // /dev/ublkcN; shared by every queue, not owned
	Flags     uint64 // negotiated UBLK_F_* features
	DescSize  int    // bytes per I/O descriptor (dev_info.io_desc_size); 0 means 24
	Handler   Handler
	Inline    bool
	Threads   int // engines (OS threads) per queue; >1 needs UBLK_F_PER_IO_DAEMON
	CPU       int // CPU to pin the queue's threads to, or -1
	// ZeroCopyFile, if >= 0, serves every request zero-copy against this
	// file descriptor (device offset 0 = file offset ZeroCopyBase); Handler
	// is then unused. Needs UBLK_F_SUPPORT_ZERO_COPY in Flags, and uses
	// automatic buffer registration when Flags has UBLK_F_AUTO_BUF_REG.
	ZeroCopyFile int
	ZeroCopyBase int64
	Logger       interfaces.Logger

	newRing func(entries uint32) (ring, error) // tests substitute a fake kernel
}

// Queue serves one ublk hardware queue: it maps the queue's descriptor array
// and data buffers and runs one or more engines over its tags.
type Queue struct {
	cfg     QueueConfig
	desc    []byte
	bufs    []byte
	engines []*engine
	done    chan struct{}
	closed  bool
}

func defaultRing(entries uint32) (ring, error) {
	return uring.NewIoUring(uring.SetupOptions{
		Entries:   entries,
		CQEntries: 2 * entries,
		// What the kernel's own ublk selftest server uses; each is dropped if
		// the running kernel predates it.
		OptionalFlags: uring.IORING_SETUP_COOP_TASKRUN | uring.IORING_SETUP_SINGLE_ISSUER |
			uring.IORING_SETUP_DEFER_TASKRUN,
	})
}

func pageRound(n int) int {
	p := os.Getpagesize()
	return (n + p - 1) / p * p
}

// NewQueue maps the queue's memory and prepares its engines; Start runs them.
func NewQueue(cfg QueueConfig) (*Queue, error) {
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
	if cfg.DescSize == 0 {
		cfg.DescSize = int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
	}
	if cfg.Threads < 1 {
		cfg.Threads = 1
	}
	if cfg.Threads > cfg.Depth {
		cfg.Threads = cfg.Depth
	}
	if cfg.newRing == nil {
		cfg.newRing = defaultRing
	}

	// The kernel maps queue q's descriptors at q * round_up(4096 * desc size,
	// PAGE): a stride fixed by UBLK_MAX_QUEUE_DEPTH, not by this queue's
	// depth (getting this wrong aliased every queue onto queue 0, Critical
	// Bug #1). The length must be exactly round_up(depth * desc size, PAGE).
	stride := pageRound(uapi.UBLK_MAX_QUEUE_DEPTH * cfg.DescSize)
	desc, err := unix.Mmap(cfg.CharFd, int64(cfg.QueueID)*int64(stride), pageRound(cfg.Depth*cfg.DescSize),
		unix.PROT_READ, unix.MAP_SHARED|unix.MAP_POPULATE)
	if err != nil {
		return nil, fmt.Errorf("queue %d: mmap descriptors: %w", cfg.QueueID, err)
	}
	maxInt := int(^uint(0) >> 1)
	if cfg.MaxIOSize > maxInt/cfg.Depth {
		_ = unix.Munmap(desc)
		return nil, fmt.Errorf("queue buffer allocation overflows: depth %d, max I/O %d", cfg.Depth, cfg.MaxIOSize)
	}
	// Zero copy needs no data buffers: the bytes never reach the server.
	var bufs []byte
	var bufPtr unsafe.Pointer
	var zc *zeroCopyConfig
	if zeroCopy {
		zc = &zeroCopyConfig{fd: cfg.ZeroCopyFile, base: cfg.ZeroCopyBase,
			auto: cfg.Flags&uapi.UBLK_F_AUTO_BUF_REG != 0}
	} else {
		bufs, err = unix.Mmap(-1, 0, cfg.Depth*cfg.MaxIOSize, unix.PROT_READ|unix.PROT_WRITE,
			unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
		if err != nil {
			_ = unix.Munmap(desc)
			return nil, fmt.Errorf("queue %d: allocate %d-byte data buffers: %w", cfg.QueueID, cfg.Depth*cfg.MaxIOSize, err)
		}
		bufPtr = unsafe.Pointer(&bufs[0])
	}

	q := &Queue{cfg: cfg, desc: desc, bufs: bufs, done: make(chan struct{})}
	per := (cfg.Depth + cfg.Threads - 1) / cfg.Threads
	for lo := 0; lo < cfg.Depth; lo += per {
		q.engines = append(q.engines, newEngine(engineConfig{
			queueID:    cfg.QueueID,
			tagLo:      lo,
			tagHi:      min(lo+per, cfg.Depth),
			charFd:     cfg.CharFd,
			desc:       unsafe.Pointer(&desc[0]),
			descStride: uintptr(cfg.DescSize),
			bufs:       bufPtr,
			bufSize:    cfg.MaxIOSize,
			userCopy:   cfg.Flags&uapi.UBLK_F_USER_COPY != 0,
			zeroCopy:   zc,
			handler:    cfg.Handler,
			inline:     cfg.Inline,
			cpu:        cfg.CPU,
			logger:     cfg.Logger,
			newRing:    cfg.newRing,
		}))
	}
	return q, nil
}

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
	go func() {
		for _, e := range q.engines[:started] {
			<-e.done
		}
		close(q.done)
	}()
	if err != nil {
		for _, e := range q.engines[:started] {
			e.abandon()
		}
		q.engines = q.engines[:started]
		return err
	}
	return nil
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
	var bufErr error
	if q.bufs != nil {
		bufErr = unix.Munmap(q.bufs)
	}
	return errors.Join(unix.Munmap(q.desc), bufErr)
}
