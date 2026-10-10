package queue

import (
	"runtime"
	"sync/atomic"
	"testing"
)

// Isolate adapter and completion bookkeeping from memory copies and locking
// inside the backend. The VM fio protocol measures the complete data path.
type benchmarkBackend struct{}

func (benchmarkBackend) ReadAt(p []byte, _ int64) (int, error)  { return len(p), nil }
func (benchmarkBackend) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }
func (benchmarkBackend) Flush() error                           { return nil }
func (benchmarkBackend) Close() error                           { return nil }
func (benchmarkBackend) Size() int64                            { return 512 << 20 }

func BenchmarkBackendParallel(b *testing.B) {
	for _, phase := range []struct {
		name  string
		state uint32
	}{{"Inline", reqDispatching}, {"Async", reqAsync}} {
		for _, op := range []Op{OpRead, OpWrite} {
			b.Run(phase.name+"/"+op.String(), func(b *testing.B) {
				var requests [2][64]Request
				var engines [2]engine
				var data [4096]byte
				for q := range requests {
					for tag := range requests[q] {
						r := &requests[q][tag]
						r.e, r.Queue, r.Tag = &engines[q], uint16(q), uint16(tag)
						r.Op, r.Length, r.Data = op, int64(len(data)), data[:]
					}
				}
				h := BackendHandler(benchmarkBackend{}, nil)
				var workers atomic.Uint32
				b.ReportAllocs()
				b.RunParallel(func(pb *testing.PB) {
					id := int(workers.Add(1) - 1)
					stride := runtime.GOMAXPROCS(0)
					q, tag := 0, id
					for pb.Next() {
						r := &requests[q][tag]
						if err := r.state.BeginPhase(phase.state); err != nil {
							b.Fatal(err)
						}
						h.HandleRequest(r)
						if r.result != int32(len(data)) {
							b.Fatal("lost backend result")
						}
						// Model completion-list collection, without kernel setup.
						engines[q].head.Swap(nil)
						q ^= 1
						if q == 0 {
							tag += stride
							if tag >= 64 {
								tag = id
							}
						}
					}
				})
			})
		}
	}
}

// Include the default dispatch model: fresh handler goroutines, with up to
// depth-64 requests outstanding per queue. This catches stack-frame costs that
// disappear when a scalar benchmark reuses one goroutine forever.
func BenchmarkBackendGoroutines(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var e engine
		e.cfg.handler = BackendHandler(benchmarkBackend{}, nil)
		var requests [64]Request
		var data [4096]byte
		free := make(chan *Request, len(requests))
		for tag := range requests {
			r := &requests[tag]
			r.e, r.Tag, r.Op = &e, uint16(tag), OpRead
			r.Length, r.Data = int64(len(data)), data[:]
			free <- r
		}
		for pb.Next() {
			r := <-free
			if err := r.state.BeginPhase(reqAsync); err != nil {
				b.Fatal(err)
			}
			go func(r *Request) {
				e.call(r)
				if r.result != int32(len(data)) {
					panic("lost backend result")
				}
				free <- r
			}(r)
		}
		for range requests {
			<-free // join every handler before stopping the timer
		}
	})
}
