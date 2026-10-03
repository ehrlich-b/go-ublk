package uring

// ring_index_test.go drives the ring-buffer index arithmetic of minimalRing
// (submitToRing, prepareSQE, pollCtrlCompletion) with hand-built synthetic
// rings over plain Go byte slices standing in for the normally-mmap'd ring
// memory. Neither the helpers nor the tests call NewMinimalRing/NewRing or any
// syscall: the functions under test only ever read/write through
// unsafe.Add(r.sqAddr/cqAddr/sqesAddr, offset), which is identical on a byte
// slice's backing array.
//
// Byte layout (little-endian u32):
//
//	SQ ring: head @0, tail @4, array entries @8 (4 bytes each)
//	         sqesBuf: sqEntries * 128-byte sqe128 slots
//	CQ ring: head @0, tail @4, cqes @8 (32 bytes each)
//
// The shared ring fields are written by the functions under test with
// atomic.StoreUint32 / read with atomic.LoadUint32 and direct deref; the tests
// read and drive those same bytes with encoding/binary on the backing slice.

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"unsafe"
)

const ringTestEntries = 4

// newTestSQRing returns a synthetic submission ring plus its backing buffers.
// The buffer slices are returned so callers keep them referenced for the whole
// test, exactly as the task requires: r's sqAddr/sqesAddr are unsafe.Pointer
// views into them, invisible to the GC.
func newTestSQRing(entries uint32) (*minimalRing, []byte, []byte) {
	sqBuf := make([]byte, 8+int(entries)*4)
	sqesBuf := make([]byte, int(entries)*128)
	var params io_uring_params
	params.sqEntries = entries
	params.sqOff.head = 0
	params.sqOff.tail = 4
	params.sqOff.array = 8
	return &minimalRing{
		sqAddr:   unsafe.Pointer(&sqBuf[0]),
		sqesAddr: unsafe.Pointer(&sqesBuf[0]),
		params:   params,
	}, sqBuf, sqesBuf
}

// newTestCQRing returns a synthetic completion ring plus its backing buffer.
func newTestCQRing(entries uint32) (*minimalRing, []byte) {
	cqBuf := make([]byte, 8+int(entries)*32)
	var params io_uring_params
	params.cqEntries = entries
	params.cqOff.head = 0
	params.cqOff.tail = 4
	params.cqOff.cqes = 8
	return &minimalRing{
		cqAddr: unsafe.Pointer(&cqBuf[0]),
		params: params,
	}, cqBuf
}

// TestSubmitToRingFillRejectAndWrap covers task items 1-3: fill to exactly
// sqEntries submissions with array[i] == i each time, verify the (entries+1)th
// is rejected without advancing tail, then drain one entry and confirm the
// next submission wraps to slot tail & mask.
func TestSubmitToRingFillRejectAndWrap(t *testing.T) {
	r, sqBuf, sqesBuf := newTestSQRing(ringTestEntries)
	var mask uint32 = ringTestEntries - 1

	// Item 1: fill exactly to capacity.
	for i := uint32(0); i < ringTestEntries; i++ {
		if err := r.submitToRing(&sqe128{opcode: 46}); err != nil {
			t.Fatalf("submit %d: unexpected error: %v", i, err)
		}
		if got := binary.LittleEndian.Uint32(sqBuf[4:8]); got != i+1 {
			t.Fatalf("after submit %d: tail = %d, want %d", i, got, i+1)
		}
		if got := binary.LittleEndian.Uint32(sqBuf[8+4*i:]); got != i {
			t.Fatalf("after submit %d: array[%d] = %d, want %d", i, i, got, i)
		}
		if got := sqesBuf[i*128]; got != 46 {
			t.Fatalf("after submit %d: sqesBuf slot %d opcode = %d, want 46", i, i, got)
		}
	}

	// Item 2: the (entries+1)th submission is rejected, and tail is unchanged.
	// The source returns a plain fmt.Errorf here (not ErrRingFull), so any
	// non-nil error is the expected rejection; the invariant under test is that
	// tail does NOT move.
	tailBefore := binary.LittleEndian.Uint32(sqBuf[4:8])
	if err := r.submitToRing(&sqe128{opcode: 46}); err == nil {
		t.Fatal("expected submission queue full error, got nil")
	}
	if got := binary.LittleEndian.Uint32(sqBuf[4:8]); got != tailBefore {
		t.Fatalf("rejected full-queue submission advanced tail: got %d, want %d", got, tailBefore)
	}

	// Item 3: drain one entry (kernel consumed it -> head = 1), then submit.
	// tail was 4, mask is 3, so the new SQE must land at slot 4&3 = 0, wrapping
	// from the physical end of the array back to its start.
	binary.LittleEndian.PutUint32(sqBuf[0:4], 1)
	tailBefore = binary.LittleEndian.Uint32(sqBuf[4:8]) // 4
	if err := r.submitToRing(&sqe128{opcode: 46}); err != nil {
		t.Fatalf("post-drain submit: unexpected error: %v", err)
	}
	if got := binary.LittleEndian.Uint32(sqBuf[4:8]); got != tailBefore+1 {
		t.Fatalf("post-drain tail = %d, want %d", got, tailBefore+1)
	}
	wrapIndex := tailBefore & mask // 0
	if got := binary.LittleEndian.Uint32(sqBuf[8+4*wrapIndex:]); got != wrapIndex {
		t.Fatalf("array[%d] after wrap = %d, want %d", wrapIndex, got, wrapIndex)
	}
	if got := sqesBuf[wrapIndex*128]; got != 46 {
		t.Fatalf("wrapped SQE opcode at slot %d = %d, want 46", wrapIndex, got)
	}
}

