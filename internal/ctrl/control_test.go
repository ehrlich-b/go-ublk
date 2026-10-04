package ctrl

import (
	"encoding/binary"
	"errors"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

type controlTestRing struct {
	uring.Ring
	submit   func(uint32, *uapi.UblksrvCtrlCmd) (uring.Result, error)
	closeErr error

	mu     sync.Mutex
	closes int
}

func (r *controlTestRing) SubmitCtrlCmd(op uint32, cmd *uapi.UblksrvCtrlCmd, _ uint64) (uring.Result, error) {
	return r.submit(op, cmd)
}

func (r *controlTestRing) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes++
	return r.closeErr
}

func (r *controlTestRing) closeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closes
}

type controlTestResult int32

func (r controlTestResult) Value() int32     { return int32(r) }
func (r controlTestResult) UserData() uint64 { return 0 }
func (r controlTestResult) Error() error {
	if r < 0 {
		return syscall.Errno(-int64(r))
	}
	return nil
}

// controlTestBuffer stands in for the driver's copy_to/from_user: it
// reinterprets the command's address bits as a pointer, only at this
// synthetic kernel boundary. The addresses are mmap'd scratch pages.
//
//go:nocheckptr
func controlTestBuffer(cmd *uapi.UblksrvCtrlCmd) []byte {
	if cmd.Addr == 0 {
		return nil
	}
	addr := *(*unsafe.Pointer)(unsafe.Pointer(&cmd.Addr))
	return unsafe.Slice((*byte)(addr), int(cmd.Len))
}

// testSlots builds Controllers whose slots use real mmap'd scratch pages but
// a fake ring, and records every unmap so tests can check nothing the kernel
// may still write is released.
type testSlots struct {
	ring *controlTestRing

	mu      sync.Mutex
	created int
	unmaps  int
}

func (ts *testSlots) factory() (*slot, error) {
	mem, err := unix.Mmap(-1, 0, scratchSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		return nil, err
	}
	ts.mu.Lock()
	ts.created++
	ts.mu.Unlock()
	return &slot{ring: ts.ring, fd: -1, mem: mem, unmap: func(b []byte) error {
		ts.mu.Lock()
		ts.unmaps++
		ts.mu.Unlock()
		return unix.Munmap(b)
	}}, nil
}

func (ts *testSlots) counts() (created, unmaps int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.created, ts.unmaps
}

func newTestControllerSlots(t testing.TB, ring *controlTestRing) (*Controller, *testSlots) {
	t.Helper()
	ts := &testSlots{ring: ring}
	c, err := newController(ts.factory)
	if err != nil {
		t.Fatal(err)
	}
	c.logger = logging.Default()
	return c, ts
}

func newTestController(ring *controlTestRing) *Controller {
	ts := &testSlots{ring: ring}
	c, err := newController(ts.factory)
	if err != nil {
		panic(err)
	}
	return c
}

func getDevInfoForPinTest(c *Controller, id uint32) (*uapi.UblksrvCtrlDevInfo, error) {
	return c.GetDevInfo(bg, id)
}

func TestGetParamsRequestAndFixedOffsetResponse(t *testing.T) {
	ring := &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		if op == uapi.UBLK_U_CMD_GET_DEV_INFO {
			return controlTestResult(0), nil // privilege probe: privileged device
		}
		if op != uapi.UBLK_U_CMD_GET_PARAMS || cmd.DevID != 42 || cmd.QueueID != 0xffff || cmd.DevPathLen != 0 {
			t.Fatalf("wrong request: op=%x %+v", op, cmd)
		}
		buf := controlTestBuffer(cmd)
		if len(buf) < uapi.UblkParamsSize || binary.LittleEndian.Uint32(buf[:4]) != uint32(len(buf)) {
			t.Fatal("GET_PARAMS input length missing or exceeds capacity")
		}
		clear(buf)
		// Independent kernel layout: basic @8, discard hole @40, devt @60.
		binary.LittleEndian.PutUint32(buf[:4], 112)
		binary.LittleEndian.PutUint32(buf[4:8], uapi.UBLK_PARAM_TYPE_BASIC|uapi.UBLK_PARAM_TYPE_DEVT)
		binary.LittleEndian.PutUint64(buf[24:32], 8192)
		binary.LittleEndian.PutUint32(buf[60:64], 241)
		binary.LittleEndian.PutUint32(buf[64:68], 42)
		binary.LittleEndian.PutUint32(buf[68:72], 259)
		binary.LittleEndian.PutUint32(buf[72:76], 7)
		return controlTestResult(0), nil
	}}
	p, err := newTestController(ring).GetParams(bg, 42)
	if err != nil {
		t.Fatal(err)
	}
	if p.Basic.DevSectors != 8192 || p.Devt != (uapi.UblkParamDevt{CharMajor: 241, CharMinor: 42, DiskMajor: 259, DiskMinor: 7}) || p.Discard != (uapi.UblkParamDiscard{}) {
		t.Fatalf("decoded wrong kernel offsets: %+v", p)
	}
}

