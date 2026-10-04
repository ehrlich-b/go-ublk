---
title: "User recovery and quiesce"
linkTitle: "User recovery & quiesce"
description: "Keeping /dev/ublkbN alive across a server crash or upgrade: the recovery flags, the QUIESCED and FAIL_IO states, QUIESCE_DEV, and the recovery sequence."
weight: 90
---

By default a ublk device lives exactly as long as its server. When the server's `/dev/ublkcN` file is released, because the process exited, crashed or was killed, the kernel stops the device: requests the server held fail, new requests fail, `del_gendisk` runs and `/dev/ublkbN` disappears. A filesystem mounted on it sees a burst of I/O errors and is finished until someone unmounts it and starts over.

User recovery decouples the two. The block device, its ID, its parameters and everything stacked on top of it survive the server's exit; a new server process attaches to the same device and carries on. This is what makes it possible to upgrade a ublk server, or survive a crash, without unmounting.

## The flags

Recovery is chosen per device at `ADD_DEV` time, through `ublksrv_ctrl_dev_info.flags`:

| Flag | Since | Bit | What it adds |
|---|---|---|---|
| {{< uapi "UBLK_F_USER_RECOVERY" >}} | 6.1 | `1 << 3` | The device survives a server exit. Requests the server held are failed; new requests wait. |
| {{< uapi "UBLK_F_USER_RECOVERY_REISSUE" >}} | 6.1 | `1 << 4` | Requests the server held are requeued and reissued to the next server instead of failed. |
| {{< uapi "UBLK_F_USER_RECOVERY_FAIL_IO" >}} | 6.13 | `1 << 9` | While there is no server, every request fails immediately instead of waiting. |
| {{< uapi "UBLK_F_QUIESCE" >}} | 6.16 | `1 << 12` | Enables `UBLK_U_CMD_QUIESCE_DEV`, for handing a live device to a new server on purpose. |

`ADD_DEV` accepts exactly four combinations of the recovery bits and fails anything else with `-EINVAL`:

- none (the device dies with its server)
- `USER_RECOVERY`
- `USER_RECOVERY | USER_RECOVERY_REISSUE`
- `USER_RECOVERY | USER_RECOVERY_FAIL_IO`

`UBLK_F_QUIESCE` without `UBLK_F_USER_RECOVERY` is also `-EINVAL`. On an unprivileged device the kernel silently clears `USER_RECOVERY` and `USER_RECOVERY_REISSUE` rather than failing, because recovery can stall error handling on a device nobody trusts; read the negotiated `flags` that `ADD_DEV` writes back to see what you actually got. On 6.5 and later, `UBLK_U_CMD_GET_FEATURES` tells you up front whether the kernel knows a flag at all (see [feature flags](/guide/features/)).

## What happens when the server goes away

Recovery is triggered by the release of the `/dev/ublkcN` file, not by a signal or a command. Normally that means the server process is gone: its file descriptors are closed and its io_uring instances torn down. The kernel then works in two stages.

First, io_uring cancels every ublk command still pending in the dying rings. Commands for idle tags complete with `UBLK_IO_RES_ABORT` (`-ENODEV`), and nothing is left that could deliver a request to the old process.

Second, the char-device release handler (`ublk_ch_release_work_fn` in the driver) runs from a workqueue. It waits until any zero-copy buffer references are dropped, marks every queue as canceling, and walks every tag the server owned at the moment it died, meaning requests it had received and not yet committed. Each of those is either requeued (with `REISSUE`) or completed with `-EIO`. Then it picks the device's new state from the flags, resets the per-queue state so a new process can map and fetch again, and clears the "open" bit on `/dev/ublkcN`.

