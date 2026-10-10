package queue

import (
	"os"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

func TestEngineAutoUserCopyKeepsGoroutineDispatch(t *testing.T) {
	for _, mode := range []dispatchCase{
		{dispatch: DispatchAuto, declared: true},
		{dispatch: DispatchAdaptive, declared: true},
	} {
		name := "auto"
		if mode.dispatch == DispatchAdaptive {
			name = "adaptive"
		}
		t.Run(name, func(t *testing.T) {
			k := newFakeKernel(t, 1, testBufSize)
			file, err := os.CreateTemp(t.TempDir(), "user-copy")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = file.Close() })
			k.ufile = int(file.Fd())
			phases := make(chan uint32, 1)
			startEngine(t, k, HandlerFunc(func(r *Request) {
				phases <- r.state.Load()
				r.Complete(nil)
			}), mode)
			k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 1})
			select {
			case phase := <-phases:
				if phase != reqAsync {
					t.Fatalf("user-copy phase=%d, want asynchronous", phase)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("user-copy handler was not dispatched")
			}
			k.waitCommits(1, 5*time.Second)
		})
	}
}

func TestEngineAutoDispatchPhases(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       DispatchMode
		declared   bool
		inline     bool
		requests   int
		controlCQE bool
		wantPhase  uint32
	}{
		{"auto-singleton", DispatchAuto, true, false, 1, false, reqDispatching},
		{"auto-crowded", DispatchAuto, true, false, 2, false, reqDispatching},
		{"auto-undeclared", DispatchAuto, false, false, 1, false, reqAsync},
		{"adaptive-singleton", DispatchAdaptive, true, false, 1, false, reqDispatching},
		{"adaptive-control-cqe", DispatchAdaptive, true, false, 1, true, reqDispatching},
		{"adaptive-crowded", DispatchAdaptive, true, false, 2, false, reqAsync},
		{"adaptive-undeclared", DispatchAdaptive, false, false, 1, false, reqAsync},
		{"inline-override", DispatchAdaptive, false, true, 2, false, reqDispatching},
	} {
		for _, layout := range []struct {
			name         string
			batch, merge bool
		}{
			{name: "copy"}, {name: "batch-split", batch: true},
			{name: "batch-merged", batch: true, merge: true},
		} {
			t.Run(tc.name+"/"+layout.name, func(t *testing.T) {
				phases := make(chan uint32, tc.requests)
				completed := make(chan struct{}, tc.requests)
				h := HandlerFunc(func(r *Request) {
					phases <- r.state.Load()
					r.Complete(nil)
					completed <- struct{}{}
				})
				mode := dispatchCase{dispatch: tc.mode, declared: tc.declared, inline: tc.inline}
				e, k := scheduledEngineMode(t, 2, h, layout.batch, mode)
				if tc.controlCQE {
					// A successful buffer-unregister CQE is control work, not a request.
					k.scheduleCompletions(uring.CQE{UserData: kindUnreg})
				}
				// Hold the fake lock to put every request in the same ready snapshot.
				k.mu.Lock()
				if layout.merge {
					k.fetchArmed = false // Accumulate tags for one multi-tag FETCH CQE.
				}
				for tag := 0; tag < tc.requests; tag++ {
					k.tags[tag].pending = append(k.tags[tag].pending, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: tag})
					k.deliver(tag)
				}
				if layout.merge {
					k.fetchArmed = true
					k.flushBatch()
				}
				k.mu.Unlock()
				e.drainCQEs()
				for range tc.requests {
					select {
					case phase := <-phases:
						if phase != tc.wantPhase {
							t.Errorf("phase=%d, want %d", phase, tc.wantPhase)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("handler was not dispatched")
					}
				}
				for range tc.requests {
					select {
					case <-completed:
					case <-time.After(5 * time.Second):
						t.Fatal("handler did not publish completion")
					}
				}
				e.drainCompletions()
				if e.cfg.batch {
					e.flushBatchCommits()
				}
				if _, err := k.Submit(); err != nil {
					t.Fatal(err)
				}
				reapScheduled(e, k)
				commits, violations := k.snapshot()
				if e.err != nil || len(commits) != tc.requests || len(violations) != 0 || e.handlers.Load() != 0 {
					t.Fatalf("commits=%d violations=%v handlers=%d error=%v",
						len(commits), violations, e.handlers.Load(), e.err)
				}
			})
		}
	}
}

