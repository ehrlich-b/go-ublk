package ublk

import (
	"testing"
)

// Mock backend for testing
type mockBackend struct {
	data     []byte
	size     int64
	closed   bool
	flushed  bool
	synced   bool
	stats    map[string]interface{}
}

func newMockBackend(size int64) *mockBackend {
	return &mockBackend{
		data:  make([]byte, size),
		size:  size,
		stats: make(map[string]interface{}),
	}
}

func (m *mockBackend) ReadAt(p []byte, off int64) (int, error) {
	if m.closed {
		return 0, ErrDeviceNotFound
	}

	if off >= m.size {
		return 0, nil
	}

	n := copy(p, m.data[off:])
	return n, nil
}

func (m *mockBackend) WriteAt(p []byte, off int64) (int, error) {
	if m.closed {
		return 0, ErrDeviceNotFound
	}

	if off >= m.size {
		return 0, ErrInvalidParameters
	}

	n := copy(m.data[off:], p)
	return n, nil
}

func (m *mockBackend) Size() int64 {
	return m.size
}

func (m *mockBackend) Close() error {
	m.closed = true
	return nil
}

func (m *mockBackend) Flush() error {
	m.flushed = true
	return nil
}

// Optional interfaces
func (m *mockBackend) Discard(offset, length int64) error {
	// Zero out the range
	end := offset + length
	if end > m.size {
		end = m.size
	}
	for i := offset; i < end; i++ {
		m.data[i] = 0
	}
	return nil
}

func (m *mockBackend) WriteZeroes(offset, length int64) error {
	end := offset + length
	if end > m.size {
		end = m.size
	}
	for i := offset; i < end; i++ {
		m.data[i] = 0
	}
	return nil
}

func (m *mockBackend) Sync() error {
	m.synced = true
	return nil
}

func (m *mockBackend) SyncRange(offset, length int64) error {
	m.synced = true
	return nil
}

func (m *mockBackend) Stats() map[string]interface{} {
	m.stats["reads"] = 42
	m.stats["writes"] = 24
	return m.stats
}

func (m *mockBackend) Resize(newSize int64) error {
	if newSize < 0 {
		return ErrInvalidParameters
	}

	if newSize > m.size {
		// Expand
		newData := make([]byte, newSize)
		copy(newData, m.data)
		m.data = newData
	} else if newSize < m.size {
		// Truncate
		m.data = m.data[:newSize]
	}

	m.size = newSize
	return nil
}