| | No recovery flag | `USER_RECOVERY` | `+ REISSUE` | `+ FAIL_IO` |
|---|---|---|---|---|
| Requests the server held when it died | fail (`EIO`) | fail (`EIO`) | requeued, reissued to the new server | fail (`EIO`) |
| Requests queued but not yet delivered | fail | requeued | requeued | fail |
| New requests while there is no server | fail | wait | wait | fail immediately |
| `/dev/ublkbN` | removed | kept | kept | kept |
| Resulting state | `UBLK_S_DEV_DEAD` | `UBLK_S_DEV_QUIESCED` | `UBLK_S_DEV_QUIESCED` | `UBLK_S_DEV_FAIL_IO` |

In the `FAIL_IO` state new requests are rejected at submission with `BLK_STS_TARGET`, which direct and raw I/O sees as `EREMOTEIO` (a filesystem above may report `EIO` instead). Waiting requests in the `QUIESCED` state sit on the block layer's requeue list until recovery finishes, or until someone stops the device.

> [!NOTE]
> "Fail with `EIO`" does not mean "untouched". A write the dead server was processing may have reached its backend completely, partly or not at all. That is the same contract as a real disk that loses power mid-write, and filesystems are built for it, but your backend has to be consistent at block granularity when it restarts.

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

The new server is an ordinary process that knows the device ID. It does not send `ADD_DEV` or `SET_PARAMS`; the device and its parameters already exist.

1. **Read the device.** Send `UBLK_U_CMD_GET_DEV_INFO` for the ID. Check that `flags` contains `UBLK_F_USER_RECOVERY` and that `state` is `QUIESCED` or `FAIL_IO`. Take `nr_hw_queues`, `queue_depth` and `max_io_buf_bytes` from the reply rather than from your configuration: they were fixed when the device was added, and the kernel will expect exactly that many queues and tags. Send `UBLK_U_CMD_GET_PARAMS` if you need the size or block sizes.
2. **Start recovery.** Send {{< uapi "UBLK_U_CMD_START_USER_RECOVERY" >}}. It fails with `-EINVAL` if the device was created without `USER_RECOVERY`, and with `-EBUSY` while the old `/dev/ublkcN` is still open or the device is not in a recoverable state. The release handler runs asynchronously, so a new server that starts within milliseconds of the old one dying can see `-EBUSY`; retry with a short backoff. `-EBUSY` that persists means the old process is still alive, or something else holds `/dev/ublkcN`.
3. **Attach the queues.** Open `/dev/ublkcN` (only one open file is allowed at a time; a second open fails with `-EBUSY`). The process that opens it becomes the device's server. Map each queue's descriptor array, set up the per-queue rings, and issue `UBLK_U_IO_FETCH_REQ` for every tag of every queue, exactly as at first startup (see the [data plane](/guide/data-plane/)).
4. **End recovery.** Send {{< uapi "UBLK_U_CMD_END_USER_RECOVERY" >}} with `data[0]` set to the server's PID, which must be the thread-group ID of the process that opened `/dev/ublkcN` or the command fails with `-EINVAL`. Like `START_DEV`, it waits until every tag has been fetched, so send it from a control thread while the queue threads prime, or after they have. A signal interrupts the wait with `-EINTR`. When it returns 0 the device is `LIVE` again and the kernel kicks the requeue list: waiting and reissued requests arrive as completions of the fetches you just submitted.

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

The kernel keeps the device ID, the gendisk and every file descriptor open on `/dev/ublkbN`, the parameters, the negotiated flags, the queue count, depth and buffer size, and the `ublksrv_flags` field of the device info, which the driver stores without interpreting and returns verbatim from `GET_DEV_INFO`. `SET_PARAMS` is refused with `-EACCES` once the disk has been created, so recovery cannot change the geometry; use `UBLK_U_CMD_UPDATE_SIZE` ({{< since "6.16" >}}) after recovery if the size must change.

