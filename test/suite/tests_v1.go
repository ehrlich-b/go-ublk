//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk"
)

// Feature tests: one per optional ublk feature or go-ublk mode, each skipping
// cleanly on kernels without it. This file sorts after the lifecycle tests, so
// these run last.
func init() {
	register("features/probe", time.Minute, testProbe)
	for _, m := range []struct {
		name string
		need ublk.Features
		set  func(*ublk.DeviceParams)
	}{
		{"inline", 0, func(p *ublk.DeviceParams) { p.Inline = true }},
		{"user-copy", ublk.FeatureUserCopy, func(p *ublk.DeviceParams) { p.EnableUserCopy = true }},
		{"need-get-data", ublk.FeatureNeedGetData, func(p *ublk.DeviceParams) { p.NeedGetData = true }},
		{"threads-per-queue", ublk.FeaturePerIODaemon, func(p *ublk.DeviceParams) { p.ThreadsPerQueue = 4 }},
		{"batch-io", ublk.FeatureBatchIO, func(p *ublk.DeviceParams) { p.BatchIO = true }},
		{"batch-io-user-copy", ublk.FeatureBatchIO | ublk.FeatureUserCopy, func(p *ublk.DeviceParams) {
			p.BatchIO, p.EnableUserCopy = true, true
		}},
	} {
		m := m
		register("features/integrity-"+m.name, 3*time.Minute, func(t *T) error {
			if err := needFeatures(m.need); err != nil {
				return err
			}
			params, b := memParams(32 << 20)
			params.NumQueues, params.QueueDepth = 2, 64
			m.set(&params)
			return integrity(t, params, b, true, t.Duration(2*time.Second))
		})
	}
	register("features/zero-copy", 3*time.Minute, testZeroCopy)
	register("features/batch-io-close-under-load", 2*time.Minute, func(t *T) error {
		if err := needFeatures(ublk.FeatureBatchIO); err != nil {
			return err
		}
		return closeUnderLoad(t, func(p *ublk.DeviceParams) { p.BatchIO = true })
	})
	register("features/zero-copy-close-under-load", 2*time.Minute, func(t *T) error {
		if err := needFeatures(ublk.FeatureZeroCopy); err != nil {
			return err
		}
		path := filepath.Join(os.TempDir(), fmt.Sprintf("ublk-suite-zcl-%d", os.Getpid()))
		t.Cleanup(func() { _ = os.Remove(path) })
		fb, err := openFileBackend(path, 64<<20)
		if err != nil {
			return err
		}
		t.Cleanup(func() { _ = fb.Close() })
		return closeUnderLoad(t, func(p *ublk.DeviceParams) {
			p.Backend = zcFile{fb}
			p.EnableZeroCopy = true
		})
	})
	register("features/handler-async", 3*time.Minute, testHandlerAsync)
	register("features/fua", time.Minute, testFUA)
	register("features/tag-find", time.Minute, testTagFind)
	register("features/resize", time.Minute, testResize)
	register("features/no-partition-scan", time.Minute, testNoPartitionScan)
	register("features/safe-stop", time.Minute, testSafeStop)
	register("lifecycle/ctx-cancel-under-load", 2*time.Minute, testCtxCancelUnderLoad)
	register("recovery/kill-and-recover", 3*time.Minute, testKillAndRecover)
	register("recovery/detach-handoff", 3*time.Minute, testDetachHandoff)
}

var (
	probeOnce sync.Once
	probed    ublk.KernelSupport
	probeErr  error
)

// needFeatures skips the test unless the kernel reports every feature in f.
func needFeatures(f ublk.Features) error {
	probeOnce.Do(func() { probed, probeErr = ublk.Probe() })
	if probeErr != nil {
		return probeErr
	}
	if f != 0 && (!probed.Known || !probed.Features.Has(f)) {
		return skipf("kernel lacks %s (reports %s, known=%v)", f&^probed.Features, probed.Features, probed.Known)
	}
	return nil
}

func testProbe(t *T) error {
	if err := needFeatures(0); err != nil {
		return err
	}
	t.Logf("kernel features %s (known=%v)", probed.Features, probed.Known)
	return nil
}

