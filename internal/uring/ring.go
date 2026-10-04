package uring

import (
	"fmt"
	"math/bits"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Memory ordering
//
// The SQ and CQ rings are shared with the kernel. Go's sync/atomic operations
// are sequentially consistent: on arm64 a Store is STLR (store-release) and a
// Load is LDAR (load-acquire); on amd64 a Store is XCHG (a full barrier) and a
// Load is a plain MOV, which x86-TSO already orders. The compiler never moves
// ordinary memory accesses across an atomic. That is exactly what the ring
// protocol needs, so no separate fences are issued (the old Sfence/Mfence
// helpers, an atomic add on an unrelated variable, added nothing on top):
//
//   - SQEs are written with plain stores, then the SQ tail is published with
//     atomic.StoreUint32 (release); the kernel reads it with smp_load_acquire.
//   - The SQ head is read with atomic.LoadUint32 (acquire) before an SQE slot
//     is reused; the kernel stores it with smp_store_release after reading.
//   - The CQ tail is read with atomic.LoadUint32 (acquire) before any CQE is
//     read; the kernel stores it with smp_store_release after writing them.
//   - The CQ head is stored with atomic.StoreUint32 (release) after the CQEs
//     are read, so the kernel cannot overwrite a slot still being read.
//
// Every shared index is accessed only through those atomics; a plain read of
// a kernel-written index is a data race the compiler may hoist or tear.

// Pinning
//
// Memory whose address goes into an SQE is read or written by the kernel
// asynchronously, after the Go code that queued it has moved on. Such memory
// must not live on a goroutine stack (the runtime moves stacks and does not
// update integers holding their addresses) and must stay reachable until the
// request's CQE has been reaped. Use one of:
//
//   - off-heap memory from AllocOffHeap (never moved, never collected);
//   - Go heap memory kept referenced until completion (Go's GC does not move
//     heap objects); the []byte prep helpers force their buffer to the heap
//     via AddrOf, but the caller must still keep it reachable;
//   - runtime.Pinner over a heap object.
//
// Memory read only during submission (iovec arrays, timeout timespecs) needs
// to stay valid only until Submit returns (IORING_FEAT_SUBMIT_STABLE, 5.5).
// The ring's own wait arguments live inside the IoUring heap object.

// supportedSetupFlags are the IORING_SETUP_* flags IoUring implements. The
// rest change the ring protocol (SQPOLL, IOPOLL, NO_SQARRAY, NO_MMAP, ...).
const supportedSetupFlags = IORING_SETUP_CQSIZE | IORING_SETUP_CLAMP | IORING_SETUP_R_DISABLED |
	IORING_SETUP_SUBMIT_ALL | IORING_SETUP_COOP_TASKRUN | IORING_SETUP_TASKRUN_FLAG |
	IORING_SETUP_SQE128 | IORING_SETUP_CQE32 | IORING_SETUP_SINGLE_ISSUER | IORING_SETUP_DEFER_TASKRUN

// timeoutUserData tags the internal IORING_OP_TIMEOUT used by waits on
// kernels without IORING_FEAT_EXT_ARG (< 5.11); PeekCQE hides those CQEs.
const timeoutUserData = ^uint64(0)

// SetupOptions configures NewIoUring.
type SetupOptions struct {
	// Entries is the SQ size; the kernel rounds it up to a power of two.
	Entries uint32
	// CQEntries sets the CQ size (IORING_SETUP_CQSIZE); 0 keeps the kernel
	// default of twice the SQ size.
	CQEntries uint32
	// Flags are required IORING_SETUP_* flags: NewIoUring fails if the kernel
	// rejects them.
	Flags uint32
	// OptionalFlags are IORING_SETUP_* flags to use if the kernel accepts
	// them. On EINVAL they are dropped one at a time, newest first, and setup
	// retried; Flags reports what was accepted.
	OptionalFlags uint32
}

// IoUring is a general-purpose io_uring instance: it owns the ring fd, the
// mapped SQ/CQ/SQE regions and whatever it registered, and exposes an
// allocation-free SQE/CQE API.
//
// An IoUring is not safe for concurrent use: one goroutine at a time submits,
// reaps, registers and closes. With IORING_SETUP_SINGLE_ISSUER the kernel
// further requires every io_uring_enter and io_uring_register to come from the
// thread that created the ring (or that called EnableRings on an
// IORING_SETUP_R_DISABLED ring) and fails others with EEXIST, so the owning
// goroutine must runtime.LockOSThread before NewIoUring and stay locked.
//
// With IORING_SETUP_DEFER_TASKRUN (6.1, requires SINGLE_ISSUER) the kernel
// posts asynchronous completions to the CQ only when the owning thread enters
// io_uring_enter with GETEVENTS (SubmitAndWait, WaitCQEs, WaitCQE, GetEvents).
// Until then PeekCQE sees nothing, no other thread can reap them, and nothing
// progresses while the owner is busy outside the ring. Completions produced
// inline during submission are visible right away. Add
// IORING_SETUP_TASKRUN_FLAG so PeekCQE can see pending work and flush it.
type IoUring struct {
	fd         int
	flags      uint32 // setup flags the kernel accepted
	features   uint32 // IORING_FEAT_* reported by the kernel
	sqeShift   uint32 // log2(SQE size / 64)
	cqeShift   uint32 // log2(CQE size / 16)
	extArg     bool   // IORING_FEAT_EXT_ARG: waits take a timeout argument
	singleMmap bool   // IORING_FEAT_SINGLE_MMAP: SQ and CQ rings share a mapping
	closed     bool
	// Submission queue. sqHead and sqFlags are written by the kernel, sqTail
	// by us; sqeTail counts SQEs handed out and is published by Submit.
	sqHead    *uint32
	sqTail    *uint32
	sqFlags   *uint32
	sqMask    uint32
	sqEntries uint32
	sqes      unsafe.Pointer
	sqeTail   uint32
	// Completion queue. cqTail and cqOverflow are written by the kernel, cqHead
	// by us; cqHeadLocal mirrors it.
	cqHead      *uint32
	cqTail      *uint32
	cqOverflow  *uint32
	cqMask      uint32
	cqEntries   uint32
	cqes        unsafe.Pointer
	cqHeadLocal uint32
	// Mappings unmapped by Close.
	sqRing  []byte
	cqRing  []byte
	sqeRing []byte
	// Wait arguments handed to the kernel; heap fields, so their addresses
	// are stable for the duration of the syscall.
	waitArg      getEventsArg
	waitTs       Timespec
	timeoutTs    Timespec
	fallbackUsed bool // an internal timeout SQE was queued (no EXT_ARG)
	// Registrations Close releases.
	filesRegistered   bool
	buffersRegistered bool
	bufRings          []*BufRing
}

// NewIoUring creates an io_uring instance and maps its rings.
func NewIoUring(opts SetupOptions) (*IoUring, error) {
	if bad := (opts.Flags | opts.OptionalFlags) &^ supportedSetupFlags; bad != 0 {
		return nil, fmt.Errorf("unsupported io_uring setup flags %#x", bad)
	}
	required := opts.Flags
	if opts.CQEntries != 0 {
		required |= IORING_SETUP_CQSIZE
	}
	var p ioUringParams
	fd, flags, err := setup(opts.Entries, opts.CQEntries, required, opts.OptionalFlags&^required, &p)
	if err != nil {
		return nil, err
	}
	r := &IoUring{
		fd:         fd,
		flags:      flags,
		features:   p.features,
		extArg:     p.features&IORING_FEAT_EXT_ARG != 0,
		singleMmap: p.features&IORING_FEAT_SINGLE_MMAP != 0,
	}
	if err := r.mapRings(&p); err != nil {
		_ = r.unmapRings()
		_ = unix.Close(fd)
		return nil, err
	}
	return r, nil
}

// setup calls io_uring_setup, dropping optional flags on EINVAL. Setup flag
// bits are allocated in kernel-release order, so the highest optional bit is
// the newest feature and the most likely cause; dropping it first also sheds
// DEFER_TASKRUN before the SINGLE_ISSUER it depends on.
func setup(entries, cqEntries, required, optional uint32, p *ioUringParams) (int, uint32, error) {
	flags := required | optional
	for {
		*p = ioUringParams{flags: flags, cqEntries: cqEntries}
		fd, _, errno := unix.Syscall(unix.SYS_IO_URING_SETUP, uintptr(entries), uintptr(unsafe.Pointer(p)), 0)
		if errno == 0 {
			return int(fd), flags, nil
		}
		remaining := flags & optional
		if errno != unix.EINVAL || remaining == 0 {
			return -1, 0, fmt.Errorf("io_uring_setup entries=%d flags=%#x: %w", entries, flags, errno)
		}
		flags &^= 1 << (31 - bits.LeadingZeros32(remaining))
	}
}

func (r *IoUring) mapRings(p *ioUringParams) error {
	if r.flags&IORING_SETUP_SQE128 != 0 {
		r.sqeShift = 1
	}
	if r.flags&IORING_SETUP_CQE32 != 0 {
		r.cqeShift = 1
	}
	sqSize := int(p.sqOff.array + p.sqEntries*4)
	cqSize := int(p.cqOff.cqes + p.cqEntries<<(4+r.cqeShift))
	if r.singleMmap {
		sqSize = max(sqSize, cqSize)
	}
	var err error
	if r.sqRing, err = mmapShared(r.fd, IORING_OFF_SQ_RING, sqSize); err != nil {
		return fmt.Errorf("mmap SQ ring: %w", err)
	}
	if r.singleMmap {
		r.cqRing = r.sqRing
	} else if r.cqRing, err = mmapShared(r.fd, IORING_OFF_CQ_RING, cqSize); err != nil {
		return fmt.Errorf("mmap CQ ring: %w", err)
	}
	if r.sqeRing, err = mmapShared(r.fd, IORING_OFF_SQES, int(p.sqEntries)<<(6+r.sqeShift)); err != nil {
		return fmt.Errorf("mmap SQEs: %w", err)
	}
	sq := unsafe.Pointer(&r.sqRing[0])
	r.sqHead = (*uint32)(unsafe.Add(sq, p.sqOff.head))
	r.sqTail = (*uint32)(unsafe.Add(sq, p.sqOff.tail))
	r.sqFlags = (*uint32)(unsafe.Add(sq, p.sqOff.flags))
	r.sqMask = *(*uint32)(unsafe.Add(sq, p.sqOff.ringMask))
	r.sqEntries = p.sqEntries
	r.sqes = unsafe.Pointer(&r.sqeRing[0])
	cq := unsafe.Pointer(&r.cqRing[0])
	r.cqHead = (*uint32)(unsafe.Add(cq, p.cqOff.head))
	r.cqTail = (*uint32)(unsafe.Add(cq, p.cqOff.tail))
	r.cqOverflow = (*uint32)(unsafe.Add(cq, p.cqOff.overflow))
	r.cqMask = *(*uint32)(unsafe.Add(cq, p.cqOff.ringMask))
	r.cqEntries = p.cqEntries
	r.cqes = unsafe.Add(cq, p.cqOff.cqes)
	// Map SQ slot i to SQE i once, as liburing does; the array is never
	// written again, so a submission costs no indirection store.
	array := unsafe.Slice((*uint32)(unsafe.Add(sq, p.sqOff.array)), p.sqEntries)
	for i := range array {
		array[i] = uint32(i)
	}
	r.sqeTail = atomic.LoadUint32(r.sqTail)
	r.cqHeadLocal = atomic.LoadUint32(r.cqHead)
	return nil
}

func mmapShared(fd int, offset int64, size int) ([]byte, error) {
	return unix.Mmap(fd, offset, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_POPULATE)
}

// unmapRings unmaps whatever mapRings mapped and drops every pointer into
// those mappings, so no stale pointer outlives them.
func (r *IoUring) unmapRings() error {
	var err error
	unmap := func(b []byte) {
		if b != nil {
			if e := unix.Munmap(b); e != nil && err == nil {
				err = e
			}
		}
	}
	unmap(r.sqeRing)
	if !r.singleMmap {
		unmap(r.cqRing)
	}
	unmap(r.sqRing)
	r.sqRing, r.cqRing, r.sqeRing = nil, nil, nil
	r.sqHead, r.sqTail, r.sqFlags, r.sqes = nil, nil, nil, nil
	r.cqHead, r.cqTail, r.cqOverflow, r.cqes = nil, nil, nil, nil
	return err
}

// Close releases the ring. It is idempotent. It unregisters the provided
// buffer rings, buffers and files this IoUring registered, unmaps every
// mapping and closes the fd. Unregistration is best effort: on a
// SINGLE_ISSUER ring called from a thread other than the submitter it fails
// with EEXIST and the kernel releases them when ring teardown finishes
// (asynchronously, after the fd is closed).
//
// Close does not wait for in-flight requests: the kernel cancels them during
// teardown, so memory they reference must stay valid until then. Close must
// not run concurrently with any other method, and the IoUring must not be
// used afterwards.
func (r *IoUring) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	for _, br := range r.bufRings {
		_ = br.unregister()
		_ = br.free()
	}
	r.bufRings = nil
	if r.buffersRegistered {
		_ = r.UnregisterBuffers()
	}
	if r.filesRegistered {
		_ = r.UnregisterFiles()
	}
	err := r.unmapRings()
	if cerr := unix.Close(r.fd); cerr != nil && err == nil {
		err = cerr
	}
	r.fd = -1
	if err != nil {
		return fmt.Errorf("close io_uring: %w", err)
	}
	return nil
}

