package queue

import (
	"encoding/binary"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

const testDescBytes = int(unsafe.Sizeof(uapi.UblksrvIODesc{}))

type fakeRing struct {
	mu       sync.Mutex
	prepared []struct {
		cmd      uint32
		ioCmd    uapi.UblksrvIOCmd
		userData uint64
	}
}

func (f *fakeRing) Close() error { return nil }
func (f *fakeRing) SubmitCtrlCmd(cmd uint32, c *uapi.UblksrvCtrlCmd, ud uint64) (uring.Result, error) {
	return nil, nil
}
func (f *fakeRing) SubmitCtrlCmdAsync(cmd uint32, c *uapi.UblksrvCtrlCmd, ud uint64) (*uring.AsyncHandle, error) {
	return nil, nil
}
func (f *fakeRing) SubmitIOCmd(cmd uint32, c *uapi.UblksrvIOCmd, ud uint64) (uring.Result, error) {
	return nil, nil
}
func (f *fakeRing) PrepareIOCmd(cmd uint32, c *uapi.UblksrvIOCmd, ud uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepared = append(f.prepared, struct {
		cmd      uint32
		ioCmd    uapi.UblksrvIOCmd
		userData uint64
	}{cmd, *c, ud})
	return nil
}
func (f *fakeRing) FlushSubmissions() (uint32, error)                     { return uint32(len(f.prepared)), nil }
func (f *fakeRing) WaitForCompletion(timeout int) ([]uring.Result, error) { return nil, nil }
func (f *fakeRing) NewBatch() uring.Batch                                 { return nil }
func (f *fakeRing) prepareCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.prepared)
}
func (f *fakeRing) lastPrepared() (uint32, uapi.UblksrvIOCmd, uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.prepared) == 0 {
		return 0, uapi.UblksrvIOCmd{}, 0, false
	}
	p := f.prepared[len(f.prepared)-1]
	return p.cmd, p.ioCmd, p.userData, true
}

var _ uring.Ring = (*fakeRing)(nil)

func newSyntheticRunner(depth int, descBuf, bufBuf []byte) (*Runner, *fakeRing) {
	if descBuf == nil {
		descBuf = make([]byte, depth*testDescBytes)
	}
	if bufBuf == nil {
		bufBuf = make([]byte, depth*64*1024)
	}
	ring := &fakeRing{}
	r := &Runner{
		depth:      depth,
		backend:    newMockBackend(1 << 20),
		ring:       ring,
		descPtr:    unsafe.Pointer(&descBuf[0]),
		bufPtr:     unsafe.Pointer(&bufBuf[0]),
		tagStates:  make([]TagState, depth),
		tagMutexes: make([]sync.Mutex, depth),
		ioCmds:     make([]uapi.UblksrvIOCmd, depth),
	}
	return r, ring
}

func setTagState(r *Runner, tag int, state TagState) {
	r.tagMutexes[tag].Lock()
	r.tagStates[tag] = state
	r.tagMutexes[tag].Unlock()
}

func assertCommitPrepared(t *testing.T, ring *fakeRing, tag uint16) {
	t.Helper()
	if got := ring.prepareCount(); got != 1 {
		t.Fatalf("expected exactly 1 PrepareIOCmd call, got %d", got)
	}
	cmd, ioCmd, ud, ok := ring.lastPrepared()
	if !ok {
		t.Fatal("no prepared command recorded")
	}
	wantCmd := uapi.UblkIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ)
	if cmd != wantCmd {
		t.Errorf("cmd = %#x, want %#x (COMMIT_AND_FETCH_REQ)", cmd, wantCmd)
	}
	if ioCmd.Result != 0 {
		t.Errorf("ioCmd.Result = %d, want 0 (zero sectors -> 0<<9)", ioCmd.Result)
	}
	if ioCmd.Tag != tag {
		t.Errorf("ioCmd.Tag = %d, want %d", ioCmd.Tag, tag)
	}
	if ud&udOpCommit == 0 {
		t.Errorf("userData = %#x should encode the COMMIT operation type", ud)
	}
}

