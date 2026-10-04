package ublk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/ctrl"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// Features is a set of kernel ublk features (UBLK_F_* bits).
type Features uint64

// The ublk features, with the kernel release that introduced each.
const (
	FeatureZeroCopy        Features = uapi.UBLK_F_SUPPORT_ZERO_COPY      // 6.15 (functional)
	FeatureCompInTask      Features = uapi.UBLK_F_URING_CMD_COMP_IN_TASK // 6.0, inert since 6.5
	FeatureNeedGetData     Features = uapi.UBLK_F_NEED_GET_DATA          // 6.0
	FeatureUserRecovery    Features = uapi.UBLK_F_USER_RECOVERY          // 6.1
	FeatureRecoveryReissue Features = uapi.UBLK_F_USER_RECOVERY_REISSUE  // 6.1
	FeatureUnprivileged    Features = uapi.UBLK_F_UNPRIVILEGED_DEV       // 6.3
	FeatureIoctlEncode     Features = uapi.UBLK_F_CMD_IOCTL_ENCODE       // 6.4
	FeatureUserCopy        Features = uapi.UBLK_F_USER_COPY              // 6.5
	FeatureZoned           Features = uapi.UBLK_F_ZONED                  // 6.6
	FeatureRecoveryFailIO  Features = uapi.UBLK_F_USER_RECOVERY_FAIL_IO  // 6.13
	FeatureUpdateSize      Features = uapi.UBLK_F_UPDATE_SIZE            // 6.16
	FeatureAutoBufReg      Features = uapi.UBLK_F_AUTO_BUF_REG           // 6.16
	FeatureQuiesce         Features = uapi.UBLK_F_QUIESCE                // 6.16
	FeaturePerIODaemon     Features = uapi.UBLK_F_PER_IO_DAEMON          // 6.16
	FeatureBufRegOffDaemon Features = uapi.UBLK_F_BUF_REG_OFF_DAEMON     // 6.17
	FeatureBatchIO         Features = uapi.UBLK_F_BATCH_IO               // 7.0
	FeatureIntegrity       Features = uapi.UBLK_F_INTEGRITY              // 7.0
	FeatureSafeStop        Features = uapi.UBLK_F_SAFE_STOP_DEV          // 7.0
	FeatureNoPartScan      Features = uapi.UBLK_F_NO_AUTO_PART_SCAN      // 7.0
	FeatureSharedMemoryZC  Features = uapi.UBLK_F_SHMEM_ZC               // 7.1
	FeatureIODescSize      Features = uapi.UBLK_F_IO_DESC_SIZE           // 7.3
)

// Has reports whether every feature in f2 is in f.
func (f Features) Has(f2 Features) bool { return f&f2 == f2 }

func (f Features) String() string { return ctrl.FeatureNames(uint64(f)) }

// KernelSupport describes the running kernel's ublk support.
type KernelSupport struct {
	// Features the kernel reports (GET_FEATURES, kernel 6.5+). When Known is
	// false the kernel is too old to say and Features is the 6.0 base set.
	Features Features
	Known    bool
}

// Probe reports what the running kernel's ublk driver supports. It fails if
// /dev/ublk-control cannot be opened (ublk_drv not loaded, or no permission)
// or io_uring is unavailable (some distributions disable it by default with
// the kernel.io_uring_disabled sysctl).
func Probe() (KernelSupport, error) {
	c, err := createController()
	if err != nil {
		return KernelSupport{}, explainControlError(err)
	}
	defer c.Close()
	fs, err := c.Features(context.Background())
	if err != nil {
		return KernelSupport{}, err
	}
	return KernelSupport{Features: Features(fs.Flags), Known: fs.Known}, nil
}

