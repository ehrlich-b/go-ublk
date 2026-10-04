package queue

import (
	"bytes"
	"errors"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

const testBufSize = 64 << 10

// startEngine runs an engine over all tags of a fake kernel.
func startEngine(t *testing.T, k *fakeKernel, h Handler, inline bool) *engine {
	t.Helper()
	return startEngineWait(t, k, h, inline, 20*time.Millisecond)
}

// startEngineWait is startEngine with an explicit bound on each kernel wait.
func startEngineWait(t *testing.T, k *fakeKernel, h Handler, inline bool, wait time.Duration) *engine {
	t.Helper()
	e := newEngine(engineConfig{
		queueID:      0,
		tagLo:        0,
		tagHi:        k.depth,
		charFd:       k.ufile,
		desc:         unsafe.Pointer(&k.desc[0]),
		descStride:   24,
		bufs:         unsafe.Pointer(&k.bufs[0]),
		bufSize:      testBufSize,
		userCopy:     k.ufile >= 0,
		handler:      h,
		inline:       inline,
		newRing:      func(uint32) (ring, error) { return k, nil },
		waitInterval: wait,
	})
	if err := e.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		e.abandon()
		select {
		case <-e.done:
		case <-time.After(5 * time.Second):
			t.Errorf("engine did not exit")
		}
	})
	return e
}

func waitDone(t *testing.T, e *engine) {
	t.Helper()
	select {
	case <-e.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("engine did not exit")
	}
}

// memHandler is a RAM backend served through BackendHandler.
type memBackend struct {
	mu   sync.Mutex
	data []byte
}

func (m *memBackend) ReadAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return copy(p, m.data[off:]), nil
}
func (m *memBackend) WriteAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return copy(m.data[off:], p), nil
}
func (m *memBackend) Size() int64  { return int64(len(m.data)) }
func (m *memBackend) Close() error { return nil }
func (m *memBackend) Flush() error { return nil }
func (m *memBackend) Discard(off, n int64) error {
	return nil
}

func pattern(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i*7)
	}
	return b
}

func testRoundTrip(t *testing.T, inline, userCopy bool) {
	k := newFakeKernel(t, 4, testBufSize)
	if userCopy {
		f, err := os.CreateTemp(t.TempDir(), "ublkc")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		k.ufile = int(f.Fd())
	}
	b := &memBackend{data: make([]byte, 1<<20)}
	startEngine(t, k, BackendHandler(b, nil), inline)

	w := pattern(8192, 3)
	k.inject(1, fkReq{op: uapi.UBLK_IO_OP_WRITE, sector: 16, nr: 16, data: w, id: 1})
	k.inject(1, fkReq{op: uapi.UBLK_IO_OP_READ, sector: 16, nr: 16, id: 2})
	c := k.waitCommits(2, 5*time.Second)
	if c[0].result != 8192 || c[1].result != 8192 {
		t.Fatalf("results %d, %d; want 8192, 8192", c[0].result, c[1].result)
	}
	if !bytes.Equal(b.data[8192:16384], w) {
		t.Fatalf("backend did not receive the write")
	}
	if !bytes.Equal(c[1].data, w) {
		t.Fatalf("read returned different data")
	}
	if userCopy && (c[0].addr != 0 || c[1].addr != 0) {
		t.Fatalf("user-copy commits carried buffer addresses %#x, %#x", c[0].addr, c[1].addr)
	}
}

func TestEngineRoundTripInline(t *testing.T)            { testRoundTrip(t, true, false) }
func TestEngineRoundTripGoroutine(t *testing.T)         { testRoundTrip(t, false, false) }
func TestEngineRoundTripUserCopyInline(t *testing.T)    { testRoundTrip(t, true, true) }
func TestEngineRoundTripUserCopyGoroutine(t *testing.T) { testRoundTrip(t, false, true) }

