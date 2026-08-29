// Package main implements the compressed RAM backend (zipBackend) for the
// ublk-mem example. This file contains the first tests ever written for it.
//
// The oracle is a plain []byte: every operation is applied to the device and to
// a shadow slice, and the whole device is compared against the shadow
// byte-for-byte after every step. No hand-derived constants anywhere.
//
// math/rand is used with FIXED seeds throughout so the suites are
// deterministic and reproducible.
package main

import (
	"bytes"
	"math/rand"
	"testing"
)

// zipState is the differential oracle: the device and its shadow, kept in lock
// step by the helpers below.
type zipState struct {
	b      *zipBackend
	shadow []byte
}

func newZipState(size int64) *zipState {
	return &zipState{b: newZipBackend(size), shadow: make([]byte, size)}
}

// verify reads the entire device and compares it to the shadow. Every helper
// ends with a verify, so a mismatch anywhere is caught at the op that caused it.
func (s *zipState) verify(t *testing.T, what string) {
	t.Helper()
	got := make([]byte, len(s.shadow))
	n, err := s.b.ReadAt(got, 0)
	if err != nil {
		t.Fatalf("%s: read whole device: %v", what, err)
	}
	if n != len(s.shadow) {
		t.Fatalf("%s: read whole device returned %d bytes, want %d", what, n, len(s.shadow))
	}
	if !bytes.Equal(got, s.shadow) {
		for i := range got {
			if got[i] != s.shadow[i] {
				t.Fatalf("%s: device != shadow, first diff at byte %d: got %#x, want %#x",
					what, i, got[i], s.shadow[i])
			}
		}
		t.Fatalf("%s: device != shadow", what)
	}
}

func (s *zipState) write(t *testing.T, off int64, p []byte, what string) {
	t.Helper()
	if off < 0 || off+int64(len(p)) > int64(len(s.shadow)) {
		t.Fatalf("%s: test bug: out-of-bounds write off=%d len=%d size=%d", what, off, len(p), len(s.shadow))
	}
	n, err := s.b.WriteAt(p, off)
	if err != nil || n != len(p) {
		t.Fatalf("%s: WriteAt(off=%d,len=%d)=(%d,%v)", what, off, len(p), n, err)
	}
	copy(s.shadow[off:], p)
	s.verify(t, what+" (after write)")
}

func (s *zipState) readCmp(t *testing.T, off, length int64, what string) {
	t.Helper()
	if off < 0 || off+length > int64(len(s.shadow)) {
		t.Fatalf("%s: test bug: out-of-bounds read off=%d n=%d size=%d", what, off, length, len(s.shadow))
	}
	got := make([]byte, length)
	n, err := s.b.ReadAt(got, off)
	if err != nil || int64(n) != length {
		t.Fatalf("%s: ReadAt(off=%d,n=%d)=(%d,%v)", what, off, length, n, err)
	}
	if !bytes.Equal(got, s.shadow[off:off+length]) {
		t.Fatalf("%s: read mismatch at off=%d", what, off)
	}
}

// zero applies WriteZeroes to both device and shadow. The observable semantics
// a caller can rely on: the covered range reads as zero and the bytes on either
// side are untouched.
func (s *zipState) zero(t *testing.T, off, length int64, what string) {
	t.Helper()
	if off < 0 || off+length > int64(len(s.shadow)) {
		t.Fatalf("%s: test bug: out-of-bounds zero off=%d n=%d size=%d", what, off, length, len(s.shadow))
	}
	if err := s.b.WriteZeroes(off, length); err != nil {
		t.Fatalf("%s: WriteZeroes: %v", what, err)
	}
	for i := off; i < off+length; i++ {
		s.shadow[i] = 0
	}
	s.verify(t, what+" (after WriteZeroes)")
}

// discard applies Discard to both device and shadow. Same observable semantics
// as WriteZeroes for this backend (see the Discard/WriteZeroes comments in
// zip.go: WriteZeroes is literally Discard; both zero the range). A length that
// runs past the end is clamped by the device (zip.go:190-192), which the shadow
// models here.
func (s *zipState) discard(t *testing.T, off, length int64, what string) {
	t.Helper()
	if off < 0 || off > int64(len(s.shadow)) {
		t.Fatalf("%s: test bug: out-of-bounds discard off=%d n=%d size=%d", what, off, length, len(s.shadow))
	}
	if err := s.b.Discard(off, length); err != nil {
		t.Fatalf("%s: Discard: %v", what, err)
	}
	end := off + length
	if end > int64(len(s.shadow)) {
		end = int64(len(s.shadow))
	}
	for i := off; i < end; i++ {
		s.shadow[i] = 0
	}
	s.verify(t, what+" (after Discard)")
}