It forgets everything that belonged to the old process: the descriptor mappings, the rings, the per-tag buffers and the per-tag daemon tasks. Your server's own state is your problem. Backend connections, open files and caches have to be rebuilt, and the backend must already hold every write the old server acknowledged. If the device advertises a volatile write cache, an acknowledged but unflushed write lost in the crash is the same event as a disk losing its cache on power failure: allowed by the contract, and handled by the filesystem's flushes. If it does not, every acknowledged write must have been durable.

Persist the device ID somewhere the next server can find it, together with whatever identifies the backend. The kernel has no lookup by name.

## Planned upgrades: QUIESCE_DEV

{{< since "6.16" >}} Killing a server to replace it works, but every request it held at that moment fails (or is reissued). {{< uapi "UBLK_U_CMD_QUIESCE_DEV" >}} lets the old server stop cleanly first. It requires `UBLK_F_QUIESCE`, which in turn requires `UBLK_F_USER_RECOVERY`; the kernel selftests server sets `QUIESCE` automatically whenever the kernel supports it and recovery was requested.

| Field | Value |
|---|---|
| `data[0]` | timeout in milliseconds; 0 waits forever |
| result | 0 on success; `-EOPNOTSUPP` without `UBLK_F_QUIESCE`; `-ENODEV` if the device has no disk or is `DEAD`; `-EBUSY` on timeout; `-EINTR` on a signal |

On a `LIVE` device the kernel marks every queue as canceling, then polls every 3 ms until each queue has at least one idle tag, meaning a tag whose fetch command is still waiting in the server's ring. That idle command is the only channel the kernel has to tell the server anything, so the wait is what makes the command reliable. When every queue has one, the kernel cancels the waiting fetch commands, and the server sees them complete with `UBLK_IO_RES_ABORT`. If the device is already `QUIESCED` or `FAIL_IO`, the command returns 0 without doing anything.

The old server has to cooperate:

