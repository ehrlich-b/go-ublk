---
title: "Configuration reference"
linkTitle: "Configuration"
description: "Every field of DeviceParams and Options: defaults, valid ranges, what each one sends to the kernel, and which ones are not implemented yet."
weight: 30
---

A device is configured by a `DeviceParams` value, normally obtained from `DefaultParams(backend)` and then adjusted, and an optional `*Options`. Both are read once, at `Create` or `CreateAndServe`.

```go
params := ublk.DefaultParams(backend)
params.NumQueues = 4
params.QueueDepth = 64
params.LogicalBlockSize = 4096
params.VolatileCache = false

device, err := ublk.CreateAndServe(ctx, params, &ublk.Options{Logger: myLogger})
```

Invalid values are rejected before anything is sent to the kernel, with a plain error describing the field (these are not `*ublk.Error` values and do not match `ErrInvalidParameters`).

## DeviceParams

### Geometry and limits

| Field | Default | Valid | Effect |
|---|---|---|---|
| `Backend` | required | non-nil | The storage. Its optional interfaces decide whether discard and write-zeroes are advertised |
| `NumQueues` | 0 | 0 to 4096 | Hardware queues; 0 means `runtime.NumCPU()`. The kernel caps the count at the number of CPUs, and go-ublk uses whatever `ADD_DEV` returns: read it back with `Device.NumQueues()` |
| `QueueDepth` | 128 | 1 to 4096 | Requests the kernel can have outstanding per queue. Not backend concurrency: each queue calls the backend one request at a time |
| `LogicalBlockSize` | 512 | power of two, 512 to the page size | Sent as both the logical and physical block size and as the minimum I/O size |
| `MaxIOSize` | 1 MiB | page-aligned, at least one page, a multiple of `LogicalBlockSize`, at most 2<sup>31</sup>-1 | Largest request; the kernel splits bigger ones. Each queue maps `QueueDepth × MaxIOSize` bytes of buffers. The kernel's negotiated value is used if it differs |
| `Backend.Size()` | | positive multiple of `LogicalBlockSize` | The capacity, fixed for the device's life |

### Attributes

