// This file tests loopBackend, the example backend that exports a regular
// file as a block device. Unlike the in-memory backends elsewhere in the
// repo, this one wraps a real *os.File, so the tests create and destroy real
// files on disk.
//
// Rule: no writes outside the repo clone. The scratch directories are created
// with os.MkdirTemp(".", ...) — "." is the package directory, since "go test"
// runs with its working directory set there — and are removed by t.Cleanup.
package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// scratchDir creates a throwaway directory inside the package directory (never
// the OS temp dir, which is outside the repo clone) and removes it when the
// test finishes.
func scratchDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", "looptest-")
	if err != nil {
		t.Fatalf("os.MkdirTemp inside package dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// patternBytes returns n bytes in 1..255, so that no byte is ever 0: a caller
// can distinguish "pattern data" from "hole/zeroed data" unambiguously.
func patternBytes(n int, seed int64) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(1 + ((i + int(seed)) % 255))
	}
	return p
}

// deterministicRNG is a minimal LCG so tests-using-random-offsets are
// reproducible across runs.
type deterministicRNG struct{ state uint64 }

func newDeterministicRNG() *deterministicRNG {
	return &deterministicRNG{state: 0x9E3779B97F4A7C15}
}

func (r *deterministicRNG) Int63n(n int64) int64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return int64(r.state % uint64(n))
}

// TestOpenLoopFreshPath covers openLoop on a path that does not exist yet:
// the file is created, the reported Size() is the requested size rounded DOWN
// to a 512-byte multiple, and a freshly created (sparse) backend reads back as
// zeros everywhere.
func TestOpenLoopFreshPath(t *testing.T) {
	dir := scratchDir(t)
	path := dir + "/fresh.bin"

	// Deliberately NOT a multiple of 512, to actually exercise the rounding.
	const requested = 1024*1024 + 100
	backend, err := openLoop(path, requested, false, false)
	if err != nil {
		t.Fatalf("openLoop: %v", err)
	}
	defer backend.Close()

	// 100 < 512, so (2^20+100) rounds down to 2^20.
	expectedSize := int64(1024 * 1024)
	if backend.Size() != expectedSize {
		t.Errorf("Size() = %d, want %d", backend.Size(), expectedSize)
	}

	// The raw file on disk keeps the requested size (the Truncate happens
	// before rounding); only the device size is rounded.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat backing file: %v", err)
	}
	if info.Size() != requested {
		t.Errorf("on-disk file size = %d, want %d", info.Size(), requested)
	}

	// Reading anywhere in a fresh backend returns zeros: the OS gives us a
	// sparse zero-filled file.
	for _, off := range []int64{0, 512, 4096, backend.Size() - 512} {
		buf := make([]byte, 512)
		n, err := backend.ReadAt(buf, off)
		if err != nil {
			t.Errorf("ReadAt(off=%d): %v", off, err)
			continue
		}
		if n != len(buf) {
			t.Errorf("ReadAt(off=%d) read %d bytes, want %d", off, n, len(buf))
		}
		for i, b := range buf {
			if b != 0 {
				t.Errorf("fresh read at off=%d byte %d = %d, want 0", off, i, b)
			}
		}
	}
}

