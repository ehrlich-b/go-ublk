package ctrl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

type controlTestRing struct {
	uring.Ring
	submit   func(uint32, *uapi.UblksrvCtrlCmd) (uring.Result, error)
	closeErr error
	closes   int
}

func (r *controlTestRing) SubmitCtrlCmd(op uint32, cmd *uapi.UblksrvCtrlCmd, _ uint64) (uring.Result, error) {
	return r.submit(op, cmd)
}
func (r *controlTestRing) Close() error { r.closes++; return r.closeErr }

type controlTestResult int32

func (r controlTestResult) Value() int32     { return int32(r) }
func (r controlTestResult) UserData() uint64 { return 0 }
func (r controlTestResult) Error() error {
	if r < 0 {
		return syscall.Errno(-int64(r))
	}
	return nil
}

// This stands in for the driver's copy_to/from_user against a Go-owned buffer.
// Reinterpret the command's address bits as a pointer only at this synthetic
// kernel boundary. checkptr cannot track addresses carried as uint64; suppress
// it only here. Payload addresses are never dereferenced.
//
//go:nocheckptr
func controlTestBuffer(cmd *uapi.UblksrvCtrlCmd) []byte {
	addr := *(*unsafe.Pointer)(unsafe.Pointer(&cmd.Addr))
	return unsafe.Slice((*byte)(addr), int(cmd.Len))
}

func TestGetParamsRequestAndFixedOffsetResponse(t *testing.T) {
	ring := &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		if op != uapi.UblkCtrlCmd(uapi.UBLK_CMD_GET_PARAMS) || cmd.DevID != 42 || cmd.QueueID != 0xffff {
			t.Fatalf("wrong request: op=%x %+v", op, cmd)
		}
		buf := controlTestBuffer(cmd)
		if len(buf) < 112 || binary.LittleEndian.Uint32(buf[:4]) != uint32(len(buf)) {
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
	c := &Controller{controlFd: -1, ring: ring}
	p, err := c.GetParams(42)
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
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			ring := &controlTestRing{submit: func(_ uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
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
			p, err := (&Controller{controlFd: -1, ring: ring}).GetParams(1)
			if err != nil || p.Len != length || p.Devt.CharMajor != 241 || p.Devt.DiskMajor != 259 {
				t.Fatalf("p=%+v err=%v", p, err)
			}
		})
	}
}

func TestControlOperationsPreserveErrors(t *testing.T) {
	params := DefaultDeviceParams(plainBackend{})
	operations := []struct {
		name string
		call func(*Controller) error
	}{
		{"add", func(c *Controller) error { _, err := c.AddDevice(&params); return err }},
		{"set params", func(c *Controller) error { return c.SetParams(42, &params) }},
		{"start", func(c *Controller) error { return c.StartDevice(42) }},
		{"stop", func(c *Controller) error { return c.StopDevice(42) }},
		{"delete", func(c *Controller) error { return c.DeleteDevice(42) }},
		{"get info", func(c *Controller) error { _, err := c.GetDeviceInfo(42); return err }},
		{"get params", func(c *Controller) error { _, err := c.GetParams(42); return err }},
	}
	for _, op := range operations {
		for _, transport := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/transport=%v", op.name, transport), func(t *testing.T) {
				ring := &controlTestRing{submit: func(_ uint32, _ *uapi.UblksrvCtrlCmd) (uring.Result, error) {
					if transport {
						return nil, fmt.Errorf("synthetic submit: %w", syscall.EOPNOTSUPP)
					}
					return controlTestResult(-int32(syscall.EOPNOTSUPP)), nil
				}}
				c := &Controller{controlFd: -1, ring: ring, logger: logging.Default()}
				if err := op.call(c); !errors.Is(err, syscall.EOPNOTSUPP) {
					t.Fatalf("lost errno: %v", err)
				}
			})
		}
	}
}

func TestControllerCloseConsumesResourcesOnError(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "control-fd")
	if err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Dup(int(file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	sentinel := errors.New("synthetic ring close error")
	ring := &controlTestRing{closeErr: sentinel}
	c := &Controller{controlFd: fd, ring: ring}
	if err := c.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("lost ring close error: %v", err)
	}
	if c.controlFd != -1 || c.ring != nil || ring.closes != 1 {
		t.Fatalf("resources not consumed: fd=%d closes=%d", c.controlFd, ring.closes)
	}
	if err := syscall.Fsync(fd); err != syscall.EBADF {
		t.Fatalf("control fd still open: %v", err)
	}
	if err := c.Close(); err != nil || ring.closes != 1 {
		t.Fatalf("second close: %v closes=%d", err, ring.closes)
	}
}

func TestControllerCloseJoinsErrors(t *testing.T) {
	sentinel := errors.New("ring close")
	c := &Controller{controlFd: 1 << 30, ring: &controlTestRing{closeErr: sentinel}}
	err := c.Close()
	if !errors.Is(err, sentinel) || !errors.Is(err, syscall.EBADF) {
		t.Fatalf("close errors missing: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
