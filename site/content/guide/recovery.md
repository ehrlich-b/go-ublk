---
title: "User recovery and quiesce"
linkTitle: "User recovery & quiesce"
description: "Keeping /dev/ublkbN alive across a server crash or upgrade: the recovery flags, the QUIESCED and FAIL_IO states, QUIESCE_DEV, and the recovery sequence."
weight: 90
---

By default, releasing the server's `/dev/ublkcN` after exit, crash, or kill stops the device. Pending and new requests fail, `del_gendisk` removes `/dev/ublkbN`, and a mounted filesystem needs unmounting and restarting.

Recovery preserves the device, ID, parameters, and storage stacked above it. A replacement attaches after a crash or upgrade without unmounting.

## The flags

Choose recovery at `ADD_DEV` through `ublksrv_ctrl_dev_info.flags`:

| Flag | Since | Bit | What it adds |
|---|---|---|---|
| {{< uapi "UBLK_F_USER_RECOVERY" >}} | 6.1 | `1 << 3` | The device survives a server exit. Requests the server held are failed; new requests wait. |
| {{< uapi "UBLK_F_USER_RECOVERY_REISSUE" >}} | 6.1 | `1 << 4` | Requests the server held are requeued and reissued to the next server instead of failed. |
| {{< uapi "UBLK_F_USER_RECOVERY_FAIL_IO" >}} | 6.13 | `1 << 9` | While there is no server, every request fails immediately instead of waiting. |
| {{< uapi "UBLK_F_QUIESCE" >}} | 6.16 | `1 << 12` | Enables `UBLK_U_CMD_QUIESCE_DEV`, for handing a live device to a new server on purpose. |

`ADD_DEV` accepts four recovery combinations; others return `-EINVAL`:

- none (the device dies with its server)
- `USER_RECOVERY`
- `USER_RECOVERY | USER_RECOVERY_REISSUE`
- `USER_RECOVERY | USER_RECOVERY_FAIL_IO`

`UBLK_F_QUIESCE` requires `UBLK_F_USER_RECOVERY`, or `-EINVAL`. Unprivileged devices silently lose `USER_RECOVERY`/`USER_RECOVERY_REISSUE`: recovery could stall error handling for an untrusted device. Inspect returned flags. `UBLK_U_CMD_GET_FEATURES` (6.5+) probes support beforehand; see [feature flags](/guide/features/).

## What happens when the server goes away

Releasing `/dev/ublkcN` triggers recovery, usually after process exit closes fds and tears down rings. Signals or commands alone do not trigger it.

First, io_uring cancels pending ublk commands. Idle tags receive `UBLK_IO_RES_ABORT` (`-ENODEV`), ending delivery to the old process.

Then `ublk_ch_release_work_fn` runs on a workqueue: wait for zero-copy references, mark queues canceling, and requeue delivered-but-uncommitted requests with `REISSUE` or fail them with `-EIO`. It selects the new device state, resets queues for mapping/fetching by the replacement, and clears the char device's open bit.

| | No recovery flag | `USER_RECOVERY` | `+ REISSUE` | `+ FAIL_IO` |
|---|---|---|---|---|
| Requests the server held when it died | fail (`EIO`) | fail (`EIO`) | requeued, reissued to the new server | fail (`EIO`) |
| Requests queued but not yet delivered | fail | requeued | requeued | fail |
| New requests while there is no server | fail | wait | wait | fail immediately |
| `/dev/ublkbN` | removed | kept | kept | kept |
| Resulting state | `UBLK_S_DEV_DEAD` | `UBLK_S_DEV_QUIESCED` | `UBLK_S_DEV_QUIESCED` | `UBLK_S_DEV_FAIL_IO` |

`FAIL_IO` rejects new submissions with `BLK_STS_TARGET`: direct/raw I/O sees `EREMOTEIO`, possibly translated to `EIO` by a filesystem. `QUIESCED` requests remain on the block-layer requeue list until recovery or stop.

> [!NOTE]
> A failed write may have reached storage completely, partly, or not at all, as after a disk power loss. Filesystems handle that contract; the backend must restart with block-level consistency.

{{< diagram "device-states" "Device states. Recovery adds the two no-server states at the bottom; the device moves there when its server's /dev/ublkcN is released. `QUIESCE_DEV` starts that hand-off deliberately: the old server drains and exits, and the release moves the device into the no-server state." >}}

`UBLK_U_CMD_GET_DEV_INFO` reports the current state in `ublksrv_ctrl_dev_info.state`:

| State | Value | Since | Meaning |
|---|---|---|---|
| {{< uapi "UBLK_S_DEV_DEAD" >}} | 0 | 6.0 | No disk: not started yet, or stopped |
| {{< uapi "UBLK_S_DEV_LIVE" >}} | 1 | 6.0 | Disk present and a server attached |
| {{< uapi "UBLK_S_DEV_QUIESCED" >}} | 2 | 6.1 | Disk present, no server, I/O waits |
| {{< uapi "UBLK_S_DEV_FAIL_IO" >}} | 3 | 6.13 | Disk present, no server, I/O fails |

## Recovering a device

{{< diagram "recovery-sequence" "Recovering a device after its server exits. The kernel keeps the disk; the new server re-attaches with START_USER_RECOVERY, a full set of FETCH_REQs, and END_USER_RECOVERY." >}}

The replacement needs the device ID. Omit `ADD_DEV` and `SET_PARAMS`; the device and parameters already exist.

1. Read `UBLK_U_CMD_GET_DEV_INFO`. Require `UBLK_F_USER_RECOVERY` and state QUIESCED or FAIL_IO. Use its `nr_hw_queues`, `queue_depth`, and `max_io_buf_bytes`, fixed at creation; query `UBLK_U_CMD_GET_PARAMS` for capacity/block sizes.
2. Send {{< uapi "UBLK_U_CMD_START_USER_RECOVERY" >}}. Missing USER_RECOVERY returns `-EINVAL`; an open old char device or nonrecoverable state returns `-EBUSY`. Release is asynchronous: retry with short backoff immediately after a crash. Persistent EBUSY means the old process or another reference still holds `/dev/ublkcN`.
3. Open `/dev/ublkcN`, becoming its server; another open returns `-EBUSY`. Map descriptors, create queue rings, and issue `UBLK_U_IO_FETCH_REQ` for every tag as at [startup](/guide/data-plane/).
4. Send {{< uapi "UBLK_U_CMD_END_USER_RECOVERY" >}} with `data[0]` equal to the opener's PID (thread-group ID), or receive `-EINVAL`. Wait from a control thread while queues prime, or issue it after they have. A signal returns `-EINTR`; success returns 0, marks LIVE, and delivers queued/reissued requests through the new fetches.

```c
/* new server, recovering device `id` */
struct ublksrv_ctrl_dev_info info;
ctrl_cmd(UBLK_U_CMD_GET_DEV_INFO, id, .addr = &info, .len = sizeof(info));
if (!(info.flags & UBLK_F_USER_RECOVERY) ||
    (info.state != UBLK_S_DEV_QUIESCED && info.state != UBLK_S_DEV_FAIL_IO))
        fail("device is not waiting for recovery");

for (tries = 0; ; tries++) {
        ret = ctrl_cmd(UBLK_U_CMD_START_USER_RECOVERY, id);
        if (ret != -EBUSY || tries == 50)
                break;
        sleep_ms(100);                  /* old /dev/ublkcN still being released */
}

int cfd = open("/dev/ublkc<id>", O_RDWR);
for (q = 0; q < info.nr_hw_queues; q++)
        start_queue_thread(q, cfd, info.queue_depth, info.max_io_buf_bytes);
        /* each thread: mmap descriptors, set up its ring, FETCH_REQ every tag */

ctrl_cmd(UBLK_U_CMD_END_USER_RECOVERY, id, .data = getpid());  /* blocks until all fetched */
```

### What survives and what does not

The kernel retains the ID, gendisk, open `/dev/ublkbN` fds, parameters, flags, queue count/depth/buffer size, and uninterpreted `ublksrv_flags` returned by GET_DEV_INFO. `SET_PARAMS` returns `-EACCES` after disk creation; resize with `UBLK_U_CMD_UPDATE_SIZE` ({{< since "6.16" >}}) after recovery.

Recreate descriptor mappings, rings, per-tag buffers/daemons, backend connections, open files, and caches. Without a volatile-cache flag, every acknowledged write must already be durable. With it, acknowledged but unflushed writes may be lost, as with a physical disk's cache; filesystem flushes supply durability.

Persist the device ID and backend identity for the replacement. The kernel has no name lookup.

## Planned upgrades: QUIESCE_DEV

{{< since "6.16" >}} {{< uapi "UBLK_U_CMD_QUIESCE_DEV" >}} lets a server drain before replacement, avoiding failed or replayed in-flight requests. It requires `UBLK_F_QUIESCE` and `UBLK_F_USER_RECOVERY`. The kernel selftest server requests QUIESCE whenever recovery and kernel support permit.

| Field | Value |
|---|---|
| `data[0]` | timeout in milliseconds; 0 waits forever |
| result | 0 on success; `-EOPNOTSUPP` without `UBLK_F_QUIESCE`; `-ENODEV` if the device has no disk or is `DEAD`; `-EBUSY` on timeout; `-EINTR` on a signal |

On LIVE devices, mark queues canceling and poll every 3 ms for at least one idle tag per queue. Its waiting fetch is the channel for notifying the server. Once every queue has one, cancel waiting fetches with `UBLK_IO_RES_ABORT`. Already QUIESCED/FAIL_IO devices return 0 immediately.

