//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk"
)

// Registration order is run order: data-path tests first, the lifecycle tests
// that kill servers or race teardown last, so a test that wedges the kernel
// costs as few results as possible.
func init() {
	for _, c := range []struct {
		name                     string
		queues, depth, bs, maxIO int
	}{
		{"q1-d1-bs512", 1, 1, 512, 1 << 20},
		{"q1-d64-bs512", 1, 64, 512, 1 << 20},
		{"q2-d32-bs4096", 2, 32, 4096, 1 << 20},
		{"q4-d128-bs512", 4, 128, 512, 1 << 20},
		{"q4-d16-bs4096-maxio64k", 4, 16, 4096, 64 << 10},
	} {
		c := c
		register("io/integrity/"+c.name, 3*time.Minute, func(t *T) error {
			params, b := memParams(32 << 20)
			params.NumQueues, params.QueueDepth = c.queues, c.depth
			params.LogicalBlockSize, params.MaxIOSize = c.bs, c.maxIO
			return integrity(t, params, b, true, t.Duration(2*time.Second))
		})
	}
	register("io/integrity-buffered", 3*time.Minute, func(t *T) error {
		params, b := memParams(32 << 20)
		params.NumQueues, params.QueueDepth = 4, 64
		return integrity(t, params, b, false, t.Duration(2*time.Second))
	})
	register("io/boundaries", time.Minute, testBoundaries)
	register("io/flush", time.Minute, testFlush)
	register("io/no-volatile-cache", time.Minute, testNoVolatileCache)
	register("io/discard", time.Minute, testDiscard)
	register("io/discard-large", 2*time.Minute, testDiscardLarge)
	register("io/write-zeroes", time.Minute, testWriteZeroes)
	register("io/write-zeroes-large", 2*time.Minute, testWriteZeroesLarge)
	register("io/no-discard-advertised", time.Minute, testNoDiscardAdvertised)
	register("io/error-propagation", time.Minute, testErrorPropagation)
	register("io/readonly", time.Minute, testReadOnly)
	register("io/multi-device", 3*time.Minute, testMultiDevice)
	register("params/geometry", time.Minute, testGeometry)
	register("fs/ext4", 5*time.Minute, func(t *T) error { return testFilesystem(t, "ext4") })
	register("fs/xfs", 5*time.Minute, func(t *T) error { return testFilesystem(t, "xfs") })
}

// integrity drives the device with random reads and writes from several
// workers, each owning a disjoint stripe so the expected contents are always
// well defined, and compares every read — and finally the whole device, read
// both through the kernel and straight from the backend — against a shadow
// copy. Writes deliberately exceed MaxIOSize sometimes, so the kernel's request
// splitting is exercised too.
func integrity(t *T, params ublk.DeviceParams, b *memBackend, direct bool, dur time.Duration) error {
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	flags := os.O_RDWR
	if direct {
		flags |= syscall.O_DIRECT
	}
	f, err := os.OpenFile(dev.Path, flags, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())

	size := b.Size()
	workers := params.NumQueues * 2
	stripe := (size / int64(workers)) &^ 4095
	shadow := make([]byte, size)
	unit := 4096 // O_DIRECT needs logical-block alignment; 4096 covers 512 and 4Kn
	if !direct {
		unit = 1 // buffered I/O may be byte-granular
	}
	maxLen := min(2*params.MaxIOSize, int(stripe))

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	deadline := time.Now().Add(dur)
	var ops atomic.Int64
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := newRNG(uint64(w+1) * 0x51ed27)
			base := int64(w) * stripe
			buf := alignedBuf(maxLen)
			for n := 0; time.Now().Before(deadline) || n < 8; n++ {
				length := (1 + r.intn(maxLen/unit)) * unit
				off := base + int64(r.intn(int(stripe)-length+1)/unit*unit)
				p := buf[:length]
				if r.intn(3) > 0 {
					r.fill(p)
					if err := pwriteFull(fd, p, off); err != nil {
						errs <- fmt.Errorf("worker %d: write %d@%d: %w", w, length, off, err)
						return
					}
					copy(shadow[off:], p)
				} else {
					if err := preadFull(fd, p, off); err != nil {
						errs <- fmt.Errorf("worker %d: read %d@%d: %w", w, length, off, err)
						return
					}
					if i := firstDiff(p, shadow[off:off+int64(length)]); i >= 0 {
						errs <- fmt.Errorf("worker %d: read %d@%d mismatches at byte %d", w, length, off, off+int64(i))
						return
					}
				}
				ops.Add(1)
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}

	// The whole device through the kernel...
	got := alignedBuf(int(stripe) * workers)
	if err := preadFull(fd, got, 0); err != nil {
		return fmt.Errorf("final read-back: %w", err)
	}
	if i := firstDiff(got, shadow[:len(got)]); i >= 0 {
		return fmt.Errorf("final read-back mismatches at byte %d", i)
	}
	// ...and straight from the backend, which catches a kernel-side cache
	// hiding a write that never reached the server.
	if _, err := b.ReadAt(got, 0); err != nil {
		return err
	}
	if i := firstDiff(got, shadow[:len(got)]); i >= 0 {
		return fmt.Errorf("backend contents mismatch shadow at byte %d after fsync", i)
	}
	t.Logf("%d ops, %d workers, direct=%v", ops.Load(), workers, direct)
	return nil
}