// TestEngineAsyncOutOfOrderStress completes requests from other goroutines in
// random order with random delays, across many tags. Every request must be
// committed exactly once with its own result. Each kernel wait is bounded by an
// hour rather than the usual interval, so a lost eventfd wakeup hangs the
// engine and fails the test instead of costing a few milliseconds.
func TestEngineAsyncOutOfOrderStress(t *testing.T) {
	const depth, perTag = 32, 200
	k := newFakeKernel(t, depth, testBufSize)
	h := HandlerFunc(func(r *Request) {
		go func() {
			if rand.Intn(4) == 0 {
				time.Sleep(time.Duration(rand.Intn(200)) * time.Microsecond)
			}
			r.CompleteN(int(r.Length), nil)
		}()
	})
	startEngineWait(t, k, h, false, time.Hour)
	var wg sync.WaitGroup
	for tag := 0; tag < depth; tag++ {
		wg.Add(1)
		go func(tag int) {
			defer wg.Done()
			for i := 0; i < perTag; i++ {
				nr := uint32(1 + (tag+i)%16)
				k.inject(tag, fkReq{op: uapi.UBLK_IO_OP_READ, nr: nr, id: tag*perTag + i})
			}
		}(tag)
	}
	wg.Wait()
	c := k.waitCommits(depth*perTag, 30*time.Second)
	seen := make(map[int]bool)
	for _, x := range c {
		if seen[x.id] {
			t.Fatalf("request %d committed twice", x.id)
		}
		seen[x.id] = true
		tag, i := x.id/perTag, x.id%perTag
		if want := int32(1+(tag+i)%16) << 9; x.result != want {
			t.Fatalf("request %d result %d, want %d", x.id, x.result, want)
		}
	}
}

// TestEngineRangeOpsCommitZero is the engine-level regression test for
// Critical Bug #16: a discard of 4 GiB must commit 0, not a byte count that
// overflows int32 and makes the kernel fail it with EIO.
func TestEngineRangeOpsCommitZero(t *testing.T) {
	k := newFakeKernel(t, 2, testBufSize)
	startEngine(t, k, BackendHandler(&memBackend{data: make([]byte, 4096)}, nil), true)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_DISCARD, sector: 0, nr: 1 << 23, id: 1})
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 2})
	c := k.waitCommits(2, 5*time.Second)
	for _, x := range c {
		if x.result != 0 {
			t.Fatalf("op %d committed %d, want 0", x.op, x.result)
		}
	}
}

func TestEngineErrnoMapping(t *testing.T) {
	k := newFakeKernel(t, 1, testBufSize)
	errs := []error{syscall.ENOSPC, errors.New("opaque"), os.ErrDeadlineExceeded, nil}
	var n atomic.Int32
	h := HandlerFunc(func(r *Request) { r.Complete(errs[n.Add(1)-1]) })
	startEngine(t, k, h, true)
	for i := range errs {
		k.inject(0, fkReq{op: uapi.UBLK_IO_OP_WRITE, nr: 8, data: make([]byte, 4096), id: i})
	}
	c := k.waitCommits(len(errs), 5*time.Second)
	want := []int32{-int32(syscall.ENOSPC), -int32(syscall.EIO), -int32(syscall.ETIMEDOUT), 4096}
	for i, x := range c {
		if x.result != want[i] {
			t.Fatalf("commit %d result %d, want %d", i, x.result, want[i])
		}
	}
}

func TestEngineShortWriteFails(t *testing.T) {
	k := newFakeKernel(t, 1, testBufSize)
	h := HandlerFunc(func(r *Request) { r.CompleteN(int(r.Length)/2, nil) })
	startEngine(t, k, h, true)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_WRITE, nr: 8, data: make([]byte, 4096), id: 1})
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 8, id: 2})
	c := k.waitCommits(2, 5*time.Second)
	if c[0].result != -int32(syscall.EIO) {
		t.Fatalf("short write committed %d, want -EIO", c[0].result)
	}
	if c[1].result != 2048 {
		t.Fatalf("short read committed %d, want 2048 (partial reads are allowed)", c[1].result)
	}
}

func TestEngineNeedGetData(t *testing.T) {
	k := newFakeKernel(t, 2, testBufSize)
	k.needGetData = true
	b := &memBackend{data: make([]byte, 1<<20)}
	startEngine(t, k, BackendHandler(b, nil), false)
	w := pattern(4096, 9)
	k.inject(1, fkReq{op: uapi.UBLK_IO_OP_WRITE, sector: 8, nr: 8, data: w, id: 1})
	c := k.waitCommits(1, 5*time.Second)
	if c[0].result != 4096 || !bytes.Equal(b.data[4096:8192], w) {
		t.Fatalf("NEED_GET_DATA write: result %d, data matches %v", c[0].result, bytes.Equal(b.data[4096:8192], w))
	}
}

