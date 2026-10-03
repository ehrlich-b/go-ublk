package main

// Tests for memoryBackend (mem.go), the sharded-lock RAM backend that
// examples/ublk-mem defaults to.
//
// The oracle throughout is a plain []byte shadow of what the device should
// contain (same pattern as zip_test.go): every operation is applied to both
// the backend and the shadow, and full-device reads are compared.

import (
	"bytes"
	"sync"
	"testing"
)

// pattern returns a deterministic, seed-distinct byte string.
func pattern(n, seed int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(seed*31 + i*7)
	}
	return b
}

// memReadAll reads the whole device into a fresh slice.
func memReadAll(t *testing.T, m *memoryBackend) []byte {
	t.Helper()
	buf := make([]byte, m.Size())
	n, err := m.ReadAt(buf, 0)
	if err != nil {
		t.Fatalf("ReadAt(all): %v", err)
	}
	if int64(n) != m.Size() {
		t.Fatalf("ReadAt(all) n = %d, want %d", n, m.Size())
	}
	return buf
}

// verifyShadow reads the full device and compares it byte-for-byte against the
// shadow of what it should contain.
func verifyShadow(t *testing.T, m *memoryBackend, shadow []byte) {
	t.Helper()
	got := memReadAll(t, m)
	if bytes.Equal(got, shadow) {
		return
	}
	for i := range shadow {
		if i >= len(got) || got[i] != shadow[i] {
			t.Errorf("data mismatch at byte %d: got %#x, want %#x", i, got[i], shadow[i])
			return
		}
	}
	t.Errorf("data mismatch: got %d bytes, want %d", len(got), len(shadow))
}

func TestNewMemoryBackendReadsZero(t *testing.T) {
	const size = int64(3*shardSize + 7777) // 4 shards, the last one partial
	m := newMemoryBackend(size)

	if m.Size() != size {
		t.Errorf("Size() = %d, want %d", m.Size(), size)
	}

	buf := memReadAll(t, m)
	for i, b := range buf {
		if b != 0 {
			t.Errorf("fresh backend byte %d = %#x, want 0", i, b)
			break
		}
	}

	wantShards := (size + shardSize - 1) / shardSize
	if got := int64(len(m.shards)); got != wantShards {
		t.Errorf("shard count = %d, want %d", got, wantShards)
	}
}

func TestRoundTripWithinAndAcrossShards(t *testing.T) {
	// 4 shards, last one partial: [0,64K) [64K,128K) [128K,192K) [192K,192K+7777)
	const size = int64(3*shardSize + 7777)
	m := newMemoryBackend(size)
	shadow := make([]byte, size)

	fillAndCheck := func(off int64, data []byte, what string) {
		t.Helper()
		n, err := m.WriteAt(data, off)
		if err != nil || n != len(data) {
			t.Fatalf("WriteAt(%s off=%d len=%d) = (%d,%v)", what, off, len(data), n, err)
		}
		copy(shadow[off:off+int64(len(data))], data)
		verifyShadow(t, m, shadow)

		// Read the same range back and confirm it round-trips.
		got := make([]byte, len(data))
		n, err = m.ReadAt(got, off)
		if err != nil || n != len(data) {
			t.Fatalf("ReadAt(%s off=%d len=%d) = (%d,%v)", what, off, len(data), n, err)
		}
		if !bytes.Equal(got, data) {
			t.Errorf("%s round trip at off=%d does not match what was written", what, off)
		}
	}

	// Entirely inside shard 0.
	fillAndCheck(int64(1000), pattern(3000, 1), "within-shard")

	// Starts before and ends after a 64KB boundary: [64K-10, 64K+54).
	fillAndCheck(int64(shardSize-10), pattern(64, 2), "cross-shard")

	// Spans three shards with a full untouched shard in the middle:
	// [100, 100+2*64K+200) covers shard 0's tail, all of shard 1, and shard 2's
	// head.
	fillAndCheck(int64(100), pattern(2*shardSize+200, 3), "three-shard")
}

