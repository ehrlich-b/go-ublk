package completion

import (
	"testing"
)

// BenchmarkCompletion measures dispatch, completion claim and inline return,
// without kernel setup or callback allocation.
func BenchmarkCompletion(b *testing.B) {
	var state Ownership
	var result int
	stage := func() { result++ }
	enqueue := func() { b.Fatal("unexpected async publication") }
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := state.Begin(); err != nil {
			b.Fatal(err)
		}
		state.Store(Dispatching)
		if err := Finish(&state, stage, enqueue); err != nil || !ReturnInline(&state) {
			b.Fatal("completion failed", err)
		}
	}
	if result != b.N {
		b.Fatal("lost completion")
	}
}

func BenchmarkCompletionHandle(b *testing.B) {
	var state Ownership
	result := 0
	stage := func() { result++ }
	enqueue := func() { b.Fatal("unexpected async publication") }
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := state.Begin(); err != nil {
			b.Fatal(err)
		}
		state.Store(Dispatching)
		generation := state.Generation()
		if err := FinishGeneration(&state, generation, stage, enqueue); err != nil || !ReturnInline(&state) {
			b.Fatal("completion failed", err)
		}
	}
	if result != b.N {
		b.Fatal("lost completion")
	}
}

func TestGenerationHotPathDoesNotAllocate(t *testing.T) {
	var state Ownership
	stage := func() {}
	enqueue := func() { t.Fatal("unexpected async publication") }
	use := func() error { return nil }
	allocations := testing.AllocsPerRun(1000, func() {
		_ = state.Begin()
		state.Store(Dispatching)
		generation := state.Generation()
		if err := Access(&state, generation, use); err != nil {
			t.Fatal(err)
		}
		if err := FinishGeneration(&state, generation, stage, enqueue); err != nil || !ReturnInline(&state) {
			t.Fatal("completion failed", err)
		}
	})
	if allocations != 0 {
		t.Fatalf("generation/buffer/completion hot path allocated %g times", allocations)
	}
}
