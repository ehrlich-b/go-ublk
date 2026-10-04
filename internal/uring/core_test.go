package uring

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Real-ring tests for IoUring. They need a kernel that permits io_uring
// (inside a default-seccomp container io_uring_setup fails with EPERM, so
// there they skip). Kernel 6.6 has everything they use.

func newTestIoUring(t testing.TB, opts SetupOptions) *IoUring {
	t.Helper()
	r, err := NewIoUring(opts)
	if err != nil {
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOSYS) {
			t.Skipf("io_uring unavailable: %v", err)
		}
		t.Fatalf("NewIoUring(%+v): %v", opts, err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return r
}

func mustGetSQE(t testing.TB, r *IoUring) *SQE {
	t.Helper()
	sqe := r.GetSQE()
	if sqe == nil {
		t.Fatal("GetSQE: SQ full")
	}
	return sqe
}

// reap collects n CQEs by user_data, failing if they do not arrive in time
// or a user_data repeats.
func reap(t testing.TB, r *IoUring, n int, timeout time.Duration) map[uint64]CQE {
	t.Helper()
	got := make(map[uint64]CQE, n)
	deadline := time.Now().Add(timeout)
	for len(got) < n {
		left := time.Until(deadline)
		if left <= 0 {
			t.Fatalf("reaped %d of %d CQEs before the deadline: %v", len(got), n, got)
		}
		cqe, err := r.WaitCQE(left)
		if err != nil {
			t.Fatalf("WaitCQE after %d of %d CQEs: %v", len(got), n, err)
		}
		if _, dup := got[cqe.UserData]; dup {
			t.Fatalf("user_data %#x completed twice", cqe.UserData)
		}
		got[cqe.UserData] = *cqe
		r.CQESeen()
	}
	return got
}

func tempFileFd(t *testing.T) (*os.File, int32) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "uring")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, int32(f.Fd())
}

func pipeFds(t *testing.T) (int32, int32) {
	t.Helper()
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC); err != nil {
		t.Fatalf("pipe2: %v", err)
	}
	t.Cleanup(func() {
		_ = unix.Close(p[0])
		_ = unix.Close(p[1])
	})
	return int32(p[0]), int32(p[1])
}

func offHeap(t testing.TB, size int) []byte {
	t.Helper()
	b, err := AllocOffHeap(size)
	if err != nil {
		t.Fatalf("AllocOffHeap(%d): %v", size, err)
	}
	t.Cleanup(func() { _ = FreeOffHeap(b) })
	return b
}

func TestSetupSizesAndFeatures(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 5, CQEntries: 20})
	if r.SQEntries() != 8 || r.CQEntries() != 32 {
		t.Errorf("SQ/CQ entries = %d/%d, want 8/32 (rounded up to powers of two)", r.SQEntries(), r.CQEntries())
	}
	if r.Flags() != IORING_SETUP_CQSIZE {
		t.Errorf("Flags() = %#x, want IORING_SETUP_CQSIZE", r.Flags())
	}
	for _, f := range []uint32{IORING_FEAT_SINGLE_MMAP, IORING_FEAT_NODROP, IORING_FEAT_SUBMIT_STABLE,
		IORING_FEAT_EXT_ARG} {
		if r.Features()&f == 0 {
			t.Errorf("kernel lacks feature %#x (features %#x)", f, r.Features())
		}
	}
	t.Logf("features %#x", r.Features())
}

