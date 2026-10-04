//go:build linux

// Command ctrl exercises every ublk control command against the running
// kernel through internal/ctrl, plus raw submissions that check the driver
// behaviour internal/ctrl relies on (opcode matching, the feature rules,
// the unprivileged dev-path protocol). It creates and deletes real devices:
// run it only in a disposable test guest.
//
//	ctrl                 # as root: everything except unprivileged mode
//	ctrl -mode unpriv    # as a normal user; needs /dev/ublk-control mode 0666
//	                     # and /dev/ublkc* owned by that user (udev rules)
//	ctrl -mode inspect -dev N   # as root: control an unprivileged device
//
// Each check prints PASS, FAIL or SKIP; the exit status is 1 on any FAIL.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/ctrl"
	"github.com/ehrlich-b/go-ublk/internal/queue"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

var failures int

func pass(name, format string, a ...any) {
	fmt.Printf("PASS %-34s %s\n", name, fmt.Sprintf(format, a...))
}
func skip(name, format string, a ...any) {
	fmt.Printf("SKIP %-34s %s\n", name, fmt.Sprintf(format, a...))
}
func fail(name, format string, a ...any) {
	failures++
	fmt.Printf("FAIL %-34s %s\n", name, fmt.Sprintf(format, a...))
}

// check reports err == nil as PASS.
func check(name string, err error, detail string) bool {
	if err != nil {
		fail(name, "%v", err)
		return false
	}
	pass(name, "%s", detail)
	return true
}

// expect reports errors.Is(err, want) as PASS.
func expect(name string, err, want error) {
	if errors.Is(err, want) {
		pass(name, "got %v as expected", want)
	} else {
		fail(name, "got %v, want %v", err, want)
	}
}

func bg() context.Context { return context.Background() }

func timeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// raw submits hand-built control commands, bypassing internal/ctrl, to check
// the kernel facts it is built on. Buffers are mmap'd so they never move.
type raw struct {
	fd   int
	ring uring.Ring
	mem  []byte
}

func newRaw() *raw {
	fd, err := syscall.Open(uapi.UBLK_CONTROL_DEV, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		panic(err)
	}
	ring, err := uring.NewRing(uring.Config{Entries: 4, FD: int32(fd)})
	if err != nil {
		panic(err)
	}
	mem, err := unix.Mmap(-1, 0, 8192, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		panic(err)
	}
	return &raw{fd: fd, ring: ring, mem: mem}
}

// submit sends op with buf copied into the mmap'd scratch (devPathLen bytes
// of it being a path prefix) and returns the CQE result and the buffer after.
func (r *raw) submit(op, devID uint32, data uint64, buf []byte, devPathLen uint16) (int32, []byte) {
	clear(r.mem)
	copy(r.mem, buf)
	cmd := uapi.UblksrvCtrlCmd{DevID: devID, QueueID: 0xffff, Len: uint16(len(buf)), Data: data, DevPathLen: devPathLen}
	if len(buf) > 0 {
		cmd.Addr = uint64(uintptr(unsafe.Pointer(&r.mem[0])))
	}
	res, err := r.ring.SubmitCtrlCmd(op, &cmd, 0)
	if err != nil {
		panic(err)
	}
	return res.Value(), append([]byte(nil), r.mem[:len(buf)]...)
}

func (r *raw) addDev(flags uint64) (int32, uapi.UblksrvCtrlDevInfo) {
	in := uapi.UblksrvCtrlDevInfo{NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20, DevID: ctrl.AnyDevID, Flags: flags}
	res, out := r.submit(uapi.UBLK_U_CMD_ADD_DEV, ctrl.AnyDevID, 0, uapi.Marshal(&in), 0)
	var info uapi.UblksrvCtrlDevInfo
	_ = uapi.Unmarshal(out, &info)
	return res, info
}

func errnoOf(res int32) error {
	if res >= 0 {
		return nil
	}
	return syscall.Errno(-res)
}

func main() {
	mode := flag.String("mode", "root", "root, unpriv or inspect")
	dev := flag.Uint("dev", 0, "device id for -mode inspect")
	keep := flag.Bool("keep", false, "unpriv: leave the device for -mode inspect")
	flag.Parse()

	var u unix.Utsname
	_ = unix.Uname(&u)
	fmt.Printf("kernel %s, uid %d, mode %s\n", unix.ByteSliceToString(u.Release[:]), os.Getuid(), *mode)
	if b, err := os.ReadFile("/sys/module/ublk_drv/parameters/ublks_max"); err == nil {
		fmt.Printf("ublk_drv ublks_max=%s", b)
	}

	c, err := ctrl.NewController()
	if err != nil {
		fmt.Println("FAIL open control device:", err)
		os.Exit(1)
	}
	defer c.Close()
	switch *mode {
	case "root":
		rootChecks(c)
	case "unpriv":
		unprivChecks(c, *keep)
	case "inspect":
		inspectChecks(c, uint32(*dev))
	default:
		fmt.Println("unknown mode")
		os.Exit(2)
	}
	fmt.Printf("RESULT %d failure(s)\n", failures)
	if failures != 0 {
		os.Exit(1)
	}
}

