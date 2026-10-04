package ctrl

import (
	"errors"
	"syscall"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

func TestValidateFeatures(t *testing.T) {
	const (
		rec    = uapi.UBLK_F_USER_RECOVERY
		reis   = uapi.UBLK_F_USER_RECOVERY_REISSUE
		failIO = uapi.UBLK_F_USER_RECOVERY_FAIL_IO
		unpriv = uapi.UBLK_F_UNPRIVILEGED_DEV
		ucopy  = uapi.UBLK_F_USER_COPY
		zcopy  = uapi.UBLK_F_SUPPORT_ZERO_COPY
		abr    = uapi.UBLK_F_AUTO_BUF_REG
		gd     = uapi.UBLK_F_NEED_GET_DATA
		batch  = uapi.UBLK_F_BATCH_IO
		desc   = uapi.UBLK_F_IO_DESC_SIZE
	)
	for _, tc := range []struct {
		flags uint64
		desc  uint16
		ok    bool
	}{
		{0, 0, true},
		{rec, 0, true},
		{rec | reis, 0, true},
		{rec | failIO, 0, true},
		{reis, 0, false},
		{failIO, 0, false},
		{rec | reis | failIO, 0, false},
		{uapi.UBLK_F_QUIESCE, 0, false},
		{uapi.UBLK_F_QUIESCE | rec, 0, true},
		{unpriv, 0, true},
		{unpriv | ucopy, 0, false},
		{unpriv | zcopy, 0, false},
		{unpriv | abr, 0, false},
		{unpriv | rec, 0, false},
		{unpriv | uapi.UBLK_F_SHMEM_ZC, 0, true},
		{uapi.UBLK_F_INTEGRITY, 0, false},
		{uapi.UBLK_F_INTEGRITY | ucopy, 0, true},
		{uapi.UBLK_F_ZONED, 0, false},
		{uapi.UBLK_F_ZONED | ucopy, 0, true},
		{uapi.UBLK_F_ZONED | zcopy, 0, true},
		{gd, 0, true},
		{gd | ucopy, 0, false},
		{gd | zcopy, 0, false},
		{gd | abr, 0, false},
		{gd | batch, 0, false},
		{batch, 0, true},
		{uapi.UBLK_F_PER_IO_DAEMON | batch, 0, false},
		{desc, 24, true},
		{desc, 256, true},
		{desc, 16, false},
		{desc, 260, false},
		{desc, 30, false},
		{0, 24, false},
	} {
		err := ValidateFeatures(tc.flags, tc.desc)
		if (err == nil) != tc.ok {
			t.Errorf("ValidateFeatures(%s, %d) = %v, want ok=%v", FeatureNames(tc.flags), tc.desc, err, tc.ok)
		}
		var fe *FeatureConflictError
		if err != nil && (!errors.As(err, &fe) || !errors.Is(err, syscall.EINVAL)) {
			t.Errorf("error type %T", err)
		}
	}
}

func TestFeatureNames(t *testing.T) {
	if got := FeatureNames(uapi.UBLK_F_USER_COPY | uapi.UBLK_F_IO_DESC_SIZE | 1<<40); got != "USER_COPY|IO_DESC_SIZE|bit40" {
		t.Fatal(got)
	}
	if FeatureNames(0) != "none" || len(featureNames) != 21 {
		t.Fatal("feature name table")
	}
	if got := ParamTypeNames(uapi.UBLK_PARAM_TYPE_DMA_ALIGN | uapi.UBLK_PARAM_TYPE_INTEGRITY); got != "DMA_ALIGN|INTEGRITY" {
		t.Fatal(got)
	}
}

func TestNegotiate(t *testing.T) {
	known := FeatureSet{Flags: uapi.UBLK_F_USER_COPY | uapi.UBLK_F_USER_RECOVERY | uapi.UBLK_F_CMD_IOCTL_ENCODE, Known: true}
	send, err := Negotiate(uapi.UBLK_F_USER_COPY, known)
	if err != nil || send != uapi.UBLK_F_USER_COPY|uapi.UBLK_F_CMD_IOCTL_ENCODE {
		t.Fatalf("Negotiate = %#x, %v", send, err)
	}
	_, err = Negotiate(uapi.UBLK_F_USER_COPY|uapi.UBLK_F_ZONED|uapi.UBLK_F_QUIESCE|uapi.UBLK_F_USER_RECOVERY, known)
	var me *MissingFeaturesError
	if !errors.As(err, &me) || me.Missing != uapi.UBLK_F_ZONED|uapi.UBLK_F_QUIESCE || !errors.Is(err, syscall.EOPNOTSUPP) {
		t.Fatalf("missing = %v", err)
	}
	// Unknown (pre-v6.5): nothing is rejected up front; ADD_DEV decides.
	send, err = Negotiate(uapi.UBLK_F_USER_RECOVERY, FeatureSet{Flags: BaseFeatures})
	if err != nil || send != uapi.UBLK_F_USER_RECOVERY|uapi.UBLK_F_CMD_IOCTL_ENCODE {
		t.Fatalf("unknown set: %#x %v", send, err)
	}
	if _, err := Negotiate(uapi.UBLK_F_QUIESCE, known); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("conflict not checked first: %v", err)
	}
}