// zipPattern returns a deterministic, non-constant pattern of length n.
func zipPattern(seed byte, n int) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = seed + byte(i)
	}
	return p
}

// zipExpectPanic asserts that fn panics (used for the negative-offset cases).
func zipExpectPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("%s: expected a runtime panic, got none", what)
		}
	}()
	fn()
}

// 1. A fresh backend reads as all zeroes, including across a chunk boundary,
// and Size() returns exactly what was passed in.
func TestZipFreshBackendReadsZero(t *testing.T) {
	size := int64(3*zipChunkSize + 1234)
	if s := newZipBackend(size).Size(); s != size {
		t.Fatalf("Size()=%d, want %d", s, size)
	}
	s := newZipState(size)
	s.verify(t, "fresh device")

	// Read a spread of ranges, including one spanning a chunk boundary.
	for _, r := range []struct{ off, n int64 }{
		{0, 1},
		{0, zipChunkSize},
		{zipChunkSize - 10, 40},
		{zipChunkSize, zipChunkSize},
		{size / 3, size / 3},
		{size - 1234, 1234},
	} {
		s.verify(t, "pre-read")
		got := make([]byte, r.n)
		n, err := s.b.ReadAt(got, r.off)
		if err != nil {
			t.Fatalf("ReadAt(off=%d,n=%d): %v", r.off, r.n, err)
		}
		if int64(n) != r.n {
			t.Fatalf("ReadAt(off=%d,n=%d) returned %d bytes", r.off, r.n, n)
		}
		if !bytes.Equal(got, make([]byte, r.n)) {
			t.Fatalf("fresh read at off=%d not zero", r.off)
		}
	}
	s.verify(t, "after coverage reads")
}

// 2. Round trip within a single chunk. Offsets and lengths are relative to
// zipChunkSize so the test is independent of its value.
func TestZipRoundTripWithinChunk(t *testing.T) {
	s := newZipState(int64(4 * zipChunkSize))
	off := int64(zipChunkSize / 4)
	p := zipPattern(0x41, zipChunkSize/2)
	s.write(t, off, p, "round trip")
	s.readCmp(t, off, int64(len(p)), "round trip read")
}

// 3. THE CHUNK-BOUNDARY CASE. A write that starts before and ends after a
// zipChunkSize boundary, then a write spanning three chunks with a full chunk
// (covered end-to-end, so it takes the skip-the-load path in WriteAt) between
// two partially covered ones.
func TestZipWriteAcrossChunkBoundary(t *testing.T) {
	s := newZipState(int64(4 * zipChunkSize))
	s.write(t, 0, zipPattern(0x10, 4*zipChunkSize), "prefill")

	// Start just before a boundary, end just after it.
	off := zipChunkSize - 100
	p := zipPattern(0x20, 200)
	s.write(t, int64(off), p, "across one boundary")
	s.readCmp(t, int64(off), int64(len(p)), "read back the spanning write")
	// And the byte that sits right at the boundary on each side.
	s.readCmp(t, int64(off+100-1), 1, "byte before boundary")
	s.readCmp(t, int64(off+100), 1, "byte after boundary")
}

func TestZipWriteSpanningThreeChunks(t *testing.T) {
	size := int64(4 * zipChunkSize)
	s := newZipState(size)
	// Prefill the whole device with pattern A so a full untouched chunk in the
	// middle has distinguishable content that must survive correctly.
	s.write(t, 0, zipPattern(0x30, int(size)), "prefill all")

	// Write B: starts in the middle of chunk 0, fully covers chunk 1, ends in
	// the middle of chunk 2. Chunk 1 is the full chunk in the middle and takes
	// the no-decompress overwrite path in WriteAt.
	bOff := int64(zipChunkSize / 2)
	bLen := 2 * zipChunkSize
	p := zipPattern(0x40, bLen)
	s.write(t, bOff, p, "span three chunks")

	// Chunks 0 and 2 split A|B or B|A at their write boundaries; chunk 1 is all B.
	s.readCmp(t, 0, bOff, "chunk 0 head stays A")
	s.readCmp(t, bOff, int64(bLen), "middle is all B")
	s.readCmp(t, bOff+int64(bLen), size-(bOff+int64(bLen)), "chunk 2 tail stays A")
}

