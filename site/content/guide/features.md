---
title: "Feature flags"
linkTitle: "Feature flags"
description: "All UBLK_F_* feature flags: what each one changes, which release added it, what it requires or excludes, and how negotiation works."
weight: 60
---

At creation, `struct ublksrv_ctrl_dev_info.flags` selects protocols and optional commands through a 64-bit `UBLK_F_*` mask. Some bits merely report kernel capabilities. Linux 7.3-rc5 defines 21 flags, bits 0-20.

For protocol details, see [data copy](/guide/data-copy/), [batch I/O](/guide/batch-io/), [recovery](/guide/recovery/), [unprivileged devices](/guide/unprivileged/), [zoned devices](/guide/zoned/), and [integrity](/guide/integrity/).

## How negotiation works

Probe capabilities with `GET_FEATURES`, then check the device's negotiated `ADD_DEV` flags.

### The ADD_DEV round trip

{{< uapi "UBLK_U_CMD_ADD_DEV" >}} validates the requested `flags` in `struct ublksrv_ctrl_dev_info`, edits them, and copies the structure back. The returned flags govern the device's lifetime.

Unknown bits are silently masked with `UBLK_F_ALL`. Compare requested and returned flags; a missing bit means the device lacks that feature.

`ublk_ctrl_add_dev()` applies these rules in 7.3-rc5 (6.17 lacks the integrity, descriptor-size, and batch steps):

1. With `CAP_SYS_ADMIN`, clear `UNPRIVILEGED_DEV`. Without it, require that flag or return `-EPERM`.
2. Accept no recovery flags, `USER_RECOVERY`, or that flag with either `REISSUE` or `FAIL_IO`. Other combinations return `-EINVAL`.
3. Reject `QUIESCE` without `USER_RECOVERY` (`-EINVAL`).
4. For unprivileged devices, clear `USER_RECOVERY`/`USER_RECOVERY_REISSUE`; reject `USER_COPY`, `SUPPORT_ZERO_COPY`, or `AUTO_BUF_REG` (`-EINVAL`). These modes could expose uninitialized READ memory.
5. Reject `INTEGRITY` without `USER_COPY` (`-EINVAL`).
6. With `IO_DESC_SIZE`, require 24-256 bytes in multiples of 8 or return `-EINVAL`; otherwise set 24.
7. Apply `flags &= UBLK_F_ALL`. `INTEGRITY` requires `CONFIG_BLK_DEV_INTEGRITY` in this mask.
8. Force capability bits: `CMD_IOCTL_ENCODE` (6.4+), `URING_CMD_COMP_IN_TASK` (6.5+), `PER_IO_DAEMON` (6.16+), `BUF_REG_OFF_DAEMON` (6.17+), and `SAFE_STOP_DEV` (7.0+).
9. For `BATCH_IO`, clear `PER_IO_DAEMON` and `NEED_GET_DATA`: batch has neither per-tag daemons nor get-data.
10. For `USER_COPY`, `SUPPORT_ZERO_COPY`, or `AUTO_BUF_REG`, clear `NEED_GET_DATA`: these modes skip WRITE copying at delivery.
11. For `ZONED`, require `CONFIG_BLK_DEV_ZONED` and either `USER_COPY` or `SUPPORT_ZERO_COPY`, or return `-EINVAL`.

### GET_FEATURES

{{< since "6.5" >}} {{< uapi "UBLK_U_CMD_GET_FEATURES" >}} returns `UBLK_F_ALL` independently of any device. Set `addr` to an 8-byte buffer and `len` to `UBLK_FEATURES_LEN` (8); other lengths return `-EINVAL`. The driver ignores `dev_id` and handles this before permission checks, so anyone able to open `/dev/ublk-control` can probe.

```c
__u64 features = 0;
struct ublksrv_ctrl_cmd c = {
	.dev_id   = (__u32)-1,
	.queue_id = (__u16)-1,
	.addr     = (__u64)(uintptr_t)&features,
	.len      = UBLK_FEATURES_LEN,          /* must be exactly 8 */
};
int res = ctrl_cmd(ctrl_ring, UBLK_U_CMD_GET_FEATURES, &c); /* 0x80207513 */
if (res < 0)
	features = 0;   /* pre-6.5 kernel: fall back to probing with ADD_DEV */
```

Before 6.5, the unknown opcode usually returns `-ENODEV`: the driver looks up `dev_id` before dispatch. With an existing ID, 6.3/6.4 return `-ENOTSUPP` (524); 6.0-6.2 return `-EPERM` without `CAP_SYS_ADMIN`. Treat failure as missing `GET_FEATURES` and inspect `ADD_DEV` instead.

