package queue

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// The whole I/O dispatch surface (handleIORequest + submitCommitAndFetch) is
// exercised here with synthetic memory and a fake ring — zero syscalls.

// ---------- Harness ----------

// fakeRing implements uring.Ring without any kernel interaction; it records
// every PrepareIOCmd call so the test can inspect what submitCommitAndFetch
// encoded.
type fakeRing struct {
	mu          sync.Mutex
	prepared    []preparedIOCmd
	prepareErr  error
	flushResult uint32
}

type preparedIOCmd struct {
	cmd      uint32
	ioCmd    *uapi.UblksrvIOCmd
	userData uint64
}

func (f *fakeRing) Close() error { return nil }

func (f *fakeRing) SubmitCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (uring.Result, error) {
	return nil, nil
}

func (f *fakeRing) SubmitCtrlCmdAsync(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (*uring.AsyncHandle, error) {
	return nil, nil
}

func (f *fakeRing) SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) (uring.Result, error) {
	return nil, nil
}

func (f *fakeRing) PrepareIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prepareErr != nil {
		return f.prepareErr
	}
	f.prepared = append(f.prepared, preparedIOCmd{cmd: cmd, ioCmd: ioCmd, userData: userData})
	return nil
}

func (f *fakeRing) FlushSubmissions() (uint32, error) { return f.flushResult, nil }

func (f *fakeRing) WaitForCompletion(timeout int) ([]uring.Result, error) { return nil, nil }

func (f *fakeRing) NewBatch() uring.Batch { return nil }

func (f *fakeRing) prepareCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.prepared)
}

// newIOHarness builds a hand-constructed Runner over synthetic descriptor and
// buffer memory (no NewRunner, no mmap, no real ring/device, zero syscalls),
// plus a fake ring recording PrepareIOCmd calls. descMem and bufMem are the
// byte slices backing descPtr and bufPtr, so tests read/write them directly.
func newIOHarness(t *testing.T, depth int, backend interfaces.Backend) (*Runner, *fakeRing, []byte, []byte) {
	t.Helper()
	if depth < 1 {
		t.Fatal("newIOHarness: depth must be >= 1")
	}

	descMem := make([]byte, depth*int(unsafe.Sizeof(uapi.UblksrvIODesc{})))
	bufMem := make([]byte, depth*constants.IOBufferSizePerTag)

	ring := &fakeRing{}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runner := &Runner{
		deviceID:     0,
		queueID:      0,
		depth:        depth,
		backend:      backend,
		charDeviceFd: -1,
		ring:         ring,
		descPtr:      unsafe.Pointer(&descMem[0]),
		bufPtr:       unsafe.Pointer(&bufMem[0]),
		ctx:          ctx,
		cancel:       cancel,
		logger:       nil,
		observer:     nil,
		cpuAffinity:  nil,
		tagStates:    make([]TagState, depth),
		tagMutexes:   make([]sync.Mutex, depth),
		ioCmds:       make([]uapi.UblksrvIOCmd, depth),
		done:         make(chan struct{}),
		launched:     false,
	}

	return runner, ring, descMem, bufMem
}

// putDesc stores desc into the synthetic descriptor memory at tag's slot using
// the exact same offsets/atomics loadDescriptor reads from, so the pair stays
// race-detector-clean even though both live in the test goroutine.
func putDesc(t *testing.T, descMem []byte, tag uint16, desc uapi.UblksrvIODesc) {
	t.Helper()
	base := uintptr(tag) * unsafe.Sizeof(uapi.UblksrvIODesc{})
	ptr := unsafe.Pointer(&descMem[0])
	if uintptr(tag)*unsafe.Sizeof(uapi.UblksrvIODesc{})+unsafe.Sizeof(uapi.UblksrvIODesc{}) > uintptr(len(descMem)) {
		t.Fatalf("putDesc: tag %d out of range", tag)
	}
	atomic.StoreUint32((*uint32)(unsafe.Add(ptr, base+descOpFlagsOffset)), desc.OpFlags)
	atomic.StoreUint32((*uint32)(unsafe.Add(ptr, base+descNrSectorsOffset)), desc.NrSectors)
	atomic.StoreUint64((*uint64)(unsafe.Add(ptr, base+descStartSectorOffset)), desc.StartSector)
	atomic.StoreUint64((*uint64)(unsafe.Add(ptr, base+descAddrOffset)), desc.Addr)
}

