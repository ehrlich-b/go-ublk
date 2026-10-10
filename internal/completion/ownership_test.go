package completion

import (
	"bytes"
	"sync/atomic"
	"testing"
)

func TestCompletionClaimsBeforeCopyAndRejectsConcurrentDuplicate(t *testing.T) {
	for _, initial := range []uint32{Dispatching, Async} {
		var state atomic.Uint32
		state.Store(initial)
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		var copies, enqueues atomic.Int32
		payload := bytes.Repeat([]byte{0xcd}, 16)
		go func() {
			done <- Finish(&state, func() {
				close(entered)
				<-release
				copies.Add(1)
				copy(payload, bytes.Repeat([]byte{0x31}, 16))
			}, func() { enqueues.Add(1) })
		}()
		<-entered
		if err := Finish(&state, func() { copies.Add(1); clear(payload) }, func() { enqueues.Add(1) }); err == nil {
			t.Fatal("duplicate completion claimed a request while its first copy was staging")
		}
		if copies.Load() != 0 || enqueues.Load() != 0 || !bytes.Equal(payload, bytes.Repeat([]byte{0xcd}, 16)) {
			t.Fatal("losing completion changed the poisoned buffer or publication ledger")
		}
		if initial == Dispatching && ReturnInline(&state) {
			t.Fatal("engine committed while the first copy was still staging")
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if copies.Load() != 1 || enqueues.Load() != 1 || state.Load() != Queued || !bytes.Equal(payload, bytes.Repeat([]byte{0x31}, 16)) {
			t.Fatal("accepted completion was not staged and enqueued exactly once")
		}
	}
}

func TestInlineCompletionPublishesOnlyAfterStaging(t *testing.T) {
	var state atomic.Uint32
	state.Store(Dispatching)
	result, enqueued := 0, false
	if err := Finish(&state, func() { result = 512 }, func() { enqueued = true }); err != nil {
		t.Fatal(err)
	}
	if !ReturnInline(&state) || enqueued || result != 512 {
		t.Fatal("inline result was not ready exactly once")
	}
	if err := Finish(&state, func() { result = -1 }, func() { enqueued = true }); err == nil || result != 512 || enqueued {
		t.Fatal("duplicate inline completion overwrote the accepted result")
	}
}