func TestGetParamsRetainedSetLengthWithKernelAddedDevt(t *testing.T) {
	// Linux copies params.Len from SET_PARAMS verbatim but adds DEVT to GET,
	// including before SET_PARAMS. A large retained Len may also exceed the
	// GET buffer capacity while all known blocks fit the returned prefix.
	for _, length := range []uint32{0, 40, 60, 112, 4096} {
		ring := &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
			if op == uapi.UBLK_U_CMD_GET_DEV_INFO {
				return controlTestResult(0), nil
			}
			buf := controlTestBuffer(cmd)
			clear(buf)
			binary.LittleEndian.PutUint32(buf, length)
			types := uint32(uapi.UBLK_PARAM_TYPE_BASIC | uapi.UBLK_PARAM_TYPE_DEVT)
			if length == 0 {
				types = uapi.UBLK_PARAM_TYPE_DEVT
			}
			binary.LittleEndian.PutUint32(buf[4:8], types)
			binary.LittleEndian.PutUint32(buf[60:64], 241)
			binary.LittleEndian.PutUint32(buf[68:72], 259)
			return controlTestResult(0), nil
		}}
		p, err := newTestController(ring).GetParams(bg, 1)
		if err != nil || p.Len != length || p.Devt.CharMajor != 241 || p.Devt.DiskMajor != 259 {
			t.Fatalf("Len %d: p=%+v err=%v", length, p, err)
		}
	}
}

func TestControllerCloseReleasesIdleSlots(t *testing.T) {
	sentinel := errors.New("synthetic ring close error")
	ring := &controlTestRing{closeErr: sentinel}
	c, ts := newTestControllerSlots(t, ring)
	if err := c.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("lost ring close error: %v", err)
	}
	if _, unmaps := ts.counts(); ring.closeCount() != 1 || unmaps != 1 {
		t.Fatalf("idle slot not released: closes=%d unmaps=%d", ring.closeCount(), unmaps)
	}
	if err := c.Close(); err != nil || ring.closeCount() != 1 {
		t.Fatalf("second close: %v closes=%d", err, ring.closeCount())
	}
	if _, err := c.GetFeatures(bg); !errors.Is(err, ErrClosed) {
		t.Fatalf("command after Close: %v", err)
	}
}

func TestControllerCloseFdError(t *testing.T) {
	ring := &controlTestRing{}
	ts := &testSlots{ring: ring}
	c, err := newController(func() (*slot, error) {
		s, err := ts.factory()
		if s != nil {
			s.fd = 1 << 30 // not open: close fails with EBADF
		}
		return s, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("fd close error missing: %v", err)
	}
}

func TestNewControllerReportsOpenError(t *testing.T) {
	_, err := newController(func() (*slot, error) { return nil, syscall.EACCES })
	if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("open error lost: %v", err)
	}
}

// A transport failure leaves the command's fate unknown (the old ring's wait
// times out while the kernel still runs it), so the slot must never be reused
// and its scratch page must never be unmapped.
func TestTransportErrorRetiresSlot(t *testing.T) {
	fail := true
	ring := &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		if fail {
			return nil, errors.New("synthetic: timeout waiting for control command completion")
		}
		binary.LittleEndian.PutUint64(controlTestBuffer(cmd), 7)
		return controlTestResult(0), nil
	}}
	c, ts := newTestControllerSlots(t, ring)
	if _, err := c.GetFeatures(bg); err == nil {
		t.Fatal("transport error not reported")
	}
	if created, unmaps := ts.counts(); created != 1 || unmaps != 0 || ring.closeCount() != 1 {
		t.Fatalf("retired slot: created=%d unmaps=%d closes=%d", created, unmaps, ring.closeCount())
	}
	fail = false
	if f, err := c.GetFeatures(bg); err != nil || f != 7 {
		t.Fatalf("next command: %v %v", f, err)
	}
	if created, unmaps := ts.counts(); created != 2 || unmaps != 0 {
		t.Fatalf("retired slot reused: created=%d unmaps=%d", created, unmaps)
	}
}
