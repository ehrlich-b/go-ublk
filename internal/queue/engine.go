package queue

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// ring is the part of uring.IoUring the engine uses, so tests can substitute
// a scripted fake kernel.
type ring interface {
	GetSQE() *uring.SQE
	Submit() (int, error)
	SubmitAndWait(minComplete uint32, timeout time.Duration) (int, error)
	PeekCQE() *uring.CQE
	CQAdvance(n uint32)
	Close() error
}

// user_data layout: kind in the top byte, tag in the low 16 bits.
const (
	kindIO   uint64 = 1 << 56 // FETCH, COMMIT_AND_FETCH or NEED_GET_DATA for a tag
	kindWake uint64 = 2 << 56 // the eventfd read that wakes the engine
	kindMask uint64 = 0xff << 56
)

// Per-tag states, owned by the engine thread.
const (
	tagFetching uint8 = iota // FETCH or COMMIT_AND_FETCH in flight: the kernel owns the tag
	tagGetData               // NEED_GET_DATA in flight
	tagHandling              // the handler owns the request
	tagAborted               // the kernel aborted the tag: never fetch it again
	tagOrphaned              // a request arrived while abandoning: left for the kernel to requeue or fail
)

// engineConfig describes the tags one engine serves: [tagLo, tagHi) of one
// queue. Without UBLK_F_PER_IO_DAEMON an engine serves its whole queue.
type engineConfig struct {
	queueID      uint16
	tagLo, tagHi int
	charFd       int
	desc         unsafe.Pointer // the queue's descriptor array
	descStride   uintptr
	bufs         unsafe.Pointer // the queue's data buffers, bufSize bytes per tag
	bufSize      int
	userCopy     bool // UBLK_F_USER_COPY: data moves by pread/pwrite on charFd
	handler      Handler
	inline       bool
	cpu          int // -1: no affinity
	logger       interfaces.Logger
	newRing      func(entries uint32) (ring, error)
	waitInterval time.Duration
}

// engine serves a range of one queue's tags on one OS thread with one
// io_uring. The ublk driver requires every command for a tag after its FETCH
// to come from the task that fetched it, so the engine goroutine locks its OS
// thread for its whole life and never unlocks it: when the goroutine returns
// the thread exits too, which is how kernels without uring_cmd cancellation
// notice a dead server.
type engine struct {
	cfg  engineConfig
	ring ring
	reqs []Request
	tags []uint8
	live int // tags not yet aborted or orphaned

	handlers atomic.Int32 // requests handed to the handler and not yet committed

	// Completions from other goroutines: a lock-free stack, drained by the
	// engine. sleeping is set while the engine may block in the kernel; a
	// completer that pushes and finds it set writes the eventfd.
	head      atomic.Pointer[Request]
	sleeping  atomic.Bool
	wakeMu    sync.RWMutex
	wakeFd    int // -1 once closed
	wakeBuf   []byte
	wakeArmed bool // an eventfd read is in the kernel, targeting wakeBuf

	stopping atomic.Bool // abandon: stop dispatching, drain handlers, exit

	// testBeforeSleep, if set, runs between draining completions and publishing
	// sleeping: the window a lost-wakeup bug lives in. Tests only.
	testBeforeSleep func()
	err             error
	done            chan struct{}
}

func newEngine(cfg engineConfig) *engine {
	n := cfg.tagHi - cfg.tagLo
	e := &engine{
		cfg:    cfg,
		reqs:   make([]Request, n),
		tags:   make([]uint8, n),
		live:   n,
		wakeFd: -1,
		done:   make(chan struct{}),
	}
	for i := range e.reqs {
		e.reqs[i].e = e
		e.reqs[i].Queue = cfg.queueID
		e.reqs[i].Tag = uint16(cfg.tagLo + i)
	}
	if e.cfg.waitInterval == 0 {
		e.cfg.waitInterval = time.Second
	}
	return e
}

// start launches the engine thread and returns once every tag's FETCH has
// been submitted, or with the error that prevented it.
func (e *engine) start() error {
	started := make(chan error, 1)
	go e.run(started)
	return <-started
}

