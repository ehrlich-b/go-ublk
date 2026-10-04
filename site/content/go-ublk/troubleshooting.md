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
: Creating and managing devices needs root or `CAP_SYS_ADMIN`, unless you use [unprivileged devices](/guide/unprivileged/) (`EnableUnprivileged`, which needs udev rules). On RHEL 10, CentOS Stream 10 and AlmaLinux 10, `io_uring_setup` itself fails with `EPERM` because io_uring is disabled by default: set `sysctl kernel.io_uring_disabled=0` (or `1` with the server in `kernel.io_uring_group`). `ublk.Probe()` names this cause.

**`ADD_DEV` never returns, and the kernel log shows `BUG: kernel NULL pointer dereference` in `ublk_init_queues`**
: The kernel is one of the broken Ubuntu 6.17 builds (generic builds in the -24 to -40 range, crash confirmed on -35 and -40, and `6.17.0-1019-aws`). Every ublk server hits it on its first device. Move to `linux-hwe-6.17` 6.17.0-41 or later, `linux-aws-6.17` 6.17.0-1020 or later, or `linux-hwe-7.0`. The machine needs a reboot. See [Known kernel bugs](/guide/kernel-bugs/).

**Creation fails with `operation not supported` and a list of features**
: You asked for a feature the running kernel does not have (for example `RecoveryFailIO` before 6.13, or `NoPartitionScan` before 7.0). The error names the missing features; `ublk.Probe()` lists what the kernel supports. go-ublk never silently creates a device without a feature you asked for.

**Every command fails with `no such device` on an old kernel**
: Kernels before 6.4 do not understand ioctl-encoded control commands, which go-ublk always uses. go-ublk documents 6.8 as its minimum; see the [compatibility matrix](/reference/matrix/).

**`file exists`**
: `DeviceID` names an ID that is already registered, often an orphan from a crashed run. Reap it (below) or use `AutoAssignDeviceID`.

**`character device did not appear: /dev/ublkcN`**
: `/dev/ublkcN` did not show up within 5 seconds of `ADD_DEV`. Check that devtmpfs is mounted on `/dev` and that udev is not wedged; in a container, the node may need to be created or bind-mounted explicitly.

**The device has fewer queues than I asked for**
: The kernel caps hardware queues at the number of CPUs and go-ublk uses what it returns. `Device.NumQueues()` reports the real count.

**`EnableZeroCopy` or `EnableZoned` is rejected at creation**
: Zero copy needs a backend that implements `ZeroCopyBackend` (it serves the device from a file descriptor); zoned devices need a `Handler`, because a `Backend` cannot serve zone operations. See [Configuration](/go-ublk/configuration/#data-copy-modes).

## Serving I/O

**`blkdiscard` says "operation not supported"; `fstrim` does nothing**
: The backend does not implement `DiscardBackend` (or a wrapper type hides the method), so the device does not advertise discard. The same goes for `WriteZeroesBackend` and `blkdiscard -z`. `MaxDiscardSectors = 0` also turns both off.

**`Flush` is never called**
: `VolatileCache` is false, which tells the kernel every completed write is already durable, so it never sends a flush. That is correct only if it is true. See [durability](/go-ublk/backends/#flush-and-durability).

**Applications get `EIO` but my backend returned a specific error**
: Return (or wrap) a `syscall.Errno` and go-ublk passes it through; any other error becomes `EIO`. Even then, kernels whose ublk driver does not translate errnos report every failure as `EIO`. A short read or write count is a failure. See [Errors](/go-ublk/backends/#errors).

**Reads past the end of a file-backed device fail**
: `os.File.ReadAt` returns `io.EOF` with a short count when the file is shorter than the device. A block device must return zeros there: clear the rest of the buffer and return the full length, as the `ublk-loop` example does.

**I/O is slow and one CPU is pegged**
: With `Inline` set, each queue runs the backend on its own thread one request at a time, so a slow backend bounds throughput per queue; unset it. Otherwise look at the backend, and see [Performance](/go-ublk/performance/). If `Options.Debug` is on, turn it off.

**Data corruption with more than one queue**
: Old builds of go-ublk mapped every queue's descriptor array at the wrong offset. That was fixed in June 2026; if you see corruption on a current build, it is a serious bug, so please report it with the queue count, depth, kernel and a reproducer. The `test/verify` oracle can usually produce one.

## Stopping

**`Close` fails with a timeout, or waits a long time**
: `STOP_DEV` drains in-flight I/O through your backend, and the filesystem's writeback with it; a backend call that never returns holds it up until `Options.StopTimeout`. The device keeps serving when that happens. Find the stuck call (a SIGQUIT goroutine dump shows it).

**`Start` fails with `ErrStopped`**
: A stopped device cannot be restarted (the kernel does not reliably support it). `Close` it and create a new one.

**`DEL_DEV` blocks forever in my own tool**
: Something still holds `/dev/ublkcN` open: a duplicated descriptor, a mapping, or an io_uring with the descriptor registered. The kernel completes `DEL_DEV` only after the last reference is gone. go-ublk's `Close` releases its own references before deleting; a custom tool must do the same.

**A killed server left a device behind**
: Expected: the kernel keeps the device registered. With a recovery mode, take it over with `ublk.Recover`. Without one, reap it with `ublk.DeleteDevice` (or either example's `--del=all`). See [Deployment](/go-ublk/deployment/#crashes-and-upgrades-without-downtime).

**`Recover` fails with "is its previous server still running?"**
: `START_USER_RECOVERY` returns `EBUSY` until the kernel has released the previous server's `/dev/ublkcN`, which happens after that process has fully exited. `Recover` retries for 30 seconds. A previous server stuck in an uninterruptible wait (an `fsync` on a slow disk, for example) can take longer to exit.

**`ListDevices` does not show a device I can see in `/dev`**
: It probes IDs 0 through 63 only. Devices with higher IDs are not listed, though `DeleteDevice` works for any ID.

**The machine hangs during reboot with "blocked for more than 122 seconds" for the server and `iou-wrk` threads**
: The server was not running as a systemd unit ordered before the filesystem's unmount, so it died while the filesystem still needed it. See [Deployment](/go-ublk/deployment/).

**Processes stuck in `D` state on `/dev/ublkbN`**
: A request was delivered and never committed: a backend call (or a `Handler` that never calls `Complete`) is stuck. For a privileged device the kernel never times requests out. With a recovery mode, I/O issued while no server is attached also waits, by design, until `Recover`. Killing the server process fails the outstanding requests and releases the waiters. If the server is already gone and tasks remain stuck, capture `/proc/<pid>/stack` for the stuck tasks and the kernel log before rebooting; that is worth a bug report.

## Building

**`go build` fails on macOS or Windows**
: The package is Linux-only. Cross-compile with `GOOS=linux`. On macOS, the repository's `make vm-test-unit` runs the unit tests on a Linux VM.

**Does it need cgo or liburing?**
: No. Builds work with `CGO_ENABLED=0`, and the binary has no runtime dependencies beyond the kernel.

**Can I run more than one device in a process?**
: Yes; each `Create` or `CreateAndServe` makes an independent device with its own queues. Logging configuration is process-wide, so the most recent call's `Options.Logger` and `Debug` apply to all of them. A process that creates and closes many devices over time leaks a few io_uring mappings per cycle until that defect is fixed.

**Can a backend serve a device over the network?**
: Yes, a backend can do anything. Keep in mind that calls are synchronous per queue, that a call must not block indefinitely, and that a server crash takes the device with it until user recovery is implemented.