`GET_FEATURES` describes driver capabilities; `ADD_DEV` confirms what survived validation, privilege checks, and configuration for this device.

## All flags at a glance

"Since" is the first mainline release whose UAPI header defines the flag. Requirements are enforced by the kernel unless marked as convention.

| Bit | Flag | Since | What it does | Requires / excludes | Chapter |
|---|---|---|---|---|---|
| 0 | `UBLK_F_SUPPORT_ZERO_COPY` | 6.0 (works from 6.15) | Zero copy through io_uring fixed buffers: `REGISTER_IO_BUF` / `UNREGISTER_IO_BUF` | Excludes unprivileged | [Data copy](/guide/data-copy/) |
| 1 | `UBLK_F_URING_CMD_COMP_IN_TASK` | 6.0 | Complete I/O commands with `io_uring_cmd_complete_in_task` | Forced on by current kernels | below |
| 2 | `UBLK_F_NEED_GET_DATA` | 6.0 | Deliver WRITEs without data; server supplies a buffer with `NEED_GET_DATA` | Cleared by any non-copy mode | [Data copy](/guide/data-copy/) |
| 3 | `UBLK_F_USER_RECOVERY` | 6.1 | Keep the device across a server exit; recover with a new server | Dropped for unprivileged | [Recovery](/guide/recovery/) |
| 4 | `UBLK_F_USER_RECOVERY_REISSUE` | 6.1 | On server exit, requeue I/O the server had received instead of failing it | `USER_RECOVERY`; not with `FAIL_IO` | [Recovery](/guide/recovery/) |
| 5 | `UBLK_F_UNPRIVILEGED_DEV` | 6.3 | Device created and controlled by a non-root owner | Excludes user copy and zero copy | [Unprivileged](/guide/unprivileged/) |
| 6 | `UBLK_F_CMD_IOCTL_ENCODE` | 6.4 | Commands use ioctl-encoded `UBLK_U_*` opcodes | Forced on | below |
| 7 | `UBLK_F_USER_COPY` | 6.5 | Server copies data with `pread`/`pwrite` on `/dev/ublkcN` | Excludes unprivileged | [Data copy](/guide/data-copy/) |
| 8 | `UBLK_F_ZONED` | 6.6 | Zoned block device: zone ops, `REPORT_ZONES`, zone append | `USER_COPY` or `SUPPORT_ZERO_COPY` | [Zoned](/guide/zoned/) |
| 9 | `UBLK_F_USER_RECOVERY_FAIL_IO` | 6.13 | With no server, fail new I/O immediately instead of queueing it | `USER_RECOVERY`; not with `REISSUE` | [Recovery](/guide/recovery/) |
| 10 | `UBLK_F_UPDATE_SIZE` | 6.16 | Resize a live device with `UBLK_U_CMD_UPDATE_SIZE` | | below |
| 11 | `UBLK_F_AUTO_BUF_REG` | 6.16 | Kernel registers each request buffer in the server's io_uring automatically | Excludes unprivileged | [Data copy](/guide/data-copy/) |
| 12 | `UBLK_F_QUIESCE` | 6.16 | Enables `UBLK_U_CMD_QUIESCE_DEV` for planned server handover | `USER_RECOVERY` | [Recovery](/guide/recovery/) |
| 13 | `UBLK_F_PER_IO_DAEMON` | 6.16 | Each (queue, tag) may be served by its own task | Forced on, except with `BATCH_IO` | below |
| 14 | `UBLK_F_BUF_REG_OFF_DAEMON` | 6.17 | `REGISTER_IO_BUF` / `UNREGISTER_IO_BUF` from any task | Forced on | [Data copy](/guide/data-copy/) |
| 15 | `UBLK_F_BATCH_IO` | 7.0 | Per-queue batch commands replace per-tag FETCH and COMMIT | Excludes the per-tag commands; clears `PER_IO_DAEMON` and `NEED_GET_DATA` | [Batch I/O](/guide/batch-io/) |
| 16 | `UBLK_F_INTEGRITY` | 7.0 | Requests carry integrity (protection information) buffers | `USER_COPY`; kernel with `CONFIG_BLK_DEV_INTEGRITY` | [Integrity](/guide/integrity/) |
| 17 | `UBLK_F_SAFE_STOP_DEV` | 7.0 | Advertises `UBLK_U_CMD_TRY_STOP_DEV`: stop only if nobody has the disk open | Forced on in 7.3 | below |
| 18 | `UBLK_F_NO_AUTO_PART_SCAN` | 7.0 | Do not scan for partitions when the device starts | | below |
| 19 | `UBLK_F_SHMEM_ZC` | 7.1 | Shared-memory zero copy via `UBLK_U_CMD_REG_BUF` | Allowed for unprivileged | [Data copy](/guide/data-copy/) |
| 20 | `UBLK_F_IO_DESC_SIZE` | 7.3 | Server-chosen descriptor slot size in `ublksrv_ctrl_dev_info.io_desc_size` | 24 to 256, multiple of 8 | below |

