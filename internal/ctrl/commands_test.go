package ctrl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

func addTestDev(t *testing.T, c *Controller, flags uint64) *uapi.UblksrvCtrlDevInfo {
	t.Helper()
	info, err := c.AddDev(bg, AddDevOptions{DevID: AnyDevID, NrHwQueues: 2, QueueDepth: 64, MaxIOBufBytes: 1<<20 + 100, Flags: flags, UblksrvFlags: 0xC0FFEE})
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func testParams() *uapi.UblkParams {
	return &uapi.UblkParams{
		Types: uapi.UBLK_PARAM_TYPE_BASIC | uapi.UBLK_PARAM_TYPE_DISCARD | uapi.UBLK_PARAM_TYPE_DMA_ALIGN | uapi.UBLK_PARAM_TYPE_SEGMENT,
		Basic: uapi.UblkParamBasic{LogicalBSShift: 12, PhysicalBSShift: 12, IOMinShift: 12, MaxSectors: 2048, DevSectors: 1 << 21},
		Discard: uapi.UblkParamDiscard{DiscardGranularity: 4096, MaxDiscardSectors: 1 << 16,
			MaxWriteZeroesSectors: 1 << 16, MaxDiscardSegments: 1},
		DMA: uapi.UblkParamDMAAlign{Alignment: 511},
		Seg: uapi.UblkParamSegment{SegBoundaryMask: 1<<32 - 1, MaxSegmentSize: 1 << 16, MaxSegments: 128},
	}
}

// TestCommandLifecycleAgainstFakeKernel runs every control command through a
// model of the driver's dispatch, privileged and unprivileged.
func TestCommandLifecycleAgainstFakeKernel(t *testing.T) {
	for _, unpriv := range []bool{false, true} {
		t.Run(fmt.Sprintf("unprivileged=%v", unpriv), func(t *testing.T) {
			k := newFakeKernel()
			k.callerUnpriv = unpriv
			c := newTestController(k.ring())
			flags := uint64(uapi.UBLK_F_USER_RECOVERY | uapi.UBLK_F_QUIESCE | uapi.UBLK_F_SHMEM_ZC)
			if unpriv {
				flags = uapi.UBLK_F_UNPRIVILEGED_DEV | uapi.UBLK_F_SHMEM_ZC
			}
			info := addTestDev(t, c, flags)
			id := info.DevID
			if info.UblksrvFlags != 0xC0FFEE || info.NrHwQueues != 2 || info.MaxIOBufBytes != 1<<20 ||
				info.Flags&uapi.UBLK_F_CMD_IOCTL_ENCODE == 0 || info.IODescSize != 24 {
				t.Fatalf("ADD_DEV result %+v", info)
			}
			if got := info.Flags&uapi.UBLK_F_UNPRIVILEGED_DEV != 0; got != unpriv {
				t.Fatalf("UNPRIVILEGED_DEV = %v", got)
			}

			for _, get := range []func() (*uapi.UblksrvCtrlDevInfo, error){
				func() (*uapi.UblksrvCtrlDevInfo, error) { return c.GetDevInfo(bg, id) },
				func() (*uapi.UblksrvCtrlDevInfo, error) { return c.GetDevInfo2(bg, id) },
			} {
				got, err := get()
				if err != nil || *got != *info {
					t.Fatalf("GET_DEV_INFO = %+v, %v; want %+v", got, err, info)
				}
			}

			p := testParams()
			if err := c.SetParams(bg, id, p); err != nil {
				t.Fatal(err)
			}
			got, err := c.GetParams(bg, id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Types != p.Types|uapi.UBLK_PARAM_TYPE_DEVT || got.Len != uapi.UblkParamsSize ||
				got.Basic != p.Basic || got.Discard != p.Discard || got.DMA != p.DMA || got.Seg != p.Seg ||
				got.Devt.CharMajor != 240 || got.Devt.CharMinor != id {
				t.Fatalf("GET_PARAMS = %+v", got)
			}

			cpus, err := c.GetQueueAffinity(bg, id, 1)
			if err != nil || !reflect.DeepEqual(cpus, []int{1, 3, 5}) {
				t.Fatalf("GET_QUEUE_AFFINITY = %v, %v", cpus, err)
			}
			if _, err := c.GetQueueAffinity(bg, id, 2); !errors.Is(err, syscall.EINVAL) {
				t.Fatalf("affinity of a missing queue: %v", err)
			}

			if err := c.UpdateSize(bg, id, 1<<22); !errors.Is(err, ErrNotStarted) {
				t.Fatalf("UPDATE_SIZE before START_DEV: %v", err)
			}
			if err := c.TryStopDev(bg, id); !errors.Is(err, syscall.ENODEV) {
				t.Fatalf("TRY_STOP_DEV before START_DEV: %v", err)
			}
			if err := c.StartDev(bg, id, 4242); err != nil {
				t.Fatal(err)
			}
			if err := c.SetParams(bg, id, p); !errors.Is(err, syscall.EACCES) {
				t.Fatalf("SET_PARAMS after START_DEV: %v", err)
			}
			if err := c.UpdateSize(bg, id, 1<<22); err != nil {
				t.Fatal(err)
			}
			if got, _ := c.GetParams(bg, id); got.Basic.DevSectors != 1<<22 {
				t.Fatalf("UPDATE_SIZE not applied: %d", got.Basic.DevSectors)
			}

			idx, err := c.RegBuf(bg, id, 1<<30, 1<<20, uapi.UBLK_SHMEM_BUF_READ_ONLY)
			if err != nil || idx != 0 {
				t.Fatalf("REG_BUF = %d, %v", idx, err)
			}
			if _, err := c.RegBuf(bg, id, 1<<30+1, 1<<20, 0); !errors.Is(err, syscall.EINVAL) {
				t.Fatalf("misaligned REG_BUF: %v", err)
			}
			if err := c.UnregBuf(bg, id, idx); err != nil {
				t.Fatal(err)
			}
			if err := c.UnregBuf(bg, id, idx); !errors.Is(err, syscall.ENOENT) {
				t.Fatalf("double UNREG_BUF: %v", err)
			}

			if !unpriv {
				if err := c.QuiesceDev(bg, id, 5*time.Millisecond); !errors.Is(err, syscall.EBUSY) {
					t.Fatalf("QUIESCE_DEV short timeout: %v", err)
				}
				if err := c.QuiesceDev(bg, id, time.Second); err != nil {
					t.Fatal(err)
				}
				if err := c.StartUserRecovery(bg, id); err != nil {
					t.Fatal(err)
				}
				if err := c.EndUserRecovery(bg, id, 4243); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := c.QuiesceDev(bg, id, time.Second); !errors.Is(err, syscall.EOPNOTSUPP) {
					t.Fatalf("QUIESCE_DEV without UBLK_F_QUIESCE: %v", err)
				}
				if err := c.StartUserRecovery(bg, id); !errors.Is(err, syscall.EINVAL) {
					t.Fatalf("START_USER_RECOVERY without USER_RECOVERY: %v", err)
				}
			}

			if err := c.TryStopDev(bg, id); err != nil {
				t.Fatal(err)
			}
			if err := c.StopDev(bg, id); err != nil {
				t.Fatal(err)
			}
			if err := c.DelDev(bg, id); err != nil {
				t.Fatal(err)
			}
			if _, err := c.GetDevInfo(bg, id); !errors.Is(err, syscall.ENODEV) {
				t.Fatalf("GET_DEV_INFO after DEL_DEV: %v", err)
			}
			info2 := addTestDev(t, c, flags&uapi.UBLK_F_UNPRIVILEGED_DEV)
			if err := c.DelDevAsync(bg, info2.DevID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Every opcode sent must be the header's exact encoding (GET_FEATURES is
// matched in full by the driver; the rest are checked here regardless).
func TestCommandsUseExactOpcodes(t *testing.T) {
	k := newFakeKernel()
	c := newTestController(k.ring())
	info := addTestDev(t, c, uapi.UBLK_F_USER_RECOVERY|uapi.UBLK_F_QUIESCE|uapi.UBLK_F_SHMEM_ZC)
	id := info.DevID
	_ = c.SetParams(bg, id, testParams())
	_, _ = c.GetDevInfo2(bg, id)
	_, _ = c.GetQueueAffinity(bg, id, 0)
	_ = c.StartDev(bg, id, 1)
	_ = c.UpdateSize(bg, id, 8)
	_, _ = c.RegBuf(bg, id, 0, 4096, 0)
	_ = c.UnregBuf(bg, id, 0)
	_ = c.QuiesceDev(bg, id, time.Second)
	_ = c.StartUserRecovery(bg, id)
	_ = c.EndUserRecovery(bg, id, 1)
	_ = c.TryStopDev(bg, id)
	_ = c.StopDev(bg, id)
	_ = c.DelDevAsync(bg, id)
	_ = c.DelDev(bg, addTestDev(t, c, 0).DevID)
	allowed := map[uint32]bool{
		uapi.UBLK_U_CMD_GET_FEATURES: true, uapi.UBLK_U_CMD_ADD_DEV: true, uapi.UBLK_U_CMD_GET_DEV_INFO: true,
		uapi.UBLK_U_CMD_SET_PARAMS: true, uapi.UBLK_U_CMD_GET_PARAMS: true, uapi.UBLK_U_CMD_GET_DEV_INFO2: true,
		uapi.UBLK_U_CMD_GET_QUEUE_AFFINITY: true, uapi.UBLK_U_CMD_START_DEV: true, uapi.UBLK_U_CMD_UPDATE_SIZE: true,
		uapi.UBLK_U_CMD_REG_BUF: true, uapi.UBLK_U_CMD_UNREG_BUF: true, uapi.UBLK_U_CMD_QUIESCE_DEV: true,
		uapi.UBLK_U_CMD_START_USER_RECOVERY: true, uapi.UBLK_U_CMD_END_USER_RECOVERY: true,
		uapi.UBLK_U_CMD_TRY_STOP_DEV: true, uapi.UBLK_U_CMD_STOP_DEV: true, uapi.UBLK_U_CMD_DEL_DEV_ASYNC: true,
		uapi.UBLK_U_CMD_DEL_DEV: true,
	}
	seen := map[uint32]bool{}
	for _, op := range k.opsSeen() {
		if !allowed[op] {
			t.Errorf("unexpected opcode %#x", op)
		}
		seen[op] = true
	}
	for op := range allowed {
		if !seen[op] {
			t.Errorf("opcode %#x never sent", op)
		}
	}
}

// The unprivileged buffer layout: [path NUL pad][payload], dev_path_len =
// padded path length, len covering both; privileged devices get no path
// except for GET_DEV_INFO2.
func TestDevPathLayout(t *testing.T) {
	type seen struct {
		op  uint32
		cmd uapi.UblksrvCtrlCmd
		buf []byte
	}
	var calls []seen
	unprivileged := true
	ring := &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		calls = append(calls, seen{op, *cmd, append([]byte(nil), controlTestBuffer(cmd)...)})
		if op == uapi.UBLK_U_CMD_GET_DEV_INFO && cmd.DevPathLen == 0 && unprivileged {
			return controlTestResult(neg(syscall.EINVAL)), nil
		}
		return controlTestResult(0), nil
	}}
	c := newTestController(ring)
	if err := c.QuiesceDev(bg, 123, 1500*time.Microsecond); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].op != uapi.UBLK_U_CMD_GET_DEV_INFO || calls[0].cmd.DevPathLen != 0 {
		t.Fatalf("expected GET_DEV_INFO probe then QUIESCE_DEV: %+v", calls)
	}
	q := calls[1]
	path := "/dev/ublkc123"
	if q.op != uapi.UBLK_U_CMD_QUIESCE_DEV || q.cmd.DevPathLen != 16 || q.cmd.Len != 16 || q.cmd.Data != 2 ||
		string(q.buf[:len(path)]) != path || q.buf[len(path)] != 0 {
		t.Fatalf("QUIESCE_DEV with path: %+v buf %q", q.cmd, q.buf)
	}

	calls = nil
	if _, err := c.GetParams(bg, 123); err != nil {
		t.Fatal(err)
	}
	g := calls[1]
	if g.cmd.DevPathLen != 16 || int(g.cmd.Len) != 16+getParamsCapacity ||
		binary.LittleEndian.Uint32(g.buf[16:20]) != getParamsCapacity {
		t.Fatalf("GET_PARAMS with path: %+v", g.cmd)
	}

	unprivileged = false
	calls = nil
	if _, err := c.GetParams(bg, 123); err != nil {
		t.Fatal(err)
	}
	if g := calls[1]; g.cmd.DevPathLen != 0 || int(g.cmd.Len) != getParamsCapacity {
		t.Fatalf("privileged GET_PARAMS: %+v", g.cmd)
	}
	calls = nil
	if _, err := c.GetDevInfo2(bg, 123); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].cmd.DevPathLen != 16 || calls[0].cmd.Len != 16+64 {
		t.Fatalf("GET_DEV_INFO2 must always carry the path: %+v", calls)
	}
	calls = nil
	if _, err := c.GetFeatures(bg); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].cmd.DevID != AnyDevID || calls[0].cmd.Len != 8 || calls[0].cmd.DevPathLen != 0 {
		t.Fatalf("GET_FEATURES: %+v", calls)
	}
}

func TestSetParamsReportsTypesOlderKernelsDrop(t *testing.T) {
	for _, kernel := range []struct {
		name    string
		size    int
		all     uint32
		dropped uint32
	}{
		{"v6.8", 112, 0xf, uapi.UBLK_PARAM_TYPE_DMA_ALIGN | uapi.UBLK_PARAM_TYPE_SEGMENT},
		{"v6.15", 136, 0x3f, 0},
	} {
		t.Run(kernel.name, func(t *testing.T) {
			k := newFakeKernel()
			k.paramsSize, k.paramTypesAll = kernel.size, kernel.all
			c := newTestController(k.ring())
			id := addTestDev(t, c, 0).DevID
			err := c.SetParams(bg, id, testParams())
			var ue *UnsupportedParamsError
			if kernel.dropped == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.As(err, &ue) || ue.Dropped != kernel.dropped || !errors.Is(err, syscall.EOPNOTSUPP) {
				t.Fatalf("SetParams on %s = %v", kernel.name, err)
			}
			got, _ := c.GetParams(bg, id)
			if got.Basic != testParams().Basic || got.Discard != testParams().Discard {
				t.Fatalf("supported blocks not applied: %+v", got)
			}
		})
	}
}

func TestSetParamsRejectsDevtAndMissingBasic(t *testing.T) {
	c := newTestController(newFakeKernel().ring())
	for _, types := range []uint32{uapi.UBLK_PARAM_TYPE_BASIC | uapi.UBLK_PARAM_TYPE_DEVT, uapi.UBLK_PARAM_TYPE_DISCARD} {
		if err := c.SetParams(bg, 0, &uapi.UblkParams{Types: types}); !errors.Is(err, syscall.EINVAL) {
			t.Fatalf("types %#x: %v", types, err)
		}
	}
}

func TestQuiesceTimeoutEncoding(t *testing.T) {
	var data []uint64
	ring := &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		if op == uapi.UBLK_U_CMD_QUIESCE_DEV {
			data = append(data, cmd.Data)
		}
		return controlTestResult(0), nil
	}}
	c := newTestController(ring)
	for _, d := range []time.Duration{0, time.Nanosecond, time.Millisecond, 1500 * time.Microsecond, 100 * 24 * time.Hour * 365} {
		if err := c.QuiesceDev(bg, 1, d); err != nil {
			t.Fatal(err)
		}
	}
	want := []uint64{0, 1, 1, 2, 1<<32 - 1}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("timeouts sent %v, want %v", data, want)
	}
	if err := c.QuiesceDev(bg, 1, -time.Second); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("negative timeout: %v", err)
	}
}

