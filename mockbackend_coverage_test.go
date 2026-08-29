package ublk

import "testing"

// Helpers shared by the MockBackend coverage tests below. backend_test.go does
// not define any helpers, so these names are collision-free.

func mockBackendBytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mockBackendAllZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// TestMockBackend_ResizeGrow: growing preserves existing data and zero-fills the
// newly added tail.
func TestMockBackend_ResizeGrow(t *testing.T) {
	const origSize int64 = 1024
	backend := NewMockBackend(origSize)

	pattern := []byte("tail-pattern-16B")
	if _, err := backend.WriteAt(pattern, origSize-int64(len(pattern))); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	const newSize int64 = 2048
	if err := backend.Resize(newSize); err != nil {
		t.Fatalf("Resize(grow) = %v, want nil", err)
	}
	if got := backend.Size(); got != newSize {
		t.Errorf("Size() = %d, want %d", got, newSize)
	}

	// Original pattern still intact at its original offset.
	buf := make([]byte, len(pattern))
	if n, err := backend.ReadAt(buf, origSize-int64(len(pattern))); err != nil || n != len(pattern) {
		t.Fatalf("ReadAt after grow: n=%d err=%v", n, err)
	}
	if !mockBackendBytesEqual(buf, pattern) {
		t.Errorf("pattern after grow = %q, want %q", buf, pattern)
	}

	// Newly added tail reads as zero.
	tail := make([]byte, int(newSize-origSize))
	if n, err := backend.ReadAt(tail, origSize); err != nil || n != len(tail) {
		t.Fatalf("ReadAt(tail): n=%d err=%v", n, err)
	}
	for i, v := range tail {
		if v != 0 {
			t.Errorf("grow tail nonzero at byte %d (=0x%02x); want all zeros", i, v)
			break
		}
	}
}

// TestMockBackend_ResizeShrink: shrinking keeps in-bounds data intact and a read
// at/past the new size behaves like a backend that was always that size.
func TestMockBackend_ResizeShrink(t *testing.T) {
	const origSize int64 = 1024
	const newSize int64 = 512
	backend := NewMockBackend(origSize)

	// A 100-byte pattern spanning the new boundary: [500, 600).
	const patOff int64 = 500
	pattern := make([]byte, 100)
	for i := range pattern {
		pattern[i] = byte(i)
	}
	if _, err := backend.WriteAt(pattern, patOff); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	if err := backend.Resize(newSize); err != nil {
		t.Fatalf("Resize(shrink) = %v, want nil", err)
	}
	if got := backend.Size(); got != newSize {
		t.Errorf("Size() = %d, want %d", got, newSize)
	}

	// In-bounds part of the pattern ([500, 512)) is intact.
	inBounds := newSize - patOff // 12
	buf := make([]byte, inBounds)
	if n, err := backend.ReadAt(buf, patOff); err != nil || n != int(inBounds) {
		t.Fatalf("ReadAt(patOff): n=%d err=%v", n, err)
	}
	if !mockBackendBytesEqual(buf, pattern[:inBounds]) {
		t.Errorf("in-bounds bytes = %v, want %v", buf, pattern[:inBounds])
	}

	// A control backend that was always newSize behaves identically.
	control := NewMockBackend(newSize)
	if _, err := control.WriteAt(pattern[:inBounds], patOff); err != nil {
		t.Fatalf("control WriteAt: %v", err)
	}
	tryRead := func(b *MockBackend, off, n int64) (int, error, []byte) {
		out := make([]byte, n)
		got, err := b.ReadAt(out, off)
		return got, err, out
	}

	// Read spanning past the new size: truncated to the in-bounds part.
	na, erra, bufa := tryRead(backend, patOff, 64)
	nb, errb, bufb := tryRead(control, patOff, 64)
	if na != nb || erra != errb || !mockBackendBytesEqual(bufa[:na], bufb[:nb]) {
		t.Errorf("boundary-spanning read differs from control: (%d,%v) vs (%d,%v)", na, erra, nb, errb)
	}
	if na != int(newSize-patOff) {
		t.Errorf("boundary-spanning read n = %d, want %d", na, newSize-patOff)
	}

	// Read at the new size: past-end on both, (0, nil).
	na, erra, _ = tryRead(backend, newSize, 8)
	nb, errb, _ = tryRead(control, newSize, 8)
	if na != 0 || erra != nil || nb != 0 || errb != nil {
		t.Errorf("past-end read differs from control: (%d,%v) vs (%d,%v)", na, erra, nb, errb)
	}
}

