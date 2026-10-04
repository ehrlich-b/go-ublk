---
title: "Device lifecycle"
linkTitle: "Device lifecycle"
description: "Create, Start, Stop and Close; CreateAndServe; Done and Err; Detach and Recover; Resize; ListDevices and DeleteDevice; errors; metrics."
weight: 40
---

A `*ublk.Device` wraps one kernel ublk device from creation to deletion. This page maps each call to what it does in the kernel. Every method is safe to call from any goroutine.

## Creating and starting

There are two entry points.

**`CreateAndServe(ctx, params, options)`** does everything and returns a running device:

1. Validates `params` and, if `options.Logger` is set, routes the library's diagnostics to it. Without a logger the library writes nothing; every failure comes back as an error.
2. Asks the kernel which features it has (`GET_FEATURES`, 6.5+) and fails with an error matching `syscall.EOPNOTSUPP` if a requested feature is missing — it never silently creates a weaker device. Features that cost nothing to have (`UPDATE_SIZE` for [Resize](#resizing), and `QUIESCE` when recovery is on) are requested automatically when available.
3. `ADD_DEV`, which creates `/dev/ublkcN`. The queue count, depth and maximum request size the kernel returns replace the requested ones (the kernel caps the queue count at the number of CPUs).
4. `SET_PARAMS`: capacity, block size, attributes, discard limits and any optional geometry. Parameter blocks an older kernel silently drops are reported as an error.
5. Opens `/dev/ublkcN` and, for each queue, starts an I/O thread with its own io_uring that maps the queue's descriptors and `QueueDepth × MaxIOSize` bytes of request buffers and submits `FETCH_REQ` for every tag.
6. `START_DEV`, which waits until every tag has been fetched and then creates `/dev/ublkbN`.

If any step fails, everything done so far is undone and the error is returned. No device is left behind.

**`Create(params, options)`** stops after step 4 and returns a device in state `created`: `/dev/ublkcN` exists, `/dev/ublkbN` does not, nothing is serving. **`device.Start(ctx)`** then performs steps 5 and 6.

When `CreateAndServe` or `Start` returns, `/dev/ublkbN` exists. udev's work (permissions, `/dev/disk/by-*` links) follows asynchronously; wait for it with `udevadm settle` if you depend on it.

```text
              Create                    Start(ctx)
  (none) ───────────────► created ───────────────────────► running ──── Detach ───► detached
          ADD_DEV                 open /dev/ublkcN, queues,   │  │        (QUIESCE, let go;
          SET_PARAMS              FETCH every tag, START_DEV  │  │         kernel keeps device)
                                                              │  │
            ┌──────────────────────── Close ──────────────────┘  │ Stop, or ctx cancelled
            ▼            STOP_DEV, drain, join queues, DEL_DEV   ▼
         closed ◄──────────────── Close ───────────────────── stopped
                                 DEL_DEV
```

## The serving context

The context given to `CreateAndServe` or `Start` (or `Options.Context`) bounds how long the device serves. **Cancelling it stops the device gracefully**, exactly as `Stop` does: the kernel drains in-flight I/O through the still-running queues, then the queues exit. Afterwards call `Close` to delete the device. Wiring a signal context straight into `CreateAndServe` is therefore safe:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
defer stop()
dev, err := ublk.CreateAndServe(ctx, params, nil)
if err != nil {
	log.Fatal(err)
}
<-dev.Done()       // the signal arrived and the device has stopped
_ = dev.Close()    // delete it
```

(Before go-ublk v0.2.0 cancelling the context killed the queues under in-flight I/O, which could wedge the kernel's control plane. That is fixed; see Critical Bug #22 in `TODO.md`.)

## Knowing when serving ends

**`device.Done()`** is a channel closed when the device stops serving for any reason: `Stop`, `Close`, `Detach`, the context being cancelled, or a queue failing. **`device.Err()`** says why: `nil` after an orderly stop, or the queue's error. A queue fails only on something unexpected from the kernel; it then commits what its backend calls still hold, closes its ring (so the kernel fails or requeues that queue's outstanding requests), and the device enters state `failed`. **`device.Wait(ctx)`** blocks for either.

A long-running server should watch `Done` and treat a non-nil `Err` as fatal for that device: `Close` it and, with recovery enabled, let a fresh process [take over](#detach-and-recover).

## Shutting down

**`device.Close()`** is the normal way out. On a running device it:

1. Sends `STOP_DEV` while the queues are still serving. The kernel removes `/dev/ublkbN`, which drains in-flight I/O (and syncs a mounted filesystem through the device); only the running queues can complete that I/O. The wait is bounded by `Options.StopTimeout` (default one minute).
2. Waits for every queue to finish what its backend calls hold and exit, then frees their rings, mappings and `/dev/ublkcN`.
3. Sends `DEL_DEV`, which deletes `/dev/ublkcN` and frees the ID. The kernel finishes it only when nothing holds the device any more, and an open file descriptor on `/dev/ublkbN` (even after `STOP_DEV` removed the node) keeps it alive: `Close` then waits up to `Options.StopTimeout` and returns an error, and the deletion completes when that descriptor is closed. Close your own descriptors on the block device before calling `Close`.

If `STOP_DEV` fails (a timeout, or `SafeStop` with the device open), `Close` returns the error **with nothing torn down**: the device keeps serving and you can retry. If a queue does not exit, its memory is deliberately leaked rather than freed under a running backend call, `/dev/ublkcN` stays held, and `Close` returns an error instead of deleting a device it cannot safely release.

`Close` is idempotent. It does not close the backend; do that after `Close` returns.

**`device.Stop()`** performs steps 1 and 2 and keeps the device registered; `Close` afterwards deletes it. **A stopped device cannot be started again**: `Start` returns `ErrStopped` without touching the kernel. The kernel cannot reliably restart a stopped ublk device — go-ublk's kernel matrix saw it fail with `EBUSY`, wedge the control plane on 6.10–6.12, and oops Arch's 7.2.8 — so close it and create a new one.

**Safe stop.** With `DeviceParams.SafeStop` (kernel 7.0+), `Stop` and `Close` send `TRY_STOP_DEV`, which refuses while anything has `/dev/ublkbN` open; the call returns an error matching `ublk.ErrDeviceBusy` and the device keeps serving. Use it when stopping a device that is still mounted would be a mistake.

### When the process dies instead

If the server exits without `Close` (SIGKILL, a crash, `os.Exit`), the kernel notices when the last reference to `/dev/ublkcN` goes away. What happens next depends on [`DeviceParams.Recovery`](#detach-and-recover):

- **`RecoveryNone`** (the default): the kernel fails the outstanding requests with I/O errors and removes `/dev/ublkbN`. The device stays registered in state `dead` until someone deletes it with `DeleteDevice`. A filesystem mounted on it sees the errors (ext4 aborts its journal).
- **Any recovery mode**: the block device stays, and a new process can take over with `Recover`.

A daemon that a mounted filesystem depends on must also be stopped in the right order relative to the unmount. See [Deployment](/go-ublk/deployment/).

## Detach and recover

Recovery lets a block device outlive the process serving it: for crash recovery, and for zero-downtime upgrades. Enable it at creation with `DeviceParams.Recovery`:

| Mode | Outstanding I/O when the server goes | New I/O until `Recover` | Kernel |
|---|---|---|---|
| `RecoveryReissue` | requeued and reissued to the new server | held | 6.1+ |
| `RecoveryQueue` | failed with an I/O error | held | 6.1+ |
| `RecoveryFailIO` | failed | failed | 6.13+ |

`RecoveryReissue` is the right choice for nearly everything: no I/O error ever reaches the application. A reissued write may reach the backend twice, which block-device semantics allow.

**`device.Detach()`** lets go of a running device without deleting it. On kernels with `QUIESCE` (6.16+) it first drains in-flight I/O with `QUIESCE_DEV`; on older kernels the kernel requeues it. The process can then exit; applications keep `/dev/ublkbN` open and their I/O waits.

**`ublk.Recover(ctx, id, params, options)`** takes such a device over — after a `Detach`, or after the previous server crashed. Geometry and features come from the kernel; `params` supplies the backend and the data-plane options (`Inline`, `ThreadsPerQueue`, `CPUAffinity`), and the backend's size must match the device's. It waits (up to 30 seconds) for the kernel to finish releasing the previous server, sends `START_USER_RECOVERY`, starts the queues, and sends `END_USER_RECOVERY`, which makes the device live again and releases the held I/O.

```go
// New server process, upgraded binary:
ids, _ := ublk.FindDevices(myTag)        // devices created with DeviceParams.Tag = myTag
dev, err := ublk.Recover(ctx, ids[0], ublk.DefaultParams(backend), nil)
```

`DeviceParams.Tag` is a 64-bit value the kernel stores with the device and returns from `GetDeviceInfo`, so a restarted process can find its devices without a state file. go-ublk's conformance suite tests both paths on real kernels: a server SIGKILLed mid-write and recovered by another process, and a `Detach`/`Recover` handoff under a running writer. In both, the writer sees no error and every acknowledged block reads back intact. The protocol itself is described in the guide's [User recovery and quiesce](/guide/recovery/) chapter.

## Resizing

**`device.Resize(newSize)`** changes the size of a running device (`UPDATE_SIZE`, kernel 6.16+). Grow or shrink the backend first, so it already serves the new size; the kernel then updates `/dev/ublkbN`'s capacity and notifies listeners (a filesystem must still be grown separately). On older kernels `Resize` returns an error matching `ErrNotImplemented`.

## States

`device.State()` reports the `Device`'s own state:

| State | Means |
|---|---|
| `created` | after `Create`, before `Start` |
| `running` | serving I/O |
| `failed` | was running, and a queue failed; see `Err` |
| `stopped` | after `Stop` or a cancelled context; `Close` it |
| `detached` | after `Detach`; the kernel keeps the device for `Recover` |
| `closed` | after `Close`, or a nil `*Device` |

For the kernel's view of any device — including ones another process created — use **`ublk.GetDeviceInfo(id)`** (or `device.KernelInfo()`): kernel state (`dead`, `live`, `quiesced`, `fail-io`), negotiated features, queue geometry, server PID, owner and tag.

## Inspecting a device

| Accessor | Returns |
|---|---|
| `ID`, `DeviceID()` | kernel device ID |
| `Path`, `BlockPath()` | `/dev/ublkbN` |
| `CharPath`, `CharDevicePath()` | `/dev/ublkcN` |
| `NumQueues()`, `QueueDepth()` | the negotiated values |
| `BlockSize()` | logical block size |
| `Size()` | the device size |
| `Features()` | the features negotiated with the kernel |
| `Info()` | the above as a `DeviceInfo`, with JSON tags |

**`ublk.Probe()`** reports what the running kernel supports before you create anything; it explains the usual failures (module not loaded, io_uring disabled by sysctl).

## Metrics

Unless you supply `Options.Observer`, every device records metrics, read with `device.Metrics()` (live atomic counters) or `device.MetricsSnapshot()`:

- Operation, byte and error counts for reads, writes, discards and flushes. Write-zeroes are counted as writes.
- Average latency, and P50, P99 and P99.9 from a histogram with buckets at 1 µs, 10 µs, 100 µs, 1 ms, 10 ms, 100 ms, 1 s and 10 s. Percentiles are bucket upper bounds, so they are coarse.
- IOPS and bandwidth over the device's uptime, and the error rate.

Latency is measured around the backend call only. The queue-depth fields are always zero: nothing reports queue depth yet. With a `Handler` instead of a `Backend`, the library cannot see operations, so no metrics are recorded.

An `Observer` receives the same events (`ObserveRead`, `ObserveWrite`, `ObserveDiscard`, `ObserveFlush`) on whichever goroutine ran the request, so it must be fast and safe for concurrent use. Supplying one replaces the built-in metrics.

## Devices you do not own

**`ublk.ListDevices()`** returns the IDs of every registered ublk device, including orphans left by dead servers. The kernel has no list command, so it asks for the info of IDs 0 through 63 in turn and collects the ones that exist. Devices with higher IDs are not found. A query that fails for any reason other than "no such device" fails the whole call, so an error never masquerades as an empty list.

**`ublk.FindDevices(tag)`** narrows that to the devices created with a given `Tag`.

**`ublk.DeleteDevice(id)`** stops (best effort) and deletes one device by ID. It is the cleanup path for devices whose server died without recovery. On a device that another process is still serving, `STOP_DEV` drains through that server and `DEL_DEV` then waits until that process releases the device; it is not a way to take a device away from a live server. A device your own process owns should be closed with `Device.Close`.

All of these need `CAP_SYS_ADMIN` (or, for unprivileged devices, ownership of the device).

## Errors

Errors from the lifecycle functions wrap the underlying cause with `%w`, and a kernel failure is a `syscall.Errno`, so `errors.Is` works on the errno:

| Check | Typical cause |
|---|---|
| `errors.Is(err, syscall.ENOENT)` | `/dev/ublk-control` does not exist: `ublk_drv` is not loaded |
| `errors.Is(err, syscall.EACCES)`, `syscall.EPERM` | not root and no `CAP_SYS_ADMIN`; or io_uring disabled (`kernel.io_uring_disabled`, the default on RHEL 10 family) |
| `errors.Is(err, syscall.EEXIST)` | `DeviceID` is already taken |
| `errors.Is(err, syscall.EINVAL)` | the kernel rejected the parameters, or a feature combination it does not allow |
| `errors.Is(err, syscall.EOPNOTSUPP)` | the kernel lacks a requested feature; the error lists which |
| `errors.Is(err, syscall.ENODEV)` | `DeleteDevice` or `GetDeviceInfo` on an ID that does not exist; or a kernel older than 6.4, which does not understand ioctl-encoded commands |
| `errors.Is(err, ublk.ErrDeviceBusy)` | `SafeStop` refused because the device is open |
| `errors.Is(err, ublk.ErrStopped)` | `Start` on a stopped device |
| `errors.Is(err, ublk.ErrNotImplemented)` | `Resize` on a device the kernel did not grant `UPDATE_SIZE`, or `RegisterSharedMemory` on a device created without `SharedMemoryZeroCopy` |

Parameter validation errors are plain errors naming the field.