// TestOpenLoopGrowsExistingFile covers openLoop on a file that already exists
// but is smaller than the requested size: it must be grown. Two independent
// checks of the same fact: the backend's Size() and the real file's on-disk
// size via os.Stat.
func TestOpenLoopGrowsExistingFile(t *testing.T) {
	dir := scratchDir(t)
	path := dir + "/small.bin"

	const requested = int64(1024 * 1024) // already a 512 multiple; no rounding noise

	front := []byte("pre-existing contents")
	if err := os.WriteFile(path, []byte("pre-existing contents that are far shorter than the requested size"), 0o600); err != nil {
		t.Fatalf("seed backing file: %v", err)
	}
	if info, _ := os.Stat(path); info.Size() >= requested {
		t.Fatalf("test precondition: seed file should be smaller than %d", requested)
	}

	backend, err := openLoop(path, requested, false, false)
	if err != nil {
		t.Fatalf("openLoop: %v", err)
	}
	defer backend.Close()

	// Check 1: the backend reports the requested size.
	if backend.Size() != requested {
		t.Errorf("Size() = %d, want %d", backend.Size(), requested)
	}
	// Check 2: the real file on disk actually grew to match.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat backing file: %v", err)
	}
	if info.Size() != requested {
		t.Errorf("on-disk file size = %d, want %d", info.Size(), requested)
	}

	// Data near the front of the pre-existing file survived the growth.
	buf := make([]byte, len(front))
	if n, err := backend.ReadAt(buf, 0); err != nil || n != len(front) {
		t.Fatalf("ReadAt front: n=%d err=%v", n, err)
	}
	if !bytes.Equal(buf, front) {
		t.Errorf("front of file = %q, want %q", buf, front)
	}
}

// TestOpenLoopReadOnlyCannotGrow covers openLoop with readOnly=true on a file
// smaller than the requested size: it must fail rather than truncate/write
// through a read-only fd, and the file must be left untouched on disk.
func TestOpenLoopReadOnlyCannotGrow(t *testing.T) {
	dir := scratchDir(t)
	path := dir + "/ro.bin"

	const requested = int64(1024 * 1024)
	seed := []byte("small file")
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		t.Fatalf("seed read-only backing file: %v", err)
	}

	backend, err := openLoop(path, requested, true, false)
	if err == nil {
		backend.Close()
		t.Fatal("openLoop readOnly on a file smaller than the requested size should fail")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("error = %q, want a read-only grow refusal", err)
	}

	// The file must not have grown: a read-only open cannot legally truncate.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat read-only backing file: %v", err)
	}
	if info.Size() != int64(len(seed)) {
		t.Errorf("read-only open changed on-disk size to %d, want %d", info.Size(), len(seed))
	}
}

// TestOpenLoopRoundsDownToZeroFails covers openLoop when the size it would
// report rounds down to zero logical blocks. It must return an error rather
// than silently producing a zero- (or negative-) capacity device.
func TestOpenLoopRoundsDownToZeroFails(t *testing.T) {
	dir := scratchDir(t)

	t.Run("freshPathSubBlockSize", func(t *testing.T) {
		backend, err := openLoop(dir+"/tiny.bin", 100, false, false)
		if err == nil {
			backend.Close()
			t.Fatal("openLoop with a sub-block requested size should fail, not yield a zero-capacity device")
		}
	})

	t.Run("freshPathZeroSize", func(t *testing.T) {
		// size=0 means "use the file's current size"; a fresh file is 0 bytes,
		// which also rounds down to zero.
		backend, err := openLoop(dir+"/empty.bin", 0, false, false)
		if err == nil {
			backend.Close()
			t.Fatal("openLoop on an empty file with size=0 should fail")
		}
	})

	t.Run("subBlockSizeOnBiggerFile", func(t *testing.T) {
		// The requested size takes precedence even when the file on disk is
		// bigger; a sub-block requested size still rounds to zero.
		path := dir + "/bigger.bin"
		if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
			t.Fatalf("seed file: %v", err)
		}
		backend, err := openLoop(path, 100, false, false)
		if err == nil {
			backend.Close()
			t.Fatal("openLoop with a sub-block requested size should fail even when the file is bigger")
		}
	})
}