// iodesc builds a descriptor with the op in bits 0-7 of OpFlags (GetOp's mask).
func iodesc(op uint8, startSector uint64, nrSectors uint32) uapi.UblksrvIODesc {
	return uapi.UblksrvIODesc{
		OpFlags:     uint32(op),
		NrSectors:   nrSectors,
		StartSector: startSector,
	}
}

// setOwned moves tag into TagStateOwned under its per-tag lock, mirroring the
// real transition that handleCompletion performs before handleIORequest runs.
func setOwned(runner *Runner, tag uint16) {
	runner.tagMutexes[tag].Lock()
	runner.tagStates[tag] = TagStateOwned
	runner.tagMutexes[tag].Unlock()
}

// patternBytes returns a deterministic n-byte pattern starting at start.
func patternBytes(start byte, n int) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = start + byte(i)
	}
	return p
}

// Patched tracking backend: mockBackend only implements Backend (no
// DiscardBackend / no WriteZeroesBackend). This double embeds it and adds the
// two optional dispatch methods, recording arguments, plus a Flush counter.
type discardWZBackend struct {
	*mockBackend
	mu         sync.Mutex
	flushCount int
	discards   []discardCall
	writeZeros []writeZeroCall
}

type discardCall struct {
	offset int64
	length int64
}

type writeZeroCall struct {
	offset int64
	length int64
}

// Flush shadows the embedded no-op so the test can count flush invocations.
func (b *discardWZBackend) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushCount++
	return nil
}

func (b *discardWZBackend) Discard(offset, length int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.discards = append(b.discards, discardCall{offset: offset, length: length})
	return nil
}

func (b *discardWZBackend) WriteZeroes(offset, length int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writeZeros = append(b.writeZeros, writeZeroCall{offset: offset, length: length})
	return nil
}

func (b *discardWZBackend) drainDiscards() []discardCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := append([]discardCall(nil), b.discards...)
	b.discards = nil
	return out
}

func (b *discardWZBackend) drainWriteZeros() []writeZeroCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := append([]writeZeroCall(nil), b.writeZeros...)
	b.writeZeros = nil
	return out
}

// ---------- 1. READ ----------