// TestMockBackend_ResizeShrinkThenGrow: after shrink-then-grow the regrown tail
// is freshly zeroed — the shrunk-away bytes do NOT resurface, because Resize's
// grow branch always allocates a fresh zero-filled buffer and copies only the
// current (shrunk) length.
func TestMockBackend_ResizeShrinkThenGrow(t *testing.T) {
	const origSize int64 = 1024
	backend := NewMockBackend(origSize)

	// Marker below the shrink point; pattern that will be shrunk away.
	marker := []byte("keep-me")
	shrunkAway := []byte("shrunk-away")
	if _, err := backend.WriteAt(marker, 100); err != nil {
		t.Fatalf("WriteAt(marker): %v", err)
	}
	if _, err := backend.WriteAt(shrunkAway, 1000); err != nil {
		t.Fatalf("WriteAt(shrunkAway): %v", err)
	}

	if err := backend.Resize(512); err != nil {
		t.Fatalf("Resize(shrink) = %v, want nil", err)
	}
	if got := backend.Size(); got != 512 {
		t.Fatalf("Size() after shrink = %d, want 512", got)
	}

	if err := backend.Resize(origSize); err != nil {
		t.Fatalf("Resize(grow back) = %v, want nil", err)
	}
	if got := backend.Size(); got != origSize {
		t.Fatalf("Size() after regrow = %d, want %d", got, origSize)
	}

	// Marker below the shrink point survived.
	mbuf := make([]byte, len(marker))
	if n, err := backend.ReadAt(mbuf, 100); err != nil || n != len(marker) {
		t.Fatalf("ReadAt(marker): n=%d err=%v", n, err)
	}
	if !mockBackendBytesEqual(mbuf, marker) {
		t.Errorf("marker = %q, want %q", mbuf, marker)
	}

	// The region that was shrunk away is now ZERO, not the old bytes.
	region := make([]byte, 1024-512)
	if n, err := backend.ReadAt(region, 512); err != nil || n != len(region) {
		t.Fatalf("ReadAt(regrown tail): n=%d err=%v", n, err)
	}
	for i, v := range region {
		if v != 0 {
			t.Errorf("regrown tail nonzero at offset %d (=0x%02x); want all zeros (shrunk-away data must not resurface)", 512+i, v)
			break
		}
	}
}

