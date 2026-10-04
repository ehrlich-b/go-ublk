package uring

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// The submission and completion paths must not allocate: they run once per
// I/O in the queue engine.
func TestHotPathDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector's instrumentation allocates")
	}
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	buf := offHeap(t, 4096)
	_, fd := tempFileFd(t)
	core := testing.AllocsPerRun(1000, func() {
		sqe := r.GetSQE()
		PrepNop(sqe)
		sqe.UserData = 1
		sqe = r.GetSQE()
		PrepRead(sqe, fd, buf, 0)
		sqe.UserData = 2
		if _, err := r.SubmitAndWait(2, time.Second); err != nil {
			t.Fatal(err)
		}
		var batch [2]*CQE
		r.CQAdvance(uint32(r.PeekBatchCQE(batch[:])))
		for r.PeekCQE() != nil {
			r.CQESeen()
		}
	})
	if core != 0 {
		t.Errorf("IoUring submit/wait/reap: %v allocations per run, want 0", core)
	}
	ring, err := NewMinimalRing(8, int32(closedPipe(t).Fd()))
	if err != nil {
		t.Fatalf("NewMinimalRing: %v", err)
	}
	defer ring.Close()
	ioCmd := &uapi.UblksrvIOCmd{}
	legacy := testing.AllocsPerRun(1000, func() {
		if err := ring.PrepareIOCmd(0, ioCmd, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := ring.FlushSubmissions(); err != nil {
			t.Fatal(err)
		}
		if _, err := ring.WaitForCompletion(0); err != nil {
			t.Fatal(err)
		}
	})
	if legacy != 0 {
		t.Errorf("Ring PrepareIOCmd/FlushSubmissions/WaitForCompletion: %v allocations per run, want 0", legacy)
	}
}

// BenchmarkNop measures submit+complete round trips of NOPs in batches;
// ns/op is per NOP.
func BenchmarkNop(b *testing.B) {
	for _, batch := range []int{1, 16, 64} {
		b.Run(fmt.Sprintf("batch=%d", batch), func(b *testing.B) {
			r := newTestIoUring(b, SetupOptions{Entries: 64})
			cqes := make([]*CQE, batch)
			b.ReportAllocs()
			b.ResetTimer()
			for done := 0; done < b.N; done += batch {
				for i := 0; i < batch; i++ {
					PrepNop(r.GetSQE())
				}
				if _, err := r.SubmitAndWait(uint32(batch), 0); err != nil {
					b.Fatal(err)
				}
				for n := 0; n < batch; {
					got := r.PeekBatchCQE(cqes)
					r.CQAdvance(uint32(got))
					n += got
				}
			}
		})
	}
}

// BenchmarkRingIOCmd measures the Ring path the current queue runner uses,
// one URING_CMD per round trip (completing -EOPNOTSUPP against a pipe).
func BenchmarkRingIOCmd(b *testing.B) {
	quietLogs(b)
	pr, pw, err := os.Pipe()
	if err != nil {
		b.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	ring, err := NewMinimalRing(64, int32(pr.Fd()))
	if err != nil {
		b.Skipf("io_uring unavailable: %v", err)
	}
	defer ring.Close()
	ioCmd := &uapi.UblksrvIOCmd{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ring.PrepareIOCmd(0, ioCmd, uint64(i)); err != nil {
			b.Fatal(err)
		}
		if _, err := ring.FlushSubmissions(); err != nil {
			b.Fatal(err)
		}
		if _, err := ring.WaitForCompletion(0); err != nil {
			b.Fatal(err)
		}
	}
}
