package queue

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// fakeResult is a minimal uring.Result implementation for the fake ring.
type fakeResult struct {
	userData uint64
	value    int32
	err      error
}

func (fr *fakeResult) UserData() uint64 { return fr.userData }
func (fr *fakeResult) Value() int32     { return fr.value }
func (fr *fakeResult) Error() error     { return fr.err }

// lifecycleFakeRing implements uring.Ring with no syscalls. SubmitIOCmd counts
// calls (needed to prove Prime() submits one initial FETCH_REQ per tag);
// WaitForCompletion returns nothing immediately by default, or blocks on
// release until the test closes it (channel-gated, no raw sleeps).
type lifecycleFakeRing struct {
	mu          sync.Mutex
	submitCount int
	submitErr   error
	release     chan struct{}
}

func (f *lifecycleFakeRing) Close() error { return nil }

func (f *lifecycleFakeRing) SubmitCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (uring.Result, error) {
	return &fakeResult{userData: userData}, nil
}

func (f *lifecycleFakeRing) SubmitCtrlCmdAsync(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (*uring.AsyncHandle, error) {
	return nil, nil
}

func (f *lifecycleFakeRing) SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) (uring.Result, error) {
	f.mu.Lock()
	f.submitCount++
	f.mu.Unlock()
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	return &fakeResult{userData: userData}, nil
}

func (f *lifecycleFakeRing) PrepareIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error {
	return nil
}

func (f *lifecycleFakeRing) FlushSubmissions() (uint32, error) {
	return 0, nil
}

func (f *lifecycleFakeRing) WaitForCompletion(timeout int) ([]uring.Result, error) {
	if f.release != nil {
		<-f.release
	}
	return nil, nil
}

func (f *lifecycleFakeRing) NewBatch() uring.Batch {
	return nil
}

// mmapSyntheticMem allocates anonymous memory for the synthetic runner's
// descPtr/bufPtr using the exact sizes Runner.Close() will munmap. This is
// deliberate: a Go heap make([]byte, ...) buffer of bufSize (depth * 64KB) is
// a page-aligned direct mmap, and Close()'s munmap of it is undefined
// behavior that can crash the running ioLoop goroutine. Anonymous mmap makes
// the Close() unmap path well-defined (mirroring production mmapQueues).
func mmapSyntheticMem(depth int) (unsafe.Pointer, unsafe.Pointer) {
	descSize := depth * int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
	bufSize := depth * constants.IOBufferSizePerTag
	if rem := descSize % os.Getpagesize(); rem != 0 {
		descSize += os.Getpagesize() - rem
	}
	desc, err := syscall.Mmap(-1, 0, descSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANONYMOUS|syscall.MAP_PRIVATE)
	if err != nil {
		panic(fmt.Sprintf("mmap desc synthetic memory: %v", err))
	}
	buf, err := syscall.Mmap(-1, 0, bufSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANONYMOUS|syscall.MAP_PRIVATE)
	if err != nil {
		panic(fmt.Sprintf("mmap buf synthetic memory: %v", err))
	}
	return unsafe.Pointer(&desc[0]), unsafe.Pointer(&buf[0])
}

// newLifecycleRunner builds a Runner wired to f with non-stub prerequisites
// (charDeviceFd >= 0, ring != nil) so Start()/ioLoop take the REAL path that
// calls Prime() and runs processRequests. charDeviceFd 0 is never used as a
// real fd by the fake ring; Close() closing fd 0 (stdin) is harmless.
func newLifecycleRunner(f uring.Ring, depth int) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	descPtr, bufPtr := mmapSyntheticMem(depth)
	return &Runner{
		deviceID:     0,
		queueID:      0,
		depth:        depth,
		charDeviceFd: 0,
		ring:         f,
		descPtr:      descPtr,
		bufPtr:       bufPtr,
		ctx:          ctx,
		cancel:       cancel,
		tagStates:    make([]TagState, depth),
		tagMutexes:   make([]sync.Mutex, depth),
		ioCmds:       make([]uapi.UblksrvIOCmd, depth),
		done:         make(chan struct{}),
	}
}

func (f *lifecycleFakeRing) submits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.submitCount
}

// TestRunnerLifecycleFastHappyPath exercises the real ioLoop path for the first
// time: Prime() submits one initial FETCH_REQ per tag, then the loop spins
// cheaply (WaitForCompletion returns no work) until Stop() cancels the context.
func TestRunnerLifecycleFastHappyPath(t *testing.T) {
	fake := &lifecycleFakeRing{}
	r := newLifecycleRunner(fake, 4)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	if n := fake.submits(); n != 4 {
		t.Fatalf("Prime() should have submitted exactly 4 initial FETCH_REQs (one per tag), got %d", n)
	}

	if err := r.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}
	if !r.Wait(2 * time.Second) {
		t.Fatal("Wait(2s) returned false; ioLoop did not exit after Stop()")
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}

