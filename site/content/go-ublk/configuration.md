---
title: "Configuration reference"
linkTitle: "Configuration"
description: "Every field of DeviceParams and Options: defaults, valid ranges, what each one sends to the kernel, and which kernel each feature needs."
weight: 30
---

A device is configured by a `DeviceParams` value, normally obtained from `DefaultParams(backend)` and then adjusted, and an optional `*Options`. Both are read once, at `Create` or `CreateAndServe` (or `Recover`).

```go
params := ublk.DefaultParams(backend)
params.NumQueues = 4
params.QueueDepth = 64
params.LogicalBlockSize = 4096
params.Recovery = ublk.RecoveryReissue

device, err := ublk.CreateAndServe(ctx, params, &ublk.Options{Logger: myLogger})
```

Invalid values are rejected before anything is sent to the kernel, with a plain error describing the field. Features the running kernel lacks are rejected before the device is created, with an error matching `syscall.EOPNOTSUPP` that names them; go-ublk never quietly creates a device without a feature you asked for. `ublk.Probe()` tells you up front what the kernel has.

## DeviceParams

### What serves the device

| Field | Default | Effect |
|---|---|---|
| `Backend` | required (or `Handler`) | The storage, as `ReadAt`/`WriteAt`/`Size`/`Flush`. Its optional interfaces decide whether discard, write-zeroes and FUA are advertised. See [Writing a backend](/go-ublk/backends/) |
| `Handler` | nil | Instead of a `Backend`: receives each raw `*Request` (operation, flags, offset, buffer) and completes it, possibly asynchronously. Exposes every operation and flag the kernel sends |
| `Size` | 0 | Device size in bytes. Required with `Handler`; with a `Backend`, 0 means `Backend.Size()` |
| `HandlerDiscard`, `HandlerWriteZeroes` | false | With a `Handler`, advertise discard and write-zeroes |
| `Inline` | false | Run the backend on each queue's own I/O thread instead of a goroutine per request. Lowest latency, but one request at a time per queue: only for backends that never block (RAM) |

### Geometry and limits

| Field | Default | Valid | Effect |
|---|---|---|---|
| `NumQueues` | 0 | 0 to 4096 | Hardware queues, each with its own I/O thread; 0 means `runtime.NumCPU()`. The kernel caps the count at the number of CPUs; read the result with `Device.NumQueues()` |
| `QueueDepth` | 128 | 1 to 4096 | Requests the kernel can have outstanding per queue. Unless `Inline`, up to this many backend calls run concurrently per queue |
| `ThreadsPerQueue` | 0 | ≥ 0 | Split each queue's tags across this many I/O threads, each with its own io_uring (`UBLK_F_PER_IO_DAEMON`, 6.16+) |
| `LogicalBlockSize` | 512 | power of two, 512 to the page size | Logical block size |
| `PhysicalBlockSize`, `IOMinSize`, `IOOptSize` | 0 | 0, or a power of two ≥ `LogicalBlockSize` | Geometry hints; 0 derives physical and minimum from the logical size and leaves optimal unset |
| `DMAAlignment` | 0 | alignment mask, e.g. 511 | Buffer alignment the device needs (`UBLK_PARAM_TYPE_DMA_ALIGN`, 6.15+) |
| `MaxIOSize` | 1 MiB | page-aligned, ≥ one page, a multiple of `LogicalBlockSize`, ≤ 2<sup>31</sup>-1 | Largest request; the kernel splits bigger ones. Each queue maps `QueueDepth × MaxIOSize` bytes of buffers |

### Attributes