func (e *engine) run(started chan<- error) {
	runtime.LockOSThread() // never unlocked: see the type comment
	defer close(e.done)

	if err := e.setup(); err != nil {
		e.teardown()
		started <- err
		return
	}
	started <- nil
	e.loop()
	e.teardown()
}

func (e *engine) setup() error {
	if e.cfg.cpu >= 0 {
		var set unix.CPUSet
		set.Set(e.cfg.cpu)
		_ = unix.SchedSetaffinity(0, &set) // best effort
	}
	n := e.cfg.tagHi - e.cfg.tagLo
	// Room for one command per tag plus the eventfd read, twice over so a
	// burst of commits never finds the SQ full.
	r, err := e.cfg.newRing(uint32(2*n + 4))
	if err != nil {
		return fmt.Errorf("queue %d: io_uring setup: %w", e.cfg.queueID, err)
	}
	e.ring = r

	efd, err := unix.Eventfd(0, unix.EFD_CLOEXEC)
	if err != nil {
		return fmt.Errorf("queue %d: eventfd: %w", e.cfg.queueID, err)
	}
	e.wakeFd = efd
	if e.wakeBuf, err = uring.AllocOffHeap(8); err != nil {
		return fmt.Errorf("queue %d: %w", e.cfg.queueID, err)
	}
	if err := e.armWake(); err != nil {
		return err
	}
	for i := range e.tags {
		if err := e.prepIO(uapi.UBLK_IO_FETCH_REQ, i, 0); err != nil {
			return err
		}
		e.tags[i] = tagFetching
	}
	if _, err := e.ring.Submit(); err != nil {
		return fmt.Errorf("queue %d: submit FETCH_REQ: %w", e.cfg.queueID, err)
	}
	return nil
}

// teardown runs on the engine thread. Closing the ring cancels any command
// still in the kernel, which lets the driver abort or requeue its request.
// Data buffers are not touched here: the Queue frees them only once every
// engine has exited and no handler holds a request.
func (e *engine) teardown() {
	e.retireWake()
	e.wakeMu.Lock()
	if e.wakeFd >= 0 {
		_ = unix.Close(e.wakeFd)
		e.wakeFd = -1
	}
	e.wakeMu.Unlock()
	if e.ring != nil {
		_ = e.ring.Close()
	}
	// Free wakeBuf only once its read has completed. If it couldn't be
	// retired, leak it: a page is cheaper than a kernel write into memory
	// that has since been reused.
	if e.wakeBuf != nil && !e.wakeArmed {
		_ = uring.FreeOffHeap(e.wakeBuf)
		e.wakeBuf = nil
	}
}

// retireWake completes the armed eventfd read by writing the eventfd itself
// and reaps its completion, so no kernel write into wakeBuf is outstanding.
// Other completions reaped meanwhile are discarded: the engine is exiting and
// closing the ring cancels their commands.
func (e *engine) retireWake() {
	if !e.wakeArmed || e.ring == nil || e.wakeFd < 0 {
		return
	}
	_, _ = unix.Write(e.wakeFd, wakeValue[:])
	for tries := 0; tries < 50 && e.wakeArmed; tries++ {
		_, _ = e.ring.SubmitAndWait(1, 100*time.Millisecond)
		for {
			cqe := e.ring.PeekCQE()
			if cqe == nil {
				break
			}
			if cqe.UserData&kindMask == kindWake {
				e.wakeArmed = false
			}
			e.ring.CQAdvance(1)
		}
	}
}

