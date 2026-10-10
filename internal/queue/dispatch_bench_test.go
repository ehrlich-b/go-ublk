package queue

import (
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

const dispatchBenchDepth = 64
const dispatchBenchBytes = 4096

// dispatchRing is an allocation-free fake ring for a depth-64 copy queue.
// Each step delivers one 4 KiB read and consumes its COMMIT_AND_FETCH. Unlike
// fakeKernel's correctness recorder, it neither copies data nor grows logs.
// The owner runs synchronously; no kernel wait or eventfd syscall is measured.
type dispatchRing struct {
	sqe     uring.SQE
	cqe     uring.CQE
	pending bool
	ready   bool
	commits int
	tag     uint16
}

func (r *dispatchRing) GetSQE() *uring.SQE {
	if r.pending {
		return nil
	}
	r.sqe = uring.SQE{}
	r.pending = true
	return &r.sqe
}

func (r *dispatchRing) Submit() (int, error) {
	if !r.pending {
		return 0, nil
	}
	cmd := (*uapi.UblksrvIOCmd)(unsafe.Pointer(r.sqe.Cmd()))
	if r.sqe.Opcode != uring.IORING_OP_URING_CMD ||
		r.sqe.CmdOp() != uapi.UblkIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ) ||
		cmd.Tag != r.tag || cmd.Result != dispatchBenchBytes {
		panic("dispatch benchmark committed an invalid request")
	}
	r.commits++
	r.pending = false
	return 1, nil
}

func (r *dispatchRing) SubmitAndWait(uint32, time.Duration) (int, error) {
	panic("dispatch benchmark must not wait in the kernel")
}

func (r *dispatchRing) PeekCQE() *uring.CQE {
	if r.ready {
		return &r.cqe
	}
	return nil
}

func (r *dispatchRing) CQAdvance(uint32) { r.ready = false }
func (r *dispatchRing) Close() error     { return nil }

func (r *dispatchRing) CQReady() uint32 {
	if r.ready {
		return 1
	}
	return 0
}

func (r *dispatchRing) step(e *engine, tag int) {
	r.tag = uint16(tag)
	r.cqe = uring.CQE{UserData: kindIO | uint64(tag), Res: uapi.UBLK_IO_RES_OK}
	r.ready = true
	e.drainCQEs()
	if e.head.Load() != nil || e.handlers.Load() != 0 {
		for e.head.Load() == nil {
			runtime.Gosched()
		}
		e.drainCompletions()
	}
	if _, err := r.Submit(); err != nil {
		panic(err)
	}
}

func newDispatchBenchmark(tb testing.TB, inline bool, mode DispatchMode) (*engine, *dispatchRing) {
	tb.Helper()
	desc := make([]uapi.UblksrvIODesc, dispatchBenchDepth)
	data := make([]byte, dispatchBenchDepth*dispatchBenchBytes)
	for i := range desc {
		desc[i].OpFlags = uapi.UBLK_IO_OP_READ
		desc[i].NrSectors = dispatchBenchBytes / uapi.SectorSize
	}
	e := newEngine(engineConfig{
		capacity: func() int64 { return 512 << 20 }, logicalBlockSize: 512,
		tagHi: dispatchBenchDepth, charFd: -1,
		desc: unsafe.Pointer(&desc[0]), descStride: unsafe.Sizeof(desc[0]),
		bufs: unsafe.Pointer(&data[0]), bufSize: dispatchBenchBytes,
		handler: BackendHandler(benchmarkBackend{}, nil), inline: inline, dispatch: mode,
	})
	ring := &dispatchRing{}
	e.ring = ring
	tb.Cleanup(e.teardown)
	// Warm every worker's handler stack before measuring steady-state dispatch.
	for i := 0; i < 4*dispatchBenchDepth; i++ {
		ring.step(e, i%dispatchBenchDepth)
	}
	return e, ring
}

func benchmarkDispatch(b *testing.B, inline bool, mode DispatchMode) {
	e, ring := newDispatchBenchmark(b, inline, mode)
	before := ring.commits
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ring.step(e, i%dispatchBenchDepth)
	}
	b.StopTimer()
	if ring.commits-before != b.N || e.handlers.Load() != 0 || e.err != nil {
		b.Fatalf("dispatch lost a completion: commits=%d handlers=%d error=%v",
			ring.commits-before, e.handlers.Load(), e.err)
	}
}

func BenchmarkDispatchInline(b *testing.B)    { benchmarkDispatch(b, true, DispatchGoroutine) }
func BenchmarkDispatchGoroutine(b *testing.B) { benchmarkDispatch(b, false, DispatchGoroutine) }
func BenchmarkDispatchPool(b *testing.B)      { benchmarkDispatch(b, false, DispatchPool) }
func BenchmarkDispatchAuto(b *testing.B)      { benchmarkDispatch(b, false, DispatchAuto) }
func BenchmarkDispatchAdaptive(b *testing.B)  { benchmarkDispatch(b, false, DispatchAdaptive) }

func TestDispatchHotPathDoesNotAllocate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inline bool
		mode   DispatchMode
	}{
		{"inline", true, DispatchGoroutine}, {"pool", false, DispatchPool},
		{"auto", false, DispatchAuto}, {"adaptive", false, DispatchAdaptive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, ring := newDispatchBenchmark(t, tc.inline, tc.mode)
			tag := 0
			allocs := testing.AllocsPerRun(1000, func() {
				ring.step(e, tag)
				tag = (tag + 1) % dispatchBenchDepth
			})
			if allocs != 0 {
				t.Fatalf("dispatch allocates %.2f objects per I/O, want zero", allocs)
			}
		})
	}
}
