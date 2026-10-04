---
title: "Device lifecycle"
linkTitle: "Device lifecycle"
description: "Create, Start, Stop and Close; CreateAndServe; ListDevices and DeleteDevice; teardown order; errors; metrics."
weight: 40
---

A `*ublk.Device` wraps one kernel ublk device from creation to deletion. This page maps each call to what it does in the kernel, and covers the one rule that matters most: how to shut down.

## Creating and starting

There are two entry points.

**`CreateAndServe(ctx, params, options)`** does everything and returns a running device:

1. Validates `params` and configures logging from `options`.
2. `ADD_DEV`, which creates `/dev/ublkcN`. The queue count, depth and maximum request size the kernel returns replace the requested ones.
3. `SET_PARAMS`: capacity, block size, attributes, discard limits.
4. Opens `/dev/ublkcN` once, retrying for up to 5 seconds while udev creates it.
5. For each queue: duplicates the descriptor, creates an io_uring, maps the queue's descriptor array, maps `QueueDepth × MaxIOSize` bytes of request buffers, and starts a goroutine locked to an OS thread that submits `FETCH_REQ` for every tag.
6. Waits 100 ms, then sends `START_DEV`, which creates `/dev/ublkbN`.

If any step fails, everything done so far is undone (queues cancelled and joined, descriptors closed, device deleted) and the error is returned. No device is left behind.

**`Create(params, options)`** stops after step 3 and returns a device in state `created`: `/dev/ublkcN` exists, `/dev/ublkbN` does not, nothing is serving. **`device.Start(ctx)`** then performs steps 4 to 6. Use the split when you want to create the device, record its ID or prepare something, and start serving later.

When `CreateAndServe` or `Start` returns, `/dev/ublkbN` exists. udev's work (permissions, `/dev/disk/by-*` links) follows asynchronously; wait for it with `udevadm settle` if you depend on it.

```text
              Create                       Start(ctx)
  (none) ───────────────► created ─────────────────────────► running
          ADD_DEV                  open /dev/ublkcN, queues,      │
          SET_PARAMS               FETCH every tag, START_DEV     │
                                                                  │
            ┌───────────────────── Close ─────────────────────────┤
            ▼                STOP_DEV, join queues, DEL_DEV       │ Stop
         closed ◄─────────────── Close ──────────── stopped ◄─────┘
                                DEL_DEV                  STOP_DEV, join queues
```

## Shutting down

**`device.Close()`** is the normal way out. On a running device it:

1. Sends `STOP_DEV` **while the queue goroutines are still serving**. The kernel removes `/dev/ublkbN`, which drains in-flight I/O (and syncs a mounted filesystem through the device); only the running queues can complete that I/O.
2. Cancels the queue goroutines' context and joins them, waiting up to 2 seconds each.
3. Closes each queue's io_uring, unmaps its descriptor array and buffers, and closes its descriptor.
4. Sends `DEL_DEV`, which deletes `/dev/ublkcN` and frees the ID. The kernel only completes it once every reference to the device is gone, which step 3 guarantees.

`Close` is idempotent. It does not close the backend; do that after `Close` returns. Idle teardown takes about a tenth of a second, and teardown under load is not much slower: the drain in step 1 completes requests that are already in flight.

**`device.Stop()`** performs steps 1 to 3 and keeps the device registered. `Close` afterwards deletes it. Restarting a stopped device with `Start` is not covered by go-ublk's tests; prefer `Close` and a new device.

### Do not cancel the context first

> [!CAUTION]
> The context passed to `CreateAndServe` or `Start` (or `Options.Context`) controls the queue goroutines. Cancelling it stops them **without telling the kernel**: the device stays live, requests routed to its queues are never answered, and the processes issuing them hang. A later `Close` sends `STOP_DEV`, whose drain now waits for I/O that nothing will complete; the control command gives up after 10 seconds and teardown continues in a degraded state.
>
> Shut down with `Close`, and treat the context as a backstop for abnormal exits only. In a signal handler, call `Close`; do not cancel first.

This ordering is the single most important thing in the lifecycle, and it is why the examples' signal handlers call `Close` directly. It was the cause of go-ublk's worst teardown hang before it was fixed.

### When the process dies instead

If the server exits without `Close` (SIGKILL, a crash, `os.Exit`, an unrecovered panic), the kernel releases the device's char device and, because go-ublk does not enable user recovery, stops the device: in-flight requests fail with I/O errors and `/dev/ublkbN` disappears. go-ublk's crash tests check that a writer blocked on the dead device gets its error within seconds instead of hanging. A filesystem mounted on the device sees those errors too (ext4 aborts its journal). The device itself stays registered in state `DEAD` until someone deletes it, which is what `DeleteDevice` is for.

A daemon that a mounted filesystem depends on must also be stopped in the right order relative to the unmount. See [Deployment](/go-ublk/deployment/).

## States

`device.State()` returns one of four values, derived from what the `Device` has done rather than from the kernel:

| State | Means |
|---|---|
| `created` | after `Create`, and also after `Stop` |
| `running` | started and not stopped, and the context is not cancelled |
| `stopped` | started, and the context was cancelled (the dangerous case above) |
| `closed` | after `Close`, or a nil `*Device` |

`IsRunning()` is `State() == running`. To see the kernel's view (for example after a crash), query the kernel; go-ublk does not expose `GET_DEV_INFO` publicly yet.

## Inspecting a device

| Accessor | Returns |
|---|---|
| `ID`, `DeviceID()` | kernel device ID |
| `Path`, `BlockPath()` | `/dev/ublkbN` |
| `CharPath`, `CharDevicePath()` | `/dev/ublkcN` |
| `NumQueues()`, `QueueDepth()` | the negotiated values |
| `BlockSize()` | logical block size |
| `Size()` | `Backend.Size()` |
| `Info()` | all of the above as a `DeviceInfo`, with JSON tags |

## Metrics

Unless you supply `Options.Observer`, every device records metrics, read with `device.Metrics()` (live atomic counters) or `device.MetricsSnapshot()`:

- Operation, byte and error counts for reads, writes, discards and flushes. Write-zeroes are counted as writes.
- Average latency, and P50, P99 and P99.9 from a histogram with buckets at 1 µs, 10 µs, 100 µs, 1 ms, 10 ms, 100 ms, 1 s and 10 s. Percentiles are bucket upper bounds, so they are coarse.
- IOPS and bandwidth over the device's uptime, and the error rate.

Latency is measured around the backend call only. The queue-depth fields are always zero: nothing reports queue depth yet.

An `Observer` receives the same events (`ObserveRead`, `ObserveWrite`, `ObserveDiscard`, `ObserveFlush`) synchronously on the queue goroutine, so it must be fast and safe for concurrent use. Supplying one replaces the built-in metrics.

## Devices you do not own

**`ublk.ListDevices()`** returns the IDs of every registered ublk device, including orphans left by dead servers. The kernel has no list command, so it asks for the info of IDs 0 through 63 in turn and collects the ones that exist. Devices with higher IDs are not found. A query that fails for any reason other than "no such device" fails the whole call, so an error never masquerades as an empty list.

**`ublk.DeleteDevice(id)`** stops (best effort) and deletes one device by ID. It is the recovery path for devices whose server died. On a device that another process is still serving, `STOP_DEV` drains through that server and `DEL_DEV` then waits until that process releases the device, typically by exiting; it is not a way to forcibly take a device away from a live server. A device your own process owns should be closed with `Device.Close`.

Both need `CAP_SYS_ADMIN`, and each opens and closes its own control channel.

## Errors

Errors from the lifecycle functions wrap the underlying cause with `%w`, and a kernel failure is a `syscall.Errno`, so `errors.Is` works on the errno:

| Check | Typical cause |
|---|---|
| `errors.Is(err, syscall.ENOENT)` | `/dev/ublk-control` does not exist: `ublk_drv` is not loaded |
| `errors.Is(err, syscall.EACCES)`, `syscall.EPERM` | not root and no `CAP_SYS_ADMIN` |
| `errors.Is(err, syscall.EEXIST)` | `DeviceID` is already taken |
| `errors.Is(err, syscall.EINVAL)` | the kernel rejected the parameters |
| `errors.Is(err, syscall.EOPNOTSUPP)` | the kernel does not understand ioctl-encoded commands (older than 6.4) |
| `errors.Is(err, syscall.ENODEV)` | `DeleteDevice` on an ID that does not exist |
| `errors.Is(err, ublk.ErrNotImplemented)` | a feature switch go-ublk does not implement, such as `EnableUserCopy` |

Parameter validation errors are plain errors naming the field. The package also defines a structured `*ublk.Error` (operation, device, queue, category, errno) with sentinel values such as `ErrDeviceNotFound` and helpers `IsCode` and `IsErrno`; today the lifecycle functions return it only for `ErrNotImplemented` and for methods called on a nil `*Device`, so prefer `errors.Is` with an errno.

## Known defects

These lifecycle defects were found by a code audit on 2026-10-03 and are open. They matter for long-running daemons more than for one-shot tools:

- Every io_uring the library creates stays mapped until the process exits: about four per device create/close cycle and three per `ListDevices` call. A daemon that creates devices on demand grows without bound.
- `Close` ignores a failed `STOP_DEV` and tears the queues down anyway. `STOP_DEV` on a busy, dirty, mounted device can take longer than the 10-second control timeout.
- If a queue goroutine does not exit within 2 seconds (a backend call slower than that), its memory is released while it may still be using it.
- A queue goroutine can exit on an unexpected completion while the device stays live, and nothing notices; I/O on that queue then hangs until `STOP_DEV`.
- Buffers passed to some control commands are not pinned against the Go runtime for the full duration the kernel may use them.

Their status is tracked on the [roadmap](/go-ublk/roadmap/).
