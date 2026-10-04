---
title: "Releases and changelog"
linkTitle: "Releases & changelog"
description: "Tagged releases of go-ublk, what changed, and the versioning policy."
weight: 90
---

go-ublk is pre-1.0. Releases are git tags on [GitHub](https://github.com/ehrlich-b/go-ublk/tags); `go get github.com/ehrlich-b/go-ublk@v0.1.0` pins one, and `@main` follows development.

## Versioning

Until 1.0 the public API can change between minor versions, and the project deliberately carries no compatibility shims: when something is wrong it is fixed, not deprecated. Each release notes what changed. Bug fixes that affect data integrity or teardown are called out explicitly, because they are the reason to upgrade.

## Unreleased

Changes on `main` since v0.1.0:

- **Fixed: discards and write-zeroes of 2 GiB or more failed with `EIO`.** Completions reported the request length as a signed 32-bit byte count, which wraps negative at 2 GiB, and the kernel failed the request after the backend had done the work. `mkfs.ext4` on any device over 2 GiB hit it through its whole-device discard. Range operations now complete with 0. Verified on arm64 7.0.0-30 and x86_64 7.0.0-38.
- `EnableUserCopy` is now rejected at creation with an error matching `ErrNotImplemented`, instead of producing a device that could not serve I/O.
- Control-plane hardening: the `SET_PARAMS` buffer is kept reachable for the duration of the command; `GET_PARAMS` responses are decoded using the length the kernel actually returned; kernel errnos are preserved through the error chain so `errors.Is(err, syscall.EPERM)` and friends work.
- A large set of unit tests: the queue state machine, request dispatch, the mmap stride, the runner lifecycle, ring index wraparound, real io_uring round trips, ABI layout of 128-byte SQEs and 32-byte CQEs, metrics percentiles, and both example backends.
- Documentation of the open lifecycle defects found by the 2026-10-03 audit (see [Roadmap](/go-ublk/roadmap/)).

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
