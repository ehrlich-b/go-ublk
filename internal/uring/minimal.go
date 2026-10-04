// Package uring is a pure-Go io_uring core (IoUring) and the ublk command
// ring (Ring) built on it.
package uring

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"golang.org/x/sys/unix"
)

// The ublk command payloads are copied into the SQE command area verbatim.
var (
	_ [32]byte = [unsafe.Sizeof(uapi.UblksrvCtrlCmd{})]byte{}
	_ [16]byte = [unsafe.Sizeof(uapi.UblksrvIOCmd{})]byte{}
)

const (
	// defaultCtrlTimeout bounds one synchronous control command (Critical Bug #8).
	defaultCtrlTimeout = 10 * time.Second
	// ctrlWaitSlice bounds each io_uring_enter of a control or I/O wait, so a
	// waiter notices a deadline, Close or context cancellation promptly.
	ctrlWaitSlice = 100 * time.Millisecond
	// ctrlTagBase marks the internal user_data of a synchronous control
	// command, so its CQE can be told apart from a straggler's;
	// ctrlCancelTagBase marks the ASYNC_CANCEL sent for one.
	ctrlTagBase       = uint64(0xC7) << 56
	ctrlCancelTagBase = uint64(0xC8) << 56
	ctrlTagSeqMask    = 1<<56 - 1
	// defaultCtrlCancelGrace bounds the wait for a cancelled control command
	// to finish before it is abandoned.
	defaultCtrlCancelGrace = time.Second
	// ctrlScratchSize holds the largest buffer a ublksrv_ctrl_cmd can describe
	// (len is a u16).
	ctrlScratchSize = 64 << 10
	// ctrlCmd field offsets in struct ublksrv_ctrl_cmd.
	ctrlCmdLenOffset  = 6
	ctrlCmdAddrOffset = 8
)

// AsyncHandle represents a pending io_uring operation
type AsyncHandle struct {
	userData uint64
	ring     *minimalRing
}

// Wait polls for completion of async operation
func (h *AsyncHandle) Wait(timeout time.Duration) (Result, error) {
	logger := logging.Default()
	logger.Debug("waiting for completion", "userData", h.userData, "timeout", timeout)
	deadline := time.Now().Add(timeout)
	attempts := 0
	for time.Now().Before(deadline) {
		attempts++
		result, err := h.ring.tryGetCompletion(h.userData)
		if err == nil {
			logger.Debug("found completion", "attempts", attempts, "result", result.Value())
			return result, nil
		}
		if attempts%100 == 0 {
			logger.Debug("still waiting for completion", "attempts", attempts, "error", err.Error())
		}
		// 10ms balances responsiveness with CPU overhead for async polling.
		time.Sleep(10 * time.Millisecond)
	}
	logger.Debug("timeout waiting for completion", "attempts", attempts)
	return nil, fmt.Errorf("timeout waiting for completion after %d attempts", attempts)
}

// minimalRing implements Ring for ublk on an IoUring with 128-byte SQEs
// (ublk_ctrl_uring_cmd rejects control commands in smaller ones with EINVAL)
// and 32-byte CQEs.
type minimalRing struct {
	core            *IoUring
	targetFd        int
	ctrlTimeout     time.Duration
	ctrlCancelGrace time.Duration
	// Pre-allocated so the I/O hot path does not allocate.
	resultsPool []Result
	cqePool     []minimalResult
	// Control path: ring-owned off-heap staging for ctrl_cmd.addr (see
	// SubmitCtrlCmd) and the sequence behind the internal user_data tags.
	ctrlScratch []byte
	ctrlSeq     uint64
	// Lifetime. Close can race a goroutine still inside a method: the queue
	// runner closes after a bounded join. Methods hold active; teardown runs
	// in Close if nothing is active, else in the last method to return, so
	// the rings are never unmapped under a live caller and a recycled fd
	// number is never entered. closing is set before Close touches the core
	// and closed after, which publishes those writes to that last method.
	active   atomic.Int32
	closing  atomic.Bool
	closed   atomic.Bool
	teardown sync.Once
	closeErr error
}

// NewMinimalRing creates the ublk command ring for ctrlFd (/dev/ublk-control
// or /dev/ublkcN), or with no target if ctrlFd is negative.
func NewMinimalRing(entries uint32, ctrlFd int32) (Ring, error) {
	return newMinimalRing(Config{Entries: entries, FD: ctrlFd})
}

