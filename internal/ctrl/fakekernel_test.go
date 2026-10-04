package ctrl

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"syscall"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

var bg = context.Background()

// fakeKernel models ublk_drv's control dispatch closely enough to check every
// request we build: opcode matching (only _IOC_TYPE/_IOC_NR, except the exact
// GET_FEATURES compare), device lookup, the unprivileged dev-path protocol
// with its len/addr adjustment, buffer lengths, and the per-command checks of
// ublk_ctrl_uring_cmd in v7.3-rc5. Knobs emulate older kernels.
type fakeKernel struct {
	mu   sync.Mutex
	devs map[uint32]*fakeDev
	ops  []uint32 // every opcode received, in order

	features       uint64 // UBLK_F_ALL of this kernel
	noGetFeatures  bool   // pre-v6.5: GET_FEATURES falls through to the device lookup
	paramsSize     int    // sizeof(struct ublk_params): 112, 136 or 152
	paramTypesAll  uint32 // UBLK_PARAM_TYPE_ALL
	nrCPUs         int
	unknownOpErrno syscall.Errno
	callerUnpriv   bool // the caller lacks CAP_SYS_ADMIN
	nextReg        int32
}

type fakeDev struct {
	info    uapi.UblksrvCtrlDevInfo
	params  []byte
	started bool
	opened  bool
	bufs    map[int32]bool
}

func newFakeKernel() *fakeKernel {
	return &fakeKernel{
		devs: map[uint32]*fakeDev{},
		features: uapi.UBLK_F_SUPPORT_ZERO_COPY | uapi.UBLK_F_URING_CMD_COMP_IN_TASK | uapi.UBLK_F_NEED_GET_DATA |
			uapi.UBLK_F_USER_RECOVERY | uapi.UBLK_F_USER_RECOVERY_REISSUE | uapi.UBLK_F_UNPRIVILEGED_DEV |
			uapi.UBLK_F_CMD_IOCTL_ENCODE | uapi.UBLK_F_USER_COPY | uapi.UBLK_F_ZONED | uapi.UBLK_F_USER_RECOVERY_FAIL_IO |
			uapi.UBLK_F_UPDATE_SIZE | uapi.UBLK_F_AUTO_BUF_REG | uapi.UBLK_F_QUIESCE | uapi.UBLK_F_PER_IO_DAEMON |
			uapi.UBLK_F_BUF_REG_OFF_DAEMON | uapi.UBLK_F_INTEGRITY | uapi.UBLK_F_SAFE_STOP_DEV | uapi.UBLK_F_BATCH_IO |
			uapi.UBLK_F_NO_AUTO_PART_SCAN | uapi.UBLK_F_SHMEM_ZC | uapi.UBLK_F_IO_DESC_SIZE,
		paramsSize:     uapi.UblkParamsSize,
		paramTypesAll:  1<<7 - 1,
		nrCPUs:         6,
		unknownOpErrno: syscall.EOPNOTSUPP,
	}
}

func (k *fakeKernel) ring() *controlTestRing {
	return &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		hdr := *cmd // the driver works on a copy of the SQE header
		return controlTestResult(k.dispatch(op, &hdr)), nil
	}}
}

func (k *fakeKernel) opsSeen() []uint32 {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]uint32(nil), k.ops...)
}

func neg(e syscall.Errno) int32 { return -int32(e) }