func TestEngineOversizedRequestFails(t *testing.T) {
	k := newFakeKernel(t, 1, testBufSize)
	called := false
	startEngine(t, k, HandlerFunc(func(r *Request) { called = true; r.Complete(nil) }), true)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: uint32(testBufSize/512) + 8, id: 1})
	c := k.waitCommits(1, 5*time.Second)
	if c[0].result != -int32(syscall.EIO) || called {
		t.Fatalf("oversized read: result %d, handler called %v", c[0].result, called)
	}
}

func TestEngineHandlerPanicFailsRequest(t *testing.T) {
	for _, inline := range []bool{true, false} {
		k := newFakeKernel(t, 1, testBufSize)
		var n atomic.Int32
		h := HandlerFunc(func(r *Request) {
			if n.Add(1) == 1 {
				panic("boom")
			}
			r.Complete(nil)
		})
		startEngine(t, k, h, inline)
		k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 1})
		k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 2})
		c := k.waitCommits(2, 5*time.Second)
		if c[0].result != -int32(syscall.EIO) || c[1].result != 0 {
			t.Fatalf("inline=%v: results %d, %d; want -EIO then 0", inline, c[0].result, c[1].result)
		}
	}
}

func TestRequestDoubleCompletePanics(t *testing.T) {
	k := newFakeKernel(t, 1, testBufSize)
	got := make(chan any, 1)
	h := HandlerFunc(func(r *Request) {
		r.Complete(nil)
		defer func() { got <- recover() }()
		r.Complete(nil)
	})
	startEngine(t, k, h, false)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 1})
	select {
	case p := <-got:
		if p == nil {
			t.Fatalf("second Complete did not panic")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("handler never ran")
	}
}

// TestEngineStopDrainsThenExits models STOP_DEV: a request still in the
// handler is committed, every tag is then aborted, and the engine exits
// cleanly with no error.
func TestEngineStopDrainsThenExits(t *testing.T) {
	k := newFakeKernel(t, 4, testBufSize)
	release := make(chan struct{})
	h := HandlerFunc(func(r *Request) {
		go func() { <-release; r.Complete(nil) }()
	})
	e := startEngine(t, k, h, false)
	k.inject(2, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 1})
	time.Sleep(20 * time.Millisecond)
	k.stop()
	select {
	case <-e.done:
		t.Fatalf("engine exited while a handler still held a request")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	waitDone(t, e)
	if e.err != nil {
		t.Fatalf("clean stop reported %v", e.err)
	}
	if c, _ := k.snapshot(); len(c) != 1 || c[0].result != 0 {
		t.Fatalf("commits after stop: %+v", c)
	}
	if !k.closed {
		t.Fatalf("engine did not close its ring")
	}
}

// TestEngineAbandonLeavesNewRequestsToKernel models Detach: the request in
// the handler is committed, a request that arrives afterwards is not
// dispatched, and the engine exits without the kernel aborting anything.
func TestEngineAbandonLeavesNewRequestsToKernel(t *testing.T) {
	k := newFakeKernel(t, 2, testBufSize)
	release := make(chan struct{})
	var calls atomic.Int32
	h := HandlerFunc(func(r *Request) {
		calls.Add(1)
		go func() { <-release; r.Complete(nil) }()
	})
	e := startEngine(t, k, h, false)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 1})
	time.Sleep(20 * time.Millisecond)
	e.abandon()
	k.inject(1, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 2})
	time.Sleep(20 * time.Millisecond)
	close(release)
	waitDone(t, e)
	if calls.Load() != 1 {
		t.Fatalf("handler called %d times; the request arriving during abandon must not be dispatched", calls.Load())
	}
	if c, _ := k.snapshot(); len(c) != 1 || c[0].id != 1 {
		t.Fatalf("commits: %+v, want only request 1", c)
	}
}

