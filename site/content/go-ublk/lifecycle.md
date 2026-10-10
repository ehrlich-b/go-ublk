---
title: "Device lifecycle"
linkTitle: "Device lifecycle"
description: "Create, Start, Stop and Close; CreateAndServe; Done and Err; Detach and Recover; Resize; ListDevices and DeleteDevice; errors; metrics."
weight: 40
---

`*ublk.Device` manages one kernel device from creation to deletion. Its methods are safe across goroutines.

## Creating and starting

Create and start together, or in separate calls:

`CreateAndServe(ctx, params, options)` returns a running device after:

1. Validate params and route diagnostics to `options.Logger` if supplied. Otherwise the library is silent; failures return errors.
2. Probe GET_FEATURES (6.5+). Missing requested features match `syscall.EOPNOTSUPP`. Request supported UPDATE_SIZE for [Resize](#resizing) and QUIESCE for recovery automatically.
3. ADD_DEV creates `/dev/ublkcN`; use returned queues, depth, and maximum request size. Queue count is capped at CPU count.
4. SET_PARAMS supplies capacity, block size, attributes, discard limits, and optional geometry. Silently dropped parameter blocks become errors.
5. Open `/dev/ublkcN`; start queue threads/rings, map descriptors and `QueueDepth × MaxIOSize` request buffers, and FETCH_REQ every tag.
6. START_DEV waits for all fetches and creates `/dev/ublkbN`.

A failed step unwinds completed work and returns its error.

`Create(params, options)` stops after step 4 in state `created`: only the char node exists, with no serving queues. `device.Start(ctx)` performs steps 5-6.

After CreateAndServe/Start, the block node exists, but udev permissions and `/dev/disk/by-*` links arrive asynchronously. Use `udevadm settle` if needed.

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

The CreateAndServe/Start context, or `Options.Context`, bounds serving time. Cancellation performs graceful Stop: drain I/O through live queues, then exit them. Call Close afterwards to delete. A signal context can drive this:

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

Before v0.2.0, cancellation killed queues beneath in-flight I/O and could wedge the control plane; Critical Bug #22 in `TODO.md` records the fix.

## Knowing when serving ends

`device.Done()` closes after Stop, Close, Detach, cancellation, or queue failure. Err returns nil for orderly stop or the queue error. Unexpected kernel behavior fails a queue: it commits backend-held requests and closes its ring, letting the kernel fail/requeue outstanding I/O; the device enters `failed`. `device.Wait(ctx)` waits for completion or context cancellation.

Watch Done and treat non-nil Err as fatal for that device. Close deletes it; to preserve a recoverable device, release the failed process and let a fresh one [take over](#detach-and-recover).

## Shutting down

`device.Close()` tears down a running device:

1. STOP_DEV removes `/dev/ublkbN`, syncs mounted-filesystem data, and drains through still-serving queues. `Options.StopTimeout` bounds the wait (default one minute).
2. Wait for backend-held requests and queue exit; release rings, mappings, and `/dev/ublkcN`.
3. DEL_DEV removes the char node and frees the ID after all references vanish. An open block-device fd retains a reference even after node removal: Close times out, and deletion finishes after that fd closes. Close those fds first.

A failed STOP_DEV, including timeout or SafeStop with openers, leaves serving resources intact for retry. A queue that cannot exit retains its memory and char-device reference; Close returns an error rather than freeing memory under a backend call or deleting the device.

Close is idempotent and leaves the backend open. Close the backend after successful device closure.

`device.Stop()` performs steps 1-2, retaining registration until Close. Restart returns `ErrStopped` without a kernel call: matrix attempts returned EBUSY, hung 6.10-6.12 control planes, or oopsed Arch 7.2.8. Close and create a new device.

With `DeviceParams.SafeStop` (7.0+), Stop/Close use TRY_STOP_DEV. Open `/dev/ublkbN` references return an error matching `ublk.ErrDeviceBusy`; serving continues. This protects mounted devices.

### When the process dies instead

After SIGKILL, crash, or `os.Exit`, the kernel detects the last char-device reference closing. [`DeviceParams.Recovery`](#detach-and-recover) determines the outcome:

- RecoveryNone (default) fails outstanding I/O and removes the block node. Registration remains `dead` until DeleteDevice. Mounted filesystems see errors; ext4 aborts its journal.
- Recovery modes retain the block device for takeover with Recover.

Unmount before stopping a filesystem's server; see [deployment](/go-ublk/deployment/).

## Detach and recover

Set `DeviceParams.Recovery` at creation to preserve the device across crashes and upgrades:

| Mode | Outstanding I/O when the server goes | New I/O until `Recover` | Kernel |
|---|---|---|---|
| `RecoveryReissue` | requeued and reissued to the new server | held | 6.1+ |
| `RecoveryQueue` | failed with an I/O error | held | 6.1+ |
| `RecoveryFailIO` | failed | failed | 6.13+ |

`RecoveryReissue` avoids failing outstanding I/O by replaying it. Use it where duplicate writes are harmless; side-effecting operations such as zone append require care.

`device.Detach()` releases a running device without deletion. With QUIESCE (6.16+), it drains first, except for batch devices. Otherwise release requeues outstanding I/O with RecoveryReissue or fails it with RecoveryQueue. Applications retain their block-device fds; subsequent I/O waits or fails according to the mode.

`ublk.Recover(ctx, id, params, options)` attaches after Detach or crash. Kernel geometry/features remain; supply the backend and Inline/ThreadsPerQueue/CPUAffinity options. Backend size must match. It waits up to 30 seconds for release, sends START_USER_RECOVERY, starts queues, and sends END_USER_RECOVERY to resume held I/O.

```go
// New server process, upgraded binary:
ids, _ := ublk.FindDevices(myTag)        // devices created with DeviceParams.Tag = myTag
dev, err := ublk.Recover(ctx, ids[0], ublk.DefaultParams(backend), nil)
```

The kernel stores the 64-bit `DeviceParams.Tag`, returned by GetDeviceInfo, allowing discovery without a state file. Real-kernel tests cover SIGKILL during writes and Detach/Recover under a running writer: no I/O errors, acknowledged blocks intact. See the [recovery protocol](/guide/recovery/).

## Resizing

`device.Resize(newSize)` uses UPDATE_SIZE (6.16+) on a running device. The backend must already serve the new capacity. The kernel updates `/dev/ublkbN` and notifies listeners; grow the filesystem separately. Missing support returns an error matching ErrNotImplemented.

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

`ublk.GetDeviceInfo(id)` or `device.KernelInfo()` reports kernel state (dead/live/quiesced/fail-io), negotiated features, queue geometry, server PID, owner, and tag, including for other processes' devices.

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

`ublk.Probe()` reports kernel support before creation and explains failures such as an unloaded module or sysctl-disabled io_uring.

## Metrics

Without `Options.Observer`, inspect built-in metrics through `device.Metrics()` (live atomic counters) or `device.MetricsSnapshot()`:

- Read/write/discard/flush operation, byte, and error counts; write-zeroes count as writes.
- Average latency and P50/P99/P99.9 histogram upper bounds, with buckets at 1/10/100 µs, 1/10/100 ms, and 1/10 s. Percentiles are coarse.
- IOPS, bandwidth, and error rate over device uptime.

Latency covers only backend calls. Queue-depth fields stay zero; Handlers bypass operation tracking and record no metrics.

A supplied Observer replaces built-in metrics. ObserveRead/ObserveWrite/ObserveDiscard/ObserveFlush run on the request's goroutine; callbacks must be fast and concurrency-safe.

## Devices you do not own

`ublk.ListDevices()` enumerates `/sys/class/ublk-char`, then queries registered IDs, including orphans. If sysfs is unavailable, it probes only IDs 0-63 and misses higher IDs. The kernel has no list command. Errors other than missing devices fail the call instead of returning an incomplete list.

`ublk.FindDevices(tag)` filters devices by their creation Tag.

`ublk.DeleteDevice(id)` stops best-effort, then deletes an orphan by ID. With a live server, STOP_DEV drains through it and DEL_DEV waits for its references; deletion cannot forcibly detach that server. Use Device.Close for locally owned devices.

All of these need `CAP_SYS_ADMIN` (or, for unprivileged devices, ownership of the device).

## Errors

Lifecycle errors wrap causes with `%w`; `errors.Is` matches kernel `syscall.Errno` values:

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