// TestLoopRoundTrip is the differential-oracle test: keep a plain []byte
// shadow, apply the same writes to both backend and shadow, then compare.
// The OS page cache and the real filesystem sit between us and the bytes, so
// matching the shadow end-to-end is the strongest evidence the pread/pwrite
// offset arithmetic is right.
func TestLoopRoundTrip(t *testing.T) {
	dir := scratchDir(t)
	const size = int64(1024 * 1024)
	backend, err := openLoop(dir+"/roundtrip.bin", size, false, false)
	if err != nil {
		t.Fatalf("openLoop: %v", err)
	}
	defer backend.Close()

	shadow := make([]byte, size)

	// Deterministic set of writes: in-block, straddling a 512 boundary, full
	// device, and ending exactly at the device edge.
	ops := []struct {
		off int64
		len int
	}{
		{0, int(size)},        // whole device
		{100, 300},            // entirely within logical block 0
		{450, 200},            // straddles the 512 boundary
		{512*3 - 17, 34},      // straddles a later 512 boundary
		{size - 12345, 12345}, // ends exactly at the device edge
		{size - 1, 1},         // the very last byte
		{4096, 4096},          // exactly one filesystem block
		{100000, 55555},       // arbitrary interior range
		{17, 100000},          // long, unaligned on both ends
	}
	for _, op := range ops {
		if op.off+int64(op.len) > size {
			t.Fatalf("test bug: op off=%d len=%d exceeds size", op.off, op.len)
		}
		data := patternBytes(op.len, op.off)
		n, err := backend.WriteAt(data, op.off)
		if err != nil {
			t.Errorf("WriteAt(off=%d, len=%d): %v", op.off, op.len, err)
			continue
		}
		if n != op.len {
			t.Errorf("WriteAt(off=%d) wrote %d bytes, want %d", op.off, n, op.len)
			continue
		}
		copy(shadow[op.off:], data)

		got := make([]byte, op.len)
		n, err = backend.ReadAt(got, op.off)
		if err != nil {
			t.Errorf("ReadAt(off=%d): %v", op.off, err)
			continue
		}
		if n != op.len {
			t.Errorf("ReadAt(off=%d) read %d bytes, want %d", op.off, n, op.len)
			continue
		}
		if !bytes.Equal(got, data) {
			t.Errorf("read-back mismatch after WriteAt(off=%d, len=%d)", op.off, op.len)
		}
	}

	// Overlapping random writes on top, mirrored into the shadow.
	rng := newDeterministicRNG()
	for i := 0; i < 200; i++ {
		off := rng.Int63n(size)
		if off == size-1 {
			continue
		}
		maxLen := int(size - off)
		l := 1 + rng.Int63n(int64(maxLen))
		if l > 8192 {
			l = 8192
		}
		data := patternBytes(int(l), off)
		n, err := backend.WriteAt(data, off)
		if err != nil {
			t.Errorf("WriteAt(off=%d): %v", off, err)
			continue
		}
		if n != int(l) {
			t.Errorf("WriteAt(off=%d) wrote %d bytes, want %d", off, n, int(l))
			continue
		}
		copy(shadow[off:off+int64(l)], data)
	}

	// Full read must match the shadow byte for byte.
	full := make([]byte, size)
	n, err := backend.ReadAt(full, 0)
	if err != nil {
		t.Fatalf("final full ReadAt: %v", err)
	}
	if int64(n) != size {
		t.Fatalf("final full ReadAt read %d bytes, want %d", n, size)
	}
	for i, b := range full {
		if b != shadow[i] {
			t.Fatalf("full read-back mismatch at byte %d: got %d, want %d", i, b, shadow[i])
		}
	}
}