func TestLoadDescriptorOffsets(t *testing.T) {
	depth := 3
	descBuf := make([]byte, depth*testDescBytes)

	binary.LittleEndian.PutUint32(descBuf[0:4], 0xDEADBEEF)
	binary.LittleEndian.PutUint32(descBuf[4:8], 0x11223344)
	binary.LittleEndian.PutUint64(descBuf[8:16], 0x1122334455667788)
	binary.LittleEndian.PutUint64(descBuf[16:24], 0xAABBCCDDEEFF0011)

	off2 := 2 * testDescBytes
	binary.LittleEndian.PutUint32(descBuf[off2+0:off2+4], 0x01020304)
	binary.LittleEndian.PutUint32(descBuf[off2+4:off2+8], 0x05060708)
	binary.LittleEndian.PutUint64(descBuf[off2+8:off2+16], 0x090A0B0C0D0E0F10)
	binary.LittleEndian.PutUint64(descBuf[off2+16:off2+24], 0x1020304050607080)

	r, _ := newSyntheticRunner(depth, descBuf, nil)

	desc0 := r.loadDescriptor(0)
	if desc0.OpFlags != 0xDEADBEEF {
		t.Errorf("tag0 OpFlags = %#x, want 0xDEADBEEF", desc0.OpFlags)
	}
	if desc0.NrSectors != 0x11223344 {
		t.Errorf("tag0 NrSectors = %#x, want 0x11223344", desc0.NrSectors)
	}
	if desc0.StartSector != 0x1122334455667788 {
		t.Errorf("tag0 StartSector = %#x, want 0x1122334455667788", desc0.StartSector)
	}
	if desc0.Addr != 0xAABBCCDDEEFF0011 {
		t.Errorf("tag0 Addr = %#x, want 0xAABBCCDDEEFF0011", desc0.Addr)
	}

	desc2 := r.loadDescriptor(2)
	if desc2.OpFlags != 0x01020304 {
		t.Errorf("tag2 OpFlags = %#x, want 0x01020304", desc2.OpFlags)
	}
	if desc2.NrSectors != 0x05060708 {
		t.Errorf("tag2 NrSectors = %#x, want 0x05060708", desc2.NrSectors)
	}
	if desc2.StartSector != 0x090A0B0C0D0E0F10 {
		t.Errorf("tag2 StartSector = %#x, want 0x090A0B0C0D0E0F10", desc2.StartSector)
	}
	if desc2.Addr != 0x1020304050607080 {
		t.Errorf("tag2 Addr = %#x, want 0x1020304050607080", desc2.Addr)
	}

	desc1 := r.loadDescriptor(1)
	if desc1.OpFlags != 0 || desc1.NrSectors != 0 || desc1.StartSector != 0 || desc1.Addr != 0 {
		t.Errorf("untouched tag1 should read all-zero descriptor, got %+v", desc1)
	}
}

func TestHandleCompletionFetchOK(t *testing.T) {
	r, ring := newSyntheticRunner(1, nil, nil)
	setTagState(r, 0, TagStateInFlightFetch)

	if err := r.handleCompletion(0, false, 0); err != nil {
		t.Fatalf("fetch OK cycle should not error: %v", err)
	}

	if r.tagStates[0] != TagStateInFlightCommit {
		t.Errorf("state = %d, want TagStateInFlightCommit", r.tagStates[0])
	}
	assertCommitPrepared(t, ring, 0)
}

func TestHandleCompletionCommitOK(t *testing.T) {
	r, ring := newSyntheticRunner(1, nil, nil)
	setTagState(r, 0, TagStateInFlightCommit)

	if err := r.handleCompletion(0, true, 0); err != nil {
		t.Fatalf("commit OK cycle should not error: %v", err)
	}

	if r.tagStates[0] != TagStateInFlightCommit {
		t.Errorf("state = %d, want TagStateInFlightCommit (steady-state repeat)", r.tagStates[0])
	}
	assertCommitPrepared(t, ring, 0)
}

func TestHandleCompletionNeedGetData(t *testing.T) {
	sub := func(t *testing.T, name string, initialState TagState, isCommit bool) {
		t.Run(name, func(t *testing.T) {
			r, ring := newSyntheticRunner(1, nil, nil)
			setTagState(r, 0, initialState)

			err := r.handleCompletion(0, isCommit, 1)
			if err == nil {
				t.Fatal("expected error for NEED_GET_DATA, got nil")
			}
			if !strings.Contains(err.Error(), "NEED_GET_DATA") {
				t.Errorf("error should mention NEED_GET_DATA, got: %v", err)
			}
			if r.tagStates[0] != TagStateOwned {
				t.Errorf("state = %d, want TagStateOwned", r.tagStates[0])
			}
			if got := ring.prepareCount(); got != 0 {
				t.Errorf("expected 0 PrepareIOCmd calls (no commit on NEED_GET_DATA), got %d", got)
			}
		})
	}

	sub(t, "fromFetch", TagStateInFlightFetch, false)
	sub(t, "fromCommit", TagStateInFlightCommit, true)
}

func TestHandleCompletionCommitError(t *testing.T) {
	r, ring := newSyntheticRunner(1, nil, nil)
	setTagState(r, 0, TagStateInFlightCommit)

	err := r.handleCompletion(0, true, -5)
	if err == nil {
		t.Fatal("expected error for negative COMMIT result, got nil")
	}
	if !strings.Contains(err.Error(), "COMMIT_AND_FETCH error") {
		t.Errorf("error should mention COMMIT_AND_FETCH error, got: %v", err)
	}
	if r.tagStates[0] != TagStateOwned {
		t.Errorf("state = %d, want TagStateOwned (tag reusable after error)", r.tagStates[0])
	}
	if got := ring.prepareCount(); got != 0 {
		t.Errorf("expected 0 PrepareIOCmd calls, got %d", got)
	}
}