// Fd returns the io_uring file descriptor.
func (r *IoUring) Fd() int { return r.fd }

// Flags returns the IORING_SETUP_* flags the kernel accepted.
func (r *IoUring) Flags() uint32 { return r.flags }

// Features returns the kernel's IORING_FEAT_* bits.
func (r *IoUring) Features() uint32 { return r.features }

// SQEntries returns the SQ size.
func (r *IoUring) SQEntries() uint32 { return r.sqEntries }

// CQEntries returns the CQ size.
func (r *IoUring) CQEntries() uint32 { return r.cqEntries }

// SQFlags returns the kernel's IORING_SQ_* ring flags.
func (r *IoUring) SQFlags() uint32 { return atomic.LoadUint32(r.sqFlags) }

// CQOverflow returns the kernel's count of completions it had to drop. With
// IORING_FEAT_NODROP (5.5) a full CQ parks completions in an overflow list
// instead (IORING_SQ_CQ_OVERFLOW), so this only counts allocation failures,
// which io_uring_enter also reports once as EBADR.
func (r *IoUring) CQOverflow() uint32 { return atomic.LoadUint32(r.cqOverflow) }

// GetSQE returns the next free SQE, zeroed, or nil if the SQ is full. The
// SQE becomes visible to the kernel at the next Submit or SubmitAndWait.
func (r *IoUring) GetSQE() *SQE {
	if r.sqeTail-atomic.LoadUint32(r.sqHead) >= r.sqEntries {
		return nil
	}
	p := unsafe.Add(r.sqes, uintptr(r.sqeTail&r.sqMask)<<(6+r.sqeShift))
	r.sqeTail++
	if r.sqeShift == 0 {
		*(*SQE)(p) = SQE{}
	} else {
		*(*SQE128)(p) = SQE128{}
	}
	return (*SQE)(p)
}