// TestReadPastEOFZerosWritePastSizeError pins the deliberate ReadAt/WriteAt
// asymmetry: ReadAt beyond what was written (but within Size()) returns zeros,
// while WriteAt at or beyond Size() returns an error and must not modify the
// device (the shadow is not updated for that call).
func TestReadPastEOFZerosWritePastSizeError(t *testing.T) {
	dir := scratchDir(t)
	const size = int64(4 * 1024 * 1024)
	backend, err := openLoop(dir+"/asym.bin", size, false, false)
	if err != nil {
		t.Fatalf("openLoop: %v", err)
	}
	defer backend.Close()

	// Write real data only in the first 4096 bytes.
	written := patternBytes(4096, 0)
	if n, err := backend.WriteAt(written, 0); err != nil || n != len(written) {
		t.Fatalf("WriteAt(0): n=%d err=%v", n, err)
	}

	t.Run("readPastWrittenButWithinSizeReturnsZeros", func(t *testing.T) {
		off := int64(4096 + 100) // past the written data, well inside Size()
		buf := make([]byte, 4096)
		n, err := backend.ReadAt(buf, off)
		if err != nil {
			t.Errorf("ReadAt past written data: %v (want zeros, not an error)", err)
		}
		if n != len(buf) {
			t.Errorf("ReadAt read %d bytes, want %d", n, len(buf))
		}
		for i, b := range buf {
			if b != 0 {
				t.Errorf("byte %d at off=%d = %d, want 0", i, off, b)
			}
		}
	})

	t.Run("writeAtExactlySizeReturnsError", func(t *testing.T) {
		data := []byte("must not be written")
		if n, err := backend.WriteAt(data, backend.Size()); err == nil {
			t.Errorf("WriteAt exactly at Size() = nil error, n=%d; want an error", n)
		}
	})

	t.Run("writeBeyondSizeReturnsError", func(t *testing.T) {
		data := []byte("must not be written")
		if n, err := backend.WriteAt(data, backend.Size()+37); err == nil {
			t.Errorf("WriteAt beyond Size() = nil error, n=%d; want an error", n)
		}
	})

	// The failed writes must not have updated anything: the region beyond the
	// initial 4096-byte write (all the way to the device end) is still zeros.
	t.Run("failedWritesDidNotTouchDevice", func(t *testing.T) {
		full := make([]byte, size)
		if n, err := backend.ReadAt(full, 0); err != nil || int64(n) != size {
			t.Fatalf("full ReadAt: n=%d err=%v", n, err)
		}
		for i, b := range full[4096:] {
			if b != 0 {
				t.Fatalf("byte %d (off %d) changed to %d after rejected writes; shadow should not be updated", i+4096, i+4096, b)
			}
		}
	})

	t.Run("readAtOrBeyondSizeReturnsEmpty", func(t *testing.T) {
		// The other half of the asymmetry: ReadAt at/after the device edge
		// returns (0, nil), not an error.
		for _, off := range []int64{backend.Size(), backend.Size() + 100} {
			buf := make([]byte, 512)
			n, err := backend.ReadAt(buf, off)
			if err != nil {
				t.Errorf("ReadAt(off=%d) = (%d, %v), want (0, nil)", off, n, err)
			}
			if n != 0 {
				t.Errorf("ReadAt(off=%d) read %d bytes, want 0", off, n)
			}
		}
	})

	t.Run("writeStraddlingEdgeIsTruncated", func(t *testing.T) {
		// A write that starts in bounds but extends past Size() is truncated
		// to the device, not rejected.
		off := backend.Size() - 100
		data := patternBytes(150, off)
		n, err := backend.WriteAt(data, off)
		if err != nil {
			t.Errorf("WriteAt straddling the edge: %v", err)
		}
		if n != 100 {
			t.Errorf("WriteAt straddling the edge wrote %d bytes, want 100", n)
		}
		got := make([]byte, 100)
		if m, err := backend.ReadAt(got, off); err != nil || m != 100 {
			t.Fatalf("ReadAt truncated tail: m=%d err=%v", m, err)
		}
		if !bytes.Equal(got, data[:100]) {
			t.Error("truncated tail read-back mismatch")
		}
	})
}