func newMinimalRing(config Config) (*minimalRing, error) {
	logger := logging.Default()
	core, err := NewIoUring(SetupOptions{
		Entries: config.Entries,
		Flags:   IORING_SETUP_SQE128 | IORING_SETUP_CQE32 | config.Flags,
	})
	if err != nil {
		return nil, err
	}
	poolSize := max(int(core.CQEntries()), 64)
	r := &minimalRing{
		core:            core,
		targetFd:        int(config.FD),
		ctrlTimeout:     config.CtrlTimeout,
		ctrlCancelGrace: defaultCtrlCancelGrace,
		resultsPool:     make([]Result, 0, poolSize),
		cqePool:         make([]minimalResult, poolSize),
	}
	if r.ctrlTimeout == 0 {
		r.ctrlTimeout = defaultCtrlTimeout
	}
	// Register the target fd as fixed file 0, as ublksrv does. This pins the
	// file; Close drops the registration synchronously (see Close).
	if config.FD >= 0 {
		if err := core.RegisterFiles([]int32{config.FD}); err != nil {
			logger.Warn("failed to register files with io_uring", "error", err)
		} else {
			logger.Info("registered char device with io_uring", "fd", config.FD)
		}
	}
	return r, nil
}

func (r *minimalRing) acquire() bool {
	r.active.Add(1)
	if r.closing.Load() {
		r.release()
		return false
	}
	return true
}

func (r *minimalRing) release() {
	if r.active.Add(-1) == 0 && r.closed.Load() {
		r.teardown.Do(r.closeCore)
	}
}

// closeCore unmaps the rings and closes the fd. The control staging buffer
// is unmapped only if no abandoned command can still write to it (an
// abandoned one is dropped from ctrlScratch and deliberately leaked).
func (r *minimalRing) closeCore() {
	r.closeErr = r.core.Close()
	if r.ctrlScratch != nil {
		if err := FreeOffHeap(r.ctrlScratch); err != nil && r.closeErr == nil {
			r.closeErr = err
		}
		r.ctrlScratch = nil
	}
}

// Close releases the ring: every mapping, the staging buffer and the fd
// (Critical Bug #17). It is idempotent and safe to call while another
// goroutine is still inside a method; teardown is then deferred until that
// call returns, which a blocked wait does within 100ms, and later calls fail
// with ErrRingClosed.
func (r *minimalRing) Close() error {
	if !r.closing.CompareAndSwap(false, true) {
		return nil
	}
	// Drop the fixed-file registration now, even if teardown is deferred.
	// It pins the ublk char device open; io_uring otherwise releases fixed
	// files only in its asynchronous exit work, which can stall long enough
	// to block DEL_DEV on the device refcount.
	if r.core.filesRegistered {
		_ = r.core.UnregisterFiles()
	}
	r.closed.Store(true)
	if r.active.Load() == 0 {
		r.teardown.Do(r.closeCore)
		return r.closeErr
	}
	return nil
}

// SubmitCtrlCmd submits one ublk control command and waits for its CQE; it
// is SubmitCtrlCmdContext without a context.
func (r *minimalRing) SubmitCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (Result, error) {
	return r.SubmitCtrlCmdContext(context.Background(), cmd, ctrlCmd, userData)
}