func TestShardRange(t *testing.T) {
	// Three full shards: [0,64K) [64K,128K) [128K,192K).
	//
	// shardRange(off,length): start = off/64K; end = (off+length-1)/64K (the
	// shard holding the LAST byte of the range), clamped to len(shards)-1.
	const size = int64(3 * shardSize)
	m := newMemoryBackend(size)
	if len(m.shards) != 3 {
		t.Fatalf("shard count = %d, want 3", len(m.shards))
	}

	cases := []struct {
		off, length        int64
		wantStart, wantEnd int
		why                string
	}{
		// Entirely inside shard 0: start = 0/64K = 0, end = (0+1000-1)/64K = 999/64K = 0.
		{0, 1000, 0, 0, "entirely inside shard 0"},
		// Starts at exactly 64K, the first byte of shard 1:
		// start = 64K/64K = 1, end = (64K+1-1)/64K = 64K/64K = 1.
		{shardSize, 1, 1, 1, "first byte of shard 1"},
		// Ends at exactly 64K-1, the last byte of shard 0:
		// start = 0, end = (0+64K-1)/64K = 65535/64K = 0.
		{0, shardSize, 0, 0, "ends at exactly shardSize-1 (last byte of shard 0)"},
		// Crosses the 0/1 boundary: [65535, 65537) touches both shards:
		// start = 65535/64K = 0, end = (65535+2-1)/64K = 65536/64K = 1.
		{65535, 2, 0, 1, "crosses shard 0/1 boundary"},
		// Ends at exactly 2*64K-1, the last byte of shard 1:
		// start = 64K/64K = 1, end = (64K+64K-1)/64K = 131071/64K = 1.
		{shardSize, shardSize, 1, 1, "ends at exactly 2*shardSize-1 (last byte of shard 1)"},
		// Length-1 read at the very last byte of the backend: no clamping is
		// actually needed here because (196607+1-1)/64K = 196607/64K = 2 already,
		// i.e. the last shard. The clamp line is what keeps it at 2 instead of 3.
		{size - 1, 1, 2, 2, "length-1 at the very last byte of the backend"},
		// Length 2 at the very last byte runs one byte past the device:
		// (196607+2-1)/64K = 196608/64K = 3 >= len(shards)=3, must clamp end to 2.
		{size - 1, 2, 2, 2, "runs past the last shard; end must clamp to len(shards)-1"},
	}

	for _, c := range cases {
		start, end := m.shardRange(c.off, c.length)
		if start != c.wantStart || end != c.wantEnd {
			t.Errorf("shardRange(off=%d,length=%d) = (%d,%d), want (%d,%d) [%s]",
				c.off, c.length, start, end, c.wantStart, c.wantEnd, c.why)
		}
	}
}

func TestUnalignedReadsWrites(t *testing.T) {
	const size = int64(3*shardSize + 7777)
	m := newMemoryBackend(size)
	shadow := make([]byte, size)

	// Offsets/lengths deliberately not aligned to any power of two.
	writes := []struct {
		off, length int64
		seed        int
	}{
		{1234, 5678, 1},
		{int64(shardSize) - 333, 7777, 2},   // crosses a boundary, unaligned
		{2*int64(shardSize) + 51515, 99, 3}, // deep into shard 2's tail
		{1, 1, 4},
		{200000, 42, 5}, // near the partial-tail end, still in bounds
	}
	for _, w := range writes {
		data := pattern(int(w.length), w.seed)
		n, err := m.WriteAt(data, w.off)
		if err != nil || int64(n) != w.length {
			t.Fatalf("WriteAt(off=%d,len=%d) = (%d,%v)", w.off, w.length, n, err)
		}
		copy(shadow[w.off:w.off+w.length], data)
	}
	verifyShadow(t, m, shadow)

	// Unaligned reads at the same spots must agree with the shadow.
	for _, w := range writes {
		buf := make([]byte, w.length)
		n, err := m.ReadAt(buf, w.off)
		if err != nil || int64(n) != w.length {
			t.Fatalf("ReadAt(off=%d,len=%d) = (%d,%v)", w.off, w.length, n, err)
		}
		if !bytes.Equal(buf, shadow[w.off:w.off+w.length]) {
			t.Errorf("unaligned read at off=%d does not match shadow", w.off)
		}
	}
}

