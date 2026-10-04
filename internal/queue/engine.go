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
	kindIO    uint64 = 1 << 56 // FETCH, COMMIT_AND_FETCH or NEED_GET_DATA for a tag
	kindWake  uint64 = 2 << 56 // the eventfd read that wakes the engine
	kindReg   uint64 = 3 << 56 // zero copy: REGISTER_IO_BUF for a tag
	kindZC    uint64 = 4 << 56 // zero copy: the backing-file operation for a tag
	kindUnreg uint64 = 5 << 56 // zero copy: UNREGISTER_IO_BUF for a tag
	kindPrep  uint64 = 6 << 56 // batch: PREP_IO_CMDS
	kindFetch uint64 = 7 << 56 // batch: the multishot FETCH_IO_CMDS
	kindBatch uint64 = 8 << 56 // batch: a COMMIT_IO_CMDS; low bits index the commit buffer
	kindMask  uint64 = 0xff << 56
)

// Batch I/O (UBLK_F_BATCH_IO, 7.0): one PREP_IO_CMDS registers every tag, a
// multishot FETCH_IO_CMDS delivers ready tags as u16 lists into a
// provided-buffer ring, and completions go back many at a time in
// COMMIT_IO_CMDS element buffers.
const (
	batchTagBufs    = 16  // provided buffers for tag lists
	batchTagBufSize = 256 // bytes each: 128 tags, the kernel's per-CQE maximum
	batchCommitBufs = 4   // COMMIT_IO_CMDS element buffers in flight at once
)

// tagRing is the provided-buffer ring FETCH_IO_CMDS writes tag lists into.
type tagRing interface {
	Add(buf []byte, bid uint16, offset int)
	Advance(count int)
}

// tagRingProvider is implemented by the engine's ring when it can register a
// provided-buffer ring (uring.IoUring, wrapped by defaultRing).
type tagRingProvider interface {
	NewTagRing(bgid uint16, entries uint32) (tagRing, error)
}

// Per-tag states, owned by the engine thread.
const (
	tagFetching    uint8 = iota // FETCH or COMMIT_AND_FETCH in flight: the kernel owns the tag
	tagGetData                  // NEED_GET_DATA in flight
	tagHandling                 // the handler owns the request
	tagAborted                  // the kernel aborted the tag: never fetch it again
	tagOrphaned                 // a request arrived while abandoning: left for the kernel to requeue or fail
	tagRegistering              // zero copy: REGISTER_IO_BUF in flight
	tagFileIO                   // zero copy: the backing-file operation in flight
)

// zeroCopyConfig is the backing file a zero-copy engine moves request data
// to and from with io_uring fixed-buffer operations; the server never sees
// the bytes.
type zeroCopyConfig struct {
	fd   int
	base int64 // device offset 0 is file offset base
	auto bool  // UBLK_F_AUTO_BUF_REG: the kernel registers each request's buffer itself
}

// bufRegistrar is implemented by uring.IoUring; zero copy needs a sparse
// buffer table on the engine's ring.
type bufRegistrar interface {
	RegisterBuffersSparse(n uint32) error
}

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
	zeroCopy     *zeroCopyConfig
	batch        bool          // UBLK_F_BATCH_IO
	zoned        bool          // batch elements carry the zone-append LBA
	shmem        *SharedMemory // UBLK_F_SHMEM_ZC regions, or nil
	// UBLK_F_INTEGRITY: per-tag metadata buffers of integSize bytes, holding
	// integMeta bytes per integInterval bytes of data.
	integ         unsafe.Pointer
	integSize     int
	integInterval int
	integMeta     int
	handler       Handler
	inline        bool
	cpu           int // -1: no affinity
	logger        interfaces.Logger
	newRing       func(entries uint32) (ring, error)
	waitInterval  time.Duration
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

	// batch I/O state (engine thread only)
	tagBufs       []byte
	tags16        tagRing
	prepBuf       []byte
	commitBufs    [batchCommitBufs][]byte
	commitSent    [batchCommitBufs][]*Request // nil slot: buffer free
	pendingCommit []*Request

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

	if e.cfg.zeroCopy != nil {
		br, ok := e.ring.(bufRegistrar)
		if !ok {
			return fmt.Errorf("queue %d: zero copy needs a ring with a buffer table", e.cfg.queueID)
		}
		// One slot per tag, indexed by tag; the kernel installs each request's
		// pages there (UBLK_F_AUTO_BUF_REG or REGISTER_IO_BUF).
		if err := br.RegisterBuffersSparse(uint32(e.cfg.tagHi)); err != nil {
			return fmt.Errorf("queue %d: register sparse buffer table: %w", e.cfg.queueID, err)
		}
	}

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
	if e.cfg.batch {
		if err := e.setupBatch(); err != nil {
			return err
		}
	} else {
		for i := range e.tags {
			if err := e.prepIO(uapi.UBLK_IO_FETCH_REQ, i, 0); err != nil {
				return err
			}
			e.tags[i] = tagFetching
		}
	}
	if _, err := e.ring.Submit(); err != nil {
		return fmt.Errorf("queue %d: submit fetches: %w", e.cfg.queueID, err)
	}
	return nil
}

