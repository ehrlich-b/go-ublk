package queue

import (
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

func TestEngineDispatchConcurrentBlockingHandlers(t *testing.T) {
	for _, mode := range dispatchCases[:2] {
		t.Run(mode.name, func(t *testing.T) {
			const depth = 8
			k := newFakeKernel(t, depth, testBufSize)
			entered := make(chan struct{}, depth)
			release := make(chan struct{})
			e := startEngine(t, k, RequestHandlerFunc(func(h RequestHandle) {
				entered <- struct{}{}
				<-release
				if err := h.Complete(nil); err != nil {
					panic(err)
				}
			}), mode)
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			for tag := 0; tag < depth; tag++ {
				k.inject(tag, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: tag})
			}
			for range depth {
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("dispatch did not provide queue-depth concurrency")
				}
			}
			e.abandon()
			if e.handlers.Load() != depth {
				t.Fatalf("held handlers=%d, want %d", e.handlers.Load(), depth)
			}
			select {
			case <-e.done:
				t.Fatal("abandon discarded outstanding handlers")
			default:
			}
			close(release)
			waitDone(t, e)
			commits, violations := k.snapshot()
			if len(commits) != depth || len(violations) != 0 || e.handlers.Load() != 0 {
				t.Fatalf("drain: commits=%d violations=%v handlers=%d", len(commits), violations, e.handlers.Load())
			}
		})
	}
}

func TestEngineDispatchTeardownAfterCompletion(t *testing.T) {
	for _, mode := range dispatchCases[:2] {
		t.Run(mode.name, func(t *testing.T) {
			k := newFakeKernel(t, 1, testBufSize)
			release, returned := make(chan struct{}), make(chan struct{})
			e := startEngine(t, k, RequestHandlerFunc(func(h RequestHandle) {
				defer close(returned)
				if err := h.Complete(nil); err != nil {
					panic(err)
				}
				<-release // Completed callbacks may still be unwinding at teardown.
			}), mode)
			t.Cleanup(func() { close(release); <-returned })
			k.inject(0, fkReq{op: uapi.UBLK_IO_OP_FLUSH, id: 1})
			k.waitCommits(1, 5*time.Second)
			e.abandon()
			waitDone(t, e)
			if e.handlers.Load() != 0 || !k.closed {
				t.Fatal("completion did not release the engine for teardown")
			}
		})
	}
}
