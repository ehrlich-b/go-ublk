package queue

import (
	"bytes"
	"errors"
	"syscall"
	"testing"
)

type heldBackend struct {
	entered, release chan struct{}
	panics           bool
}

func (b *heldBackend) ReadAt(p []byte, _ int64) (int, error) {
	close(b.entered)
	<-b.release
	if b.panics {
		panic("backend failed")
	}
	clear(p)
	return len(p), nil
}
func (b *heldBackend) WriteAt(p []byte, off int64) (int, error) { return b.ReadAt(p, off) }
func (*heldBackend) Flush() error                               { return nil }
func (*heldBackend) Close() error                               { return nil }
func (*heldBackend) Size() int64                                { return 4096 }

// A fused adapter claim must exclude both stale handles and competing current
// handles for the whole backend call. Panic recovery must still publish once.
func TestBackendOwnsBuffersThroughCompletion(t *testing.T) {
	for _, panics := range []bool{false, true} {
		name := "success"
		if panics {
			name = "panic"
		}
		t.Run(name, func(t *testing.T) {
			b := &heldBackend{entered: make(chan struct{}), release: make(chan struct{}), panics: panics}
			e := newEngine(engineConfig{tagHi: 1, handler: BackendHandler(b, nil)})
			r := &e.reqs[0]
			if err := r.state.Begin(); err != nil {
				t.Fatal(err)
			}
			r.state.Store(reqAsync)
			old := r.Handle()
			if err := old.Complete(nil); err != nil {
				t.Fatal(err)
			}
			e.head.Store(nil)
			if err := r.state.Begin(); err != nil {
				t.Fatal(err)
			}
			r.Op, r.Length = OpRead, 512
			r.Data, r.result = bytes.Repeat([]byte{0xcd}, 512), 0
			r.state.Store(reqAsync)
			current := r.Handle()
			done := make(chan struct{})
			go func() { defer close(done); e.call(r) }()
			<-b.entered
			// Always unblock the backend even when a rejection assertion fails.
			released := false
			defer func() {
				if !released {
					close(b.release)
					<-done
				}
			}()
			for _, handle := range []RequestHandle{old, current} {
				want := ErrStaleRequest
				if handle.generation == current.generation {
					want = ErrRequestCompleted
				}
				if err := handle.Complete(syscall.ENOSPC); !errors.Is(err, want) {
					t.Fatal(err)
				}
				if err := handle.WithBuffers(func(data, _, _ []byte) error { clear(data); t.Error("claimed buffers accessed"); return nil }); !errors.Is(err, want) {
					t.Fatal(err)
				}
			}
			if r.result != 0 || !bytes.Equal(r.Data, bytes.Repeat([]byte{0xcd}, 512)) || e.head.Load() != nil {
				t.Fatal("rejected completion changed backend-owned delivery")
			}
			close(b.release)
			released = true
			<-done
			want := int32(512)
			if panics {
				want = -int32(syscall.EIO)
			}
			if r.result != want || r.state.Load() != reqQueued || e.head.Load() != r || r.next != nil {
				t.Fatal("backend did not publish exactly one complete result")
			}
			if err := current.Complete(nil); !errors.Is(err, ErrRequestCompleted) || r.result != want || r.next != nil {
				t.Fatal("duplicate completion changed the backend result")
			}
		})
	}
}