func TestControlOperationsPreserveErrors(t *testing.T) {
	params := DefaultDeviceParams(plainBackend{})
	operations := []struct {
		name string
		call func(*Controller) error
	}{
		{"add", func(c *Controller) error { _, err := c.AddDevice(bg, &params); return err }},
		{"set params", func(c *Controller) error { return c.SetDeviceParams(bg, 42, &params) }},
		{"start", func(c *Controller) error { return c.StartDev(bg, 42, 1) }},
		{"stop", func(c *Controller) error { return c.StopDev(bg, 42) }},
		{"try stop", func(c *Controller) error { return c.TryStopDev(bg, 42) }},
		{"delete", func(c *Controller) error { return c.DelDev(bg, 42) }},
		{"delete async", func(c *Controller) error { return c.DelDevAsync(bg, 42) }},
		{"get info", func(c *Controller) error { _, err := c.GetDevInfo(bg, 42); return err }},
		{"get info2", func(c *Controller) error { _, err := c.GetDevInfo2(bg, 42); return err }},
		{"get params", func(c *Controller) error { _, err := c.GetParams(bg, 42); return err }},
		{"affinity", func(c *Controller) error { _, err := c.GetQueueAffinity(bg, 42, 0); return err }},
		{"features", func(c *Controller) error { _, err := c.GetFeatures(bg); return err }},
		{"start recovery", func(c *Controller) error { return c.StartUserRecovery(bg, 42) }},
		{"end recovery", func(c *Controller) error { return c.EndUserRecovery(bg, 42, 1) }},
		{"update size", func(c *Controller) error { return c.UpdateSize(bg, 42, 8) }},
		{"quiesce", func(c *Controller) error { return c.QuiesceDev(bg, 42, time.Second) }},
		{"reg buf", func(c *Controller) error { _, err := c.RegBuf(bg, 42, 0, 4096, 0); return err }},
		{"unreg buf", func(c *Controller) error { return c.UnregBuf(bg, 42, 0) }},
	}
	for _, op := range operations {
		for _, transport := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/transport=%v", op.name, transport), func(t *testing.T) {
				ring := &controlTestRing{submit: func(_ uint32, _ *uapi.UblksrvCtrlCmd) (uring.Result, error) {
					if transport {
						return nil, fmt.Errorf("synthetic submit: %w", syscall.ENOMEM)
					}
					return controlTestResult(neg(syscall.ENOMEM)), nil
				}}
				err := op.call(newTestController(ring))
				var ce *Error
				if !errors.Is(err, syscall.ENOMEM) || !errors.As(err, &ce) {
					t.Fatalf("lost errno: %v", err)
				}
			})
		}
	}
}

func TestDecodeCPUMask(t *testing.T) {
	for _, tc := range []struct {
		mask []byte
		want []int
	}{
		{make([]byte, 8), []int{}},
		{[]byte{0x01, 0, 0, 0, 0, 0, 0, 0x80}, []int{0, 63}},
		{[]byte{0xff, 0x01}, []int{0, 1, 2, 3, 4, 5, 6, 7, 8}},
		{[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0x02}, []int{65}},
	} {
		if got := decodeCPUMask(tc.mask); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("decodeCPUMask(%x) = %v, want %v", tc.mask, got, tc.want)
		}
	}
}
