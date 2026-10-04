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

The repository ships units for the `ublk-loop` example in [`examples/systemd/`](https://github.com/ehrlich-b/go-ublk/tree/main/examples/systemd); adapt them for your server. They are the configuration the shutdown-storm test validated, plus recovery.

```ini
# /etc/systemd/system/ublk-loop@.service — instance = device ID
[Unit]
Description=go-ublk loop device /dev/ublkb%i
DefaultDependencies=no
After=local-fs.target systemd-modules-load.service
Before=umount.target
Conflicts=umount.target

[Service]
Type=notify
ExecStartPre=/sbin/modprobe ublk_drv
ExecStart=/usr/local/bin/ublk-loop -id=%i -file=/var/lib/ublk/%i.img -recovery
KillMode=mixed
TimeoutStopSec=90
Restart=always
RestartSec=200ms

[Install]
WantedBy=multi-user.target
```

```ini
# /etc/systemd/system/srv-ublk0.mount  (the file name must match Where=)
[Unit]
Description=Filesystem on go-ublk device /dev/ublkb0
Requires=ublk-loop@0.service
After=ublk-loop@0.service
DefaultDependencies=no
Before=umount.target user.slice
Conflicts=umount.target

[Mount]
What=/dev/ublkb0
Where=/srv/ublk0
Type=ext4
Options=defaults

[Install]
WantedBy=multi-user.target
```

What each part does:

- **`Requires=` and `After=` on the mount** are the load-bearing lines. They make systemd unmount before it stops the service.
- **`Before=user.slice` on the mount** makes systemd stop every login session and user service before it unmounts. Without it, a process in a session with a file open on the filesystem (an admin's shell, a job started by hand) makes the unmount fail with "target is busy"; systemd then stops the server anyway, and the filesystem's writeback fails against a device that is gone. On a VM rebooted under write load this happened in 3 of 5 reboots without the line and 0 of 5 with it. Services that use the filesystem need the same treatment from their side: give them `RequiresMountsFor=/srv/ublk0`, which orders them to stop before the unmount. At boot the line also means a login waits for the mount if both start together.
- **`DefaultDependencies=no` with `Conflicts=`/`Before=umount.target`** keeps the service out of the ordinary early-shutdown sweep, so it stays alive until unmounting actually happens.
- **`Type=notify`**: `ublk-loop` sends `READY=1` (a dozen lines of Go over `$NOTIFY_SOCKET`, no dependency) once `/dev/ublkbN` is serving, so the mount starts only when the device exists. Do the same in your server, or the mount may race the device.
- **`After=local-fs.target`** is there because the backing file lives on a local filesystem. If yours lives elsewhere, order after that instead (`RequiresMountsFor=/var/lib/ublk` is the precise form). Do not put the ublk mount itself in `local-fs.target` (for example through a plain `/etc/fstab` line) while the service is ordered after `local-fs.target`; that is a dependency cycle.
- **`KillMode=mixed`** sends SIGTERM to the main process only, so the server can tear down its own device, and SIGKILL to everything after `TimeoutStopSec`.
- **A fixed device ID** (`-id=%i`, i.e. `DeviceParams.DeviceID`) keeps `/dev/ublkbN` stable, so the mount unit can name it.
- **`-recovery` with `Restart=always`**: see the next section. An explicit stop — shutdown, `systemctl stop` — is never restarted.

If you mount from `/etc/fstab` instead, the mount options `x-systemd.requires=ublk-loop@0.service,x-systemd.before=user.slice` add the same `Requires=`, `After=` and `Before=user.slice`. An fstab mount is ordered before `local-fs.target`, though, so the service must not be ordered after it: replace `After=local-fs.target` with `RequiresMountsFor=` on the backing store's path. Add `nofail` so a failed device does not block boot. This variant has not been run through the storm test.

## Crashes and upgrades without downtime

With [user recovery](/go-ublk/lifecycle/#detach-and-recover) (`DeviceParams.Recovery = RecoveryReissue`, which `ublk-loop -recovery` sets), the block device — and the filesystem mounted on it — outlives the server process:

- **Crash.** If the server dies, the kernel holds new I/O and requeues what was in flight. systemd restarts the service (`Restart=always`); the new process finds the device (`GetDeviceInfo`, or `FindDevices` by tag) and takes it over with `Recover`, and the held I/O completes. Applications see a pause, not an error.
- **Upgrade.** Install the new binary, then `systemctl kill -s SIGUSR2 ublk-loop@0`. The running server calls `Detach` — which drains in-flight I/O with `QUIESCE_DEV` on 6.16+ — and exits; systemd starts the new binary, which recovers the device.

Measured on Ubuntu 24.04 with kernel 7.0.0-38 (emulated x86_64), with ext4 mounted on the device and `fio --verify=crc32c` writing through it: two upgrade handoffs (2.2 s and 2.4 s, including systemd's 200 ms restart delay) and one SIGKILL crash (1.1 s) in a single run, **zero I/O errors**, fio's verification clean, and the mount active throughout.

The startup logic in `ublk-loop` is the pattern to copy:

```go
info, err := ublk.GetDeviceInfo(id)
switch {
case err != nil: // no such device: first start
	dev, err = ublk.CreateAndServe(ctx, params, opts)
case info.Features.Has(ublk.FeatureUserRecovery):
	dev, err = ublk.Recover(ctx, id, params, opts) // crashed or detached predecessor
default: // leftover from a server without recovery: cannot be taken over
	_ = ublk.DeleteDevice(id)
	dev, err = ublk.CreateAndServe(ctx, params, opts)
}
```

`Recover` waits (up to 30 seconds) for the kernel to finish releasing the previous process, so it can run the moment systemd restarts the service. A reissued write may reach the backend twice, which block semantics allow; a backend must not depend on seeing each write exactly once.

## Signal handling in the server

systemd stops the service with SIGTERM. Either cancel the serving context — wire `signal.NotifyContext` into `CreateAndServe`, and cancellation stops the device gracefully — or call `device.Close()` from the handler. Then close the backend (for a buffered file backend, the final `fsync`) and exit.

Handle **SIGINT, SIGTERM and SIGHUP**. A Go program with no SIGHUP handler is killed by it, and logind sends SIGHUP to processes in a closing session — a server killed while `STOP_DEV` is draining strands that I/O. Ignore SIGPIPE too, so a vanished log pipe cannot kill the server. The examples do all of this.

`Stop` and `Close` are bounded by `Options.StopTimeout` (default one minute). If the bound is hit the call returns an error and the device keeps serving; decide whether to retry or exit and leave the device for `Recover` or `DeleteDevice`.

## Restarts without recovery

A server created without a recovery mode takes its block device with it when it dies: in-flight I/O fails, the filesystem on it sees errors, and the kernel removes `/dev/ublkbN`. The device stays registered until it is deleted, and with a fixed ID the next start's `ADD_DEV` fails with `EEXIST` until then. Reap it at startup, as the `default:` branch above does. Use `Restart=no` for such a server: an automatic restart would bring up a fresh device under a mount that has already failed.

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

- [ ] The server runs as a systemd service; every mount of its device has `Requires=` and `After=` on it, and `Before=user.slice`.
- [ ] Every service that uses the filesystem has `RequiresMountsFor=` on its mount point.
- [ ] Fixed `DeviceID` per device; orphans with that ID are reaped at startup.
- [ ] `VolatileCache` matches the backend: true unless every completed write is already durable.
- [ ] SIGINT, SIGTERM and SIGHUP call `device.Close()`, then close the backend.
- [ ] `ublk_drv` loads at boot.
- [ ] The kernel is not one of the known-bad builds.
- [ ] `Options.Debug` is off.
- [ ] Memory budget includes `NumQueues × QueueDepth × MaxIOSize` of request buffers (virtual; resident as used).

## What has not been tested

The crash and power-fail tests cover a SIGKILLed daemon and a guest-level hard reset (`sysrq-b`) mid-write, with no lost, torn or misplaced blocks. A guest reset drops the guest's page cache but not the host's cache of the virtual disk, so a real **host power cut** remains untested. Soak testing has run for hours (see `make vm-soak`), not days. See [Testing and compatibility](/go-ublk/testing/).