// testHandlerAsync serves a device with a raw Handler that completes every
// request from another goroutine after a random delay, over its own RAM store,
// and runs the integrity workload against it.
func testHandlerAsync(t *T) error {
	const size = 32 << 20
	store := make([]byte, size)
	var mu sync.RWMutex
	h := ublk.HandlerFunc(func(r *ublk.Request) {
		go func() {
			if rand.Intn(3) == 0 {
				time.Sleep(time.Duration(rand.Intn(300)) * time.Microsecond)
			}
			switch r.Op {
			case ublk.OpRead:
				mu.RLock()
				copy(r.Data, store[r.Offset:])
				mu.RUnlock()
				r.Complete(nil)
			case ublk.OpWrite:
				mu.Lock()
				copy(store[r.Offset:], r.Data)
				mu.Unlock()
				r.Complete(nil)
			case ublk.OpFlush:
				r.Complete(nil)
			default:
				r.Complete(syscall.EOPNOTSUPP)
			}
		}()
	})
	params := ublk.DefaultParams(nil)
	params.Backend = nil
	params.Handler = h
	params.Size = size
	params.NumQueues, params.QueueDepth = 2, 64
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
	r := newRNG(42)
	shadow := make([]byte, size)
	buf := alignedBuf(256 << 10)
	deadline := time.Now().Add(t.Duration(2 * time.Second))
	ops := 0
	for ; time.Now().Before(deadline) || ops < 32; ops++ {
		n := (1 + r.intn(64)) * 4096
		off := int64(r.intn((size-n)/4096)) * 4096
		p := buf[:n]
		r.fill(p)
		if err := pwriteFull(fd, p, off); err != nil {
			return fmt.Errorf("write %d@%d: %w", n, off, err)
		}
		copy(shadow[off:], p)
		if err := preadFull(fd, p, off); err != nil {
			return fmt.Errorf("read %d@%d: %w", n, off, err)
		}
		if i := firstDiff(p, shadow[off:off+int64(n)]); i >= 0 {
			return fmt.Errorf("read %d@%d differs at %d", n, off, i)
		}
	}
	mu.RLock()
	defer mu.RUnlock()
	if !bytes.Equal(store, shadow) {
		return fmt.Errorf("handler store differs from shadow after %d ops", ops)
	}
	t.Logf("%d verified ops through an async Handler", ops)
	return nil
}

// fuaMem is a memBackend that also honors per-I/O FUA, counting such writes.
type fuaMem struct {
	*memBackend
	fua atomic.Int64
}

func (f *fuaMem) WriteAtFUA(p []byte, off int64) (int, error) {
	f.fua.Add(1)
	return f.WriteAt(p, off)
}