// TestDiscardAndWriteZeroes verifies that both Discard and WriteZeroes leave
// the target range reading back as zeros while leaving the surrounding bytes
// untouched. The range is deliberately UNALIGNED to the filesystem block size
// so a partial-block punch/zero cannot silently corrupt a shared block's other
// half: the whole device is compared against a shadow in which only the
// requested range is zeroed, so any clobbered neighbour shows up.
func TestDiscardAndWriteZeroes(t *testing.T) {
	dir := scratchDir(t)

	// Neither edge is a multiple of 4096 (filesystem block size).
	const (
		zeroOff = 1234
		zeroLen = 4321
	)

	seedWholeDevice := func(t *testing.T, l *loopBackend) []byte {
		t.Helper()
		expected := patternBytes(int(l.Size()), 0)
		n, err := l.WriteAt(expected, 0)
		if err != nil || n != len(expected) {
			t.Fatalf("seed pattern: n=%d err=%v", n, err)
		}
		return expected
	}

	verifyAgainst := func(t *testing.T, l *loopBackend, expected []byte, what string) {
		t.Helper()
		got := make([]byte, l.Size())
		n, err := l.ReadAt(got, 0)
		if err != nil || int64(n) != l.Size() {
			t.Fatalf("read back after %s: n=%d err=%v", what, n, err)
		}
		for i, b := range got {
			if b != expected[i] {
				t.Fatalf("%s mismatch at byte %d: got %d, want %d", what, i, b, expected[i])
			}
		}
	}

	t.Run("Discard", func(t *testing.T) {
		l, err := openLoop(dir+"/discard.bin", 1024*1024, false, false)
		if err != nil {
			t.Fatalf("openLoop: %v", err)
		}
		defer l.Close()

		expected := seedWholeDevice(t, l)
		if err := l.Discard(zeroOff, zeroLen); err != nil {
			t.Fatalf("Discard: %v", err)
		}
		for i := zeroOff; i < zeroOff+zeroLen; i++ {
			expected[i] = 0
		}
		verifyAgainst(t, l, expected, "Discard")
	})

	t.Run("WriteZeroes", func(t *testing.T) {
		l, err := openLoop(dir+"/wz.bin", 1024*1024, false, false)
		if err != nil {
			t.Fatalf("openLoop: %v", err)
		}
		defer l.Close()

		expected := seedWholeDevice(t, l)
		if err := l.WriteZeroes(zeroOff, zeroLen); err != nil {
			t.Fatalf("WriteZeroes: %v", err)
		}
		for i := zeroOff; i < zeroOff+zeroLen; i++ {
			expected[i] = 0
		}
		verifyAgainst(t, l, expected, "WriteZeroes")
	})

	// Reusing ONE backend across many Discard calls exercises the SAME noPunch
	// latch across calls — exactly the production behaviour. (We cannot force
	// the EOPNOTSUPP path here; it depends on the underlying filesystem.)
	t.Run("repeatedDiscardsShareLatch", func(t *testing.T) {
		l, err := openLoop(dir+"/latch.bin", 1024*1024, false, false)
		if err != nil {
			t.Fatalf("openLoop: %v", err)
		}
		defer l.Close()

		expected := seedWholeDevice(t, l)
		for i := 0; i < 50; i++ {
			off := int64(i * 17) // unaligned, overlapping ranges
			const length = int64(9000)
			if err := l.Discard(off, length); err != nil {
				t.Fatalf("Discard #%d: %v", i, err)
			}
			for j := int64(0); j < length && off+j < int64(len(expected)); j++ {
				expected[off+j] = 0
			}
		}
		verifyAgainst(t, l, expected, "repeated Discard")
	})
}

// TestFlush verifies Flush calls f.Sync() and does not error on an open
// backend, and that data already written is still correct on read-back
// afterwards.
func TestFlush(t *testing.T) {
	dir := scratchDir(t)
	l, err := openLoop(dir+"/flush.bin", 1024*1024, false, false)
	if err != nil {
		t.Fatalf("openLoop: %v", err)
	}
	defer l.Close()

	data := patternBytes(8192, 7)
	if n, err := l.WriteAt(data, 0); err != nil || n != len(data) {
		t.Fatalf("WriteAt: n=%d err=%v", n, err)
	}

	if err := l.Flush(); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if err := l.Flush(); err != nil {
		t.Errorf("second Flush: %v", err)
	}

	got := make([]byte, len(data))
	if n, err := l.ReadAt(got, 0); err != nil || n != len(got) {
		t.Fatalf("ReadAt: n=%d err=%v", n, err)
	}
	if !bytes.Equal(got, data) {
		t.Error("read-back after Flush mismatch")
	}
}

