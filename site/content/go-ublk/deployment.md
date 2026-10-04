---
title: "Deployment"
linkTitle: "Deployment"
description: "Running a go-ublk server in production: the systemd unit and mount ordering it requires, signals, teardown, and kernel selection."
weight: 50
---

A ublk server is a userspace process that a kernel block device, and often a mounted filesystem, depends on. That inverts the usual relationship between a daemon and the system: at shutdown, the filesystem has to be unmounted **while the daemon is still running**, because the unmount's writeback goes through it. Getting this wrong does not just lose data; it can stop the machine from rebooting.

## The requirement: a systemd unit, with the mount ordered after it

> [!IMPORTANT]
> Run the server as a systemd service, and give the mount that uses its device `Requires=` and `After=` on that service. Run as a bare background process, a normal `systemctl reboot` under load loses the unmount's writeback every time and wedges the reboot about one time in five.

go-ublk's shutdown-storm test (`make vm-shutdown-storm`) reboots a VM normally while an ext4 filesystem on a ublk device is under `fio` load, and checks the next boot. The results, from 2026-08-22:

| Deployment | Reboots | Wedged reboots | I/O errors on the device per shutdown |
|---|---|---|---|
| Bare background process (started from an ssh session) | 14 | **3** | **10, every cycle** |
| systemd service, mount ordered `Requires=`/`After=` it | 9 | **0** | **0, every cycle** |

**Why.** A process started from a login session lives in that session's scope, and systemd tears session scopes down at the *start* of shutdown. The daemon dies while the filesystem above it still has dirty data. The unmount then fails, ext4 aborts its journal, and roughly one time in five the machine never finishes: `systemd-shutdown` reaches its final "syncing filesystems and block devices" step and waits forever on tasks that can no longer complete, and the host needs a hard power cycle. Stop order in systemd is the reverse of start order, so making the mount depend on the service flips it: the filesystem unmounts through a live daemon, and only then is the daemon stopped.

The I/O-error result is deterministic (10 against 0 on every cycle). The wedge result is the same mechanism but statistically weaker on its own (0 wedges in 9 cycles against a 21% rate). One part is not yet explained: why the daemon is reported as blocked "by coredump" in the wedged runs. See [Roadmap](/go-ublk/roadmap/).

## Units

These are the units the storm test uses for its passing configuration (`scripts/vm-shutdown-storm.sh`), with paths made generic.

```ini
# /etc/systemd/system/ublk-data.service
[Unit]
Description=go-ublk server for /dev/ublkb0
DefaultDependencies=no
After=local-fs.target
Before=umount.target
Conflicts=umount.target

[Service]
Type=simple
ExecStartPre=/sbin/modprobe ublk_drv
ExecStart=/usr/local/bin/my-ublk-server --device-id=0 --file=/var/lib/ublk/data.img
KillMode=mixed
TimeoutStopSec=90
Restart=no

[Install]
WantedBy=multi-user.target
```

```ini
# /etc/systemd/system/mnt-data.mount  (the file name must match Where=)
[Unit]
Description=Filesystem on /dev/ublkb0
Requires=ublk-data.service
After=ublk-data.service
DefaultDependencies=no
Before=umount.target
Conflicts=umount.target

[Mount]
What=/dev/ublkb0
Where=/mnt/data
Type=ext4
Options=defaults

[Install]
WantedBy=multi-user.target
```

What each part does:

- **`Requires=` and `After=` on the mount** are the load-bearing lines. They make systemd unmount before it stops the service.
- **`DefaultDependencies=no` with `Conflicts=`/`Before=umount.target`** keeps the service out of the ordinary early-shutdown sweep, so it stays alive until unmounting actually happens.
- **`After=local-fs.target`** is there because the test's backing file lives on a local filesystem. If yours lives elsewhere, order after that instead (`RequiresMountsFor=/var/lib/ublk` is the precise form). Do not put the ublk mount itself in `local-fs.target` (for example through a plain `/etc/fstab` line) while the service is ordered after `local-fs.target`; that is a dependency cycle.
- **`KillMode=mixed`** sends SIGTERM to the main process only, so the server can tear down its own device, and SIGKILL to everything after `TimeoutStopSec`.
- **A fixed device ID** (`DeviceParams.DeviceID = 0` behind `--device-id=0` here) keeps `/dev/ublkb0` stable, so the mount unit can name it. systemd makes a mount of a device path wait for the device to appear.
- The `[Install]` sections enable boot-time start. The storm test starts the units by hand, so they are not part of what was measured, but they do not affect stop order.