func testFUA(t *T) error {
	b := &fuaMem{memBackend: newMemBackend(16 << 20)}
	params := ublk.DefaultParams(b)
	params.NumQueues, params.QueueDepth = 1, 16
	params.VolatileCache, params.EnableFUA = true, true
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	if v, err := sysfsQueue(dev.Path, "fua"); err != nil || v != "1" {
		return fmt.Errorf("queue/fua = %q (%v), want 1", v, err)
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT|syscall.O_DSYNC, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := pwriteFull(int(f.Fd()), alignedBuf(4096), 0); err != nil {
		return err
	}
	if b.fua.Load() == 0 {
		return fmt.Errorf("an O_DSYNC write on a FUA device never reached WriteAtFUA")
	}
	return nil
}

func testTagFind(t *T) error {
	params, _ := memParams(16 << 20)
	params.Tag = uint64(time.Now().UnixNano()) | 1
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	info, err := dev.KernelInfo()
	if err != nil {
		return err
	}
	if info.Tag != params.Tag || info.State != ublk.KernelStateLive {
		return fmt.Errorf("kernel info tag %#x state %s; want %#x live", info.Tag, info.State, params.Tag)
	}
	ids, err := ublk.FindDevices(params.Tag)
	if err != nil {
		return err
	}
	if len(ids) != 1 || ids[0] != dev.ID {
		return fmt.Errorf("FindDevices(%#x) = %v, want [%d]", params.Tag, ids, dev.ID)
	}
	return nil
}

func testResize(t *T) error {
	if err := needFeatures(ublk.FeatureUpdateSize); err != nil {
		return err
	}
	params, b := memParams(64 << 20)
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	if !dev.Features().Has(ublk.FeatureUpdateSize) {
		return fmt.Errorf("kernel supports UPDATE_SIZE but the device did not negotiate it (features %s)", dev.Features())
	}
	b.size = 128 << 20 // the backend grows first
	if err := dev.Resize(128 << 20); err != nil {
		return err
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if sz, err := blockSize64(int(f.Fd())); err != nil || sz != 128<<20 {
		return fmt.Errorf("BLKGETSIZE64 after Resize = %d (%v), want %d", sz, err, 128<<20)
	}
	p := alignedBuf(4096)
	newRNG(9).fill(p)
	if err := pwriteFull(int(f.Fd()), p, 100<<20); err != nil {
		return fmt.Errorf("write beyond the old end: %w", err)
	}
	return nil
}

// mbrWithPartition returns a sector-0 MBR holding one Linux partition from
// sector 2048 to the end of a size-byte disk.
func mbrWithPartition(size int64) []byte {
	b := make([]byte, 512)
	e := b[446:462]
	e[4] = 0x83
	le32 := func(p []byte, v uint32) { p[0], p[1], p[2], p[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24) }
	le32(e[8:12], 2048)
	le32(e[12:16], uint32(size/512-2048))
	b[510], b[511] = 0x55, 0xaa
	return b
}

func testNoPartitionScan(t *T) error {
	if err := needFeatures(ublk.FeatureNoPartScan); err != nil {
		return err
	}
	for _, scan := range []bool{true, false} {
		params, b := memParams(64 << 20)
		params.NoPartitionScan = !scan
		if _, err := b.WriteAt(mbrWithPartition(b.Size()), 0); err != nil {
			return err
		}
		dev, err := newDevice(t, params)
		if err != nil {
			return err
		}
		part := dev.Path + "p1"
		appeared := waitForNode(part, 3*time.Second) == nil
		if appeared != scan {
			return fmt.Errorf("NoPartitionScan=%v: %s appeared=%v", !scan, part, appeared)
		}
		if err := dev.Close(); err != nil {
			return err
		}
	}
	return nil
}

func testSafeStop(t *T) error {
	if err := needFeatures(ublk.FeatureSafeStop); err != nil {
		return err
	}
	params, b := memParams(16 << 20)
	params.SafeStop = true
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	// udev may hold the device briefly after creation; wait for our open to be
	// the only one by retrying until Stop's refusal is about us.
	if err := dev.Stop(); !errors.Is(err, ublk.ErrDeviceBusy) {
		f.Close()
		return fmt.Errorf("Stop with the device open = %v, want ErrDeviceBusy", err)
	}
	before := b.writes.Load()
	if err := pwriteFull(int(f.Fd()), alignedBuf(4096), 0); err != nil {
		f.Close()
		return fmt.Errorf("write after a refused Stop: %w", err)
	}
	f.Close()
	if b.writes.Load() == before {
		return fmt.Errorf("device stopped serving after a refused Stop")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		err = dev.Stop()
		if !errors.Is(err, ublk.ErrDeviceBusy) || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond) // udev's probe may still hold it
	}
	if err != nil {
		return fmt.Errorf("Stop after closing: %w", err)
	}
	return nil
}

// testCtxCancelUnderLoad cancels the serving context while writers are busy.
// Cancellation must stop the device gracefully: Done closes with a nil Err,
// writers see errors rather than hang, and Close then deletes it. (Before the
// v1 engine, cancelling killed the queues under in-flight I/O and wedged the
// kernel's partition scan on 7.0.0-38.)
func testCtxCancelUnderLoad(t *T) error {
	mark := kmsgMark()
	params, _ := memParams(64 << 20)
	params.NumQueues, params.QueueDepth = 2, 64
	ctx, cancel := context.WithCancel(context.Background())
	dev, err := ublk.CreateAndServe(ctx, params, nil)
	if err != nil {
		cancel()
		return err
	}
	t.Cleanup(func() { _ = dev.Close() })
	if err := waitForNode(dev.Path, 5*time.Second); err != nil {
		cancel()
		return err
	}
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
		if err != nil {
			cancel()
			return err
		}
		wg.Add(1)
		go func(w int, f *os.File) {
			defer wg.Done()
			defer f.Close()
			r := newRNG(uint64(w + 7))
			p := alignedBuf(64 << 10)
			for {
				if err := pwriteFull(int(f.Fd()), p, int64(r.intn(1000))*(64<<10)); err != nil {
					return
				}
			}
		}(w, f)
	}
	time.Sleep(t.Duration(300 * time.Millisecond))
	cancel()
	select {
	case <-dev.Done():
		if err := dev.Err(); err != nil {
			return fmt.Errorf("context cancel ended serving with %v, want a clean stop", err)
		}
	case <-time.After(60 * time.Second):
		return fmt.Errorf("Done not closed 60s after the context was cancelled")
	}
	writers := make(chan struct{})
	go func() { wg.Wait(); close(writers) }()
	select {
	case <-writers:
	case <-time.After(30 * time.Second):
		return fmt.Errorf("writers still blocked 30s after the device stopped")
	}
	if err := dev.Close(); err != nil {
		return fmt.Errorf("Close after cancel: %w", err)
	}
	if p := kernelProblems(mark); p != "" {
		return fmt.Errorf("kernel log: %s", p)
	}
	return nil
}

// verifiedWriter writes a deterministic pattern over [0, span) in 64 KiB
// blocks, round after round, recording which generation each block holds. It
// returns the first error.
type verifiedWriter struct {
	f    *os.File
	span int64
	gen  []uint32 // per block, last generation written successfully
	errs atomic.Int64
	ops  atomic.Int64
	err  error
}

const vwBlock = 64 << 10

func fillBlock(p []byte, block int64, gen uint32) {
	r := newRNG(uint64(block)<<32 | uint64(gen) | 1)
	r.fill(p)
}

func (w *verifiedWriter) run(stop <-chan struct{}) {
	p := alignedBuf(vwBlock)
	blocks := w.span / vwBlock
	for g := uint32(1); ; g++ {
		for b := int64(0); b < blocks; b++ {
			select {
			case <-stop:
				return
			default:
			}
			fillBlock(p, b, g)
			if err := pwriteFull(int(w.f.Fd()), p, b*vwBlock); err != nil {
				w.err = fmt.Errorf("write block %d gen %d: %w", b, g, err)
				return
			}
			w.gen[b] = g
			w.ops.Add(1)
		}
	}
}

func (w *verifiedWriter) verify() error {
	p, want := alignedBuf(vwBlock), make([]byte, vwBlock)
	for b, g := range w.gen {
		if err := preadFull(int(w.f.Fd()), p, int64(b)*vwBlock); err != nil {
			return fmt.Errorf("read block %d: %w", b, err)
		}
		if g == 0 {
			continue
		}
		fillBlock(want, int64(b), g)
		if !bytes.Equal(p, want) {
			return fmt.Errorf("block %d does not hold generation %d", b, g)
		}
	}
	return nil
}