// TestPrepareSQEExhaustsToErrRingFull covers task item 4: prepareSQE uses the
// LOCAL tail (sqTailLocal) against the shared head and returns the ErrRingFull
// SENTINEL (matched with errors.Is) when exhausted, without advancing the local
// tail past capacity.
func TestPrepareSQEExhaustsToErrRingFull(t *testing.T) {
	r, sqBuf, sqesBuf := newTestSQRing(ringTestEntries)
	_ = sqBuf // shared head/tail stay at 0 (fresh ring)

	for i := uint32(0); i < ringTestEntries; i++ {
		if err := r.prepareSQE(&sqe128{opcode: 46}); err != nil {
			t.Fatalf("prepare %d: unexpected error: %v", i, err)
		}
		if got := r.sqTailLocal; got != i+1 {
			t.Fatalf("after prepare %d: sqTailLocal = %d, want %d", i, got, i+1)
		}
		// index-wise the prepared SQE went to slot tail & mask == tail.
		if got := sqesBuf[i*128]; got != 46 {
			t.Fatalf("prepare %d: sqesBuf slot %d opcode = %d, want 46", i, i, got)
		}
	}

	err := r.prepareSQE(&sqe128{opcode: 46})
	if !errors.Is(err, ErrRingFull) {
		t.Fatalf("expected exactly ErrRingFull, got %v", err)
	}
	if got := r.sqTailLocal; got != ringTestEntries {
		t.Fatalf("rejected prepare advanced sqTailLocal: got %d, want %d", got, ringTestEntries)
	}
}

// TestSubmitToRingWraparoundAt32Bit covers task item 5: with shared tail at
// math.MaxUint32 and head at MaxUint32 - sqEntries + 1 there is EXACTLY one
// free slot by unsigned subtraction; submitToRing must succeed and wrap tail
// to 0, writing its array entry at MaxUint32 & (sqEntries-1).
func TestSubmitToRingWraparoundAt32Bit(t *testing.T) {
	r, sqBuf, sqesBuf := newTestSQRing(ringTestEntries)

	head := uint32(math.MaxUint32) - ringTestEntries + 1
	binary.LittleEndian.PutUint32(sqBuf[0:4], head)
	binary.LittleEndian.PutUint32(sqBuf[4:8], math.MaxUint32)

	if err := r.submitToRing(&sqe128{opcode: 46}); err != nil {
		t.Fatalf("submit at 32-bit boundary: unexpected error: %v", err)
	}
	// hand-derived: MaxUint32 + 1 ≡ 0 (mod 2^32)
	if got := binary.LittleEndian.Uint32(sqBuf[4:8]); got != 0 {
		t.Fatalf("tail after MaxUint32 wrap = %d, want 0", got)
	}
	wantIndex := uint32(math.MaxUint32) & (ringTestEntries - 1)
	if got := binary.LittleEndian.Uint32(sqBuf[8+4*wantIndex:]); got != wantIndex {
		t.Fatalf("array[%d] at boundary = %d, want %d", wantIndex, got, wantIndex)
	}
	if got := sqesBuf[wantIndex*128]; got != 46 {
		t.Fatalf("boundary SQE opcode at slot %d = %d, want 46", wantIndex, got)
	}
}

