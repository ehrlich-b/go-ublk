package main

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"sync"

	"github.com/ehrlich-b/go-ublk"
)

// zipChunkSize is the compression unit. I/O is decompressed and recompressed a
// chunk at a time, which trades ratio (bigger chunks compress better) against
// the read-modify-write cost a partial write pays (smaller chunks waste less).
const zipChunkSize = 64 * 1024

// zipLevel is deliberately BestSpeed: compression here runs inline on the block
// device's I/O path, where latency is what the caller notices and a few points
// of ratio are not.
const zipLevel = flate.BestSpeed

// zeroChunk backs the all-zeros test in storeLocked. bytes.Equal against it is
// an assembly memcmp, which is far cheaper than compressing zeros.
var zeroChunk [zipChunkSize]byte

// zipBackend is a compressed RAM backend: it stores the device as independently
// flate-compressed chunks, so a device holding compressible data costs a
// fraction of its addressable size. An all-zero chunk is not stored at all,
// which makes a freshly created device (and any discarded range) free.
//
// It exists to demonstrate that a go-ublk backend is just five methods over
// whatever storage the caller wants — the block layer above it cannot tell that
// reads are being decompressed on the fly.
type zipBackend struct {
	size   int64
	chunks [][]byte       // compressed chunk data; nil means a hole (all zeros)
	locks  []sync.RWMutex // one per chunk, so queues touching different chunks never contend

	writers sync.Pool // *flate.Writer
	readers sync.Pool // io.ReadCloser (also a flate.Resetter)
	scratch sync.Pool // *[]byte, one decompressed chunk
}

func newZipBackend(size int64) *zipBackend {
	n := int((size + zipChunkSize - 1) / zipChunkSize)
	z := &zipBackend{
		size:   size,
		chunks: make([][]byte, n),
		locks:  make([]sync.RWMutex, n),
	}
	z.writers.New = func() any {
		// The error is only ever "invalid level", and zipLevel is a constant.
		w, _ := flate.NewWriter(io.Discard, zipLevel)
		return w
	}
	z.readers.New = func() any { return flate.NewReader(bytes.NewReader(nil)) }
	z.scratch.New = func() any { b := make([]byte, zipChunkSize); return &b }
	return z
}

// chunkLen is the decompressed length of chunk idx, which is short for the last
// chunk when the device size is not a multiple of zipChunkSize.
func (z *zipBackend) chunkLen(idx int) int {
	if end := int64(idx+1) * zipChunkSize; end > z.size {
		return int(z.size - int64(idx)*zipChunkSize)
	}
	return zipChunkSize
}

// loadLocked decompresses chunk idx into dst, which must be exactly
// chunkLen(idx) bytes. The caller must hold the chunk's lock.
func (z *zipBackend) loadLocked(idx int, dst []byte) error {
	if z.chunks[idx] == nil {
		clear(dst)
		return nil
	}
	r := z.readers.Get().(io.ReadCloser)
	defer z.readers.Put(r)
	if err := r.(flate.Resetter).Reset(bytes.NewReader(z.chunks[idx]), nil); err != nil {
		return err
	}
	if _, err := io.ReadFull(r, dst); err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	return nil
}

// storeLocked compresses src into chunk idx. The caller must hold the chunk's
// lock and src must be exactly chunkLen(idx) bytes.
func (z *zipBackend) storeLocked(idx int, src []byte) error {
	// A hole costs nothing to store and nothing to read, so never spend the
	// ~40 bytes flate wants for 64KB of zeros. This is what makes discard and
	// a freshly created device free.
	if bytes.Equal(src, zeroChunk[:len(src)]) {
		z.chunks[idx] = nil
		return nil
	}

	var buf bytes.Buffer
	buf.Grow(len(src) / 4)
	w := z.writers.Get().(*flate.Writer)
	defer z.writers.Put(w)
	w.Reset(&buf)
	if _, err := w.Write(src); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	z.chunks[idx] = buf.Bytes()
	return nil
}

