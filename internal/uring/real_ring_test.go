package uring

import (
	"os"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
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
