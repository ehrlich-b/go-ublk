---
title: "Releases and changelog"
linkTitle: "Releases & changelog"
description: "Tagged releases of go-ublk, what changed, and the versioning policy."
weight: 90
---

go-ublk is pre-1.0. Releases are git tags on [GitHub](https://github.com/ehrlich-b/go-ublk/tags); `go get github.com/ehrlich-b/go-ublk@v0.1.0` pins one, and `@main` follows development.

## Versioning

Before 1.0, minor releases may change the API. Previous-version code keeps compiling where practical; obsolete fields remain documented no-ops for one release. Incorrect behavior is fixed. Notes identify changes, with integrity and teardown fixes called out as upgrade reasons.

## How releases are made

Release gates and recorded evidence:

1. `make release-check`: formatting, `go vet`, unit/race tests, and extended UAPI-decoder/queue-engine fuzzing.
2. [`test/matrix`](https://github.com/ehrlich-b/go-ublk/tree/main/test/matrix) boots ublk-suite across mainline 6.0-7.3-rc and major distribution kernels. Tests must pass where ublk is available, except documented kernel failures. Commit-pinned results in `site/data/matrix.json` populate the [compatibility matrix](/reference/matrix/).
3. Test shipped systemd units with a mounted filesystem, verifying writer, crash, and upgrade handoff; require zero I/O errors.
4. Tag the release. Tag pushes rerun CI and publish the version's changelog section through `scripts/release-notes.sh vX.Y.Z`; missing sections fail.

Release notes put integrity and teardown fixes first.

## v0.2.0 (2026-10-04)

A new I/O engine, recovery, full kernel control surface, and conformance suite across dozens of kernels. v0.1.0 code still compiles; behavior changes follow.

**Fixed**

- Pre-6.11 write-zeroes limits truncated lengths to 32 bits (#24): `blkdiscard -z -l 5G` left 4 GiB unchanged on 6.4-6.10, Ubuntu 6.8, and openSUSE Leap 15.6. Capped discard/zeroes limits now force splitting.
- Ring mappings leaked until exit (#17): four per device lifecycle, one per `ListDevices`.
- Teardown frees mappings only after queues and backend calls finish; otherwise memory stays allocated (#19).
- Control replies no longer target movable goroutine stacks (#21).
- Discards/zeroes at least 2 GiB returned `EIO` (#16); range operations now complete with 0.
- Examples handle SIGHUP and ignore SIGPIPE, the suspected shutdown-wedge trigger (#15).

**Behavior changes**

- Requests now run on separate goroutines, up to `QueueDepth` calls per queue. v0.1.0 serialized within queues; backends already needed cross-queue concurrency safety. `DeviceParams.Inline` restores serialization for RAM-speed backends.
- Context cancellation gracefully stops the device. v0.1.0 killed queues beneath I/O, wedging the control plane (Critical Bug #22, deterministic on Ubuntu 7.0.0-38).
- `Start` after `Stop` returns `ErrStopped`: restart oopsed Arch 7.2.8 and wedged 6.10-6.12 (#23).
- No `Options.Logger` means no output; v0.1.0 logged INFO to stderr.
- Backend `syscall.Errno` passes through on translating kernels instead of always becoming `EIO`. Panics fail that request with `EIO` while the server continues.
- Failed `STOP_DEV` leaves `Stop`/`Close` serving and returns an error (#18). `Options.StopTimeout` defaults to one minute, replacing 10 s.
- Missing features fail creation with their names instead of being silently dropped.
- `EnableUserCopy`/`EnableZeroCopy` now serve I/O; v0.1.0 accepted them but could not. `EnableFUA` works with `FUABackend`/`Handler`. `EnableIoctlEncode` and `DeviceName` are documented no-ops.

**New**

- Recovery: `DeviceParams.Recovery` (`RecoveryReissue`/`RecoveryQueue`/`RecoveryFailIO`), `Device.Detach`, `ublk.Recover`, and Tag/`FindDevices`. Detach drains with `QUIESCE_DEV` on 6.16+, except batch devices. `Recover` reads geometry and zero-copy/batch/zoned/integrity modes; supply the backend. Default/batch/integrity SIGKILL and live-handoff tests verify every block without writer errors; Queue holds I/O and FailIO fails immediately. Under systemd/ext4, verified fio handoffs take about 1-2.5 s with zero errors. `ublk-loop -recovery` ships Restart=always, SIGUSR2 upgrades, and mount ordering after login-session shutdown but before server stop.
- Zero copy: `EnableZeroCopy` with `ZeroCopyBackend` (6.15+), using kernel fixed-buffer file I/O, fdatasync, and fallocate; automatic registration on 6.16+.
- `SharedMemoryZeroCopy`/`RegisterSharedMemory` (7.1+) and `IODescSize` (7.3+) complete feature coverage of the 7.3-rc5 UAPI.
- `BatchIO` (7.0+): up to 128 requests per multishot completion via provided-buffer rings, bulk commits, and copy/user-copy/zero-copy support.
- `DeviceParams.Integrity` (7.0+): metadata with optional T10-DIF, IP, or NVMe CRC64 generated/verified by the kernel, exposed through `Request.Integrity` or `IntegrityBackend`. Corrupted-tuple tests fail exactly that block.
- `EnableZoned` (6.6+): host-managed devices through `Handler`, `Request.ReportZones`, and `CompleteZoneAppend`.
- `Handler`/`Request` expose all operations/flags with completion from any goroutine; `HandlerDiscard`/`HandlerWriteZeroes` advertise optional operations.
- `Device.Done`/`Err`/`Wait` and state failed expose unexpected queue failures (#20).
- `EnableUnprivileged` prefixes device-control payloads with the char path and waits for ownership via udev. `examples/ublk-chown` supplies the helper/rule; tests verify I/O as nobody.
- `Device.Resize` (`UPDATE_SIZE`, 6.16+), `SafeStop` (`TRY_STOP_DEV`, 7.0+), `NoPartitionScan` (7.0+), `ThreadsPerQueue` (`PER_IO_DAEMON`, 6.16+), `NeedGetData`, and `PhysicalBlockSize`/`IOMinSize`/`IOOptSize`/`DMAAlignment` hints.
- `GET_QUEUE_AFFINITY` pins threads to routing CPUs by default. `ListDevices` uses sysfs, finding IDs above 63. `DeleteDeviceAsync` uses `DEL_DEV_ASYNC` (6.9+).
- `ublk.Probe`/`GetDeviceInfo`/Errno, `Device.KernelInfo`/Features, and `FUABackend`.
- About 60 conformance tests in ublk-suite (`make suite`), test/matrix for mainline/distribution kernels, make release-check, release workflow, and this site.
- Internals: allocation-free io_uring core, fake-kernel engine tests including a falsified-without-fix lost-wakeup test, 7.3-rc5 control commands with C-fixture layout parity, and off-heap control buffers.

**Verification**

- Release candidate `4d9712f` has 47 passing [matrix rows](/reference/matrix/), covering unit/large-I/O tests, integrity sweep, and applicable conformance tests. Later release commits change documentation/CI only. Kernels include mainline 6.4-7.3-rc3; Ubuntu 22.04 HWE, 24.04 GA/HWE 6.11/6.14/6.17/7.0 and AWS/Azure/GCP 7.0, 25.04/25.10/26.04; Debian 12 backports/13; Fedora 42/43/44; Arch/LTS; openSUSE Tumbleweed/Leap 15.6; Oracle UEK8; CentOS Stream/AlmaLinux/Rocky 10 with io_uring enabled. Default-disabled io_uring rows and the broken Ubuntu 6.17.0-40 control fail as documented; findings are [kernel bugs](/guide/kernel-bugs/), with no recorded product failures.
- `make release-check`: formatting, go vet, unit/race tests, 60 s per fuzz target.
- Systemd/ext4 recovery with verified fio: crash/upgrade handoffs in 1-2 s, zero I/O errors.
- Eight loaded reboots with shipped units, ext4, and about 300 MB dirty data: no errors, clean e2fsck. Before adding Before=user.slice, 3/5 reboots lost writeback.
- Four-hour Ubuntu 7.0.0-38 soak: 23 crash/upgrade handoffs, every block verified, no memory/fd growth, clean e2fsck.

## v0.1.0 (2026-09-30)

Initial release:

**Correctness**

- Correct fixed-stride descriptor mappings; later queues previously read queue 0, corrupting data and hanging unkillably.
- Use 512-byte sectors throughout, fixing capacity/offsets for 4096-byte logical blocks. Validate block sizes from 512 bytes through page size.
- Discards larger than request buffers no longer panic; only reads/writes use buffers.
- Send `ReadOnly`/`Rotational`/`VolatileCache` and discard limits, making flush/discard reachable.
- Default `VolatileCache` to true for durability.
- Honor backend results; fail short reads/writes.
- Size per-tag buffers from negotiated `MaxIOSize` (default 1 MiB), supporting full-size requests.

**Lifecycle**

- `STOP_DEV` drains busy devices through serving queues with bounded, signal-tolerant control waits; 50+ loaded teardowns had no hangs/leaks.
- Join queues and release char-device references before `DEL_DEV`.
- Unwind failed startup and honor negotiated queue counts.
- Add public `ListDevices`/`DeleteDevice` for orphan cleanup.
- Wire `WriteZeroesBackend` to `UBLK_IO_OP_WRITE_ZEROES`; remove unused `SyncBackend`/`StatBackend`/`ResizeBackend`.
- Route diagnostics to `Options.Logger`; add `Options.Debug`.

**Examples and testing**

- `ublk-loop` exports files like losetup; `ublk-mem` adds compression. Both use only the public API.
- Integrity sweep, churn, crash/power-fail oracles, shutdown storm, and arm64/x86_64 verification on 6.17/7.0. See [testing](/go-ublk/testing/).