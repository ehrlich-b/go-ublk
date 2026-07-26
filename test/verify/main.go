//go:build linux

// Command verify is a standalone, dependency-free data-integrity oracle for a
// ublk (or any) block device. It drives a block device (e.g. /dev/ublkbN) with
// a randomized, reproducible op stream while maintaining an in-process "shadow"
// copy of every byte it writes, then compares reads — and a final full-device
// read-back — against that shadow. Any offset/aliasing/corruption bug in the
// block path surfaces as a byte-exact mismatch, independent of fio.
//
// Soundness under concurrency: the device is partitioned into one disjoint
// stripe per worker, so no two workers ever touch the same bytes and the
// expected state is always well-defined even with many I/Os in flight across
// queues. This exercises the multi-queue data path (the class of the original
// descriptor-mmap-stride bug) without making the oracle itself racy.
//
// It imports only the standard library (CGO-free), so it cross-compiles for
// linux/amd64 and linux/arm64 and runs as a static binary on a bare test box
// with no Go toolchain. Failures print the seed for exact reproduction.
package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// align is a superset alignment that satisfies O_DIRECT on both 512e and 4Kn
// devices: every offset and length we issue is a multiple of it, and every
// buffer is aligned to it.
const align = 4096

// blkGetSize64 is the BLKGETSIZE64 ioctl (_IOR(0x12,114,size_t)); returns the
// device size in bytes via a *uint64.
const blkGetSize64 = 0x80081272

func main() {
	var (
		devPath  = flag.String("device", "", "block device to test, e.g. /dev/ublkb0 (required)")
		workers  = flag.Int("workers", 4, "concurrent workers (each owns a disjoint stripe)")
		duration = flag.Duration("duration", 20*time.Second, "how long to run the random phase")
		maxIO    = flag.Int("maxio", 256*1024, "max I/O length in bytes (aligned down)")
		direct   = flag.Bool("direct", false, "open with O_DIRECT")
		seed     = flag.Int64("seed", 0, "base RNG seed (0 = derive from wall clock)")
		sizeOvr  = flag.Int64("size", 0, "device size in bytes (0 = query via BLKGETSIZE64)")
	)
	flag.Parse()

	if *devPath == "" {
		fmt.Fprintln(os.Stderr, "verify: -device is required")
		os.Exit(2)
	}
	if *seed == 0 {
		*seed = time.Now().UnixNano()
	}
	maxio := (*maxIO / align) * align
	if maxio < align {
		maxio = align
	}

	// Probe the device size (one short-lived fd), then floor to alignment.
	size := *sizeOvr
	if size == 0 {
		fd, err := syscall.Open(*devPath, syscall.O_RDONLY, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "verify: open %s: %v\n", *devPath, err)
			os.Exit(1)
		}
		var sz uint64
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), blkGetSize64, uintptr(unsafe.Pointer(&sz)))
		syscall.Close(fd)
		if errno != 0 {
			fmt.Fprintf(os.Stderr, "verify: BLKGETSIZE64 %s: %v\n", *devPath, errno)
			os.Exit(1)
		}
		size = int64(sz)
	}
	size = (size / align) * align

	stripe := (size / int64(*workers) / align) * align
	if stripe < align {
		fmt.Fprintf(os.Stderr, "verify: device too small (%d bytes) for %d workers\n", size, *workers)
		os.Exit(1)
	}

	fmt.Printf("verify: device=%s size=%d stripe=%d workers=%d direct=%v maxio=%d seed=%d duration=%s\n",
		*devPath, size, stripe, *workers, *direct, maxio, *seed, *duration)

	openFlags := syscall.O_RDWR
	if *direct {
		openFlags |= syscall.O_DIRECT
	}

	var (
		wg       sync.WaitGroup
		failed   atomic.Bool
		firstErr atomic.Pointer[string]
		totalOps atomic.Int64
		deadline = time.Now().Add(*duration)
	)
	fail := func(format string, a ...any) {
		msg := fmt.Sprintf(format, a...)
		if firstErr.CompareAndSwap(nil, &msg) {
			failed.Store(true)
		}
	}

	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			base := int64(w) * stripe

			fd, err := syscall.Open(*devPath, openFlags, 0)
			if err != nil {
				fail("worker %d: open: %v", w, err)
				return
			}
			defer syscall.Close(fd)

			// Each worker keeps a byte-exact shadow of its own stripe. The
			// RNG is seeded per-worker so a failing run reproduces exactly.
			shadow := make([]byte, stripe)
			buf := alignedBuf(int(maxio))
			rng := newRNG(uint64(*seed)*2654435761 + uint64(w) + 1)

			pwriteAt := func(off int64, n int) bool {
				for i := 0; i < n; i++ {
					buf[i] = byte(rng.next())
				}
				if _, err := pwriteFull(fd, buf[:n], base+off); err != nil {
					fail("worker %d: pwrite off=%d n=%d: %v", w, base+off, n, err)
					return false
				}
				copy(shadow[off:off+int64(n)], buf[:n])
				return true
			}
			preadVerify := func(off int64, n int) bool {
				rb := alignedBuf(n)
				if _, err := preadFull(fd, rb[:n], base+off); err != nil {
					fail("worker %d: pread off=%d n=%d: %v", w, base+off, n, err)
					return false
				}
				if bad := firstDiff(rb[:n], shadow[off:off+int64(n)]); bad >= 0 {
					fail("worker %d: MISMATCH at dev-offset %d (stripe-offset %d): got 0x%02x want 0x%02x\n%s",
						w, base+off+int64(bad), off+int64(bad), rb[bad], shadow[off+int64(bad)],
						hexContext(rb[:n], shadow[off:off+int64(n)], bad))
					return false
				}
				return true
			}

			// Phase 1: seed the whole stripe with known data (also a full
			// sequential-write pass), establishing a defined initial state.
			for off := int64(0); off < stripe; off += int64(maxio) {
				n := maxio
				if off+int64(n) > stripe {
					n = int(stripe - off)
				}
				if !pwriteAt(off, n) {
					return
				}
			}
			if !preadWholeStripe(fd, base, shadow, fail, w) {
				return
			}

			// Phase 2: randomized read/write with immediate read-after-write
			// plus independent random reads of previously-written regions.
			blocks := stripe / align
			for time.Now().Before(deadline) && !failed.Load() {
				n := (int(rng.next()%uint64(maxio))/align + 1) * align
				if int64(n) > stripe {
					n = int(stripe)
				}
				maxStart := blocks - int64(n/align)
				if maxStart < 0 {
					maxStart = 0
				}
				off := (int64(rng.next() % uint64(maxStart+1))) * align

				// write + immediate read-back at the same place
				if !pwriteAt(off, n) || !preadVerify(off, n) {
					return
				}
				// independent random read elsewhere in the stripe
				roff := (int64(rng.next() % uint64(maxStart+1))) * align
				if !preadVerify(roff, n) {
					return
				}
				if rng.next()%64 == 0 {
					_ = syscall.Fsync(fd)
				}
				totalOps.Add(1)
			}

			// Phase 3: final full-stripe read-back vs shadow.
			_ = syscall.Fsync(fd)
			preadWholeStripe(fd, base, shadow, fail, w)
		}(w)
	}

	wg.Wait()

	if failed.Load() {
		fmt.Fprintf(os.Stderr, "\nFAIL (seed=%d): %s\n", *seed, *firstErr.Load())
		os.Exit(1)
	}
	fmt.Printf("PASS: %d random ops, final full-device read-back byte-exact (seed=%d)\n", totalOps.Load(), *seed)
}