func testParams() *uapi.UblkParams {
	return &uapi.UblkParams{
		Types: uapi.UBLK_PARAM_TYPE_BASIC | uapi.UBLK_PARAM_TYPE_DISCARD | uapi.UBLK_PARAM_TYPE_DMA_ALIGN | uapi.UBLK_PARAM_TYPE_SEGMENT,
		Basic: uapi.UblkParamBasic{Attrs: uapi.UBLK_ATTR_VOLATILE_CACHE, LogicalBSShift: 9, PhysicalBSShift: 12, IOMinShift: 9,
			IOOptShift: 16, MaxSectors: 2048, DevSectors: 1 << 17},
		Discard: uapi.UblkParamDiscard{DiscardGranularity: 4096, MaxDiscardSectors: 1 << 16, MaxWriteZeroesSectors: 1 << 16, MaxDiscardSegments: 1},
		DMA:     uapi.UblkParamDMAAlign{Alignment: 511},
		Seg:     uapi.UblkParamSegment{SegBoundaryMask: 1<<32 - 1, MaxSegmentSize: 1 << 16, MaxSegments: 64},
	}
}

func checkParamsRoundTrip(name string, c *ctrl.Controller, id uint32, p *uapi.UblkParams) {
	if err := c.SetParams(bg(), id, p); err != nil {
		var ue *ctrl.UnsupportedParamsError
		if errors.As(err, &ue) {
			fail(name, "kernel ignored %s", ctrl.ParamTypeNames(ue.Dropped))
		} else {
			fail(name, "SET_PARAMS: %v", err)
		}
		return
	}
	got, err := c.GetParams(bg(), id)
	if err != nil {
		fail(name, "GET_PARAMS: %v", err)
		return
	}
	var bad []string
	if got.Types != p.Types|uapi.UBLK_PARAM_TYPE_DEVT {
		bad = append(bad, fmt.Sprintf("types %s", ctrl.ParamTypeNames(got.Types)))
	}
	if got.Len != uapi.UblkParamsSize {
		bad = append(bad, fmt.Sprintf("len %d", got.Len))
	}
	for _, b := range []struct {
		name      string
		bit       uint32
		got, want any
	}{
		{"basic", uapi.UBLK_PARAM_TYPE_BASIC, got.Basic, p.Basic},
		{"discard", uapi.UBLK_PARAM_TYPE_DISCARD, got.Discard, p.Discard},
		{"zoned", uapi.UBLK_PARAM_TYPE_ZONED, got.Zoned, p.Zoned},
		{"dma", uapi.UBLK_PARAM_TYPE_DMA_ALIGN, got.DMA, p.DMA},
		{"seg", uapi.UBLK_PARAM_TYPE_SEGMENT, got.Seg, p.Seg},
		{"integrity", uapi.UBLK_PARAM_TYPE_INTEGRITY, got.Integrity, p.Integrity},
	} {
		if p.Types&b.bit != 0 && !reflect.DeepEqual(b.got, b.want) {
			bad = append(bad, fmt.Sprintf("%s %+v != %+v", b.name, b.got, b.want))
		}
	}
	if got.Devt.CharMajor == 0 || got.Devt.CharMinor != id {
		bad = append(bad, fmt.Sprintf("devt %+v", got.Devt))
	}
	if len(bad) > 0 {
		fail(name, "%s", strings.Join(bad, "; "))
		return
	}
	pass(name, "types %s round-tripped, devt char %d:%d disk %d:%d", ctrl.ParamTypeNames(p.Types),
		got.Devt.CharMajor, got.Devt.CharMinor, got.Devt.DiskMajor, got.Devt.DiskMinor)
}

func possibleCPUs() []int {
	b, err := os.ReadFile("/sys/devices/system/cpu/possible")
	if err != nil {
		return nil
	}
	var cpus []int
	for _, part := range strings.Split(strings.TrimSpace(string(b)), ",") {
		lo, hi, found := strings.Cut(part, "-")
		a, _ := strconv.Atoi(lo)
		z := a
		if found {
			z, _ = strconv.Atoi(hi)
		}
		for i := a; i <= z; i++ {
			cpus = append(cpus, i)
		}
	}
	return cpus
}

func checkAffinity(name string, c *ctrl.Controller, id uint32, queues uint16) {
	var all []int
	var desc []string
	for q := uint16(0); q < queues; q++ {
		cpus, err := c.GetQueueAffinity(bg(), id, q)
		if err != nil {
			fail(name, "queue %d: %v", q, err)
			return
		}
		all = append(all, cpus...)
		desc = append(desc, fmt.Sprintf("q%d=%v", q, cpus))
	}
	sort.Ints(all)
	if want := possibleCPUs(); !reflect.DeepEqual(all, want) {
		fail(name, "queues cover %v, possible CPUs %v", all, want)
		return
	}
	pass(name, "%s", strings.Join(desc, " "))
	_, err := c.GetQueueAffinity(bg(), id, queues)
	expect(name+" (bad queue)", err, syscall.EINVAL)
}