func TestDiscardAndWriteZeroesIdentical(t *testing.T) {
	const size = int64(3*shardSize + 7777)
	// A sub-range that starts and ends mid-shard and spans the 0/1 boundary, so
	// neighbours on all sides must survive untouched.
	const dOff, dLen = int64(shardSize) / 3, int64(shardSize)

	data := pattern(int(size), 5)

	mD := newMemoryBackend(size)
	if n, err := mD.WriteAt(data, 0); err != nil || n != len(data) {
		t.Fatalf("seed write (discard): (%d,%v)", n, err)
	}
	if err := mD.Discard(dOff, dLen); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	mZ := newMemoryBackend(size)
	if n, err := mZ.WriteAt(data, 0); err != nil || n != len(data) {
		t.Fatalf("seed write (zeroes): (%d,%v)", n, err)
	}
	if err := mZ.WriteZeroes(dOff, dLen); err != nil {
		t.Fatalf("WriteZeroes: %v", err)
	}

	want := bytes.Clone(data)
	clear(want[dOff : dOff+dLen])

	gotD := memReadAll(t, mD)
	gotZ := memReadAll(t, mZ)
	for i := range want {
		if gotD[i] != want[i] {
			t.Errorf("Discard: byte %d = %#x, want %#x", i, gotD[i], want[i])
			break
		}
	}
	for i := range want {
		if gotZ[i] != want[i] {
			t.Errorf("WriteZeroes: byte %d = %#x, want %#x", i, gotZ[i], want[i])
			break
		}
	}
	// WriteZeroes is literally `return m.Discard(...)` (mem.go:126-128), so the
	// two must be observationally identical. If they diverge here, that is a
	// contradiction of the one-line delegation — a real finding, recorded, not
	// fixed.
	if !bytes.Equal(gotD, gotZ) {
		t.Errorf("Discard and WriteZeroes produced different results for the same inputs")
	}
}