// READ at a non-trivial offset: despatches to Backend.ReadAt from the synthetic
// buffer, copies the backend bytes into the tag buffer, and commits 512 bytes
// processed (NrSectors=1 -> 1<<9).
func TestHandleIORequestRead(t *testing.T) {
	const depth = 2
	backend := newMockBackend(256 * 1024)
	runner, ring, descMem, bufMem := newIOHarness(t, depth, backend)

	// StartSector=8 -> byte offset 8*512=4096; NrSectors=1 -> 512 bytes.
	const startSector = uint64(8)
	const backendOffset = int64(startSector * uapi.SectorSize)
	const nrSectors = uint32(1)
	const length = int(nrSectors * uapi.SectorSize)

	pattern := patternBytes(0xA0, length)
	if _, err := backend.WriteAt(pattern, backendOffset); err != nil {
		t.Fatalf("pre-fill backend: %v", err)
	}

	putDesc(t, descMem, 0, iodesc(uapi.UBLK_IO_OP_READ, startSector, nrSectors))
	setOwned(runner, 0)

	// loadDescriptor pulls the descriptor back through the synthetic mmap;
	// handleIORequest then dispatches it.
	loaded := runner.loadDescriptor(0)
	if err := runner.handleIORequest(0, loaded); err != nil {
		t.Fatalf("handleIORequest(READ) returned error: %v", err)
	}

	// The tag 0 buffer region (start of the 64KB region since the read is
	// small) must now hold exactly the backend pattern.
	if got := bufMem[0:length]; !bytes.Equal(got, pattern) {
		t.Fatalf("tag 0 buffer mismatch:\n got %x\nwant %x", got, pattern)
	}

	// submitCommitAndFetch was reached, encoded bytes-processed (1<<9 == 512),
	// and the tag moved Owned -> InFlightCommit.
	if runner.tagStates[0] != TagStateInFlightCommit {
		t.Errorf("tag 0 state = %d, want TagStateInFlightCommit", runner.tagStates[0])
	}
	if got := runner.ioCmds[0].Result; got != 512 {
		t.Errorf("commit Result = %d, want 512 (1<<9)", got)
	}
	if got := runner.ioCmds[0].Tag; got != 0 {
		t.Errorf("commit Tag = %d, want 0", got)
	}
	if got := runner.ioCmds[0].QID; got != 0 {
		t.Errorf("commit QID = %d, want 0", got)
	}
	if got := ring.prepareCount(); got != 1 {
		t.Fatalf("PrepareIOCmd calls = %d, want 1", got)
	}
	if ring.prepared[0].userData != (udOpCommit | uint64(0) | 0) {
		t.Errorf("userData = %#x, want %#x (udOpCommit | queueID<<16 | tag)", ring.prepared[0].userData, udOpCommit)
	}
	if ring.prepared[0].cmd != uapi.UblkIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ) {
		t.Errorf("prepared cmd = %#x, want %#x", ring.prepared[0].cmd, uapi.UblkIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ))
	}
}

// ---------- 2. WRITE ----------

// WRITE at a different offset: copies the pattern out of the tag buffer into
// Backend.WriteAt at the derived offset; commit reports bytes processed.
func TestHandleIORequestWrite(t *testing.T) {
	const depth = 2
	backend := newMockBackend(256 * 1024)
	runner, ring, descMem, bufMem := newIOHarness(t, depth, backend)

	// StartSector=16 -> byte offset 16*512=8192; NrSectors=2 -> 1024 bytes.
	const startSector = uint64(16)
	const backendOffset = int64(startSector * uapi.SectorSize)
	const nrSectors = uint32(2)
	const length = int(nrSectors * uapi.SectorSize)

	// Distinctive pattern placed in the tag 0 buffer region first, simulating
	// what the kernel would have written there for a WRITE.
	pattern := patternBytes(0x50, length)
	copy(bufMem[0:length], pattern)

	putDesc(t, descMem, 0, iodesc(uapi.UBLK_IO_OP_WRITE, startSector, nrSectors))
	setOwned(runner, 0)

	loaded := runner.loadDescriptor(0)
	if err := runner.handleIORequest(0, loaded); err != nil {
		t.Fatalf("handleIORequest(WRITE) returned error: %v", err)
	}

	// Prove the bytes actually moved to the backend by reading back through
	// the backend's own ReadAt — not by re-reading the buffer.
	back := make([]byte, length)
	if _, err := backend.ReadAt(back, backendOffset); err != nil {
		t.Fatalf("read back backend at %d: %v", backendOffset, err)
	}
	if !bytes.Equal(back, pattern) {
		t.Fatalf("backend data at %d mismatch:\n got %x\nwant %x", backendOffset, back, pattern)
	}

	if runner.tagStates[0] != TagStateInFlightCommit {
		t.Errorf("tag 0 state = %d, want TagStateInFlightCommit", runner.tagStates[0])
	}
	// NrSectors=2 -> 2<<9 == 1024 bytes processed.
	if got := runner.ioCmds[0].Result; got != int32(nrSectors<<9) {
		t.Errorf("commit Result = %d, want %d (2<<9)", got, int32(nrSectors<<9))
	}
	if got := ring.prepareCount(); got != 1 {
		t.Errorf("PrepareIOCmd calls = %d, want 1", got)
	}
}