func TestEngineUnexpectedCompletionIsFatal(t *testing.T) {
	k := newFakeKernel(t, 2, testBufSize)
	e := startEngine(t, k, HandlerFunc(func(r *Request) { r.Complete(nil) }), true)
	k.mu.Lock()
	k.post(kindIO|1, -int32(syscall.EINVAL)) // tag 1 never asked for this
	k.mu.Unlock()
	waitDone(t, e)
	if e.err == nil {
		t.Fatalf("an unexpected completion did not fail the engine")
	}
}

func TestErrno(t *testing.T) {
	for _, c := range []struct {
		err  error
		want int32
	}{
		{nil, 0},
		{syscall.ENOSPC, int32(syscall.ENOSPC)},
		{&os.PathError{Op: "write", Err: syscall.EROFS}, int32(syscall.EROFS)},
		{errors.ErrUnsupported, int32(syscall.EOPNOTSUPP)},
		{os.ErrDeadlineExceeded, int32(syscall.ETIMEDOUT)},
		{errors.New("x"), int32(syscall.EIO)},
		{syscall.Errno(0), int32(syscall.EIO)},
	} {
		if got := Errno(c.err); got != c.want {
			t.Errorf("Errno(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

// TestEngineNoLostWakeup completes a request in exactly the window between the
// engine draining its completion list and announcing that it may sleep. The
// completer then sees "not sleeping" and skips the eventfd write, so only the
// engine's re-check after announcing can notice the completion. Each wait is
// bounded by an hour, so a lost wakeup fails the test.
func TestEngineNoLostWakeup(t *testing.T) {
	k := newFakeKernel(t, 1, testBufSize)
	// Inline mode: the handler and the hook both run on the engine thread, so
	// the interleaving is deterministic. The handler keeps the request without
	// completing it; the hook then completes it from inside the window.
	var pending *Request
	fired := false
	e := newEngine(engineConfig{
		tagLo: 0, tagHi: 1, charFd: -1,
		desc: unsafe.Pointer(&k.desc[0]), descStride: 24,
		bufs: unsafe.Pointer(&k.bufs[0]), bufSize: testBufSize,
		handler: HandlerFunc(func(r *Request) { pending = r }),
		inline:  true, waitInterval: time.Hour,
		newRing: func(uint32) (ring, error) { return k, nil },
	})
	e.testBeforeSleep = func() {
		if pending != nil && !fired {
			fired = true
			pending.Complete(nil) // pushes while sleeping is false: no eventfd write
		}
	}
	if err := e.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.abandon(); <-e.done })
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 1})
	c := k.waitCommits(1, 5*time.Second)
	if c[0].result != 0 {
		t.Fatalf("commits=%+v", c)
	}
}

func startZeroCopyEngine(t *testing.T, k *fakeKernel, auto bool) *engine {
	t.Helper()
	k.zcAuto = auto
	e := newEngine(engineConfig{
		tagLo: 0, tagHi: k.depth, charFd: 99,
		desc: unsafe.Pointer(&k.desc[0]), descStride: 24,
		bufSize:      testBufSize,
		zeroCopy:     &zeroCopyConfig{fd: 77, base: 1 << 20, auto: auto},
		waitInterval: 20 * time.Millisecond,
		newRing:      func(uint32) (ring, error) { return k, nil },
	})
	if err := e.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { e.abandon(); <-e.done })
	return e
}