| Field | Default | Effect |
|---|---|---|
| `VolatileCache` | **true** | Advertises a write-back cache, so the kernel sends `FLUSH` and your `Flush` runs. Set false only if every completed write is already durable. See [durability](/go-ublk/backends/#flush-and-durability) |
| `EnableFUA` | false | Advertises Force Unit Access (with `VolatileCache`), so writes needing durability arrive flagged instead of as write + flush. Honored only with a `Handler` or a backend implementing `FUABackend`; otherwise ignored, and the block layer emulates FUA with a flush |
| `ReadOnly` | false | Creates a read-only disk; the block layer rejects writes before they reach the backend |
| `Rotational` | false | Marks the device rotational, which changes scheduler and filesystem heuristics |
| `NoPartitionScan` | false | Don't scan for a partition table at start (`UBLK_F_NO_AUTO_PART_SCAN`, 7.0+) |

### Discard and write-zeroes

Used only if the backend implements `DiscardBackend` or `WriteZeroesBackend` (or, with a `Handler`, `HandlerDiscard`/`HandlerWriteZeroes`).

| Field | Default | Effect |
|---|---|---|
| `MaxDiscardSectors` | `0xffffffff` | Largest discard, and also the largest write-zeroes, in 512-byte sectors. 0 disables both |
| `DiscardGranularity` | 4096 | The backend's allocation unit in bytes. 0 is replaced by `LogicalBlockSize`, since the kernel requires a non-zero value |
| `DiscardAlignment` | 4096 | Offset of the first aligned allocation unit, in bytes |
| `MaxDiscardSegments` | 1 | Always sent as 1, the only value the kernel accepts |

### Lifecycle and recovery

| Field | Default | Effect |
|---|---|---|
| `Recovery` | `RecoveryNone` | Whether the block device survives its server: `RecoveryReissue` (requeue outstanding I/O to the next server; recommended), `RecoveryQueue` (fail it, hold new I/O), `RecoveryFailIO` (fail everything until recovered; 6.13+). Required for `Detach` and `Recover`. See [Detach and recover](/go-ublk/lifecycle/#detach-and-recover) |
| `SafeStop` | false | `Stop`/`Close` use `TRY_STOP_DEV`, refusing with `ErrDeviceBusy` while the device is open (7.0+) |
| `Tag` | 0 | 64 bits stored by the kernel with the device and returned by `GetDeviceInfo`; find your devices after a restart with `FindDevices(tag)` |
| `DeviceID` | -1 | Requested device ID, or -1 (`AutoAssignDeviceID`) for the lowest free one. A fixed ID gives stable `/dev/ublkbN` and `/dev/ublkcN` names, which a [systemd mount unit](/go-ublk/deployment/) needs |
| `CPUAffinity` | nil | CPUs to pin queue threads to: queue *i* runs on `CPUAffinity[i % len(CPUAffinity)]` |

### Data copy modes

How request data moves between the kernel and the server. The default copies it into a per-tag buffer and needs nothing beyond kernel 6.0.

| Field | Kernel | Effect |
|---|---|---|
| `EnableUserCopy` | 6.5+ | `UBLK_F_USER_COPY`: data moves with `pread`/`pwrite` on `/dev/ublkcN` instead of the kernel copying into a buffer address. Required by zoned and integrity devices; otherwise slower |
| `NeedGetData` | 6.0+ | `UBLK_F_NEED_GET_DATA`: the kernel asks for a write's buffer before copying its data. Supported for completeness; with go-ublk's fixed buffers it only adds a round trip |
| `BatchIO` | 7.0+ | `UBLK_F_BATCH_IO`: one `PREP_IO_CMDS` registers every tag, a multishot `FETCH_IO_CMDS` delivers up to 128 ready tags per completion into a provided-buffer ring, and each I/O thread commits all the requests completed in a round with one `COMMIT_IO_CMDS`. Fewer commands per request. Works with copy, user-copy and zero-copy modes (zero copy uses automatic buffer registration); not with `NeedGetData` or `ThreadsPerQueue` > 1 |
| `SharedMemoryZeroCopy` | 7.1+ | `UBLK_F_SHMEM_ZC`: register shared memory (a memfd or hugetlbfs mapping the applications also map) with `Device.RegisterSharedMemory`; an `O_DIRECT` request whose pages all lie in a registered region arrives with `FlagSharedMemory` and `Request.Data` pointing into it, with no copy either way. Other requests use the normal path. Not with `EnableZeroCopy` |
| `IODescSize` | 7.3+ | `UBLK_F_IO_DESC_SIZE`: descriptors larger than 24 bytes (up to 256, a multiple of 8); a `Handler` sees the extra bytes as `Request.DescriptorExtra`. For kernels that add descriptor fields |
| `EnableUnprivileged` | 6.3+ | `UBLK_F_UNPRIVILEGED_DEV`: a non-root user creates and owns the device. Needs udev rules granting access to `/dev/ublk-control` and the device nodes; see [Unprivileged devices](/guide/unprivileged/). Excludes recovery, user copy and zero copy. The kernel checks the caller's access to `/dev/ublkcN`, so a udev rule must hand each device's nodes to its creator: install `examples/ublk-chown` and its `99-ublk-unprivileged.rules`. Creation waits up to 5 s for udev. Covered end to end by the suite (a server running as `nobody`) |
| `EnableZeroCopy` | 6.15+ | Serve every request in the kernel against a `ZeroCopyBackend`'s file: data moves between the request's pages and the file with io_uring fixed-buffer reads and writes, flush is `fdatasync`, discard punches holes, write-zeroes zeroes the range, FUA writes use `RWF_DSYNC`. Uses automatic buffer registration (`UBLK_F_AUTO_BUF_REG`, 6.16+) when available, manual `REGISTER_IO_BUF` otherwise. The backend's `ReadAt`/`WriteAt` are never called. See [zero copy](/go-ublk/backends/#zero-copy) |
| `EnableZoned` + `Zoned` | 6.6+ | A host-managed zoned device served by a `Handler`: `Zoned.ZoneSize` (power of two; the device size must be a multiple), `MaxOpenZones`, `MaxActiveZones`, `MaxZoneAppendSize`. The handler serves `OpZoneOpen/Close/Finish/Reset/ResetAll`, `OpZoneAppend` (complete with `CompleteZoneAppend(sector, err)`) and `OpReportZones` (fill with `Request.ReportZones`). User copy is turned on automatically, as the kernel requires. Needs `CONFIG_BLK_DEV_ZONED` |
| `Integrity` | 7.0+ | Per-block integrity metadata (`UBLK_PARAM_TYPE_INTEGRITY`): `MetadataSize` bytes per `IntervalSize`, an optional protection checksum (`IntegrityCsumIP`, `IntegrityCsumCRC16` for T10-DIF, `IntegrityCsumCRC64NVMe`) with `RefTag`, `PIOffset`, `TagSize`. With a checksum the block layer generates protection information on write and verifies it on read; the server stores and returns `Request.Integrity`, through a `Handler` or an `IntegrityBackend`. User copy is turned on automatically. Needs `CONFIG_BLK_DEV_INTEGRITY` |
| `EnableIoctlEncode` | | Deprecated, no effect: ioctl-encoded commands are always used |
| `DeviceName` | | Deprecated, no effect: ublk devices have no name |

Features requested automatically when the kernel has them, because they cost nothing: `UPDATE_SIZE` (6.16+, for `Device.Resize`), with a recovery mode `QUIESCE` (6.16+, so `Detach` drains in-flight I/O first), and with zero copy `AUTO_BUF_REG` (6.16+).

## What reaches the kernel

| Kernel field | Value go-ublk sends |
|---|---|
| `ADD_DEV` `nr_hw_queues`, `queue_depth`, `max_io_buf_bytes` | `NumQueues` (or the CPU count), `QueueDepth`, `MaxIOSize` |
| `ADD_DEV` `flags` | `UBLK_F_CMD_IOCTL_ENCODE`, the features above, and the automatic ones |
| `ADD_DEV` `ublksrv_flags` | `Tag` |
| `basic.logical_bs_shift` / `physical_bs_shift` / `io_min_shift` / `io_opt_shift` | log2 of the configured sizes |
| `basic.max_sectors`, `basic.dev_sectors` | `MaxIOSize / 512`, size / 512 |
| `basic.attrs` | `READ_ONLY`, `ROTATIONAL`, `VOLATILE_CACHE`, `FUA` as configured |
| discard block | sent only if discard or write-zeroes is served and `MaxDiscardSectors` > 0 |
| DMA alignment block | sent when `DMAAlignment` is set |

## Options

```go
type Options struct {
	Context     context.Context // overrides the ctx argument of CreateAndServe
	Logger      Logger          // receives library logging (nil: silent)
	Debug       bool            // debug-level logging
	Observer    Observer        // per-operation callbacks (nil: built-in metrics)
	StopTimeout time.Duration   // bound on STOP_DEV and DEL_DEV (0: one minute)
}
```

| Field | Effect |
|---|---|
| `Context` | If non-nil, replaces the `ctx` passed to `CreateAndServe`/`Recover`. Cancelling it stops the device gracefully |
| `Logger` | Receives the library's messages and its internal control-plane and queue diagnostics. Without it the library logs nothing. Control-plane logging is process-wide: the most recent call with a `Logger` sets it |
| `Debug` | Enables debug logging. The I/O hot path never logs, but debug output elsewhere can change timing |
| `Observer` | Called for every read, write, discard and flush with size, latency and success. If you supply one, `Device.Metrics()` stays empty |
| `StopTimeout` | How long `Stop`/`Close` wait for the kernel's `STOP_DEV` (which first drains in-flight I/O through the backend) and `DEL_DEV`. On timeout the call fails and the device keeps serving |

## Exported constants

| Constant | Value |
|---|---|
| `DefaultQueueDepth` | 128 |
| `DefaultLogicalBlockSize` | 512 |
| `DefaultMaxIOSize` | 1 MiB |
| `DefaultDiscardAlignment`, `DefaultDiscardGranularity` | 4096 |
| `DefaultMaxDiscardSectors` | `0xffffffff` |
| `DefaultMaxDiscardSegments` | 1 |
| `AutoAssignDeviceID` | -1 |
| `IOBufferSizePerTag` | 64 KiB; legacy and unused. Use `MaxIOSize` |
