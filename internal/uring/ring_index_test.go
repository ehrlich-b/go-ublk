package uring

// ring_index_test.go drives the ring-index arithmetic of IoUring (GetSQE,
// flushSQ, PeekCQE, CQAdvance) on a synthetic ring laid over plain Go byte
// slices standing in for the kernel-shared mappings. No syscall is made: the
// methods under test only read and write through the ring pointers, which
// behave identically over a byte slice. The tests play the kernel by writing
// the SQ head, CQ tail and CQEs directly.
//
// Layout (little-endian u32):
//
//	SQ ring: head @0, tail @4, flags @8
//	CQ ring: head @0, tail @4, overflow @8, cqes @16

import (
	"encoding/binary"
	"math"
	"testing"
	"unsafe"
)

const ringTestEntries = 4

type syntheticRing struct {
	*IoUring
	sq, sqes, cq []byte
}

func newSyntheticRing(sqEntries, cqEntries uint32) *syntheticRing {
	s := &syntheticRing{
		sq:   make([]byte, 16),
		sqes: make([]byte, int(sqEntries)*64),
		cq:   make([]byte, 16+int(cqEntries)*16),
	}
	sq, cq := unsafe.Pointer(&s.sq[0]), unsafe.Pointer(&s.cq[0])
	s.IoUring = &IoUring{
		sqHead:     (*uint32)(sq),
		sqTail:     (*uint32)(unsafe.Add(sq, 4)),
		sqFlags:    (*uint32)(unsafe.Add(sq, 8)),
		sqMask:     sqEntries - 1,
		sqEntries:  sqEntries,
		sqes:       unsafe.Pointer(&s.sqes[0]),
		cqHead:     (*uint32)(cq),
		cqTail:     (*uint32)(unsafe.Add(cq, 4)),
		cqOverflow: (*uint32)(unsafe.Add(cq, 8)),
		cqMask:     cqEntries - 1,
		cqEntries:  cqEntries,
		cqes:       unsafe.Add(cq, 16),
	}
	return s
}

func (s *syntheticRing) setSQ(head, tail uint32) {
	binary.LittleEndian.PutUint32(s.sq[0:], head)
	binary.LittleEndian.PutUint32(s.sq[4:], tail)
	s.sqeTail = tail
}

func (s *syntheticRing) sharedSQTail() uint32 { return binary.LittleEndian.Uint32(s.sq[4:]) }

// postCQE plays the kernel: writes a CQE at the tail and bumps it.
func (s *syntheticRing) postCQE(userData uint64, res int32) {
	tail := binary.LittleEndian.Uint32(s.cq[4:])
	slot := 16 + int(tail&s.cqMask)*16
	binary.LittleEndian.PutUint64(s.cq[slot:], userData)
	binary.LittleEndian.PutUint32(s.cq[slot+8:], uint32(res))
	binary.LittleEndian.PutUint32(s.cq[4:], tail+1)
}

// GetSQE hands out exactly sqEntries slots, wrapping through the mask, and
// returns nil (without advancing) when the ring is full. The kernel frees
// slots by advancing the head.
func TestGetSQEFillRejectAndWrap(t *testing.T) {
	s := newSyntheticRing(ringTestEntries, ringTestEntries)
	for i := uint32(0); i < ringTestEntries; i++ {
		sqe := s.GetSQE()
		if sqe == nil {
			t.Fatalf("GetSQE %d: nil on a ring with space", i)
		}
		if want := unsafe.Pointer(&s.sqes[i*64]); unsafe.Pointer(sqe) != want {
			t.Fatalf("GetSQE %d: slot %p, want %p", i, sqe, want)
		}
		sqe.Opcode = 0x7F
	}
	if sqe := s.GetSQE(); sqe != nil {
		t.Fatal("GetSQE returned an SQE from a full ring")
	}
	if s.sqeTail != ringTestEntries {
		t.Fatalf("rejected GetSQE moved the local tail to %d", s.sqeTail)
	}
	if got := s.flushSQ(); got != ringTestEntries || s.sharedSQTail() != ringTestEntries {
		t.Fatalf("flushSQ = %d with shared tail %d, want %d and %d", got, s.sharedSQTail(), ringTestEntries, ringTestEntries)
	}
	// Kernel consumed one: the next SQE wraps to slot 4&3 = 0 and is zeroed.
	binary.LittleEndian.PutUint32(s.sq[0:], 1)
	sqe := s.GetSQE()
	if sqe == nil || unsafe.Pointer(sqe) != unsafe.Pointer(&s.sqes[0]) {
		t.Fatalf("post-drain GetSQE = %p, want slot 0 %p", sqe, &s.sqes[0])
	}
	if s.sqes[0] != 0 {
		t.Fatalf("reused slot not zeroed: opcode %#x", s.sqes[0])
	}
}

