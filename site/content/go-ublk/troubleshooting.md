---
title: "Troubleshooting and FAQ"
linkTitle: "Troubleshooting & FAQ"
description: "Symptoms, causes and fixes: missing /dev/ublk-control, leaked devices, hangs at teardown, missing flushes and discards, and kernel oopses."
weight: 80
---

## Creating a device

**`open /dev/ublk-control: no such file or directory`**
: Load `sudo modprobe ublk_drv`. If unavailable, check `modinfo ublk_drv`: Ubuntu AWS packages it in `linux-modules-extra-$(uname -r)`; checked WSL2 kernels and some cloud images omit it.

**`permission denied` or `operation not permitted`**
: Use root/CAP_SYS_ADMIN or [EnableUnprivileged with udev rules](/guide/unprivileged/). RHEL 10/CentOS Stream 10/AlmaLinux 10 disable io_uring by default, causing io_uring_setup EPERM. Set `sysctl kernel.io_uring_disabled=0`, or 1 with membership in kernel.io_uring_group. Probe identifies this cause.

**`ADD_DEV` never returns, and the kernel log shows `BUG: kernel NULL pointer dereference` in `ublk_init_queues`**
: Suspect Ubuntu generic 6.17 -24 through -40 (crashes confirmed on -35/-40) or 6.17.0-1019-aws. Other servers reproduce it too. Reboot into linux-hwe-6.17 >= 6.17.0-41, linux-aws-6.17 >= 6.17.0-1020, or linux-hwe-7.0. See [kernel bugs](/guide/kernel-bugs/).

**Creation fails with `operation not supported` and a list of features**
: The kernel lacks requested features, e.g. RecoveryFailIO before 6.13 or NoPartitionScan before 7.0. Read the named error and `ublk.Probe()` output; required features are not silently dropped.

**Every command fails with `no such device` on an old kernel**
: go-ublk requires ioctl-encoded commands, introduced in its minimum kernel, 6.4. See the [matrix](/reference/matrix/).

**`file exists`**
: DeviceID is occupied, possibly by an orphan. Reap it or use AutoAssignDeviceID.

**`character device did not appear: /dev/ublkcN`**
: The node was absent 5 seconds after ADD_DEV. Check devtmpfs on `/dev` and udev; containers may need node creation or a bind mount.

**The device has fewer queues than I asked for**
: Queues are capped at CPU count. Device.NumQueues() reports the negotiated count.

**`EnableZeroCopy` or `EnableZoned` is rejected at creation**
: Zero copy requires a ZeroCopyBackend file descriptor; zoned operations require a Handler. See [configuration](/go-ublk/configuration/#data-copy-modes).

## Serving I/O

**`blkdiscard` says "operation not supported"; `fstrim` does nothing**
: Implement DiscardBackend, exposed through any wrapper. WriteZeroesBackend similarly enables `blkdiscard -z`; MaxDiscardSectors = 0 disables both.

**`Flush` is never called**
: VolatileCache = false suppresses flushes, promising completed writes are durable. Verify that promise; see [durability](/go-ublk/backends/#flush-and-durability).

**Applications get `EIO` but my backend returned a specific error**
: Return or wrap syscall.Errno for passthrough. Other errors and short transfers become EIO; kernel errno translation also matters. See [error mapping](/go-ublk/backends/#errors).

**Reads past the end of a file-backed device fail**
: Short files return io.EOF and a short count. Within device capacity, zero-fill the rest and return full length, as ublk-loop does.

**I/O is slow and one CPU is pegged**
: Inline serializes each queue's calls; unset it for slow backends. Disable Options.Debug, inspect backend latency, and consult [performance](/go-ublk/performance/).

**Data corruption with more than one queue**
: A June 2026 fix corrected descriptor mmap offsets. For current corruption, report queues, depth, kernel, and reproducer; test/verify can help produce one.

## Stopping

**`Close` fails with a timeout, or waits a long time**
: STOP_DEV drains requests and filesystem writeback through the backend. Hung calls delay it until Options.StopTimeout, retaining serving resources. Find them with a SIGQUIT goroutine dump.

**`Start` fails with `ErrStopped`**
: Kernel restart is unreliable. Close the stopped device and create a new one.

**`DEL_DEV` blocks forever in my own tool**
: Release every char-device reference, including duplicate fds, mappings, and fixed-file registrations. DEL_DEV waits for the last reference; go-ublk Close releases its own before deletion.

**A killed server left a device behind**
: Registration survives server death. Use ublk.Recover with recovery enabled; otherwise ublk.DeleteDevice or an example's `--del=all`. See [deployment](/go-ublk/deployment/#crashes-and-upgrades-without-downtime).

**`Recover` fails with "is its previous server still running?"**
: START_USER_RECOVERY returns EBUSY until the old char device releases. Recover retries for 30 seconds; an old process stuck in uninterruptible I/O, such as fsync, may outlive that wait.

**`ListDevices` does not show a device I can see in `/dev`**
: With sysfs unavailable, the fallback probes only 0-63. Normally ListDevices enumerates `/sys/class/ublk-char`, including higher IDs; DeleteDevice accepts any ID.

**The machine hangs during reboot with "blocked for more than 122 seconds" for the server and `iou-wrk` threads**
: Check systemd mount ordering: the server may have died before unmount finished. See [deployment](/go-ublk/deployment/).

**Processes stuck in `D` state on `/dev/ublkbN`**
: Look for a hung backend or Handler missing Complete; privileged requests do not time out. Recovery modes can also hold I/O without a server. Killing the server fails requests only without replay/queueing recovery; otherwise recover or stop the device. For unexplained waits after server exit, capture `/proc/<pid>/stack` and kernel logs before rebooting and report them.

## Building

**`go build` fails on macOS or Windows**
: Linux-only: cross-compile with GOOS=linux. On macOS, make vm-test-unit runs tests in a Linux VM.

**Does it need cgo or liburing?**
: No; use CGO_ENABLED=0. Runtime dependencies stop at the kernel.

**Can I run more than one device in a process?**
: Yes. Each Create/CreateAndServe has independent queues. Logging is process-wide; the most recent Options.Logger/Debug settings apply. v0.2.0 fixed the per-lifecycle io_uring mapping leak.

**Can a backend serve a device over the network?**
: Yes. Calls run concurrently by default; Inline serializes each queue. Bound network waits. Recovery can preserve the device after a crash.