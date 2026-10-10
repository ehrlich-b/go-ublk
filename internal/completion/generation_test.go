package completion

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestStaleCompletionAndBufferAccessAfterReuse(t *testing.T) {
	for _, phase := range []uint32{Dispatching, Async} {
		var o Ownership
		_ = o.Begin()
		o.Store(phase)
		old := o.Generation()
		if err := FinishGeneration(&o, old, func() {}, func() {}); err != nil {
			t.Fatal(err)
		}
		o.Store(Idle)
		_ = o.Begin()
		o.Store(phase)
		payload, metadata := bytes.Repeat([]byte{0xcd}, 16), bytes.Repeat([]byte{0xd7}, 8)
		result, copies, enqueues := int32(0), 0, 0
		for _, reject := range []func() error{
			func() error {
				return FinishGeneration(&o, old, func() { result = 512; copies++ }, func() { enqueues++ })
			},
			func() error { return Access(&o, old, func() error { clear(payload); clear(metadata); return nil }) },
		} {
			if err := reject(); !errors.Is(err, ErrStaleRequest) {
				t.Fatalf("stale generation accepted: %v", err)
			}
		}
		if result != 0 || copies != 0 || enqueues != 0 || !bytes.Equal(payload, bytes.Repeat([]byte{0xcd}, 16)) ||
			!bytes.Equal(metadata, bytes.Repeat([]byte{0xd7}, 8)) {
			t.Fatal("stale handle changed new delivery")
		}
		current := o.Generation()
		if err := Access(&o, current, func() error { clear(payload); return nil }); err != nil {
			t.Fatal(err)
		}
		if err := FinishGeneration(&o, current, func() { result = 512; copies++ }, func() { enqueues++ }); err != nil {
			t.Fatal(err)
		}
		if result != 512 || copies != 1 {
			t.Fatal("current generation lost completion")
		}
	}
}

func TestBufferAccessExcludesCompletionAndSurvivesInlineReturn(t *testing.T) {
	var o Ownership
	_ = o.Begin()
	o.Store(Dispatching)
	generation := o.Generation()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { done <- Access(&o, generation, func() error { close(entered); <-release; return nil }) }()
	<-entered
	if err := FinishGeneration(&o, generation, func() { t.Error("completion staged during borrow") }, func() {}); !errors.Is(err, ErrRequestBusy) {
		t.Fatal(err)
	}
	if err := Access(&o, generation, func() error { t.Error("nested access accepted"); return nil }); !errors.Is(err, ErrRequestBusy) {
		t.Fatal(err)
	}
	if ReturnInline(&o) {
		t.Fatal("committed during borrow")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if o.Load() != Async {
		t.Fatal("inline return lost async ownership")
	}
	var staged, queued int
	if err := FinishGeneration(&o, generation, func() { staged++ }, func() { queued++ }); err != nil || staged != 1 || queued != 1 {
		t.Fatalf("retry: %v, %d/%d", err, staged, queued)
	}
}

func TestGenerationExhaustionAndZeroHandle(t *testing.T) {
	var o Ownership
	if err := FinishGeneration(&o, 0, func() { t.Error("zero staged") }, func() {}); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	if err := Access(&o, 0, func() error { t.Error("zero accessed"); return nil }); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	o.word.Store((MaxGeneration - 1) << stateBits)
	if err := o.Begin(); err != nil {
		t.Fatal(err)
	}
	o.Store(Async)
	last := o.word.Load()
	if err := o.Begin(); !errors.Is(err, ErrGenerationExhausted) || o.word.Load() != last {
		t.Fatal("generation wrapped or changed on exhaustion")
	}
}

func TestBeginPhaseRejectsRetiredHandlesBeforeFieldReset(t *testing.T) {
	for _, phase := range []uint32{Dispatching, Async} {
		var o Ownership
		if err := o.BeginPhase(Async); err != nil {
			t.Fatal(err)
		}
		old := o.Generation()
		if err := FinishGeneration(&o, old, func() {}, func() {}); err != nil {
			t.Fatal(err)
		}
		if err := o.BeginPhase(phase); err != nil {
			t.Fatal(err)
		}
		if o.Generation() != old+1 || o.Load() != phase {
			t.Fatal("wrong delivery identity or phase")
		}
		if err := Access(&o, old, func() error { t.Error("retired handle accessed recycled fields"); return nil }); !errors.Is(err, ErrStaleRequest) {
			t.Fatal(err)
		}
		if err := FinishGeneration(&o, old, func() { t.Error("retired handle staged a result") }, func() { t.Error("retired handle published") }); !errors.Is(err, ErrStaleRequest) {
			t.Fatal(err)
		}
		if err := FinishGeneration(&o, o.Generation(), func() {}, func() {}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBufferPanicReleasesOwnership(t *testing.T) {
	var o Ownership
	_ = o.Begin()
	o.Store(Dispatching)
	func() {
		defer func() { _ = recover() }()
		_ = Access(&o, o.Generation(), func() error { panic("backend failure") })
	}()
	if err := FinishGeneration(&o, o.Generation(), func() {}, func() {}); err != nil {
		t.Fatal(err)
	}
}

// Old handles repeatedly contend with ordinary tag reuse. Rejection must not
// touch fields reset by the owner; race instrumentation checks that boundary.
func TestStaleHandlesRaceWithReuse(t *testing.T) {
	var o Ownership
	_ = o.Begin()
	o.Store(Async)
	old := o.Generation()
	_ = FinishGeneration(&o, old, func() {}, func() {})
	var effects atomic.Int32
	var payload [16]byte
	var result int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 1000 {
				if err := FinishGeneration(&o, old, func() { effects.Add(1); result = -28; clear(payload[:]) }, func() { effects.Add(1); result = -28; clear(payload[:]) }); !errors.Is(err, ErrStaleRequest) {
					t.Errorf("stale completion: %v", err)
					return
				}
				if err := Access(&o, old, func() error { effects.Add(1); clear(payload[:]); return nil }); !errors.Is(err, ErrStaleRequest) {
					t.Errorf("stale access: %v", err)
					return
				}
			}
		}()
	}
	_ = o.Begin()
	o.Store(Async)
	close(start)
	for range 1000 {
		_ = FinishGeneration(&o, o.Generation(), func() {}, func() {})
		_ = o.BeginPhase(Async)
		for i := range payload {
			payload[i] = 0xcd
		}
		result = 0
	}
	wg.Wait()
	if effects.Load() != 0 || result != 0 || !bytes.Equal(payload[:], bytes.Repeat([]byte{0xcd}, len(payload))) {
		t.Fatal("stale handle had effects")
	}
}
