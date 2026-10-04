//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/ehrlich-b/go-ublk"
)

func init() {
	register("lifecycle/create-close", time.Minute, testCreateClose)
	register("lifecycle/fixed-id", time.Minute, testFixedID)
	register("lifecycle/create-start-stop-close", time.Minute, testCreateStartStopClose)
	register("lifecycle/ctx-cancel-idle", time.Minute, testCtxCancelIdle)
	register("lifecycle/churn-leaks", 5*time.Minute, testChurnLeaks)
	register("lifecycle/concurrent-create", 5*time.Minute, testConcurrentCreate)
	register("lifecycle/close-under-load", 2*time.Minute, testCloseUnderLoad)
	register("lifecycle/server-killed", 2*time.Minute, testServerKilled)
	register("lifecycle/restart-after-stop", time.Minute, testRestartAfterStop)
}

func listed(id uint32) (bool, error) {
	ids, err := ublk.ListDevices()
	if err != nil {
		return false, err
	}
	return slices.Contains(ids, id), nil
}

func testCreateClose(t *T) error {
	mark := kmsgMark()
	params, _ := memParams(64 << 20)
	dev, err := ublk.CreateAndServe(context.Background(), params, nil)
	if err != nil {
		return err
	}
	if err := waitForNode(dev.Path, 5*time.Second); err != nil {
		_ = dev.Close()
		return err
	}
	if ok, err := listed(dev.ID); err != nil || !ok {
		_ = dev.Close()
		return fmt.Errorf("ListDevices does not include live device %d (%v)", dev.ID, err)
	}
	if err := dev.Close(); err != nil {
		return fmt.Errorf("Close: %w", err)
	}
	if err := waitForGone(dev.Path, 5*time.Second); err != nil {
		return err
	}
	if ok, err := listed(dev.ID); err != nil || ok {
		return fmt.Errorf("ListDevices still includes closed device %d (%v)", dev.ID, err)
	}
	if err := dev.Close(); err != nil {
		return fmt.Errorf("second Close is not idempotent: %w", err)
	}
	if p := kernelProblems(mark); p != "" {
		return fmt.Errorf("kernel log: %s", p)
	}
	return nil
}

func testFixedID(t *T) error {
	ids, err := ublk.ListDevices()
	if err != nil {
		return err
	}
	id := int32(37)
	for slices.Contains(ids, uint32(id)) {
		id++
	}
	params, _ := memParams(16 << 20)
	params.DeviceID = id
	dev, err := newDevice(t, params)
	if err != nil {
		return err
	}
	if want := fmt.Sprintf("/dev/ublkb%d", id); dev.Path != want || dev.ID != uint32(id) {
		return fmt.Errorf("requested ID %d, got %d at %s", id, dev.ID, dev.Path)
	}
	dup, _ := memParams(16 << 20)
	dup.DeviceID = id
	if d2, err := ublk.CreateAndServe(context.Background(), dup, nil); err == nil {
		_ = d2.Close()
		return fmt.Errorf("a second device with ID %d was created", id)
	} else if !errors.Is(err, syscall.EEXIST) {
		t.Logf("duplicate ID rejected with %v (not EEXIST)", err)
	}
	return nil
}

func testCreateStartStopClose(t *T) error {
	params, _ := memParams(16 << 20)
	dev, err := ublk.Create(params, nil)
	if err != nil {
		return err
	}
	t.Cleanup(func() { _ = dev.Close() })
	if dev.State() != ublk.DeviceStateCreated {
		return fmt.Errorf("state after Create = %s", dev.State())
	}
	if err := dev.Start(context.Background()); err != nil {
		return fmt.Errorf("Start: %w", err)
	}
	if err := waitForNode(dev.Path, 5*time.Second); err != nil {
		return err
	}
	f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	if err := pwriteFull(int(f.Fd()), alignedBuf(4096), 0); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if err := dev.Stop(); err != nil {
		return fmt.Errorf("Stop: %w", err)
	}
	if err := dev.Close(); err != nil {
		return fmt.Errorf("Close after Stop: %w", err)
	}
	return waitForGone(dev.Path, 5*time.Second)
}