- Treat `UBLK_IO_RES_ABORT` on a tag as final and do not fetch that tag again.
- Finish the requests it currently owns and commit them. Each commit completes its request, but the fetch it re-arms will receive nothing (new requests are being requeued) and is only completed, with `UBLK_IO_RES_ABORT`, when the server's io_uring is torn down or the device is stopped.
- Once every tag it owned has been committed and every other tag aborted, close `/dev/ublkcN` and exit without waiting for the re-armed fetches. (Waiting for them stalls the handoff until a timeout; go-ublk's `Detach` once did exactly that.)

The release handler then moves the device to `QUIESCED` (or `FAIL_IO`) exactly as after a crash, except that nothing was in flight, and the new server runs the recovery sequence above. Applications see a pause, not an error. Any process allowed to send control commands for the device can issue `QUIESCE_DEV`: the new binary, the old one in response to a signal, or an admin tool.

On kernels older than 6.16 the only way to hand over is to stop the old server abruptly and recover, so choose `REISSUE` if requests in flight at the switch must not fail.

## Choosing a mode

- **`USER_RECOVERY | USER_RECOVERY_REISSUE`** is what keeps a mounted filesystem alive across a crash. Nothing fails; requests the dead server held are simply sent again. The price is that a request can reach your backend twice. Overwriting a block with the same data is idempotent, so a file or block backend is fine; anything with side effects per request (zone append, an append-only log, a backend that counts writes) is not. The kernel documentation suggests it for read-only filesystems and VM backends.
- **`USER_RECOVERY` alone** keeps the device but fails what was in flight. A filesystem that gets `EIO` on a metadata or journal write typically aborts its journal or remounts read-only, so on its own this mostly suits planned hand-overs with `QUIESCE_DEV`, where nothing is in flight, or raw consumers that retry.
- **`USER_RECOVERY | USER_RECOVERY_FAIL_IO`** keeps the device but turns "no server" into fast errors. Use it when something above the device can act on errors, such as RAID, multipath or an application with its own retry policy, and must not hang waiting for a server that may never return.
- **No flag** when the device's lifetime should be the server's lifetime.

## Pitfalls

**A quiesced device waits forever.** With plain `USER_RECOVERY` or `REISSUE`, I/O issued while there is no server blocks until a server recovers the device. If your supervisor cannot restart the server, those processes sit in uninterruptible sleep. Have a way out: `UBLK_U_CMD_STOP_DEV` works from `QUIESCED` and `FAIL_IO`, aborts the waiting requests and removes the disk, and `DEL_DEV` then frees the ID. See the [control plane](/guide/control-plane/).

**A recovering server that dies half-way.** If a new server dies after fetching only some tags of a queue, kernels without `0842186d2c4e` ("ublk: reset per-IO canceled flag on each fetch", CVE-2026-53124) can leave the cancellation of those fetches stuck forever. It is in mainline 7.1 and was backported to 7.0.10, so Ubuntu's `linux-hwe-7.0` has it from 7.0.0-28.

**The reset path itself.** `f7700a4415af` ("ublk: fix use-after-free in `ublk_cancel_cmd()`") also covers the `USER_RECOVERY` reset path. It first appears in v7.1-rc3, carries no `Cc: stable`, and is not in 7.0.y or in any `linux-hwe-7.0` build through 7.0.0-39. Test recovery on the kernel you will ship. [Known kernel bugs](/guide/kernel-bugs/) has the details.

**Batch I/O.** Recovery for `UBLK_F_BATCH_IO` devices had its own fixes ("ublk: fix batch I/O recovery -ENODEV error", "ublk: fix canceling flag handling in batch I/O recovery"), both in 7.0. Recovering a batch device after `QUIESCE_DEV` also needs "ublk: clear force_abort in ublk_queue_reset_io_flags()" (7.3-rc3, stable 7.2.7); see [known kernel bugs](/guide/kernel-bugs/#found-by-go-ublks-kernel-matrix).

**Unprivileged devices** cannot use recovery at all; see [unprivileged devices](/guide/unprivileged/).

## go-ublk

go-ublk implements user recovery. Set `DeviceParams.Recovery`:

| Mode | Flags | In-flight I/O when the server dies | New I/O until recovery |
|---|---|---|---|
| `RecoveryReissue` | `USER_RECOVERY \| USER_RECOVERY_REISSUE` | requeued and reissued to the new server | held |
| `RecoveryQueue` | `USER_RECOVERY` | failed | held |
| `RecoveryFailIO` | `USER_RECOVERY \| USER_RECOVERY_FAIL_IO` (6.13+) | failed | failed |

`Device.Detach` is the planned handoff: it sends `QUIESCE_DEV` when the kernel has it (6.16+), then lets go of the device without deleting it. A crash leaves the device in the same state. `ublk.Recover(ctx, id, params, opts)` takes a device over: it reads the geometry and features back from the kernel, retries `START_USER_RECOVERY` while it returns `-EBUSY` (until the kernel has released the old server's `/dev/ublkcN`, up to 30 seconds), starts the queues and sends `END_USER_RECOVERY`. `DeviceParams.Tag` is stored in `ublksrv_flags`, so a restarted process finds its devices with `ublk.FindDevices(tag)`.

The `ublk-loop` example's `-recovery` flag and the [systemd units](/go-ublk/deployment/) put this together: a crash restarts the service, a `SIGUSR2` upgrades it, and the filesystem on the device stays mounted throughout. `Recover` reads the device's modes back from the kernel too (zero copy, batch I/O, zoned parameters, integrity format), so the new process supplies only a backend. On batch devices `Detach` skips `QUIESCE_DEV`, which loses I/O across the handoff on kernels without the force_abort fix described below, and lets the kernel reissue outstanding requests instead. The conformance suite tests crash recovery and live handoff in the default, batch and integrity configurations, and that `RecoveryQueue` holds I/O while `RecoveryFailIO` fails it with `EREMOTEIO`. Shared-memory regions stay registered across a recovery but cannot be served by the new process.
