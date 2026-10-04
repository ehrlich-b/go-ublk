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

	if err := cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("old server exited with %v after Detach", err)
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
