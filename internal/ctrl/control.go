// Package ctrl issues ublk control commands on /dev/ublk-control.
//
// Every command in the v7.3-rc5 UAPI has one typed method (commands.go).
// Commands go through io_uring URING_CMD with the header's exact ioctl
// encodings. A Controller is safe for concurrent use: each in-flight command
// owns a private io_uring and an off-heap scratch page for its buffer.
package ctrl

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

const (
	UblkControlPath = "/dev/ublk-control"

	// AnyDevID asks ADD_DEV to pick the device number.
	AnyDevID = ^uint32(0)

	// defaultMaxInFlight bounds commands running at once on one Controller.
	// Commands abandoned by their context do not count against it.
	defaultMaxInFlight = 8

	// scratchSize is each slot's off-heap command buffer: the char-device
	// path prefix (at most maxDevPathArea bytes) followed by the payload.
	scratchSize    = 2 * 4096
	maxDevPathArea = 256
	maxPayload     = scratchSize - maxDevPathArea
)

// ENOTSUPP is the kernel-internal "operation not supported" (524) that
// ublk_drv before v7.x returns for an unknown control command. It is not a
// userspace errno, so syscall has no name for it.
const ENOTSUPP = syscall.Errno(524)

// ErrClosed is returned by commands issued after Close.
var ErrClosed = errors.New("ublk controller closed")

// Error is a failed control command. Err is the kernel's negative CQE result
// as a syscall.Errno, or the transport error from io_uring. Use errors.Is on
// it, e.g. errors.Is(err, syscall.EBUSY).
type Error struct {
	Op    string // command name, e.g. "STOP_DEV"
	DevID uint32
	Err   error
}

func (e *Error) Error() string {
	if e.DevID == AnyDevID {
		return fmt.Sprintf("ublk %s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("ublk %s dev %d: %v", e.Op, e.DevID, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// IsUnsupported reports whether err means the running kernel does not know
// the command: EOPNOTSUPP (v7.x) or ENOTSUPP (v6.x drivers).
func IsUnsupported(err error) bool {
	return errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, ENOTSUPP)
}

// InFlightError is returned when a command's context ends before the kernel
// completes it. The command is NOT cancelled: the kernel keeps executing it,
// and the Controller keeps its ring and buffer alive until the completion
// arrives. errors.Is(err, context.DeadlineExceeded) (or Canceled) holds.
type InFlightError struct {
	Op    string
	DevID uint32
	Err   error // the context's error

	done   chan struct{}
	result error
}

func (e *InFlightError) Error() string {
	return fmt.Sprintf("ublk %s dev %d: %v (command still running in the kernel)", e.Op, e.DevID, e.Err)
}

func (e *InFlightError) Unwrap() error { return e.Err }

// Done is closed when the kernel completes the abandoned command.
func (e *InFlightError) Done() <-chan struct{} { return e.done }

// Result is the abandoned command's outcome once Done is closed (nil on
// success); before that it returns nil.
func (e *InFlightError) Result() error {
	select {
	case <-e.done:
		return e.result
	default:
		return nil
	}
}

// slot is one command's private transport: an io_uring on its own
// /dev/ublk-control fd and an mmap'd scratch buffer. The kernel reads and
// writes ctrl_cmd.addr after io_uring_enter returns (every sleeping command
// runs on io-wq), so the buffer must never move or be freed while a command
// may be in flight: Go heap memory can move with a goroutine stack (it did,
// Critical Bug #21) and is freed by the GC, mmap'd memory is neither.
type slot struct {
	ring  uring.Ring
	fd    int
	mem   []byte
	unmap func([]byte) error
}

func (s *slot) close() error {
	err := s.ring.Close()
	if s.fd >= 0 {
		err = errors.Join(err, syscall.Close(s.fd))
	}
	return errors.Join(err, s.unmap(s.mem))
}

// retire releases a slot whose last command failed in transport. The kernel
// may still own that command (a timed-out wait), so the ring and fd are
// closed but the scratch page is leaked: a late kernel write must land in
// memory nobody else will ever use.
func (s *slot) retire() {
	_ = s.ring.Close()
	if s.fd >= 0 {
		_ = syscall.Close(s.fd)
	}
}

func newSlot() (*slot, error) {
	fd, err := syscall.Open(UblkControlPath, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", UblkControlPath, err)
	}
	// Once internal/uring has Config.CtrlTimeout and SubmitCtrlCmdContext
	// (ctxRing), set CtrlTimeout: -1 here so the caller's context, not a fixed
	// 10s cap, bounds STOP_DEV and DEL_DEV; the context then cancels.
	ring, err := uring.NewRing(uring.Config{Entries: 4, FD: int32(fd)})
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to create io_uring: %w", err)
	}
	mem, err := unix.Mmap(-1, 0, scratchSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		ring.Close()
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to map control buffer: %w", err)
	}
	return &slot{ring: ring, fd: fd, mem: mem, unmap: unix.Munmap}, nil
}

// Controller issues ublk control commands. It is safe for concurrent use;
// up to defaultMaxInFlight commands run at once, each on its own ring.
type Controller struct {
	logger  *logging.Logger
	newSlot func() (*slot, error)
	sem     chan struct{}

	mu     sync.Mutex
	idle   []*slot
	closed bool

	featMu   sync.Mutex
	features *FeatureSet
}

// NewController opens /dev/ublk-control. The first ring is created eagerly
// so open and permission errors surface here.
func NewController() (*Controller, error) {
	return newController(newSlot)
}

func newController(factory func() (*slot, error)) (*Controller, error) {
	s, err := factory()
	if err != nil {
		return nil, err
	}
	c := &Controller{
		logger:  logging.Default(),
		newSlot: factory,
		sem:     make(chan struct{}, defaultMaxInFlight),
		idle:    []*slot{s},
	}
	return c, nil
}

// SetLogger sets the logger for this controller
func (c *Controller) SetLogger(logger *logging.Logger) {
	if logger != nil {
		c.logger = logger
	}
}

// Close releases idle rings and makes further commands fail with ErrClosed.
// It does not wait for commands still running: a command in flight (or
// abandoned by its context) releases its ring when the kernel completes it.
func (c *Controller) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	idle := c.idle
	c.idle = nil
	c.mu.Unlock()
	var err error
	for _, s := range idle {
		err = errors.Join(err, s.close())
	}
	return err
}