// TestSQRingFullDetectionAt32BitBoundary covers task item 6: unsigned-subtraction
// full detection straddling the 32-bit boundary.
//
// NOTE on the spec's stated numbers: with tail = 2 and head = MaxUint32 - 1,
// the true logical distance is (2 - (MaxUint32-1)) ≡ 4 (mod 2^32) — exactly
// sqEntries for entries = 4, i.e. the ring is genuinely FULL, not below
// capacity as the task prose assumed. submitToRing must therefore report full
// (a false "has space" here would be the exact bug family this test hunts). We
// assert that, and separately drive the free-space side of the same boundary
// with distance 3 (< entries) to prove acceptance + correct wrap both hold.
func TestSQRingFullDetectionAt32BitBoundary(t *testing.T) {
	r, sqBuf, _ := newTestSQRing(ringTestEntries)

	// --- genuinely full at the boundary (distance == sqEntries) ---
	binary.LittleEndian.PutUint32(sqBuf[0:4], math.MaxUint32-1)
	binary.LittleEndian.PutUint32(sqBuf[4:8], 2)

	tail := binary.LittleEndian.Uint32(sqBuf[4:8])
	head := binary.LittleEndian.Uint32(sqBuf[0:4])
	distance := tail - head // uint32 arithmetic, exactly as submitToRing does
	// hand-derived: 2 - (2^32 - 2) = 4 - 2^32 ≡ 4 (mod 2^32); signed reading
	// ("tail < head") would look backwards by 2^32.
	if distance != ringTestEntries {
		t.Fatalf("hand-derive check: tail-head = %d, want %d", distance, ringTestEntries)
	}

	// A correct implementation must report full here (distance == entries).
	// Accepting would mean a false-negative full check at the boundary.
	if err := r.submitToRing(&sqe128{opcode: 46}); err == nil {
		t.Fatal("DEFECT: submitToRing accepted a submission into a genuinely full ring at the 32-bit boundary (distance == sqEntries)")
	}
	if got := binary.LittleEndian.Uint32(sqBuf[4:8]); got != tail {
		t.Fatalf("rejected boundary submit advanced tail: got %d, want %d", got, tail)
	}

	// --- free space straddling the boundary (distance 3 < entries) ---
	binary.LittleEndian.PutUint32(sqBuf[0:4], math.MaxUint32-1)
	binary.LittleEndian.PutUint32(sqBuf[4:8], 1)
	head = binary.LittleEndian.Uint32(sqBuf[0:4])
	tail = binary.LittleEndian.Uint32(sqBuf[4:8])
	distance = tail - head
	// hand-derived: 1 - (2^32 - 2) = 3 - 2^32 ≡ 3 (mod 2^32)
	if distance != ringTestEntries-1 {
		t.Fatalf("hand-derive check: free-side tail-head = %d, want %d", distance, ringTestEntries-1)
	}

	if err := r.submitToRing(&sqe128{opcode: 46}); err != nil {
		t.Fatalf("DEFECT: submitToRing rejected a submission into a ring with space at the 32-bit boundary (distance = %d < %d): %v", distance, ringTestEntries, err)
	}
	if got := binary.LittleEndian.Uint32(sqBuf[4:8]); got != tail+1 {
		t.Fatalf("free-side submit tail = %d, want %d", got, tail+1)
	}
	idx := tail & (ringTestEntries - 1)
	if got := binary.LittleEndian.Uint32(sqBuf[8+4*idx:]); got != idx {
		t.Fatalf("free-side array[%d] = %d, want %d", idx, got, idx)
	}
}

