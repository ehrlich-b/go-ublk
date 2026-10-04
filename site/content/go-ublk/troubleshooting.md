---
title: "Troubleshooting and FAQ"
linkTitle: "Troubleshooting & FAQ"
description: "Symptoms, causes and fixes: missing /dev/ublk-control, leaked devices, hangs at teardown, missing flushes and discards, and kernel oopses."
weight: 80
---

## Creating a device

**`open /dev/ublk-control: no such file or directory`**
: `ublk_drv` is not loaded. Run `sudo modprobe ublk_drv`. If the module does not exist, the kernel either ships it in a separate package (Ubuntu's AWS kernels: `linux-modules-extra-$(uname -r)`) or was built without it (WSL2 kernels, some minimal cloud images). `modinfo ublk_drv` tells you which.

**`permission denied` or `operation not permitted`**
: Creating and managing devices needs root or `CAP_SYS_ADMIN`. go-ublk does not support unprivileged devices yet.

**`ADD_DEV` never returns, and the kernel log shows `BUG: kernel NULL pointer dereference` in `ublk_init_queues`**
: The kernel is one of the broken Ubuntu 6.17 builds (generic builds in the -24 to -40 range, crash confirmed on -35 and -40, and `6.17.0-1019-aws`). Every ublk server hits it on its first device. Move to `linux-hwe-6.17` 6.17.0-41 or later, `linux-aws-6.17` 6.17.0-1020 or later, or `linux-hwe-7.0`. The machine needs a reboot. See [Known kernel bugs](/guide/kernel-bugs/).

**`ADD_DEV` fails with `operation not supported`**
: The kernel predates ioctl-encoded commands (Linux 6.4), or the ublk driver is too old. go-ublk needs 6.4 at the very least and documents 6.8 as its minimum.

**`file exists`**
: `DeviceID` names an ID that is already registered, often an orphan from a crashed run. Reap it (below) or use `AutoAssignDeviceID`.

**`character device did not appear: /dev/ublkcN`**
: `/dev/ublkcN` did not show up within 5 seconds of `ADD_DEV`. Check that devtmpfs is mounted on `/dev` and that udev is not wedged; in a container, the node may need to be created or bind-mounted explicitly.

**The device has fewer queues than I asked for**
: The kernel caps hardware queues at the number of CPUs and go-ublk uses what it returns. `Device.NumQueues()` reports the real count.

**`EnableUserCopy` returns "not implemented"**
: User-copy mode is not supported. So far go-ublk serves devices in the kernel's default copy mode only; `EnableZeroCopy`, `EnableUnprivileged` and `EnableZoned` do not work either. See [Configuration](/go-ublk/configuration/#kernel-feature-switches).

## Serving I/O

**`blkdiscard` says "operation not supported"; `fstrim` does nothing**
: The backend does not implement `DiscardBackend` (or a wrapper type hides the method), so the device does not advertise discard. The same goes for `WriteZeroesBackend` and `blkdiscard -z`. `MaxDiscardSectors = 0` also turns both off.

**`Flush` is never called**
: `VolatileCache` is false, which tells the kernel every completed write is already durable, so it never sends a flush. That is correct only if it is true. See [durability](/go-ublk/backends/#flush-and-durability).

**Applications get `EIO` but my backend returned a specific error**
: go-ublk fails every unsuccessful request with `-EIO`; errnos are not passed through. A short read or write count is also a failure.

**Reads past the end of a file-backed device fail**
: `os.File.ReadAt` returns `io.EOF` with a short count when the file is shorter than the device. A block device must return zeros there: clear the rest of the buffer and return the full length, as the `ublk-loop` example does.

**I/O is slow and one CPU is pegged**
: Each queue calls the backend synchronously, one request at a time; a slow backend bounds throughput per queue. Use more queues (up to the CPU count) and see [Performance](/go-ublk/performance/). If `Options.Debug` is on, turn it off: debug logging serializes on a lock and can stall queues under load.

**Data corruption with more than one queue**
: Old builds of go-ublk mapped every queue's descriptor array at the wrong offset. That was fixed in June 2026; if you see corruption on a current build, it is a serious bug, so please report it with the queue count, depth, kernel and a reproducer. The `test/verify` oracle can usually produce one.

## Stopping

**Teardown hangs, or `Close` takes about 10 seconds**
: Usually the device's context was cancelled before `Close`, so the queues stopped before `STOP_DEV` could drain through them. Call `Close` first and do not cancel; see [Device lifecycle](/go-ublk/lifecycle/#do-not-cancel-the-context-first). Another cause is a backend call that does not return.

**`DEL_DEV` blocks forever in my own tool**
: Something still holds `/dev/ublkcN` open: a duplicated descriptor, a mapping, or an io_uring with the descriptor registered. The kernel completes `DEL_DEV` only after the last reference is gone. go-ublk's `Close` releases its own references before deleting; a custom tool must do the same.

**A killed server left a device behind**
: Expected: the kernel keeps the device registered until it is deleted. Reap it with `ublk.ListDevices` and `ublk.DeleteDevice`, or with either example's `--del=all` flag. See [Getting started](/go-ublk/getting-started/#cleaning-up-a-leaked-device).

**`ListDevices` does not show a device I can see in `/dev`**
: It probes IDs 0 through 63 only. Devices with higher IDs are not listed, though `DeleteDevice` works for any ID.

**The machine hangs during reboot with "blocked for more than 122 seconds" for the server and `iou-wrk` threads**
: The server was not running as a systemd unit ordered before the filesystem's unmount, so it died while the filesystem still needed it. See [Deployment](/go-ublk/deployment/).

**Processes stuck in `D` state on `/dev/ublkbN`**
: A request was delivered and never committed: the server is wedged, or a queue goroutine exited without the device stopping (an open defect). For a privileged device the kernel never times requests out. Killing the server process fails the outstanding requests and releases the waiters. If the server is already gone and tasks remain stuck, capture `/proc/<pid>/stack` for the stuck tasks and the kernel log before rebooting; that is worth a bug report.

## Building

**`go build` fails on macOS or Windows**
: The package is Linux-only. Cross-compile with `GOOS=linux`. On macOS, the repository's `make vm-test-unit` runs the unit tests on a Linux VM.

**Does it need cgo or liburing?**
: No. Builds work with `CGO_ENABLED=0`, and the binary has no runtime dependencies beyond the kernel.

**Can I run more than one device in a process?**
: Yes; each `Create` or `CreateAndServe` makes an independent device with its own queues. Logging configuration is process-wide, so the most recent call's `Options.Logger` and `Debug` apply to all of them. A process that creates and closes many devices over time leaks a few io_uring mappings per cycle until that defect is fixed.

**Can a backend serve a device over the network?**
: Yes, a backend can do anything. Keep in mind that calls are synchronous per queue, that a call must not block indefinitely, and that a server crash takes the device with it until user recovery is implemented.