// preadWholeStripe reads a worker's entire stripe and compares to its shadow.
func preadWholeStripe(fd int, base int64, shadow []byte, fail func(string, ...any), w int) bool {
	rb := alignedBuf(len(shadow))
	if _, err := preadFull(fd, rb, base); err != nil {
		fail("worker %d: full pread: %v", w, err)
		return false
	}
	if bad := firstDiff(rb, shadow); bad >= 0 {
		fail("worker %d: FULL-READBACK MISMATCH at dev-offset %d (stripe-offset %d): got 0x%02x want 0x%02x\n%s",
			w, base+int64(bad), bad, rb[bad], shadow[bad], hexContext(rb, shadow, bad))
		return false
	}
	return true
}

// pwriteFull / preadFull loop over short writes/reads.
func pwriteFull(fd int, p []byte, off int64) (int, error) {
	total := 0
	for total < len(p) {
		n, err := syscall.Pwrite(fd, p[total:], off+int64(total))
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, fmt.Errorf("pwrite returned 0")
		}
		total += n
	}
	return total, nil
}

func preadFull(fd int, p []byte, off int64) (int, error) {
	total := 0
	for total < len(p) {
		n, err := syscall.Pread(fd, p[total:], off+int64(total))
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, fmt.Errorf("pread returned 0 (short read at off=%d)", off+int64(total))
		}
		total += n
	}
	return total, nil
}

// firstDiff returns the index of the first differing byte, or -1 if equal.
func firstDiff(a, b []byte) int {
	for i := range a {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}

// hexContext renders a few bytes around a mismatch for quick diagnosis.
func hexContext(got, want []byte, at int) string {
	lo := at - 8
	if lo < 0 {
		lo = 0
	}
	hi := at + 8
	if hi > len(got) {
		hi = len(got)
	}
	return fmt.Sprintf("  got : % x\n  want: % x", got[lo:hi], want[lo:hi])
}

// alignedBuf returns a slice of length n whose backing array starts on an
// `align`-byte boundary (required for O_DIRECT).
func alignedBuf(n int) []byte {
	raw := make([]byte, n+align)
	off := int(align - (uintptr(unsafe.Pointer(&raw[0])) % align))
	if off == align {
		off = 0
	}
	return raw[off : off+n]
}

// rng is a tiny deterministic splitmix64 generator — no external deps, and
// seeding it per worker makes any failure reproducible from the printed seed.
type rng struct{ s uint64 }

func newRNG(seed uint64) *rng { return &rng{s: seed} }

func (r *rng) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}