func waitKernelState(id uint32, want ublk.KernelDeviceState, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, err := ublk.GetDeviceInfo(id)
		if err == nil && info.State == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("device %d state %s (err %v), want %s", id, info.State, err, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// testKillAndRecover SIGKILLs a server created with RecoveryReissue while a
// writer is busy, then takes the device over in this process. The block device
// must survive, the writer's in-flight and queued I/O must complete without
// error once recovered, and every acknowledged block must read back intact.
func testKillAndRecover(t *T) error {
	if err := needFeatures(ublk.FeatureUserRecovery | ublk.FeatureRecoveryReissue); err != nil {
		return err
	}
	mark := kmsgMark()
	const size = 32 << 20
	path := filepath.Join(os.TempDir(), fmt.Sprintf("ublk-suite-recover-%d", os.Getpid()))
	t.Cleanup(func() { _ = os.Remove(path) })
	cmd, id, err := startServer(t, size, "-file", path, "-recovery")
	if err != nil {
		return err
	}
	dpath := fmt.Sprintf("/dev/ublkb%d", id)
	if err := waitForNode(dpath, 5*time.Second); err != nil {
		return err
	}
	f, err := os.OpenFile(dpath, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	w := &verifiedWriter{f: f, span: 8 << 20, gen: make([]uint32, (8<<20)/vwBlock)}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { w.run(stop); close(done) }()
	time.Sleep(t.Duration(300 * time.Millisecond))

	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	_ = cmd.Wait()
	if err := waitKernelState(id, ublk.KernelStateQuiesced, 30*time.Second); err != nil {
		return fmt.Errorf("after SIGKILL: %w", err)
	}
	before := w.ops.Load()

	b, err := openFileBackend(path, size)
	if err != nil {
		return err
	}
	t.Cleanup(func() { _ = b.Close() })
	params := ublk.DefaultParams(b)
	dev, err := ublk.Recover(context.Background(), id, params, nil)
	if err != nil {
		return fmt.Errorf("Recover: %w", err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	time.Sleep(t.Duration(300 * time.Millisecond))
	close(stop)
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		return fmt.Errorf("writer still blocked 60s after Recover")
	}
	if w.err != nil {
		return fmt.Errorf("writer failed across the crash: %w", w.err)
	}
	if w.ops.Load() == before {
		return fmt.Errorf("no writes completed after Recover")
	}
	if err := w.verify(); err != nil {
		return err
	}
	t.Logf("%d writes, %d after recovery", w.ops.Load(), w.ops.Load()-before)
	if p := kernelProblems(mark); p != "" {
		return fmt.Errorf("kernel log: %s", p)
	}
	return nil
}

// testDetachHandoff is a zero-downtime upgrade: the old server Detaches and
// exits while a writer is busy, and this process Recovers the device. The
// writer must see no error at all and every block must read back intact.
func testDetachHandoff(t *T) error {
	if err := needFeatures(ublk.FeatureUserRecovery | ublk.FeatureRecoveryReissue); err != nil {
		return err
	}
	mark := kmsgMark()
	const size = 32 << 20
	path := filepath.Join(os.TempDir(), fmt.Sprintf("ublk-suite-handoff-%d", os.Getpid()))
	t.Cleanup(func() { _ = os.Remove(path) })
	cmd, id, err := startServer(t, size, "-file", path, "-recovery", "-detach-on-usr1")
	if err != nil {
		return err
	}
	dpath := fmt.Sprintf("/dev/ublkb%d", id)
	if err := waitForNode(dpath, 5*time.Second); err != nil {
		return err
	}
	f, err := os.OpenFile(dpath, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	w := &verifiedWriter{f: f, span: 8 << 20, gen: make([]uint32, (8<<20)/vwBlock)}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { w.run(stop); close(done) }()
	time.Sleep(t.Duration(300 * time.Millisecond))

	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("old server exited with %v after Detach", err)
	}
	// Detach drains what handlers hold and lets go; it must not wait out a
	// timeout (it once took 30s under load: fetches re-armed by late commits
	// after QUIESCE_DEV were never aborted).
	if took := time.Since(start); took > 15*time.Second {
		return fmt.Errorf("Detach under load took %s", took.Round(time.Millisecond))
	} else {
		t.Logf("Detach under load took %s", took.Round(time.Millisecond))
	}
	before := w.ops.Load()
	b, err := openFileBackend(path, size)
	if err != nil {
		return err
	}
	t.Cleanup(func() { _ = b.Close() })
	dev, err := ublk.Recover(context.Background(), id, ublk.DefaultParams(b), nil)
	if err != nil {
		return fmt.Errorf("Recover after Detach: %w", err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	time.Sleep(t.Duration(300 * time.Millisecond))
	close(stop)
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		return fmt.Errorf("writer still blocked 60s after Recover")
	}
	if w.err != nil {
		return fmt.Errorf("writer saw an error across the handoff: %w", w.err)
	}
	if w.ops.Load() == before {
		return fmt.Errorf("no writes completed after the handoff")
	}
	if err := w.verify(); err != nil {
		return err
	}
	t.Logf("%d writes, %d after the handoff", w.ops.Load(), w.ops.Load()-before)
	if p := kernelProblems(mark); p != "" {
		return fmt.Errorf("kernel log: %s", p)
	}
	return nil
}

// zcFile is a file backend that exposes its descriptor for zero copy.
type zcFile struct{ *fileBackend }

func (z zcFile) ZeroCopyFile() (int, int64) { return int(z.f.Fd()), 0 }

// testZeroCopy serves a file-backed device zero-copy and checks data,
// discard, write-zeroes and FUA against the file itself, which the kernel
// reads and writes directly.
func testZeroCopy(t *T) error {
	if err := needFeatures(ublk.FeatureZeroCopy); err != nil {
		return err
	}
	const size = 64 << 20
	path := filepath.Join(os.TempDir(), fmt.Sprintf("ublk-suite-zc-%d", os.Getpid()))
	t.Cleanup(func() { _ = os.Remove(path) })
	fb, err := openFileBackend(path, size)
	if err != nil {
		return err
	}
	t.Cleanup(func() { _ = fb.Close() })
	params := ublk.DefaultParams(zcFile{fb})
	params.EnableZeroCopy = true
	params.EnableFUA = true
	params.NumQueues, params.QueueDepth = 2, 64
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	if !dev.Features().Has(ublk.FeatureZeroCopy) {
		return fmt.Errorf("device did not negotiate zero copy: %s", dev.Features())
	}
	t.Logf("features %s", dev.Features())
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())

	// Random writes and reads through the device, verified against the file.
	r := newRNG(77)
	shadow := make([]byte, size)
	buf := alignedBuf(512 << 10)
	deadline := time.Now().Add(t.Duration(2 * time.Second))
	ops := 0
	for ; time.Now().Before(deadline) || ops < 64; ops++ {
		n := (1 + r.intn(128)) * 4096
		off := int64(r.intn((size-n)/4096)) * 4096
		p := buf[:n]
		if r.intn(3) > 0 {
			r.fill(p)
			if err := pwriteFull(fd, p, off); err != nil {
				return fmt.Errorf("write %d@%d: %w", n, off, err)
			}
			copy(shadow[off:], p)
		} else {
			if err := preadFull(fd, p, off); err != nil {
				return fmt.Errorf("read %d@%d: %w", n, off, err)
			}
			if i := firstDiff(p, shadow[off:off+int64(n)]); i >= 0 {
				return fmt.Errorf("read %d@%d differs at %d", n, off, i)
			}
		}
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}
	got := make([]byte, size)
	if _, err := fb.f.ReadAt(got, 0); err != nil {
		return err
	}
	if i := firstDiff(got, shadow); i >= 0 {
		return fmt.Errorf("backing file differs from what was written at byte %d", i)
	}

	// Discard and write-zeroes become fallocate on the file.
	if err := blkRange(fd, blkDiscard, 4<<20, 8<<20); err != nil {
		return fmt.Errorf("discard: %w", err)
	}
	if err := blkRange(fd, blkZeroOut, 16<<20, 4<<20); err != nil {
		return fmt.Errorf("zeroout: %w", err)
	}
	zero := make([]byte, 4<<20)
	for _, off := range []int64{4 << 20, 16 << 20} {
		if _, err := fb.f.ReadAt(got[:4<<20], off); err != nil {
			return err
		}
		if !bytes.Equal(got[:4<<20], zero) {
			return fmt.Errorf("backing file not zeroed at %d after discard/zeroout", off)
		}
	}

	// FUA writes are advertised and accepted.
	if v, err := sysfsQueue(dev.Path, "fua"); err != nil || v != "1" {
		return fmt.Errorf("queue/fua = %q (%v), want 1", v, err)
	}
	g, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT|syscall.O_DSYNC, 0)
	if err != nil {
		return err
	}
	defer g.Close()
	if err := pwriteFull(int(g.Fd()), alignedBuf(4096), 0); err != nil {
		return fmt.Errorf("O_DSYNC write: %w", err)
	}
	t.Logf("%d zero-copy ops verified against the backing file", ops)
	return nil
}

func init() {
	register("features/zoned", 3*time.Minute, testZoned)
}

// Zoned block device ioctls (include/uapi/linux/blkzoned.h).
const (
	blkReportZone = 0xc0101282 // _IOWR(0x12, 130, struct blk_zone_report)
	blkResetZone  = 0x40101283 // _IOW(0x12, 131, struct blk_zone_range)
	blkOpenZone   = 0x40101286 // _IOW(0x12, 134, ...)
	blkCloseZone  = 0x40101287 // _IOW(0x12, 135, ...)
	blkFinishZone = 0x40101288 // _IOW(0x12, 136, ...)
)

// zonedMem is a host-managed zoned device in RAM: sequential-write-required
// zones with write pointers, served through the raw Handler interface.
type zonedMem struct {
	mu       sync.Mutex
	data     []byte
	zoneSize int64
	wp       []int64 // absolute byte offsets
	cond     []uint8
}

func newZonedMem(size, zoneSize int64) *zonedMem {
	n := size / zoneSize
	z := &zonedMem{data: make([]byte, size), zoneSize: zoneSize, wp: make([]int64, n), cond: make([]uint8, n)}
	for i := range z.wp {
		z.wp[i] = int64(i) * zoneSize
		z.cond[i] = ublk.ZoneCondEmpty
	}
	return z
}

func (z *zonedMem) zone(off int64) int { return int(off / z.zoneSize) }

func (z *zonedMem) HandleRequest(r *ublk.Request) {
	z.mu.Lock()
	defer z.mu.Unlock()
	i := z.zone(r.Offset)
	end := func(i int) int64 { return int64(i+1) * z.zoneSize }
	switch r.Op {
	case ublk.OpRead:
		copy(r.Data, z.data[r.Offset:])
		r.Complete(nil)
	case ublk.OpWrite, ublk.OpZoneAppend:
		off := r.Offset
		if r.Op == ublk.OpZoneAppend {
			off = z.wp[i]
		}
		if off != z.wp[i] || off+r.Length > end(i) {
			r.Complete(syscall.EIO) // not at the write pointer, or past the zone
			return
		}
		copy(z.data[off:], r.Data)
		z.wp[i] += r.Length
		z.cond[i] = ublk.ZoneCondImpOpen
		if z.wp[i] == end(i) {
			z.cond[i] = ublk.ZoneCondFull
		}
		if r.Op == ublk.OpZoneAppend {
			r.CompleteZoneAppend(uint64(off>>9), nil)
		} else {
			r.Complete(nil)
		}
	case ublk.OpZoneReset:
		z.wp[i], z.cond[i] = int64(i)*z.zoneSize, ublk.ZoneCondEmpty
		r.Complete(nil)
	case ublk.OpZoneResetAll:
		for j := range z.wp {
			z.wp[j], z.cond[j] = int64(j)*z.zoneSize, ublk.ZoneCondEmpty
		}
		r.Complete(nil)
	case ublk.OpZoneOpen:
		z.cond[i] = ublk.ZoneCondExpOpen
		r.Complete(nil)
	case ublk.OpZoneClose:
		if z.wp[i] == int64(i)*z.zoneSize {
			z.cond[i] = ublk.ZoneCondEmpty
		} else {
			z.cond[i] = ublk.ZoneCondClosed
		}
		r.Complete(nil)
	case ublk.OpZoneFinish:
		z.wp[i], z.cond[i] = end(i), ublk.ZoneCondFull
		r.Complete(nil)
	case ublk.OpReportZones:
		var zones []ublk.BlkZone
		for j := i; j < len(z.wp) && len(zones) < int(r.NrZones); j++ {
			zones = append(zones, ublk.BlkZone{Start: int64(j) * z.zoneSize, Len: z.zoneSize,
				WritePointer: z.wp[j], Type: ublk.ZoneTypeSeqWriteReq, Cond: z.cond[j]})
		}
		r.ReportZones(zones)
	case ublk.OpFlush:
		r.Complete(nil)
	default:
		r.Complete(syscall.EOPNOTSUPP)
	}
}

type zoneInfo struct {
	start, len, wp uint64 // sectors
	cond           uint8
}

func reportZones(fd int, sector uint64, n int) ([]zoneInfo, error) {
	buf := make([]byte, 16+64*n)
	*(*uint64)(unsafe.Pointer(&buf[0])) = sector
	*(*uint32)(unsafe.Pointer(&buf[8])) = uint32(n)
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), blkReportZone, uintptr(unsafe.Pointer(&buf[0]))); e != 0 {
		return nil, e
	}
	got := int(*(*uint32)(unsafe.Pointer(&buf[8])))
	out := make([]zoneInfo, got)
	for i := range out {
		b := buf[16+64*i:]
		out[i] = zoneInfo{
			start: *(*uint64)(unsafe.Pointer(&b[0])), len: *(*uint64)(unsafe.Pointer(&b[8])),
			wp: *(*uint64)(unsafe.Pointer(&b[16])), cond: b[25],
		}
	}
	return out, nil
}

func zoneOp(fd int, req uintptr, sector, nr uint64) error {
	r := [2]uint64{sector, nr}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&r[0]))); e != 0 {
		return e
	}
	return nil
}

