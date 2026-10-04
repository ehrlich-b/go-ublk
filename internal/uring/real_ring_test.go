package uring

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"golang.org/x/sys/unix"
)

const realRingCtrlTimeout = 15 * time.Second

func closedPipe(t *testing.T) *os.File {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = pw.Close()
		_ = pr.Close()
	})
	return pr
}

func newRealRing(t *testing.T, ctrlFd int32) Ring {
	t.Helper()
	ring, err := NewMinimalRing(4, ctrlFd)
	if err != nil {
		t.Fatalf("NewMinimalRing(4, %d): %v", ctrlFd, err)
	}
	t.Cleanup(func() {
		if err := ring.Close(); err != nil {
			t.Errorf("ring.Close after test: %v", err)
		}
	})
	return ring
}

func submitCtrlCmdBounded(t *testing.T, ring Ring, userData uint64) Result {
	t.Helper()
	done := make(chan struct{})
	var res Result
	var err error
	go func() {
		res, err = ring.SubmitCtrlCmd(
			uapi.UblkCtrlCmd(uapi.UBLK_CMD_GET_DEV_INFO),
			&uapi.UblksrvCtrlCmd{DevID: 0, QueueID: 0xFFFF},
			userData,
		)
		close(done)
	}()
	select {
	case <-done:
		if err != nil {
			t.Fatalf("SubmitCtrlCmd returned error: %v", err)
		}
		return res
	case <-time.After(realRingCtrlTimeout):
		t.Fatalf("SubmitCtrlCmd hung for %v with userData=%d", realRingCtrlTimeout, userData)
	}
	return nil
}

func TestRealRingNewCloseWithPipe(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer pw.Close()
	defer pr.Close()

	ring, err := NewMinimalRing(4, int32(pr.Fd()))
	if err != nil {
		t.Fatalf("NewMinimalRing(4, %d): %v", pr.Fd(), err)
	}
	defer func() {
		if err := ring.Close(); err != nil {
			t.Errorf("ring.Close returned error: %v", err)
		}
	}()
}

func TestRealRingSubmitCtrlCmdPipeUnsupported(t *testing.T) {
	ring := newRealRing(t, int32(closedPipe(t).Fd()))

	const userData = 12345
	res := submitCtrlCmdBounded(t, ring, userData)

	if got, want := res.Value(), int32(-95); got != want {
		t.Fatalf("completion Value() = %d, want %d (-EOPNOTSUPP)", got, want)
	}
	if got, want := res.UserData(), uint64(userData); got != want {
		t.Fatalf("completion UserData() = %d, want %d", got, userData)
	}
}

func TestRealRingSecondRoundTripSameRing(t *testing.T) {
	ring := newRealRing(t, int32(closedPipe(t).Fd()))

	const firstUserData = 2001
	res1 := submitCtrlCmdBounded(t, ring, firstUserData)
	if got, want := res1.Value(), int32(-95); got != want {
		t.Fatalf("first round trip Value() = %d, want %d (-EOPNOTSUPP)", got, want)
	}
	if got := res1.UserData(); got != uint64(firstUserData) {
		t.Fatalf("first round trip UserData() = %d, want %d", got, firstUserData)
	}

	const secondUserData = 2002
	res2 := submitCtrlCmdBounded(t, ring, secondUserData)
	if got, want := res2.Value(), int32(-95); got != want {
		t.Fatalf("second round trip Value() = %d, want %d (-EOPNOTSUPP)", got, want)
	}
	if got := res2.UserData(); got != uint64(secondUserData) {
		t.Fatalf("second round trip UserData() = %d (stale completion from the first call?), want %d", got, secondUserData)
	}
}

// SubmitCtrlCmd must never hand the kernel the caller's buffer (Critical Bug
// #21): the SQE the kernel saw points at the ring's off-heap staging copy,
// the caller's struct is untouched, and the buffer is copied back.
func TestSubmitCtrlCmdStagesCallerBuffer(t *testing.T) {
	ring, err := newMinimalRing(Config{Entries: 4, FD: int32(closedPipe(t).Fd())})
	if err != nil {
		t.Fatalf("newMinimalRing: %v", err)
	}
	defer ring.Close()
	buf := make([]byte, 64)
	for i := range buf {
		buf[i] = byte(0xA0 + i)
	}
	want := append([]byte(nil), buf...)
	cmd := &uapi.UblksrvCtrlCmd{QueueID: 0xFFFF, Len: 64, Addr: AddrOf(buf)}
	res, err := ring.SubmitCtrlCmd(0, cmd, 77)
	if err != nil {
		t.Fatalf("SubmitCtrlCmd: %v", err)
	}
	if res.Value() != -95 || res.UserData() != 77 {
		t.Fatalf("result {ud=%d val=%d}, want {77 -95}", res.UserData(), res.Value())
	}
	core := ring.core
	slot := (*SQE128)(unsafe.Add(core.sqes, uintptr((core.sqeTail-1)&core.sqMask)<<7))
	staged := binary.LittleEndian.Uint64(slot.Cmd()[ctrlCmdAddrOffset:])
	stagedLen := binary.LittleEndian.Uint16(slot.Cmd()[ctrlCmdLenOffset:])
	if staged == AddrOf(buf) || staged != AddrOf(ring.ctrlScratch) || stagedLen != 64 {
		t.Fatalf("SQE addr %#x len %d; caller buffer %#x, staging %#x",
			staged, stagedLen, AddrOf(buf), AddrOf(ring.ctrlScratch))
	}
	if !bytes.Equal(ring.ctrlScratch[:64], want) || !bytes.Equal(buf, want) || cmd.Addr != AddrOf(buf) {
		t.Fatal("staging copy, copy-back or caller struct wrong")
	}
	runtime.KeepAlive(buf)
	// A command without a buffer stages nothing.
	bare, err := newMinimalRing(Config{Entries: 4, FD: int32(closedPipe(t).Fd())})
	if err != nil {
		t.Fatalf("newMinimalRing: %v", err)
	}
	defer bare.Close()
	if _, err := bare.SubmitCtrlCmd(0, &uapi.UblksrvCtrlCmd{}, 1); err != nil || bare.ctrlScratch != nil {
		t.Fatalf("bufferless command: err %v, staging allocated %v", err, bare.ctrlScratch != nil)
	}
}