// A real EINVAL from the kernel drives the optional-flag fallback:
// DEFER_TASKRUN without SINGLE_ISSUER is invalid, so it is dropped (it is the
// highest optional bit) and SUBMIT_ALL survives. As a required flag it fails.
func TestSetupDropsRejectedOptionalFlags(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{
		Entries:       4,
		OptionalFlags: IORING_SETUP_DEFER_TASKRUN | IORING_SETUP_SUBMIT_ALL,
	})
	if r.Flags() != IORING_SETUP_SUBMIT_ALL {
		t.Errorf("Flags() = %#x, want only IORING_SETUP_SUBMIT_ALL", r.Flags())
	}
	if _, err := NewIoUring(SetupOptions{Entries: 4, Flags: IORING_SETUP_DEFER_TASKRUN}); !errors.Is(err, unix.EINVAL) {
		t.Errorf("required DEFER_TASKRUN without SINGLE_ISSUER: err = %v, want EINVAL", err)
	}
	if _, err := NewIoUring(SetupOptions{Entries: 4, Flags: IORING_SETUP_SQPOLL}); err == nil {
		t.Error("unsupported SQPOLL accepted")
	}
}

func TestProbe(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 4})
	p, err := r.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	for _, op := range []uint8{IORING_OP_NOP, IORING_OP_READ, IORING_OP_WRITE_FIXED, IORING_OP_FALLOCATE,
		IORING_OP_MSG_RING, IORING_OP_URING_CMD} {
		if !p.Supported(op) {
			t.Errorf("opcode %d reported unsupported", op)
		}
	}
	if p.LastOp() < IORING_OP_URING_CMD || p.Supported(255) {
		t.Errorf("LastOp %d, Supported(255) %v", p.LastOp(), p.Supported(255))
	}
	t.Logf("last opcode %d", p.LastOp())
}

func TestNopRoundTrips(t *testing.T) {
	for _, flags := range []uint32{0, IORING_SETUP_SQE128 | IORING_SETUP_CQE32} {
		r := newTestIoUring(t, SetupOptions{Entries: 8, Flags: flags})
		var batch [8]*CQE
		for round := 0; round < 100; round++ { // wraps the rings many times
			for i := 0; i < 8; i++ {
				sqe := mustGetSQE(t, r)
				PrepNop(sqe)
				sqe.UserData = uint64(round*8 + i)
			}
			if r.GetSQE() != nil {
				t.Fatal("GetSQE succeeded on a full SQ")
			}
			if n, err := r.SubmitAndWait(8, time.Second); err != nil || n != 8 {
				t.Fatalf("round %d: SubmitAndWait = %d, %v", round, n, err)
			}
			if n := r.PeekBatchCQE(batch[:]); n != 8 {
				t.Fatalf("round %d: PeekBatchCQE = %d, want 8", round, n)
			}
			for i, cqe := range batch {
				if cqe.UserData != uint64(round*8+i) || cqe.Res != 0 {
					t.Fatalf("round %d CQE %d = %+v", round, i, *cqe)
				}
			}
			r.CQAdvance(8)
		}
		if r.CQReady() != 0 || r.SQSpaceLeft() != 8 {
			t.Errorf("flags %#x: CQReady %d, SQSpaceLeft %d after draining", flags, r.CQReady(), r.SQSpaceLeft())
		}
		if (r.BigCQE(&CQE{}) == nil) == (flags&IORING_SETUP_CQE32 != 0) {
			t.Errorf("flags %#x: BigCQE availability wrong", flags)
		}
	}
}

func TestSubmitAndWaitTimesOut(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 4})
	start := time.Now()
	if _, err := r.SubmitAndWait(1, 50*time.Millisecond); !errors.Is(err, unix.ETIME) {
		t.Fatalf("SubmitAndWait on an idle ring: %v, want ETIME", err)
	}
	if d := time.Since(start); d < 45*time.Millisecond || d > time.Second {
		t.Errorf("timed out after %v, want ~50ms", d)
	}
	if err := r.WaitCQEs(0, time.Millisecond); err != nil {
		t.Errorf("WaitCQEs(0): %v", err)
	}
}

