---
title: "Releases and changelog"
linkTitle: "Releases & changelog"
description: "Tagged releases of go-ublk, what changed, and the versioning policy."
weight: 90
---

go-ublk is pre-1.0. Releases are git tags on [GitHub](https://github.com/ehrlich-b/go-ublk/tags); `go get github.com/ehrlich-b/go-ublk@v0.1.0` pins one, and `@main` follows development.

## Versioning

Until 1.0 the public API can change between minor versions. Code written against the previous minor version keeps compiling where that costs little (fields that no longer do anything are kept as documented no-ops for a release); behavior that was wrong is fixed, not preserved. Each release notes what changed. Bug fixes that affect data integrity or teardown are called out explicitly, because they are the reason to upgrade.

## How releases are made

Every tag passes the same gates, and the evidence ships with it:

1. **`make release-check`** — formatting, `go vet`, unit tests, the race detector, and a longer fuzzing pass over the UAPI decoders and the queue engine.
2. **The kernel matrix** — `ublk-suite` booted under every kernel in [`test/matrix`](https://github.com/ehrlich-b/go-ublk/tree/main/test/matrix): mainline 6.0–7.3-rc and the current kernels of the major distributions. A release requires every test to pass on every kernel that has ublk, except failures traced to a documented kernel bug. The results for the tagged commit are committed as `site/data/matrix.json` and published on the [compatibility matrix](/reference/matrix/).
3. **Recovery under systemd** — the shipped units, a mounted filesystem, a verifying writer, a crash and an upgrade handoff, zero I/O errors.
4. The tag is cut, and the GitHub release body is this page's section for it (`scripts/release-notes.sh vX.Y.Z`).

Bug fixes that affect data integrity or teardown are listed first in a release's notes.

## v0.2.0 (unreleased)

A rebuild of everything under the public API, aimed at production use: a new I/O engine, user recovery, the whole kernel control surface, and a conformance suite that runs under dozens of kernels. Code written for v0.1.0 compiles unchanged; the behavior changes below are the ones to read.

**Behavior changes**

- **Backends run concurrently within a queue.** Each request runs on its own goroutine, so up to `QueueDepth` calls per queue are in flight (v0.1.0 called the backend one request at a time per queue). Backends already had to be safe for concurrent use across queues; now latency-bound backends scale. `DeviceParams.Inline` restores the old inline behavior for RAM-speed backends.
- **Cancelling the serving context stops the device gracefully** (like `Stop`). In v0.1.0 it killed the queues under in-flight I/O, which could wedge the kernel's control plane (Critical Bug #22, deterministic on Ubuntu 7.0.0-38).
- **`Start` after `Stop` returns `ErrStopped`.** The kernel cannot reliably restart a stopped device; it oopsed Arch's 7.2.8 and wedged 6.10–6.12 in testing (#23).
- **The library is silent by default.** With no `Options.Logger` nothing is written; v0.1.0 logged at INFO to stderr.
- **Errnos pass through.** A backend error that is a `syscall.Errno` reaches the application as that errno (on kernels that translate them) instead of always `EIO`. A panicking backend fails the request with `EIO` instead of crashing the server.
- **`Stop`/`Close` keep serving if `STOP_DEV` fails** and return the error (#18); the wait is `Options.StopTimeout` (default one minute) instead of a fixed 10 s.
- **Missing kernel features fail creation** with an error listing them, instead of being silently dropped.
- `EnableUserCopy` and `EnableZeroCopy` work (v0.1.0 accepted both but produced devices that could not serve I/O); `EnableFUA` works with a `FUABackend` or `Handler`; `EnableIoctlEncode` and `DeviceName` are documented no-ops.

**New**

- **User recovery**: `DeviceParams.Recovery` (`RecoveryReissue`, `RecoveryQueue`, `RecoveryFailIO`), `Device.Detach` (drains with `QUIESCE_DEV` on 6.16+), `ublk.Recover`, and `DeviceParams.Tag` with `FindDevices`. The conformance suite SIGKILLs a server mid-write and recovers it from another process, and hands a device off live between processes; the writer sees no error and every block verifies. Under systemd with ext4 mounted and a verifying fio running, crash and upgrade handoffs complete in about 1–2.5 s with zero I/O errors. `ublk-loop -recovery` and its shipped systemd unit (`Restart=always`, SIGUSR2 upgrade) demonstrate it.
- **Zero copy** (`EnableZeroCopy` with a `ZeroCopyBackend`, kernel 6.15+): file-backed devices are served entirely in the kernel with io_uring fixed-buffer reads and writes, `fdatasync`, and `fallocate`, using automatic buffer registration on 6.16+.
- **Shared-memory zero copy** (`SharedMemoryZeroCopy` with `RegisterSharedMemory`, kernel 7.1+) and **larger descriptors** (`IODescSize`, kernel 7.3+). With these, every feature in the 7.3-rc5 UAPI is implemented.
- **Batch I/O** (`BatchIO`, kernel 7.0+): requests fetched up to 128 per completion through a multishot command and a provided-buffer ring, and committed many per command.
- **Integrity metadata** (`DeviceParams.Integrity`, kernel 7.0+): per-block metadata with optional T10-DIF, IP or NVMe CRC64 protection that the kernel generates and verifies; served through `Request.Integrity` or an `IntegrityBackend`. The suite checks that a corrupted tuple fails exactly that block's read.
- **Zoned devices** (`EnableZoned`, kernel 6.6+): host-managed zoned devices served by a `Handler`, with `Request.ReportZones` and `CompleteZoneAppend`.
- **`Handler` and `Request`**: a raw request interface exposing every operation and flag, completable from any goroutine; `HandlerDiscard`/`HandlerWriteZeroes`.
- `Device.Done`, `Err`, `Wait` and a `failed` state: a queue that hits something unexpected now fails the device visibly instead of dying silently (#20).
- `Device.Resize` (`UPDATE_SIZE`, 6.16+), `SafeStop` (`TRY_STOP_DEV`, 7.0+), `NoPartitionScan` (7.0+), `ThreadsPerQueue` (`PER_IO_DAEMON`, 6.16+), `NeedGetData`, `EnableUnprivileged`, geometry hints (`PhysicalBlockSize`, `IOMinSize`, `IOOptSize`, `DMAAlignment`).
- `ublk.Probe`, `GetDeviceInfo`, `Device.KernelInfo`, `Device.Features`, `ublk.Errno`, `FUABackend`.
- Internally: a general allocation-free io_uring core, an I/O engine with a fake-kernel test suite (including a deterministic lost-wakeup test proven to fail without its fix), every control command of the 7.3-rc5 UAPI with C-fixture layout parity, and off-heap control buffers.

**Fixed**

- **The ring leak (#17)**: every io_uring the library created stayed mapped until exit — four per device lifecycle, one per `ListDevices`.
- **Use-after-unmap at teardown (#19)**: memory is freed only after every queue has exited and no backend call holds a request; otherwise it is deliberately leaked.
- **Unpinned control buffers (#21)**: some control replies were written into goroutine stacks that could move.
- **Discards and write-zeroes of 2 GiB or more failed with `EIO`** (#16): range operations now complete with 0.
- The examples handle SIGHUP and ignore SIGPIPE (the likely trigger of the shutdown wedge in #15).

**Verification**: the [compatibility matrix](/reference/matrix/) lists the kernels and distributions this release was run on.

## v0.1.0 (2026-09-30)

The first tag. Highlights, roughly in the order they landed:

**Correctness**

- Multi-queue devices map each queue's descriptor array at the kernel's fixed stride. Before this, every queue after the first read queue 0's descriptors, causing silent data corruption and unkillable hangs on multi-queue devices.
- Sectors are counted in 512-byte units everywhere, so devices with 4096-byte logical blocks report the right capacity and do I/O at the right offsets. Block sizes from 512 bytes to the page size are accepted and validated against the kernel's rules.
- A discard larger than the request buffer no longer panics the server; only reads and writes use request buffers.
- Device attributes (`ReadOnly`, `Rotational`, `VolatileCache`) and discard limits are actually sent to the kernel; before, flush and discard were unreachable.
- `VolatileCache` defaults to true, the fail-safe choice for durability.
- Backend transfer results are honored: short reads and writes fail the request.
- Per-tag request buffers are sized from the negotiated `MaxIOSize` (default 1 MiB), so full-size requests work.

**Lifecycle**

- Graceful stop of a busy device: `STOP_DEV` runs while the queues still serve, and the control-plane wait is bounded and robust to signal interruptions. 50+ teardown-under-load cycles without a hang or leak.
- `DEL_DEV` no longer hangs: queue goroutines are joined and every char-device reference released before deletion.
- Failed startup tears down cleanly instead of wedging the process; the queue count the kernel returns is honored.
- `ListDevices` and `DeleteDevice` in the public API for reaping leaked devices.
- `WriteZeroesBackend` wired to `UBLK_IO_OP_WRITE_ZEROES`; the unused `SyncBackend`, `StatBackend` and `ResizeBackend` interfaces removed.
- `Options.Logger` receives the library's internal diagnostics; `Options.Debug` added.

**Examples and testing**

- `ublk-loop`, a losetup-style file exporter, and a compressed mode for `ublk-mem`, both on the public API only.
- Integrity sweep, teardown churn, crash and power-fail oracles, the shutdown-storm test, and verified runs on arm64 and x86_64 with kernels 6.17 and 7.0. See [Testing and compatibility](/go-ublk/testing/).