// explainControlError adds the usual cause to errors opening the control
// plane.
func explainControlError(err error) error {
	var errno syscall.Errno
	errors.As(err, &errno)
	switch {
	case errno == syscall.ENOENT:
		return fmt.Errorf("%w (is the ublk_drv module loaded? try: modprobe ublk_drv)", err)
	case errno == syscall.EPERM && strings.Contains(err.Error(), "io_uring"):
		// io_uring_setup itself was refused: the RHEL 10 family ships with
		// kernel.io_uring_disabled set. (syscall.EPERM also matches
		// os.ErrPermission, so test the errno itself.)
		return fmt.Errorf("%w (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, "+
			"or 1 with this process in kernel.io_uring_group)", err)
	case errno == syscall.EACCES || errno == syscall.EPERM:
		return fmt.Errorf("%w (creating ublk devices needs root or CAP_SYS_ADMIN, or EnableUnprivileged with udev rules)", err)
	}
	return err
}

// Features reports the features negotiated for this device.
func (d *Device) Features() Features { return Features(d.flags) }

// KernelDeviceState is a device's state as the kernel reports it.
type KernelDeviceState string

const (
	KernelStateDead     KernelDeviceState = "dead"     // created or stopped: no block device
	KernelStateLive     KernelDeviceState = "live"     // serving
	KernelStateQuiesced KernelDeviceState = "quiesced" // server gone, I/O held for Recover
	KernelStateFailIO   KernelDeviceState = "fail-io"  // server gone, I/O failing until Recover
)

// KernelDeviceInfo is what the kernel knows about a ublk device.
type KernelDeviceInfo struct {
	ID         uint32
	State      KernelDeviceState
	Features   Features
	NumQueues  int
	QueueDepth int
	MaxIOSize  int
	ServerPID  int
	OwnerUID   uint32
	OwnerGID   uint32
	Tag        uint64 // DeviceParams.Tag, as stored by the kernel
}

func kernelDeviceInfo(info *uapi.UblksrvCtrlDevInfo) KernelDeviceInfo {
	state := KernelDeviceState(fmt.Sprintf("state-%d", info.State))
	switch info.State {
	case uapi.UBLK_S_DEV_DEAD:
		state = KernelStateDead
	case uapi.UBLK_S_DEV_LIVE:
		state = KernelStateLive
	case uapi.UBLK_S_DEV_QUIESCED:
		state = KernelStateQuiesced
	case uapi.UBLK_S_DEV_FAIL_IO:
		state = KernelStateFailIO
	}
	return KernelDeviceInfo{
		ID: info.DevID, State: state, Features: Features(info.Flags),
		NumQueues: int(info.NrHwQueues), QueueDepth: int(info.QueueDepth),
		MaxIOSize: int(info.MaxIOBufBytes), ServerPID: int(info.UblksrvPID),
		OwnerUID: info.OwnerUID, OwnerGID: info.OwnerGID, Tag: info.UblksrvFlags,
	}
}

// GetDeviceInfo asks the kernel about any ublk device, owned by this process
// or not.
func GetDeviceInfo(id uint32) (KernelDeviceInfo, error) {
	c, err := createController()
	if err != nil {
		return KernelDeviceInfo{}, explainControlError(err)
	}
	defer c.Close()
	info, err := c.GetDevInfo(context.Background(), id)
	if err != nil {
		return KernelDeviceInfo{}, fmt.Errorf("device %d: %w", id, err)
	}
	return kernelDeviceInfo(info), nil
}

// KernelInfo is GetDeviceInfo for this device.
func (d *Device) KernelInfo() (KernelDeviceInfo, error) { return GetDeviceInfo(d.ID) }

// FindDevices returns the IDs of the registered devices created with
// DeviceParams.Tag set to tag.
func FindDevices(tag uint64) ([]uint32, error) {
	ids, err := ListDevices()
	if err != nil {
		return nil, err
	}
	var found []uint32
	for _, id := range ids {
		info, err := GetDeviceInfo(id)
		if errors.Is(err, syscall.ENODEV) {
			continue // deleted since the scan
		}
		if err != nil {
			return nil, err
		}
		if info.Tag == tag {
			found = append(found, id)
		}
	}
	return found, nil
}