func testZoned(t *T) error {
	if err := needFeatures(ublk.FeatureZoned | ublk.FeatureUserCopy); err != nil {
		return err
	}
	const size, zoneSize = 64 << 20, 4 << 20
	zsec := uint64(zoneSize >> 9)
	z := newZonedMem(size, zoneSize)
	params := ublk.DefaultParams(nil)
	params.Backend, params.Handler, params.Size = nil, z, size
	params.EnableZoned = true
	params.Zoned = ublk.ZonedParams{ZoneSize: zoneSize, MaxOpenZones: 8, MaxActiveZones: 8}
	params.NumQueues, params.QueueDepth = 1, 32
	params.Inline = true
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	if v, err := sysfsQueue(dev.Path, "zoned"); err != nil || v != "host-managed" {
		return fmt.Errorf("queue/zoned = %q (%v), want host-managed", v, err)
	}
	if n, err := sysfsInt(dev.Path, "nr_zones"); err != nil || n != size/zoneSize {
		return fmt.Errorf("queue/nr_zones = %d (%v), want %d", n, err, size/zoneSize)
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())

	zones, err := reportZones(fd, 0, 64)
	if err != nil {
		return fmt.Errorf("BLKREPORTZONE: %w", err)
	}
	if len(zones) != size/zoneSize || zones[3].start != 3*zsec || zones[3].wp != 3*zsec || zones[3].cond != ublk.ZoneCondEmpty {
		return fmt.Errorf("initial report: %d zones, zone 3 = %+v", len(zones), zones[3])
	}

	// Sequential writes at zone 2's write pointer succeed and advance it.
	p := alignedBuf(64 << 10)
	newRNG(5).fill(p)
	for k := int64(0); k < 3; k++ {
		if err := pwriteFull(fd, p, 2*zoneSize+k*int64(len(p))); err != nil {
			return fmt.Errorf("sequential write %d: %w", k, err)
		}
	}
	zones, _ = reportZones(fd, 2*zsec, 1)
	if len(zones) != 1 || zones[0].wp != 2*zsec+3*uint64(len(p))>>9 {
		return fmt.Errorf("after 3 writes zone 2 = %+v", zones)
	}
	// A write that is not at the write pointer fails (in the block layer or
	// in the handler).
	if err := pwriteFull(fd, p, 5*zoneSize+4096); err == nil {
		return fmt.Errorf("a write not at the write pointer succeeded")
	}
	q := alignedBuf(len(p))
	if err := preadFull(fd, q, 2*zoneSize+int64(len(p))); err != nil || !bytes.Equal(p, q) {
		return fmt.Errorf("read back of zone 2: %v", err)
	}

	// Zone management.
	for _, c := range []struct {
		name string
		req  uintptr
		zone uint64
		cond uint8
	}{
		{"reset", blkResetZone, 2, ublk.ZoneCondEmpty},
		{"open", blkOpenZone, 4, ublk.ZoneCondExpOpen},
		{"close", blkCloseZone, 4, ublk.ZoneCondEmpty},
		{"finish", blkFinishZone, 6, ublk.ZoneCondFull},
	} {
		if err := zoneOp(fd, c.req, c.zone*zsec, zsec); err != nil {
			return fmt.Errorf("zone %s: %w", c.name, err)
		}
		zones, err := reportZones(fd, c.zone*zsec, 1)
		if err != nil || len(zones) != 1 || zones[0].cond != c.cond {
			return fmt.Errorf("after zone %s: %+v (%v), want cond %#x", c.name, zones, err, c.cond)
		}
	}
	return nil
}