func (e *engine) loop() {
	for {
		e.drainCompletions()
		if e.finished() {
			return
		}
		if e.testBeforeSleep != nil {
			e.testBeforeSleep()
		}
		// Publish that we may sleep, then look again: a completer pushes
		// before checking sleeping, so one of us always sees the other.
		e.sleeping.Store(true)
		wait := uint32(1)
		if e.head.Load() != nil {
			wait = 0
		}
		_, err := e.ring.SubmitAndWait(wait, e.cfg.waitInterval)
		e.sleeping.Store(false)
		if err != nil && !errors.Is(err, unix.ETIME) && !errors.Is(err, unix.EINTR) {
			e.fail(fmt.Errorf("queue %d: io_uring wait: %w", e.cfg.queueID, err))
			return
		}
		for {
			cqe := e.ring.PeekCQE()
			if cqe == nil {
				break
			}
			ud, res := cqe.UserData, cqe.Res
			e.ring.CQAdvance(1)
			e.handleCQE(ud, res)
		}
	}
}

// finished reports whether the engine can exit: every tag is done with the
// kernel and no handler still holds a request (whose buffer it may be using).
func (e *engine) finished() bool {
	if e.handlers.Load() != 0 {
		return false
	}
	if e.live == 0 {
		return true
	}
	return e.stopping.Load()
}

func (e *engine) handleCQE(ud uint64, res int32) {
	switch ud & kindMask {
	case kindWake:
		e.wakeArmed = false
		if !e.stopping.Load() || e.handlers.Load() != 0 {
			if err := e.armWake(); err != nil {
				e.fail(err)
			}
		}
	case kindIO:
		e.handleIO(int(ud&0xffff)-e.cfg.tagLo, res)
	default:
		e.fail(fmt.Errorf("queue %d: completion with unknown user_data %#x", e.cfg.queueID, ud))
	}
}

func (e *engine) handleIO(i int, res int32) {
	if i < 0 || i >= len(e.tags) {
		e.fail(fmt.Errorf("queue %d: completion for tag %d outside [%d,%d)", e.cfg.queueID,
			i+e.cfg.tagLo, e.cfg.tagLo, e.cfg.tagHi))
		return
	}
	tag := i + e.cfg.tagLo
	switch st := e.tags[i]; {
	case st != tagFetching && st != tagGetData:
		e.fail(fmt.Errorf("queue %d tag %d: unexpected completion (res %d) in state %d", e.cfg.queueID, tag, res, st))
	case res == uapi.UBLK_IO_RES_ABORT || res == -int32(syscall.ECANCELED):
		e.tags[i] = tagAborted
		e.live--
	case res == uapi.UBLK_IO_RES_NEED_GET_DATA && st == tagFetching:
		if err := e.prepIO(uapi.UBLK_IO_NEED_GET_DATA, i, 0); err != nil {
			e.fail(err)
			return
		}
		e.tags[i] = tagGetData
	case res == uapi.UBLK_IO_RES_OK:
		if e.stopping.Load() {
			e.tags[i] = tagOrphaned
			e.live--
			return
		}
		e.dispatch(i)
	default:
		cmd := "FETCH/COMMIT_AND_FETCH"
		if st == tagGetData {
			cmd = "NEED_GET_DATA"
		}
		e.fail(fmt.Errorf("queue %d tag %d: %s failed: %w", e.cfg.queueID, tag, cmd, syscall.Errno(-res)))
	}
}

// fail records the first fatal error and starts draining: no new requests are
// dispatched, in-flight ones are still committed, then the engine exits.
func (e *engine) fail(err error) {
	if e.err == nil {
		e.err = err
		if e.cfg.logger != nil {
			e.cfg.logger.Printf("ublk: %v", err)
		}
	}
	e.stopping.Store(true)
}

func (e *engine) descriptor(tag int) uapi.UblksrvIODesc {
	base := unsafe.Add(e.cfg.desc, uintptr(tag)*e.cfg.descStride)
	return uapi.UblksrvIODesc{
		OpFlags:     atomic.LoadUint32((*uint32)(base)),
		NrSectors:   atomic.LoadUint32((*uint32)(unsafe.Add(base, 4))),
		StartSector: atomic.LoadUint64((*uint64)(unsafe.Add(base, 8))),
		Addr:        atomic.LoadUint64((*uint64)(unsafe.Add(base, 16))),
	}
}

func (e *engine) buffer(tag int) unsafe.Pointer {
	return unsafe.Add(e.cfg.bufs, tag*e.cfg.bufSize)
}