The old server must:

- Retire tags receiving `UBLK_IO_RES_ABORT`; do not fetch them again.
- Commit owned requests. Their re-armed fetches receive no new requests, which are requeued; only ring teardown or device stop completes those fetches with ABORT.
- After committing owned tags and aborting others, close `/dev/ublkcN` and exit. Waiting for re-armed fetches stalls handoff until timeout, a former go-ublk Detach bug.

Release moves the device to QUIESCED/FAIL_IO with nothing in flight; the replacement recovers it. Applications pause without I/O errors. Any authorized control caller can issue QUIESCE_DEV: the new binary, old server responding to a signal, or an admin tool.

Before 6.16, handoff requires abrupt release and recovery. Use REISSUE to avoid failing in-flight requests.

## Choosing a mode

- `USER_RECOVERY | USER_RECOVERY_REISSUE` preserves mounted filesystems by replaying outstanding requests. Repeated overwrites are harmless for file/block backends; zone append, append-only logs, and write-counting backends may duplicate side effects. Kernel documentation suggests read-only filesystems and VM backends.
- `USER_RECOVERY` alone fails in-flight requests. Metadata/journal EIO can abort a journal or remount read-only; use it for drained QUIESCE_DEV handovers or raw consumers that retry.
- `USER_RECOVERY | USER_RECOVERY_FAIL_IO` suits RAID, multipath, or applications that act on errors and cannot wait indefinitely for a server.
- No flag couples device lifetime to server lifetime.

## Pitfalls

**Quiesced I/O can wait forever.** Without a successful restart, USER_RECOVERY/REISSUE leave processes in uninterruptible sleep. `UBLK_U_CMD_STOP_DEV` accepts QUIESCED/FAIL_IO, aborts waiting requests, and removes the disk; DEL_DEV frees the ID. See [control plane](/guide/control-plane/).

**A replacement can die mid-recovery.** Fetching only part of a queue before death can leave uncancellable commands without `0842186d2c4e` ("ublk: reset per-IO canceled flag on each fetch", CVE-2026-53124). Fixed in 7.1, stable 7.0.10, and Ubuntu `linux-hwe-7.0` from 7.0.0-28.

**Test the recovery reset path.** `f7700a4415af` ("ublk: fix use-after-free in `ublk_cancel_cmd()`") fixes USER_RECOVERY too. It entered v7.1-rc3 without `Cc: stable`, and is absent from 7.0.y and `linux-hwe-7.0` through 7.0.0-39. See [kernel bugs](/guide/kernel-bugs/).

**Batch recovery has separate fixes.** 7.0 includes "ublk: fix batch I/O recovery -ENODEV error" and "ublk: fix canceling flag handling in batch I/O recovery". After QUIESCE_DEV, `UBLK_F_BATCH_IO` also needs "ublk: clear force_abort in ublk_queue_reset_io_flags()" (7.3-rc3, stable 7.2.7). See [matrix findings](/guide/kernel-bugs/#found-by-go-ublks-kernel-matrix).

**Unprivileged devices** cannot use recovery at all; see [unprivileged devices](/guide/unprivileged/).

## go-ublk

Set `DeviceParams.Recovery`:

| Mode | Flags | In-flight I/O when the server dies | New I/O until recovery |
|---|---|---|---|
| `RecoveryReissue` | `USER_RECOVERY \| USER_RECOVERY_REISSUE` | requeued and reissued to the new server | held |
| `RecoveryQueue` | `USER_RECOVERY` | failed | held |
| `RecoveryFailIO` | `USER_RECOVERY \| USER_RECOVERY_FAIL_IO` (6.13+) | failed | failed |

`Device.Detach` releases without deleting, first sending QUIESCE_DEV when available (6.16+), except on batch devices. A crash leaves the same device state. `ublk.Recover(ctx, id, params, opts)` reads geometry/features, retries START_USER_RECOVERY on EBUSY for up to 30 seconds while the old char device releases, starts queues, and sends END_USER_RECOVERY. `DeviceParams.Tag` persists in `ublksrv_flags`; locate devices with `ublk.FindDevices(tag)`.

`ublk-loop -recovery` and the [systemd units](/go-ublk/deployment/) restart after crashes and upgrade on `SIGUSR2`, preserving the mount. Recover reads zero-copy/batch modes, zoned parameters, and integrity format; supply the backend. Batch Detach skips QUIESCE_DEV to avoid the force_abort bug, letting the kernel reissue outstanding requests. The suite tests default/batch/integrity crash and live handoffs, RecoveryQueue holding I/O, and RecoveryFailIO returning `EREMOTEIO`. Shared-memory regions survive registration but cannot be served by the replacement.