// testRestartAfterStop checks that Start refuses a stopped device. Restarting
// a stopped ublk device is unreliable in the kernel (it oopsed Arch's 7.2.8 in
// the partition scan and wedged 6.10-6.12), so the library must never try.
func testRestartAfterStop(t *T) error {
	params, _ := memParams(16 << 20)
	dev, err := ublk.Create(params, nil)
	if err != nil {
		return err
	}
	t.Cleanup(func() { _ = dev.Close() })
	if err := dev.Start(context.Background()); err != nil {
		return fmt.Errorf("first Start: %w", err)
	}
	if err := dev.Stop(); err != nil {
		return fmt.Errorf("Stop: %w", err)
	}
	if err := dev.Start(context.Background()); !errors.Is(err, ublk.ErrStopped) {
		return fmt.Errorf("Start after Stop = %v, want ErrStopped", err)
	}
	return nil
}

func testCtxCancelIdle(t *T) error {
	params, _ := memParams(16 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	dev, err := ublk.CreateAndServe(ctx, params, nil)
	if err != nil {
		cancel()
		return err
	}
	cancel()
	time.Sleep(300 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- dev.Close() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("Close after context cancel: %w", err)
		}
	case <-time.After(20 * time.Second):
		return fmt.Errorf("Close did not return within 20s after the context was cancelled")
	}
	return waitForGone(dev.Path, 5*time.Second)
}

// testChurnLeaks creates and closes devices repeatedly and checks the process
// does not accumulate file descriptors or memory mappings (Critical Bug #17:
// rings that were closed but never unmapped).
func testChurnLeaks(t *T) error {
	cycle := func() error {
		params, _ := memParams(8 << 20)
		params.NumQueues, params.QueueDepth = 2, 16
		dev, err := ublk.CreateAndServe(context.Background(), params, nil)
		if err != nil {
			return err
		}
		if err := waitForNode(dev.Path, 5*time.Second); err != nil {
			_ = dev.Close()
			return err
		}
		f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
		if err == nil {
			p := alignedBuf(64 << 10)
			newRNG(5).fill(p)
			err = pwriteFull(int(f.Fd()), p, 0)
			f.Close()
		}
		if cerr := dev.Close(); err == nil {
			err = cerr
		}
		return err
	}
	if err := cycle(); err != nil { // warm up lazily initialized runtime state
		return err
	}
	fds, maps := openFDs(), mapCount()
	n := t.Count(20)
	for i := 0; i < n; i++ {
		if err := cycle(); err != nil {
			return fmt.Errorf("cycle %d: %w", i, err)
		}
	}
	dFDs, dMaps := openFDs()-fds, mapCount()-maps
	t.Logf("%d cycles: fds %+d, maps %+d", n, dFDs, dMaps)
	if dFDs > 2 {
		return fmt.Errorf("leaked %d file descriptors over %d create/close cycles", dFDs, n)
	}
	if dMaps > 8 {
		return fmt.Errorf("leaked %d memory mappings over %d create/close cycles (~%.1f per cycle)",
			dMaps, n, float64(dMaps)/float64(n))
	}
	return nil
}