// Kernels before 5.11 lack IORING_FEAT_EXT_ARG; the wait is then bounded by
// an internal IORING_OP_TIMEOUT whose CQE the caller never sees.
func TestWaitTimeoutFallbackWithoutExtArg(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 4})
	r.extArg = false
	start := time.Now()
	if _, err := r.SubmitAndWait(1, 50*time.Millisecond); !errors.Is(err, unix.ETIME) {
		t.Fatalf("SubmitAndWait: %v, want ETIME", err)
	}
	if d := time.Since(start); d < 45*time.Millisecond || d > time.Second {
		t.Errorf("timed out after %v, want ~50ms", d)
	}
	time.Sleep(20 * time.Millisecond)
	if cqe := r.PeekCQE(); cqe != nil {
		t.Fatalf("internal timeout CQE leaked: %+v", *cqe)
	}
	sqe := mustGetSQE(t, r)
	PrepNop(sqe)
	sqe.UserData = 42
	if n, err := r.SubmitAndWait(1, time.Second); err != nil || n != 1 {
		t.Fatalf("SubmitAndWait = %d, %v; want 1 (the NOP, not the timeout)", n, err)
	}
	if cqe := r.PeekCQE(); cqe == nil || cqe.UserData != 42 {
		t.Fatalf("PeekCQE = %v, want the NOP", cqe)
	}
}

func TestReadWriteFile(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	_, fd := tempFileFd(t)
	out := offHeap(t, 8192)
	for i := range out {
		out[i] = byte(i * 7)
	}
	in := make([]byte, 8192)
	sqe := mustGetSQE(t, r)
	PrepWrite(sqe, fd, out, 4096)
	sqe.UserData = 1
	got := reapAfterSubmit(t, r, 1)
	if got[1].Res != 8192 {
		t.Fatalf("write res = %d", got[1].Res)
	}
	sqe = mustGetSQE(t, r)
	PrepRead(sqe, fd, in, 4096)
	sqe.UserData = 2
	if got = reapAfterSubmit(t, r, 1); got[2].Res != 8192 || !bytes.Equal(in, out) {
		t.Fatalf("read res = %d, data equal %v", got[2].Res, bytes.Equal(in, out))
	}
	iov := []unix.Iovec{{Base: &in[0]}, {Base: &in[100]}}
	iov[0].SetLen(100)
	iov[1].SetLen(100)
	clear(in)
	sqe = mustGetSQE(t, r)
	PrepReadv(sqe, fd, iov, 4096)
	sqe.UserData = 3
	if got = reapAfterSubmit(t, r, 1); got[3].Res != 200 || !bytes.Equal(in[:200], out[:200]) {
		t.Fatalf("readv res = %d", got[3].Res)
	}
	runtime.KeepAlive(in)
}

func reapAfterSubmit(t *testing.T, r *IoUring, n int) map[uint64]CQE {
	t.Helper()
	if _, err := r.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return reap(t, r, n, 2*time.Second)
}

// An eventfd read parks until written, which is how the queue engine will
// hear about asynchronous backend completions.
func TestEventfdRead(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	efd, err := unix.Eventfd(0, unix.EFD_CLOEXEC)
	if err != nil {
		t.Fatalf("eventfd: %v", err)
	}
	defer unix.Close(efd)
	buf := offHeap(t, 8)
	sqe := mustGetSQE(t, r)
	PrepRead(sqe, int32(efd), buf, 0)
	sqe.UserData = 1
	if _, err := r.SubmitAndWait(1, 30*time.Millisecond); !errors.Is(err, unix.ETIME) {
		t.Fatalf("eventfd read completed before any write: %v", err)
	}
	var v [8]byte
	binary.LittleEndian.PutUint64(v[:], 5)
	if _, err := unix.Write(efd, v[:]); err != nil {
		t.Fatalf("write eventfd: %v", err)
	}
	got := reap(t, r, 1, 2*time.Second)
	if got[1].Res != 8 || binary.LittleEndian.Uint64(buf) != 5 {
		t.Fatalf("eventfd read res %d value %d, want 8 and 5", got[1].Res, binary.LittleEndian.Uint64(buf))
	}
	// Signal it through the ring as well.
	in := offHeap(t, 8)
	binary.LittleEndian.PutUint64(in, 7)
	sqe = mustGetSQE(t, r)
	PrepWrite(sqe, int32(efd), in, 0)
	sqe.UserData = 2
	sqe = mustGetSQE(t, r)
	PrepRead(sqe, int32(efd), buf, 0)
	sqe.UserData = 3
	got = reapAfterSubmit(t, r, 2)
	if got[2].Res != 8 || got[3].Res != 8 || binary.LittleEndian.Uint64(buf) != 7 {
		t.Fatalf("ring eventfd write/read = %d/%d value %d", got[2].Res, got[3].Res, binary.LittleEndian.Uint64(buf))
	}
}