// TestMockBackend_ResizeNegative: a negative size is rejected and leaves the
// backend untouched.
func TestMockBackend_ResizeNegative(t *testing.T) {
	backend := NewMockBackend(1024)
	pattern := []byte("unchanged")
	if _, err := backend.WriteAt(pattern, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	if err := backend.Resize(-1); err != ErrInvalidParameters {
		t.Errorf("Resize(-1) = %v, want ErrInvalidParameters", err)
	}
	if err := backend.Resize(-1024); err != ErrInvalidParameters {
		t.Errorf("Resize(-1024) = %v, want ErrInvalidParameters", err)
	}

	if got := backend.Size(); got != 1024 {
		t.Errorf("Size() = %d, want 1024 (unchanged)", got)
	}
	buf := make([]byte, len(pattern))
	if n, err := backend.ReadAt(buf, 0); err != nil || n != len(pattern) {
		t.Fatalf("ReadAt: n=%d err=%v", n, err)
	}
	if !mockBackendBytesEqual(buf, pattern) {
		t.Errorf("data = %q, want %q (unchanged)", buf, pattern)
	}
}

// TestMockBackend_ResizeNoop: resizing to the same size is a safe no-op.
func TestMockBackend_ResizeNoop(t *testing.T) {
	backend := NewMockBackend(1024)
	pattern := []byte("no-op-data")
	if _, err := backend.WriteAt(pattern, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	if err := backend.Resize(1024); err != nil {
		t.Fatalf("Resize(same size) = %v, want nil", err)
	}
	if got := backend.Size(); got != 1024 {
		t.Errorf("Size() = %d, want 1024", got)
	}
	buf := make([]byte, len(pattern))
	if n, err := backend.ReadAt(buf, 0); err != nil || n != len(pattern) {
		t.Fatalf("ReadAt: n=%d err=%v", n, err)
	}
	if !mockBackendBytesEqual(buf, pattern) {
		t.Errorf("data = %q, want %q (unchanged)", buf, pattern)
	}
}

// TestMockBackend_Sync: Sync increments syncCalls and sets synced.
func TestMockBackend_Sync(t *testing.T) {
	backend := NewMockBackend(1024)

	if backend.IsSynced() {
		t.Error("fresh backend IsSynced() = true, want false")
	}
	if err := backend.Sync(); err != nil {
		t.Fatalf("Sync = %v, want nil", err)
	}
	if !backend.IsSynced() {
		t.Error("IsSynced() = false after Sync, want true")
	}
	if got := backend.CallCounts()["sync"]; got != 1 {
		t.Errorf("CallCounts()[\"sync\"] = %d, want 1", got)
	}

	if err := backend.Sync(); err != nil {
		t.Fatalf("second Sync = %v, want nil", err)
	}
	if got := backend.CallCounts()["sync"]; got != 2 {
		t.Errorf("CallCounts()[\"sync\"] = %d, want 2", got)
	}
}

// TestMockBackend_SyncRange: SyncRange increments syncCalls/sets synced and —
// per testing.go — ignores its offset/length arguments entirely, accepting
// negative and out-of-range values silently.
func TestMockBackend_SyncRange(t *testing.T) {
	backend := NewMockBackend(1024)

	if err := backend.SyncRange(0, 1024); err != nil {
		t.Fatalf("SyncRange(0, size) = %v, want nil", err)
	}
	if !backend.IsSynced() {
		t.Error("IsSynced() = false after SyncRange, want true")
	}
	if got := backend.CallCounts()["sync"]; got != 1 {
		t.Errorf("CallCounts()[\"sync\"] = %d, want 1", got)
	}

	// Documented behaviour: arguments are not validated or used at all.
	for _, args := range [][2]int64{{-1, 1024}, {0, int64(1) << 40}, {0, -1}, {-1, -1}} {
		if err := backend.SyncRange(args[0], args[1]); err != nil {
			t.Errorf("SyncRange(%d, %d) = %v, want nil (arguments are ignored)", args[0], args[1], err)
		}
	}
	if got := backend.CallCounts()["sync"]; got != 5 {
		t.Errorf("CallCounts()[\"sync\"] = %d, want 5", got)
	}
}

// TestMockBackend_StatsReturnsCopy: mutating the returned map must not affect a
// subsequent Stats() call.
func TestMockBackend_StatsReturnsCopy(t *testing.T) {
	backend := NewMockBackend(1024)
	backend.SetCustomStats(map[string]interface{}{"custom_key": "custom_value"})

	first := backend.Stats()
	for k := range first {
		first[k] = "mutated"
	}
	first["injected"] = "added"

	second := backend.Stats()
	if second["custom_key"] != "custom_value" {
		t.Errorf("Stats() custom_key = %#v after mutating the first map, want \"custom_value\"", second["custom_key"])
	}
	for _, k := range []string{"read_calls", "write_calls", "flush_calls", "sync_calls"} {
		if second[k] == "mutated" {
			t.Errorf("Stats()[%q] = %v was mutated through the first map", k, second[k])
		}
	}
	if _, ok := second["injected"]; ok {
		t.Error("mutation of the first Stats() map leaked into a later Stats() call")
	}
}

// TestMockBackend_StatsLiveCallCounts: Stats() reflects live call counters and
// uses different key names than CallCounts() for the same four counters.
func TestMockBackend_StatsLiveCallCounts(t *testing.T) {
	backend := NewMockBackend(4096)

	if _, err := backend.WriteAt([]byte("aaaa"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if _, err := backend.WriteAt([]byte("bbbb"), 4); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := backend.ReadAt(make([]byte, 4), 0); err != nil {
			t.Fatalf("ReadAt: %v", err)
		}
	}
	if err := backend.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := backend.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := backend.SyncRange(0, 4096); err != nil {
		t.Fatalf("SyncRange: %v", err)
	}

	stats := backend.Stats()
	counts := backend.CallCounts()

	// Stats() and CallCounts() use different key names for the same counters.
	pairs := []struct {
		statsKey string
		countKey string
		want     int
	}{
		{"read_calls", "read", 3},
		{"write_calls", "write", 2},
		{"flush_calls", "flush", 1},
		{"sync_calls", "sync", 2}, // Sync + SyncRange
	}
	for _, p := range pairs {
		v, ok := stats[p.statsKey].(int)
		if !ok {
			t.Errorf("Stats()[%q] = %#v, want an int", p.statsKey, stats[p.statsKey])
			continue
		}
		c, ok := counts[p.countKey]
		if !ok {
			t.Errorf("CallCounts() has no %q key", p.countKey)
			continue
		}
		if v != c {
			t.Errorf("Stats()[%q] = %d != CallCounts()[%q] = %d", p.statsKey, v, p.countKey, c)
		}
		if v != p.want {
			t.Errorf("Stats()[%q] = %d, want %d", p.statsKey, v, p.want)
		}
	}
}

// TestMockBackend_SetCustomStatsReplaces: SetCustomStats replaces the internal
// custom map (does not merge) and copies its argument.
func TestMockBackend_SetCustomStatsReplaces(t *testing.T) {
	backend := NewMockBackend(1024)

	backend.SetCustomStats(map[string]interface{}{"a": 1})
	backend.SetCustomStats(map[string]interface{}{"b": 2})

	stats := backend.Stats()
	if _, ok := stats["a"]; ok {
		t.Error("Stats() still contains key \"a\" after a second SetCustomStats; SetCustomStats must replace, not merge")
	}
	if stats["b"] != 2 {
		t.Errorf("Stats()[\"b\"] = %v, want 2", stats["b"])
	}
	for _, k := range []string{"read_calls", "write_calls", "flush_calls", "sync_calls"} {
		if _, ok := stats[k]; !ok {
			t.Errorf("Stats() missing always-present key %q", k)
		}
	}

	// The argument is copied, not aliased.
	arg := map[string]interface{}{"x": 1}
	backend.SetCustomStats(arg)
	arg["x"] = 99
	if got := backend.Stats()["x"]; got != 1 {
		t.Errorf("Stats()[\"x\"] = %v after mutating the SetCustomStats argument, want 1 (argument must be copied)", got)
	}
}

// TestMockBackend_Reset: Reset zeroes call counters and clears flushed/synced,
// but does NOT un-close a closed mock, does NOT clear custom stats, and does
// not touch the underlying data.
func TestMockBackend_Reset(t *testing.T) {
	backend := NewMockBackend(1024)

	pattern := []byte("reset-data")
	if _, err := backend.WriteAt(pattern, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if _, err := backend.ReadAt(make([]byte, 4), 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if err := backend.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := backend.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	backend.SetCustomStats(map[string]interface{}{"k": "v"})

	// Reset (open backend): zeroes counters, clears flushed/synced, and does not
	// touch the data contents.
	backend.Reset()
	if cc := backend.CallCounts(); cc["read"] != 0 || cc["write"] != 0 || cc["flush"] != 0 || cc["sync"] != 0 {
		t.Errorf("CallCounts() after Reset = %#v, want all zero", cc)
	}
	if backend.IsFlushed() {
		t.Error("IsFlushed() = true after Reset, want false")
	}
	if backend.IsSynced() {
		t.Error("IsSynced() = true after Reset, want false")
	}
	if backend.IsClosed() {
		t.Error("backend unexpectedly closed")
	}
	buf := make([]byte, len(pattern))
	if n, err := backend.ReadAt(buf, 0); err != nil || n != len(pattern) {
		t.Fatalf("ReadAt after Reset: n=%d err=%v", n, err)
	}
	if !mockBackendBytesEqual(buf, pattern) {
		t.Errorf("data = %q, want %q (Reset must not touch data)", buf, pattern)
	}

	// Close, then Reset again: Reset does NOT un-close the mock.
	if err := backend.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !backend.IsClosed() {
		t.Fatal("IsClosed() = false after Close, want true")
	}
	backend.Reset()
	if !backend.IsClosed() {
		t.Error("IsClosed() = false after Reset on a closed mock; Reset does not touch closed (pinned asymmetry)")
	}
	if cc := backend.CallCounts(); cc["read"] != 0 || cc["write"] != 0 || cc["flush"] != 0 || cc["sync"] != 0 {
		t.Errorf("CallCounts() after second Reset = %#v, want all zero", cc)
	}
	if backend.IsFlushed() || backend.IsSynced() {
		t.Error("flushed/synced not cleared by second Reset")
	}
	// Custom stats survive Reset.
	if got := backend.Stats()["k"]; got != "v" {
		t.Errorf("Stats()[\"k\"] = %v, want \"v\" (Reset must not clear custom stats)", got)
	}
	// Close nils m.data and Reset does not restore it: reads still fail.
	if _, err := backend.ReadAt(make([]byte, 4), 0); err == nil {
		t.Error("ReadAt after Close+Reset succeeded, want ErrDeviceNotFound")
	}
}

// TestMockBackend_DiscardNotCounted: Discard performs its zeroing but increments
// no counter, so CallCounts() cannot reflect discard activity at all — there is
// no way to assert "discard was called N times" against this mock.
func TestMockBackend_DiscardNotCounted(t *testing.T) {
	backend := NewMockBackend(1024)
	pattern := []byte("discard-me")
	if _, err := backend.WriteAt(pattern, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	before := backend.CallCounts()
	for i := 0; i < 5; i++ {
		if err := backend.Discard(0, int64(len(pattern))); err != nil {
			t.Fatalf("Discard #%d: %v", i, err)
		}
	}
	after := backend.CallCounts()

	// Discard works functionally (data is zeroed)...
	buf := make([]byte, len(pattern))
	if n, err := backend.ReadAt(buf, 0); err != nil || n != len(pattern) {
		t.Fatalf("ReadAt: n=%d err=%v", n, err)
	}
	if !mockBackendAllZero(buf) {
		t.Error("data not zeroed after Discard")
	}

	// ...but it is invisible to CallCounts(): no new key appears and the four
	// existing keys are unaffected, so discard activity cannot be asserted.
	for k, v := range before {
		if after[k] != v {
			t.Errorf("CallCounts()[%q] changed from %d to %d across Discard calls", k, v, after[k])
		}
	}
	if len(before) != 4 || len(after) != 4 {
		t.Errorf("CallCounts() key count changed across Discard calls: before=%d after=%d", len(before), len(after))
	}
	if _, ok := after["discard"]; ok {
		t.Error("CallCounts() unexpectedly gained a \"discard\" key")
	}

	// Stats() has no discard counter either, and its four counters are
	// unaffected by discard activity (cross-checked against CallCounts()).
	stats := backend.Stats()
	counts := backend.CallCounts()
	for _, p := range []struct{ s, c string }{{"read_calls", "read"}, {"write_calls", "write"}, {"flush_calls", "flush"}, {"sync_calls", "sync"}} {
		if v, ok := stats[p.s].(int); !ok || v != counts[p.c] {
			t.Errorf("Stats()[%q] = %#v, want CallCounts()[%q] = %d", p.s, stats[p.s], p.c, counts[p.c])
		}
	}
	if _, ok := stats["discard_calls"]; ok {
		t.Error("Stats() unexpectedly gained a \"discard_calls\" key")
	}
	if _, ok := stats["discard"]; ok {
		t.Error("Stats() unexpectedly gained a \"discard\" key")
	}
}