func TestHandleCompletionUnexpectedResults(t *testing.T) {
	t.Run("fromFetch", func(t *testing.T) {
		r, ring := newSyntheticRunner(1, nil, nil)
		setTagState(r, 0, TagStateInFlightFetch)

		err := r.handleCompletion(0, false, 5)
		if err == nil {
			t.Fatal("expected error for unexpected FETCH result, got nil")
		}
		if !strings.Contains(err.Error(), "unexpected FETCH result") {
			t.Errorf("error should mention unexpected FETCH result, got: %v", err)
		}
		if r.tagStates[0] != TagStateInFlightFetch {
			t.Errorf("state = %d, want TagStateInFlightFetch UNCHANGED", r.tagStates[0])
		}
		if got := ring.prepareCount(); got != 0 {
			t.Errorf("expected 0 PrepareIOCmd calls, got %d", got)
		}
	})

	t.Run("fromCommit", func(t *testing.T) {
		r, ring := newSyntheticRunner(1, nil, nil)
		setTagState(r, 0, TagStateInFlightCommit)

		err := r.handleCompletion(0, true, 5)
		if err == nil {
			t.Fatal("expected error for unexpected COMMIT result, got nil")
		}
		if !strings.Contains(err.Error(), "unexpected COMMIT result") {
			t.Errorf("error should mention unexpected COMMIT result, got: %v", err)
		}
		if r.tagStates[0] != TagStateInFlightCommit {
			t.Errorf("state = %d, want TagStateInFlightCommit UNCHANGED", r.tagStates[0])
		}
		if got := ring.prepareCount(); got != 0 {
			t.Errorf("expected 0 PrepareIOCmd calls, got %d", got)
		}
	})
}

func TestHandleCompletionOwnedGuard(t *testing.T) {
	r, ring := newSyntheticRunner(1, nil, nil)
	setTagState(r, 0, TagStateOwned)

	err := r.handleCompletion(0, false, 0)
	if err == nil {
		t.Fatal("expected error for completion in Owned state, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected completion") {
		t.Errorf("error should mention unexpected completion, got: %v", err)
	}
	if r.tagStates[0] != TagStateOwned {
		t.Errorf("state = %d, want TagStateOwned UNCHANGED", r.tagStates[0])
	}
	if got := ring.prepareCount(); got != 0 {
		t.Errorf("expected 0 PrepareIOCmd calls, got %d", got)
	}
}

func TestHandleCompletionInvalidState(t *testing.T) {
	r, ring := newSyntheticRunner(1, nil, nil)
	setTagState(r, 0, TagState(99))

	err := r.handleCompletion(0, false, 0)
	if err == nil {
		t.Fatal("expected error for invalid tag state, got nil")
	}
	if !strings.Contains(err.Error(), "invalid state") {
		t.Errorf("error should mention invalid state, got: %v", err)
	}
	if r.tagStates[0] != TagState(99) {
		t.Errorf("state = %d, want 99 UNCHANGED", r.tagStates[0])
	}
	if got := ring.prepareCount(); got != 0 {
		t.Errorf("expected 0 PrepareIOCmd calls, got %d", got)
	}
}

func TestHandleCompletionConcurrentTags(t *testing.T) {
	depth := 2
	r, ring := newSyntheticRunner(depth, nil, nil)

	const iterations = 5
	var wg sync.WaitGroup
	for tag := 0; tag < depth; tag++ {
		wg.Add(1)
		go func(tg int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				setTagState(r, tg, TagStateInFlightFetch)
				if err := r.handleCompletion(uint16(tg), false, 0); err != nil {
					t.Errorf("tag %d fetch cycle: %v", tg, err)
					return
				}
				if r.tagStates[tg] != TagStateInFlightCommit {
					t.Errorf("tag %d after fetch cycle: state %d", tg, r.tagStates[tg])
				}
				if err := r.handleCompletion(uint16(tg), true, 0); err != nil {
					t.Errorf("tag %d commit cycle: %v", tg, err)
					return
				}
				if r.tagStates[tg] != TagStateInFlightCommit {
					t.Errorf("tag %d after commit cycle: state %d", tg, r.tagStates[tg])
				}
			}
		}(tag)
	}
	wg.Wait()

	wantPrepares := depth * iterations * 2
	if got := ring.prepareCount(); got != wantPrepares {
		t.Errorf("expected %d PrepareIOCmd calls total, got %d", wantPrepares, got)
	}
	for tag := 0; tag < depth; tag++ {
		if r.tagStates[tag] != TagStateInFlightCommit {
			t.Errorf("tag %d final state = %d, want TagStateInFlightCommit", tag, r.tagStates[tag])
		}
	}
}