func TestLinkedOps(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	_, fd := tempFileFd(t)
	out := offHeap(t, 4096)
	in := offHeap(t, 4096)
	copy(out, "linked write, fsync, read")
	sqe := mustGetSQE(t, r)
	PrepWrite(sqe, fd, out, 0)
	sqe.Flags |= IOSQE_IO_LINK
	sqe.UserData = 1
	sqe = mustGetSQE(t, r)
	PrepFsync(sqe, fd, IORING_FSYNC_DATASYNC)
	sqe.Flags |= IOSQE_IO_LINK
	sqe.UserData = 2
	sqe = mustGetSQE(t, r)
	PrepRead(sqe, fd, in, 0)
	sqe.UserData = 3
	if _, err := r.SubmitAndWait(3, 2*time.Second); err != nil {
		t.Fatalf("SubmitAndWait: %v", err)
	}
	for want := uint64(1); want <= 3; want++ { // a chain completes in order
		cqe := r.PeekCQE()
		if cqe == nil || cqe.UserData != want || cqe.Res < 0 {
			t.Fatalf("chain CQE %d = %v", want, cqe)
		}
		r.CQESeen()
	}
	if !bytes.Equal(in, out) {
		t.Fatal("read after linked write returned different data")
	}
	// A failing head cancels the rest of the chain.
	sqe = mustGetSQE(t, r)
	PrepRead(sqe, -1, in, 0)
	sqe.Flags |= IOSQE_IO_LINK
	sqe.UserData = 4
	sqe = mustGetSQE(t, r)
	PrepNop(sqe)
	sqe.UserData = 5
	got := reapAfterSubmit(t, r, 2)
	if got[4].Res != -int32(unix.EBADF) || got[5].Res != -int32(unix.ECANCELED) {
		t.Fatalf("failed chain: read %d, nop %d; want -EBADF, -ECANCELED", got[4].Res, got[5].Res)
	}
}

func TestTimeoutOps(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	ts := &Timespec{Nsec: int64(10 * time.Millisecond)}
	sqe := mustGetSQE(t, r)
	PrepTimeout(sqe, ts, 0, 0)
	sqe.UserData = 1
	start := time.Now()
	got := reapAfterSubmit(t, r, 1)
	if got[1].Res != -int32(unix.ETIME) || time.Since(start) < 9*time.Millisecond {
		t.Fatalf("timeout res %d after %v, want -ETIME after ~10ms", got[1].Res, time.Since(start))
	}
	// A link timeout cancels a read that never completes.
	rfd, _ := pipeFds(t)
	buf := offHeap(t, 64)
	sqe = mustGetSQE(t, r)
	PrepRead(sqe, rfd, buf, 0)
	sqe.Flags |= IOSQE_IO_LINK
	sqe.UserData = 2
	sqe = mustGetSQE(t, r)
	PrepLinkTimeout(sqe, &Timespec{Nsec: int64(20 * time.Millisecond)}, 0)
	sqe.UserData = 3
	got = reapAfterSubmit(t, r, 2)
	if got[2].Res != -int32(unix.ECANCELED) || got[3].Res != -int32(unix.ETIME) {
		t.Fatalf("link timeout: read %d, timeout %d; want -ECANCELED, -ETIME", got[2].Res, got[3].Res)
	}
}