func init() {
	register("features/integrity", 3*time.Minute, testIntegrity)
}

// integMem is a RAM backend that also stores integrity metadata.
type integMem struct {
	*memBackend
	mu       sync.Mutex
	meta     map[int64][]byte // per interval offset
	interval int64
	metaSize int
	writes   atomic.Int64
}

func (m *integMem) WriteIntegrity(meta []byte, off int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes.Add(1)
	for i := 0; i*m.metaSize < len(meta); i++ {
		m.meta[off+int64(i)*m.interval] = append([]byte(nil), meta[i*m.metaSize:(i+1)*m.metaSize]...)
	}
	return nil
}

func (m *integMem) ReadIntegrity(meta []byte, off int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := 0; i*m.metaSize < len(meta); i++ {
		if v, ok := m.meta[off+int64(i)*m.interval]; ok {
			copy(meta[i*m.metaSize:], v)
		} else {
			// Never written: the T10 escape (application tag 0xffff) tells the
			// block layer not to check this block.
			for j := i * m.metaSize; j < (i+1)*m.metaSize; j++ {
				meta[j] = 0xff
			}
		}
	}
	return nil
}

// testIntegrity: with T10-DIF CRC16 protection and reference tags, the block
// layer generates protection information on write and verifies it on read.
// The backend must receive non-zero metadata, reads must verify, and
// corrupting one stored tuple must fail exactly that block's read.
func testIntegrity(t *T) error {
	if err := needFeatures(ublk.FeatureIntegrity | ublk.FeatureUserCopy); err != nil {
		return err
	}
	b := &integMem{memBackend: newMemBackend(16 << 20), meta: map[int64][]byte{}, interval: 512, metaSize: 8}
	params := ublk.DefaultParams(b)
	params.NumQueues, params.QueueDepth = 1, 32
	params.Integrity = &ublk.IntegrityParams{MetadataSize: 8, IntervalSize: 512,
		Checksum: ublk.IntegrityCsumCRC16, RefTag: true}
	dev, err := ublk.CreateAndServe(context.Background(), params, nil)
	if err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EOPNOTSUPP) {
			return skipf("kernel refused integrity parameters (CONFIG_BLK_DEV_INTEGRITY?): %v", err)
		}
		return err
	}
	t.Cleanup(func() { _ = dev.Close() })
	if err := waitForNode(dev.Path, 5*time.Second); err != nil {
		return err
	}
	name := dev.Path[len("/dev/"):]
	if fmtb, err := os.ReadFile("/sys/block/" + name + "/integrity/format"); err == nil {
		t.Logf("integrity format %s", bytes.TrimSpace(fmtb))
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())
	p := alignedBuf(64 << 10)
	newRNG(21).fill(p)
	if err := pwriteFull(fd, p, 1<<20); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if b.writes.Load() == 0 {
		return fmt.Errorf("the backend never received integrity metadata for a write")
	}
	b.mu.Lock()
	tuple := b.meta[1<<20]
	b.mu.Unlock()
	if len(tuple) != 8 || bytes.Equal(tuple, make([]byte, 8)) {
		return fmt.Errorf("stored metadata for the first block is %x; the block layer should have generated a PI tuple", tuple)
	}
	q := alignedBuf(len(p))
	if err := preadFull(fd, q, 1<<20); err != nil {
		return fmt.Errorf("read with verification: %w", err)
	}
	if !bytes.Equal(p, q) {
		return fmt.Errorf("read back differs")
	}
	// Corrupt the guard tag of the block at 1 MiB + 8 KiB.
	b.mu.Lock()
	b.meta[1<<20+8192][0] ^= 0xff
	b.mu.Unlock()
	one := alignedBuf(4096)
	if err := preadFull(fd, one, 1<<20+8192); err == nil {
		return fmt.Errorf("a block with a corrupted PI tuple read back without error")
	} else {
		t.Logf("corrupted block read failed as it should: %v", err)
	}
	if err := preadFull(fd, one, 1<<20+16384); err != nil {
		return fmt.Errorf("an intact block failed to read after another was corrupted: %w", err)
	}
	return nil
}