// TestPollCtrlCompletion covers task item 7 (basic): a hand-written CQE in slot
// 0 with tail=1/head=0 is consumed (head -> 1); polling with nothing new
// returns ok=false without moving head.
func TestPollCtrlCompletion(t *testing.T) {
	r, cqBuf := newTestCQRing(ringTestEntries)

	want := cqe32{userData: 0xDEADBEEF, res: 1234}
	*((*cqe32)(unsafe.Pointer(&cqBuf[8]))) = want // cqe slot 0: userData @8, res @16
	binary.LittleEndian.PutUint32(cqBuf[0:4], 0)  // head
	binary.LittleEndian.PutUint32(cqBuf[4:8], 1)  // tail

	res, ok := r.pollCtrlCompletion()
	if !ok {
		t.Fatal("pollCtrlCompletion: ok=false on non-empty CQ, want ok=true")
	}
	if res == nil {
		t.Fatal("pollCtrlCompletion: ok=true but nil Result (contract violation)")
	}
	if res.UserData() != 0xDEADBEEF || res.Value() != 1234 {
		t.Fatalf("consumed CQE = {userData:%#x res:%d}, want {0xdeadbeef 1234}", res.UserData(), res.Value())
	}
	if got := binary.LittleEndian.Uint32(cqBuf[0:4]); got != 1 {
		t.Fatalf("head after consume = %d, want 1", got)
	}

	// Nothing new: must not consume a phantom CQE nor move head.
	res, ok = r.pollCtrlCompletion()
	if ok || res != nil {
		t.Fatalf("second poll = {res:%v ok:%v}, want {nil false}", res, ok)
	}
	if got := binary.LittleEndian.Uint32(cqBuf[0:4]); got != 1 {
		t.Fatalf("head moved on empty poll: got %d, want 1", got)
	}
}

// TestPollCtrlCompletionWraparoundAt32Bit covers task item 7 (32-bit side):
// the CQ-side analogue of items 3 and 5.
//
//   - Scenario A (spec numbers): tail = MaxUint32 with one CQE present means
//     head = MaxUint32-1; head slot = (MaxUint32-1) & 3 = 2; consuming advances
//     head to MaxUint32.
//   - Scenario B (head -> 0 wrap): head = MaxUint32, tail = 0 (tail has wrapped
//     into the next cycle, distance = 1); head slot = MaxUint32 & 3 = 3;
//     consuming wraps head to 0. res = -5 also exercises the error path.
func TestPollCtrlCompletionWraparoundAt32Bit(t *testing.T) {
	const cqEntries = 4
	r, cqBuf := newTestCQRing(cqEntries)

	// Scenario A.
	*((*cqe32)(unsafe.Pointer(&cqBuf[8+2*32]))) = cqe32{userData: 0xAAAA, res: 42}
	binary.LittleEndian.PutUint32(cqBuf[0:4], math.MaxUint32-1)
	binary.LittleEndian.PutUint32(cqBuf[4:8], math.MaxUint32)

	res, ok := r.pollCtrlCompletion()
	if !ok {
		t.Fatal("CQ boundary poll A: ok=false, want ok=true (CQE at index 2)")
	}
	if res.UserData() != 0xAAAA || res.Value() != 42 {
		t.Fatalf("CQ boundary CQE = {%#x %d}, want {0xaaaa 42}", res.UserData(), res.Value())
	}
	if got := binary.LittleEndian.Uint32(cqBuf[0:4]); got != math.MaxUint32 {
		t.Fatalf("head after boundary consume = %d, want %d", got, uint32(math.MaxUint32))
	}

	// Scenario B.
	*((*cqe32)(unsafe.Pointer(&cqBuf[8+3*32]))) = cqe32{userData: 0xBBBB, res: -5}
	binary.LittleEndian.PutUint32(cqBuf[0:4], math.MaxUint32)
	binary.LittleEndian.PutUint32(cqBuf[4:8], 0)

	res, ok = r.pollCtrlCompletion()
	if !ok {
		t.Fatal("CQ wrap poll B: ok=false, want ok=true (CQE at index 3, head wraps to 0)")
	}
	if res.UserData() != 0xBBBB || res.Value() != -5 {
		t.Fatalf("CQ wrap CQE = {%#x %d}, want {0xbbbb -5}", res.UserData(), res.Value())
	}
	if res.Error() == nil {
		t.Fatal("expected result.Error() set for res = -5, got nil")
	}
	if got := binary.LittleEndian.Uint32(cqBuf[0:4]); got != 0 {
		t.Fatalf("head after wrap consume = %d, want 0 (MaxUint32 + 1 wraps)", got)
	}
}