func testZeroCopy(t *testing.T, auto, fallback bool) {
	k := newFakeKernel(t, 4, testBufSize)
	if fallback {
		k.zcFallback[2] = true
	}
	startZeroCopyEngine(t, k, auto)
	if k.bufTable != 4 {
		t.Fatalf("buffer table of %d slots, want 4", k.bufTable)
	}
	k.inject(2, fkReq{op: uapi.UBLK_IO_OP_WRITE, sector: 8, nr: 16, flags: uint32(FlagFUA), id: 1})
	k.inject(2, fkReq{op: uapi.UBLK_IO_OP_READ, sector: 8, nr: 16, id: 2})
	k.inject(3, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 3})
	k.inject(3, fkReq{op: uapi.UBLK_IO_OP_DISCARD, sector: 64, nr: 1 << 23, id: 4})
	k.inject(3, fkReq{op: uapi.UBLK_IO_OP_WRITE_ZEROES, sector: 64, nr: 8, id: 5})
	c := k.waitCommits(5, 5*time.Second)
	want := map[int]int32{1: 8192, 2: 8192, 3: 0, 4: 0, 5: 0}
	for _, x := range c {
		if x.result != want[x.id] {
			t.Errorf("request %d result %d, want %d", x.id, x.result, want[x.id])
		}
		if x.addr != 0 {
			t.Errorf("request %d committed buffer address %#x; zero copy has none", x.id, x.addr)
		}
	}
	k.mu.Lock()
	ops := append([]fkFileOp(nil), k.fileOps...)
	k.mu.Unlock()
	var sawFUA, sawRead, sawPunch, sawZero bool
	for _, op := range ops {
		switch op.opcode {
		case uring.IORING_OP_WRITE_FIXED:
			if op.off != 1<<20+8*512 || op.length != 8192 || op.bufIndex != 2 {
				t.Errorf("WRITE_FIXED %+v", op)
			}
			sawFUA = op.rwFlags&unix.RWF_DSYNC != 0
		case uring.IORING_OP_READ_FIXED:
			sawRead = true
		case uring.IORING_OP_FALLOCATE:
			sawPunch = sawPunch || (op.length == 1<<32 && op.mode&unix.FALLOC_FL_PUNCH_HOLE != 0)
			sawZero = sawZero || (op.length == 4096 && op.mode&unix.FALLOC_FL_ZERO_RANGE != 0)
		}
	}
	if !sawFUA || !sawRead || !sawPunch || !sawZero {
		t.Errorf("file ops %+v: fua=%v read=%v punch=%v zero=%v", ops, sawFUA, sawRead, sawPunch, sawZero)
	}
}

func TestEngineZeroCopyAuto(t *testing.T)         { testZeroCopy(t, true, false) }
func TestEngineZeroCopyAutoFallback(t *testing.T) { testZeroCopy(t, true, true) }
func TestEngineZeroCopyManual(t *testing.T)       { testZeroCopy(t, false, false) }

// TestEngineZeroCopyShortReadFails: outside copy mode the kernel completes the
// whole request on any non-negative result, so a short read must fail.
func TestEngineZeroCopyShortReadFails(t *testing.T) {
	k := newFakeKernel(t, 1, testBufSize)
	k.fileShortBy = 512
	startZeroCopyEngine(t, k, true)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 8, id: 1})
	if c := k.waitCommits(1, 5*time.Second); c[0].result != -int32(syscall.EIO) {
		t.Fatalf("short zero-copy read committed %d, want -EIO", c[0].result)
	}
}

func startBatchEngine(t *testing.T, k *fakeKernel, h Handler, inline bool) *engine {
	t.Helper()
	e := newEngine(engineConfig{
		tagLo: 0, tagHi: k.depth, charFd: k.ufile,
		desc: unsafe.Pointer(&k.desc[0]), descStride: 24,
		bufs: unsafe.Pointer(&k.bufs[0]), bufSize: testBufSize,
		userCopy: k.ufile >= 0, batch: true,
		handler: h, inline: inline, waitInterval: 20 * time.Millisecond,
		newRing: func(uint32) (ring, error) { return k, nil },
	})
	if err := e.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { e.abandon(); <-e.done })
	return e
}

func testBatch(t *testing.T, inline, userCopy bool, commitLimit int) {
	k := newFakeKernel(t, 8, testBufSize)
	k.commitLimit = commitLimit
	if userCopy {
		f, err := os.CreateTemp(t.TempDir(), "ublkc")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		k.ufile = int(f.Fd())
	}
	b := &memBackend{data: make([]byte, 1<<20)}
	e := startBatchEngine(t, k, BackendHandler(b, nil), inline)
	n := 0
	for round := 0; round < 3; round++ {
		for tag := 0; tag < 8; tag++ {
			w := pattern(4096, byte(tag+round))
			off := uint64(tag*16+round*2) * 8
			k.inject(tag, fkReq{op: uapi.UBLK_IO_OP_WRITE, sector: off, nr: 8, data: w, id: n})
			k.inject(tag, fkReq{op: uapi.UBLK_IO_OP_READ, sector: off, nr: 8, id: n + 1})
			n += 2
		}
	}
	c := k.waitCommits(n, 10*time.Second)
	for _, x := range c {
		if x.result != 4096 {
			t.Fatalf("request %d result %d, want 4096", x.id, x.result)
		}
		if x.id%2 == 1 && len(x.data) != 4096 {
			t.Fatalf("read %d returned %d bytes", x.id, len(x.data))
		}
	}
	k.stop()
	waitDone(t, e)
	if e.err != nil {
		t.Fatalf("batch engine stopped with %v", e.err)
	}
}

