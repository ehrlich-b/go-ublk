package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk"
)

// loopBlockSize is the logical block size the device advertises. A block
// device's capacity has to be a whole number of logical blocks, so the backing
// file's size is rounded down to a multiple of this.
const loopBlockSize = 512

// loopBackend exports a regular file as a block device, the way losetup does.
//
// This is the example where durability is a real question. RAM has no
// distinction between "the write returned" and "the bytes survive a power cut",
// so ublk-mem cannot demonstrate one; a file does, and which side of that line
// the backend sits on is exactly what the device's write-cache attribute tells
// the kernel. See the -sync flag in main.go for the two honest configurations.
type loopBackend struct {
	f    *os.File
	size int64

	// noPunch latches once the filesystem says it cannot punch holes, so a
	// discard-heavy workload stops paying for a syscall that always fails.
	noPunch atomic.Bool
}

// openLoop opens (or creates) path and returns it as a backend. A size of 0
// means "use the file's current size"; a non-zero size grows a smaller file to
// match. The reported size is rounded down to a whole number of logical blocks.
func openLoop(path string, size int64, readOnly, syncWrites bool) (*loopBackend, error) {
	flags := os.O_RDWR | os.O_CREATE
	if readOnly {
		flags = os.O_RDONLY
	}
	if syncWrites {
		// O_DSYNC makes every write durable before it returns, which is what
		// lets this backend honestly advertise a write-through device.
		flags |= unix.O_DSYNC
	}

	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	if size == 0 {
		size = info.Size()
	} else if info.Size() < size {
		if readOnly {
			f.Close()
			return nil, fmt.Errorf("file is %d bytes, cannot grow it to %d in read-only mode", info.Size(), size)
		}
		// Truncate creates a sparse file: the device is fully addressable but
		// costs only the blocks actually written.
		if err := f.Truncate(size); err != nil {
			f.Close()
			return nil, fmt.Errorf("grow to %d bytes: %w", size, err)
		}
	}

	size -= size % loopBlockSize
	if size == 0 {
		f.Close()
		return nil, fmt.Errorf("backing file is smaller than one %d-byte block (pass -size)", loopBlockSize)
	}

	return &loopBackend{f: f, size: size}, nil
}

// ReadAt and WriteAt are pread/pwrite: they carry their own offset instead of
// sharing the file's, which is what makes it safe for every queue to call them
// concurrently without any locking here.
func (l *loopBackend) ReadAt(p []byte, off int64) (int, error) {
	if off >= l.size {
		return 0, nil
	}
	if int64(len(p)) > l.size-off {
		p = p[:l.size-off]
	}

	n, err := l.f.ReadAt(p, off)
	if errors.Is(err, io.EOF) {
		// The file is shorter than the device (a hole at the tail, or a file
		// that was never grown). A block device reads unwritten space as
		// zeros; returning EOF would fail the I/O instead.
		clear(p[n:])
		return len(p), nil
	}
	return n, err
}

func (l *loopBackend) WriteAt(p []byte, off int64) (int, error) {
	if off >= l.size {
		return 0, fmt.Errorf("write beyond end of device")
	}
	if int64(len(p)) > l.size-off {
		p = p[:l.size-off]
	}
	return l.f.WriteAt(p, off)
}

func (l *loopBackend) Size() int64 { return l.size }

// Flush is what the kernel sends when something above wants its writes durable
// — a filesystem journal commit, an fsync, a barrier. It only ever arrives if
// the device advertised a volatile write cache.
func (l *loopBackend) Flush() error { return l.f.Sync() }

func (l *loopBackend) Close() error {
	// Sync first: Close does not flush the page cache, and by this point the
	// device is already stopped, so this is the last chance to persist.
	if err := l.f.Sync(); err != nil {
		l.f.Close()
		return err
	}
	return l.f.Close()
}

// Discard deallocates the range, returning the space to the filesystem and
// making the region sparse again.
func (l *loopBackend) Discard(offset, length int64) error {
	if offset >= l.size {
		return nil
	}
	if length > l.size-offset {
		length = l.size - offset
	}

	if l.noPunch.Load() {
		return nil
	}

	err := unix.Fallocate(int(l.f.Fd()),
		unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, offset, length)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EOPNOTSUPP), errors.Is(err, unix.ENOSYS):
		// This filesystem cannot deallocate. Discard is advisory — the block
		// layer permits dropping it — and the honest alternative, writing the
		// range out as zeros, would turn a metadata operation into terabytes
		// of write amplification. So latch it off and stop trying.
		l.noPunch.Store(true)
		return nil
	default:
		return err
	}
}

// WriteZeroes must actually leave zeros behind, so unlike Discard it cannot be
// dropped when the filesystem has no punch-hole: fall back to writing them.
func (l *loopBackend) WriteZeroes(offset, length int64) error {
	if offset >= l.size {
		return nil
	}
	if length > l.size-offset {
		length = l.size - offset
	}

	if !l.noPunch.Load() {
		// A punched hole reads back as zeros, which is what was asked for, and
		// costs no space.
		err := unix.Fallocate(int(l.f.Fd()),
			unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, offset, length)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.ENOSYS) {
			return err
		}
		l.noPunch.Store(true)
	}

	zeros := make([]byte, 64*1024)
	for pos := offset; pos < offset+length; {
		n := min(int64(len(zeros)), offset+length-pos)
		written, err := l.f.WriteAt(zeros[:n], pos)
		if err != nil {
			return err
		}
		pos += int64(written)
	}
	return nil
}

// Compile-time interface checks
var (
	_ ublk.Backend            = (*loopBackend)(nil)
	_ ublk.DiscardBackend     = (*loopBackend)(nil)
	_ ublk.WriteZeroesBackend = (*loopBackend)(nil)
)