// TestCloseSyncsDataToDisk verifies Close syncs before closing (the source
// calls f.Sync() first because Close does not flush the page cache). We do not
// call any method on the backend after Close (the source doesn't guard against
// it); instead we reopen the SAME path with a fresh openLoop and confirm the
// pre-Close data actually landed on disk.
func TestCloseSyncsDataToDisk(t *testing.T) {
	dir := scratchDir(t)
	path := dir + "/close.bin"

	const size = int64(1024 * 1024)
	l, err := openLoop(path, size, false, false)
	if err != nil {
		t.Fatalf("openLoop: %v", err)
	}

	data := patternBytes(65536, 0)
	const dataOff = int64(123456)
	if n, err := l.WriteAt(data, dataOff); err != nil || n != len(data) {
		t.Fatalf("WriteAt: n=%d err=%v", n, err)
	}

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening the file hits the disk, not the old fd's page cache.
	reopened, err := openLoop(path, 0, false, false)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	// size=0 means "use the file's current size", so size survived Close.
	if reopened.Size() != size {
		t.Errorf("reopened Size() = %d, want %d", reopened.Size(), size)
	}

	got := make([]byte, len(data))
	if n, err := reopened.ReadAt(got, dataOff); err != nil || n != len(got) {
		t.Fatalf("reopened ReadAt: n=%d err=%v", n, err)
	}
	if !bytes.Equal(got, data) {
		t.Error("data written before Close did not survive on disk")
	}
}

// TestSyncWritesO_DSYNC covers the -sync configuration (syncWrites=true, i.e.
// O_DSYNC). We cannot prove real crash durability without a power cut, but the
// O_DSYNC code path must function correctly for normal I/O and must not change
// the observable read/write contract.
func TestSyncWritesO_DSYNC(t *testing.T) {
	dir := scratchDir(t)
	path := dir + "/sync.bin"

	const size = int64(1024 * 1024)
	l, err := openLoop(path, size, false, true) // syncWrites=true -> O_DSYNC
	if err != nil {
		t.Fatalf("openLoop(syncWrites=true): %v", err)
	}
	defer l.Close()

	if l.Size() != size {
		t.Errorf("Size() = %d, want %d", l.Size(), size)
	}

	// Whole-device pattern across many block boundaries.
	data := patternBytes(int(size), 0)
	if n, err := l.WriteAt(data, 0); err != nil || n != len(data) {
		t.Fatalf("WriteAt: n=%d err=%v", n, err)
	}
	got := make([]byte, size)
	if n, err := l.ReadAt(got, 0); err != nil || int64(n) != size {
		t.Fatalf("ReadAt: n=%d err=%v", n, err)
	}
	for i, b := range got {
		if b != data[i] {
			t.Fatalf("O_DSYNC round-trip mismatch at byte %d: got %d, want %d", i, b, data[i])
		}
	}

	// A sub-range overwrite round-trips too.
	sub := patternBytes(30000, 3)
	const subOff = int64(512 * 37)
	if n, err := l.WriteAt(sub, subOff); err != nil || n != len(sub) {
		t.Fatalf("sub WriteAt: n=%d err=%v", n, err)
	}
	gotSub := make([]byte, len(sub))
	if n, err := l.ReadAt(gotSub, subOff); err != nil || n != len(gotSub) {
		t.Fatalf("sub ReadAt: n=%d err=%v", n, err)
	}
	if !bytes.Equal(gotSub, sub) {
		t.Error("O_DSYNC sub-range round-trip mismatch")
	}

	// Flush (f.Sync) still works on the O_DSYNC fd.
	if err := l.Flush(); err != nil {
		t.Errorf("Flush: %v", err)
	}
}
