package queue

import (
	"bytes"
	"os"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

func completionPanic(fn func()) (p any) {
	defer func() { p = recover() }()
	fn()
	return nil
}

func TestCompletionOwnershipDuplicatePreservesResultAndUserCopy(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "user-copy")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	k := newFakeKernel(t, 1, testBufSize)
	k.ufile = int(f.Fd())
	panics := 0
	e := newEngine(engineConfig{
		capacity: testCapacity, logicalBlockSize: 512, tagHi: 1,
		charFd: k.ufile, userCopy: true, inline: true, batch: true,
		desc: unsafe.Pointer(&k.desc[0]), descStride: 24,
		bufs: unsafe.Pointer(&k.bufs[0]), bufSize: testBufSize,
		handler: HandlerFunc(func(r *Request) {
			for i := range r.Data {
				r.Data[i] = 0x31
			}
			r.Complete(nil)
			// Poison a retained, now invalid borrow to expose duplicate copy-out.
			for i := range r.Data {
				r.Data[i] = 0xd7
			}
			if completionPanic(func() { r.Complete(nil) }) != nil {
				panics++
			}
			if completionPanic(func() { r.Complete(syscall.ENOSPC) }) != nil {
				panics++
			}
		}),
	})
	e.ring = k
	if err := e.setupBatch(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.teardown)
	_, _ = k.Submit()
	reapScheduled(e, k)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, id: 1})
	reapScheduled(e, k)
	e.flushBatchCommits()
	_, _ = k.Submit()
	reapScheduled(e, k)
	commits, violations := k.snapshot()
	if panics != 2 || len(violations) != 0 || len(commits) != 1 || commits[0].result != 512 ||
		!bytes.Equal(commits[0].data, bytes.Repeat([]byte{0x31}, 512)) {
		t.Fatalf("duplicate completion changed accepted result/copy: panics=%d commits=%v violations=%v", panics, commits, violations)
	}
}

func TestCompletionOwnershipDuplicatePreservesAppendLBA(t *testing.T) {
	e := newEngine(engineConfig{tagHi: 1})
	r := &e.reqs[0]
	r.Op, r.Length = OpZoneAppend, 512
	if err := r.state.Begin(); err != nil {
		t.Fatal(err)
	}
	r.state.Store(reqDispatching)
	r.CompleteZoneAppend(42, nil)
	if completionPanic(func() { r.CompleteZoneAppend(99, nil) }) == nil || r.lba != 42 || r.result != 512 {
		t.Fatalf("duplicate append changed committed LBA/result: lba=%d result=%d", r.lba, r.result)
	}
}

func TestCompletionOwnershipConcurrentCallsHaveOnePublishedResult(t *testing.T) {
	var held *Request
	e, k := scheduledBatchEngine(t, 1, HandlerFunc(func(r *Request) { held = r }))
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, id: 1})
	reapScheduled(e, k)
	start := make(chan struct{})
	panics := make(chan any, 2)
	var wg sync.WaitGroup
	for _, result := range []error{nil, syscall.ENOSPC} {
		wg.Add(1)
		go func(result error) {
			defer wg.Done()
			<-start
			panics <- completionPanic(func() { held.Complete(result) })
		}(result)
	}
	close(start)
	wg.Wait()
	a, b := <-panics, <-panics
	if (a == nil) == (b == nil) {
		t.Fatal("concurrent completion did not accept exactly one caller")
	}
	e.drainCompletions()
	e.flushBatchCommits()
	_, _ = k.Submit()
	reapScheduled(e, k)
	commits, violations := k.snapshot()
	if len(commits) != 1 || len(violations) != 0 || e.handlers.Load() != 0 ||
		(commits[0].result != 512 && commits[0].result != -int32(syscall.ENOSPC)) {
		t.Fatalf("concurrent publication: commits=%v violations=%v loans=%d", commits, violations, e.handlers.Load())
	}
}

// Characterization, not a fix: reusing the public *Request lets an old caller
// complete the tag's new request. A CAS on the reused state cannot authenticate
// the caller's generation. Keep this witness until a completion-token API (or
// a distinct, never recycled public Request per delivery) replaces it.
func TestCompletionOwnershipCharacterizesStalePointerAfterTagReuse(t *testing.T) {
	var delivered []*Request
	e, k := scheduledBatchEngine(t, 1, HandlerFunc(func(r *Request) {
		delivered = append(delivered, r)
		for i := range r.Data {
			r.Data[i] = 0xcd
		} // newly borrowed buffer remains poisoned
	}))
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, id: 1})
	reapScheduled(e, k)
	old := delivered[0]
	for i := range old.Data {
		old.Data[i] = 0x31
	}
	old.Complete(nil)
	e.drainCompletions()
	e.flushBatchCommits()
	_, _ = k.Submit()
	reapScheduled(e, k)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, id: 2})
	reapScheduled(e, k)
	current := delivered[1]
	if old != current {
		t.Fatal("ownership changed: replace this characterization with a stale-call rejection regression")
	}
	if p := completionPanic(func() { old.Complete(nil) }); p != nil {
		t.Fatalf("stale completion behavior changed: %v", p)
	}
	e.drainCompletions()
	e.flushBatchCommits()
	_, _ = k.Submit()
	reapScheduled(e, k)
	commits, violations := k.snapshot()
	if len(commits) != 2 || len(violations) != 0 || commits[1].id != 2 || commits[1].result != 512 ||
		!bytes.Equal(commits[1].data, bytes.Repeat([]byte{0xcd}, 512)) || e.handlers.Load() != 0 {
		t.Fatalf("stale-call witness changed: commits=%v violations=%v loans=%d", commits, violations, e.handlers.Load())
	}
}