| Field | Default | Effect |
|---|---|---|
| `VolatileCache` | **true** | Advertises a write-back cache, so the kernel sends `FLUSH` and your `Flush` runs. Set false only if every completed write is already durable. See [durability](/go-ublk/backends/#flush-and-durability) |
| `ReadOnly` | false | Creates a read-only disk; the block layer rejects writes, so they never reach the backend |
| `Rotational` | false | Marks the device rotational, which changes I/O scheduler and filesystem heuristics |
| `EnableFUA` | false | **Not implemented.** Logs a warning and is otherwise ignored; FUA is never advertised. The block layer emulates FUA with a flush, so durability does not depend on it |

### Discard and write-zeroes

Used only if the backend implements `DiscardBackend` or `WriteZeroesBackend`.

| Field | Default | Effect |
|---|---|---|
| `MaxDiscardSectors` | `0xffffffff` | Largest discard, and also the largest write-zeroes, in 512-byte sectors. The default lets the block layer send requests as large as it likes. 0 disables both operations |
| `DiscardGranularity` | 4096 | The backend's allocation unit in bytes. 0 is replaced by `LogicalBlockSize`, since the kernel requires a non-zero value |
| `DiscardAlignment` | 4096 | Offset of the first aligned allocation unit, in bytes |
| `MaxDiscardSegments` | 1 | Always sent as 1, the only value the kernel accepts; anything else logs a warning |

### Device identity and placement

| Field | Default | Effect |
|---|---|---|
| `DeviceID` | -1 | Requested device ID, or -1 (`AutoAssignDeviceID`) for the lowest free one. A fixed ID gives stable `/dev/ublkbN` and `/dev/ublkcN` names, which a [systemd mount unit](/go-ublk/deployment/) needs; creation fails if the ID is taken |
| `CPUAffinity` | nil | CPUs to pin queue goroutines' OS threads to: queue *i* runs on `CPUAffinity[i % len(CPUAffinity)]`. A failed pin is logged and ignored |
| `DeviceName` | "" | **No effect.** Not sent to the kernel; ublk devices have no name |

### Kernel feature switches

These map to `UBLK_F_*` flags at `ADD_DEV`. Most are placeholders for work that is not done; setting them does not give you the feature.

| Field | Status | What actually happens |
|---|---|---|
| `EnableIoctlEncode` | harmless | Requests `UBLK_F_CMD_IOCTL_ENCODE`. go-ublk sends ioctl-encoded commands regardless, and modern kernels report the flag on every device |
| `EnableZeroCopy` | **not implemented; do not set** | Requests `UBLK_F_SUPPORT_ZERO_COPY`, but the data plane still passes buffer addresses and never registers request buffers, which is not how a zero-copy device is served |
| `EnableUserCopy` | **not implemented** | `Create` and `CreateAndServe` fail with an error matching `ErrNotImplemented` |
| `EnableUnprivileged` | **not implemented** | Requests `UBLK_F_UNPRIVILEGED_DEV`. As root the kernel clears the flag. A non-root user who has been given access to `/dev/ublk-control` can get through `ADD_DEV`, but every later command fails, because go-ublk does not send the char-device path that [unprivileged devices](/guide/unprivileged/) require |
| `EnableZoned` | **no effect** | Not sent to the kernel |

go-ublk always requests `UBLK_F_URING_CMD_COMP_IN_TASK`. It does not request user recovery, quiesce, batch I/O, automatic buffer registration or any other feature; see the [roadmap](/go-ublk/roadmap/).

## What reaches the kernel

For reference when reading kernel logs or the guide:

| Kernel field | Value go-ublk sends |
|---|---|
| `ADD_DEV` `nr_hw_queues`, `queue_depth`, `max_io_buf_bytes` | `NumQueues` (or the CPU count), `QueueDepth`, `MaxIOSize` |
| `ADD_DEV` `flags` | `UBLK_F_URING_CMD_COMP_IN_TASK`, plus the switches above |
| `basic.logical_bs_shift`, `physical_bs_shift`, `io_min_shift` | log2(`LogicalBlockSize`) |
| `basic.io_opt_shift`, `chunk_sectors`, `virt_boundary_mask` | 0 |
| `basic.max_sectors` | `MaxIOSize / 512` (negotiated) |
| `basic.dev_sectors` | `Backend.Size() / 512` |
| `basic.attrs` | `UBLK_ATTR_READ_ONLY`, `UBLK_ATTR_ROTATIONAL`, `UBLK_ATTR_VOLATILE_CACHE` as configured |
| discard block | sent only if the backend implements an optional interface and `MaxDiscardSectors` > 0 |

No other parameter blocks (zoned, DMA alignment, segments, integrity) are sent.

## Options

```go
type Options struct {
	Context  context.Context // overrides the ctx argument of CreateAndServe
	Logger   Logger          // receives all library logging (nil: stderr)
	Debug    bool            // debug-level logging
	Observer Observer        // per-operation callbacks (nil: built-in metrics)
}

type Logger interface {
	Printf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}
```

| Field | Effect |
|---|---|
| `Context` | If non-nil, replaces the `ctx` passed to `CreateAndServe`. The context bounds the queue goroutines; see the warning in [Device lifecycle](/go-ublk/lifecycle/) about cancelling it |
| `Logger` | Receives the library's own messages and the internal control-plane and queue diagnostics. Logging configuration is process-wide: the most recent `Create` or `CreateAndServe` call decides it for every device in the process |
| `Debug` | Enables debug logging. The I/O hot path never logs, but debug output elsewhere is heavy enough to change timing, and the internal logger serializes writes, so leave it off under load |
| `Observer` | Called for every read, write, discard and flush with size, latency and success. If you supply one, `Device.Metrics()` stays empty, because the built-in metrics are themselves the default observer |

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