// ---------- 3. FLUSH ----------

// FLUSH carries no data (NrSectors=0): the backend's Flush must be invoked,
// and the commit must report 0 bytes processed.
func TestHandleIORequestFlush(t *testing.T) {
	const depth = 2
	be := &discardWZBackend{mockBackend: newMockBackend(256 * 1024)}
	runner, _, descMem, _ := newIOHarness(t, depth, be)

	putDesc(t, descMem, 0, iodesc(uapi.UBLK_IO_OP_FLUSH, 0, 0))
	setOwned(runner, 0)

	loaded := runner.loadDescriptor(0)
	if err := runner.handleIORequest(0, loaded); err != nil {
		t.Fatalf("handleIORequest(FLUSH) returned error: %v", err)
	}

	be.mu.Lock()
	flushes := be.flushCount
	be.mu.Unlock()
	if flushes != 1 {
		t.Errorf("backend Flush calls = %d, want 1", flushes)
	}

	if runner.tagStates[0] != TagStateInFlightCommit {
		t.Errorf("tag 0 state = %d, want TagStateInFlightCommit", runner.tagStates[0])
	}
	// NrSectors==0 -> 0<<9 == 0.
	if got := runner.ioCmds[0].Result; got != 0 {
		t.Errorf("commit Result = %d, want 0", got)
	}
}

// ---------- 4. Unsupported op ----------

func TestHandleIORequestUnsupportedOp(t *testing.T) {
	const depth = 2
	backend := newMockBackend(256 * 1024)
	runner, ring, descMem, _ := newIOHarness(t, depth, backend)

	// UBLK_IO_OP_ZONE_OPEN (10) is outside every handled switch case.
	putDesc(t, descMem, 0, iodesc(uapi.UBLK_IO_OP_ZONE_OPEN, 0, 1))
	setOwned(runner, 0)

	loaded := runner.loadDescriptor(0)
	err := runner.handleIORequest(0, loaded)

	// PINNED FINDING (divergence from the task brief): the brief expected
	// handleIORequest to return the "unsupported operation" error. The actual
	// code folds the error into the COMMIT result and returns the (nil) result
	// of submitCommitAndFetch, so an unsupported op is ACKNOWLEDGED to the
	// kernel as -EIO without tearing down the whole queue — a failed tag that
	// is never committed would wedge its slot forever, and processIOAndCommit
	// treats any non-nil return as fatal to the loop. So the real contract is:
	// error is encoded as Result==-5, and the function returns nil.
	if err != nil {
		t.Fatalf("handleIORequest(unsupported) should return nil (error folded into COMMIT result), got %v", err)
	}

	// The COMMIT must still have been prepared and must report -EIO.
	if got := ring.prepareCount(); got != 1 {
		t.Fatalf("PrepareIOCmd calls = %d, want 1 (unsupported op must be acknowledged, not dropped)", got)
	}
	if got := runner.ioCmds[0].Result; got != -5 {
		t.Errorf("commit Result = %d, want -5 (-EIO)", got)
	}
	if runner.tagStates[0] != TagStateInFlightCommit {
		t.Errorf("tag 0 state = %d, want TagStateInFlightCommit", runner.tagStates[0])
	}
}

// ---------- 5. DISCARD / WRITE_ZEROES asymmetry ----------