// queueStandInCtrlCmd queues a read on an empty pipe tagged as the ring's
// next control command: a command the kernel holds onto until the pipe is
// written.
func queueStandInCtrlCmd(t *testing.T, ring *minimalRing, rfd int32) uint64 {
	t.Helper()
	ring.ctrlSeq++
	tag := ctrlTagBase | ring.ctrlSeq
	sqe := ring.core.GetSQE()
	PrepRead(sqe, rfd, offHeap(t, 8), 0)
	sqe.UserData = tag
	if _, err := ring.core.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return tag
}

// A control command that outlives CtrlTimeout yields ErrCtrlTimeout, and its
// late CQE is not mistaken for the next command's.
func TestCtrlTimeoutAndStragglerCQE(t *testing.T) {
	rfd, wfd := pipeFds(t)
	ring, err := newMinimalRing(Config{Entries: 4, FD: rfd, CtrlTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("newMinimalRing: %v", err)
	}
	defer ring.Close()
	tag := queueStandInCtrlCmd(t, ring, rfd)
	start := time.Now()
	if _, err := ring.waitCtrlCompletion(tag); !errors.Is(err, ErrCtrlTimeout) {
		t.Fatalf("waitCtrlCompletion: %v, want ErrCtrlTimeout", err)
	}
	if d := time.Since(start); d < 45*time.Millisecond || d > time.Second {
		t.Errorf("gave up after %v, want ~50ms", d)
	}
	if _, err := unix.Write(int(wfd), []byte("x")); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	time.Sleep(20 * time.Millisecond) // let the straggler land in the CQ
	res, err := ring.SubmitCtrlCmd(0, &uapi.UblksrvCtrlCmd{}, 9)
	if err != nil {
		t.Fatalf("SubmitCtrlCmd after the straggler: %v", err)
	}
	if res.UserData() != 9 || res.Value() != -95 {
		t.Fatalf("result {ud=%d val=%d}, want {9 -95}: the straggler was taken for this command", res.UserData(), res.Value())
	}
}

// Close during an unbounded control wait ends the wait with ErrCtrlTimeout
// and defers teardown until the waiter has left the ring.
func TestCloseDuringUnboundedCtrlWait(t *testing.T) {
	rfd, _ := pipeFds(t)
	ring, err := newMinimalRing(Config{Entries: 4, FD: rfd, CtrlTimeout: -1})
	if err != nil {
		t.Fatalf("newMinimalRing: %v", err)
	}
	tag := queueStandInCtrlCmd(t, ring, rfd)
	done := make(chan error, 1)
	go func() {
		if !ring.acquire() {
			done <- ErrRingClosed
			return
		}
		_, err := ring.waitCtrlCompletion(tag)
		ring.release()
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if err := ring.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrCtrlTimeout) {
			t.Fatalf("wait ended with %v, want ErrCtrlTimeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unbounded control wait did not notice Close")
	}
	if !ring.core.closed || ring.core.fd != -1 {
		t.Fatal("teardown did not run when the waiter left")
	}
}

// The queue runner closes its ring after a bounded join, possibly while its
// ioLoop is still parked in WaitForCompletion. That must not unmap the rings
// under the waiter; the waiter gets ErrRingClosed and teardown follows.
func TestCloseWhileWaitForCompletionParked(t *testing.T) {
	rfd, _ := pipeFds(t)
	ring, err := newMinimalRing(Config{Entries: 4, FD: rfd})
	if err != nil {
		t.Fatalf("newMinimalRing: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		for {
			if _, err := ring.WaitForCompletion(0); err != nil {
				done <- err
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	if err := ring.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrRingClosed) {
			t.Fatalf("waiter ended with %v, want ErrRingClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parked waiter did not return after Close")
	}
	if !ring.core.closed {
		t.Fatal("teardown did not run")
	}
	if err := ring.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := ring.PrepareIOCmd(0, &uapi.UblksrvIOCmd{}, 1); !errors.Is(err, ErrRingClosed) {
		t.Fatalf("PrepareIOCmd after Close: %v, want ErrRingClosed", err)
	}
}

func TestRealRingOpenCloseNoRegistration(t *testing.T) {
	ring, err := NewMinimalRing(4, -1)
	if err != nil {
		t.Fatalf("NewMinimalRing(4, -1): %v", err)
	}
	defer func() {
		if err := ring.Close(); err != nil {
			t.Errorf("ring.Close returned error: %v", err)
		}
	}()
}