func TestEngineAdaptiveSnapshotBounds(t *testing.T) {
	deliveries := make(chan RequestHandle, 1)
	phases := make(chan uint32, 1)
	h := HandlerFunc(func(r *Request) { phases <- r.state.Load(); deliveries <- r.Handle() })
	e, k := scheduledEngineMode(t, 1, h, false,
		dispatchCase{dispatch: DispatchAdaptive, declared: true})
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 1})
	// A larger CQ backlog must not hide requests outside the captured prefix.
	// Add control work beyond the cap and require a conservative async call.
	for range cap(e.cqeBatch) {
		k.scheduleCompletions(uring.CQE{UserData: kindUnreg})
	}
	before := k.CQReady()
	e.drainCQEs()
	if got := k.CQReady(); got != before-uint32(cap(e.cqeBatch)) {
		t.Fatalf("snapshot consumed an unbounded CQ prefix: remaining=%d before=%d", got, before)
	}
	select {
	case phase := <-phases:
		if phase != reqAsync {
			t.Fatalf("oversized snapshot phase=%d, want asynchronous", phase)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot did not dispatch its ready request")
	}
	if err := (<-deliveries).Complete(nil); err != nil {
		t.Fatal(err)
	}
	reapScheduled(e, k)
	if _, err := k.Submit(); err != nil {
		t.Fatal(err)
	}
	commits, violations := k.snapshot()
	if e.err != nil || len(commits) != 1 || len(violations) != 0 {
		t.Fatalf("commits=%d violations=%v error=%v", len(commits), violations, e.err)
	}
}

func TestEngineAdaptiveOutstandingHandlerUsesGoroutine(t *testing.T) {
	deliveries := make(chan RequestHandle, 2)
	phases := make(chan uint32, 2)
	h := HandlerFunc(func(r *Request) { phases <- r.state.Load(); deliveries <- r.Handle() })
	mode := dispatchCase{dispatch: DispatchAdaptive, declared: true}
	e, k := scheduledEngineMode(t, 2, h, false, mode)
	for tag, want := range []uint32{reqDispatching, reqAsync} {
		k.inject(tag, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: tag})
		e.drainCQEs()
		select {
		case phase := <-phases:
			if phase != want {
				t.Errorf("tag %d phase=%d, want %d", tag, phase, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("handler was not dispatched")
		}
	}
	for range 2 {
		if err := (<-deliveries).Complete(nil); err != nil {
			t.Fatal(err)
		}
	}
	e.drainCompletions()
	if _, err := k.Submit(); err != nil {
		t.Fatal(err)
	}
	if e.handlers.Load() != 0 || e.err != nil {
		t.Fatalf("handlers=%d error=%v", e.handlers.Load(), e.err)
	}
}

func TestEngineAdaptiveBlockingSingletonDoesNotStallArrival(t *testing.T) {
	for _, batch := range []bool{false, true} {
		name := "copy"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			k := newFakeKernel(t, 2, testBufSize)
			entered, release, ready := make(chan struct{}), make(chan struct{}), make(chan struct{})
			h := HandlerFunc(func(r *Request) {
				if r.Tag == 0 {
					close(entered)
					<-release
				} else {
					close(ready)
				}
				r.Complete(nil)
			})
			mode := dispatchCase{dispatch: DispatchAdaptive}
			var e *engine
			if batch {
				e = startBatchEngine(t, k, h, mode)
			} else {
				e = startEngine(t, k, h, mode)
			}
			// Registered after engine cleanup, so release runs before drain waits.
			t.Cleanup(func() { close(release) })
			k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 0})
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("blocking singleton did not start")
			}
			k.inject(1, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 1})
			// The dispatch policy adds zero inline-handler wait. This generous
			// 250ms watchdog allows scheduler/fake-ring overhead, not a handler budget.
			select {
			case <-ready:
			case <-time.After(250 * time.Millisecond):
				t.Fatal("later arrival stalled behind the blocked singleton")
			}
			commits := k.waitCommits(1, 250*time.Millisecond)
			if commits[0].id != 1 || e.handlers.Load() != 1 {
				t.Fatalf("blocking singleton prevented independent commit: %+v", commits)
			}
		})
	}
}

type declaredBackend struct {
	benchmarkBackend
	nonBlocking bool
}

func (b declaredBackend) NonBlocking() bool { return b.nonBlocking }

type testObserver struct{ nonBlocking bool }

func (o testObserver) NonBlocking() bool                 { return o.nonBlocking }
func (testObserver) ObserveRead(uint64, uint64, bool)    {}
func (testObserver) ObserveWrite(uint64, uint64, bool)   {}
func (testObserver) ObserveDiscard(uint64, uint64, bool) {}
func (testObserver) ObserveFlush(uint64, bool)           {}
func (testObserver) ObserveQueueDepth(uint32)            {}

type undeclaredBackend struct{ interfaces.Backend }
type undeclaredObserver struct{ interfaces.Observer }

func TestBackendHandlerNonBlockingDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		backend  interfaces.Backend
		observer interfaces.Observer
		want     bool
	}{
		{"undeclared", undeclaredBackend{benchmarkBackend{}}, nil, false},
		{"declined", declaredBackend{nonBlocking: false}, nil, false},
		{"declared", declaredBackend{nonBlocking: true}, nil, true},
		{"observer-declared", benchmarkBackend{}, testObserver{true}, true},
		{"observer-declined", benchmarkBackend{}, testObserver{false}, false},
		{"observer-undeclared", benchmarkBackend{}, undeclaredObserver{testObserver{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := BackendHandler(tc.backend, tc.observer).(interfaces.NonBlockingDeclarer)
			if got := h.NonBlocking(); got != tc.want {
				t.Fatalf("NonBlocking()=%v, want %v", got, tc.want)
			}
		})
	}
}