func testBoundaries(t *T) error {
	const size = 16 << 20
	params, b := memParams(size)
	params.MaxIOSize = 128 << 10
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())
	r := newRNG(7)

	for _, c := range []struct {
		off    int64
		length int
	}{
		{0, 4096},
		{size - 4096, 4096},
		{4 << 20, 128 << 10},  // exactly MaxIOSize
		{8 << 20, 1 << 20},    // 8x MaxIOSize: the kernel must split it
		{12<<20 + 4096, 8192}, // unaligned to MaxIOSize
	} {
		p := alignedBuf(c.length)
		r.fill(p)
		writesBefore := b.writes.Load()
		if err := pwriteFull(fd, p, c.off); err != nil {
			return fmt.Errorf("write %d@%d: %w", c.length, c.off, err)
		}
		q := alignedBuf(c.length)
		if err := preadFull(fd, q, c.off); err != nil {
			return fmt.Errorf("read %d@%d: %w", c.length, c.off, err)
		}
		if !bytes.Equal(p, q) {
			return fmt.Errorf("%d@%d: read back differs at byte %d", c.length, c.off, firstDiff(p, q))
		}
		if c.length > params.MaxIOSize {
			if n := b.writes.Load() - writesBefore; n < int64(c.length/params.MaxIOSize) {
				return fmt.Errorf("%d-byte write reached the backend as %d requests; MaxIOSize %d means at least %d",
					c.length, n, params.MaxIOSize, c.length/params.MaxIOSize)
			}
		}
	}

	// Past the end: reads see EOF, writes fail; neither may reach the backend.
	p := alignedBuf(4096)
	if n, err := unix.Pread(fd, p, size); n != 0 || err != nil {
		return fmt.Errorf("read at end of device: n=%d err=%v, want EOF (0, nil)", n, err)
	}
	if _, err := unix.Pwrite(fd, p, size); err == nil {
		return fmt.Errorf("write at end of device succeeded")
	}
	return nil
}

func testFlush(t *T) error {
	params, b := memParams(16 << 20)
	params.VolatileCache = true
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	if wc, err := sysfsQueue(dev.Path, "write_cache"); err != nil || wc != "write back" {
		return fmt.Errorf("write_cache = %q (%v), want \"write back\"", wc, err)
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := pwriteFull(int(f.Fd()), alignedBuf(4096), 0); err != nil {
		return err
	}
	before := b.flushes.Load()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}
	if b.flushes.Load() == before {
		return fmt.Errorf("fsync on a volatile-cache device did not reach Backend.Flush")
	}
	// O_DSYNC writes need FUA; without advertised FUA the block layer must
	// emulate it with a flush after the write.
	g, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT|syscall.O_DSYNC, 0)
	if err != nil {
		return err
	}
	defer g.Close()
	before = b.flushes.Load()
	if err := pwriteFull(int(g.Fd()), alignedBuf(4096), 8192); err != nil {
		return err
	}
	if b.flushes.Load() == before {
		return fmt.Errorf("an O_DSYNC write did not produce a flush (FUA emulation)")
	}
	return nil
}