// GetSQE128 is GetSQE for an IORING_SETUP_SQE128 ring; it returns nil if the
// SQ is full or the ring uses 64-byte SQEs.
func (r *IoUring) GetSQE128() *SQE128 {
	if r.sqeShift == 0 {
		return nil
	}
	return (*SQE128)(unsafe.Pointer(r.GetSQE()))
}

// SQReady returns how many SQEs were handed out but not yet published.
func (r *IoUring) SQReady() uint32 { return r.sqeTail - atomic.LoadUint32(r.sqTail) }

// SQSpaceLeft returns how many SQEs GetSQE can still hand out.
func (r *IoUring) SQSpaceLeft() uint32 {
	return r.sqEntries - (r.sqeTail - atomic.LoadUint32(r.sqHead))
}

// flushSQ publishes every SQE handed out so far and returns how many the
// kernel has not consumed, including any a previous failed enter left behind.
func (r *IoUring) flushSQ() uint32 {
	atomic.StoreUint32(r.sqTail, r.sqeTail)
	return r.sqeTail - atomic.LoadUint32(r.sqHead)
}

// withdrawUnsubmitted drops queued SQEs the kernel has not consumed, after
// a failed enter. Without SQPOLL the kernel reads SQEs only inside
// io_uring_enter, which only the owner calls, so none can be in use.
func (r *IoUring) withdrawUnsubmitted() {
	r.sqeTail = atomic.LoadUint32(r.sqHead)
	atomic.StoreUint32(r.sqTail, r.sqeTail)
}