// SubmitCtrlCmdContext submits one ublk control command and waits for its CQE.
//
// Buffer contract: ctrlCmd.Addr and ctrlCmd.Len describe the command's data
// buffer (input, output or both; Len includes any dev_path prefix). ublk
// runs most control commands on an io-wq worker that reads and writes that
// buffer asynchronously, so the kernel is never handed the caller's memory
// (Critical Bug #21): the Len bytes at Addr are copied into ring-owned
// off-heap memory, the SQE points at the copy, and it is copied back to Addr
// once the CQE has been reaped, whatever its result. The caller's buffer must
// be heap or off-heap memory, not a stack variable (its address travels as an
// integer, which the runtime does not update when it moves a stack), and must
// stay reachable (runtime.KeepAlive) until the call returns. Addr 0 or Len 0
// means no buffer. *ctrlCmd itself is copied into the SQE before submission
// and may live anywhere.
//
// Cancelling ctx cancels the command: an IORING_OP_ASYNC_CANCEL interrupts it
// (a command blocked on an io-wq worker gets a signal, so interruptible waits
// such as END_USER_RECOVERY's end with -EINTR), and the call keeps waiting up
// to a second for its CQE. If the CQE is reaped, the command is finished and
// its buffer copied back: the call returns the Result, whose Value says what
// the kernel did (the command may have completed before the cancel landed),
// together with an error wrapping ErrCtrlCanceled and ctx.Err(). A ctx that is
// already done returns ctx.Err() without submitting.
//
// The wait is also bounded by Config.CtrlTimeout: 10s by default (Critical
// Bug #8), unbounded if negative; the deadline does not cancel. To bound a
// command and still reap it, use a ctx deadline with CtrlTimeout < 0. If the
// call stops waiting before the CQE is reaped (deadline, Close, a failed wait,
// or a cancelled command that ignores the cancel) the error wraps
// ErrCtrlTimeout: the command may still execute. Its staging buffer is then
// abandoned, never reused or unmapped, so a late kernel write lands nowhere
// live, and its eventual CQE is recognised by an internal user_data tag and
// discarded by the next command. Errors before submission wrap neither.
//
// Not safe for concurrent use: one command at a time per ring.
// Result.UserData returns userData.
func (r *minimalRing) SubmitCtrlCmdContext(ctx context.Context, cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd,
	userData uint64) (Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !r.acquire() {
		return nil, ErrRingClosed
	}
	defer r.release()
	// An 8-aligned copy of the 32-byte command, so its u64 addr can be read
	// and rewritten in place.
	var words [4]uint64
	payload := unsafe.Slice((*byte)(unsafe.Pointer(&words)), len(words)*8)
	copy(payload, (*[32]byte)(unsafe.Pointer(ctrlCmd))[:])
	length := int(*(*uint16)(unsafe.Pointer(&payload[ctrlCmdLenOffset])))
	addr := &words[ctrlCmdAddrOffset/8]
	var user []byte
	if *addr != 0 && length != 0 {
		if r.ctrlScratch == nil {
			scratch, err := AllocOffHeap(ctrlScratchSize)
			if err != nil {
				return nil, fmt.Errorf("control command staging buffer: %w", err)
			}
			r.ctrlScratch = scratch
		}
		// The caller's address arrives as an integer; reinterpret it rather
		// than convert, which go vet rightly flags in general.
		user = unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(addr))), length)
		copy(r.ctrlScratch, user)
		*addr = uint64(uintptr(unsafe.Pointer(&r.ctrlScratch[0])))
	}
	r.ctrlSeq++
	tag := ctrlTagBase | r.ctrlSeq&ctrlTagSeqMask
	sqe := r.core.GetSQE128()
	if sqe == nil {
		return nil, fmt.Errorf("submit control command %#x: %w", cmd, ErrRingFull)
	}
	PrepUringCmd128(sqe, int32(r.targetFd), cmd, payload)
	sqe.Len = uint32(length) // ignored by the kernel; kept from earlier versions
	sqe.UserData = tag
	if _, err := r.core.Submit(); err != nil {
		r.core.withdrawUnsubmitted()
		return nil, fmt.Errorf("io_uring_enter submit failed: %w", err)
	}
	res, reaped, err := r.waitCtrlCompletion(ctx, tag)
	if !reaped {
		if user != nil {
			r.ctrlScratch = nil // abandoned to the in-flight command; leaked on purpose
		}
		return nil, err
	}
	if user != nil {
		copy(user, r.ctrlScratch[:length])
	}
	result := &minimalResult{userData: userData, value: res}
	if res < 0 {
		result.err = fmt.Errorf("operation failed with result: %d", res)
	}
	return result, err
}

// waitCtrlCompletion waits for the CQE tagged tag. reaped reports whether it
// was consumed; err is non-nil if the wait was cut short (ErrCtrlTimeout) or
// the command cancelled (ErrCtrlCanceled, with reaped true). It re-waits
// across EINTR and empty bounded wakeups: UBLK_F_URING_CMD_COMP_IN_TASK defers
// control completions to task work, so one io_uring_enter can return before
// the CQE is posted.
func (r *minimalRing) waitCtrlCompletion(ctx context.Context, tag uint64) (int32, bool, error) {
	var deadline time.Time
	if r.ctrlTimeout > 0 {
		deadline = time.Now().Add(r.ctrlTimeout)
	}
	for {
		if res, ok := r.reapCtrl(tag); ok {
			return res, true, nil
		}
		if cause := ctx.Err(); cause != nil {
			return r.cancelCtrl(tag, cause)
		}
		if r.closing.Load() {
			return 0, false, fmt.Errorf("ring closed while waiting for control command completion: %w", ErrCtrlTimeout)
		}
		wait := ctrlWaitSlice
		if !deadline.IsZero() {
			left := time.Until(deadline)
			if left <= 0 {
				return 0, false, fmt.Errorf("no control command completion after %s: %w", r.ctrlTimeout, ErrCtrlTimeout)
			}
			wait = min(wait, left)
		}
		if err := r.core.WaitCQEs(1, wait); err != nil && err != unix.ETIME {
			return 0, false, fmt.Errorf("io_uring_enter wait failed: %w (%w)", err, ErrCtrlTimeout)
		}
	}
}