func waitGone(c *ctrl.Controller, id uint32) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := c.GetDevInfo(bg(), id)
		if errors.Is(err, syscall.ENODEV) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("device %d still present: %v", id, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func rootChecks(c *ctrl.Controller) {
	r := newRaw()

	// --- GET_FEATURES and opcode encoding facts ---
	features, err := c.GetFeatures(bg())
	if !check("GET_FEATURES", err, ctrl.FeatureNames(features)) {
		return
	}
	res, _ := r.submit(uapi.IoctlEncode(3, 'u', uapi.UBLK_CMD_GET_FEATURES, 32), ctrl.AnyDevID, 0, make([]byte, 8), 0)
	expect("kernel: GET_FEATURES as _IOWR", errnoOf(res), syscall.ENODEV)
	res, _ = r.submit(uapi.IoctlEncode(2, 'u', uapi.UBLK_CMD_GET_FEATURES, 16), ctrl.AnyDevID, 0, make([]byte, 8), 0)
	expect("kernel: GET_FEATURES with size 16", errnoOf(res), syscall.ENODEV)

	// --- device A: privileged, no data plane ---
	want := uint64(uapi.UBLK_F_USER_RECOVERY | uapi.UBLK_F_QUIESCE | uapi.UBLK_F_UPDATE_SIZE)
	if features&uapi.UBLK_F_NO_AUTO_PART_SCAN != 0 {
		want |= uapi.UBLK_F_NO_AUTO_PART_SCAN
	}
	const tag = 0x60b1c0ffee
	info, err := c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 2, QueueDepth: 64,
		MaxIOBufBytes: 1<<20 + 123, Flags: want, UblksrvFlags: tag})
	if !check("ADD_DEV", err, "") {
		return
	}
	id := info.DevID
	fmt.Printf("     device %d flags %s max_io %d io_desc_size %d owner %d:%d\n", id, ctrl.FeatureNames(info.Flags),
		info.MaxIOBufBytes, info.IODescSize, info.OwnerUID, info.OwnerGID)
	if info.Flags&want != want || info.Flags&uapi.UBLK_F_CMD_IOCTL_ENCODE == 0 || info.MaxIOBufBytes != 1<<20 {
		fail("ADD_DEV negotiation", "flags %s", ctrl.FeatureNames(info.Flags))
	} else {
		pass("ADD_DEV negotiation", "requested flags kept, CMD_IOCTL_ENCODE set, max_io rounded to page")
	}
	if info.Flags&uapi.UBLK_F_URING_CMD_COMP_IN_TASK != 0 {
		pass("kernel: forces COMP_IN_TASK", "reported although not requested")
	} else {
		fail("kernel: forces COMP_IN_TASK", "not reported")
	}
	for _, get := range []struct {
		name string
		fn   func(context.Context, uint32) (*uapi.UblksrvCtrlDevInfo, error)
	}{{"GET_DEV_INFO", c.GetDevInfo}, {"GET_DEV_INFO2", c.GetDevInfo2}} {
		got, err := get.fn(bg(), id)
		if err != nil {
			fail(get.name, "%v", err)
		} else if got.UblksrvFlags != tag || got.State != uapi.UBLK_S_DEV_DEAD || got.Flags != info.Flags || got.UblksrvPID != -1 {
			fail(get.name, "%+v", got)
		} else {
			pass(get.name, "ublksrv_flags %#x round-tripped, state DEAD, pid -1", got.UblksrvFlags)
		}
	}
	// GET_DEV_INFO sent _IOWR: the driver ignores direction and size bits.
	res, _ = r.submit(uapi.IoctlEncode(3, 'u', uapi.UBLK_CMD_GET_DEV_INFO, 32), id, 0, make([]byte, 64), 0)
	if res == 0 {
		pass("kernel: GET_DEV_INFO as _IOWR", "accepted (only _IOC_TYPE/_IOC_NR matter)")
	} else {
		fail("kernel: GET_DEV_INFO as _IOWR", "%v", errnoOf(res))
	}
	res, _ = r.submit(uapi.UBLK_U_CMD_GET_DEV_INFO, id, 0, make([]byte, 63), 0)
	expect("kernel: GET_DEV_INFO len 63", errnoOf(res), syscall.EINVAL)

	checkParamsRoundTrip("SET_PARAMS/GET_PARAMS", c, id, testParams())
	bad := testParams()
	bad.DMA.Alignment = 4096
	if err := c.SetParams(bg(), id, bad); errors.Is(err, syscall.EINVAL) {
		pass("SET_PARAMS validation", "dma alignment 4096 rejected EINVAL")
	} else {
		fail("SET_PARAMS validation", "%v", err)
	}
	integ := testParams()
	integ.Types |= uapi.UBLK_PARAM_TYPE_INTEGRITY
	integ.Integrity = uapi.UblkParamIntegrity{MetadataSize: 8, IntervalExp: 9, CsumType: uapi.LBMD_PI_CSUM_CRC16_T10DIF}
	expect("SET_PARAMS INTEGRITY w/o F_INTEGRITY", c.SetParams(bg(), id, integ), syscall.EINVAL)
	checkParamsRoundTrip("SET_PARAMS again", c, id, testParams())

	checkAffinity("GET_QUEUE_AFFINITY", c, id, info.NrHwQueues)

	expect("UPDATE_SIZE before START_DEV", c.UpdateSize(bg(), id, 1<<16), ctrl.ErrNotStarted)
	res, _ = r.submit(uapi.UBLK_U_CMD_UPDATE_SIZE, id, 0, nil, 0)
	expect("kernel: UPDATE_SIZE unstarted (raw)", errnoOf(res), syscall.ENODEV)
	expect("TRY_STOP_DEV before START_DEV", c.TryStopDev(bg(), id), syscall.ENODEV)
	expect("QUIESCE_DEV before START_DEV", c.QuiesceDev(bg(), id, time.Second), syscall.ENODEV)
	expect("START_USER_RECOVERY on DEAD", c.StartUserRecovery(bg(), id), syscall.EBUSY)
	expect("START_DEV pid 0", c.StartDev(bg(), id, 0), syscall.EINVAL)

	check("DEL_DEV_ASYNC", c.DelDevAsync(bg(), id), "")
	check("DEL_DEV_ASYNC: device gone", waitGone(c, id), "GET_DEV_INFO -> ENODEV")

	// --- feature negotiation and the rules, client side and kernel side ---
	for _, f := range []uint64{uapi.UBLK_F_SHMEM_ZC, uapi.UBLK_F_IO_DESC_SIZE, uapi.UBLK_F_INTEGRITY, uapi.UBLK_F_ZONED} {
		if features&f != 0 {
			continue
		}
		_, err := c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20,
			Flags: f | uapi.UBLK_F_USER_COPY, IODescSize: map[bool]uint16{true: 32}[f == uapi.UBLK_F_IO_DESC_SIZE]})
		var me *ctrl.MissingFeaturesError
		if errors.As(err, &me) {
			pass("negotiate missing "+ctrl.FeatureNames(f), "%v", err)
		} else {
			fail("negotiate missing "+ctrl.FeatureNames(f), "%v", err)
		}
	}
	for _, rule := range []struct {
		name  string
		flags uint64
		errno syscall.Errno // 0: accepted, check cleared instead
		clear uint64
	}{
		{"QUIESCE w/o USER_RECOVERY", uapi.UBLK_F_QUIESCE, syscall.EINVAL, 0},
		{"REISSUE w/o USER_RECOVERY", uapi.UBLK_F_USER_RECOVERY_REISSUE, syscall.EINVAL, 0},
		{"FAIL_IO w/o USER_RECOVERY", uapi.UBLK_F_USER_RECOVERY_FAIL_IO, syscall.EINVAL, 0},
		{"REISSUE|FAIL_IO", uapi.UBLK_F_USER_RECOVERY | uapi.UBLK_F_USER_RECOVERY_REISSUE | uapi.UBLK_F_USER_RECOVERY_FAIL_IO, syscall.EINVAL, 0},
		{"INTEGRITY w/o USER_COPY", uapi.UBLK_F_INTEGRITY, syscall.EINVAL, 0},
		{"ZONED w/o USER_COPY", uapi.UBLK_F_ZONED, syscall.EINVAL, 0},
		{"NEED_GET_DATA|USER_COPY", uapi.UBLK_F_NEED_GET_DATA | uapi.UBLK_F_USER_COPY, 0, uapi.UBLK_F_NEED_GET_DATA},
		{"NEED_GET_DATA|BATCH_IO", uapi.UBLK_F_NEED_GET_DATA | uapi.UBLK_F_BATCH_IO, 0, uapi.UBLK_F_NEED_GET_DATA},
		{"PER_IO_DAEMON|BATCH_IO", uapi.UBLK_F_PER_IO_DAEMON | uapi.UBLK_F_BATCH_IO, 0, uapi.UBLK_F_PER_IO_DAEMON},
		{"UNPRIVILEGED_DEV as root", uapi.UBLK_F_UNPRIVILEGED_DEV, 0, uapi.UBLK_F_UNPRIVILEGED_DEV},
		{"IO_DESC_SIZE size 0", uapi.UBLK_F_IO_DESC_SIZE, syscall.EINVAL, 0},
	} {
		unknown := rule.flags &^ features
		res, out := r.addDev(rule.flags)
		switch {
		case unknown != 0:
			skip("kernel rule: "+rule.name, "kernel predates %s (ADD_DEV: %v, flags back %s)",
				ctrl.FeatureNames(unknown), errnoOf(res), ctrl.FeatureNames(out.Flags))
		case rule.errno != 0:
			if errnoOf(res) == rule.errno {
				pass("kernel rule: "+rule.name, "rejected %v", rule.errno)
			} else {
				fail("kernel rule: "+rule.name, "got %v", errnoOf(res))
			}
		case res != 0:
			fail("kernel rule: "+rule.name, "ADD_DEV failed %v", errnoOf(res))
		case out.Flags&rule.clear != 0:
			fail("kernel rule: "+rule.name, "kept %s", ctrl.FeatureNames(rule.clear))
		default:
			pass("kernel rule: "+rule.name, "accepted, %s cleared", ctrl.FeatureNames(rule.clear))
		}
		if res == 0 {
			_ = c.DelDev(bg(), out.DevID)
		}
		if err := ctrl.ValidateFeatures(rule.flags, 0); err == nil && rule.flags != uapi.UBLK_F_UNPRIVILEGED_DEV {
			fail("client rule: "+rule.name, "ValidateFeatures accepted it")
		}
	}

	// --- device B: INTEGRITY params ---
	if features&uapi.UBLK_F_INTEGRITY != 0 {
		info, err := c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20,
			Flags: uapi.UBLK_F_INTEGRITY | uapi.UBLK_F_USER_COPY})
		if check("ADD_DEV INTEGRITY|USER_COPY", err, "") {
			p := testParams()
			p.Types |= uapi.UBLK_PARAM_TYPE_INTEGRITY
			p.Integrity = uapi.UblkParamIntegrity{Flags: uapi.LBMD_PI_CAP_INTEGRITY | uapi.LBMD_PI_CAP_REFTAG,
				MaxIntegritySegments: 4, IntervalExp: 9, MetadataSize: 8, PIOffset: 0, CsumType: uapi.LBMD_PI_CSUM_CRC16_T10DIF, TagSize: 2}
			checkParamsRoundTrip("SET_PARAMS INTEGRITY", c, info.DevID, p)
			check("DEL_DEV", c.DelDev(bg(), info.DevID), "")
		}
	} else {
		skip("SET_PARAMS INTEGRITY", "kernel lacks UBLK_F_INTEGRITY (or CONFIG_BLK_DEV_INTEGRITY)")
	}

	// --- device C: ZONED params ---
	if features&uapi.UBLK_F_ZONED != 0 {
		info, err := c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20,
			Flags: uapi.UBLK_F_ZONED | uapi.UBLK_F_USER_COPY})
		if errors.Is(err, syscall.EINVAL) {
			skip("SET_PARAMS ZONED", "ADD_DEV ZONED EINVAL: CONFIG_BLK_DEV_ZONED off")
		} else if check("ADD_DEV ZONED|USER_COPY", err, "") {
			p := testParams()
			p.Types = uapi.UBLK_PARAM_TYPE_BASIC | uapi.UBLK_PARAM_TYPE_ZONED
			p.Basic.ChunkSectors = 1 << 11
			p.Zoned = uapi.UblkParamZoned{MaxOpenZones: 8, MaxActiveZones: 8, MaxZoneAppendSectors: 1 << 10}
			checkParamsRoundTrip("SET_PARAMS ZONED", c, info.DevID, p)
			check("DEL_DEV", c.DelDev(bg(), info.DevID), "")
		}
	}

	// --- device D: shared-memory zero copy ---
	if features&uapi.UBLK_F_SHMEM_ZC != 0 {
		info, err := c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20,
			Flags: uapi.UBLK_F_SHMEM_ZC})
		if check("ADD_DEV SHMEM_ZC", err, "") {
			mem, _ := unix.Mmap(-1, 0, 1<<20, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_ANONYMOUS)
			idx, err := c.RegBuf(bg(), info.DevID, uintptr(unsafe.Pointer(&mem[0])), 1<<20, 0)
			if check("REG_BUF", err, fmt.Sprintf("index %d", idx)) {
				check("UNREG_BUF", c.UnregBuf(bg(), info.DevID, idx), "")
				expect("UNREG_BUF twice", c.UnregBuf(bg(), info.DevID, idx), syscall.ENOENT)
			}
			check("DEL_DEV", c.DelDev(bg(), info.DevID), "")
		}
	} else {
		info, err := c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20})
		if check("ADD_DEV (for REG_BUF)", err, "") {
			_, err := c.RegBuf(bg(), info.DevID, 0, 4096, 0)
			if ctrl.IsUnsupported(err) {
				pass("REG_BUF without SHMEM_ZC kernel", "unsupported: %v", err)
			} else {
				fail("REG_BUF without SHMEM_ZC kernel", "%v", err)
			}
			if err := c.UnregBuf(bg(), info.DevID, 0); ctrl.IsUnsupported(err) {
				pass("UNREG_BUF without SHMEM_ZC kernel", "unsupported: %v", err)
			} else {
				fail("UNREG_BUF without SHMEM_ZC kernel", "%v", err)
			}
			check("DEL_DEV", c.DelDev(bg(), info.DevID), "")
		}
	}

	liveChecks(c, features)

	// END_USER_RECOVERY waits for every queue to FETCH; with no data plane it
	// waits forever. The context abandons it; the controller keeps its ring
	// until the kernel lets go. Only an io_uring cancel (ring teardown) ends
	// that kernel wait, so whether the device can then be deleted depends on
	// the ring implementation tearing the ring down on Close. Last, because
	// the device may stay behind until this process exits.
	abandonedEndRecovery(c)
}