func (e *engine) dispatch(i int) {
	tag := i + e.cfg.tagLo
	d := e.descriptor(tag)
	r := &e.reqs[i]
	r.Op = Op(d.OpFlags & 0xff)
	r.Flags = RequestFlags(d.OpFlags &^ 0xff)
	r.Offset = int64(d.StartSector) << uapi.SectorShift
	r.Length = int64(d.NrSectors) << uapi.SectorShift
	r.NrZones = 0
	r.Data = nil
	r.result, r.lba = 0, 0
	e.tags[i] = tagHandling
	e.handlers.Add(1)

	if r.Op == OpReportZones {
		r.NrZones = d.NrSectors
		r.Length = int64(e.cfg.bufSize)
	}
	if r.Op.carriesData() {
		if r.Length > int64(e.cfg.bufSize) {
			r.state.Store(reqAsync)
			r.Complete(fmt.Errorf("%d-byte request exceeds the %d-byte tag buffer: %w",
				r.Length, e.cfg.bufSize, syscall.EIO))
			return
		}
		r.Data = unsafe.Slice((*byte)(e.buffer(tag)), int(r.Length))
	}

	if e.cfg.inline {
		r.state.Store(reqDispatching)
		e.call(r)
		if !r.state.CompareAndSwap(reqDispatching, reqAsync) {
			e.commit(r) // completed before the handler returned
		}
		return
	}
	r.state.Store(reqAsync)
	go e.call(r)
}

// call runs the handler, copying a user-copy write's data in first. A panic in
// the handler fails the request with EIO instead of killing the server with
// I/O in flight.
func (e *engine) call(r *Request) {
	defer func() {
		if p := recover(); p != nil {
			if e.cfg.logger != nil {
				e.cfg.logger.Printf("ublk: handler panic on queue %d tag %d %s: %v", r.Queue, r.Tag, r.Op, p)
			}
			if s := r.state.Load(); s == reqDispatching || s == reqAsync {
				r.finish(-int32(syscall.EIO))
			}
		}
	}()
	if e.cfg.userCopy && r.Data != nil && (r.Op == OpWrite || r.Op == OpZoneAppend) {
		if err := e.copyIn(r); err != nil {
			r.Complete(err)
			return
		}
	}
	e.cfg.handler.HandleRequest(r)
}

// userCopyPos is the /dev/ublkcN file position addressing a request's data
// (UBLKSRV_IO_BUF_OFFSET + qid/tag/offset encoding).
func userCopyPos(q uint16, tag uint16, off int64) int64 {
	return uapi.UBLKSRV_IO_BUF_OFFSET + int64(q)<<uapi.UBLK_QID_OFF + int64(tag)<<uapi.UBLK_TAG_OFF + off
}