func (r *IoUring) enter(toSubmit, minComplete, flags uint32, arg unsafe.Pointer, argSize uintptr) (int, unix.Errno) {
	n, _, errno := unix.Syscall6(unix.SYS_IO_URING_ENTER, uintptr(r.fd), uintptr(toSubmit),
		uintptr(minComplete), uintptr(flags), uintptr(arg), argSize)
	return int(n), errno
}

// Submit publishes the pending SQEs and submits every SQE the kernel has not
// consumed, without waiting. It returns how many the kernel consumed; fewer
// than pending means it stopped at an SQE that failed to prep (that SQE's
// error is in its CQE) and the rest stay queued for the next submit. Errors
// are raw errnos: EAGAIN (no request memory; reap and retry), EBUSY (older
// kernels, CQ overflow backlog; reap and retry), EEXIST (SINGLE_ISSUER ring,
// wrong thread). EINTR is retried.
func (r *IoUring) Submit() (int, error) {
	for {
		toSubmit := r.flushSQ()
		if toSubmit == 0 {
			return 0, nil
		}
		n, errno := r.enter(toSubmit, 0, 0, nil, 0)
		if errno == 0 {
			return n, nil
		}
		if errno != unix.EINTR {
			return 0, errno
		}
	}
}

// SubmitAndWait submits like Submit and waits until at least minComplete
// CQEs are ready, normally in a single io_uring_enter. A positive timeout
// bounds the wait (IORING_ENTER_EXT_ARG; an internal IORING_OP_TIMEOUT on
// kernels without it) and expiry returns unix.ETIME with whatever CQEs did
// arrive still in the CQ; timeout <= 0 waits without a deadline.
//
// The kernel swallows a wait error once it has submitted something, so a
// signal (including Go's async-preemption SIGURG) can end the syscall early
// without reporting EINTR; SubmitAndWait re-waits for the remainder of the
// timeout in that case. It returns early without error if the kernel stopped
// at an SQE that failed to prep, whose error CQE is then in the CQ. Other
// errors are raw errnos as for Submit, plus EBADR (CQEs were dropped).
func (r *IoUring) SubmitAndWait(minComplete uint32, timeout time.Duration) (int, error) {
	return r.submitAndWait(true, minComplete, timeout)
}