## Data copy flags

Choose default copy, `NEED_GET_DATA`, `USER_COPY`, or zero copy (`SUPPORT_ZERO_COPY` and/or `AUTO_BUF_REG`); `SHMEM_ZC` adds a fast path. See [data copy modes](/guide/data-copy/).

- {{< uapi "UBLK_F_SUPPORT_ZERO_COPY" >}} installs request pages in an io_uring sparse buffer table for `*_FIXED` operations. The server cannot touch the bytes. FETCH/COMMIT require `addr = 0`; otherwise `-EINVAL`. Reserved in 6.0, it became functional with REGISTER/UNREGISTER_IO_BUF in 6.15.
- {{< uapi "UBLK_F_NEED_GET_DATA" >}} delivers a WRITE as `UBLK_IO_RES_NEED_GET_DATA` (1); answer with `UBLK_U_IO_NEED_GET_DATA` and a buffer address to receive data. It adds one round trip. Retained for servers without pre-allocated buffers; avoid in new servers.
- {{< uapi "UBLK_F_USER_COPY" >}} uses `pread()` for WRITE data and `pwrite()` for READ data on `/dev/ublkcN`, with queue/tag/byte offset encoded in the position. FETCH/COMMIT use `addr = 0` and perform no copy.
- {{< uapi "UBLK_F_AUTO_BUF_REG" >}} registers before delivery and unregisters at commit, saving two commands. Fetch/commit SQE `addr` carries `struct ublk_auto_buf_reg` with the buffer index.
- {{< uapi "UBLK_F_BUF_REG_OFF_DAEMON" >}} permits registration from any task; unregistration ignores `q_id`/`tag`. Forced on since 6.17.
- {{< uapi "UBLK_F_SHMEM_ZC" >}} registers memfd/hugetlbfs memory through `UBLK_U_CMD_REG_BUF`. Matching O_DIRECT requests carry `UBLK_IO_F_SHMEM_ZC` and an index/offset in `addr`; others silently use the normal path. Unprivileged devices may use it.

## Recovery flags

On server exit, the [recovery flags](/guide/recovery/) determine device and request survival:

- {{< uapi "UBLK_F_USER_RECOVERY" >}} keeps the device in `UBLK_S_DEV_QUIESCED`, queues new I/O, and fails delivered requests. A replacement uses START/END_USER_RECOVERY. Without recovery, the device disappears and pending requests fail.
- {{< uapi "UBLK_F_USER_RECOVERY_REISSUE" >}} also requeues delivered requests. Writes may execute twice; the backend must tolerate replay.
- {{< uapi "UBLK_F_USER_RECOVERY_FAIL_IO" >}} (6.13) keeps the device in `UBLK_S_DEV_FAIL_IO` but fails old and new I/O until recovery.
- {{< uapi "UBLK_F_QUIESCE" >}} (6.16) enables planned handover through `UBLK_U_CMD_QUIESCE_DEV`. `data[0]` is a timeout in milliseconds; 0 waits forever. If any queue has no idle tag before timeout, return `-EBUSY`. Requires `USER_RECOVERY`.

Unprivileged devices lose `USER_RECOVERY`/`REISSUE` silently at `ADD_DEV`; inspect the reply.

## Protocol and threading flags

- {{< uapi "UBLK_F_URING_CMD_COMP_IN_TASK" >}} originally selected `io_uring_cmd_complete_in_task` over `task_work_add` for daemon-context completion, mainly for benchmarking. The latter path disappeared in 6.5; the bit is now forced. Modular builds already forced it in 6.1-6.4.
- {{< uapi "UBLK_F_CMD_IOCTL_ENCODE" >}} reports ioctl-encoded `UBLK_U_CMD_*`/`UBLK_U_IO_*` support, e.g. `_IOWR('u', 0x04, struct ublksrv_ctrl_cmd) = 0xc0207504` for ADD_DEV. Forced since 6.4. Legacy ADD_DEV is 0x04; legacy acceptance depends on `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES`. Use encoded commands; post-6.4 additions such as GET_FEATURES have no legacy form.
- {{< uapi "UBLK_F_PER_IO_DAEMON" >}} (6.16) lets threads split a queue's tags. The task issuing a tag's FETCH owns subsequent commands; older kernels require one task per queue. Forced except with batch I/O.
- {{< uapi "UBLK_F_BATCH_IO" >}} (7.0) replaces per-tag FETCH/COMMIT/NEED_GET_DATA with PREP_IO_CMDS, COMMIT_IO_CMDS, and multishot FETCH_IO_CMDS. Any task may handle any tag; see [batch I/O](/guide/batch-io/).