func testNoVolatileCache(t *T) error {
	params, b := memParams(16 << 20)
	params.VolatileCache = false
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	if wc, err := sysfsQueue(dev.Path, "write_cache"); err != nil || wc != "write through" {
		return fmt.Errorf("write_cache = %q (%v), want \"write through\"", wc, err)
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := pwriteFull(int(f.Fd()), alignedBuf(4096), 0); err != nil {
		return err
	}
	before := b.flushes.Load()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}
	if n := b.flushes.Load() - before; n != 0 {
		return fmt.Errorf("a write-through device received %d flushes", n)
	}
	return nil
}

// covered sums recorded ranges and checks they tile [off, off+length) exactly.
func covered(ops []rangeOp, off, length int64) error {
	var total int64
	for _, o := range ops {
		if o.off < off || o.off+o.length > off+length {
			return fmt.Errorf("range [%d,+%d) is outside the requested [%d,+%d)", o.off, o.length, off, length)
		}
		total += o.length
	}
	if total != length {
		return fmt.Errorf("backend saw %d bytes in %d ranges, want %d", total, len(ops), length)
	}
	return nil
}

func rangeTest(t *T, size, off, length int64, discard bool) error {
	params, b := memParams(size)
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	attr, req, what := "write_zeroes_max_bytes", uintptr(blkZeroOut), "write-zeroes"
	if discard {
		attr, req, what = "discard_max_bytes", uintptr(blkDiscard), "discard"
	}
	if v, err := sysfsInt(dev.Path, attr); err != nil || v == 0 {
		return fmt.Errorf("%s = %d (%v); the backend implements %s, so it must be advertised", attr, v, err, what)
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())

	// Surround the range with data that must survive, and fill its first MiB.
	pattern := alignedBuf(1 << 20)
	newRNG(3).fill(pattern)
	guards := []int64{off - int64(len(pattern)), off + length}
	for _, g := range guards {
		if g >= 0 && g+int64(len(pattern)) <= size {
			if err := pwriteFull(fd, pattern, g); err != nil {
				return err
			}
		}
	}
	if err := pwriteFull(fd, pattern, off); err != nil {
		return err
	}

	if err := blkRange(fd, req, off, length); err != nil {
		return fmt.Errorf("%s [%d,+%d): %w", what, off, length, err)
	}
	if err := covered(b.rangeOps(discard), off, length); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	got := alignedBuf(len(pattern))
	if err := preadFull(fd, got, off); err != nil {
		return err
	}
	if !bytes.Equal(got, make([]byte, len(got))) {
		return fmt.Errorf("%s range still holds data after the backend zeroed it", what)
	}
	for _, g := range guards {
		if g >= 0 && g+int64(len(pattern)) <= size {
			if err := preadFull(fd, got, g); err != nil {
				return err
			}
			if !bytes.Equal(got, pattern) {
				return fmt.Errorf("%s clobbered data at %d outside its range", what, g)
			}
		}
	}
	return nil
}

func testDiscard(t *T) error     { return rangeTest(t, 64<<20, 4<<20, 16<<20, true) }
func testWriteZeroes(t *T) error { return rangeTest(t, 64<<20, 4<<20, 16<<20, false) }

// The large variants are the regression test for Critical Bug #16: a range
// operation of 2 GiB or more used to complete with an int32-overflowed byte
// count, which the kernel turned into EIO. The backend is sparse, so a 6 GiB
// device costs only the bytes written.
func testDiscardLarge(t *T) error     { return rangeTest(t, 6<<30, 1<<20, 5<<30, true) }
func testWriteZeroesLarge(t *T) error { return rangeTest(t, 6<<30, 1<<20, 5<<30, false) }