func (c *Controller) acquire(ctx context.Context) (*slot, error) {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		<-c.sem
		return nil, ErrClosed
	}
	if n := len(c.idle); n > 0 {
		s := c.idle[n-1]
		c.idle = c.idle[:n-1]
		c.mu.Unlock()
		return s, nil
	}
	c.mu.Unlock()
	s, err := c.newSlot()
	if err != nil {
		<-c.sem
		return nil, err
	}
	return s, nil
}

// release returns a slot after its command completed (in transport terms).
func (c *Controller) release(s *slot, transportErr error) {
	if transportErr != nil {
		s.retire()
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = s.close()
		return
	}
	c.idle = append(c.idle, s)
	c.mu.Unlock()
}

// request describes one control command.
type request struct {
	name    string
	op      uint32 // UBLK_U_CMD_*
	devID   uint32
	data    uint64
	devPath string // prefixed to the buffer when non-empty (dev_path_len)
	payload int    // payload bytes after the path; 0 means no buffer
	fill    func(p []byte)
	// read decodes the payload after a successful (res >= 0) completion,
	// while the slot is still owned.
	read func(p []byte, res int32) error
	// late runs on the reaper goroutine if the caller abandoned the command
	// and it later succeeded (ADD_DEV uses it to delete the orphan).
	late func(p []byte, res int32)
}

type outcome struct {
	res int32
	err error
}