func (z *zipBackend) ReadAt(p []byte, off int64) (int, error) {
	if off >= z.size {
		return 0, nil
	}
	if int64(len(p)) > z.size-off {
		p = p[:z.size-off]
	}

	scratch := z.scratch.Get().(*[]byte)
	defer z.scratch.Put(scratch)

	done := 0
	for done < len(p) {
		pos := off + int64(done)
		idx := int(pos / zipChunkSize)
		inChunk := int(pos % zipChunkSize)
		chunk := (*scratch)[:z.chunkLen(idx)]

		z.locks[idx].RLock()
		err := z.loadLocked(idx, chunk)
		z.locks[idx].RUnlock()
		if err != nil {
			return done, fmt.Errorf("chunk %d: %w", idx, err)
		}

		done += copy(p[done:], chunk[inChunk:])
	}
	return done, nil
}

func (z *zipBackend) WriteAt(p []byte, off int64) (int, error) {
	if off >= z.size {
		return 0, fmt.Errorf("write beyond end of device")
	}
	if int64(len(p)) > z.size-off {
		p = p[:z.size-off]
	}

	scratch := z.scratch.Get().(*[]byte)
	defer z.scratch.Put(scratch)

	done := 0
	for done < len(p) {
		pos := off + int64(done)
		idx := int(pos / zipChunkSize)
		inChunk := int(pos % zipChunkSize)
		clen := z.chunkLen(idx)
		n := min(clen-inChunk, len(p)-done)
		chunk := (*scratch)[:clen]

		z.locks[idx].Lock()
		var err error
		// Only a partial write needs the old contents back first; a full
		// overwrite would be decompressing data it is about to replace.
		if inChunk != 0 || n != clen {
			err = z.loadLocked(idx, chunk)
		}
		if err == nil {
			copy(chunk[inChunk:inChunk+n], p[done:done+n])
			err = z.storeLocked(idx, chunk)
		}
		z.locks[idx].Unlock()
		if err != nil {
			return done, fmt.Errorf("chunk %d: %w", idx, err)
		}

		done += n
	}
	return done, nil
}

func (z *zipBackend) Discard(offset, length int64) error {
	if offset >= z.size {
		return nil
	}
	if length > z.size-offset {
		length = z.size - offset
	}
	end := offset + length

	scratch := z.scratch.Get().(*[]byte)
	defer z.scratch.Put(scratch)

	for pos := offset; pos < end; {
		idx := int(pos / zipChunkSize)
		inChunk := int(pos % zipChunkSize)
		clen := z.chunkLen(idx)
		n := min(int64(clen-inChunk), end-pos)
		chunk := (*scratch)[:clen]

		z.locks[idx].Lock()
		var err error
		if inChunk == 0 && n == int64(clen) {
			// Fully covered: drop the chunk and reclaim the memory outright.
			z.chunks[idx] = nil
		} else if err = z.loadLocked(idx, chunk); err == nil {
			// A partial chunk has surviving bytes on one side, so its zeros
			// have to be written through the normal path.
			clear(chunk[inChunk : inChunk+int(n)])
			err = z.storeLocked(idx, chunk)
		}
		z.locks[idx].Unlock()
		if err != nil {
			return fmt.Errorf("chunk %d: %w", idx, err)
		}

		pos += n
	}
	return nil
}

func (z *zipBackend) Size() int64 { return z.size }

func (z *zipBackend) Flush() error { return nil }

func (z *zipBackend) Close() error {
	z.chunks = nil
	return nil
}

// compressedBytes is what the device currently costs in RAM: the total size of
// the stored chunks, with holes counting as zero.
func (z *zipBackend) compressedBytes() int64 {
	var total int64
	for i := range z.chunks {
		z.locks[i].RLock()
		total += int64(len(z.chunks[i]))
		z.locks[i].RUnlock()
	}
	return total
}

// Compile-time interface checks
var (
	_ ublk.Backend        = (*zipBackend)(nil)
	_ ublk.DiscardBackend = (*zipBackend)(nil)
)
