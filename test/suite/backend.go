//go:build linux

package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
)

// rangeOp records one discard or write-zeroes call as the backend saw it.
type rangeOp struct{ off, length int64 }

// memBackend is a RAM backend that records what the kernel actually sent it.
// Its data is a sparse map of fixed-size chunks, so a "5 GiB" device costs only
// what is written — large discards and zeroouts can be tested in a small VM.
// Reads of never-written or discarded chunks return zeros.
type memBackend struct {
	size  int64
	chunk int64

	mu     sync.RWMutex
	chunks map[int64][]byte

	reads, writes, flushes atomic.Int64

	opsMu       sync.Mutex
	discards    []rangeOp
	writeZeroes []rangeOp

	// failRange makes reads and writes overlapping [failOff, failOff+failLen)
	// return failErr, to test error propagation.
	failMu           sync.RWMutex
	failOff, failLen int64
	failErr          error
}

const memChunk = 64 << 10

func newMemBackend(size int64) *memBackend {
	return &memBackend{size: size, chunk: memChunk, chunks: make(map[int64][]byte)}
}

func (b *memBackend) Size() int64  { return b.size }
func (b *memBackend) Close() error { return nil }

func (b *memBackend) Flush() error {
	b.flushes.Add(1)
	return nil
}

func (b *memBackend) setFailure(off, length int64, err error) {
	b.failMu.Lock()
	b.failOff, b.failLen, b.failErr = off, length, err
	b.failMu.Unlock()
}

func (b *memBackend) injected(off int64, n int) error {
	b.failMu.RLock()
	defer b.failMu.RUnlock()
	if b.failErr != nil && off < b.failOff+b.failLen && b.failOff < off+int64(n) {
		return b.failErr
	}
	return nil
}

func (b *memBackend) check(off int64, n int) error {
	if off < 0 || off+int64(n) > b.size {
		return fmt.Errorf("access [%d,+%d) outside %d-byte backend: %w", off, n, b.size, syscall.EINVAL)
	}
	return nil
}

func (b *memBackend) ReadAt(p []byte, off int64) (int, error) {
	b.reads.Add(1)
	if err := b.check(off, len(p)); err != nil {
		return 0, err
	}
	if err := b.injected(off, len(p)); err != nil {
		return 0, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for done := 0; done < len(p); {
		pos := off + int64(done)
		idx, in := pos/b.chunk, pos%b.chunk
		n := min(int(b.chunk-in), len(p)-done)
		if c, ok := b.chunks[idx]; ok {
			copy(p[done:done+n], c[in:])
		} else {
			clear(p[done : done+n])
		}
		done += n
	}
	return len(p), nil
}

func (b *memBackend) WriteAt(p []byte, off int64) (int, error) {
	b.writes.Add(1)
	if err := b.check(off, len(p)); err != nil {
		return 0, err
	}
	if err := b.injected(off, len(p)); err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for done := 0; done < len(p); {
		pos := off + int64(done)
		idx, in := pos/b.chunk, pos%b.chunk
		n := min(int(b.chunk-in), len(p)-done)
		c, ok := b.chunks[idx]
		if !ok {
			c = make([]byte, b.chunk)
			b.chunks[idx] = c
		}
		copy(c[in:], p[done:done+n])
		done += n
	}
	return len(p), nil
}

// zeroRange drops whole chunks and clears partial ones.
func (b *memBackend) zeroRange(off, length int64) error {
	if off < 0 || length < 0 || off+length > b.size {
		return fmt.Errorf("range [%d,+%d) outside %d-byte backend: %w", off, length, b.size, syscall.EINVAL)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for pos, end := off, off+length; pos < end; {
		idx, in := pos/b.chunk, pos%b.chunk
		n := min(b.chunk-in, end-pos)
		if c, ok := b.chunks[idx]; ok {
			if in == 0 && n == b.chunk {
				delete(b.chunks, idx)
			} else {
				clear(c[in : in+n])
			}
		}
		pos += n
	}
	return nil
}

func (b *memBackend) Discard(off, length int64) error {
	b.opsMu.Lock()
	b.discards = append(b.discards, rangeOp{off, length})
	b.opsMu.Unlock()
	return b.zeroRange(off, length)
}

func (b *memBackend) WriteZeroes(off, length int64) error {
	b.opsMu.Lock()
	b.writeZeroes = append(b.writeZeroes, rangeOp{off, length})
	b.opsMu.Unlock()
	return b.zeroRange(off, length)
}

// rangeOps returns a copy of the recorded discards or write-zeroes.
func (b *memBackend) rangeOps(discard bool) []rangeOp {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()
	if discard {
		return append([]rangeOp(nil), b.discards...)
	}
	return append([]rangeOp(nil), b.writeZeroes...)
}

// plainBackend hides memBackend's optional interfaces, for testing what the
// library advertises when a backend can't discard or zero.
type plainBackend struct{ m *memBackend }

func (p plainBackend) ReadAt(b []byte, off int64) (int, error)  { return p.m.ReadAt(b, off) }
func (p plainBackend) WriteAt(b []byte, off int64) (int, error) { return p.m.WriteAt(b, off) }
func (p plainBackend) Size() int64                              { return p.m.Size() }
func (p plainBackend) Close() error                             { return nil }
func (p plainBackend) Flush() error                             { return p.m.Flush() }