// 4. Read-modify-write: overwrite a handful of bytes in the middle of a filled
// chunk and confirm the whole chunk differs only in those bytes.
func TestZipReadModifyWritePreservesSurroundings(t *testing.T) {
	s := newZipState(int64(8 * zipChunkSize))
	// Fill the first chunk completely (chunk 0 spans [0, zipChunkSize)).
	s.write(t, 0, zipPattern(0x50, zipChunkSize), "fill chunk 0")

	// Overwrite a few bytes in the middle.
	over := zipPattern(0x60, 17)
	off := int64(4100)
	s.write(t, off, over, "rmw small overwrite")

	// Whole chunk back: the shadow (updated only on [off, off+17)) is the
	// assertion that no other byte moved.
	s.readCmp(t, 0, zipChunkSize, "whole chunk after rmw")
}

// 5. Unaligned everything: fixed non-power-of-two offsets and lengths whose
// capacity is relative to zipChunkSize so the test works for any chunk size.
func TestZipUnalignedOffsetsAndLengths(t *testing.T) {
	s := newZipState(int64(8 * zipChunkSize))
	longLen := zipChunkSize + 12345 // deliberately not a multiple of 2
	for i, op := range []struct {
		off int64
		len int
	}{
		{1234, 5678},                // the canonical unaligned pair
		{zipChunkSize - 1234, 5678}, // straddles a boundary unaligned on both sides
		{zipChunkSize + 1234, 12345},
		{zipChunkSize + 321, longLen},
	} {
		if op.off+int64(op.len) > int64(8*zipChunkSize) {
			t.Fatalf("test bug: unaligned op %d out of bounds for chunk %d", i, zipChunkSize)
		}
		p := zipPattern(byte(0x70+i), op.len)
		s.write(t, op.off, p, "unaligned write")
	}
	s.readCmp(t, 1234, 5678, "unaligned reread 1")
	s.readCmp(t, zipChunkSize-1234, 5678, "unaligned reread 2")
	s.readCmp(t, zipChunkSize+1234, 12345, "unaligned reread 3")
	s.readCmp(t, zipChunkSize+321, int64(longLen), "unaligned reread 4")
}

// 6. Both flate paths round-trip byte-for-byte: highly compressible input and
// incompressible input (here math/rand with a FIXED seed, so the "random" data
// is reproducible and the test is deterministic).
func TestZipCompressibleAndIncompressible(t *testing.T) {
	s := newZipState(int64(4 * zipChunkSize))

	// Highly compressible: long runs of one byte. Lengths relative to
	// zipChunkSize so the test is chunk-size independent.
	comp := make([]byte, 3*zipChunkSize)
	for i := range comp {
		if i%97 < 90 {
			comp[i] = 0xAA
		} else {
			comp[i] = byte(i)
		}
	}
	s.write(t, 500, comp, "compressible write") // spans chunk boundaries
	s.readCmp(t, 500, int64(len(comp)), "compressible read")

	// Incompressible: math/rand with a FIXED seed (so this is reproducible).
	rng := rand.New(rand.NewSource(7))
	inc := make([]byte, 2*zipChunkSize)
	rng.Read(inc)
	s.write(t, int64(zipChunkSize+777), inc, "incompressible write") // spans chunks
	s.readCmp(t, int64(zipChunkSize+777), int64(len(inc)), "incompressible read")
}

// 7. WriteZeroes and Discard semantics.
//
// What the implementation claims (zip.go:226-230): WriteZeroes is exactly
// Discard — "zeroing and deallocating are the same operation". What Discard
// guarantees (zip.go:186-224): the covered range reads as zero afterwards and
// bytes on either side are untouched; a range that fully covers a chunk stores
// it as a hole, a partially covered chunk is zeroed through the normal store
// path.
//
// The observable contract we assert for both: after the call, reading
// [off, off+length) returns zeroes and nothing outside the range changed.
func TestZipWriteZeroes(t *testing.T) {
	s := newZipState(int64(4 * zipChunkSize))
	s.write(t, 0, zipPattern(0x11, int(s.b.size)), "seed device")

	// Partial within a chunk.
	s.zero(t, 10000, 5000, "WriteZeroes partial")
	// Spanning a chunk boundary (the RMW edges).
	s.zero(t, int64(zipChunkSize-100), 200, "WriteZeroes across boundary")
	// A whole chunk becomes a hole.
	s.zero(t, int64(2*zipChunkSize), zipChunkSize, "WriteZeroes full chunk")
	// The last (full) chunk.
	s.zero(t, int64(3*zipChunkSize), zipChunkSize, "WriteZeroes last chunk")
}