func abandonedEndRecovery(c *ctrl.Controller) {
	info, err := c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 1, QueueDepth: 8,
		MaxIOBufBytes: 1 << 20, Flags: uapi.UBLK_F_USER_RECOVERY})
	if !check("ADD_DEV (abandon test)", err, "") {
		return
	}
	id := info.DevID
	ctx, cancel := timeout(time.Second)
	err = c.EndUserRecovery(ctx, id, os.Getpid())
	cancel()
	var ife *ctrl.InFlightError
	if !errors.As(err, &ife) || !errors.Is(err, context.DeadlineExceeded) {
		fail("END_USER_RECOVERY abandoned", "%v", err)
		return
	}
	pass("END_USER_RECOVERY abandoned", "%v", err)
	select {
	case <-ife.Done():
		pass("END_USER_RECOVERY late completion", "result %v", ife.Result())
	case <-time.After(30 * time.Second):
		fail("END_USER_RECOVERY late completion", "still in the kernel after 30s")
	}
	check("DEL_DEV_ASYNC (abandon test)", c.DelDevAsync(bg(), id), "")
	if err := waitGone(c, id); err != nil {
		skip("abandoned device released", "%v: the kernel wait still holds a reference (ring not torn down)", err)
	} else {
		pass("abandoned device released", "the kernel wait was cancelled (ASYNC_CANCEL or ring teardown)")
	}
}

