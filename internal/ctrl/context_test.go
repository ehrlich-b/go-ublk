package ctrl

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// blockingRing parks STOP_DEV/DEL_DEV until released, like a kernel stuck
// flushing I/O, and answers the GET_DEV_INFO privilege probe immediately.
type blockingRing struct {
	release chan struct{}
	entered chan *uapi.UblksrvCtrlCmd
	res     int32
}

func newBlockingRing() *blockingRing {
	return &blockingRing{release: make(chan struct{}), entered: make(chan *uapi.UblksrvCtrlCmd, 16)}
}

func (b *blockingRing) ring() *controlTestRing {
	return &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		if op == uapi.UBLK_U_CMD_GET_DEV_INFO {
			return controlTestResult(0), nil
		}
		b.entered <- cmd
		<-b.release
		if buf := controlTestBuffer(cmd); len(buf) >= 64 {
			// A late kernel write into the command's buffer.
			binary.LittleEndian.PutUint32(buf[12:16], 99)
		}
		return controlTestResult(b.res), nil
	}}
}

func TestContextAbandonsButKeepsCommandAlive(t *testing.T) {
	b := newBlockingRing()
	c, ts := newTestControllerSlots(t, b.ring())
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	err := c.StopDev(ctx, 5)
	var ife *InFlightError
	if !errors.As(err, &ife) || !errors.Is(err, context.DeadlineExceeded) || ife.Op != "STOP_DEV" || ife.DevID != 5 {
		t.Fatalf("StopDev = %v", err)
	}
	select {
	case <-ife.Done():
		t.Fatal("Done closed while the kernel still runs the command")
	default:
	}
	// The abandoned slot is still owned: nothing unmapped, the ring not closed,
	// and a new command gets a fresh slot instead of reusing it.
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if created, unmaps := ts.counts(); created != 1 || unmaps != 0 {
		t.Fatalf("slot released under the kernel: created=%d unmaps=%d", created, unmaps)
	}
	close(b.release)
	select {
	case <-ife.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("late completion never published")
	}
	if ife.Result() != nil {
		t.Fatalf("late result %v", ife.Result())
	}
	// Completed after Close: now the slot is released.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, unmaps := ts.counts(); unmaps == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slot never released after the late completion")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAbandonedCommandDoesNotBlockOthers(t *testing.T) {
	b := newBlockingRing()
	ring := b.ring()
	inner := ring.submit
	ring.submit = func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		if op == uapi.UBLK_U_CMD_GET_FEATURES {
			binary.LittleEndian.PutUint64(controlTestBuffer(cmd), 1)
			return controlTestResult(0), nil
		}
		return inner(op, cmd)
	}
	c := newTestController(ring)
	defer close(b.release)
	for i := 0; i < defaultMaxInFlight+2; i++ {
		ctx, cancel := context.WithTimeout(bg, time.Millisecond)
		if err := c.DelDev(ctx, uint32(i)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("DelDev %d = %v", i, err)
		}
		cancel()
	}
	// More commands are stuck in the "kernel" than the in-flight cap, yet the
	// controller still serves new ones.
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	if f, err := c.GetFeatures(ctx); err != nil || f != 1 {
		t.Fatalf("GetFeatures behind abandoned commands: %v %v", f, err)
	}
}

func TestLateAddDevDeletesOrphan(t *testing.T) {
	k := newFakeKernel()
	release := make(chan struct{})
	ring := k.ring()
	inner := ring.submit
	ring.submit = func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		if op == uapi.UBLK_U_CMD_ADD_DEV {
			<-release
		}
		return inner(op, cmd)
	}
	c := newTestController(ring)
	ctx, cancel := context.WithTimeout(bg, 10*time.Millisecond)
	defer cancel()
	_, err := c.AddDev(ctx, AddDevOptions{DevID: AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20})
	var ife *InFlightError
	if !errors.As(err, &ife) {
		t.Fatalf("AddDev = %v", err)
	}
	close(release)
	<-ife.Done()
	deadline := time.Now().Add(5 * time.Second)
	for {
		k.mu.Lock()
		n := len(k.devs)
		k.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("orphaned device from an abandoned ADD_DEV was not deleted")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestContextAlreadyDone(t *testing.T) {
	var sent atomic.Int32
	ring := &controlTestRing{submit: func(uint32, *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		sent.Add(1)
		return controlTestResult(0), nil
	}}
	c := newTestController(ring)
	// Fill the in-flight cap so acquire has to wait, then cancel.
	for i := 0; i < defaultMaxInFlight; i++ {
		c.sem <- struct{}{}
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if err := c.StopDev(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("StopDev with a cancelled context = %v", err)
	}
	if sent.Load() != 0 {
		t.Fatal("command sent after its context was cancelled")
	}
}

// Concurrent commands each get a private slot; buffers never cross. Run with
// -race.
func TestConcurrentCommands(t *testing.T) {
	var inFlight, peak atomic.Int32
	ring := &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		defer inFlight.Add(-1)
		time.Sleep(200 * time.Microsecond)
		if op == uapi.UBLK_U_CMD_GET_DEV_INFO {
			info := uapi.UblksrvCtrlDevInfo{DevID: cmd.DevID, QueueDepth: uint16(cmd.DevID)}
			copy(controlTestBuffer(cmd), uapi.Marshal(&info))
		}
		return controlTestResult(0), nil
	}}
	c, ts := newTestControllerSlots(t, ring)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				id := uint32(g*100 + i)
				ctx := bg
				if i%2 == 1 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(bg, time.Minute)
					defer cancel()
				}
				info, err := c.GetDevInfo(ctx, id)
				if err != nil || info.DevID != id || info.QueueDepth != uint16(id) {
					errs <- errors.New("cross-talk or error")
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if peak.Load() > defaultMaxInFlight {
		t.Fatalf("%d commands in flight, cap %d", peak.Load(), defaultMaxInFlight)
	}
	if created, _ := ts.counts(); created > defaultMaxInFlight {
		t.Fatalf("%d slots created for at most %d concurrent commands", created, defaultMaxInFlight)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if created, unmaps := ts.counts(); unmaps != created {
		t.Fatalf("Close released %d of %d slots", unmaps, created)
	}
}

func TestUnknownCommandIsUnsupported(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EOPNOTSUPP, ENOTSUPP} {
		k := newFakeKernel()
		k.unknownOpErrno = errno
		c := newTestController(k.ring())
		id := addTestDev(t, c, 0).DevID
		k.mu.Lock()
		k.devs[id].started = true
		k.mu.Unlock()
		// Emulate a kernel without TRY_STOP_DEV by making the fake reject it.
		ring := k.ring()
		inner := ring.submit
		ring.submit = func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
			if op == uapi.UBLK_U_CMD_TRY_STOP_DEV {
				return controlTestResult(neg(errno)), nil
			}
			return inner(op, cmd)
		}
		if err := newTestController(ring).TryStopDev(bg, id); !IsUnsupported(err) {
			t.Fatalf("%v not recognised as unsupported", err)
		}
	}
}