func (k *fakeKernel) dispatch(op uint32, h *uapi.UblksrvCtrlCmd) int32 {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.ops = append(k.ops, op)
	if (op>>8)&0xff != 'u' {
		return neg(syscall.EOPNOTSUPP) // ublk_check_cmd_op
	}
	if op == uapi.UBLK_U_CMD_GET_FEATURES && !k.noGetFeatures {
		buf := controlTestBuffer(h)
		if h.Len != uapi.UBLK_FEATURES_LEN || buf == nil {
			return neg(syscall.EINVAL)
		}
		binary.LittleEndian.PutUint64(buf, k.features)
		return 0
	}
	nr := op & 0xff
	if nr == uapi.UBLK_CMD_ADD_DEV {
		return k.addDev(h)
	}
	if int32(h.DevID) < 0 {
		return neg(syscall.ENODEV)
	}
	dev, ok := k.devs[h.DevID]
	if !ok {
		return neg(syscall.ENODEV)
	}
	if errno := k.permission(dev, nr, h); errno != 0 {
		return neg(errno)
	}
	switch nr {
	case uapi.UBLK_CMD_GET_DEV_INFO, uapi.UBLK_CMD_GET_DEV_INFO2:
		buf := controlTestBuffer(h)
		if h.Len < 64 || buf == nil {
			return neg(syscall.EINVAL)
		}
		copy(buf, uapi.Marshal(&dev.info))
	case uapi.UBLK_CMD_DEL_DEV, uapi.UBLK_CMD_DEL_DEV_ASYNC:
		delete(k.devs, h.DevID)
	case uapi.UBLK_CMD_START_DEV:
		if int32(h.Data) <= 0 || len(dev.params) == 0 {
			return neg(syscall.EINVAL)
		}
		if dev.started {
			return neg(syscall.EEXIST)
		}
		dev.started = true
		dev.info.State = uapi.UBLK_S_DEV_LIVE
		dev.info.UblksrvPID = int32(h.Data)
	case uapi.UBLK_CMD_STOP_DEV:
		dev.started = false
		dev.info.State = uapi.UBLK_S_DEV_DEAD
	case uapi.UBLK_CMD_TRY_STOP_DEV:
		if !dev.started {
			return neg(syscall.ENODEV)
		}
		if dev.opened {
			return neg(syscall.EBUSY)
		}
		dev.started = false
		dev.info.State = uapi.UBLK_S_DEV_DEAD
	case uapi.UBLK_CMD_SET_PARAMS:
		return k.setParams(dev, h)
	case uapi.UBLK_CMD_GET_PARAMS:
		buf := controlTestBuffer(h)
		if h.Len <= 8 || buf == nil {
			return neg(syscall.EINVAL)
		}
		phLen := int(binary.LittleEndian.Uint32(buf))
		if phLen > int(h.Len) || phLen == 0 {
			return neg(syscall.EINVAL)
		}
		phLen = min(phLen, k.paramsSize)
		img := make([]byte, k.paramsSize)
		copy(img, dev.params)
		types := binary.LittleEndian.Uint32(img[4:8]) | uapi.UBLK_PARAM_TYPE_DEVT
		binary.LittleEndian.PutUint32(img[4:8], types)
		binary.LittleEndian.PutUint32(img[60:64], 240)
		binary.LittleEndian.PutUint32(img[64:68], h.DevID)
		copy(buf[:phLen], img)
	case uapi.UBLK_CMD_GET_QUEUE_AFFINITY:
		buf := controlTestBuffer(h)
		if int(h.Len)*8 < k.nrCPUs || h.Len%8 != 0 || buf == nil {
			return neg(syscall.EINVAL)
		}
		if h.Data >= uint64(dev.info.NrHwQueues) {
			return neg(syscall.EINVAL)
		}
		clear(buf)
		for cpu := 0; cpu < k.nrCPUs; cpu++ {
			if uint64(cpu)%uint64(dev.info.NrHwQueues) == h.Data {
				buf[cpu/8] |= 1 << (cpu % 8)
			}
		}
	case uapi.UBLK_CMD_START_USER_RECOVERY:
		if dev.info.Flags&uapi.UBLK_F_USER_RECOVERY == 0 {
			return neg(syscall.EINVAL)
		}
		if dev.opened || (dev.info.State != uapi.UBLK_S_DEV_QUIESCED && dev.info.State != uapi.UBLK_S_DEV_FAIL_IO) {
			return neg(syscall.EBUSY)
		}
	case uapi.UBLK_CMD_END_USER_RECOVERY:
		if int32(h.Data) <= 0 || dev.info.Flags&uapi.UBLK_F_USER_RECOVERY == 0 {
			return neg(syscall.EINVAL)
		}
		if dev.info.State != uapi.UBLK_S_DEV_QUIESCED && dev.info.State != uapi.UBLK_S_DEV_FAIL_IO {
			return neg(syscall.EBUSY)
		}
		dev.info.State = uapi.UBLK_S_DEV_LIVE
	case uapi.UBLK_CMD_UPDATE_SIZE:
		if !dev.started {
			return neg(syscall.ENODEV)
		}
		binary.LittleEndian.PutUint64(dev.params[24:32], h.Data)
	case uapi.UBLK_CMD_QUIESCE_DEV:
		if dev.info.Flags&uapi.UBLK_F_QUIESCE == 0 {
			return neg(syscall.EOPNOTSUPP)
		}
		if !dev.started || dev.info.State == uapi.UBLK_S_DEV_DEAD {
			return neg(syscall.ENODEV)
		}
		if h.Data != 0 && h.Data < 10 {
			return neg(syscall.EBUSY) // "timed out"
		}
		dev.info.State = uapi.UBLK_S_DEV_QUIESCED
	case uapi.UBLK_CMD_REG_BUF:
		if dev.info.Flags&uapi.UBLK_F_SHMEM_ZC == 0 {
			return neg(syscall.EOPNOTSUPP)
		}
		var reg uapi.UblkShmemBufReg
		buf := controlTestBuffer(h)
		if len(buf) < 24 || uapi.Unmarshal(buf, &reg) != nil {
			return neg(syscall.EFAULT)
		}
		if reg.Flags&^uapi.UBLK_SHMEM_BUF_READ_ONLY != 0 || reg.Reserved != 0 || reg.Len == 0 ||
			reg.Len > 1<<32 || reg.Len%4096 != 0 || reg.Addr%4096 != 0 {
			return neg(syscall.EINVAL)
		}
		idx := k.nextReg
		k.nextReg++
		if dev.bufs == nil {
			dev.bufs = map[int32]bool{}
		}
		dev.bufs[idx] = true
		return idx
	case uapi.UBLK_CMD_UNREG_BUF:
		if dev.info.Flags&uapi.UBLK_F_SHMEM_ZC == 0 {
			return neg(syscall.EOPNOTSUPP)
		}
		if !dev.bufs[int32(h.Data)] {
			return neg(syscall.ENOENT)
		}
		delete(dev.bufs, int32(h.Data))
	default:
		return neg(k.unknownOpErrno)
	}
	return 0
}