// WaitCQEs waits like SubmitAndWait but submits nothing (except, on kernels
// without IORING_FEAT_EXT_ARG, the internal timeout and so everything queued
// before it).
func (r *IoUring) WaitCQEs(minComplete uint32, timeout time.Duration) error {
	_, err := r.submitAndWait(false, minComplete, timeout)
	return err
}

func (r *IoUring) submitAndWait(submit bool, minComplete uint32, timeout time.Duration) (int, error) {
	minComplete = min(minComplete, r.cqEntries)
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	submitted := 0
	for {
		var toSubmit, extraFlags uint32
		var arg unsafe.Pointer
		var argSize uintptr
		armed := false
		if submit {
			toSubmit = r.flushSQ()
		}
		if timeout > 0 {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return submitted, r.expired(minComplete)
			}
			var err error
			if extraFlags, arg, argSize, armed, err = r.boundWait(remaining, minComplete); err != nil {
				return submitted, err
			}
			if armed {
				toSubmit = r.flushSQ()
			}
		}
		n, errno := r.enter(toSubmit, minComplete, IORING_ENTER_GETEVENTS|extraFlags, arg, argSize)
		if n > 0 {
			submitted += n
			if armed && uint32(n) == toSubmit {
				submitted-- // the internal timeout is not the caller's
			}
		}
		switch errno {
		case 0:
		case unix.EINTR:
			continue
		case unix.ETIME:
			return submitted, r.expired(minComplete)
		default:
			return submitted, errno
		}
		if uint32(n) < toSubmit || r.CQReady() >= minComplete {
			return submitted, nil
		}
	}
}