// With the shared tail at MaxUint32 and the head entries-1 behind it there is
// exactly one free slot by unsigned subtraction; taking it wraps the tail to 0.
func TestGetSQEWraparoundAt32Bit(t *testing.T) {
	s := newSyntheticRing(ringTestEntries, ringTestEntries)
	s.setSQ(math.MaxUint32-ringTestEntries+1, math.MaxUint32)
	sqe := s.GetSQE()
	if sqe == nil {
		t.Fatal("GetSQE: nil with one free slot at the 32-bit boundary")
	}
	if want := unsafe.Pointer(&s.sqes[(math.MaxUint32&(ringTestEntries-1))*64]); unsafe.Pointer(sqe) != want {
		t.Fatalf("boundary SQE at %p, want %p", sqe, want)
	}
	if s.GetSQE() != nil {
		t.Fatal("GetSQE: second SQE from a ring that was one slot from full")
	}
	if got := s.flushSQ(); got != ringTestEntries || s.sharedSQTail() != 0 {
		t.Fatalf("flushSQ = %d, shared tail %d; want %d and 0 (MaxUint32+1 wraps)", got, s.sharedSQTail(), ringTestEntries)
	}
}

// Full detection straddling the boundary: tail 2, head MaxUint32-1 is a
// distance of 4 (full), not "tail behind head".
func TestGetSQEFullAt32BitBoundary(t *testing.T) {
	s := newSyntheticRing(ringTestEntries, ringTestEntries)
	s.setSQ(math.MaxUint32-1, 2)
	if s.GetSQE() != nil {
		t.Fatal("GetSQE accepted an SQE into a full ring at the 32-bit boundary")
	}
	s.setSQ(math.MaxUint32-1, 1)
	if s.GetSQE() == nil {
		t.Fatal("GetSQE rejected an SQE with one free slot at the 32-bit boundary")
	}
}

func TestPeekCQEAndAdvance(t *testing.T) {
	s := newSyntheticRing(ringTestEntries, ringTestEntries)
	if s.PeekCQE() != nil || s.CQReady() != 0 {
		t.Fatal("PeekCQE found a CQE in an empty ring")
	}
	s.postCQE(0xDEADBEEF, 1234)
	s.postCQE(2, -5)
	if got := s.CQReady(); got != 2 {
		t.Fatalf("CQReady = %d, want 2", got)
	}
	cqe := s.PeekCQE()
	if cqe == nil || cqe.UserData != 0xDEADBEEF || cqe.Res != 1234 {
		t.Fatalf("PeekCQE = %+v, want {0xdeadbeef 1234}", cqe)
	}
	if s.PeekCQE() != cqe {
		t.Fatal("PeekCQE consumed the CQE")
	}
	s.CQESeen()
	if got := binary.LittleEndian.Uint32(s.cq[0:]); got != 1 {
		t.Fatalf("shared CQ head = %d after CQESeen, want 1", got)
	}
	var batch [4]*CQE
	if n := s.PeekBatchCQE(batch[:]); n != 1 || batch[0].UserData != 2 || batch[0].Res != -5 {
		t.Fatalf("PeekBatchCQE = %d %+v, want 1 {2 -5}", n, batch[0])
	}
	s.CQAdvance(1)
	if s.PeekCQE() != nil {
		t.Fatal("PeekCQE found a CQE after consuming everything")
	}
}

// The CQ head wraps from MaxUint32 to 0 and the slot follows the mask.
func TestPeekCQEWraparoundAt32Bit(t *testing.T) {
	s := newSyntheticRing(ringTestEntries, ringTestEntries)
	binary.LittleEndian.PutUint32(s.cq[0:], math.MaxUint32)
	binary.LittleEndian.PutUint32(s.cq[4:], math.MaxUint32)
	s.cqHeadLocal = math.MaxUint32
	s.postCQE(0xBBBB, -5) // lands in slot MaxUint32&3 = 3; tail wraps to 0
	cqe := s.PeekCQE()
	if cqe == nil || cqe.UserData != 0xBBBB {
		t.Fatalf("PeekCQE across the boundary = %+v, want user_data 0xbbbb", cqe)
	}
	if want := unsafe.Pointer(&s.cq[16+3*16]); unsafe.Pointer(cqe) != want {
		t.Fatalf("boundary CQE at %p, want slot 3 %p", cqe, want)
	}
	s.CQESeen()
	if got := binary.LittleEndian.Uint32(s.cq[0:]); got != 0 {
		t.Fatalf("CQ head after the boundary = %d, want 0", got)
	}
}

// Internal fallback-timeout CQEs are invisible to PeekCQE, PeekBatchCQE and
// CQReady once the fallback has been used, and only then.
func TestFallbackTimeoutCQEsAreHidden(t *testing.T) {
	s := newSyntheticRing(ringTestEntries, 8)
	s.postCQE(timeoutUserData, -62)
	if cqe := s.PeekCQE(); cqe == nil || cqe.UserData != timeoutUserData {
		t.Fatal("reserved user_data hidden on a ring that never used the fallback")
	}
	s.fallbackUsed = true
	s.postCQE(1, 0)
	s.postCQE(timeoutUserData, 0)
	s.postCQE(2, 0)
	if got := s.CQReady(); got != 2 {
		t.Fatalf("CQReady = %d, want 2 user CQEs", got)
	}
	var batch [4]*CQE
	if n := s.PeekBatchCQE(batch[:]); n != 1 || batch[0].UserData != 1 {
		t.Fatalf("PeekBatchCQE = %d (first %+v), want 1 stopping before the timeout CQE", n, batch[0])
	}
	s.CQAdvance(1)
	if cqe := s.PeekCQE(); cqe == nil || cqe.UserData != 2 {
		t.Fatalf("PeekCQE = %+v, want user_data 2 past the hidden timeout", cqe)
	}
}