// Resize changes the device's size (UPDATE_SIZE, kernel 6.16+). The backend
// must already serve the new size. The device must be running.
func (d *Device) Resize(newSize int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != DeviceStateRunning {
		return fmt.Errorf("device is %s; Resize needs a running device", d.state)
	}
	if !d.Features().Has(FeatureUpdateSize) {
		return fmt.Errorf("%w: the kernel did not grant UPDATE_SIZE (kernel 6.16+)", ErrNotImplemented)
	}
	if newSize <= 0 || newSize%int64(d.blockSize) != 0 {
		return fmt.Errorf("new size %d must be positive and a multiple of the block size %d", newSize, d.blockSize)
	}
	c, err := createController()
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.UpdateSize(context.Background(), d.ID, uint64(newSize/uapi.SectorSize)); err != nil {
		return fmt.Errorf("resize device %d: %w", d.ID, err)
	}
	d.params.Size = newSize
	return nil
}

// quiesceTimeout bounds QUIESCE_DEV in Detach: how long in-flight I/O may take
// to drain before the server lets go.
const quiesceTimeout = 30 * time.Second

// Detach lets go of a running device without deleting it, for a zero-downtime
// server upgrade: the block device stays, applications keep it open, and a new
// process takes over with Recover. The device must have been created with a
// RecoveryMode. On kernels with QUIESCE (6.16+) in-flight I/O is drained
// first; on older ones it is reissued (RecoveryReissue) or failed
// (RecoveryQueue) by the kernel once this process lets go.
//
// After Detach, Close only releases the Device; it does not delete the kernel
// device.
func (d *Device) Detach() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != DeviceStateRunning {
		return fmt.Errorf("device is %s; Detach needs a running device", d.state)
	}
	if !d.Features().Has(FeatureUserRecovery) {
		return fmt.Errorf("Detach needs a device created with a RecoveryMode")
	}
	d.leaving.Store(true)
	close(d.unwatch)

	if d.Features().Has(FeatureQuiesce) {
		// Best effort: if it fails (EBUSY when no queue goes idle in time),
		// the kernel still requeues or fails what is outstanding once we let
		// go.
		if c, err := createController(); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), quiesceTimeout+5*time.Second)
			_ = c.QuiesceDev(ctx, d.ID, quiesceTimeout)
			cancel()
			c.Close()
		}
	}
	// QUIESCE_DEV stops the kernel dispatching new requests and aborts the
	// fetches that were idle, but a request a handler still holds gets a fresh
	// fetch when it commits, which nothing aborts. So the engines are always
	// abandoned: they commit what handlers hold, then close their rings,
	// cancelling whatever fetches remain. Without QUIESCE (before 6.16) a
	// request fetched meanwhile is requeued (RecoveryReissue) or failed by the
	// kernel once this process lets go.
	err := d.abandonQueues()
	d.state = DeviceStateDetached
	d.finish(nil)
	if err != nil {
		return fmt.Errorf("device detached, but %w", err)
	}
	return nil
}

// recoverWait bounds how long Recover waits for the previous server to be
// gone (START_USER_RECOVERY returns EBUSY until the kernel has released its
// /dev/ublkcN, which happens asynchronously after that process exits).
const recoverWait = 30 * time.Second