func TestConcurrentShardedReadWriteNoRace(t *testing.T) {
	// 8 goroutines, one per shard, each hammering a disjoint byte range that
	// lives entirely inside its own shard. The ranges are shard-disjoint BY
	// CONSTRUCTION, so if `go test -race` reports anything it is a lock-window
	// bug in memoryBackend, not overlapping access from the test itself.
	const shards = 8
	const size = int64(shards * shardSize)
	const perLen = 4096
	const perOff = 100 // [i*64K+100, i*64K+4196) is fully inside shard i

	m := newMemoryBackend(size)

	expect := make([][]byte, shards)
	for i := 0; i < shards; i++ {
		expect[i] = pattern(perLen, i+1)
	}

	var wg sync.WaitGroup
	for i := 0; i < shards; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			off := int64(idx)*shardSize + perOff
			buf := make([]byte, perLen)
			for iter := 0; iter < 200; iter++ {
				if _, err := m.ReadAt(buf, off); err != nil {
					t.Errorf("goroutine %d read: %v", idx, err)
					return
				}
				if _, err := m.WriteAt(expect[idx], off); err != nil {
					t.Errorf("goroutine %d write: %v", idx, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	// After every goroutine finishes, each range must hold exactly what its
	// goroutine wrote (writes are idempotent, so the final state is
	// deterministic regardless of interleaving).
	for i := 0; i < shards; i++ {
		off := int64(i)*shardSize + perOff
		got := make([]byte, perLen)
		if _, err := m.ReadAt(got, off); err != nil {
			t.Fatalf("verify read shard %d: %v", i, err)
		}
		if !bytes.Equal(got, expect[i]) {
			t.Errorf("shard %d holds wrong data after concurrent access", i)
		}
	}
}

func TestBoundaryAndErrorBehaviour(t *testing.T) {
	const size = int64(3*shardSize + 1000) // 4 shards, last one partial
	m := newMemoryBackend(size)
	end := size

	// Reads at/after the end are short reads with no error (mem.go:40-43).
	for _, off := range []int64{end, end + 1, end + 4096} {
		n, err := m.ReadAt(make([]byte, 16), off)
		if n != 0 || err != nil {
			t.Errorf("ReadAt(off=%d) = (%d,%v), want (0,nil)", off, n, err)
		}
	}

	// Writes at/after the end are an error: "write beyond end of device"
	// (mem.go:64-67).
	for _, off := range []int64{end, end + 1} {
		if _, err := m.WriteAt([]byte{1, 2, 3}, off); err == nil {
			t.Errorf("WriteAt(off=%d) returned nil error, want beyond-end error", off)
		}
	}

	// Zero-length read/write in bounds are no-ops.
	if n, err := m.ReadAt(nil, 0); n != 0 || err != nil {
		t.Errorf("ReadAt(nil,0) = (%d,%v), want (0,nil)", n, err)
	}
	if n, err := m.WriteAt(nil, 0); n != 0 || err != nil {
		t.Errorf("WriteAt(nil,0) = (%d,%v), want (0,nil)", n, err)
	}
	if n, err := m.WriteAt(nil, end-1); n != 0 || err != nil {
		t.Errorf("WriteAt(nil,end-1) = (%d,%v), want (0,nil)", n, err)
	}

	// A write/read straddling the end is truncated to the tail (mem.go:45-48,
	// 69-72): the last 10 bytes of the device hold the first 10 of the pattern.
	data := pattern(20, 7)
	if n, err := m.WriteAt(data, end-10); n != 10 || err != nil {
		t.Errorf("tail WriteAt = (%d,%v), want (10,nil)", n, err)
	}
	buf := make([]byte, 20)
	if n, err := m.ReadAt(buf, end-10); n != 10 || err != nil {
		t.Errorf("tail ReadAt = (%d,%v), want (10,nil)", n, err)
	}
	if !bytes.Equal(buf[:10], data[:10]) {
		t.Errorf("tail read does not match what was written")
	}

	// Discard/WriteZeroes at or past the end are no-ops (mem.go:101-104).
	if err := m.Discard(end, 100); err != nil {
		t.Errorf("Discard(end,100) = %v, want nil", err)
	}
	if err := m.Discard(end+1000, shardSize); err != nil {
		t.Errorf("Discard(end+1000,64K) = %v, want nil", err)
	}
	if err := m.WriteZeroes(end, shardSize); err != nil {
		t.Errorf("WriteZeroes(end,64K) = %v, want nil", err)
	}

	// Discard straddling the end zeroes only the in-bounds part: a discard of
	// [end-5, end+995) runs past Size(), so it must be clamped to [end-5, end)
	// (mem.go:106-109) and zero exactly the last 5 bytes, nothing more.
	m2 := newMemoryBackend(size)
	seed := pattern(int(size), 3)
	if n, err := m2.WriteAt(seed, 0); err != nil || n != len(seed) {
		t.Fatalf("seed write: (%d,%v)", n, err)
	}
	if err := m2.Discard(end-5, 1000); err != nil {
		t.Fatalf("straddle Discard = %v", err)
	}
	expected := bytes.Clone(seed)
	clear(expected[end-5:])
	verifyShadow(t, m2, expected)

	// PIN (documented, not fixed): a zero-length write AT the end errors even
	// though a zero-length read AT the end is a clean no-op. ReadAt guards with
	// `off >= m.size -> short read` (mem.go:41) while WriteAt guards with
	// `off >= m.size -> error` (mem.go:65). For end==size this makes an
	// empty write at the end an error and an empty read at the end a success —
	// an asymmetry that is the literal source behaviour, recorded here.
}

func TestCloseAndFlush(t *testing.T) {
	const size = int64(123456)
	m := newMemoryBackend(size)

	if err := m.Flush(); err != nil {
		t.Errorf("Flush: %v", err)
	}

	if n, err := m.WriteAt(pattern(100, 1), 1000); err != nil || n != 100 {
		t.Fatalf("seed write: (%d,%v)", n, err)
	}

	if err := m.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// Close sets m.data = nil (mem.go:92-95). Size() reads only m.size, so it
	// still reports the device size after Close and does not panic.
	if m.Size() != size {
		t.Errorf("Size() after Close = %d, want %d", m.Size(), size)
	}

	// A hypothetical ReadAt/WriteAt after Close would panic on the nil m.data
	// slice at mem.go:55 / mem.go:79 whenever there are bytes to move, so they
	// are deliberately NOT called here. Close intentionally leaves only
	// Size() and Flush() safe to call.
	if err := m.Flush(); err != nil {
		t.Errorf("Flush after Close: %v", err)
	}
}