// memBackend is a RAM disk for the live-device checks.
type memBackend struct{ data []byte }

func (m *memBackend) ReadAt(p []byte, off int64) (int, error)  { return copy(p, m.data[off:]), nil }
func (m *memBackend) WriteAt(p []byte, off int64) (int, error) { return copy(m.data[off:], p), nil }
func (m *memBackend) Size() int64                              { return int64(len(m.data)) }
func (m *memBackend) Close() error                             { return nil }
func (m *memBackend) Flush() error                             { return nil }

type plane struct {
	fd      int
	runners []*queue.Runner
	cancel  context.CancelFunc
}

// startPlane brings up the existing queue engine for id: open /dev/ublkcN,
// one runner per queue, FETCH_REQs submitted.
func startPlane(id uint32, queues uint16, depth int, be *memBackend) (*plane, error) {
	path := uapi.UblkDevicePath(id)
	fd := -1
	var err error
	for i := 0; i < constants.CharDeviceOpenRetries; i++ {
		if fd, err = syscall.Open(path, syscall.O_RDWR, 0); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if fd < 0 {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &plane{fd: fd, cancel: cancel}
	for q := uint16(0); q < queues; q++ {
		r, err := queue.NewRunner(ctx, queue.Config{DevID: id, QueueID: q, Depth: depth, MaxIOSize: 1 << 20, Backend: be, CharFd: fd})
		if err != nil {
			p.close()
			return nil, err
		}
		p.runners = append(p.runners, r)
		if err := r.Start(); err != nil {
			p.close()
			return nil, err
		}
	}
	time.Sleep(constants.QueueInitDelay)
	return p, nil
}

func (p *plane) close() {
	p.cancel()
	for _, r := range p.runners {
		r.Close()
	}
	if p.fd >= 0 {
		syscall.Close(p.fd)
		p.fd = -1
	}
}

func sysfsSectors(id uint32) (uint64, error) {
	b, err := os.ReadFile(fmt.Sprintf("/sys/block/ublkb%d/size", id))
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}

func waitState(c *ctrl.Controller, id uint32, state uint16, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		info, err := c.GetDevInfo(bg(), id)
		if err == nil && info.State == state {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("state %d, want %d", info.State, state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// liveChecks covers the commands that need a started device, using the
// existing queue engine as the data plane.
func liveChecks(c *ctrl.Controller, features uint64) {
	const queues, depth = 2, 32
	be := &memBackend{data: make([]byte, 64<<20)}
	flags := uint64(uapi.UBLK_F_USER_RECOVERY | uapi.UBLK_F_QUIESCE)
	if features&uapi.UBLK_F_NO_AUTO_PART_SCAN != 0 {
		flags |= uapi.UBLK_F_NO_AUTO_PART_SCAN
	}
	info, err := c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: queues, QueueDepth: depth,
		MaxIOBufBytes: 1 << 20, Flags: flags})
	if !check("live: ADD_DEV USER_RECOVERY|QUIESCE", err, ctrl.FeatureNames(flags)) {
		return
	}
	id := info.DevID
	var p *plane
	defer func() {
		_ = c.StopDev(bg(), id)
		if p != nil {
			p.close()
		}
		_ = c.DelDevAsync(bg(), id)
	}()
	params := &uapi.UblkParams{Types: uapi.UBLK_PARAM_TYPE_BASIC, Basic: uapi.UblkParamBasic{LogicalBSShift: 9,
		PhysicalBSShift: 12, IOMinShift: 9, MaxSectors: 2048, DevSectors: uint64(be.Size() / 512)}}
	if !check("live: SET_PARAMS", c.SetParams(bg(), id, params), "") {
		return
	}
	if p, err = startPlane(id, queues, depth, be); !check("live: queue engine up", err, "") {
		return
	}
	if !check("live: START_DEV", c.StartDev(bg(), id, os.Getpid()), "") {
		return
	}
	got, err := c.GetDevInfo(bg(), id)
	if err == nil && got.State == uapi.UBLK_S_DEV_LIVE && got.UblksrvPID == int32(os.Getpid()) {
		pass("live: GET_DEV_INFO", "LIVE, ublksrv_pid %d", got.UblksrvPID)
	} else {
		fail("live: GET_DEV_INFO", "%+v %v", got, err)
	}
	if gp, err := c.GetParams(bg(), id); err == nil && gp.Devt.DiskMajor != 0 {
		pass("live: GET_PARAMS devt", "disk %d:%d", gp.Devt.DiskMajor, gp.Devt.DiskMinor)
	} else {
		fail("live: GET_PARAMS devt", "%+v %v", gp, err)
	}
	checkAffinity("live: GET_QUEUE_AFFINITY", c, id, queues)

	before, _ := sysfsSectors(id)
	err = c.UpdateSize(bg(), id, before/2)
	after, _ := sysfsSectors(id)
	if err == nil && after == before/2 {
		pass("live: UPDATE_SIZE", "/sys/block size %d -> %d sectors", before, after)
	} else {
		fail("live: UPDATE_SIZE", "err %v, size %d -> %d", err, before, after)
	}
	if err := c.UpdateSize(bg(), id, before); err != nil {
		fail("live: UPDATE_SIZE restore", "%v", err)
	}

	blk, err := os.OpenFile(uapi.UblkBlockDevicePath(id), os.O_RDWR, 0)
	if err == nil {
		expect("live: TRY_STOP_DEV while open", c.TryStopDev(bg(), id), syscall.EBUSY)
		buf := make([]byte, 4096)
		copy(buf, "go-ublk ctrl")
		if _, err := blk.WriteAt(buf, 0); err == nil {
			_ = blk.Sync()
		}
		blk.Close()
	} else {
		fail("live: open block device", "%v", err)
	}

	// QUIESCE -> server exit -> QUIESCED -> START_USER_RECOVERY -> new
	// server -> END_USER_RECOVERY -> LIVE, with the existing queue engine.
	ctx, cancel := timeout(15 * time.Second)
	err = c.QuiesceDev(ctx, id, 5*time.Second)
	cancel()
	if check("live: QUIESCE_DEV", err, "") {
		p.close()
		p = nil
		if check("live: QUIESCED after server exit", waitState(c, id, uapi.UBLK_S_DEV_QUIESCED, 10*time.Second), "") &&
			check("live: START_USER_RECOVERY", c.StartUserRecovery(bg(), id), "") {
			if p, err = startPlane(id, queues, depth, be); check("live: new queue engine", err, "") {
				ctx, cancel := timeout(15 * time.Second)
				err = c.EndUserRecovery(ctx, id, os.Getpid())
				cancel()
				if check("live: END_USER_RECOVERY", err, "") {
					check("live: LIVE again", waitState(c, id, uapi.UBLK_S_DEV_LIVE, 5*time.Second), "")
					// O_DIRECT, so the read reaches the new server rather
					// than the page cache.
					if fd, err := unix.Open(uapi.UblkBlockDevicePath(id), unix.O_RDONLY|unix.O_DIRECT, 0); err == nil {
						buf, _ := unix.Mmap(-1, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
						n, err := unix.Pread(fd, buf, 0)
						unix.Close(fd)
						if err == nil && n == 4096 && string(buf[:12]) == "go-ublk ctrl" {
							pass("live: I/O after recovery", "O_DIRECT read returns data written before quiesce")
						} else {
							fail("live: I/O after recovery", "%v %q", err, buf[:12])
						}
						_ = unix.Munmap(buf)
					} else {
						fail("live: I/O after recovery", "open: %v", err)
					}
				}
			}
		}
	}

	// udev may still be probing the disk after the recovery uevent; EBUSY
	// then is TRY_STOP_DEV doing its job, so retry briefly.
	busy := 0
	for err = c.TryStopDev(bg(), id); errors.Is(err, syscall.EBUSY) && busy < 50; err = c.TryStopDev(bg(), id) {
		busy++
		time.Sleep(100 * time.Millisecond)
	}
	if check("live: TRY_STOP_DEV", err, fmt.Sprintf("after %d EBUSY retries", busy)) {
		check("live: DEAD after TRY_STOP_DEV", waitState(c, id, uapi.UBLK_S_DEV_DEAD, 5*time.Second), "")
	}
	if p != nil {
		p.close()
		p = nil
	}
	ctx, cancel = timeout(30 * time.Second)
	err = c.DelDev(ctx, id)
	cancel()
	check("live: DEL_DEV", err, "")
}

func unprivChecks(c *ctrl.Controller, keep bool) {
	r := newRaw()
	features, err := c.GetFeatures(bg())
	check("unpriv: GET_FEATURES", err, ctrl.FeatureNames(features))

	_, err = c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20})
	expect("unpriv: ADD_DEV w/o UNPRIVILEGED_DEV", err, syscall.EPERM)
	var ce *ctrl.FeatureConflictError
	_, err = c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 1, QueueDepth: 8, MaxIOBufBytes: 1 << 20,
		Flags: uapi.UBLK_F_UNPRIVILEGED_DEV | uapi.UBLK_F_USER_COPY})
	if errors.As(err, &ce) {
		pass("unpriv: client rejects USER_COPY", "%v", err)
	} else {
		fail("unpriv: client rejects USER_COPY", "%v", err)
	}
	for _, f := range []uint64{uapi.UBLK_F_USER_COPY, uapi.UBLK_F_SUPPORT_ZERO_COPY, uapi.UBLK_F_AUTO_BUF_REG} {
		res, _ := r.addDev(uapi.UBLK_F_UNPRIVILEGED_DEV | f)
		expect("unpriv: kernel rejects "+ctrl.FeatureNames(f), errnoOf(res), syscall.EINVAL)
	}
	res, out := r.addDev(uapi.UBLK_F_UNPRIVILEGED_DEV | uapi.UBLK_F_USER_RECOVERY)
	if res == 0 && out.Flags&uapi.UBLK_F_USER_RECOVERY == 0 {
		pass("unpriv: kernel clears USER_RECOVERY", "flags %s", ctrl.FeatureNames(out.Flags))
	} else {
		fail("unpriv: kernel clears USER_RECOVERY", "res %d flags %s", res, ctrl.FeatureNames(out.Flags))
	}
	if res == 0 {
		waitOwned(out.DevID)
		check("unpriv: DEL_DEV (rule probe device)", c.DelDev(bg(), out.DevID), "")
	}

	info, err := c.AddDev(bg(), ctrl.AddDevOptions{DevID: ctrl.AnyDevID, NrHwQueues: 2, QueueDepth: 16, MaxIOBufBytes: 1 << 20,
		Flags: uapi.UBLK_F_UNPRIVILEGED_DEV, UblksrvFlags: 0x5eed})
	if !check("unpriv: ADD_DEV UNPRIVILEGED_DEV", err, "") {
		return
	}
	id := info.DevID
	if info.Flags&uapi.UBLK_F_UNPRIVILEGED_DEV != 0 && info.OwnerUID == uint32(os.Getuid()) {
		pass("unpriv: owner", "uid %d gid %d", info.OwnerUID, info.OwnerGID)
	} else {
		fail("unpriv: owner", "%+v", info)
	}
	path := uapi.UblkDevicePath(id)
	if err := waitOwned(id); err != nil {
		fail("unpriv: udev chowns "+path, "%v", err)
	}
	// Raw: without a path the driver refuses; with it, it answers.
	res, _ = r.submit(uapi.UBLK_U_CMD_GET_DEV_INFO, id, 0, make([]byte, 64), 0)
	expect("unpriv: kernel GET_DEV_INFO w/o path", errnoOf(res), syscall.EINVAL)
	buf := make([]byte, 16+64)
	copy(buf, path)
	res, outb := r.submit(uapi.UBLK_U_CMD_GET_DEV_INFO, id, 0, buf, 16)
	if res == 0 && binary.LittleEndian.Uint32(outb[16+12:16+16]) == id {
		pass("unpriv: kernel GET_DEV_INFO with path", "payload after the path")
	} else {
		fail("unpriv: kernel GET_DEV_INFO with path", "res %d", res)
	}
	for _, get := range []struct {
		name string
		fn   func(context.Context, uint32) (*uapi.UblksrvCtrlDevInfo, error)
	}{{"unpriv: GET_DEV_INFO", c.GetDevInfo}, {"unpriv: GET_DEV_INFO2", c.GetDevInfo2}} {
		got, err := get.fn(bg(), id)
		if err == nil && got.UblksrvFlags == 0x5eed && got.DevID == id {
			pass(get.name, "via dev path")
		} else {
			fail(get.name, "%+v %v", got, err)
		}
	}
	checkParamsRoundTrip("unpriv: SET_PARAMS/GET_PARAMS", c, id, testParams())
	checkAffinity("unpriv: GET_QUEUE_AFFINITY", c, id, info.NrHwQueues)
	expect("unpriv: UPDATE_SIZE before START_DEV", c.UpdateSize(bg(), id, 8), ctrl.ErrNotStarted)
	expect("unpriv: TRY_STOP_DEV before START_DEV", c.TryStopDev(bg(), id), syscall.ENODEV)
	expect("unpriv: QUIESCE_DEV w/o F_QUIESCE", c.QuiesceDev(bg(), id, time.Second), syscall.EOPNOTSUPP)
	expect("unpriv: START_USER_RECOVERY w/o recovery", c.StartUserRecovery(bg(), id), syscall.EINVAL)
	check("unpriv: STOP_DEV", c.StopDev(bg(), id), "")
	if keep {
		fmt.Printf("KEPT %d\n", id)
		return
	}
	check("unpriv: DEL_DEV", c.DelDev(bg(), id), "")
}