// ASYMMETRY (pinned, expected — do not "fix"):
//   - A DISCARD sent to a backend that does NOT implement
//     interfaces.DiscardBackend silently no-ops and still reports SUCCESS in
//     the COMMIT result. Discard is advisory by design: a device is always
//     free to ignore one.
//   - A WRITE_ZEROES sent to a backend that does NOT implement
//     interfaces.WriteZeroesBackend is an explicit hard error (Result==-5),
//     because silently no-oping would leave non-zero data behind and violate
//     the write-zeroes contract (zeros must actually exist afterwards).
//
// Both halves are tested explicitly against each backend flavor.
func TestDiscardWriteZeroesAsymmetry(t *testing.T) {
	const startSector = uint64(32)
	const nrSectors = uint32(4)
	const wantOffset = int64(startSector * uapi.SectorSize) // 32*512 = 16384
	const wantLength = int64(nrSectors * uapi.SectorSize)   // 4*512 = 2048
	const wantResult = int32(nrSectors << 9)                // 4<<9 = 2048

	// (a) DISCARD against plain mockBackend (no DiscardBackend): NO error,
	// COMMIT reports SUCCESS.
	t.Run("discard_unsupported_silently_succeeds", func(t *testing.T) {
		backend := newMockBackend(256 * 1024)
		runner, _, descMem, _ := newIOHarness(t, 1, backend)

		putDesc(t, descMem, 0, iodesc(uapi.UBLK_IO_OP_DISCARD, startSector, nrSectors))
		setOwned(runner, 0)

		if err := runner.handleIORequest(0, runner.loadDescriptor(0)); err != nil {
			t.Fatalf("DISCARD on unsupporting backend must NOT error, got %v", err)
		}
		if got := runner.ioCmds[0].Result; got != wantResult {
			t.Errorf("DISCARD(unsupported) commit Result = %d, want %d (silently reported success)", got, wantResult)
		}
	})

	// (b) WRITE_ZEROES against plain mockBackend (no WriteZeroesBackend):
	// explicit error, COMMIT reports -5.
	t.Run("write_zeroes_unsupported_errors", func(t *testing.T) {
		backend := newMockBackend(256 * 1024)
		runner, _, descMem, _ := newIOHarness(t, 1, backend)

		putDesc(t, descMem, 0, iodesc(uapi.UBLK_IO_OP_WRITE_ZEROES, startSector, nrSectors))
		setOwned(runner, 0)

		if err := runner.handleIORequest(0, runner.loadDescriptor(0)); err != nil {
			// Error is folded into the COMMIT result; see pinned note in
			// TestHandleIORequestUnsupportedOp.
			t.Fatalf("handleIORequest should not propagate the error (folded into COMMIT), got %v", err)
		}
		if got := runner.ioCmds[0].Result; got != -5 {
			t.Errorf("WRITE_ZEROES(unsupported) commit Result = %d, want -5 (-EIO)", got)
		}
	})

	// (c) DISCARD against the discard-capable double: must actually be called
	// with the derived (offset, length), and commit reflects success.
	t.Run("discard_supported_dispatches", func(t *testing.T) {
		be := &discardWZBackend{mockBackend: newMockBackend(256 * 1024)}
		runner, _, descMem, _ := newIOHarness(t, 1, be)

		putDesc(t, descMem, 0, iodesc(uapi.UBLK_IO_OP_DISCARD, startSector, nrSectors))
		setOwned(runner, 0)

		if err := runner.handleIORequest(0, runner.loadDescriptor(0)); err != nil {
			t.Fatalf("DISCARD on supporting backend errored: %v", err)
		}
		discards := be.drainDiscards()
		if len(discards) != 1 {
			t.Fatalf("backend Discard calls = %d, want 1", len(discards))
		}
		if discards[0].offset != wantOffset || discards[0].length != wantLength {
			t.Errorf("Discard(%d, %d), want (%d, %d)", discards[0].offset, discards[0].length, wantOffset, wantLength)
		}
		if got := runner.ioCmds[0].Result; got != wantResult {
			t.Errorf("DISCARD(supported) commit Result = %d, want %d", got, wantResult)
		}
	})

	// (d) WRITE_ZEROES against the write-zeroes-capable double: called with
	// the derived (offset, length), and commit reflects success.
	t.Run("write_zeroes_supported_dispatches", func(t *testing.T) {
		be := &discardWZBackend{mockBackend: newMockBackend(256 * 1024)}
		runner, _, descMem, _ := newIOHarness(t, 1, be)

		putDesc(t, descMem, 0, iodesc(uapi.UBLK_IO_OP_WRITE_ZEROES, startSector, nrSectors))
		setOwned(runner, 0)

		if err := runner.handleIORequest(0, runner.loadDescriptor(0)); err != nil {
			t.Fatalf("WRITE_ZEROES on supporting backend errored: %v", err)
		}
		wzs := be.drainWriteZeros()
		if len(wzs) != 1 {
			t.Fatalf("backend WriteZeroes calls = %d, want 1", len(wzs))
		}
		if wzs[0].offset != wantOffset || wzs[0].length != wantLength {
			t.Errorf("WriteZeroes(%d, %d), want (%d, %d)", wzs[0].offset, wzs[0].length, wantOffset, wantLength)
		}
		if got := runner.ioCmds[0].Result; got != wantResult {
			t.Errorf("WRITE_ZEROES(supported) commit Result = %d, want %d", got, wantResult)
		}
	})
}