func testConcurrentCreate(t *T) error {
	mark := kmsgMark()
	const workers = 4
	cycles := t.Count(5)
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < cycles; i++ {
				params, _ := memParams(8 << 20)
				params.NumQueues, params.QueueDepth = 2, 16
				dev, err := ublk.CreateAndServe(context.Background(), params, nil)
				if err != nil {
					errs <- fmt.Errorf("worker %d cycle %d: create: %w", w, i, err)
					return
				}
				err = waitForNode(dev.Path, 5*time.Second)
				if err == nil {
					var f *os.File
					if f, err = os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0); err == nil {
						p, q := alignedBuf(16<<10), alignedBuf(16<<10)
						newRNG(uint64(w*100 + i + 1)).fill(p)
						if err = pwriteFull(int(f.Fd()), p, 0); err == nil {
							if err = preadFull(int(f.Fd()), q, 0); err == nil && firstDiff(p, q) >= 0 {
								err = fmt.Errorf("read back differs")
							}
						}
						f.Close()
					}
				}
				if cerr := dev.Close(); err == nil {
					err = cerr
				}
				if err != nil {
					errs <- fmt.Errorf("worker %d cycle %d (dev %d): %w", w, i, dev.ID, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		return err
	}
	if p := kernelProblems(mark); p != "" {
		return fmt.Errorf("kernel log: %s", p)
	}
	return nil
}

// testCloseUnderLoad closes a device while writers are mid-stream. Close must
// return promptly (STOP_DEV drains in-flight I/O through the still-running
// queues), the writers must see errors rather than hang, and the device must
// be gone afterwards (Critical Bug #8).
func testCloseUnderLoad(t *T) error { return closeUnderLoad(t, nil) }

func closeUnderLoad(t *T, mutate func(*ublk.DeviceParams)) error {
	mark := kmsgMark()
	params, _ := memParams(64 << 20)
	params.NumQueues, params.QueueDepth = 4, 64
	if mutate != nil {
		mutate(&params)
	}
	dev, err := ublk.CreateAndServe(context.Background(), params, nil)
	if err != nil {
		return err
	}
	if err := waitForNode(dev.Path, 5*time.Second); err != nil {
		_ = dev.Close()
		return err
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		f, err := os.OpenFile(dev.Path, os.O_RDWR|syscall.O_DIRECT, 0)
		if err != nil {
			_ = dev.Close()
			return err
		}
		wg.Add(1)
		go func(w int, f *os.File) {
			defer wg.Done()
			defer f.Close()
			r := newRNG(uint64(w + 1))
			p := alignedBuf(64 << 10)
			for {
				off := int64(r.intn(1000)) * (64 << 10)
				if err := pwriteFull(int(f.Fd()), p, off); err != nil {
					return
				}
			}
		}(w, f)
	}
	time.Sleep(t.Duration(500 * time.Millisecond))
	closed := make(chan error, 1)
	start := time.Now()
	go func() { closed <- dev.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			return fmt.Errorf("Close under load: %w", err)
		}
	case <-time.After(60 * time.Second):
		return fmt.Errorf("Close under load did not return within 60s")
	}
	t.Logf("Close under load took %s", time.Since(start).Round(time.Millisecond))
	writersDone := make(chan struct{})
	go func() { wg.Wait(); close(writersDone) }()
	select {
	case <-writersDone:
	case <-time.After(30 * time.Second):
		return fmt.Errorf("writers still blocked 30s after Close returned")
	}
	if err := waitForGone(dev.Path, 5*time.Second); err != nil {
		return err
	}
	if p := kernelProblems(mark); p != "" {
		return fmt.Errorf("kernel log: %s", p)
	}
	return nil
}