func (e *engine) batchFlags() uint16 {
	var f uint16
	if !e.cfg.userCopy {
		f |= uapi.UBLK_BATCH_F_HAS_BUF_ADDR // copy mode: each element names the tag's buffer
	}
	if e.cfg.zoned {
		f |= uapi.UBLK_BATCH_F_HAS_ZONE_LBA
	}
	return f
}

// putElem writes one batch element: header, then the buffer address and zone
// LBA if the flags call for them.
func (e *engine) putElem(b []byte, flags uint16, tag uint16, result int32, lba uint64) {
	h := (*uapi.UblkElemHeader)(unsafe.Pointer(&b[0]))
	h.Tag, h.BufIndex, h.Result = tag, 0, result
	off := 8
	if flags&uapi.UBLK_BATCH_F_HAS_BUF_ADDR != 0 {
		*(*uint64)(unsafe.Pointer(&b[off])) = uint64(uintptr(e.buffer(int(tag))))
		off += 8
	}
	if flags&uapi.UBLK_BATCH_F_HAS_ZONE_LBA != 0 {
		*(*uint64)(unsafe.Pointer(&b[off])) = lba
	}
}

func (e *engine) prepBatchCmd(op uint32, flags uint16, nr int, elemBytes uint8, buf []byte, ud uint64) error {
	sqe, err := e.getSQE()
	if err != nil {
		return err
	}
	uring.PrepUringCmd(sqe, int32(e.cfg.charFd), op, nil)
	h := (*uapi.UblkBatchIO)(unsafe.Pointer(sqe.Cmd()))
	h.QID, h.Flags, h.NrElem, h.ElemBytes = e.cfg.queueID, flags, uint16(nr), elemBytes
	if buf != nil {
		sqe.Addr = uint64(uintptr(unsafe.Pointer(&buf[0])))
	}
	sqe.UserData = ud
	return nil
}

func (e *engine) setupBatch() error {
	tp, ok := e.ring.(tagRingProvider)
	if !ok {
		return fmt.Errorf("queue %d: batch I/O needs a ring with provided-buffer rings", e.cfg.queueID)
	}
	var err error
	if e.tagBufs, err = uring.AllocOffHeap(batchTagBufs * batchTagBufSize); err != nil {
		return err
	}
	if e.tags16, err = tp.NewTagRing(0, batchTagBufs); err != nil {
		return fmt.Errorf("queue %d: register tag buffer ring: %w", e.cfg.queueID, err)
	}
	for i := 0; i < batchTagBufs; i++ {
		e.tags16.Add(e.tagBufs[i*batchTagBufSize:(i+1)*batchTagBufSize], uint16(i), i)
	}
	e.tags16.Advance(batchTagBufs)

	flags := e.batchFlags()
	eb := int(uapi.BatchElemBytes(flags))
	n := e.cfg.tagHi - e.cfg.tagLo
	if e.prepBuf, err = uring.AllocOffHeap(n * eb); err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		e.putElem(e.prepBuf[i*eb:], flags, uint16(e.cfg.tagLo+i), 0, 0)
		e.tags[i] = tagFetching
	}
	for i := range e.commitBufs {
		if e.commitBufs[i], err = uring.AllocOffHeap(n * eb); err != nil {
			return err
		}
	}
	if err := e.prepBatchCmd(uapi.UBLK_U_IO_PREP_IO_CMDS, flags, n, uint8(eb), e.prepBuf, kindPrep); err != nil {
		return err
	}
	e.live = 1 // in batch mode the multishot fetch is what keeps the engine alive
	return e.armFetch()
}