// ---------- 6. Backend error -> Result -5 ----------

// A backend failure must surface to the kernel as the hard-coded -5 (-EIO) in
// the COMMIT result, regardless of the backend's actual error text or which op
// failed (READ and WRITE are both exercised).
func TestHandleIORequestBackendErrorSurfacesAsEIO(t *testing.T) {
	cases := []struct {
		name    string
		op      uint8
		errText string
	}{
		{"read", uapi.UBLK_IO_OP_READ, "mock read error"},
		{"write", uapi.UBLK_IO_OP_WRITE, "mock write error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := newMockBackend(256 * 1024)
			runner, ring, descMem, _ := newIOHarness(t, 2, backend)

			if tc.op == uapi.UBLK_IO_OP_READ {
				backend.setReadError(errors.New(tc.errText))
			} else {
				backend.mu.Lock()
				backend.writeErr = errors.New(tc.errText)
				backend.mu.Unlock()
			}

			putDesc(t, descMem, 0, iodesc(tc.op, 0, 2))
			setOwned(runner, 0)

			loaded := runner.loadDescriptor(0)
			err := runner.handleIORequest(0, loaded)

			// PINNED FINDING (divergence from the task brief): a backend I/O
			// error is encoded into the COMMIT result (-EIO) rather than
			// returned, and submitCommitAndFetch returns nil on success, so
			// handleIORequest returns nil here too. Propagating the error would
			// be fatal to processIOAndCommit/ioLoop, taking down the whole
			// queue for one failed I/O.
			if err != nil {
				t.Fatalf("handleIORequest should fold the I/O error into COMMIT and return nil, got %v", err)
			}

			if got := ring.prepareCount(); got != 1 {
				t.Fatalf("PrepareIOCmd calls = %d, want 1", got)
			}
			if got := runner.ioCmds[0].Result; got != -5 {
				t.Errorf("commit Result = %d, want -5 (-EIO) for backend error %q", got, tc.errText)
			}
			if runner.tagStates[0] != TagStateInFlightCommit {
				t.Errorf("tag 0 state = %d, want TagStateInFlightCommit", runner.tagStates[0])
			}
		})
	}
}

// ---------- 7. Multi-tag buffer isolation ----------

