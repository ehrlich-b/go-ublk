---
title: "Deployment"
linkTitle: "Deployment"
description: "Running a go-ublk server in production: the systemd unit and mount ordering it requires, signals, teardown, and kernel selection."
weight: 50
---

Unmount a ublk filesystem while its server still runs: unmount writeback needs that process. Killing it first can lose data and hang reboot.

## The requirement: a systemd unit, with the mount ordered after it

> [!IMPORTANT]
> Run the server as a systemd service with mount Requires=/After= dependencies. In the tests below, bare background servers lost final writeback every reboot and hung about one in five.

`make vm-shutdown-storm` reboots a VM during ext4 fio load and checks the next boot. Results from 2026-08-22:

| Deployment | Reboots | Wedged reboots | I/O errors on the device per shutdown |
|---|---|---|---|
| Bare background process (started from an ssh session) | 14 | **3** | **10, every cycle** |
| systemd service, mount ordered `Requires=`/`After=` it | 9 | **0** | **0, every cycle** |

systemd kills login-session scopes early in shutdown, before dirty filesystems unmount. A server launched there dies first; unmount fails, ext4 aborts its journal, and `systemd-shutdown` can wait forever at "syncing filesystems and block devices", requiring a hard power cycle. systemd reverses start order at shutdown: a mount depending on the service unmounts through the live server before stopping it.

I/O errors were 10 versus 0 every cycle. Reboot hangs provide weaker evidence: 0/9 versus 21%. The daemon's "by coredump" blocked state remains unexplained; see [roadmap](/go-ublk/roadmap/).

## Units

Adapt the [`ublk-loop` units](https://github.com/ehrlich-b/go-ublk/tree/main/examples/systemd), validated by the shutdown storm and extended with recovery:

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

Unit settings:

- Mount Requires=/After= orders unmount before service stop.
- `Before=user.slice` stops login sessions/user services before unmount. Otherwise open files cause "target is busy", and systemd stops the server beneath the mount. Loaded-reboot tests failed 3/5 times without this line, 0/5 with it. Consumer services need `RequiresMountsFor=/srv/ublk0`. At boot, concurrent login startup waits for the mount.
- `DefaultDependencies=no` plus Conflicts=/Before=umount.target keeps the server out of early shutdown until unmount.
- `Type=notify` waits for READY=1 after `/dev/ublkbN` serves, preventing mount races. ublk-loop sends it over `$NOTIFY_SOCKET` in about twelve dependency-free Go lines.
- `After=local-fs.target` waits for local backing storage. For other storage, use its dependency, e.g. `RequiresMountsFor=/var/lib/ublk`. Putting the ublk mount in local-fs.target via plain fstab creates a cycle with a server ordered after that target.
- `KillMode=mixed` sends SIGTERM only to the main process, then SIGKILL to all after TimeoutStopSec.
- `-id=%i` (`DeviceParams.DeviceID`) gives the mount a stable `/dev/ublkbN`.
- `-recovery` with Restart=always enables the handoffs below. Explicit shutdown or `systemctl stop` never restarts the service.

For `/etc/fstab`, `x-systemd.requires=ublk-loop@0.service,x-systemd.before=user.slice` supplies equivalent mount dependencies. Because fstab mounts precede local-fs.target, replace the server's After=local-fs.target with RequiresMountsFor= on its backing path. Add `nofail` to avoid blocking boot. This variant has not passed through the storm test.

## Crashes and upgrades without downtime

[RecoveryReissue](/go-ublk/lifecycle/#detach-and-recover), enabled by `ublk-loop -recovery`, preserves the device and mount:

- On crash, the kernel holds new I/O and requeues outstanding requests. Restart=always launches a replacement, which finds the device through GetDeviceInfo or tagged FindDevices and calls Recover. Applications pause while I/O resumes.
- To upgrade, install the binary and run `systemctl kill -s SIGUSR2 ublk-loop@0`. The server Detaches, draining with QUIESCE_DEV on 6.16+ except batch devices, then exits; systemd starts the replacement for recovery.

On Ubuntu 24.04/7.0.0-38, emulated x86_64 with ext4 and `fio --verify=crc32c`: two upgrades took 2.2/2.4 s including 200 ms restart delay; one SIGKILL recovery took 1.1 s. This run had zero I/O errors, clean verification, and no unmount.

ublk-loop startup pattern:

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

Recover waits up to 30 seconds for previous-process release, so it can run immediately on restart. Replay may write twice; backends must tolerate that.

## Signal handling in the server

On systemd SIGTERM, cancel a `signal.NotifyContext` passed to CreateAndServe for graceful Stop, then Close; alternatively call `device.Close()` from the handler. After successful device closure, close the backend (final fsync for buffered files) and exit.

Handle SIGINT, SIGTERM, and SIGHUP. logind sends SIGHUP when sessions close; without a handler it kills Go servers, potentially stranding STOP_DEV drains. Ignore SIGPIPE so a lost log pipe cannot kill the server. The examples handle these signals.

Options.StopTimeout defaults to one minute. STOP failure leaves serving resources intact; DEL timeout occurs after stop, waiting for references. Handle errors by retrying or exiting for recovery/orphan cleanup as appropriate.

## Restarts without recovery

Without recovery, death fails I/O, damages the mounted filesystem's state, and removes `/dev/ublkbN`. Registration remains: fixed-ID startup gets EEXIST until cleanup, as in the example's default branch. Use Restart=no; automatic restart would put a fresh device beneath an already-failed mount.

## Choosing a kernel

| Kernel | Status |
|---|---|
| Ubuntu 24.04 `linux-hwe-7.0`, 7.0.0-30 and later | Verified on arm64 (7.0.0-30) and x86_64 (7.0.0-38). Carries one of the three upstream teardown fixes |
| Ubuntu 24.04 `linux-hwe-6.17`, 6.17.0-41 and later | Verified on arm64 (6.17.0-41) |
| Ubuntu `linux-aws-6.17`, 6.17.0-1020 | Verified on x86_64 |
| Ubuntu 6.17 generic from about -24 through -40 (crash confirmed on -35 and -40); 6.17.0-1019-aws | **Avoid.** The host oopses on the first `ADD_DEV` |
| Ubuntu 6.18 kernels | Check before use: the same packaging mistake was seen in that line |
| Mainline 7.1 and later | Has all three teardown fixes; the compatibility matrix lists passing builds |

[Known kernel bugs](/guide/kernel-bugs/) has the details, and the [compatibility matrix](/reference/matrix/) has per-kernel test results.

## Checklist

- [ ] Service-backed mounts have Requires=/After= on the server and Before=user.slice.
- [ ] Consumer services have RequiresMountsFor= on the mount point.
- [ ] Fixed DeviceID; startup handles orphaned IDs.
- [ ] VolatileCache is true unless completed writes are durable.
- [ ] SIGINT/SIGTERM/SIGHUP close the device successfully before closing the backend.
- [ ] ublk_drv loads at boot; the kernel is not a known-bad build.
- [ ] Options.Debug is off.
- [ ] Budget `NumQueues × QueueDepth × MaxIOSize` buffer address space, resident as touched.

## What has not been tested

SIGKILL and guest `sysrq-b` mid-write tests found no lost, torn, or misplaced blocks. Guest resets retain the host's virtual-disk cache, leaving real host power cuts untested. `make vm-soak` has run for hours, not days. See [testing](/go-ublk/testing/).