// expired is the outcome of a wait whose deadline passed: success if enough
// CQEs arrived anyway, else ETIME.
func (r *IoUring) expired(minComplete uint32) error {
	if r.CQReady() >= minComplete {
		return nil
	}
	return unix.ETIME
}

// boundWait bounds one wait to d: with IORING_FEAT_EXT_ARG it returns the
// enter flag and timeout argument (heap fields of r, stable during the
// syscall); otherwise it queues an internal timeout SQE and reports armed.
func (r *IoUring) boundWait(d time.Duration, minComplete uint32) (uint32, unsafe.Pointer, uintptr, bool, error) {
	if r.extArg {
		r.waitTs = durationToTimespec(d)
		r.waitArg = getEventsArg{ts: uint64(uintptr(unsafe.Pointer(&r.waitTs)))}
		return IORING_ENTER_EXT_ARG, unsafe.Pointer(&r.waitArg), unsafe.Sizeof(r.waitArg), false, nil
	}
	if !r.armTimeout(d, minComplete) {
		return 0, nil, 0, false, fmt.Errorf("no free SQE for the wait timeout: %w", ErrRingFull)
	}
	return 0, nil, 0, true, nil
}

// armTimeout queues the internal IORING_OP_TIMEOUT that bounds a wait on
// kernels without IORING_FEAT_EXT_ARG. It fires after d, or once count other
// completions have posted; either way its CQE is hidden from the caller.
func (r *IoUring) armTimeout(d time.Duration, count uint32) bool {
	sqe := r.GetSQE()
	if sqe == nil {
		return false
	}
	r.timeoutTs = durationToTimespec(d)
	sqe.Opcode = IORING_OP_TIMEOUT
	sqe.Fd = -1
	sqe.Addr = uint64(uintptr(unsafe.Pointer(&r.timeoutTs)))
	sqe.Len = 1
	sqe.Off = uint64(count)
	sqe.UserData = timeoutUserData
	r.fallbackUsed = true
	return true
}

func durationToTimespec(d time.Duration) Timespec {
	return Timespec{Sec: int64(d / time.Second), Nsec: int64(d % time.Second)}
}

// GetEvents enters the kernel with GETEVENTS without blocking: it runs
// pending task work (required to see completions on a DEFER_TASKRUN ring)
// and flushes the CQ overflow list into the CQ.
func (r *IoUring) GetEvents() error {
	for {
		_, errno := r.enter(0, 0, IORING_ENTER_GETEVENTS, nil, 0)
		if errno == 0 {
			return nil
		}
		if errno != unix.EINTR {
			return errno
		}
	}
}