// TestPrimePreconditionGuard asserts Prime()'s not-initialized guard fires
// safely (no nil-ring dereference) before any submission is attempted.
func TestPrimePreconditionGuard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		depth:        4,
		charDeviceFd: -1,
		ring:         nil,
		ctx:          ctx,
		cancel:       cancel,
		tagStates:    make([]TagState, 4),
		tagMutexes:   make([]sync.Mutex, 4),
		ioCmds:       make([]uapi.UblksrvIOCmd, 4),
		done:         make(chan struct{}),
	}

	err := r.Prime()
	if err == nil {
		t.Fatal("Prime() with charDeviceFd=-1 and nil ring returned nil, want 'not initialized' error")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("Prime() error %q does not contain 'not initialized'", err)
	}
}

// TestWaitBeforeStartNeverBlocks asserts the !launched fast path: Wait() on a
// runner that was never started returns true immediately, not after the timeout.
func TestWaitBeforeStartNeverBlocks(t *testing.T) {
	r := NewStubRunner(context.Background(), Config{DevID: 0, QueueID: 0, Depth: 4})

	start := time.Now()
	ok := r.Wait(1 * time.Second)
	elapsed := time.Since(start)

	if !ok {
		t.Fatal("Wait(1s) returned false on a never-started runner; want true")
	}
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("Wait() on a never-started runner blocked for %v; want immediate return", elapsed)
	}

	_ = r.Close()
}

// TestRunnerWaitTimeoutThenTrue asserts Wait()'s timeout branch is live: while
// the ioLoop goroutine is blocked inside WaitForCompletion, cancellation alone
// cannot wake it, so Wait() returns false; only after the test releases the
// gating channel does ioLoop observe ctx.Done() and exit, making Wait() true.
func TestRunnerWaitTimeoutThenTrue(t *testing.T) {
	fake := &lifecycleFakeRing{release: make(chan struct{})}
	r := newLifecycleRunner(fake, 4)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	if r.Wait(50 * time.Millisecond) {
		t.Fatal("Wait(50ms) returned true while the goroutine is still blocked in WaitForCompletion; want false")
	}

	close(fake.release)

	if !r.Wait(2 * time.Second) {
		t.Fatal("Wait(2s) returned false after releasing the gate; ioLoop should have exited")
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}

// TestPrimeEOPNOTSUPPRawErrno pins the friendlier branch's OWN logic: a raw
// syscall.EOPNOTSUPP surfaced directly as the error (the shape Prime() was
// written to expect) does fire the "device not ready (START_DEV pending)" path.
func TestPrimeEOPNOTSUPPRawErrno(t *testing.T) {
	fake := &lifecycleFakeRing{submitErr: syscall.EOPNOTSUPP}
	r := newLifecycleRunner(fake, 1)

	err := r.Prime()
	if err == nil {
		t.Fatal("Prime() returned nil with SubmitIOCmd failing EOPNOTSUPP")
	}
	if !strings.Contains(err.Error(), "device not ready (START_DEV pending)") {
		t.Fatalf("expected EOPNOTSUPP special-case branch, got: %v", err)
	}
}

// TestPrimeEOPNOTSUPPWrappedIsDeadCode pins the finding that the EOPNOTSUPP
// special case can never fire in production: the real minimalRing.SubmitIOCmd
// wraps the errno with fmt.Errorf(..., "%v", errno) in flushSubmissions
// (internal/uring/minimal.go:1040), which loses the syscall.Errno dynamic type,
// so the `err.(syscall.Errno)` assertion in Prime() (runner.go:227) never
// succeeds — even though the underlying cause really is EOPNOTSUPP. The generic
// "submit initial FETCH_REQ" branch fires instead.
func TestPrimeEOPNOTSUPPWrappedIsDeadCode(t *testing.T) {
	fake := &lifecycleFakeRing{submitErr: fmt.Errorf("io_uring_enter failed: %v", syscall.EOPNOTSUPP)}
	r := newLifecycleRunner(fake, 1)

	err := r.Prime()
	if err == nil {
		t.Fatal("Prime() returned nil with SubmitIOCmd failing wrapped EOPNOTSUPP")
	}
	if strings.Contains(err.Error(), "device not ready (START_DEV pending)") {
		t.Fatalf("EOPNOTSUPP branch fired for a %v-wrapped errno; production SubmitIOCmd never produces a raw syscall.Errno: %v", err, err)
	}
	if !strings.Contains(err.Error(), "submit initial FETCH_REQ[0]") {
		t.Fatalf("expected the generic 'submit initial FETCH_REQ[0]' branch, got: %v", err)
	}
}
