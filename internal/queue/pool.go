package queue

import "sync"

// BufferPool provides pooled byte slices to avoid hot-path allocations.
// Uses size-bucketed pools with power-of-2 sizes (128KB, 256KB, 512KB, 1MB)
// to balance memory efficiency with allocation reduction.
//
// The 64KB case is not pooled because runner.go uses mmap'd per-tag buffers
// for I/O <= 64KB. This pool handles the overflow case (64KB < size <= 1MB).
//
// Uses *[]byte pattern to avoid sync.Pool interface allocation overhead.

// Buffer size thresholds
const (
	size128k = 128 * 1024
	size256k = 256 * 1024
	size512k = 512 * 1024
	size1m   = 1024 * 1024
)

// globalPool is the shared buffer pool for all queue runners.
// Uses pointer-to-slice pattern for efficient sync.Pool usage.
var globalPool = struct {
	pool128k sync.Pool
	pool256k sync.Pool
	pool512k sync.Pool
	pool1m   sync.Pool
}{
	pool128k: sync.Pool{New: func() any { b := make([]byte, size128k); return &b }},
	pool256k: sync.Pool{New: func() any { b := make([]byte, size256k); return &b }},
	pool512k: sync.Pool{New: func() any { b := make([]byte, size512k); return &b }},
	pool1m:   sync.Pool{New: func() any { b := make([]byte, size1m); return &b }},
}

// GetBuffer returns a pooled buffer of at least the requested size.
// Caller must call PutBuffer when done.
func GetBuffer(size uint32) []byte {
	switch {
	case size <= size128k:
		return (*globalPool.pool128k.Get().(*[]byte))[:size]
	case size <= size256k:
		return (*globalPool.pool256k.Get().(*[]byte))[:size]
	case size <= size512k:
		return (*globalPool.pool512k.Get().(*[]byte))[:size]
	default:
		return (*globalPool.pool1m.Get().(*[]byte))[:size]
	}
}

// PutBuffer returns a buffer to the pool.
// The buffer's capacity determines which pool it goes to.
func PutBuffer(buf []byte) {
	c := cap(buf)
	// Restore full capacity before returning to pool
	buf = buf[:c]
	switch c {
	case size128k:
		globalPool.pool128k.Put(&buf)
	case size256k:
		globalPool.pool256k.Put(&buf)
	case size512k:
		globalPool.pool512k.Put(&buf)
	case size1m:
		globalPool.pool1m.Put(&buf)
		// Buffers with non-standard capacity are not returned to pool
	}
}