func (e *engine) armFetch() error {
	sqe, err := e.getSQE()
	if err != nil {
		return err
	}
	uring.PrepUringCmd(sqe, int32(e.cfg.charFd), uapi.UBLK_U_IO_FETCH_IO_CMDS, nil)
	h := (*uapi.UblkBatchIO)(unsafe.Pointer(sqe.Cmd()))
	h.QID, h.ElemBytes = e.cfg.queueID, 2
	setMultishot(sqe)
	sqe.UserData = kindFetch
	return nil
}

// setMultishot makes a FETCH_IO_CMDS multishot, selecting tag-list buffers
// from group 0.
func setMultishot(sqe *uring.SQE) {
	sqe.OpFlags |= uring.IORING_URING_CMD_MULTISHOT
	sqe.SetBufferSelect(0)
}

func (e *engine) handleFetch(res int32, flags uint32) {
	bid, hasBuf := uint16(flags>>uring.IORING_CQE_BUFFER_SHIFT), flags&uring.IORING_CQE_F_BUFFER != 0
	if hasBuf {
		buf := e.tagBufs[int(bid)*batchTagBufSize : (int(bid)+1)*batchTagBufSize]
		if res > 0 {
			for k := 0; k+1 < int(res); k += 2 {
				tag := int(buf[k]) | int(buf[k+1])<<8
				i := tag - e.cfg.tagLo
				if i < 0 || i >= len(e.tags) || e.tags[i] != tagFetching {
					st := -1
					if i >= 0 && i < len(e.tags) {
						st = int(e.tags[i])
					}
					e.fail(fmt.Errorf("queue %d: batch fetch delivered tag %d in state %d", e.cfg.queueID, tag, st))
					continue
				}
				if e.stopping.Load() {
					e.tags[i] = tagOrphaned
					continue
				}
				e.dispatch(i)
			}
		}
		e.tags16.Add(buf, bid, 0)
		e.tags16.Advance(1)
	}
	if flags&uring.IORING_CQE_F_MORE != 0 {
		return
	}
	switch {
	case res == uapi.UBLK_IO_RES_ABORT || res == -int32(syscall.ECANCELED):
		e.live = 0 // stopped, quiesced or cancelled: no more requests
	case res >= 0 || res == -int32(syscall.ENOBUFS):
		if !e.stopping.Load() {
			if err := e.armFetch(); err != nil {
				e.fail(err)
			}
		} else {
			e.live = 0
		}
	default:
		e.fail(fmt.Errorf("queue %d: FETCH_IO_CMDS failed: %w", e.cfg.queueID, syscall.Errno(-res)))
		e.live = 0
	}
}

// flushBatchCommits sends the completions gathered this round as one
// COMMIT_IO_CMDS, if a commit buffer is free.
func (e *engine) flushBatchCommits() {
	if len(e.pendingCommit) == 0 {
		return
	}
	slot := -1
	for i := range e.commitSent {
		if e.commitSent[i] == nil {
			slot = i
			break
		}
	}
	if slot < 0 {
		return // all commit buffers in flight; retried when one completes
	}
	flags := e.batchFlags()
	eb := int(uapi.BatchElemBytes(flags))
	n := min(len(e.pendingCommit), len(e.commitBufs[slot])/eb)
	sent := append([]*Request(nil), e.pendingCommit[:n]...)
	for k, r := range sent {
		lba := uint64(0)
		if r.Op == OpZoneAppend && r.result >= 0 {
			lba = r.lba
		}
		e.putElem(e.commitBufs[slot][k*eb:], flags, r.Tag, r.result, lba)
	}
	if err := e.prepBatchCmd(uapi.UBLK_U_IO_COMMIT_IO_CMDS, flags, n, uint8(eb), e.commitBufs[slot], kindBatch|uint64(slot)); err != nil {
		e.fail(err)
		return
	}
	e.pendingCommit = e.pendingCommit[n:]
	e.commitSent[slot] = sent
	// The tags go back to the kernel as soon as it consumes the commit, and
	// it may hand one a new request — announced by the fetch — before this
	// command's own completion arrives. Treat them as returned now; committed
	// takes back any the kernel did not consume.
	for _, r := range sent {
		e.tags[int(r.Tag)-e.cfg.tagLo] = tagFetching
	}
}

