package backend

import (
	"testing"
)

func TestNewMemory(t *testing.T) {
	size := int64(1024)
	mem := NewMemory(size)
	
	if mem.Size() != size {
		t.Errorf("Size() = %d, want %d", mem.Size(), size)
	}
	
	if len(mem.data) != int(size) {
		t.Errorf("data length = %d, want %d", len(mem.data), size)
	}
}

func TestMemoryReadWrite(t *testing.T) {
	mem := NewMemory(1024)
	defer mem.Close()
	
	// Test write
	testData := []byte("Hello, ublk!")
	n, err := mem.WriteAt(testData, 0)
	if err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("WriteAt wrote %d bytes, want %d", n, len(testData))
	}
	
	// Test read
	readBuf := make([]byte, len(testData))
	n, err = mem.ReadAt(readBuf, 0)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("ReadAt read %d bytes, want %d", n, len(testData))
	}
	if string(readBuf) != string(testData) {
		t.Errorf("ReadAt got %q, want %q", readBuf, testData)
	}
}

func TestMemoryBoundaryConditions(t *testing.T) {
	mem := NewMemory(100)
	defer mem.Close()
	
	// Test read beyond end
	buf := make([]byte, 50)
	n, err := mem.ReadAt(buf, 80)
	if err != nil {
		t.Errorf("ReadAt at boundary failed: %v", err)
	}
	if n != 20 {
		t.Errorf("ReadAt at boundary read %d bytes, want 20", n)
	}
	
	// Test write beyond end
	_, err = mem.WriteAt([]byte("test"), 98)
	if err != nil {
		t.Errorf("WriteAt near end failed: %v", err)
	}
	
	// Test write completely beyond end
	_, err = mem.WriteAt([]byte("test"), 101)
	if err == nil {
		t.Error("WriteAt beyond end should fail")
	}
}

func TestMemoryDiscard(t *testing.T) {
	mem := NewMemory(100)
	defer mem.Close()
	
	// Write some data
	testData := []byte("Hello, World!")
	mem.WriteAt(testData, 0)
	
	// Discard part of it
	err := mem.Discard(0, 5)
	if err != nil {
		t.Fatalf("Discard failed: %v", err)
	}
	
	// Verify the data is zeroed
	readBuf := make([]byte, len(testData))
	mem.ReadAt(readBuf, 0)
	
	for i := 0; i < 5; i++ {
		if readBuf[i] != 0 {
			t.Errorf("Byte %d not zeroed after discard: %d", i, readBuf[i])
		}
	}
	
	// Rest should be unchanged
	if string(readBuf[5:]) != string(testData[5:]) {
		t.Errorf("Non-discarded data changed: got %q, want %q", readBuf[5:], testData[5:])
	}
}

func TestMemoryStats(t *testing.T) {
	mem := NewMemory(1024)
	defer mem.Close()
	
	stats := mem.Stats()
	
	if stats["type"] != "memory" {
		t.Errorf("Stats type = %v, want 'memory'", stats["type"])
	}
	
	if stats["size"] != int64(1024) {
		t.Errorf("Stats size = %v, want 1024", stats["size"])
	}
	
	if stats["allocated"] != 1024 {
		t.Errorf("Stats allocated = %v, want 1024", stats["allocated"])
	}
}

func BenchmarkMemoryRead(b *testing.B) {
	mem := NewMemory(1024 * 1024) // 1MB
	defer mem.Close()
	
	buf := make([]byte, 4096) // 4KB reads
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offset := int64(i*4096) % (1024*1024 - 4096)
		mem.ReadAt(buf, offset)
	}
}

func BenchmarkMemoryWrite(b *testing.B) {
	mem := NewMemory(1024 * 1024) // 1MB
	defer mem.Close()
	
	buf := make([]byte, 4096) // 4KB writes
	for i := range buf {
		buf[i] = byte(i)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offset := int64(i*4096) % (1024*1024 - 4096)
		mem.WriteAt(buf, offset)
	}
}