// testServerKilled kills a server process with SIGKILL. The device must
// outlive it as a registered, server-less device that fails I/O rather than
// hanging it, and DeleteDevice must reap it.
func testServerKilled(t *T) error {
	mark := kmsgMark()
	cmd, id, err := startServer(t, 32<<20)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/dev/ublkb%d", id)
	if err := waitForNode(path, 5*time.Second); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_DIRECT, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := pwriteFull(int(f.Fd()), alignedBuf(1<<20), 0); err != nil {
		return fmt.Errorf("write before kill: %w", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	_ = cmd.Wait()

	if ok, err := listed(id); err != nil || !ok {
		return fmt.Errorf("device %d vanished when its server died (listed=%v, err=%v); it should stay registered", id, ok, err)
	}
	ioDone := make(chan error, 1)
	go func() { ioDone <- preadFull(int(f.Fd()), alignedBuf(4096), 0) }()
	select {
	case err := <-ioDone:
		if err == nil {
			t.Logf("read after server death succeeded (served before abort?)")
		}
	case <-time.After(30 * time.Second):
		return fmt.Errorf("I/O to a device whose server died hung for 30s")
	}
	f.Close()

	delDone := make(chan error, 1)
	go func() { delDone <- ublk.DeleteDevice(id) }()
	select {
	case err := <-delDone:
		if err != nil {
			return fmt.Errorf("DeleteDevice(%d) after server death: %w", id, err)
		}
	case <-time.After(60 * time.Second):
		return fmt.Errorf("DeleteDevice(%d) hung for 60s", id)
	}
	if err := waitForGone(path, 5*time.Second); err != nil {
		return err
	}
	if p := kernelProblems(mark); p != "" {
		return fmt.Errorf("kernel log: %s", p)
	}
	return nil
}

func init() {
	register("lifecycle/chaos", 10*time.Minute, testChaos)
}

// testChaos interleaves random lifecycle actions across several devices at
// once — create in-process, create in a server subprocess, I/O bursts, Close,
// SIGKILL a server, reap orphans — and requires that nothing hangs, every
// orphan can be reaped, and the kernel logs no bug. Seeded and logged, so a
// failure replays with the same action sequence.
func testChaos(t *T) error {
	mark := kmsgMark()
	seed := uint64(time.Now().UnixNano())
	t.Logf("seed %d", seed)
	r := newRNG(seed)

	type slot struct {
		dev  *ublk.Device // in-process server, or nil
		cmd  interface{ Kill() error }
		id   uint32
		path string
	}
	var live []*slot
	var orphans []uint32
	defer func() {
		for _, s := range live {
			if s.dev != nil {
				_ = s.dev.Close()
			} else if s.cmd != nil {
				_ = s.cmd.Kill()
				orphans = append(orphans, s.id)
			}
		}
		for _, id := range orphans {
			_ = ublk.DeleteDevice(id)
		}
	}()

	step := func(what string, f func() error) error {
		done := make(chan error, 1)
		go func() { done <- f() }()
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("%s: %w", what, err)
			}
			return nil
		case <-time.After(60 * time.Second):
			return fmt.Errorf("%s: hung for 60s", what)
		}
	}

	deadline := time.Now().Add(t.Duration(20 * time.Second))
	actions := 0
	for ; time.Now().Before(deadline); actions++ {
		switch a := r.intn(6); {
		case a == 0 && len(live) < 6: // in-process create
			params, _ := memParams(8 << 20)
			params.NumQueues, params.QueueDepth = 1+r.intn(3), 8<<r.intn(4)
			s := &slot{}
			if err := step("create", func() error {
				dev, err := ublk.CreateAndServe(context.Background(), params, nil)
				if err != nil {
					return err
				}
				s.dev, s.id, s.path = dev, dev.ID, dev.Path
				return waitForNode(dev.Path, 5*time.Second)
			}); err != nil {
				return err
			}
			live = append(live, s)
		case a == 1 && len(live) < 6: // subprocess create
			cmd, id, err := startServer(t, 8<<20)
			if err != nil {
				return fmt.Errorf("server create: %w", err)
			}
			s := &slot{cmd: cmd.Process, id: id, path: fmt.Sprintf("/dev/ublkb%d", id)}
			if err := waitForNode(s.path, 5*time.Second); err != nil {
				return err
			}
			live = append(live, s)
		case a == 2 && len(live) > 0: // I/O burst
			s := live[r.intn(len(live))]
			if err := step("io on "+s.path, func() error {
				f, err := os.OpenFile(s.path, os.O_RDWR|syscall.O_DIRECT, 0)
				if err != nil {
					return err
				}
				defer f.Close()
				p, q := alignedBuf(64<<10), alignedBuf(64<<10)
				r2 := newRNG(r.next())
				for i := 0; i < 16; i++ {
					off := int64(r2.intn(64)) * (64 << 10)
					r2.fill(p)
					if err := pwriteFull(int(f.Fd()), p, off); err != nil {
						return err
					}
					if err := preadFull(int(f.Fd()), q, off); err != nil {
						return err
					}
					if firstDiff(p, q) >= 0 {
						return fmt.Errorf("read back differs at %d", off)
					}
				}
				return nil
			}); err != nil {
				return err
			}
		case a == 3 && len(live) > 0: // graceful close / kill
			i := r.intn(len(live))
			s := live[i]
			live = append(live[:i], live[i+1:]...)
			if s.dev != nil {
				if err := step("close "+s.path, s.dev.Close); err != nil {
					return err
				}
			} else {
				_ = s.cmd.Kill()
				orphans = append(orphans, s.id)
			}
		case a == 4 && len(orphans) > 0: // reap an orphan
			id := orphans[0]
			orphans = orphans[1:]
			if err := step(fmt.Sprintf("reap %d", id), func() error { return ublk.DeleteDevice(id) }); err != nil {
				return err
			}
		case a == 5: // list must not fail
			if err := step("list", func() error { _, err := ublk.ListDevices(); return err }); err != nil {
				return err
			}
		}
	}
	t.Logf("%d actions", actions)
	if p := kernelProblems(mark); p != "" {
		return fmt.Errorf("kernel log (seed %d): %s", seed, p)
	}
	return nil
}
