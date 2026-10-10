package ublk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type pendingStopReply struct {
	done   chan struct{}
	result error
	reaped bool
}

func (p *pendingStopReply) Error() string         { return "STOP_DEV timeout with command still pending" }
func (p *pendingStopReply) Unwrap() error         { return context.DeadlineExceeded }
func (p *pendingStopReply) Done() <-chan struct{} { return p.done }
func (p *pendingStopReply) Result() error         { return p.result }
func (p *pendingStopReply) Reaped() bool          { return p.reaped }

// stopResourceLedger is an independent fake control peer. The char fd is a
// real duplicated fd that Device must retain; only control-ring storage and
// the kernel device are modeled. Actual queue mmap/drain belongs to VM tests.
type stopResourceLedger struct {
	t                             *testing.T
	fd                            int
	devices, rings, retainedBytes int
	creates, stops, deletes       int
	mode                          string
	pending                       *pendingStopReply
	closedPending                 bool
}

type stopPeer struct {
	l       *stopResourceLedger
	waiting bool
}

func (l *stopResourceLedger) controller() (lifecycleController, error) {
	l.creates++
	l.rings++
	l.retainedBytes += 8192
	return &stopPeer{l: l}, nil
}

func (p *stopPeer) StopDev(ctx context.Context, id uint32) error {
	p.l.stops++
	switch p.l.mode {
	case "pending":
		<-ctx.Done()
		p.waiting = true
		p.l.pending = &pendingStopReply{done: make(chan struct{})}
		return fmt.Errorf("device %d: %w", id, p.l.pending)
	case "cancelled":
		<-ctx.Done()
		return errors.Join(syscall.EINTR, ctx.Err()) // final cancellation CQE already reaped
	default:
		return nil
	}
}
func (p *stopPeer) TryStopDev(ctx context.Context, id uint32) error { return p.StopDev(ctx, id) }
func (p *stopPeer) DelDev(context.Context, uint32) error {
	if _, err := unix.FcntlInt(uintptr(p.l.fd), unix.F_GETFD, 0); !errors.Is(err, syscall.EBADF) {
		p.l.t.Errorf("DEL_DEV before char handle release: %v", err)
	}
	p.l.deletes++
	p.l.devices--
	return nil
}
func (p *stopPeer) Close() error {
	if p.waiting {
		p.l.closedPending = true
		return nil
	}
	p.l.rings--
	p.l.retainedBytes -= 8192
	return nil
}
func (l *stopResourceLedger) finish(result error, reaped bool) {
	l.pending.result, l.pending.reaped = result, reaped
	if l.closedPending && reaped {
		l.rings--
		l.retainedBytes -= 8192
	}
	close(l.pending.done)
}

func pendingStopDevice(t *testing.T, mode string) (*Device, *stopResourceLedger) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "char-handle")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	d := &Device{ID: 7, state: DeviceStateRunning, charFd: fd, done: make(chan struct{}),
		unwatch: make(chan struct{}), options: &Options{StopTimeout: time.Millisecond}}
	l := &stopResourceLedger{t: t, fd: fd, devices: 1, mode: mode}
	original := createLifecycleController
	createLifecycleController = l.controller
	t.Cleanup(func() {
		createLifecycleController = original
		if d.charFd >= 0 {
			_ = syscall.Close(d.charFd)
		}
	})
	return d, l
}

func (l *stopResourceLedger) checkRetained(t *testing.T, d *Device) {
	t.Helper()
	if d.charFd != l.fd || l.devices != 1 || l.stops != 1 || l.deletes != 0 || l.rings != 1 || l.retainedBytes != 8192 {
		t.Fatalf("pending resource ownership: fd=%d devices=%d stops=%d deletes=%d rings=%d bytes=%d", d.charFd, l.devices, l.stops, l.deletes, l.rings, l.retainedBytes)
	}
	if _, err := unix.FcntlInt(uintptr(l.fd), unix.F_GETFD, 0); err != nil {
		t.Fatalf("pending STOP freed char fd: %v", err)
	}
	select {
	case <-d.unwatch:
		t.Fatal("pending STOP stopped queue supervision")
	default:
	}
	select {
	case <-d.Done():
		t.Fatal("pending STOP was reported as terminal")
	default:
	}
}

func TestDevicePendingStopLateSuccessClose(t *testing.T) {
	d, l := pendingStopDevice(t, "pending")
	if err := d.Stop(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("STOP timeout: %v", err)
	}
	l.checkRetained(t, d)
	for _, call := range []struct {
		name string
		fn   func() error
	}{
		{"Close", d.Close}, {"Stop retry", d.Stop}, {"Resize", func() error { return d.Resize(4096) }},
		{"Detach", d.Detach}, {"register", func() error { _, err := d.RegisterSharedMemory([]byte{1}, false); return err }},
		{"unregister", func() error { return d.UnregisterSharedMemory(0) }},
	} {
		if err := call.fn(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s did not preserve pending receipt: %v", call.name, err)
		}
		l.checkRetained(t, d)
	}
	if l.creates != 1 {
		t.Fatalf("opened %d controllers for one pending STOP", l.creates)
	}
	l.finish(nil, true)
	if err := d.Close(); err != nil {
		t.Fatalf("late-success -> Close: %v", err)
	}
	if d.State() != DeviceStateClosed || d.charFd != -1 || l.devices != 0 || l.rings != 0 || l.retainedBytes != 0 || l.stops != 1 || l.deletes != 1 {
		t.Fatalf("late STOP was not reconciled exactly once: state=%s ledger=%+v fd=%d", d.State(), l, d.charFd)
	}
	if err := d.Close(); err != nil || l.deletes != 1 {
		t.Fatal("Close retry duplicated DEL_DEV")
	}
}

func TestDeviceReapedStopCancellationAllowsRetry(t *testing.T) {
	d, l := pendingStopDevice(t, "cancelled")
	if err := d.Stop(); !errors.Is(err, syscall.EINTR) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled STOP: %v", err)
	}
	if d.pendingStop != nil || d.leaving.Load() || d.charFd != l.fd || l.rings != 0 || l.retainedBytes != 0 {
		t.Fatal("reaped cancellation treated as pending work or freed the device")
	}
	l.mode = "success"
	if err := d.Close(); err != nil || l.stops != 2 || l.deletes != 1 {
		t.Fatalf("retry after cancellation: %v ledger=%+v", err, l)
	}
}

func TestDeviceUnreapedStopTransportReturnStaysPending(t *testing.T) {
	d, l := pendingStopDevice(t, "pending")
	_ = d.Stop()
	l.finish(errors.New("transport wait failed before the final CQE"), false)
	if err := d.Close(); err == nil {
		t.Fatal("unreaped transport return permitted deletion")
	}
	l.checkRetained(t, d)
	if l.creates != 1 {
		t.Fatal("unreaped STOP permitted a conflicting command")
	}
}