func TestCancel(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	rfd, _ := pipeFds(t)
	buf := offHeap(t, 64)
	sqe := mustGetSQE(t, r)
	PrepRead(sqe, rfd, buf, 0)
	sqe.UserData = 10
	if _, err := r.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sqe = mustGetSQE(t, r)
	PrepCancel(sqe, 10, 0)
	sqe.UserData = 11
	got := reapAfterSubmit(t, r, 2)
	if got[10].Res != -int32(unix.ECANCELED) || got[11].Res != 0 {
		t.Fatalf("cancel: read %d, cancel %d; want -ECANCELED, 0", got[10].Res, got[11].Res)
	}
}

func TestMsgRingWakesAnotherRing(t *testing.T) {
	target := newTestIoUring(t, SetupOptions{Entries: 4})
	sender := newTestIoUring(t, SetupOptions{Entries: 4})
	sqe := mustGetSQE(t, sender)
	PrepMsgRing(sqe, int32(target.Fd()), 99, 0xFEED, 0)
	sqe.UserData = 1
	if got := reapAfterSubmit(t, sender, 1); got[1].Res != 0 {
		t.Fatalf("MSG_RING res %d", got[1].Res)
	}
	got := reap(t, target, 1, time.Second)
	if got[0xFEED].Res != 99 {
		t.Fatalf("target CQE = %+v, want user_data 0xfeed res 99", got)
	}
}

func TestFallocateAndPollAdd(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	f, fd := tempFileFd(t)
	sqe := mustGetSQE(t, r)
	PrepFallocate(sqe, fd, 0, 0, 1<<20)
	sqe.UserData = 1
	if got := reapAfterSubmit(t, r, 1); got[1].Res != 0 {
		t.Fatalf("fallocate res %d", got[1].Res)
	}
	if st, err := f.Stat(); err != nil || st.Size() != 1<<20 {
		t.Fatalf("size after fallocate = %v, %v; want 1MiB", st.Size(), err)
	}
	rfd, wfd := pipeFds(t)
	sqe = mustGetSQE(t, r)
	PrepPollAdd(sqe, rfd, unix.POLLIN)
	sqe.UserData = 2
	if _, err := r.SubmitAndWait(1, 20*time.Millisecond); !errors.Is(err, unix.ETIME) {
		t.Fatalf("poll on an empty pipe completed: %v", err)
	}
	if _, err := unix.Write(int(wfd), []byte("x")); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	if got := reap(t, r, 1, time.Second); got[2].Res&unix.POLLIN == 0 {
		t.Fatalf("poll res %#x, want POLLIN", got[2].Res)
	}
}

// With a CQ of 4, eight completions overflow. FEAT_NODROP parks the extra
// four in the kernel's overflow list (IORING_SQ_CQ_OVERFLOW) and PeekCQE pulls
// them in once there is room; none is lost or duplicated.
func TestCQOverflowIsFlushedNotDropped(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 4, CQEntries: 4})
	for batch := 0; batch < 2; batch++ {
		for i := 0; i < 4; i++ {
			sqe := mustGetSQE(t, r)
			PrepNop(sqe)
			sqe.UserData = uint64(batch*4 + i)
		}
		if n, err := r.Submit(); err != nil || n != 4 {
			t.Fatalf("batch %d: Submit = %d, %v", batch, n, err)
		}
	}
	if r.SQFlags()&IORING_SQ_CQ_OVERFLOW == 0 {
		t.Fatalf("SQ flags %#x: overflow not flagged with 8 completions in a 4-entry CQ", r.SQFlags())
	}
	if got := r.CQReady(); got != 4 {
		t.Fatalf("CQ holds %d entries, want 4 (full)", got)
	}
	seen := make(map[uint64]bool)
	for cqe := r.PeekCQE(); cqe != nil; cqe = r.PeekCQE() {
		if seen[cqe.UserData] {
			t.Fatalf("user_data %d delivered twice", cqe.UserData)
		}
		seen[cqe.UserData] = true
		r.CQESeen()
	}
	if len(seen) != 8 {
		t.Fatalf("delivered %d completions, want 8: %v", len(seen), seen)
	}
	if r.SQFlags()&IORING_SQ_CQ_OVERFLOW != 0 || r.CQOverflow() != 0 {
		t.Errorf("after draining: SQ flags %#x, dropped %d", r.SQFlags(), r.CQOverflow())
	}
}

