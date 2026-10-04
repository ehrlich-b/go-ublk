// Package ublk serves Linux block devices from Go with ublk, the kernel's
// userspace block device framework. It is pure Go: io_uring and the ublk
// protocol are implemented on golang.org/x/sys, so binaries build with
// CGO_ENABLED=0.
//
// Implement a [Backend] (ReadAt, WriteAt, Size, Flush, Close, plus optional
// interfaces for discard, write-zeroes, FUA, integrity and zero copy), or a
// raw [Handler] that sees every request, and the package creates /dev/ublkbN
// and serves it:
//
//	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
//	defer stop()
//	dev, err := ublk.CreateAndServe(ctx, ublk.DefaultParams(backend), nil)
//	if err != nil {
//		return err
//	}
//	<-dev.Done() // cancelling ctx stops the device gracefully
//	return dev.Close()
//
// # Lifecycle
//
// [Create] adds and configures a device, [Device.Start] starts serving it,
// [Device.Stop] stops it after draining in-flight I/O, and [Device.Close]
// deletes it. [CreateAndServe] combines the first two and ties the device to
// a context. [Device.Done] and [Device.Err] report when and why serving ended.
// A stopped device cannot be started again; create a new one.
//
// Backends are called concurrently: each request runs on its own goroutine
// unless [DeviceParams].Inline is set.
//
// # Recovery
//
// With [DeviceParams].Recovery set, a device outlives the process serving it.
// [Device.Detach] hands it off on purpose (an upgrade); a crash does the same.
// [Recover] attaches a new process to it, and [FindDevices] finds it by
// [DeviceParams].Tag. With [RecoveryReissue], applications using the device
// see a pause, not an error.
//
// # Kernel features
//
// Features newer than the running kernel are negotiated, never silently
// dropped: asking for one the kernel lacks fails at [Create]. [Probe] reports
// what the kernel supports. The package covers the whole ublk UAPI as of
// Linux 7.3: zero copy, shared-memory zero copy, batch I/O, zoned devices,
// integrity metadata, user copy, unprivileged devices, resize and safe stop.
//
// Device creation needs Linux 6.4 or newer with ublk_drv loaded, and root or
// CAP_SYS_ADMIN unless the device is unprivileged.
//
// The guide and full documentation are at https://ublk.ehrlich.dev.
package ublk