func TestZipDiscard(t *testing.T) {
	s := newZipState(int64(4 * zipChunkSize))
	s.write(t, 0, zipPattern(0x22, int(s.b.size)), "seed device")

	// Partial within a chunk.
	s.discard(t, 8000, 3000, "Discard partial")
	// Spanning two chunks.
	s.discard(t, int64(zipChunkSize-60), 120, "Discard across boundary")
	// A whole chunk.
	s.discard(t, int64(2*zipChunkSize), zipChunkSize, "Discard full chunk")
	// Unaligned at the very end.
	s.discard(t, int64(3*zipChunkSize), zipChunkSize/2+1, "Discard tail unaligned")
	// Discard of a range whose tail runs past size is clamped (zip.go:190-192).
	s.discard(t, int64(3*zipChunkSize)+zipChunkSize/2, 2*zipChunkSize, "Discard past size clamps")

	// Explicitly confirm the sides of the last discard are untouched.
	s.readCmp(t, 0, int64(3*zipChunkSize)+zipChunkSize/2, "everything before clamped discard")
}

// 8. Boundary and error behaviour. zip.go pins what is returns; we reason about
// what it should return, then assert reality. Deviations are pinned and NOT
// fixed, per the task.
func TestZipBoundaryAndErrorBehaviour(t *testing.T) {
	const size = int64(2 * zipChunkSize)
	b := newZipBackend(size)
	shadow := make([]byte, size)

	// Seed some data at the head and the tail so truncation is visible against
	// known content.
	head := zipPattern(0x11, 100)
	if n, err := b.WriteAt(head, 0); err != nil || n != len(head) {
		t.Fatalf("seed head: (%d,%v)", n, err)
	}
	copy(shadow[:100], head)
	tail := zipPattern(0x22, 50)
	if n, err := b.WriteAt(tail, size-50); err != nil || n != len(tail) {
		t.Fatalf("seed tail: (%d,%v)", n, err)
	}
	copy(shadow[size-50:], tail)

	// --- Zero-length operations ---
	if n, err := b.ReadAt(make([]byte, 0), 0); n != 0 || err != nil {
		t.Errorf("zero-length read at off=0 = (%d,%v), want (0,nil)", n, err)
	}
	if n, err := b.ReadAt(make([]byte, 0), size); n != 0 || err != nil {
		t.Errorf("zero-length read at off==size = (%d,%v), want (0,nil)", n, err)
	}
	if n, err := b.WriteAt(nil, 0); n != 0 || err != nil {
		t.Errorf("zero-length write at off=0 = (%d,%v), want (0,nil)", n, err)
	}
	// The implementation's off>=size guard fires before the length guard, so a
	// zero-length write at the end still errors. That matches "out of bounds".
	if n, err := b.WriteAt(nil, size); err == nil {
		t.Errorf("zero-length write at off==size = (%d,nil), want error", n)
	}

	// --- Writes starting at/after the end error ---
	if n, err := b.WriteAt([]byte{1}, size); err == nil {
		t.Errorf("write at off==size returned (%d,nil), want error", n)
	}
	if n, err := b.WriteAt([]byte{1}, size+1000); err == nil {
		t.Errorf("write at off>size returned (%d,nil), want error", n)
	}

	// --- PINNED: a write that would run past the end is silently truncated. ---
	// Implementation (zip.go:149-151): p is clipped to the in-bounds tail, the
	// in-bounds bytes are written, and WriteAt returns (10, nil) — a short write
	// with a nil error. io.WriterAt requires n < len(p) to come with a non-nil
	// error; a caller that does not inspect the return value silently loses the
	// tail of its write. SUSPECTED DEFECT.
	big := bytes.Repeat([]byte{0xEE}, 100)
	n, err := b.WriteAt(big, size-10)
	if n != 10 {
		t.Errorf("overlong write returned n=%d, want 10 (implementation clips)", n)
	}
	if err != nil {
		t.Errorf("overlong write returned err=%v, want nil per implementation", err)
	}
	for i := size - 10; i < size; i++ {
		shadow[i] = 0xEE
	}

	// --- PINNED: reads at or past the end return (0, nil), never io.EOF. ---
	// io.ReaderAt requires a read at or past the end to return io.EOF.
	if n, err := b.ReadAt(make([]byte, 10), size); n != 0 || err != nil {
		t.Errorf("read at off==size = (%d,%v), want (0,nil) per implementation", n, err)
	}
	if n, err := b.ReadAt(make([]byte, 10), size+1000); n != 0 || err != nil {
		t.Errorf("read at off>size = (%d,%v), want (0,nil) per implementation", n, err)
	}
	// A read that begins in-bounds and runs past the end: clipped to the tail,
	// returns (10, nil) with correct bytes — again n < len(p) with a nil error,
	// deviating from io.ReaderAt. Same SUSPECTED DEFECT class as the writes.
	rp := make([]byte, 100)
	n, err = b.ReadAt(rp, size-10)
	if n != 10 || err != nil {
		t.Errorf("overrunning read = (%d,%v), want (10,nil) per implementation", n, err)
	}
	if !bytes.Equal(rp[:10], shadow[size-10:]) {
		t.Errorf("overrunning read bytes = % x, want % x", rp[:10], shadow[size-10:])
	}

	// --- PINNED: negative offsets panic instead of returning an error. ---
	// With off=-5, pos/cast gives idx=0 and inChunk=-5, and chunk[-5:] (in
	// ReadAt) or chunk[-5:2] (in WriteAt) panics at runtime. A block device
	// should reject a negative offset with an error. SUSPECTED DEFECT.
	zipExpectPanic(t, "ReadAt negative offset", func() { b.ReadAt(make([]byte, 10), -5) })
	zipExpectPanic(t, "WriteAt negative offset", func() { b.WriteAt([]byte{1, 2, 3}, -5) })
}