func init() {
	register("features/shared-memory", 2*time.Minute, testSharedMemory)
}

// shmemProbe records whether requests arrived through shared memory.
type shmemProbe struct {
	*memBackend
	region []byte
	hits   atomic.Int64
}

func (s *shmemProbe) HandleRequest(r *ublk.Request) {
	if r.Flags&ublk.FlagSharedMemory != 0 && len(r.Data) > 0 {
		lo := uintptr(unsafe.Pointer(&s.region[0]))
		p := uintptr(unsafe.Pointer(&r.Data[0]))
		if p >= lo && p < lo+uintptr(len(s.region)) {
			s.hits.Add(1)
		}
	}
	var err error
	switch r.Op {
	case ublk.OpRead:
		_, err = s.ReadAt(r.Data, r.Offset)
	case ublk.OpWrite:
		_, err = s.WriteAt(r.Data, r.Offset)
	case ublk.OpFlush:
	default:
		err = syscall.EOPNOTSUPP
	}
	r.Complete(err)
}

// testSharedMemory: O_DIRECT I/O from a buffer inside a registered memfd
// mapping reaches the handler with FlagSharedMemory and Data aliasing that
// mapping, and the bytes are right in both directions.
func testSharedMemory(t *T) error {
	if err := needFeatures(ublk.FeatureSharedMemoryZC); err != nil {
		return err
	}
	const regionSize = 4 << 20
	fd, err := unix.MemfdCreate("ublk-suite-shm", unix.MFD_CLOEXEC)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.Ftruncate(fd, regionSize); err != nil {
		return err
	}
	region, err := unix.Mmap(fd, 0, regionSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return err
	}
	defer unix.Munmap(region)

	probe := &shmemProbe{memBackend: newMemBackend(16 << 20), region: region}
	params := ublk.DefaultParams(nil)
	params.Backend, params.Handler, params.Size = nil, probe, 16<<20
	params.NumQueues, params.QueueDepth = 1, 16
	params.SharedMemoryZeroCopy = true
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	if _, err := dev.RegisterSharedMemory(region, false); err != nil {
		return err
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	src := region[64<<10 : 128<<10]
	newRNG(31).fill(src)
	if err := pwriteFull(int(f.Fd()), src, 1<<20); err != nil {
		return fmt.Errorf("write from the shared region: %w", err)
	}
	got := make([]byte, len(src))
	if _, err := probe.ReadAt(got, 1<<20); err != nil || !bytes.Equal(got, src) {
		return fmt.Errorf("backend did not receive the shared-memory write")
	}
	dst := region[1<<20 : 1<<20+len(src)]
	clear(dst)
	if err := preadFull(int(f.Fd()), dst, 1<<20); err != nil {
		return fmt.Errorf("read into the shared region: %w", err)
	}
	if !bytes.Equal(dst, src) {
		return fmt.Errorf("read into the shared region returned different bytes")
	}
	if probe.hits.Load() == 0 {
		return fmt.Errorf("no request arrived through shared memory (FlagSharedMemory with Data in the region)")
	}
	t.Logf("%d requests served zero-copy from shared memory", probe.hits.Load())
	return nil
}