func TestMockBackend(t *testing.T) {
	backend := newMockBackend(1024)

	// Test size
	if backend.Size() != 1024 {
		t.Errorf("Size() = %d, want 1024", backend.Size())
	}

	// Test write/read
	testData := []byte("hello world")
	n, err := backend.WriteAt(testData, 0)
	if err != nil {
		t.Errorf("WriteAt failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("WriteAt wrote %d bytes, want %d", n, len(testData))
	}

	readBuf := make([]byte, len(testData))
	n, err = backend.ReadAt(readBuf, 0)
	if err != nil {
		t.Errorf("ReadAt failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("ReadAt read %d bytes, want %d", n, len(testData))
	}
	if string(readBuf) != string(testData) {
		t.Errorf("ReadAt got %q, want %q", readBuf, testData)
	}

	// Test flush
	err = backend.Flush()
	if err != nil {
		t.Errorf("Flush failed: %v", err)
	}
	if !backend.flushed {
		t.Error("backend not marked as flushed")
	}

	// Test close
	err = backend.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if !backend.closed {
		t.Error("backend not marked as closed")
	}

	// Test operations after close
	_, err = backend.ReadAt(readBuf, 0)
	if err == nil {
		t.Error("ReadAt should fail after close")
	}
}

func TestDiscardBackend(t *testing.T) {
	backend := newMockBackend(1024)

	// Write some data
	testData := []byte("hello world")
	backend.WriteAt(testData, 0)

	// Verify data is there
	readBuf := make([]byte, len(testData))
	backend.ReadAt(readBuf, 0)
	if string(readBuf) != string(testData) {
		t.Errorf("Data not written correctly")
	}

	// Check if backend supports discard
	discardBackend, ok := Backend(backend).(DiscardBackend)
	if !ok {
		t.Fatal("Backend should implement DiscardBackend")
	}

	// Discard the data
	err := discardBackend.Discard(0, int64(len(testData)))
	if err != nil {
		t.Errorf("Discard failed: %v", err)
	}

	// Verify data is zeroed
	backend.ReadAt(readBuf, 0)
	for i, b := range readBuf {
		if b != 0 {
			t.Errorf("Byte %d not zeroed after discard: %d", i, b)
		}
	}
}

func TestWriteZeroesBackend(t *testing.T) {
	backend := newMockBackend(1024)

	// Write some data first
	testData := []byte("hello world")
	backend.WriteAt(testData, 0)

	// Check if backend supports WriteZeroes
	writeZeroesBackend, ok := Backend(backend).(WriteZeroesBackend)
	if !ok {
		t.Fatal("Backend should implement WriteZeroesBackend")
	}

	// Write zeros
	err := writeZeroesBackend.WriteZeroes(0, int64(len(testData)))
	if err != nil {
		t.Errorf("WriteZeroes failed: %v", err)
	}

	// Verify data is zeroed
	readBuf := make([]byte, len(testData))
	backend.ReadAt(readBuf, 0)
	for i, b := range readBuf {
		if b != 0 {
			t.Errorf("Byte %d not zeroed: %d", i, b)
		}
	}
}

func TestSyncBackend(t *testing.T) {
	backend := newMockBackend(1024)

	// Check if backend supports sync
	syncBackend, ok := Backend(backend).(SyncBackend)
	if !ok {
		t.Fatal("Backend should implement SyncBackend")
	}

	// Test sync
	err := syncBackend.Sync()
	if err != nil {
		t.Errorf("Sync failed: %v", err)
	}
	if !backend.synced {
		t.Error("backend not marked as synced")
	}

	// Reset sync flag
	backend.synced = false

	// Test sync range
	err = syncBackend.SyncRange(0, 512)
	if err != nil {
		t.Errorf("SyncRange failed: %v", err)
	}
	if !backend.synced {
		t.Error("backend not marked as synced after SyncRange")
	}
}

func TestStatBackend(t *testing.T) {
	backend := newMockBackend(1024)

	// Check if backend supports stats
	statBackend, ok := Backend(backend).(StatBackend)
	if !ok {
		t.Fatal("Backend should implement StatBackend")
	}

	// Get stats
	stats := statBackend.Stats()
	if stats == nil {
		t.Fatal("Stats() returned nil")
	}

	if reads, ok := stats["reads"].(int); !ok || reads != 42 {
		t.Errorf("Expected reads=42, got %v", stats["reads"])
	}

	if writes, ok := stats["writes"].(int); !ok || writes != 24 {
		t.Errorf("Expected writes=24, got %v", stats["writes"])
	}
}

func TestResizeBackend(t *testing.T) {
	backend := newMockBackend(1024)

	// Check if backend supports resize
	resizeBackend, ok := Backend(backend).(ResizeBackend)
	if !ok {
		t.Fatal("Backend should implement ResizeBackend")
	}

	// Test expanding
	err := resizeBackend.Resize(2048)
	if err != nil {
		t.Errorf("Resize failed: %v", err)
	}
	if backend.Size() != 2048 {
		t.Errorf("Size after resize = %d, want 2048", backend.Size())
	}

	// Test shrinking
	err = resizeBackend.Resize(512)
	if err != nil {
		t.Errorf("Resize failed: %v", err)
	}
	if backend.Size() != 512 {
		t.Errorf("Size after resize = %d, want 512", backend.Size())
	}

	// Test invalid size
	err = resizeBackend.Resize(-1)
	if err == nil {
		t.Error("Resize with negative size should fail")
	}
}

func TestDefaultParams(t *testing.T) {
	backend := newMockBackend(1024)
	params := DefaultParams(backend)

	if params.Backend != backend {
		t.Error("Backend not set correctly")
	}

	if params.QueueDepth != 128 {
		t.Errorf("QueueDepth = %d, want 128", params.QueueDepth)
	}

	if params.LogicalBlockSize != 512 {
		t.Errorf("LogicalBlockSize = %d, want 512", params.LogicalBlockSize)
	}

	if params.MaxIOSize != 1<<20 {
		t.Errorf("MaxIOSize = %d, want %d", params.MaxIOSize, 1<<20)
	}

	if params.DeviceID != -1 {
		t.Errorf("DeviceID = %d, want -1", params.DeviceID)
	}

	// Test boolean defaults
	if params.ReadOnly {
		t.Error("ReadOnly should default to false")
	}
	if params.Rotational {
		t.Error("Rotational should default to false")
	}
	if params.EnableZeroCopy {
		t.Error("EnableZeroCopy should default to false")
	}
}

func BenchmarkMockBackendRead(b *testing.B) {
	backend := newMockBackend(1024 * 1024) // 1MB
	buf := make([]byte, 4096)               // 4KB reads

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offset := int64(i*4096) % (1024*1024 - 4096)
		_, err := backend.ReadAt(buf, offset)
		if err != nil {
			b.Fatalf("ReadAt failed: %v", err)
		}
	}
}

func BenchmarkMockBackendWrite(b *testing.B) {
	backend := newMockBackend(1024 * 1024) // 1MB
	buf := make([]byte, 4096)               // 4KB writes
	for i := range buf {
		buf[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offset := int64(i*4096) % (1024*1024 - 4096)
		_, err := backend.WriteAt(buf, offset)
		if err != nil {
			b.Fatalf("WriteAt failed: %v", err)
		}
	}
}