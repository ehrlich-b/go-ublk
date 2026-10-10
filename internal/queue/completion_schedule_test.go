package queue

import (
	"bytes"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// The fake ring is driven synchronously here. Each replay step is an explicit
// kernel event; the production engine's CQE decoder, dispatch and commit paths
// run unchanged, with no worker, eventfd or scheduling timeout required.
func scheduledBatchEngine(t *testing.T, depth int, h Handler) (*engine, *fakeKernel) {
	t.Helper()
	k := newFakeKernel(t, depth, testBufSize)
	e := newEngine(engineConfig{
		capacity: testCapacity, logicalBlockSize: 512, tagHi: depth, charFd: -1,
		desc: unsafe.Pointer(&k.desc[0]), descStride: 24,
		bufs: unsafe.Pointer(&k.bufs[0]), bufSize: testBufSize,
		handler: h, inline: true, batch: true,
	})
	e.ring = k
	if err := e.setupBatch(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.teardown)
	if _, err := k.Submit(); err != nil {
		t.Fatal(err)
	}
	reapScheduled(e, k)
	return e, k
}

func reapScheduled(e *engine, k *fakeKernel) {
	for c := k.PeekCQE(); c != nil; c = k.PeekCQE() {
		ud, res, flags := c.UserData, c.Res, c.Flags
		k.CQAdvance(1)
		e.handleCQE(ud, res, flags)
	}
	e.drainCompletions()
}

func TestCompletionScheduleRejectsMalformedFetch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		res   int32
		flags uint32
	}{
		{"buffer ID", 2, uring.IORING_CQE_F_BUFFER | 16<<uring.IORING_CQE_BUFFER_SHIFT},
		{"full buffer ID", 2, uring.IORING_CQE_F_BUFFER | 65535<<uring.IORING_CQE_BUFFER_SHIFT},
		{"odd length", 3, uring.IORING_CQE_F_BUFFER},
		{"oversized length", 258, uring.IORING_CQE_F_BUFFER},
		{"no selected buffer", 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			e, k := scheduledBatchEngine(t, 2, HandlerFunc(func(r *Request) { calls++; r.Complete(nil) }))
			k.scheduleCompletions(uring.CQE{UserData: kindFetch, Res: tc.res, Flags: tc.flags})
			reapScheduled(e, k)
			if e.err == nil || calls != 0 || e.handlers.Load() != 0 {
				t.Fatalf("malformed fetch: error=%v calls=%d loans=%d", e.err, calls, e.handlers.Load())
			}
		})
	}
}

func TestCompletionScheduleRejectsMalformedCommit(t *testing.T) {
	for _, tc := range []struct {
		name string
		slot bool
		res  int32
	}{
		{"odd count", false, 1}, {"oversized count", false, 32}, {"out-of-range slot", true, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, k := scheduledBatchEngine(t, 1, HandlerFunc(func(r *Request) { r.Complete(nil) }))
			k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, id: 1})
			reapScheduled(e, k)
			e.flushBatchCommits()
			_, _ = k.Submit()
			c := *k.PeekCQE()
			k.CQAdvance(1)
			bad := c
			bad.Res = tc.res
			if tc.slot {
				bad.UserData = kindBatch | 255
			}
			k.scheduleCompletions(bad)
			reapScheduled(e, k)
			if e.err == nil {
				t.Fatal("malformed commit accepted")
			}
			if len(e.pendingCommit) != 0 {
				t.Fatal("uncertain completion was retransmitted")
			}
			if tc.slot {
				k.scheduleCompletions(c)
				reapScheduled(e, k)
			}
		})
	}
}

func TestCompletionScheduleDuplicateFetchHasOneLoan(t *testing.T) {
	var held *Request
	calls := 0
	e, k := scheduledBatchEngine(t, 1, HandlerFunc(func(r *Request) { calls++; held = r }))
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, id: 1})
	c := *k.PeekCQE()
	reapScheduled(e, k)
	k.scheduleCompletions(c)
	reapScheduled(e, k)
	if e.err == nil || calls != 1 || e.handlers.Load() != 1 {
		t.Fatalf("duplicate delivery: error=%v calls=%d loans=%d", e.err, calls, e.handlers.Load())
	}
	held.Complete(nil)
	e.drainCompletions()
	e.flushBatchCommits()
	_, _ = k.Submit()
	reapScheduled(e, k)
	if !e.finished() {
		t.Fatal("duplicate delivery prevented bounded drain")
	}
}

func TestCompletionScheduleLateDuplicateCannotRetireReusedCommitSlot(t *testing.T) {
	e, k := scheduledBatchEngine(t, 1, HandlerFunc(func(r *Request) { r.Complete(nil) }))
	var old uring.CQE
	for id := 1; id <= 2; id++ {
		k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, id: id})
		reapScheduled(e, k)
		e.flushBatchCommits()
		_, _ = k.Submit()
		if id == 1 {
			old = *k.PeekCQE()
			reapScheduled(e, k)
			continue
		}
		current := *k.PeekCQE()
		k.CQAdvance(1)
		k.scheduleCompletions(old)
		reapScheduled(e, k)
		if e.err == nil || e.commitSent[0] == nil {
			t.Fatal("old CQE retired the new command")
		}
		k.scheduleCompletions(current)
		reapScheduled(e, k)
		if e.commitSent[0] != nil {
			t.Fatal("current command did not retire")
		}
	}
}

func TestCompletionSchedulePartialCommitAndEarlyReuse(t *testing.T) {
	e, k := scheduledBatchEngine(t, 2, HandlerFunc(func(r *Request) {
		for i := range r.Data {
			r.Data[i] = byte(r.Offset/512 + 1)
		}
		r.Complete(nil)
	}))
	k.commitLimit = 1
	// A valid script includes payload-free FLUSH with its sentinel sector.
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, sector: 1, id: 1})
	k.inject(1, fkReq{op: uapi.UBLK_IO_OP_FLUSH, sector: ^uint64(0), id: 2})
	reapScheduled(e, k)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, sector: 2, id: 3})
	// COMMIT consumes tag 0 and posts its next fetch before the partial CQE.
	for step := 0; step < 4 && (len(e.pendingCommit) != 0 || e.commitSent[0] != nil); step++ {
		e.flushBatchCommits()
		_, _ = k.Submit()
		reapScheduled(e, k)
	}
	commits, violations := k.snapshot()
	if e.err != nil || len(violations) != 0 || len(commits) != 3 || len(e.pendingCommit) != 0 {
		t.Fatalf("partial replay: error=%v violations=%v commits=%v pending=%d", e.err, violations, commits, len(e.pendingCommit))
	}
	seen := map[int]bool{}
	for _, c := range commits {
		if seen[c.id] {
			t.Fatalf("generation %d committed twice", c.id)
		}
		seen[c.id] = true
		if c.op == uapi.UBLK_IO_OP_FLUSH {
			if c.result != 0 {
				t.Fatal("FLUSH did not commit zero")
			}
		} else {
			want := byte(2)
			if c.id == 3 {
				want = 3
			}
			if c.result != 512 || !bytes.Equal(c.data, bytes.Repeat([]byte{want}, 512)) {
				t.Fatalf("generation %d has wrong payload/result", c.id)
			}
		}
	}
}