// reapCtrl consumes ready CQEs until it finds the one tagged tag. Others
// are stragglers of abandoned commands or the CQEs of cancel requests.
func (r *minimalRing) reapCtrl(tag uint64) (int32, bool) {
	for cqe := r.core.PeekCQE(); cqe != nil; cqe = r.core.PeekCQE() {
		userData, res := cqe.UserData, cqe.Res
		r.core.CQESeen()
		if userData == tag {
			return res, true
		}
		if userData&^ctrlTagSeqMask != ctrlCancelTagBase {
			logging.Default().Warn("discarded stale control completion", "user_data", userData, "res", res)
		}
	}
	return 0, false
}

// cancelCtrl asks the kernel to cancel the command tagged tag and waits up to
// ctrlCancelGrace for its CQE. The cancel's own CQE (0, -ENOENT if the
// command already finished, -EALREADY if it was running and got signalled)
// is discarded: only the command's CQE says how it ended.
func (r *minimalRing) cancelCtrl(tag uint64, cause error) (int32, bool, error) {
	sqe := r.core.GetSQE128()
	if sqe == nil {
		return 0, false, fmt.Errorf("no SQE to cancel the control command (%w): %w", cause, ErrCtrlTimeout)
	}
	PrepCancel(&sqe.SQE, tag, 0)
	sqe.UserData = ctrlCancelTagBase | tag&ctrlTagSeqMask
	if _, err := r.core.Submit(); err != nil {
		r.core.withdrawUnsubmitted()
		return 0, false, fmt.Errorf("submit control command cancel: %w (%w, %w)", err, cause, ErrCtrlTimeout)
	}
	deadline := time.Now().Add(r.ctrlCancelGrace)
	for {
		if res, ok := r.reapCtrl(tag); ok {
			return res, true, fmt.Errorf("control command cancelled (%w): %w", cause, ErrCtrlCanceled)
		}
		left := time.Until(deadline)
		if left <= 0 {
			return 0, false, fmt.Errorf("control command still running %s after cancel (%w): %w",
				r.ctrlCancelGrace, cause, ErrCtrlTimeout)
		}
		if err := r.core.WaitCQEs(1, min(ctrlWaitSlice, left)); err != nil && err != unix.ETIME {
			return 0, false, fmt.Errorf("io_uring_enter wait failed: %w (%w, %w)", err, cause, ErrCtrlTimeout)
		}
	}
}