func testNoDiscardAdvertised(t *T) error {
	m := newMemBackend(16 << 20)
	params := ublk.DefaultParams(plainBackend{m})
	params.NumQueues, params.QueueDepth = 1, 16
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	for _, attr := range []string{"discard_max_bytes", "write_zeroes_max_bytes"} {
		if v, err := sysfsInt(dev.Path, attr); err != nil || v != 0 {
			return fmt.Errorf("%s = %d (%v); the backend cannot do it, so it must not be advertised", attr, v, err)
		}
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := blkRange(int(f.Fd()), blkDiscard, 0, 1<<20); !errors.Is(err, syscall.EOPNOTSUPP) {
		return fmt.Errorf("BLKDISCARD on a device without discard: %v, want EOPNOTSUPP", err)
	}
	// BLKZEROOUT falls back to writing zero pages, which a plain backend handles.
	before := m.writes.Load()
	if err := blkRange(int(f.Fd()), blkZeroOut, 0, 1<<20); err != nil {
		return fmt.Errorf("BLKZEROOUT fallback: %w", err)
	}
	if m.writes.Load() == before {
		return fmt.Errorf("BLKZEROOUT fallback wrote nothing to the backend")
	}
	return nil
}

func testErrorPropagation(t *T) error {
	params, b := memParams(16 << 20)
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())
	p := alignedBuf(4096)

	b.setFailure(4<<20, 64<<10, syscall.EIO)
	if err := preadFull(fd, p, 4<<20); !errors.Is(err, syscall.EIO) {
		return fmt.Errorf("read of a failing range: %v, want EIO", err)
	}
	if err := pwriteFull(fd, p, 4<<20); !errors.Is(err, syscall.EIO) {
		return fmt.Errorf("write to a failing range: %v, want EIO", err)
	}
	// Other ranges, on every queue, must keep working: an I/O error is not a
	// queue error.
	for off := int64(0); off < 2<<20; off += 64 << 10 {
		if err := preadFull(fd, p, off); err != nil {
			return fmt.Errorf("read at %d after an injected error: %w", off, err)
		}
	}
	// Record how a specific errno surfaces: newer kernels translate it, older
	// ones collapse every failure to EIO. Either is a pass; the detail says which.
	b.setFailure(4<<20, 64<<10, syscall.ENOSPC)
	werr := pwriteFull(fd, p, 4<<20)
	if werr == nil {
		return fmt.Errorf("write to a failing range succeeded")
	}
	t.Logf("backend ENOSPC surfaced to userspace as %v", werr)
	b.setFailure(0, 0, nil)
	if err := pwriteFull(fd, p, 4<<20); err != nil {
		return fmt.Errorf("write after clearing the failure: %w", err)
	}
	return nil
}

func testReadOnly(t *T) error {
	params, b := memParams(16 << 20)
	params.ReadOnly = true
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dev.Path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	ro, err := ioctlInt(int(f.Fd()), blkROGet)
	f.Close()
	if err != nil || ro != 1 {
		return fmt.Errorf("BLKROGET = %d (%v), want 1", ro, err)
	}
	if g, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0); err == nil {
		werr := pwriteFull(int(g.Fd()), alignedBuf(4096), 0)
		g.Close()
		if werr == nil {
			return fmt.Errorf("write to a read-only device succeeded")
		}
	}
	if n := b.writes.Load(); n != 0 {
		return fmt.Errorf("read-only device's backend received %d writes", n)
	}
	return nil
}