// exec runs req on a private slot and returns the kernel's CQE result.
func (c *Controller) exec(ctx context.Context, req request) (int32, error) {
	if req.payload > maxPayload || len(req.devPath)+1 > maxDevPathArea {
		return 0, &Error{Op: req.name, DevID: req.devID, Err: syscall.E2BIG}
	}
	if err := ctx.Err(); err != nil {
		return 0, &Error{Op: req.name, DevID: req.devID, Err: err}
	}
	s, err := c.acquire(ctx)
	if err != nil {
		return 0, &Error{Op: req.name, DevID: req.devID, Err: err}
	}
	defer func() { <-c.sem }()

	cmd, payload := layout(s.mem, req)
	if ctx.Done() == nil {
		res, terr := s.ring.SubmitCtrlCmd(req.op, &cmd, 0)
		o := c.finish(s, req, res, terr, payload, false)
		return o.res, o.err
	}

	// The submit runs on its own goroutine, which owns the slot until the
	// kernel completes the command, whether or not anyone is still waiting.
	var (
		mu        sync.Mutex
		finished  bool
		result    outcome
		abandoned *InFlightError
		done      = make(chan struct{})
	)
	go func() {
		var res uring.Result
		var terr error
		if cr, ok := s.ring.(ctxRing); ok {
			res, terr = cr.SubmitCtrlCmdContext(ctx, req.op, &cmd, 0)
		} else {
			res, terr = s.ring.SubmitCtrlCmd(req.op, &cmd, 0)
		}
		mu.Lock()
		result = c.finish(s, req, res, terr, payload, abandoned != nil)
		finished = true
		if abandoned != nil {
			abandoned.result = result.err
			close(abandoned.done)
		}
		mu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		return result.res, result.err
	case <-ctx.Done():
	}
	mu.Lock()
	defer mu.Unlock()
	if finished {
		return result.res, result.err
	}
	abandoned = &InFlightError{Op: req.name, DevID: req.devID, Err: ctx.Err(), done: make(chan struct{})}
	c.logger.Warn("control command abandoned by context, still running in the kernel",
		"op", req.name, "dev_id", req.devID, "err", ctx.Err())
	return 0, abandoned
}

// ctxRing is a ring that can cancel an in-flight control command when its
// context ends: it submits IORING_OP_ASYNC_CANCEL, which signals the io-wq
// worker so interruptible kernel waits (START_DEV and END_USER_RECOVERY
// waiting for FETCHes, DEL_DEV waiting for the last reference, QUIESCE_DEV)
// return EINTR, and it reaps the command's own completion before returning.
// It then returns a non-nil Result together with an error wrapping the
// context's; a nil Result with an error means the completion was not reaped
// and the command may still be running. Rings without it are only waited on.
type ctxRing interface {
	SubmitCtrlCmdContext(ctx context.Context, cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (uring.Result, error)
}

// finish turns a completion into an outcome, decodes the payload (read for a
// waiting caller, late for an abandoned one) and only then releases the slot.
// A non-nil Result is the kernel's verdict even when it comes with an error
// (a reaped cancellation); only a nil Result with an error leaves the
// command's fate unknown and retires the slot.
func (c *Controller) finish(s *slot, req request, res uring.Result, terr error, payload []byte, late bool) outcome {
	var cancelled error
	if res != nil && terr != nil {
		c.logger.Debug("control command cancelled and reaped", "op", req.name, "dev_id", req.devID, "res", res.Value(), "err", terr)
		cancelled, terr = terr, nil
	}
	o := outcome{err: terr}
	if terr == nil {
		o.res = res.Value()
		switch {
		case o.res < 0:
			o.err = syscall.Errno(-o.res)
			if cancelled != nil {
				o.err = errors.Join(o.err, cancelled) // keeps errors.Is(err, ctx.Err())
			}
		case late && req.late != nil:
			req.late(payload, o.res)
		case !late && req.read != nil:
			o.err = req.read(payload, o.res)
		}
	}
	c.release(s, terr)
	if o.err != nil {
		o.err = &Error{Op: req.name, DevID: req.devID, Err: o.err}
	}
	if late {
		c.logger.Info("abandoned control command completed", "op", req.name, "dev_id", req.devID, "res", o.res, "err", o.err)
	}
	return o
}

// layout clears the scratch buffer and builds the command header. With a
// device path the buffer is [path NUL pad-to-8][payload] and dev_path_len
// covers the padded path; the driver strips it before reading the payload
// (ublk_ctrl_uring_cmd_permission).
func layout(mem []byte, req request) (uapi.UblksrvCtrlCmd, []byte) {
	pathArea := 0
	if req.devPath != "" {
		pathArea = (len(req.devPath) + 1 + 7) &^ 7
	}
	total := pathArea + req.payload
	clear(mem[:total])
	copy(mem, req.devPath)
	payload := mem[pathArea:total]
	if req.fill != nil {
		req.fill(payload)
	}
	cmd := uapi.UblksrvCtrlCmd{
		DevID:      req.devID,
		QueueID:    0xFFFF,
		Len:        uint16(total),
		Data:       req.data,
		DevPathLen: uint16(pathArea),
	}
	if total > 0 {
		cmd.Addr = uint64(uintptr(unsafe.Pointer(&mem[0])))
	}
	return cmd, payload
}
