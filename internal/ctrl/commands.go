package ctrl

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"syscall"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// Every method below sends exactly one ublk control command (plus, for
// per-device commands, the GET_DEV_INFO probe described at devRequest) and
// returns *Error wrapping the kernel's errno on failure. Version notes are
// the first upstream kernel that implements the command. Common errors:
//
//	ENODEV      no such device (also: every command on v6.0-v6.3, which
//	            predate ioctl-encoded opcodes)
//	EPERM       privileged device and the caller lacks CAP_SYS_ADMIN
//	EACCES/ENOENT unprivileged device whose /dev/ublkcN the caller cannot
//	            open or cannot see
//	EOPNOTSUPP, ENOTSUPP  command unknown to the running kernel; see
//	            IsUnsupported
//
// Commands that can block in the kernel (DEL_DEV, STOP_DEV, START_DEV,
// END_USER_RECOVERY, QUIESCE_DEV) honor ctx by returning *InFlightError;
// see exec.

// devRequest prepares a per-device request. Unprivileged devices
// (UBLK_F_UNPRIVILEGED_DEV) require every command to carry the char-device
// path (ublk_ctrl_uring_cmd_permission); privileged devices must not get one,
// or the driver reads the path as the payload. Whether a device is
// unprivileged is probed on every call with GET_DEV_INFO without a path —
// success means privileged, EINVAL (missing dev_path_len) means
// unprivileged — rather than cached, because device numbers are reused.
func (c *Controller) devRequest(ctx context.Context, req request) (request, error) {
	unpriv, err := c.isUnprivileged(ctx, req.devID)
	if err != nil {
		var ce *Error
		if errors.As(err, &ce) {
			err = ce.Err
		}
		return req, &Error{Op: req.name, DevID: req.devID, Err: err}
	}
	if unpriv {
		req.devPath = uapi.UblkDevicePath(req.devID)
	}
	return req, nil
}

func (c *Controller) isUnprivileged(ctx context.Context, devID uint32) (bool, error) {
	_, err := c.getDevInfo(ctx, devID, false)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, syscall.EINVAL):
		return true, nil
	}
	return false, err
}

func (c *Controller) devExec(ctx context.Context, req request) (int32, error) {
	req, err := c.devRequest(ctx, req)
	if err != nil {
		return 0, err
	}
	return c.exec(ctx, req)
}

// AddDevOptions are the ADD_DEV inputs; the kernel ignores the other
// ublksrv_ctrl_dev_info fields and fills them in itself.
type AddDevOptions struct {
	DevID         uint32 // device number, or AnyDevID
	NrHwQueues    uint16 // 1..UBLK_MAX_NR_QUEUES; the kernel clamps to nr_cpu_ids
	QueueDepth    uint16 // 1..UBLK_MAX_QUEUE_DEPTH
	MaxIOBufBytes uint32 // rounded down to PAGE_SIZE by the kernel
	Flags         uint64 // requested UBLK_F_* features
	UblksrvFlags  uint64 // opaque to the kernel; stored and returned by GET_DEV_INFO
	IODescSize    uint16 // with UBLK_F_IO_DESC_SIZE only: 24..256, multiple of 8
}