func testMultiDevice(t *T) error {
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		params, b := memParams(16 << 20)
		params.NumQueues, params.QueueDepth = 2, 32
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := integrity(t, params, b, true, t.Duration(time.Second)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	return <-errs
}

func testGeometry(t *T) error {
	params, _ := memParams(64 << 20)
	params.LogicalBlockSize = 4096
	params.MaxIOSize = 256 << 10
	params.NumQueues, params.QueueDepth = 3, 32
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dev.Path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())
	if sz, err := blockSize64(fd); err != nil || sz != 64<<20 {
		return fmt.Errorf("BLKGETSIZE64 = %d (%v), want %d", sz, err, 64<<20)
	}
	if lbs, err := ioctlInt(fd, blkSSZGet); err != nil || lbs != 4096 {
		return fmt.Errorf("logical block size = %d (%v), want 4096", lbs, err)
	}
	if kb, err := sysfsInt(dev.Path, "max_hw_sectors_kb"); err != nil || kb != 256 {
		return fmt.Errorf("max_hw_sectors_kb = %d (%v), want 256", kb, err)
	}
	if rot, err := sysfsQueue(dev.Path, "rotational"); err != nil || rot != "0" {
		return fmt.Errorf("rotational = %q (%v), want 0", rot, err)
	}
	name := dev.Path[len("/dev/"):]
	ents, err := os.ReadDir("/sys/block/" + name + "/mq")
	if err != nil {
		return err
	}
	// The kernel clamps nr_hw_queues to the number of possible CPUs, and the
	// device must report the count it was actually granted.
	if q := dev.NumQueues(); q < 1 || q > 3 || len(ents) != q {
		return fmt.Errorf("%d hardware queues in sysfs, device reports %d, requested 3", len(ents), q)
	}
	return nil
}

func testFilesystem(t *T, fs string) error {
	mkfs := "mkfs." + fs
	if !hasCommand(mkfs) {
		return skipf("%s not installed", mkfs)
	}
	params, _ := memParams(512 << 20)
	params.NumQueues, params.QueueDepth = 2, 64
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	args := []string{"-q", dev.Path}
	if fs == "ext4" {
		args = []string{"-q", "-F", dev.Path}
	} else if fs == "xfs" {
		args = []string{"-q", "-f", dev.Path}
	}
	if err := run(mkfs, args...); err != nil {
		return err
	}
	mnt, err := os.MkdirTemp("", "ublk-suite-"+fs)
	if err != nil {
		return err
	}
	t.Cleanup(func() { _ = os.Remove(mnt) })
	if err := unix.Mount(dev.Path, mnt, fs, 0, ""); err != nil {
		if errors.Is(err, syscall.ENODEV) {
			return skipf("kernel has no %s support", fs)
		}
		return fmt.Errorf("mount %s: %w", fs, err)
	}
	mounted := true
	t.Cleanup(func() {
		if mounted {
			_ = unix.Unmount(mnt, 0)
		}
	})

	r := newRNG(11)
	files := map[string][]byte{}
	for i := 0; i < 24; i++ {
		data := make([]byte, 1+r.intn(3<<20))
		r.fill(data)
		name := fmt.Sprintf("%s/f%02d", mnt, i)
		if err := os.WriteFile(name, data, 0o644); err != nil {
			return err
		}
		files[name] = data
	}
	unix.Sync()
	if err := unix.Unmount(mnt, 0); err != nil {
		return fmt.Errorf("unmount: %w", err)
	}
	mounted = false

	switch fs {
	case "ext4":
		if hasCommand("e2fsck") {
			if err := run("e2fsck", "-fn", dev.Path); err != nil {
				return fmt.Errorf("fsck after unmount: %w", err)
			}
		}
	case "xfs":
		if hasCommand("xfs_repair") {
			if err := run("xfs_repair", "-n", dev.Path); err != nil {
				return fmt.Errorf("xfs_repair -n after unmount: %w", err)
			}
		}
	}

	if err := unix.Mount(dev.Path, mnt, fs, 0, ""); err != nil {
		return fmt.Errorf("remount: %w", err)
	}
	mounted = true
	for name, want := range files {
		got, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("%s differs after remount", name)
		}
	}
	if err := unix.Unmount(mnt, 0); err != nil {
		return fmt.Errorf("final unmount: %w", err)
	}
	mounted = false
	return nil
}