If you mount from `/etc/fstab` instead, the mount option `x-systemd.requires=ublk-data.service` adds the same `Requires=` and `After=`. An fstab mount is ordered before `local-fs.target`, though, so the service must not be ordered after it: replace `After=local-fs.target` with `RequiresMountsFor=` on the backing store's path. Add `nofail` so a failed device does not block boot. This variant has not been run through the storm test.

## Signal handling in the server

systemd stops the service with SIGTERM. The server's handler must:

1. Call `device.Close()`, which stops the device while the queues are still serving, drains, and deletes it. **Do not cancel the device's context first**; see [Device lifecycle](/go-ublk/lifecycle/#do-not-cancel-the-context-first).
2. Close the backend (for a buffered file backend, this is the final `fsync`).
3. Exit.

Handle **SIGINT, SIGTERM and SIGHUP** the same way. A Go program with no SIGHUP handler is killed by it, and logind sends SIGHUP to processes in a closing session; a server killed in the middle of `STOP_DEV` is the scenario under investigation for the wedge above. The examples do not handle SIGHUP yet.

Bound the wait. `Close` is normally fast, but a wedged backend can hold it up. The examples give it 15 seconds and then exit anyway, leaving a registered device to be reaped:

```go
sig := make(chan os.Signal, 1)
signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
<-sig

done := make(chan error, 1)
go func() { done <- device.Close() }()
select {
case err := <-done:
	if err != nil {
		log.Printf("close: %v", err)
	}
case <-time.After(15 * time.Second):
	log.Printf("close timed out; device %d left registered", device.ID)
}
backend.Close()
```

## Restarts and orphans

Without [user recovery](/guide/recovery/) (not implemented in go-ublk yet), a server that dies takes its block device with it: in-flight I/O fails, the filesystem on it sees errors, and the kernel removes `/dev/ublkbN`. Restarting the server creates a new device; it cannot rescue the old filesystem mount. That is why the unit above uses `Restart=no`: an automatic restart would bring up a fresh device under a mount that has already failed.

A crashed server also leaves its device registered. With a fixed ID, the next start's `ADD_DEV` fails with `EEXIST` until it is deleted. Reap it at startup, before creating the device:

```go
if ids, err := ublk.ListDevices(); err == nil && slices.Contains(ids, uint32(myID)) {
	_ = ublk.DeleteDevice(uint32(myID)) // left over from a previous crash
}
```

Only do this when nothing else can be serving that ID, which a single systemd unit per device guarantees.

## Choosing a kernel

| Kernel | Status |
|---|---|
| Ubuntu 24.04 `linux-hwe-7.0`, 7.0.0-30 and later | Verified on arm64 (7.0.0-30) and x86_64 (7.0.0-38). Carries one of the three upstream teardown fixes |
| Ubuntu 24.04 `linux-hwe-6.17`, 6.17.0-41 and later | Verified on arm64 (6.17.0-41) |
| Ubuntu `linux-aws-6.17`, 6.17.0-1020 | Verified on x86_64 |
| Ubuntu 6.17 generic from about -24 through -40 (crash confirmed on -35 and -40); 6.17.0-1019-aws | **Avoid.** The host oopses on the first `ADD_DEV` |
| Ubuntu 6.18 kernels | Check before use: the same packaging mistake was seen in that line |
| Mainline 7.1 and later | Has all three teardown fixes; not yet in go-ublk's verified set |

[Known kernel bugs](/guide/kernel-bugs/) has the details, and the [compatibility matrix](/reference/matrix/) has per-kernel test results.

## Checklist

- [ ] The server runs as a systemd service; every mount of its device has `Requires=` and `After=` on it.
- [ ] Fixed `DeviceID` per device; orphans with that ID are reaped at startup.
- [ ] `VolatileCache` matches the backend: true unless every completed write is already durable.
- [ ] SIGINT, SIGTERM and SIGHUP call `device.Close()`, then close the backend.
- [ ] `ublk_drv` loads at boot.
- [ ] The kernel is not one of the known-bad builds.
- [ ] `Options.Debug` is off.
- [ ] Memory budget includes `NumQueues × QueueDepth × MaxIOSize` of request buffers (virtual; resident as used).
- [ ] A daemon that creates and deletes devices repeatedly accounts for the open io_uring leak (about four rings per device cycle) until it is fixed.

## What has not been tested

The crash and power-fail tests cover a SIGKILLed daemon and a guest-level hard reset (`sysrq-b`) mid-write, with no lost, torn or misplaced blocks. A guest reset drops the guest's page cache but not the host's cache of the virtual disk, so a real **host power cut** remains untested. So does long-duration soak testing. See [Testing and compatibility](/go-ublk/testing/).