func TestFeaturesInterpretsOldKernels(t *testing.T) {
	for _, tc := range []struct {
		name  string
		errno syscall.Errno
		known bool
		fails bool
	}{
		{"v7.x", 0, true, false},
		{"pre-v6.5 lookup of dev -1", syscall.ENODEV, false, false},
		{"v6.x unknown op", ENOTSUPP, false, false},
		{"v7.x unknown op", syscall.EOPNOTSUPP, false, false},
		{"permission", syscall.EPERM, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ring := &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
				calls++
				if tc.errno != 0 {
					return controlTestResult(neg(tc.errno)), nil
				}
				controlTestBuffer(cmd)[0] = 0x80 // USER_COPY
				return controlTestResult(0), nil
			}}
			c := newTestController(ring)
			for i := 0; i < 2; i++ {
				fs, err := c.Features(bg)
				if tc.fails {
					if !errors.Is(err, tc.errno) {
						t.Fatalf("err %v", err)
					}
					continue
				}
				if err != nil || fs.Known != tc.known {
					t.Fatalf("fs %+v err %v", fs, err)
				}
				if tc.known && fs.Flags != uapi.UBLK_F_USER_COPY || !tc.known && fs.Flags != BaseFeatures {
					t.Fatalf("flags %#x", fs.Flags)
				}
			}
			if want := map[bool]int{true: 2, false: 1}[tc.fails]; calls != want {
				t.Fatalf("GET_FEATURES sent %d times, want %d (cache only definite answers)", calls, want)
			}
		})
	}
}

// Against the fake driver: a pre-v6.5 kernel answers GET_FEATURES with
// ENODEV, ADD_DEV clears what it does not know, and AddDev reports that and
// deletes the half-wanted device.
func TestAddDevReportsFlagsTheKernelCleared(t *testing.T) {
	k := newFakeKernel()
	k.noGetFeatures = true
	k.features = uapi.UBLK_F_URING_CMD_COMP_IN_TASK | uapi.UBLK_F_NEED_GET_DATA | uapi.UBLK_F_USER_RECOVERY |
		uapi.UBLK_F_USER_RECOVERY_REISSUE | uapi.UBLK_F_UNPRIVILEGED_DEV | uapi.UBLK_F_CMD_IOCTL_ENCODE
	c := newTestController(k.ring())
	fs, err := c.Features(bg)
	if err != nil || fs.Known {
		t.Fatalf("pre-v6.5 features: %+v %v", fs, err)
	}
	if _, err := c.AddDev(bg, AddDevOptions{DevID: AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20,
		Flags: uapi.UBLK_F_USER_RECOVERY}); err != nil {
		t.Fatalf("supported flag on unknown kernel: %v", err)
	}
	_, err = c.AddDev(bg, AddDevOptions{DevID: AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20,
		Flags: uapi.UBLK_F_USER_COPY | uapi.UBLK_F_UPDATE_SIZE})
	var me *MissingFeaturesError
	if !errors.As(err, &me) || me.Missing != uapi.UBLK_F_USER_COPY|uapi.UBLK_F_UPDATE_SIZE || me.Source != "ADD_DEV" {
		t.Fatalf("AddDev = %v", err)
	}
	k.mu.Lock()
	n := len(k.devs)
	k.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d devices left; the one with cleared features must be deleted", n)
	}
}

func TestAddDevRejectsBeforeSending(t *testing.T) {
	k := newFakeKernel()
	k.features &^= uapi.UBLK_F_ZONED
	c := newTestController(k.ring())
	for _, opts := range []AddDevOptions{
		{NrHwQueues: 0, QueueDepth: 8},
		{NrHwQueues: 1, QueueDepth: 4097},
		{NrHwQueues: 1, QueueDepth: 8, Flags: uapi.UBLK_F_QUIESCE},
		{NrHwQueues: 1, QueueDepth: 8, Flags: uapi.UBLK_F_ZONED | uapi.UBLK_F_USER_COPY},
		{NrHwQueues: 1, QueueDepth: 8, IODescSize: 32},
	} {
		if _, err := c.AddDev(bg, opts); err == nil {
			t.Errorf("AddDev(%+v) accepted", opts)
		}
	}
	for _, op := range k.opsSeen() {
		if op == uapi.UBLK_U_CMD_ADD_DEV {
			t.Fatal("ADD_DEV sent for a request rejected up front")
		}
	}
}

// A root caller asking for an unprivileged device gets a privileged one; the
// kernel clears the bit and that is not a missing feature.
func TestAddDevUnprivilegedRequestAsRoot(t *testing.T) {
	c := newTestController(newFakeKernel().ring())
	info, err := c.AddDev(bg, AddDevOptions{DevID: 7, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20, Flags: uapi.UBLK_F_UNPRIVILEGED_DEV})
	if err != nil || info.Flags&uapi.UBLK_F_UNPRIVILEGED_DEV != 0 || info.DevID != 7 {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestWrongGetFeaturesEncodingFails(t *testing.T) {
	// The driver compares GET_FEATURES in full; an _IOWR encoding falls into
	// the device lookup and fails ENODEV. This pins why UblkCtrlCmd must
	// produce _IOR for it.
	k := newFakeKernel()
	wrong := uapi.IoctlEncode(3, 'u', uapi.UBLK_CMD_GET_FEATURES, 32)
	cmd := uapi.UblksrvCtrlCmd{DevID: AnyDevID, Len: 8}
	if res := k.dispatch(wrong, &cmd); res != neg(syscall.ENODEV) {
		t.Fatalf("fake kernel accepted _IOWR GET_FEATURES: %d", res)
	}
	if uapi.UblkCtrlCmd(uapi.UBLK_CMD_GET_FEATURES) != uapi.UBLK_U_CMD_GET_FEATURES {
		t.Fatal("UblkCtrlCmd(GET_FEATURES) is not the header encoding")
	}
}
