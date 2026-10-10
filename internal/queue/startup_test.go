package queue

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestReviewPoolStartupFailureClosesUnstartedWorkers(t *testing.T) {
	testQueueStartupFailure(t, dispatchCase{name: "pool", dispatch: DispatchPool})
}

func TestQueueStartupFailureClosesAutoDispatch(t *testing.T) {
	for _, mode := range dispatchCases {
		if mode.dispatch == DispatchAuto || mode.dispatch == DispatchAdaptive {
			t.Run(mode.name, func(t *testing.T) { testQueueStartupFailure(t, mode) })
		}
	}
}

func testQueueStartupFailure(t *testing.T, mode dispatchCase) {
	t.Helper()
	for failAt, name := range []string{"first-engine", "second-engine", "last-engine"} {
		t.Run(name, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "descriptors")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = file.Close() })
			if err := file.Truncate(int64(os.Getpagesize())); err != nil {
				t.Fatal(err)
			}
			calls := 0
			injected := errors.New("fake io_uring setup failure")
			q, err := NewQueue(QueueConfig{
				NumQueues: 1, Capacity: 8192, LogicalBlockSize: 512,
				Depth: 3, Threads: 3, MaxIOSize: 4096,
				CharFd: int(file.Fd()), ZeroCopyFile: -1,
				Handler:  mode.handler(RequestHandlerFunc(func(h RequestHandle) { _ = h.Complete(nil) })),
				Dispatch: mode.dispatch,
				newRing: func(uint32) (ring, error) {
					index := calls
					calls++
					if index == failAt {
						return nil, injected
					}
					return newFakeKernel(t, 3, 4096), nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			all := append([]*engine(nil), q.engines...)
			// Bound the reproducer even if startup strands an unstarted pool.
			t.Cleanup(func() {
				for _, e := range all {
					e.stopDispatch()
				}
			})
			if err := q.Start(); !errors.Is(err, injected) {
				t.Fatalf("Start: %v", err)
			}
			q.Abandon()
			// Allow the engine's one-second kernel wait and scheduler overhead.
			if !q.Wait(5 * time.Second) {
				t.Fatal("started engines did not drain")
			}
			waitDone(t, all[failAt])
			if err := q.Close(); err != nil {
				t.Fatal(err)
			}
			if err := q.Close(); err != nil {
				t.Fatalf("repeated Close: %v", err)
			}
			for i, e := range all {
				if mode.dispatch == DispatchPool && !e.workClosed {
					t.Errorf("engine %d: pool channel still open after Start failed and Close succeeded", i)
				}
				if mode.dispatch != DispatchPool && e.work != nil {
					t.Errorf("engine %d: automatic dispatch allocated pool workers", i)
				}
			}
		})
	}
}
