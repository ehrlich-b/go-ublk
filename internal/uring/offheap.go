package uring

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// AllocOffHeap maps size bytes of zeroed, page-aligned anonymous memory
// outside the Go heap. The kernel may read and write it for as long as it
// likes: the runtime never moves or frees it. Release it with FreeOffHeap.
func AllocOffHeap(size int) ([]byte, error) {
	b, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("mmap %d off-heap bytes: %w", size, err)
	}
	return b, nil
}

// FreeOffHeap unmaps memory from AllocOffHeap; b must be the slice
// AllocOffHeap returned, not a reslice of it.
func FreeOffHeap(b []byte) error {
	if err := unix.Munmap(b); err != nil {
		return fmt.Errorf("munmap %d off-heap bytes: %w", len(b), err)
	}
	return nil
}

// AddrOf returns the address of b's first byte for an SQE (0 if b is empty).
// It forces b's backing array to the heap: a buffer whose address only
// travels as an integer would otherwise be eligible for the goroutine stack,
// which the runtime moves. The caller must still keep b reachable until the
// kernel is done with it.
func AddrOf(b []byte) uint64 {
	if len(b) == 0 {
		return 0
	}
	p := unsafe.Pointer(unsafe.SliceData(b))
	escape(p)
	return uint64(uintptr(p))
}

// escapeSink and escapeNever implement escape. escapeNever is never set, but
// the compiler cannot prove that, so escape analysis treats p as leaking to
// the heap while the store never runs (and never races).
var (
	escapeSink  unsafe.Pointer
	escapeNever bool
)

// escape marks p's referent as escaping to the heap (the runtime's
// internal/abi.Escape trick).
func escape(p unsafe.Pointer) {
	if escapeNever {
		escapeSink = p
	}
}
