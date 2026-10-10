package queue

import (
	"bytes"
	"errors"
	"os"
	"syscall"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// Each fake-kernel step is explicit. Retain a generation, commit it, reuse its
// tag, then reject old completion/access while the new read remains poisoned.
func TestRequestHandleRejectsStaleCompletionAndBuffersAfterReuse(t *testing.T) {
	for _, tc := range []struct {
		name                                      string
		batch, userCopy, integrity, async, shared bool
	}{
		{name: "copy-inline"}, {name: "copy-goroutine", async: true},
		{name: "user-copy-inline", userCopy: true}, {name: "user-copy-goroutine", userCopy: true, async: true},
		{name: "batch-copy", batch: true}, {name: "batch-user-copy", batch: true, userCopy: true},
		{name: "integrity", integrity: true}, {name: "batch-integrity", integrity: true, batch: true},
		{name: "shared-memory", shared: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := newFakeKernel(t, 1, testBufSize)
			k.desc = append(k.desc, make([]byte, 8)...)
			if tc.userCopy {
				f, err := os.CreateTemp(t.TempDir(), "user-copy")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = f.Close() })
				k.ufile = int(f.Fd())
			}
			metadata := bytes.Repeat([]byte{0xd7}, 16)
			deliveries := make(chan RequestHandle, 2)
			cfg := engineConfig{capacity: testCapacity, logicalBlockSize: 512, tagHi: 1,
				charFd: k.ufile, desc: unsafe.Pointer(&k.desc[0]), descStride: 32,
				bufs: unsafe.Pointer(&k.bufs[0]), bufSize: testBufSize,
				userCopy: tc.userCopy, inline: !tc.async, batch: tc.batch,
				handler: RequestHandlerFunc(func(h RequestHandle) { deliveries <- h }),
			}
			flags := uint32(0)
			if tc.integrity {
				cfg.integ, cfg.integSize = unsafe.Pointer(&metadata[0]), len(metadata)
				cfg.integInterval, cfg.integMeta = 512, 8
				flags = uint32(FlagIntegrity)
			}
			shm := uint64(0)
			region := make([]byte, testBufSize)
			if tc.shared {
				cfg.shmem = &SharedMemory{}
				cfg.shmem.Add(3, region)
				shm = 3 << 32
			}
			e := newEngine(cfg)
			e.ring = k
			t.Cleanup(e.teardown)
			if tc.batch {
				if err := e.setupBatch(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := e.prepIO(uapi.UBLK_IO_FETCH_REQ, 0, 0); err != nil {
					t.Fatal(err)
				}
			}
			_, _ = k.Submit()
			reapScheduled(e, k)
			k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, flags: flags, id: 1, shm: shm})
			reapScheduled(e, k)
			old := <-deliveries
			if err := old.WithBuffers(func(data, meta, extra []byte) error {
				for i := range data {
					data[i] = 0x31
				}
				for i := range meta {
					meta[i] = 0x41
				}
				if len(extra) != 8 {
					t.Fatal("missing descriptor extension")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := old.Complete(nil); err != nil {
				t.Fatal(err)
			}
			if err := old.WithBuffers(func(_, _, _ []byte) error { t.Error("completed buffer exposed"); return nil }); !errors.Is(err, ErrRequestCompleted) {
				t.Fatal(err)
			}
			e.drainCompletions()
			if tc.batch {
				e.flushBatchCommits()
			}
			_, _ = k.Submit()
			reapScheduled(e, k)
			k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, sector: 1, flags: flags, id: 2, shm: shm})
			reapScheduled(e, k)
			current := <-deliveries
			if current.generation == old.generation || current.r != old.r || old.Offset != 0 || current.Offset != 512 {
				t.Fatal("delivery identity/snapshot not preserved")
			}
			if err := current.WithBuffers(func(data, meta, extra []byte) error {
				for i := range data {
					data[i] = 0xcd
				}
				for i := range meta {
					meta[i] = 0xd7
				}
				for i := range extra {
					extra[i] = 0xa5
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for _, stale := range []func() error{
				func() error { return old.Complete(nil) }, func() error { return old.Complete(syscall.ENOSPC) },
				func() error { return old.CompleteN(512, nil) }, func() error { return old.CompleteZoneAppend(99, nil) },
				func() error { return old.ReportZones([]BlkZone{{Start: 99}}) },
				func() error {
					return old.WithBuffers(func(data, meta, extra []byte) error {
						clear(data)
						clear(meta)
						clear(extra)
						t.Error("stale buffer callback ran")
						return nil
					})
				},
			} {
				if err := stale(); !errors.Is(err, ErrStaleRequest) {
					t.Fatalf("stale call: %v", err)
				}
			}
			if e.handlers.Load() != 1 || current.r.result != 0 || current.r.lba != 0 {
				t.Fatal("stale completion changed new request/result")
			}
			if err := current.WithBuffers(func(data, meta, extra []byte) error {
				if !bytes.Equal(data, bytes.Repeat([]byte{0xcd}, 512)) || !bytes.Equal(meta, bytes.Repeat([]byte{0xd7}, len(meta))) || !bytes.Equal(extra, bytes.Repeat([]byte{0xa5}, 8)) {
					t.Fatal("stale handle changed poisoned buffers")
				}
				for i := range data {
					data[i] = 0x52
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := current.Complete(nil); err != nil {
				t.Fatal(err)
			}
			e.drainCompletions()
			if tc.batch {
				e.flushBatchCommits()
			}
			_, _ = k.Submit()
			reapScheduled(e, k)
			commits, violations := k.snapshot()
			if e.err != nil || len(commits) != 2 || len(violations) != 0 || e.handlers.Load() != 0 || commits[1].id != 2 || commits[1].result != 512 {
				t.Fatalf("commit ledger: err=%v commits=%v violations=%v", e.err, commits, violations)
			}
			if !tc.shared && (!bytes.Equal(commits[0].data, bytes.Repeat([]byte{0x31}, 512)) || !bytes.Equal(commits[1].data, bytes.Repeat([]byte{0x52}, 512))) {
				t.Fatal("committed stale data")
			}
		})
	}
}

func TestRequestHandleZeroCopyGenerationAfterReuse(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		auto, fallback, batch bool
	}{
		{name: "manual"}, {name: "auto", auto: true}, {name: "auto-fallback", auto: true, fallback: true},
		{name: "batch-auto", auto: true, batch: true}, {name: "batch-fallback", auto: true, fallback: true, batch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := newFakeKernel(t, 1, testBufSize)
			k.zcAuto = tc.auto
			k.zcFallback[0] = tc.fallback
			if err := k.RegisterBuffersSparse(1); err != nil {
				t.Fatal(err)
			}
			e := newEngine(engineConfig{capacity: testCapacity, logicalBlockSize: 512, tagHi: 1, charFd: 99,
				desc: unsafe.Pointer(&k.desc[0]), descStride: 24, bufSize: testBufSize, batch: tc.batch,
				zeroCopy: &zeroCopyConfig{fd: 77, base: 0, auto: tc.auto},
			})
			e.ring = k
			t.Cleanup(e.teardown)
			if tc.batch {
				if err := e.setupBatch(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := e.prepIO(uapi.UBLK_IO_FETCH_REQ, 0, 0); err != nil {
					t.Fatal(err)
				}
			}
			_, _ = k.Submit()
			reapScheduled(e, k)
			var old RequestHandle
			for id := 1; id <= 2; id++ {
				k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, id: id})
				reapScheduled(e, k)
				current := e.reqs[0].Handle()
				if id == 1 {
					old = current
				} else {
					if current.generation == old.generation {
						t.Fatal("zero-copy did not advance generation")
					}
					if err := old.Complete(nil); !errors.Is(err, ErrStaleRequest) {
						t.Fatal(err)
					}
					if err := old.WithBuffers(func(_, _, _ []byte) error { t.Error("stale zero-copy callback"); return nil }); !errors.Is(err, ErrStaleRequest) {
						t.Fatal(err)
					}
				}
				// Register, fixed-file I/O, unregister and commit are kernel events.
				for range 4 {
					_, _ = k.Submit()
					reapScheduled(e, k)
					if tc.batch {
						e.flushBatchCommits()
					}
				}
				_, _ = k.Submit()
				reapScheduled(e, k)
			}
			commits, violations := k.snapshot()
			if e.err != nil || len(commits) != 2 || len(violations) != 0 || e.handlers.Load() != 0 {
				t.Fatalf("zero-copy: err=%v commits=%v violations=%v", e.err, commits, violations)
			}
		})
	}
}

func TestRequestHandleZeroAndModifiedMetadata(t *testing.T) {
	var zero RequestHandle
	if !errors.Is(zero.Complete(nil), ErrStaleRequest) || !errors.Is(zero.WithBuffers(func(_, _, _ []byte) error { t.Error("zero callback"); return nil }), ErrStaleRequest) {
		t.Fatal("zero handle accepted")
	}
	var held RequestHandle
	e, k := scheduledEngine(t, 1, RequestHandlerFunc(func(h RequestHandle) { held = h }), false)
	k.inject(0, fkReq{op: uapi.UBLK_IO_OP_READ, nr: 1, id: 1})
	reapScheduled(e, k)
	held.Op, held.Length, held.Tag = OpFlush, 999999, 65535
	if err := held.Complete(nil); err != nil {
		t.Fatal(err)
	}
	e.drainCompletions()
	_, _ = k.Submit()
	reapScheduled(e, k)
	commits, violations := k.snapshot()
	if len(violations) != 0 || len(commits) != 1 || commits[0].result != 512 {
		t.Fatal("modified snapshot changed transport result")
	}
}

func TestRequestHandleHotPathDoesNotAllocate(t *testing.T) {
	e := newEngine(engineConfig{tagHi: 1})
	r := &e.reqs[0]
	r.Op = OpFlush
	handler := RequestHandlerFunc(func(h RequestHandle) {
		if err := h.WithBuffers(func(_, _, _ []byte) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if err := h.Complete(nil); err != nil {
			t.Fatal(err)
		}
	})
	allocations := testing.AllocsPerRun(1000, func() {
		if err := r.state.Begin(); err != nil {
			t.Fatal(err)
		}
		r.state.Store(reqDispatching)
		handler.HandleRequest(r)
		if r.state.Load() != reqDone {
			t.Fatal("inline completion not published")
		}
	})
	if allocations != 0 {
		t.Fatalf("request handle dispatch/access/completion allocated %g times", allocations)
	}
}

// An asynchronous handler may finish, let its tag be reused, then panic. Its
// recovery must use the original identity instead of completing the new tag.
func TestRequestHandlePanicAfterReusePreservesNewDelivery(t *testing.T) {
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	e := newEngine(engineConfig{tagHi: 1, handler: RequestHandlerFunc(func(h RequestHandle) {
		close(entered)
		<-release
		panic("late backend panic")
	})})
	r := &e.reqs[0]
	r.Op, r.Length = OpRead, 512
	if err := r.state.Begin(); err != nil {
		t.Fatal(err)
	}
	r.state.Store(reqAsync)
	old := r.Handle()
	go func() { defer close(returned); e.call(r) }()
	<-entered
	if err := old.Complete(nil); err != nil {
		t.Fatal(err)
	}
	if e.head.Swap(nil) != r {
		t.Fatal("old completion not published")
	}
	// Simulate the owner's completed commit and next delivery while the old
	// handler is held. The fake-kernel reuse tests cover these owner steps.
	r.state.Store(reqIdle)
	if err := r.state.Begin(); err != nil {
		t.Fatal(err)
	}
	r.Offset, r.result = 512, 0
	r.Data = bytes.Repeat([]byte{0xcd}, 512)
	r.state.Store(reqAsync)
	current := r.Handle()
	close(release)
	<-returned
	if r.state.Load() != reqAsync || r.result != 0 || e.head.Load() != nil || !bytes.Equal(r.Data, bytes.Repeat([]byte{0xcd}, 512)) {
		t.Fatal("old panic completed or changed new delivery")
	}
	if err := current.Complete(nil); err != nil {
		t.Fatal(err)
	}
}