// DEFER_TASKRUN (6.1) with SINGLE_ISSUER: asynchronous completions reach the
// CQ only when the owning thread enters with GETEVENTS, and the kernel turns
// away other threads with EEXIST.
func TestDeferTaskrunSingleIssuer(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r := newTestIoUring(t, SetupOptions{
		Entries:       8,
		Flags:         IORING_SETUP_SINGLE_ISSUER | IORING_SETUP_DEFER_TASKRUN,
		OptionalFlags: IORING_SETUP_TASKRUN_FLAG,
	})
	sqe := mustGetSQE(t, r)
	PrepNop(sqe)
	sqe.UserData = 1
	if got := reapAfterSubmit(t, r, 1); got[1].Res != 0 {
		t.Fatalf("NOP res %d", got[1].Res)
	}
	efd, err := unix.Eventfd(0, unix.EFD_CLOEXEC)
	if err != nil {
		t.Fatalf("eventfd: %v", err)
	}
	defer unix.Close(efd)
	buf := offHeap(t, 8)
	sqe = mustGetSQE(t, r)
	PrepRead(sqe, int32(efd), buf, 0)
	sqe.UserData = 2
	if _, err := r.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	other := make(chan [2]error)
	go func() {
		runtime.LockOSThread() // a different thread: ours is locked to this test
		defer runtime.UnlockOSThread()
		var v [8]byte
		v[0] = 1
		_, werr := unix.Write(efd, v[:])
		other <- [2]error{werr, r.RegisterFilesSparse(4)}
	}()
	errs := <-other
	if errs[0] != nil {
		t.Fatalf("write eventfd: %v", errs[0])
	}
	if !errors.Is(errs[1], unix.EEXIST) {
		t.Errorf("register from another thread: %v, want EEXIST", errs[1])
	}
	time.Sleep(50 * time.Millisecond)
	if n := r.CQReady(); n != 0 {
		t.Fatalf("%d CQE visible before GETEVENTS on a DEFER_TASKRUN ring", n)
	}
	if r.Flags()&IORING_SETUP_TASKRUN_FLAG != 0 && r.SQFlags()&IORING_SQ_TASKRUN == 0 {
		t.Errorf("TASKRUN_FLAG accepted but IORING_SQ_TASKRUN not set with work pending")
	}
	if err := r.GetEvents(); err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	cqe := r.PeekCQE()
	if cqe == nil || cqe.UserData != 2 || cqe.Res != 8 {
		t.Fatalf("after GETEVENTS: %v, want the eventfd read", cqe)
	}
	r.CQESeen()
}

func TestCloseIsIdempotent(t *testing.T) {
	r, err := NewIoUring(SetupOptions{Entries: 4})
	if err != nil {
		t.Skipf("io_uring unavailable: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if r.sqHead != nil || r.cqes != nil || r.sqRing != nil {
		t.Error("Close left pointers into the unmapped rings")
	}
}

func TestOffHeapIsPageAligned(t *testing.T) {
	b := offHeap(t, 100)
	if uintptr(unsafe.Pointer(&b[0]))%uintptr(os.Getpagesize()) != 0 {
		t.Error("AllocOffHeap memory is not page aligned")
	}
	if AddrOf(nil) != 0 || AddrOf(b) != uint64(uintptr(unsafe.Pointer(&b[0]))) {
		t.Error("AddrOf mismatch")
	}
}