// committed handles a COMMIT_IO_CMDS completion: res is the bytes of the
// element buffer consumed; elements after the first failure were not
// committed and go back on the pending list.
func (e *engine) committed(slot int, res int32) {
	sent := e.commitSent[slot]
	e.commitSent[slot] = nil
	eb := int32(uapi.BatchElemBytes(e.batchFlags()))
	done := 0
	if res > 0 {
		done = int(res / eb)
	}
	if done < len(sent) {
		for _, r := range sent[done:] {
			e.tags[int(r.Tag)-e.cfg.tagLo] = tagHandling // not consumed: still ours
		}
		if res < 0 && done == 0 && res != -int32(syscall.EBUSY) {
			e.fail(fmt.Errorf("queue %d: COMMIT_IO_CMDS failed: %w", e.cfg.queueID, syscall.Errno(-res)))
		}
		// Not consumed: still owned by us; send them again.
		e.pendingCommit = append(append([]*Request(nil), sent[done:]...), e.pendingCommit...)
	}
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
	// Batch buffers: the ring (and with it the tag buffer ring) is closed,
	// and finished() waited for every commit, so the kernel holds none.
	if e.ring != nil && e.cfg.batch {
		for _, b := range append([][]byte{e.tagBufs, e.prepBuf}, e.commitBufs[:]...) {
			if b != nil {
				_ = uring.FreeOffHeap(b)
			}
		}
		e.tagBufs, e.prepBuf = nil, nil
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
		if e.cfg.batch {
			e.flushBatchCommits()
		}
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
			ud, res, flags := cqe.UserData, cqe.Res, cqe.Flags
			e.ring.CQAdvance(1)
			e.handleCQE(ud, res, flags)
		}
	}
}

// finished reports whether the engine can exit: every tag is done with the
// kernel and no handler still holds a request (whose buffer it may be using).
func (e *engine) finished() bool {
	if e.handlers.Load() != 0 || len(e.pendingCommit) != 0 {
		return false
	}
	for _, sent := range e.commitSent {
		if sent != nil {
			return false
		}
	}
	if e.live == 0 {
		return true
	}
	return e.stopping.Load()
}