func TestEngineBatchCopy(t *testing.T)           { testBatch(t, false, false, 0) }
func TestEngineBatchCopyInline(t *testing.T)     { testBatch(t, true, false, 0) }
func TestEngineBatchUserCopy(t *testing.T)       { testBatch(t, false, true, 0) }
func TestEngineBatchPartialCommits(t *testing.T) { testBatch(t, false, false, 1) }

// TestEngineSharedMemory: a request flagged UBLK_IO_F_SHMEM_ZC gets Data
// pointing into the registered region at the descriptor's offset, and one
// outside every region fails.
func TestEngineSharedMemory(t *testing.T) {
	k := newFakeKernel(t, 2, testBufSize)
	region := make([]byte, 1<<20)
	copy(region[8192:], pattern(4096, 7))
	shm := &SharedMemory{}
	shm.Add(3, region)
	var gotData []byte
	h := HandlerFunc(func(r *Request) {
		if r.Flags&FlagSharedMemory != 0 && r.Op == OpWrite {
			gotData = r.Data
		}
		if r.Op == OpRead && r.Flags&FlagSharedMemory != 0 {
			copy(r.Data, pattern(len(r.Data), 9)) // "read" straight into the region
		}
		r.Complete(nil)
	})
	e := newEngine(engineConfig{
		tagLo: 0, tagHi: 2, charFd: -1, desc: unsafe.Pointer(&k.desc[0]), descStride: 24,
		bufs: unsafe.Pointer(&k.bufs[0]), bufSize: testBufSize, shmem: shm,
		handler: h, inline: true, waitInterval: 20 * time.Millisecond,
		newRing: func(uint32) (ring, error) { return k, nil },
	})
	if err := e.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.abandon(); <-e.done })
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_WRITE, nr: 8, shm: 3<<32 | 8192, id: 1})
	k.inject(1, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 8, shm: 3<<32 | 65536, id: 2})
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_WRITE, nr: 8, shm: 9<<32 | 0, id: 3})
	c := k.waitCommits(3, 5*time.Second)
	byID := map[int]int32{}
	for _, x := range c {
		byID[x.id] = x.result
	}
	if byID[1] != 4096 || byID[2] != 4096 || byID[3] != -int32(syscall.EIO) {
		t.Fatalf("results %v", byID)
	}
	if &gotData[0] != &region[8192] {
		t.Fatalf("shared-memory write's Data does not alias the registered region")
	}
	if !bytes.Equal(region[65536:65536+4096], pattern(4096, 9)) {
		t.Fatalf("shared-memory read did not land in the region")
	}
}

// TestEngineZeroCopyWriteZeroesFallsBackToPunch: on a filesystem without
// FALLOC_FL_ZERO_RANGE (tmpfs) write-zeroes is retried as a hole punch.
func TestEngineZeroCopyWriteZeroesFallsBackToPunch(t *testing.T) {
	k := newFakeKernel(t, 1, testBufSize)
	k.noZeroRange = true
	startZeroCopyEngine(t, k, true)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_WRITE_ZEROES, sector: 8, nr: 8, id: 1})
	if c := k.waitCommits(1, 5*time.Second); c[0].result != 0 {
		t.Fatalf("write-zeroes committed %d, want 0 after the punch fallback", c[0].result)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	last := k.fileOps[len(k.fileOps)-1]
	if last.mode&unix.FALLOC_FL_PUNCH_HOLE == 0 {
		t.Fatalf("last file op %+v is not a hole punch", last)
	}
}
