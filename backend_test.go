package ublk

import (
	"context"
	"testing"
)

// Tests now use the public MockBackend from testing.go

func TestMockBackend(t *testing.T) {
	backend := NewMockBackend(1024)

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
	if !backend.IsFlushed() {
		t.Error("backend not marked as flushed")
	}

	// Test close
	err = backend.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if !backend.IsClosed() {
		t.Error("backend not marked as closed")
	}

	// Test operations after close
	_, err = backend.ReadAt(readBuf, 0)
	if err == nil {
		t.Error("ReadAt should fail after close")
	}
}

func TestDiscardBackend(t *testing.T) {
	backend := NewMockBackend(1024)

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
	backend := NewMockBackend(1024)

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
	backend := NewMockBackend(1024)

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
	if !backend.IsSynced() {
		t.Error("backend not marked as synced")
	}

	// Reset sync flag
	backend.Reset()

	// Test sync range
	err = syncBackend.SyncRange(0, 512)
	if err != nil {
		t.Errorf("SyncRange failed: %v", err)
	}
	if !backend.IsSynced() {
		t.Error("backend not marked as synced after SyncRange")
	}
}

func TestStatBackend(t *testing.T) {
	backend := NewMockBackend(1024)

	// Set some custom stats
	backend.SetCustomStats(map[string]interface{}{
		"custom_stat": 42,
	})

	// Do some operations to generate call counts
	backend.ReadAt(make([]byte, 10), 0)
	backend.WriteAt([]byte("test"), 0)
	backend.Flush()

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

	if customStat, ok := stats["custom_stat"].(int); !ok || customStat != 42 {
		t.Errorf("Expected custom_stat=42, got %v", stats["custom_stat"])
	}

	if readCalls, ok := stats["read_calls"].(int); !ok || readCalls != 1 {
		t.Errorf("Expected read_calls=1, got %v", stats["read_calls"])
	}

	if writeCalls, ok := stats["write_calls"].(int); !ok || writeCalls != 1 {
		t.Errorf("Expected write_calls=1, got %v", stats["write_calls"])
	}
}

func TestResizeBackend(t *testing.T) {
	backend := NewMockBackend(1024)

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
	backend := NewMockBackend(1024)
	params := DefaultParams(backend)

	if params.Backend != backend {
		t.Error("Backend not set correctly")
	}

	if params.QueueDepth != DefaultQueueDepth {
		t.Errorf("QueueDepth = %d, want %d", params.QueueDepth, DefaultQueueDepth)
	}

	if params.LogicalBlockSize != DefaultLogicalBlockSize {
		t.Errorf("LogicalBlockSize = %d, want %d", params.LogicalBlockSize, DefaultLogicalBlockSize)
	}

	if params.MaxIOSize != DefaultMaxIOSize {
		t.Errorf("MaxIOSize = %d, want %d", params.MaxIOSize, DefaultMaxIOSize)
	}

	if params.DeviceID != AutoAssignDeviceID {
		t.Errorf("DeviceID = %d, want %d", params.DeviceID, AutoAssignDeviceID)
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
	backend := NewMockBackend(1024 * 1024) // 1MB
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
	backend := NewMockBackend(1024 * 1024) // 1MB
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

func TestDeviceStateInspection(t *testing.T) {
	// Test nil device
	var device *Device
	if device.State() != DeviceStateStopped {
		t.Error("Nil device should be in stopped state")
	}
	if device.IsRunning() {
		t.Error("Nil device should not be running")
	}

	// Test device info for nil device
	info := device.Info()
	if info.State != "" {
		t.Errorf("Nil device info should show empty state, got %s", info.State)
	}
}

func TestDeviceInfo(t *testing.T) {
	backend := NewMockBackend(1024 * 1024)
	params := DefaultParams(backend)
	params.QueueDepth = 64
	params.NumQueues = 2

	// Create a device struct manually for testing (since we can't actually create devices in unit tests)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	device := &Device{
		ID:        5,
		Path:      "/dev/ublkb5",
		CharPath:  "/dev/ublkc5",
		Backend:   backend,
		queues:    params.NumQueues,
		depth:     params.QueueDepth,
		blockSize: params.LogicalBlockSize,
		started:   true,
		ctx:       ctx,
		cancel:    cancel,
	}

	// Test inspection methods
	if device.DeviceID() != 5 {
		t.Errorf("DeviceID() = %d, want 5", device.DeviceID())
	}
	if device.BlockPath() != "/dev/ublkb5" {
		t.Errorf("BlockPath() = %s, want /dev/ublkb5", device.BlockPath())
	}
	if device.CharDevicePath() != "/dev/ublkc5" {
		t.Errorf("CharDevicePath() = %s, want /dev/ublkc5", device.CharDevicePath())
	}
	if device.NumQueues() != 2 {
		t.Errorf("NumQueues() = %d, want 2", device.NumQueues())
	}
	if device.QueueDepth() != 64 {
		t.Errorf("QueueDepth() = %d, want 64", device.QueueDepth())
	}
	if device.BlockSize() != 512 {
		t.Errorf("BlockSize() = %d, want 512", device.BlockSize())
	}
	if device.Size() != 1024*1024 {
		t.Errorf("Size() = %d, want %d", device.Size(), 1024*1024)
	}

	// Test comprehensive info
	info := device.Info()
	if info.ID != 5 {
		t.Errorf("Info.ID = %d, want 5", info.ID)
	}
	if info.BlockPath != "/dev/ublkb5" {
		t.Errorf("Info.BlockPath = %s, want /dev/ublkb5", info.BlockPath)
	}
	if info.NumQueues != 2 {
		t.Errorf("Info.NumQueues = %d, want 2", info.NumQueues)
	}
	if info.QueueDepth != 64 {
		t.Errorf("Info.QueueDepth = %d, want 64", info.QueueDepth)
	}
	if info.Size != 1024*1024 {
		t.Errorf("Info.Size = %d, want %d", info.Size, 1024*1024)
	}
}