package completion

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// Match the request array's spacing without importing the Linux-only queue
// package. Adjacent real request state words do not share a cache line.
type benchmarkRequest struct {
	state Ownership
	_     [160]byte
}

// RunParallel interleaves independent tags in two depth-64 queues. At cpu=2
// there are two queue workers; larger cpu counts split each queue's tags among
// more workers. BorrowThenComplete models the adapter in a5af4a4, which claims
// and releases buffers separately from claiming completion.
func BenchmarkCompletionParallel(b *testing.B) {
	for _, phase := range []struct {
		name  string
		state uint32
	}{{"Inline", Dispatching}, {"Async", Async}} {
		for _, borrow := range []bool{false, true} {
			name := "Complete"
			if borrow {
				name = "BorrowThenComplete"
			}
			b.Run(phase.name+"/"+name, func(b *testing.B) {
				var requests [2][64]benchmarkRequest
				var workers atomic.Uint32
				b.ReportAllocs()
				b.RunParallel(func(pb *testing.PB) {
					id := int(workers.Add(1) - 1)
					stride := runtime.GOMAXPROCS(0)
					result, queued := 0, 0
					stage := func() { result++ }
					enqueue := func() { queued++ }
					use := func() error { return nil }
					q, tag := 0, id
					for pb.Next() {
						s := &requests[q][tag].state
						if err := s.BeginPhase(phase.state); err != nil {
							b.Fatal(err)
						}
						generation := s.Generation()
						if borrow {
							if err := Access(s, generation, use); err != nil {
								b.Fatal(err)
							}
						}
						if err := FinishGeneration(s, generation, stage, enqueue); err != nil {
							b.Fatal(err)
						}
						if phase.state == Dispatching && !ReturnInline(s) {
							b.Fatal("missing inline result")
						}
						q ^= 1
						if q == 0 {
							tag += stride
							if tag >= 64 {
								tag = id
							}
						}
					}
					if phase.state == Async && result != queued {
						b.Fatal("lost async publication")
					}
				})
			})
		}
	}
}

// Queue goroutines begin deliveries while RunParallel workers complete them
// on other cores. Each queue recycles tags only after completion publication;
// channel handoff models the queue/handler boundary without Linux syscalls.
func BenchmarkCompletionHandoff(b *testing.B) {
	var requests [2][64]benchmarkRequest
	type delivery struct {
		queue, tag int
		generation uint64
	}
	ready := make(chan delivery, 128)
	var free [2]chan int
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for q := range free {
		free[q] = make(chan int, 64)
		for tag := range 64 {
			free[q] <- tag
		}
		wg.Add(1)
		go func(q int) {
			defer wg.Done()
			for {
				var tag int
				select {
				case tag = <-free[q]:
				case <-stop:
					return
				}
				s := &requests[q][tag].state
				if err := s.BeginPhase(Async); err != nil {
					panic(err)
				}
				select {
				case ready <- delivery{q, tag, s.Generation()}:
				case <-stop:
					return
				}
			}
		}(q)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			d := <-ready
			if err := FinishGeneration(&requests[d.queue][d.tag].state, d.generation,
				func() {}, func() { free[d.queue] <- d.tag }); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.StopTimer()
	close(stop)
	wg.Wait()
}