// AddDev sends ADD_DEV (v6.0): ctrl_cmd.addr points at a 64-byte
// ublksrv_ctrl_dev_info that the kernel reads and overwrites with the result;
// queue_id must be 0xFFFF and dev_id must equal info.dev_id.
//
// Before sending, Flags are checked against the dependency rules
// (ValidateFeatures, *FeatureConflictError) and against GET_FEATURES when the
// kernel has it (*MissingFeaturesError). UBLK_F_CMD_IOCTL_ENCODE is always
// added. After the kernel answers, any requested flag it cleared is reported
// as *MissingFeaturesError and the new device is deleted again; the one
// exception is UBLK_F_UNPRIVILEGED_DEV, which the kernel drops for a
// CAP_SYS_ADMIN caller (the device is then simply privileged).
//
// Kernel errors: EINVAL (bad depth/queues/flags combination, dev_id >=
// 1<<20, CONFIG_BLK_DEV_ZONED missing for ZONED), EPERM (unprivileged caller
// without UBLK_F_UNPRIVILEGED_DEV), EACCES (ublks_max unprivileged devices
// already exist), EEXIST (dev_id taken), ENOMEM.
func (c *Controller) AddDev(ctx context.Context, opts AddDevOptions) (*uapi.UblksrvCtrlDevInfo, error) {
	fail := func(err error) (*uapi.UblksrvCtrlDevInfo, error) {
		return nil, &Error{Op: "ADD_DEV", DevID: opts.DevID, Err: err}
	}
	if opts.NrHwQueues == 0 || opts.NrHwQueues > uapi.UBLK_MAX_NR_QUEUES ||
		opts.QueueDepth == 0 || opts.QueueDepth > uapi.UBLK_MAX_QUEUE_DEPTH {
		return fail(fmt.Errorf("%d queues of depth %d: %w", opts.NrHwQueues, opts.QueueDepth, syscall.EINVAL))
	}
	if err := ValidateFeatures(opts.Flags, opts.IODescSize); err != nil {
		return fail(err)
	}
	fs, err := c.Features(ctx)
	if err != nil {
		return fail(err)
	}
	send, err := Negotiate(opts.Flags, fs)
	if err != nil {
		return fail(err)
	}

	in := uapi.UblksrvCtrlDevInfo{
		NrHwQueues:    opts.NrHwQueues,
		QueueDepth:    opts.QueueDepth,
		IODescSize:    opts.IODescSize,
		MaxIOBufBytes: opts.MaxIOBufBytes,
		DevID:         opts.DevID,
		UblksrvPID:    -1,
		Flags:         send,
		UblksrvFlags:  opts.UblksrvFlags,
	}
	var out uapi.UblksrvCtrlDevInfo
	_, err = c.exec(ctx, request{
		name:    "ADD_DEV",
		op:      uapi.UBLK_U_CMD_ADD_DEV,
		devID:   opts.DevID,
		payload: 64,
		fill:    func(p []byte) { _, _ = uapi.MarshalInto(&in, p) },
		read:    func(p []byte, _ int32) error { return uapi.Unmarshal(p, &out) },
		late: func(p []byte, _ int32) {
			var orphan uapi.UblksrvCtrlDevInfo
			if uapi.Unmarshal(p, &orphan) == nil {
				go c.deleteOrphan(orphan.DevID)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	if missing := opts.Flags &^ out.Flags &^ uapi.UBLK_F_UNPRIVILEGED_DEV; missing != 0 {
		c.deleteOrphan(out.DevID)
		return fail(&MissingFeaturesError{Requested: opts.Flags, Missing: missing, Supported: out.Flags, Known: true, Source: "ADD_DEV"})
	}
	return &out, nil
}

// deleteOrphan removes a device nobody will use: one ADD_DEV created with
// features cleared, or one whose ADD_DEV completed after its caller gave up.
func (c *Controller) deleteOrphan(id uint32) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.DelDev(ctx, id); err != nil {
		c.logger.Warn("failed to delete orphaned ublk device", "dev_id", id, "error", err)
	}
}

// DelDev sends DEL_DEV (v6.0): no buffer. It removes the device and then
// waits until its number is free, i.e. until every reference is gone — an
// open /dev/ublkcN or /dev/ublkbN keeps it waiting. Bound it with ctx.
// Errors: EINTR (signal while waiting).
func (c *Controller) DelDev(ctx context.Context, id uint32) error {
	_, err := c.devExec(ctx, request{name: "DEL_DEV", op: uapi.UBLK_U_CMD_DEL_DEV, devID: id})
	return err
}

// DelDevAsync sends DEL_DEV_ASYNC (v6.11; the v6.9 and v6.10 drivers mis-dispatch it
// and returns ENOTSUPP): DEL_DEV without the wait for the device number to be
// freed. The device is gone from the control plane when it returns, but its
// number may not be reusable yet.
func (c *Controller) DelDevAsync(ctx context.Context, id uint32) error {
	_, err := c.devExec(ctx, request{name: "DEL_DEV_ASYNC", op: uapi.UBLK_U_CMD_DEL_DEV_ASYNC, devID: id})
	return err
}

// StartDev sends START_DEV (v6.0): data[0] = the ublk server's pid, which
// must be the thread group that issued the queues' FETCH_REQs (the kernel
// compares it in its pid namespace). It waits until every queue has fetched,
// applies the parameters and adds /dev/ublkbN.
// Errors: EINVAL (pid mismatch, no BASIC params), EEXIST (already started),
// EINTR, EOPNOTSUPP (ZONED without CONFIG_BLK_DEV_ZONED).
func (c *Controller) StartDev(ctx context.Context, id uint32, pid int) error {
	_, err := c.devExec(ctx, request{name: "START_DEV", op: uapi.UBLK_U_CMD_START_DEV, devID: id, data: uint64(pid)})
	return err
}

// StopDev sends STOP_DEV (v6.0): no buffer, always succeeds once dispatched.
// It deletes the gendisk, which flushes and waits for outstanding I/O through
// the server, so it can block for as long as the server takes. Bound it with
// ctx.
func (c *Controller) StopDev(ctx context.Context, id uint32) error {
	_, err := c.devExec(ctx, request{name: "STOP_DEV", op: uapi.UBLK_U_CMD_STOP_DEV, devID: id})
	return err
}

// TryStopDev sends TRY_STOP_DEV (v7.0, UBLK_F_SAFE_STOP_DEV): STOP_DEV only if
// /dev/ublkbN has no openers, and new opens fail with ENXIO from then on.
// Errors: EBUSY (opened), ENODEV (not started).
func (c *Controller) TryStopDev(ctx context.Context, id uint32) error {
	_, err := c.devExec(ctx, request{name: "TRY_STOP_DEV", op: uapi.UBLK_U_CMD_TRY_STOP_DEV, devID: id})
	return err
}

// SetParams sends SET_PARAMS (v6.0): ctrl_cmd.addr points at struct
// ublk_params with len = UblkParamsSize; blocks sit at fixed offsets and only
// those in p.Types are read. DEVT is read-only and BASIC is mandatory, so
// both are checked here. Allowed only before START_DEV.
//
// A kernel older than a param type masks it off silently (types &=
// UBLK_PARAM_TYPE_ALL), so SetParams reads the parameters back and returns
// *UnsupportedParamsError naming any type that did not stick; the rest are
// applied. Kernel errors: EINVAL (validation, see ublk_validate_params;
// INTEGRITY without UBLK_F_INTEGRITY), EACCES (device already started).
func (c *Controller) SetParams(ctx context.Context, id uint32, p *uapi.UblkParams) error {
	if p.Types&uapi.UBLK_PARAM_TYPE_DEVT != 0 {
		return &Error{Op: "SET_PARAMS", DevID: id, Err: fmt.Errorf("UBLK_PARAM_TYPE_DEVT is read-only: %w", syscall.EINVAL)}
	}
	if p.Types&uapi.UBLK_PARAM_TYPE_BASIC == 0 {
		return &Error{Op: "SET_PARAMS", DevID: id, Err: fmt.Errorf("UBLK_PARAM_TYPE_BASIC is required: %w", syscall.EINVAL)}
	}
	_, err := c.devExec(ctx, request{
		name:    "SET_PARAMS",
		op:      uapi.UBLK_U_CMD_SET_PARAMS,
		devID:   id,
		payload: uapi.UblkParamsSize,
		fill: func(b []byte) {
			_, _ = uapi.MarshalInto(p, b)
			binary.LittleEndian.PutUint32(b[0:4], uapi.UblkParamsSize)
		},
	})
	if err != nil {
		return err
	}
	got, err := c.GetParams(ctx, id)
	if err != nil {
		return fmt.Errorf("verify SET_PARAMS: %w", err)
	}
	if dropped := p.Types &^ got.Types; dropped != 0 {
		return &UnsupportedParamsError{DevID: id, Requested: p.Types, Dropped: dropped}
	}
	return nil
}

// getParamsCapacity is the GET_PARAMS buffer: comfortably larger than any
// known ublk_params so a newer kernel's appended blocks are not truncated.
const getParamsCapacity = 512

// GetParams sends GET_PARAMS (v6.0): ctrl_cmd.addr points at a buffer whose
// first 4 bytes (len) give its capacity; the kernel copies min(len,
// sizeof(its ublk_params)) bytes. DEVT is always filled in (disk numbers
// only once started). The returned Len is whatever SET_PARAMS stored, not a
// bound; see uapi.UnmarshalParamsResponse.
func (c *Controller) GetParams(ctx context.Context, id uint32) (*uapi.UblkParams, error) {
	params := &uapi.UblkParams{}
	_, err := c.devExec(ctx, request{
		name:    "GET_PARAMS",
		op:      uapi.UBLK_U_CMD_GET_PARAMS,
		devID:   id,
		payload: getParamsCapacity,
		fill:    func(b []byte) { binary.LittleEndian.PutUint32(b[0:4], getParamsCapacity) },
		read: func(b []byte, _ int32) error {
			if err := uapi.UnmarshalParamsResponse(b, params); err != nil {
				return fmt.Errorf("decode: %w", err)
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return params, nil
}

// GetDevInfo sends GET_DEV_INFO (v6.0): the kernel copies the 64-byte
// ublksrv_ctrl_dev_info to ctrl_cmd.addr, with ublksrv_pid translated into
// the caller's pid namespace (-1 if the server is gone). For an unprivileged
// device it retries with the char-device path prefixed.
func (c *Controller) GetDevInfo(ctx context.Context, id uint32) (*uapi.UblksrvCtrlDevInfo, error) {
	info, err := c.getDevInfo(ctx, id, false)
	if errors.Is(err, syscall.EINVAL) {
		info, err = c.getDevInfo(ctx, id, true)
	}
	return info, err
}

func (c *Controller) getDevInfo(ctx context.Context, id uint32, withPath bool) (*uapi.UblksrvCtrlDevInfo, error) {
	info := &uapi.UblksrvCtrlDevInfo{}
	req := devInfoRequest("GET_DEV_INFO", uapi.UBLK_U_CMD_GET_DEV_INFO, id, info)
	if withPath {
		req.devPath = uapi.UblkDevicePath(id)
	}
	if _, err := c.exec(ctx, req); err != nil {
		return nil, err
	}
	return info, nil
}

func devInfoRequest(name string, op uint32, id uint32, info *uapi.UblksrvCtrlDevInfo) request {
	return request{
		name: name, op: op, devID: id, payload: 64,
		read: func(b []byte, _ int32) error { return uapi.Unmarshal(b, info) },
	}
}

// GetDevInfo2 sends GET_DEV_INFO2 (v6.3): GET_DEV_INFO that always carries the
// char-device path (dev_path_len, NUL included), for privileged and
// unprivileged devices alike, and checks the caller may read /dev/ublkcN.
func (c *Controller) GetDevInfo2(ctx context.Context, id uint32) (*uapi.UblksrvCtrlDevInfo, error) {
	info := &uapi.UblksrvCtrlDevInfo{}
	req := devInfoRequest("GET_DEV_INFO2", uapi.UBLK_U_CMD_GET_DEV_INFO2, id, info)
	req.devPath = uapi.UblkDevicePath(id)
	if _, err := c.exec(ctx, req); err != nil {
		return nil, err
	}
	return info, nil
}

// affinityBufSize holds a cpumask for up to 16384 CPUs; the kernel requires
// len*8 >= nr_cpu_ids and len a multiple of sizeof(long).
const affinityBufSize = 2048

// GetQueueAffinity sends GET_QUEUE_AFFINITY (v6.0): data[0] = queue id; the
// kernel writes the queue's blk-mq CPU mask (a little-endian unsigned long
// bitmap) to ctrl_cmd.addr. Returned as ascending CPU numbers.
// Errors: EINVAL (queue >= nr_hw_queues).
func (c *Controller) GetQueueAffinity(ctx context.Context, id uint32, queue uint16) ([]int, error) {
	var cpus []int
	_, err := c.devExec(ctx, request{
		name:    "GET_QUEUE_AFFINITY",
		op:      uapi.UBLK_U_CMD_GET_QUEUE_AFFINITY,
		devID:   id,
		data:    uint64(queue),
		payload: affinityBufSize,
		read:    func(b []byte, _ int32) error { cpus = decodeCPUMask(b); return nil },
	})
	if err != nil {
		return nil, err
	}
	return cpus, nil
}

// decodeCPUMask lists the set bits of a kernel cpumask. On little-endian
// hosts a bitmap of unsigned longs is also a bitmap of bytes.
func decodeCPUMask(b []byte) []int {
	cpus := []int{}
	for i, v := range b {
		for bit := 0; v != 0; bit, v = bit+1, v>>1 {
			if v&1 != 0 {
				cpus = append(cpus, i*8+bit)
			}
		}
	}
	return cpus
}

// GetFeatures sends GET_FEATURES (v6.5): no device, dev_id = -1; the kernel
// writes its UBLK_F_ALL (8 bytes, len must be exactly 8) to ctrl_cmd.addr.
// The opcode must be the exact _IOR encoding. Kernels before v6.5 answer
// ENODEV (they look up dev_id -1 first); see Features for the interpretation.
func (c *Controller) GetFeatures(ctx context.Context) (uint64, error) {
	var features uint64
	_, err := c.exec(ctx, request{
		name:    "GET_FEATURES",
		op:      uapi.UBLK_U_CMD_GET_FEATURES,
		devID:   AnyDevID,
		payload: uapi.UBLK_FEATURES_LEN,
		read: func(b []byte, _ int32) error {
			features = binary.LittleEndian.Uint64(b)
			return nil
		},
	})
	return features, err
}

// StartUserRecovery sends START_USER_RECOVERY (v6.1): no buffer. Allowed only
// for a device created with UBLK_F_USER_RECOVERY whose old server has exited
// (its /dev/ublkcN released) and that sits in QUIESCED or FAIL_IO.
// Errors: EINVAL (no USER_RECOVERY), EBUSY (still open or not recoverable).
func (c *Controller) StartUserRecovery(ctx context.Context, id uint32) error {
	_, err := c.devExec(ctx, request{name: "START_USER_RECOVERY", op: uapi.UBLK_U_CMD_START_USER_RECOVERY, devID: id})
	return err
}

// EndUserRecovery sends END_USER_RECOVERY (v6.1): data[0] = the new server's
// pid (as for StartDev). It waits until every queue has fetched again, then
// moves the device back to LIVE. Bound it with ctx.
// Errors: EINVAL (pid mismatch, no USER_RECOVERY), EBUSY (not recoverable),
// EINTR.
func (c *Controller) EndUserRecovery(ctx context.Context, id uint32, pid int) error {
	_, err := c.devExec(ctx, request{name: "END_USER_RECOVERY", op: uapi.UBLK_U_CMD_END_USER_RECOVERY, devID: id, data: uint64(pid)})
	return err
}

// ErrNotStarted is returned by UpdateSize for a device without a disk.
var ErrNotStarted = errors.New("device has no disk (not started, or stopped)")

// UpdateSize sends UPDATE_SIZE (v6.16): data[0] = the new size in 512-byte
// sectors; the kernel sets the capacity and notifies (a uevent; partitions
// are not rescanned). UBLK_F_UPDATE_SIZE only advertises the command — the
// driver does not check it. v6.16-v6.19 dereference a NULL disk when the
// device was never started or is stopped, so UpdateSize first checks
// GET_DEV_INFO and returns ErrNotStarted for a DEAD device (a STOP_DEV racing
// with the call can still hit that kernel bug). v7.0+ return ENODEV.
func (c *Controller) UpdateSize(ctx context.Context, id uint32, sectors uint64) error {
	info, err := c.GetDevInfo(ctx, id)
	if err != nil {
		return err
	}
	if info.State == uapi.UBLK_S_DEV_DEAD {
		return &Error{Op: "UPDATE_SIZE", DevID: id, Err: ErrNotStarted}
	}
	_, err = c.devExec(ctx, request{name: "UPDATE_SIZE", op: uapi.UBLK_U_CMD_UPDATE_SIZE, devID: id, data: sectors})
	return err
}

// QuiesceDev sends QUIESCE_DEV (v6.16): data[0] = timeout in milliseconds,
// 0 meaning wait forever. Needs UBLK_F_QUIESCE (hence USER_RECOVERY). The
// driver marks the device canceling, waits until every queue has an idle
// command to cancel, then aborts the server's pending FETCHes with
// UBLK_IO_RES_ABORT; the device ends up QUIESCED once the server closes
// /dev/ublkcN. timeout is rounded up to whole milliseconds and capped at
// math.MaxUint32 ms (the driver truncates to unsigned int); timeout 0 waits
// forever, negative is invalid. Bound the call with ctx as well.
// Errors: EOPNOTSUPP (no UBLK_F_QUIESCE), EBUSY (timeout: all I/O stayed busy),
// ENODEV (not started or DEAD), EINTR. A device already not LIVE returns 0.
func (c *Controller) QuiesceDev(ctx context.Context, id uint32, timeout time.Duration) error {
	if timeout < 0 {
		return &Error{Op: "QUIESCE_DEV", DevID: id, Err: fmt.Errorf("negative timeout %v: %w", timeout, syscall.EINVAL)}
	}
	ms := uint64((timeout + time.Millisecond - 1) / time.Millisecond)
	if ms > math.MaxUint32 {
		ms = math.MaxUint32
	}
	_, err := c.devExec(ctx, request{name: "QUIESCE_DEV", op: uapi.UBLK_U_CMD_QUIESCE_DEV, devID: id, data: ms})
	return err
}

// RegBuf sends REG_BUF (v7.1, UBLK_F_SHMEM_ZC): ctrl_cmd.addr points at a
// 24-byte ublk_shmem_buf_reg {addr, len, flags, reserved}. The kernel
// long-term pins the pages (len page-aligned, at most 4 GiB) and returns the
// buffer index; requests whose pages fall in it arrive with
// UBLK_IO_F_SHMEM_ZC. flags: UBLK_SHMEM_BUF_READ_ONLY.
// Errors: EOPNOTSUPP (no UBLK_F_SHMEM_ZC), EINVAL (alignment, size, flags),
// EFAULT (pinning failed), ENOSPC (out of indexes).
func (c *Controller) RegBuf(ctx context.Context, id uint32, addr uintptr, length uint64, flags uint32) (uint16, error) {
	reg := uapi.UblkShmemBufReg{Addr: uint64(addr), Len: length, Flags: flags}
	res, err := c.devExec(ctx, request{
		name:    "REG_BUF",
		op:      uapi.UBLK_U_CMD_REG_BUF,
		devID:   id,
		payload: 24,
		fill:    func(b []byte) { _, _ = uapi.MarshalInto(&reg, b) },
	})
	if err != nil {
		return 0, err
	}
	if res > math.MaxUint16 {
		return 0, &Error{Op: "REG_BUF", DevID: id, Err: fmt.Errorf("buffer index %d out of range", res)}
	}
	return uint16(res), nil
}

// UnregBuf sends UNREG_BUF (v7.1, UBLK_F_SHMEM_ZC): data[0] = the index RegBuf
// returned; unpins the pages. Errors: EOPNOTSUPP, ENOENT (not registered).
func (c *Controller) UnregBuf(ctx context.Context, id uint32, index uint16) error {
	_, err := c.devExec(ctx, request{name: "UNREG_BUF", op: uapi.UBLK_U_CMD_UNREG_BUF, devID: id, data: uint64(index)})
	return err
}