## Device feature flags

- {{< uapi "UBLK_F_UNPRIVILEGED_DEV" >}} (6.3) permits creation without `CAP_SYS_ADMIN`. Device control commands after ADD_DEV carry `/dev/ublkcN` for permission checks. It excludes user copy, fixed-buffer zero copy, and recovery; unprivileged queue tasks suppress partition scanning. See [unprivileged devices](/guide/unprivileged/).
- {{< uapi "UBLK_F_ZONED" >}} (6.6) delivers zone open/close/finish/reset/reset-all, append, and REPORT_ZONES. Requires `UBLK_PARAM_TYPE_ZONED`, non-zero `chunk_sectors`, and USER_COPY or SUPPORT_ZERO_COPY. See [zoned devices](/guide/zoned/).
- {{< uapi "UBLK_F_UPDATE_SIZE" >}} (6.16) advertises UPDATE_SIZE: resize a started device to `data[0]` 512-byte sectors and emit a uevent. Neither 6.17 nor 7.3-rc5 checks the flag; request it anyway. 7.3-rc5 returns `-ENODEV` before startup.
- {{< uapi "UBLK_F_INTEGRITY" >}} (7.0) delivers protection information with `UBLK_IO_F_INTEGRITY`. Copy it through `pread`/`pwrite` with `UBLKSRV_IO_INTEGRITY_FLAG` in the offset. Requires USER_COPY and `UBLK_PARAM_TYPE_INTEGRITY`; see [integrity](/guide/integrity/).
- {{< uapi "UBLK_F_SAFE_STOP_DEV" >}} (7.0) advertises TRY_STOP_DEV (`0xc0207517`): return `-EBUSY` with disk openers or `-ENODEV` before startup; otherwise block opens and perform STOP_DEV. 7.3-rc5 forces the bit without checking it at dispatch.
- {{< uapi "UBLK_F_NO_AUTO_PART_SCAN" >}} (7.0) skips startup's partition scan while permitting manual `partprobe`/`BLKRRPART`. Before 6.19, scanning ran inside `add_disk` and required serving READs before START_DEV returned. From 6.19, trusted servers scan asynchronously to avoid deadlock on mid-scan failure. Scanning stays permanently disabled if any queue task lacks `CAP_SYS_ADMIN` (since 7.0-rc4).
- {{< uapi "UBLK_F_IO_DESC_SIZE" >}} (7.3) sets the slot size through the 16-bit `io_desc_size` at offset 6 (formerly `pad0`): 24-256 bytes, multiple of 8; default 24. Tag *t* is at `t * io_desc_size`. Queue mmap length is `round_up(queue_depth * io_desc_size, PAGE_SIZE)`; stride is `round_up(UBLK_MAX_QUEUE_DEPTH * io_desc_size, PAGE_SIZE)`. Only the 24-byte `ublksrv_io_desc` is filled; padding permits future fields and avoids false sharing across threads.

## go-ublk

go-ublk checks requested features against `GET_FEATURES` before `ADD_DEV`. Missing flags fail `Create` with a named error wrapping `syscall.EOPNOTSUPP`. It also checks the returned flags, except that privileged callers may have `UNPRIVILEGED_DEV` cleared. Before 6.5, only this post-creation check is available.

`DeviceParams` maps options to flags: `Recovery` selects the USER_RECOVERY family; other options are `EnableZeroCopy`, `EnableUserCopy`, `EnableUnprivileged`, `EnableZoned`, `Integrity`, `NeedGetData`, `BatchIO`, `ThreadsPerQueue` (PER_IO_DAEMON), `SharedMemoryZeroCopy`, `SafeStop`, `NoPartitionScan`, and `IODescSize`. When supported, go-ublk also requests UPDATE_SIZE for `Device.Resize`, QUIESCE for recoverable devices' `Device.Detach`, and AUTO_BUF_REG for zero copy. These add no protocol cost. It always uses ioctl-encoded commands without requesting the forced CMD_IOCTL_ENCODE or URING_CMD_COMP_IN_TASK bits.

`ublk.Probe()` reports what the running kernel supports, and `Device.Features()` what a device was granted.

## Reference table

Values, introducing releases, and go-ublk support; expand rows for details.

{{< uapi-table kind="feature" >}}
