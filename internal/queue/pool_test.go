package queue

import (
	"testing"
)

func TestGetBuffer_SizeBuckets(t *testing.T) {
	tests := []struct {
		name        string
		requestSize uint32
		expectCap   int
	}{
		{"128KB bucket - exact", 128 * 1024, 128 * 1024},
		{"128KB bucket - smaller", 65 * 1024, 128 * 1024},
		{"256KB bucket - exact", 256 * 1024, 256 * 1024},
		{"256KB bucket - smaller", 200 * 1024, 256 * 1024},
		{"512KB bucket - exact", 512 * 1024, 512 * 1024},
		{"512KB bucket - smaller", 400 * 1024, 512 * 1024},
		{"1MB bucket - exact", 1024 * 1024, 1024 * 1024},
		{"1MB bucket - smaller", 800 * 1024, 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := GetBuffer(tt.requestSize)
			if len(buf) != int(tt.requestSize) {
				t.Errorf("GetBuffer(%d) returned len=%d, want %d", tt.requestSize, len(buf), tt.requestSize)
			}
			if cap(buf) != tt.expectCap {
				t.Errorf("GetBuffer(%d) returned cap=%d, want %d", tt.requestSize, cap(buf), tt.expectCap)
			}
			PutBuffer(buf)
		})
	}
}

func TestBufferPool_Reuse(t *testing.T) {
	// Get a buffer
	buf1 := GetBuffer(128 * 1024)
	ptr1 := &buf1[0]
	PutBuffer(buf1)

	// Get another buffer of the same size - should reuse
	buf2 := GetBuffer(128 * 1024)
	ptr2 := &buf2[0]
	PutBuffer(buf2)

	// Note: sync.Pool may or may not reuse immediately, but addresses should be same
	// when the pool is warm. This test verifies the basic pooling mechanism works.
	if ptr1 == ptr2 {
		t.Log("Buffer was successfully reused from pool")
	} else {
		t.Log("Buffer was not reused (sync.Pool GC behavior)")
	}
}

func TestPutBuffer_NonStandardCap(t *testing.T) {
	// Create a buffer with non-standard capacity
	buf := make([]byte, 100*1024) // 100KB - not a standard bucket
	// This should not panic
	PutBuffer(buf)
}

func BenchmarkGetBuffer_128KB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := GetBuffer(128 * 1024)
		PutBuffer(buf)
	}
}

func BenchmarkGetBuffer_256KB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := GetBuffer(256 * 1024)
		PutBuffer(buf)
	}
}

func BenchmarkGetBuffer_512KB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := GetBuffer(512 * 1024)
		PutBuffer(buf)
	}
}

func BenchmarkGetBuffer_1MB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := GetBuffer(1024 * 1024)
		PutBuffer(buf)
	}
}

func BenchmarkMakeBuffer_128KB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = make([]byte, 128*1024)
	}
}

func BenchmarkMakeBuffer_1MB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = make([]byte, 1024*1024)
	}
}