func (e *engine) copyIn(r *Request) error {
	for done := 0; done < len(r.Data); {
		n, err := unix.Pread(e.cfg.charFd, r.Data[done:], userCopyPos(r.Queue, r.Tag, int64(done)))
		if err != nil {
			return fmt.Errorf("user copy in: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("user copy in: short read at %d of %d: %w", done, len(r.Data), syscall.EIO)
		}
		done += n
	}
	return nil
}

func (e *engine) copyOut(r *Request, n int) error {
	for done := 0; done < n; {
		m, err := unix.Pwrite(e.cfg.charFd, r.Data[done:n], userCopyPos(r.Queue, r.Tag, int64(done)))
		if err != nil {
			return fmt.Errorf("user copy out: %w", err)
		}
		if m == 0 {
			return fmt.Errorf("user copy out: short write at %d of %d: %w", done, n, syscall.EIO)
		}
		done += m
	}
	return nil
}

// beforeCommit runs on the completing goroutine, before the request is handed
// back to the engine: a user-copy read's data goes to the kernel here.
func (e *engine) beforeCommit(r *Request) {
	if e.cfg.userCopy && r.result > 0 && (r.Op == OpRead || r.Op == OpReportZones) {
		if err := e.copyOut(r, int(r.result)); err != nil {
			r.result = -Errno(err)
		}
	}
}

// push hands a completed request to the engine from any goroutine.
func (e *engine) push(r *Request) {
	for {
		old := e.head.Load()
		r.next = old
		if e.head.CompareAndSwap(old, r) {
			break
		}
	}
	if e.sleeping.Load() {
		e.wake()
	}
}

var wakeValue = [8]byte{1}

func (e *engine) wake() {
	e.wakeMu.RLock()
	if e.wakeFd >= 0 && e.sleeping.CompareAndSwap(true, false) {
		_, _ = unix.Write(e.wakeFd, wakeValue[:])
	}
	e.wakeMu.RUnlock()
}

func (e *engine) drainCompletions() {
	for r := e.head.Swap(nil); r != nil; {
		next := r.next
		r.next = nil
		e.commit(r)
		r = next
	}
}

// commit sends a finished request's result and re-arms the tag's fetch with
// one COMMIT_AND_FETCH_REQ. Engine thread only.
func (e *engine) commit(r *Request) {
	i := int(r.Tag) - e.cfg.tagLo
	addr := uint64(0)
	if !e.cfg.userCopy {
		addr = uint64(uintptr(e.buffer(int(r.Tag))))
	} else if r.Op == OpZoneAppend && r.result >= 0 {
		addr = r.lba
	}
	r.state.Store(reqIdle)
	r.Data = nil
	e.handlers.Add(-1)
	if err := e.prepCommit(i, r.result, addr); err != nil {
		e.fail(err)
		return
	}
	e.tags[i] = tagFetching
}

func (e *engine) prepCommit(i int, result int32, addr uint64) error {
	return e.prepCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ, i, result, addr)
}

// prepIO prepares FETCH_REQ or NEED_GET_DATA, which carry the tag's buffer
// address in copy mode and none in user-copy mode.
func (e *engine) prepIO(nr uint32, i int, result int32) error {
	addr := uint64(0)
	if !e.cfg.userCopy {
		addr = uint64(uintptr(e.buffer(i + e.cfg.tagLo)))
	}
	return e.prepCmd(nr, i, result, addr)
}

func (e *engine) prepCmd(nr uint32, i int, result int32, addr uint64) error {
	sqe, err := e.getSQE()
	if err != nil {
		return err
	}
	tag := uint16(i + e.cfg.tagLo)
	uring.PrepUringCmd(sqe, int32(e.cfg.charFd), uapi.UblkIOCmd(nr), nil)
	cmd := (*uapi.UblksrvIOCmd)(unsafe.Pointer(sqe.Cmd()))
	cmd.QID = e.cfg.queueID
	cmd.Tag = tag
	cmd.Result = result
	cmd.Addr = addr
	sqe.UserData = kindIO | uint64(tag)
	return nil
}

func (e *engine) armWake() error {
	sqe, err := e.getSQE()
	if err != nil {
		return err
	}
	uring.PrepRead(sqe, int32(e.wakeFd), e.wakeBuf, 0)
	sqe.UserData = kindWake
	e.wakeArmed = true
	return nil
}

// getSQE returns a free SQE, submitting what is queued if the SQ is full.
func (e *engine) getSQE() (*uring.SQE, error) {
	if sqe := e.ring.GetSQE(); sqe != nil {
		return sqe, nil
	}
	if _, err := e.ring.Submit(); err != nil {
		return nil, fmt.Errorf("queue %d: submit: %w", e.cfg.queueID, err)
	}
	if sqe := e.ring.GetSQE(); sqe != nil {
		return sqe, nil
	}
	return nil, fmt.Errorf("queue %d: io_uring submission queue full", e.cfg.queueID)
}

// abandon asks the engine to stop without the kernel stopping the device:
// stop dispatching, commit what handlers still hold, and exit, leaving any
// newly fetched request to the kernel. Safe from any goroutine.
func (e *engine) abandon() {
	e.stopping.Store(true)
	e.sleeping.Store(true) // force the wake write even if the engine is mid-loop
	e.wake()
}