// 9. Flush is safe to call, returns nil, and does not disturb contents.
func TestZipFlushIsSafe(t *testing.T) {
	s := newZipState(int64(3 * zipChunkSize))
	p := zipPattern(0x63, 5000)
	s.write(t, 1000, p, "seed before flush")
	s.readCmp(t, 1000, int64(len(p)), "pre-flush read")

	for i := 0; i < 10; i++ {
		if err := s.b.Flush(); err != nil {
			t.Fatalf("Flush iteration %d: %v", i, err)
		}
	}
	s.verify(t, "after repeated flushes")
}

// A longer randomized differential run: a stream of Writes, WriteZeroes and
// Discards at arbitrary offsets, cross-checked against the shadow each time.
// math/rand with a FIXED seed, so this is reproducible. No goroutines, no
// timing, no ratio claims — correctness only.
func TestZipRandomDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	s := newZipState(int64(6 * zipChunkSize))

	do := func(off, length int64, what string) {
		if off < 0 || off+length > int64(len(s.shadow)) || length <= 0 {
			t.Fatalf("test bug: bad op %s off=%d length=%d size=%d", what, off, length, len(s.shadow))
		}
		switch rng.Intn(3) {
		case 0:
			p := make([]byte, length)
			rng.Read(p)
			s.write(t, off, p, what)
		case 1:
			s.zero(t, off, length, what)
		default:
			s.discard(t, off, length, what)
		}
	}

	// Phase 1: uniform random offsets and lengths up to ~1.1 chunks.
	for i := 0; i < 300; i++ {
		off := rng.Int63n(int64(len(s.shadow)))
		maxLen := int64(len(s.shadow)) - off
		if maxLen > 70000 {
			maxLen = 70000
		}
		length := rng.Int63n(maxLen) + 1
		do(off, length, "rand op")
	}

	// Phase 2: explicitly hammer the chunk boundaries (within 150 bytes before
	// each internal boundary, lengths that cross it).
	for i := 0; i < 150; i++ {
		k := rng.Intn(5) + 1
		off := int64(k)*zipChunkSize - int64(rng.Intn(150))
		length := int64(rng.Intn(150)) + 1
		if off+length > int64(len(s.shadow)) {
			continue
		}
		do(off, length, "boundary op")
	}
}