func (r *IoUring) cqeAt(head uint32) *CQE {
	return (*CQE)(unsafe.Add(r.cqes, uintptr(head&r.cqMask)<<(4+r.cqeShift)))
}

// CQReady returns how many CQEs PeekCQE can return without entering the kernel.
func (r *IoUring) CQReady() uint32 {
	tail := atomic.LoadUint32(r.cqTail)
	if !r.fallbackUsed {
		return tail - r.cqHeadLocal
	}
	n := uint32(0)
	for head := r.cqHeadLocal; head != tail; head++ {
		if r.cqeAt(head).UserData != timeoutUserData {
			n++
		}
	}
	return n
}

// PeekCQE returns the next CQE without consuming it, or nil if none is ready.
// The CQE points into the ring and stays valid until CQESeen or CQAdvance.
// If the CQ is empty but the kernel flags pending work (IORING_SQ_CQ_OVERFLOW,
// or IORING_SQ_TASKRUN with IORING_SETUP_TASKRUN_FLAG), it calls GetEvents
// once to pull those completions in.
func (r *IoUring) PeekCQE() *CQE {
	for {
		if r.cqHeadLocal == atomic.LoadUint32(r.cqTail) {
			if atomic.LoadUint32(r.sqFlags)&(IORING_SQ_CQ_OVERFLOW|IORING_SQ_TASKRUN) == 0 {
				return nil
			}
			_ = r.GetEvents()
			if r.cqHeadLocal == atomic.LoadUint32(r.cqTail) {
				return nil
			}
		}
		cqe := r.cqeAt(r.cqHeadLocal)
		if r.fallbackUsed && cqe.UserData == timeoutUserData {
			r.CQAdvance(1)
			continue
		}
		return cqe
	}
}

// PeekBatchCQE fills cqes with up to len(cqes) ready CQEs without consuming
// them and returns how many it filled; release them with CQAdvance(n).
func (r *IoUring) PeekBatchCQE(cqes []*CQE) int {
	if len(cqes) == 0 || r.PeekCQE() == nil {
		return 0
	}
	tail := atomic.LoadUint32(r.cqTail)
	n := 0
	for head := r.cqHeadLocal; head != tail && n < len(cqes); head++ {
		cqe := r.cqeAt(head)
		if r.fallbackUsed && cqe.UserData == timeoutUserData {
			break // consumed by the next PeekCQE
		}
		cqes[n] = cqe
		n++
	}
	return n
}

// CQESeen consumes the CQE returned by PeekCQE.
func (r *IoUring) CQESeen() { r.CQAdvance(1) }

// CQAdvance consumes n CQEs, returning their slots to the kernel.
func (r *IoUring) CQAdvance(n uint32) {
	r.cqHeadLocal += n
	atomic.StoreUint32(r.cqHead, r.cqHeadLocal)
}

// WaitCQE returns the next CQE, waiting up to timeout (<= 0: no deadline)
// for one. Like PeekCQE it leaves the CQE in the ring.
func (r *IoUring) WaitCQE(timeout time.Duration) (*CQE, error) {
	if cqe := r.PeekCQE(); cqe != nil {
		return cqe, nil
	}
	if _, err := r.submitAndWait(false, 1, timeout); err != nil {
		return nil, err
	}
	if cqe := r.PeekCQE(); cqe != nil {
		return cqe, nil
	}
	return nil, unix.EAGAIN
}

// BigCQE returns the extra 16 bytes of a CQE on an IORING_SETUP_CQE32 ring,
// or nil on a ring with 16-byte CQEs.
func (r *IoUring) BigCQE(c *CQE) *[2]uint64 {
	if r.cqeShift == 0 {
		return nil
	}
	return &(*CQE32)(unsafe.Pointer(c)).BigCQE
}