// Recover takes over a device whose server crashed or called Detach, and
// serves it with params' Backend or Handler. The device's geometry and
// features come from the kernel; params supplies the backend and the
// data-plane options (Inline, ThreadsPerQueue, CPUAffinity). The backend's
// size must equal the device's. Like CreateAndServe, cancelling ctx later
// stops the device gracefully.
func Recover(ctx context.Context, id uint32, params DeviceParams, options *Options) (*Device, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options == nil {
		options = &Options{}
	}
	if options.Context != nil {
		ctx = options.Context
	}
	configureLogging(options)

	c, err := createController()
	if err != nil {
		return nil, explainControlError(err)
	}
	defer c.Close()
	bg := context.Background()
	info, err := c.GetDevInfo(bg, id)
	if err != nil {
		return nil, fmt.Errorf("device %d: %w", id, err)
	}
	if info.Flags&uapi.UBLK_F_USER_RECOVERY == 0 {
		return nil, fmt.Errorf("device %d was not created with a RecoveryMode", id)
	}
	kp, err := c.GetParams(bg, id)
	if err != nil {
		return nil, fmt.Errorf("device %d: %w", id, err)
	}

	// Geometry is the kernel's, not the caller's.
	params.NumQueues = int(info.NrHwQueues)
	params.QueueDepth = int(info.QueueDepth)
	params.MaxIOSize = int(info.MaxIOBufBytes)
	params.LogicalBlockSize = 1 << kp.Basic.LogicalBSShift
	params.ReadOnly = kp.Basic.Attrs&uapi.UBLK_ATTR_READ_ONLY != 0
	params.Rotational = kp.Basic.Attrs&uapi.UBLK_ATTR_ROTATIONAL != 0
	params.VolatileCache = kp.Basic.Attrs&uapi.UBLK_ATTR_VOLATILE_CACHE != 0
	params.EnableUserCopy = info.Flags&uapi.UBLK_F_USER_COPY != 0
	params.Tag = info.UblksrvFlags
	devSize := int64(kp.Basic.DevSectors) * uapi.SectorSize
	if params.Handler != nil && params.Size == 0 {
		params.Size = devSize
	}
	if err := validateParams(&params); err != nil {
		return nil, err
	}
	if got := params.size(); got != devSize {
		return nil, fmt.Errorf("backend size %d does not match device %d's size %d", got, id, devSize)
	}

	d := newDevice(id, params, options, info.Flags)
	d.mu.Lock()
	defer d.mu.Unlock()

	deadline := time.Now().Add(recoverWait)
	for {
		err = c.StartUserRecovery(bg, id)
		if !errors.Is(err, syscall.EBUSY) || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("START_USER_RECOVERY on device %d (is its previous server still running?): %w", id, err)
	}
	if err := d.startQueues(); err != nil {
		return nil, err
	}
	sctx, cancel := context.WithTimeout(bg, startTimeout)
	defer cancel()
	if err := c.EndUserRecovery(sctx, id, os.Getpid()); err != nil {
		d.abandonQueues()
		return nil, fmt.Errorf("END_USER_RECOVERY on device %d: %w", id, err)
	}
	d.state = DeviceStateRunning
	d.watch(ctx)
	if options.Logger != nil {
		options.Logger.Printf("Device %s recovered with %d queues", d.Path, d.queues)
	}
	return d, nil
}

// RegisterSharedMemory registers mem for shared-memory zero copy (the device
// needs DeviceParams.SharedMemoryZeroCopy; kernel 7.1+). mem must be
// page-aligned shared memory — typically an mmap of a memfd or hugetlbfs file
// that the applications using the device also map. An O_DIRECT request whose
// pages all lie in one registered region then reaches the handler with
// FlagSharedMemory and Request.Data pointing into mem: neither side copies.
// readOnly pins the pages without write access (for a write-sealed memfd);
// only writes can then match. The memory must stay mapped until
// UnregisterSharedMemory returns. Returns the region's index.
func (d *Device) RegisterSharedMemory(mem []byte, readOnly bool) (uint16, error) {
	if !d.Features().Has(FeatureSharedMemoryZC) {
		return 0, fmt.Errorf("%w: the device was not created with SharedMemoryZeroCopy (kernel 7.1+)", ErrNotImplemented)
	}
	if len(mem) == 0 {
		return 0, fmt.Errorf("empty region")
	}
	c, err := createController()
	if err != nil {
		return 0, err
	}
	defer c.Close()
	var flags uint32
	if readOnly {
		flags = uapi.UBLK_SHMEM_BUF_READ_ONLY
	}
	addr := uintptr(unsafe.Pointer(&mem[0]))
	idx, err := c.RegBuf(context.Background(), d.ID, addr, uint64(len(mem)), flags)
	if err != nil {
		return 0, fmt.Errorf("register shared memory: %w", err)
	}
	d.shmem.Add(idx, mem)
	return idx, nil
}

// UnregisterSharedMemory unregisters a region registered with
// RegisterSharedMemory. The kernel freezes the device's queue while it does,
// so no request is using the region afterwards.
func (d *Device) UnregisterSharedMemory(index uint16) error {
	c, err := createController()
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.UnregBuf(context.Background(), d.ID, index); err != nil {
		return fmt.Errorf("unregister shared memory %d: %w", index, err)
	}
	d.shmem.Remove(index)
	return nil
}