func (e *engine) handleCQE(ud uint64, res int32, flags uint32) {
	switch ud & kindMask {
	case kindPrep:
		if res < 0 {
			e.fail(fmt.Errorf("queue %d: PREP_IO_CMDS failed: %w", e.cfg.queueID, syscall.Errno(-res)))
			e.live = 0
		}
	case kindFetch:
		e.handleFetch(res, flags)
	case kindBatch:
		e.committed(int(ud&0xff), res)
	case kindWake:
		e.wakeArmed = false
		if !e.stopping.Load() || e.handlers.Load() != 0 {
			if err := e.armWake(); err != nil {
				e.fail(err)
			}
		}
	case kindIO:
		e.handleIO(int(ud&0xffff)-e.cfg.tagLo, res)
	case kindReg:
		e.registered(int(ud&0xffff)-e.cfg.tagLo, res)
	case kindZC:
		e.fileIODone(int(ud&0xffff)-e.cfg.tagLo, res)
	case kindUnreg:
		if res < 0 {
			e.fail(fmt.Errorf("queue %d tag %d: UNREGISTER_IO_BUF failed: %w", e.cfg.queueID, ud&0xffff, syscall.Errno(-res)))
		}
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

	if e.cfg.zeroCopy != nil {
		e.dispatchZeroCopy(i, r)
		return
	}
	if r.Op == OpReportZones {
		// nr_sectors carries the number of zones asked for; the report is
		// that many 64-byte struct blk_zone entries, pre-zeroed so a short
		// report ends with a zero-length zone as the kernel expects.
		r.NrZones = d.NrSectors
		r.Length = min(int64(d.NrSectors)*BlkZoneSize, int64(e.cfg.bufSize))
	}
	if r.Flags&FlagSharedMemory != 0 {
		// The request's pages are in a region we registered: no copy either
		// way, Data is that memory.
		var ok bool
		if e.cfg.shmem != nil {
			r.Data, ok = e.cfg.shmem.slice(d.Addr, r.Length)
		}
		if !ok {
			r.state.Store(reqAsync)
			r.Complete(fmt.Errorf("shared-memory request at %#x+%d is outside every registered region: %w",
				d.Addr, r.Length, syscall.EIO))
			return
		}
	} else if r.Op.carriesData() {
		if r.Length > int64(e.cfg.bufSize) {
			r.state.Store(reqAsync)
			r.Complete(fmt.Errorf("%d-byte request exceeds the %d-byte tag buffer: %w",
				r.Length, e.cfg.bufSize, syscall.EIO))
			return
		}
		r.Data = unsafe.Slice((*byte)(e.buffer(tag)), int(r.Length))
		if r.Op == OpReportZones {
			clear(r.Data)
		}
	}
	r.Integrity = nil
	if r.Flags&FlagIntegrity != 0 && e.cfg.integ != nil {
		n := int(r.Length) / e.cfg.integInterval * e.cfg.integMeta
		if n > e.cfg.integSize {
			r.state.Store(reqAsync)
			r.Complete(fmt.Errorf("%d-byte integrity buffer exceeds %d: %w", n, e.cfg.integSize, syscall.EIO))
			return
		}
		r.Integrity = unsafe.Slice((*byte)(unsafe.Add(e.cfg.integ, tag*e.cfg.integSize)), n)
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
	if e.cfg.userCopy && r.Data != nil && r.Flags&FlagSharedMemory == 0 && (r.Op == OpWrite || r.Op == OpZoneAppend) {
		if err := e.copyIn(r); err != nil {
			r.Complete(err)
			return
		}
		if r.Integrity != nil {
			if err := e.copyIntegrity(r, false); err != nil {
				r.Complete(err)
				return
			}
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
	if e.cfg.userCopy && r.result > 0 && r.Flags&FlagSharedMemory == 0 && (r.Op == OpRead || r.Op == OpReportZones) {
		if err := e.copyOut(r, int(r.result)); err != nil {
			r.result = -Errno(err)
		} else if r.Integrity != nil {
			if err := e.copyIntegrity(r, true); err != nil {
				r.result = -Errno(err)
			}
		}
	}
}

// copyIntegrity moves a request's integrity metadata: from the kernel for a
// write, to it for a read (the same user-copy position with
// UBLKSRV_IO_INTEGRITY_FLAG set).
func (e *engine) copyIntegrity(r *Request, out bool) error {
	pos := userCopyPos(r.Queue, r.Tag, 0) | uapi.UBLKSRV_IO_INTEGRITY_FLAG
	for done := 0; done < len(r.Integrity); {
		var n int
		var err error
		if out {
			n, err = unix.Pwrite(e.cfg.charFd, r.Integrity[done:], pos+int64(done))
		} else {
			n, err = unix.Pread(e.cfg.charFd, r.Integrity[done:], pos+int64(done))
		}
		if err != nil {
			return fmt.Errorf("integrity user copy: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("integrity user copy: short transfer at %d of %d: %w", done, len(r.Integrity), syscall.EIO)
		}
		done += n
	}
	return nil
}

// partialReadsOK reports whether the kernel honors a short read result by
// resubmitting the remainder, which it does only when it copies the data
// itself (neither user copy nor zero copy).
func (e *engine) partialReadsOK() bool { return !e.cfg.userCopy && e.cfg.zeroCopy == nil }

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
	if e.cfg.batch {
		// Sent with the round's other completions by flushBatchCommits. The
		// handler is done with it; the tag returns to the kernel once the
		// commit is consumed.
		r.state.Store(reqIdle)
		r.Data, r.Integrity = nil, nil
		e.handlers.Add(-1)
		e.pendingCommit = append(e.pendingCommit, r)
		return
	}
	i := int(r.Tag) - e.cfg.tagLo
	addr := uint64(0)
	if e.cfg.zeroCopy != nil {
		// The commit itself unregisters a buffer the kernel registered
		// (AUTO_BUF_REG); one we registered — including after an automatic
		// registration fell back — must be unregistered first, linked so it
		// runs before the commit.
		if r.zcManual {
			if err := e.prepBufCmd(uapi.UBLK_IO_UNREGISTER_IO_BUF, kindUnreg, i, uring.IOSQE_IO_LINK); err != nil {
				e.fail(err)
				return
			}
		}
		r.zcManual = false
	} else if !e.cfg.userCopy {
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
	if !e.cfg.userCopy && e.cfg.zeroCopy == nil {
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
	if zc := e.cfg.zeroCopy; zc != nil && zc.auto && nr != uapi.UBLK_IO_NEED_GET_DATA {
		// FETCH and COMMIT_AND_FETCH carry the auto-registration slot in
		// sqe->addr: the tag's slot, falling back to a manual registration
		// (UBLK_IO_F_NEED_REG_BUF) rather than failing the request.
		sqe.Addr = uapi.UblkAutoBufReg{Index: tag, Flags: uapi.UBLK_AUTO_BUF_REG_FALLBACK}.SQEAddr()
	}
	return nil
}

// prepBufCmd prepares REGISTER_IO_BUF or UNREGISTER_IO_BUF for a tag, with
// the tag's buffer-table slot in addr.
func (e *engine) prepBufCmd(nr uint32, kind uint64, i int, flags uint8) error {
	sqe, err := e.getSQE()
	if err != nil {
		return err
	}
	tag := uint16(i + e.cfg.tagLo)
	uring.PrepUringCmd(sqe, int32(e.cfg.charFd), uapi.UblkIOCmd(nr), nil)
	cmd := (*uapi.UblksrvIOCmd)(unsafe.Pointer(sqe.Cmd()))
	cmd.QID = e.cfg.queueID
	cmd.Tag = tag
	cmd.Addr = uint64(tag)
	sqe.UserData = kind | uint64(tag)
	sqe.Flags |= flags
	return nil
}

// dispatchZeroCopy serves a request entirely in the kernel: the request's
// pages are (or get) registered in the ring's buffer table, and a fixed-buffer
// operation on the backing file moves the data. No goroutine, no copy.
func (e *engine) dispatchZeroCopy(i int, r *Request) {
	r.zcManual = false
	if r.Op != OpRead && r.Op != OpWrite {
		e.startFileIO(i, r)
		return
	}
	if e.cfg.zeroCopy.auto && r.Flags&FlagNeedRegBuf == 0 {
		e.startFileIO(i, r) // the kernel registered it on delivery
		return
	}
	if err := e.prepBufCmd(uapi.UBLK_IO_REGISTER_IO_BUF, kindReg, i, 0); err != nil {
		e.fail(err)
		return
	}
	e.tags[i] = tagRegistering
}

func (e *engine) registered(i int, res int32) {
	if i < 0 || i >= len(e.tags) || e.tags[i] != tagRegistering {
		e.fail(fmt.Errorf("queue %d: unexpected REGISTER_IO_BUF completion for tag %d", e.cfg.queueID, i+e.cfg.tagLo))
		return
	}
	r := &e.reqs[i]
	if res < 0 {
		e.tags[i] = tagHandling
		r.state.Store(reqAsync)
		r.finish(res)
		return
	}
	r.zcManual = true
	e.startFileIO(i, r)
}

// startFileIO submits the backing-file operation for a zero-copy request.
func (e *engine) startFileIO(i int, r *Request) {
	zc := e.cfg.zeroCopy
	sqe, err := e.getSQE()
	if err != nil {
		e.fail(err)
		return
	}
	fd := int32(zc.fd)
	off := uint64(zc.base + r.Offset)
	n := uint64(r.Length)
	switch r.Op {
	case OpRead:
		uring.PrepReadFixed(sqe, fd, 0, uint32(n), off, r.Tag)
	case OpWrite:
		uring.PrepWriteFixed(sqe, fd, 0, uint32(n), off, r.Tag)
		if r.Flags&FlagFUA != 0 {
			sqe.OpFlags |= unix.RWF_DSYNC
		}
	case OpFlush:
		uring.PrepFsync(sqe, fd, uring.IORING_FSYNC_DATASYNC)
	case OpDiscard:
		uring.PrepFallocate(sqe, fd, unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, off, n)
	case OpWriteZeroes:
		uring.PrepFallocate(sqe, fd, unix.FALLOC_FL_ZERO_RANGE|unix.FALLOC_FL_KEEP_SIZE, off, n)
	default:
		uring.PrepNop(sqe)
		sqe.UserData = kindZC | uint64(r.Tag)
		e.tags[i] = tagFileIO
		r.zcUnsupported = true
		return
	}
	sqe.UserData = kindZC | uint64(r.Tag)
	e.tags[i] = tagFileIO
}

func (e *engine) fileIODone(i int, res int32) {
	if i < 0 || i >= len(e.tags) || e.tags[i] != tagFileIO {
		e.fail(fmt.Errorf("queue %d: unexpected file I/O completion for tag %d", e.cfg.queueID, i+e.cfg.tagLo))
		return
	}
	r := &e.reqs[i]
	e.tags[i] = tagHandling
	r.state.Store(reqAsync)
	switch {
	case r.zcUnsupported:
		r.zcUnsupported = false
		r.finish(-int32(syscall.EOPNOTSUPP))
	case res < 0:
		r.finish(res)
	case r.Op == OpRead || r.Op == OpWrite:
		// A short transfer means the file ended early: a read may complete
		// partially (the kernel resubmits the rest), a write may not.
		r.CompleteN(int(res), nil)
	default:
		r.finish(0)
	}
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