// permission is ublk_ctrl_uring_cmd_permission for a root caller (or a
// non-root one with callerUnpriv).
func (k *fakeKernel) permission(dev *fakeDev, nr uint32, h *uapi.UblksrvCtrlCmd) syscall.Errno {
	if dev.info.Flags&uapi.UBLK_F_UNPRIVILEGED_DEV == 0 {
		if k.callerUnpriv {
			return syscall.EPERM
		}
		if nr != uapi.UBLK_CMD_GET_DEV_INFO2 {
			return 0
		}
	}
	if h.DevPathLen == 0 || h.DevPathLen > 4096 || h.Len < h.DevPathLen {
		return syscall.EINVAL
	}
	path := controlTestBuffer(h)[:h.DevPathLen]
	if i := bytes.IndexByte(path, 0); i >= 0 {
		path = path[:i]
	}
	if string(path) != uapi.UblkDevicePath(dev.info.DevID) {
		return syscall.EPERM // a different char device (rdev mismatch)
	}
	h.Len -= h.DevPathLen
	h.Addr += uint64(h.DevPathLen)
	return 0
}

func (k *fakeKernel) addDev(h *uapi.UblksrvCtrlCmd) int32 {
	buf := controlTestBuffer(h)
	if h.Len < 64 || buf == nil || h.QueueID != 0xffff {
		return neg(syscall.EINVAL)
	}
	var info uapi.UblksrvCtrlDevInfo
	_ = uapi.Unmarshal(buf, &info)
	if info.QueueDepth == 0 || info.QueueDepth > 4096 || info.NrHwQueues == 0 || info.NrHwQueues > 4096 {
		return neg(syscall.EINVAL)
	}
	if !k.callerUnpriv {
		info.Flags &^= uapi.UBLK_F_UNPRIVILEGED_DEV
	} else if info.Flags&uapi.UBLK_F_UNPRIVILEGED_DEV == 0 {
		return neg(syscall.EPERM)
	}
	if err := ValidateFeatures(info.Flags&^uapi.UBLK_F_IO_DESC_SIZE, 0); err != nil {
		return neg(syscall.EINVAL)
	}
	if h.DevID != info.DevID {
		return neg(syscall.EINVAL)
	}
	id := info.DevID
	if id == AnyDevID {
		for id = 0; k.devs[id] != nil; id++ {
		}
	} else if k.devs[id] != nil {
		return neg(syscall.EEXIST)
	}
	info.DevID = id
	info.State = uapi.UBLK_S_DEV_DEAD
	info.UblksrvPID = -1
	info.Flags &= k.features
	info.Flags |= uapi.UBLK_F_CMD_IOCTL_ENCODE | uapi.UBLK_F_URING_CMD_COMP_IN_TASK
	if info.Flags&uapi.UBLK_F_IO_DESC_SIZE == 0 {
		info.IODescSize = 24
	}
	info.NrHwQueues = uint16(min(int(info.NrHwQueues), k.nrCPUs))
	info.MaxIOBufBytes &^= 4095
	info.OwnerUID, info.OwnerGID = 1000, 1000
	k.devs[id] = &fakeDev{info: info}
	copy(buf, uapi.Marshal(&info))
	return 0
}

func (k *fakeKernel) setParams(dev *fakeDev, h *uapi.UblksrvCtrlCmd) int32 {
	buf := controlTestBuffer(h)
	if h.Len <= 8 || buf == nil {
		return neg(syscall.EINVAL)
	}
	phLen := int(binary.LittleEndian.Uint32(buf))
	types := binary.LittleEndian.Uint32(buf[4:8])
	if phLen > int(h.Len) || phLen == 0 || types == 0 {
		return neg(syscall.EINVAL)
	}
	if dev.started {
		return neg(syscall.EACCES)
	}
	img := make([]byte, k.paramsSize)
	copy(img, buf[:min(phLen, k.paramsSize)])
	types &= k.paramTypesAll
	binary.LittleEndian.PutUint32(img[4:8], types)
	if types&uapi.UBLK_PARAM_TYPE_BASIC == 0 || types&uapi.UBLK_PARAM_TYPE_DEVT != 0 {
		dev.params = nil
		return neg(syscall.EINVAL)
	}
	if types&uapi.UBLK_PARAM_TYPE_INTEGRITY != 0 && dev.info.Flags&uapi.UBLK_F_INTEGRITY == 0 {
		dev.params = nil
		return neg(syscall.EINVAL)
	}
	dev.params = img
	return 0
}