// With depth >= 2, two simultaneous READs on different tags must land in
// disjoint 64KB regions: bufOffset := tag * IOBufferSizePerTag.
func TestHandleIORequestTagBuffersAreIsolated(t *testing.T) {
	const depth = 2
	backend := newMockBackend(256 * 1024)
	runner, ring, descMem, bufMem := newIOHarness(t, depth, backend)

	const n = int(uapi.SectorSize) // NrSectors=1 -> 512 bytes
	patternA := patternBytes(0x10, n)
	patternB := patternBytes(0x90, n)

	// Tag 0 reads StartSector=8 (offset 4096); tag 1 reads StartSector=16 (offset 8192).
	if _, err := backend.WriteAt(patternA, 8*uapi.SectorSize); err != nil {
		t.Fatalf("pre-fill A: %v", err)
	}
	if _, err := backend.WriteAt(patternB, 16*uapi.SectorSize); err != nil {
		t.Fatalf("pre-fill B: %v", err)
	}

	putDesc(t, descMem, 0, iodesc(uapi.UBLK_IO_OP_READ, 8, 1))
	putDesc(t, descMem, 1, iodesc(uapi.UBLK_IO_OP_READ, 16, 1))

	setOwned(runner, 0)
	setOwned(runner, 1)

	if err := runner.handleIORequest(0, runner.loadDescriptor(0)); err != nil {
		t.Fatalf("READ tag 0: %v", err)
	}
	if err := runner.handleIORequest(1, runner.loadDescriptor(1)); err != nil {
		t.Fatalf("READ tag 1: %v", err)
	}

	// Tag 0's 64KB region starts at byte 0; tag 1's at 1*IOBufferSizePerTag.
	tag0Region := bufMem[0:n]
	tag1Region := bufMem[constants.IOBufferSizePerTag : constants.IOBufferSizePerTag+n]

	if !bytes.Equal(tag0Region, patternA) {
		t.Errorf("tag 0 region: got %x want %x", tag0Region, patternA)
	}
	if !bytes.Equal(tag1Region, patternB) {
		t.Errorf("tag 1 region: got %x want %x", tag1Region, patternB)
	}
	// No cross-contamination in either direction.
	if bytes.Equal(tag0Region, patternB) {
		t.Error("tag 0 buffer contaminated with tag 1's pattern")
	}
	if bytes.Equal(tag1Region, patternA) {
		t.Error("tag 1 buffer contaminated with tag 0's pattern")
	}

	if got := runner.ioCmds[0].Result; got != int32(uapi.SectorSize) {
		t.Errorf("tag 0 commit Result = %d, want 512", got)
	}
	if got := runner.ioCmds[1].Result; got != int32(uapi.SectorSize) {
		t.Errorf("tag 1 commit Result = %d, want 512", got)
	}
	if got := ring.prepareCount(); got != 2 {
		t.Errorf("PrepareIOCmd calls = %d, want 2", got)
	}
}

// ---------- 8. submitCommitAndFetch refuses a non-Owned tag ----------

func TestSubmitCommitAndFetchRequiresOwned(t *testing.T) {
	backend := newMockBackend(256 * 1024)
	runner, ring, _, _ := newIOHarness(t, 2, backend)

	// Put tag 0 somewhere other than Owned (InFlightFetch) and call
	// submitCommitAndFetch DIRECTLY, bypassing handleIORequest entirely.
	runner.tagMutexes[0].Lock()
	runner.tagStates[0] = TagStateInFlightFetch
	runner.tagMutexes[0].Unlock()

	desc := iodesc(uapi.UBLK_IO_OP_READ, 0, 1)
	err := runner.submitCommitAndFetch(0, nil, desc)
	if err == nil {
		t.Fatal("submitCommitAndFetch should refuse a tag not in Owned state")
	}
	if !strings.Contains(err.Error(), "not Owned") {
		t.Errorf("refusal error = %q, want mention of 'not Owned'", err)
	}

	// The ring must NOT have been asked to prepare anything, and the tag state
	// must be unchanged (no transition on refusal).
	if got := ring.prepareCount(); got != 0 {
		t.Errorf("PrepareIOCmd calls = %d, want 0 (refused before preparing)", got)
	}
	if runner.tagStates[0] != TagStateInFlightFetch {
		t.Errorf("tag 0 state = %d, want TagStateInFlightFetch (unchanged)", runner.tagStates[0])
	}
}