// SubmitCtrlCmdAsync submits a control command without waiting; reap it with
// AsyncHandle.Wait or WaitForCompletion. Unlike SubmitCtrlCmd it hands the
// kernel ctrlCmd.Addr directly, so that buffer must stay valid, unmoved and
// reachable until the completion is reaped.
func (r *minimalRing) SubmitCtrlCmdAsync(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (*AsyncHandle, error) {
	if !r.acquire() {
		return nil, ErrRingClosed
	}
	defer r.release()
	logging.Default().Debug("submitting async ctrl command",
		"cmd_hex", fmt.Sprintf("0x%08x", cmd), "dev_id", ctrlCmd.DevID)
	sqe := r.core.GetSQE128()
	if sqe == nil {
		return nil, fmt.Errorf("submit async control command %#x: %w", cmd, ErrRingFull)
	}
	PrepUringCmd128(sqe, int32(r.targetFd), cmd, (*[32]byte)(unsafe.Pointer(ctrlCmd))[:])
	sqe.Len = uint32(ctrlCmd.Len)
	sqe.UserData = userData
	submitted, err := r.core.Submit()
	if err != nil || submitted != 1 {
		r.core.withdrawUnsubmitted()
		return nil, fmt.Errorf("failed to submit: %w", err)
	}
	return &AsyncHandle{userData: userData, ring: r}, nil
}

// tryGetCompletion looks for the CQE carrying userData without blocking. If
// found it is consumed along with every CQE ahead of it; otherwise the CQ is
// left alone.
func (r *minimalRing) tryGetCompletion(userData uint64) (Result, error) {
	if !r.acquire() {
		return nil, ErrRingClosed
	}
	defer r.release()
	_ = r.core.GetEvents()
	core := r.core
	tail := atomic.LoadUint32(core.cqTail)
	if core.cqHeadLocal == tail {
		return nil, fmt.Errorf("no completions available")
	}
	for head := core.cqHeadLocal; head != tail; head++ {
		cqe := core.cqeAt(head)
		if cqe.UserData != userData {
			continue
		}
		result := &minimalResult{userData: cqe.UserData, value: cqe.Res}
		if cqe.Res < 0 {
			result.err = fmt.Errorf("operation failed with result: %d", cqe.Res)
		}
		core.CQAdvance(head + 1 - core.cqHeadLocal)
		return result, nil
	}
	return nil, fmt.Errorf("completion not found")
}

// minimalResult implements the Result interface
type minimalResult struct {
	userData uint64
	value    int32
	err      error
}

func (r *minimalResult) UserData() uint64 { return r.userData }
func (r *minimalResult) Value() int32     { return r.value }
func (r *minimalResult) Error() error     { return r.err }

// PrepareIOCmd prepares an I/O command SQE without submitting to the kernel.
// Call FlushSubmissions() to submit all prepared commands in a single syscall.
func (r *minimalRing) PrepareIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error {
	if !r.acquire() {
		return ErrRingClosed
	}
	defer r.release()
	sqe := r.core.GetSQE128()
	if sqe == nil {
		return fmt.Errorf("failed to prepare I/O command: %w", ErrRingFull)
	}
	PrepUringCmd128(sqe, int32(r.targetFd), cmd, (*[16]byte)(unsafe.Pointer(ioCmd))[:])
	sqe.Len = 16 // 16-byte ublksrv_io_cmd; ignored by the kernel
	sqe.UserData = userData
	return nil
}

// FlushSubmissions submits all prepared SQEs with a single io_uring_enter syscall.
// Returns the number of SQEs submitted.
func (r *minimalRing) FlushSubmissions() (uint32, error) {
	if !r.acquire() {
		return 0, ErrRingClosed
	}
	defer r.release()
	submitted, err := r.core.Submit()
	if err != nil {
		return 0, fmt.Errorf("io_uring_enter failed: %w", err)
	}
	return uint32(submitted), nil
}

// SubmitIOCmd submits an I/O command and returns the result.
// This is a convenience method that calls PrepareIOCmd + FlushSubmissions.
// For batching multiple commands, use PrepareIOCmd repeatedly then FlushSubmissions once.
func (r *minimalRing) SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) (Result, error) {
	if err := r.PrepareIOCmd(cmd, ioCmd, userData); err != nil {
		return nil, err
	}
	if _, err := r.FlushSubmissions(); err != nil {
		return nil, err
	}
	return &minimalResult{userData: userData, value: 0, err: nil}, nil
}

// WaitForCompletion returns the completions that are ready. With timeout > 0
// it does not block; with timeout 0 it blocks for at least one, but only for
// up to 100ms per call so the caller's ioLoop can observe context
// cancellation and exit even when no I/O and no STOP_DEV ever wake it
// (tearing down a device that was primed but never started). The results
// are pooled and overwritten by the next call.
func (r *minimalRing) WaitForCompletion(timeout int) ([]Result, error) {
	if !r.acquire() {
		return nil, ErrRingClosed
	}
	defer r.release()
	r.resultsPool = r.resultsPool[:0]
	if r.drain(); len(r.resultsPool) > 0 {
		return r.resultsPool, nil
	}
	if timeout > 0 {
		_ = r.core.GetEvents()
		r.drain()
		return r.resultsPool, nil
	}
	if err := r.core.WaitCQEs(1, ctrlWaitSlice); err != nil && err != unix.ETIME {
		return nil, fmt.Errorf("io_uring_enter wait failed: %w", err)
	}
	r.drain()
	return r.resultsPool, nil
}

// drain moves ready CQEs into the result pool, at most one pool's worth.
func (r *minimalRing) drain() {
	for len(r.resultsPool) < len(r.cqePool) {
		cqe := r.core.PeekCQE()
		if cqe == nil {
			return
		}
		res := &r.cqePool[len(r.resultsPool)]
		*res = minimalResult{userData: cqe.UserData, value: cqe.Res}
		r.resultsPool = append(r.resultsPool, res)
		r.core.CQESeen()
	}
}

func (r *minimalRing) NewBatch() Batch {
	return &minimalBatch{}
}

// Minimal batch implementation
type minimalBatch struct{}

func (b *minimalBatch) AddCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) error {
	return fmt.Errorf("batch not implemented in minimal ring")
}

func (b *minimalBatch) AddIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error {
	return fmt.Errorf("batch not implemented in minimal ring")
}

func (b *minimalBatch) Submit() ([]Result, error) {
	return nil, fmt.Errorf("batch not implemented in minimal ring")
}

func (b *minimalBatch) Len() int {
	return 0
}