// waitOwned waits until udev has handed /dev/ublkcN to this user: the driver
// checks the caller's access to that inode for every unprivileged command,
// and devtmpfs creates it root-owned before udev applies the rule.
func waitOwned(id uint32) error {
	path := uapi.UblkDevicePath(id)
	var st syscall.Stat_t
	for i := 0; i < 100; i++ {
		if syscall.Stat(path, &st) == nil && st.Uid == uint32(os.Getuid()) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s not owned by uid %d after 5s", path, os.Getuid())
}

func inspectChecks(c *ctrl.Controller, id uint32) {
	info, err := c.GetDevInfo(bg(), id)
	if !check("inspect: GET_DEV_INFO unprivileged dev as root", err, "") {
		return
	}
	if info.Flags&uapi.UBLK_F_UNPRIVILEGED_DEV == 0 {
		fail("inspect: device is unprivileged", "flags %s", ctrl.FeatureNames(info.Flags))
	}
	if p, err := c.GetParams(bg(), id); err == nil && p.HasBasic() {
		pass("inspect: GET_PARAMS", "types %s", ctrl.ParamTypeNames(p.Types))
	} else {
		fail("inspect: GET_PARAMS", "%v", err)
	}
	check("inspect: DEL_DEV", c.DelDev(bg(), id), "")
}
