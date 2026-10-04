---
title: "Feature flags"
linkTitle: "Feature flags"
description: "All UBLK_F_* feature flags: what each one changes, which release added it, what it requires or excludes, and how negotiation works."
weight: 60
---

A ublk device's behavior is fixed at creation by a 64-bit set of `UBLK_F_*` flags in `struct ublksrv_ctrl_dev_info.flags`. Some flags select a protocol (how data moves, how I/O commands are issued), some enable optional control commands, and a few are pure capability bits that the kernel sets on its own. The 7.3-rc5 header defines 21 of them, bits 0 through 20.

This chapter explains how flags are negotiated, summarizes every flag in one table, and then describes each group. The chapters on [data copy modes](/guide/data-copy/), [batch I/O](/guide/batch-io/), [user recovery](/guide/recovery/), [unprivileged devices](/guide/unprivileged/), [zoned devices](/guide/zoned/) and [integrity metadata](/guide/integrity/) go deeper on the larger features.

## How negotiation works

There are two mechanisms, and a careful server uses both.

### The ADD_DEV round trip

{{< uapi "UBLK_U_CMD_ADD_DEV" >}} takes a `struct ublksrv_ctrl_dev_info` by pointer. The server fills in `flags` with what it wants; the kernel validates the request, edits `flags`, and copies the whole structure back before the command completes. The returned `flags` are the device's features for its entire life. Nothing changes them later.

Unknown bits do not fail the command. The kernel masks the request with its compiled-in `UBLK_F_ALL`, so a flag the running kernel has never heard of is silently cleared. **Always compare the returned flags with the ones you asked for** and treat a missing bit as "not supported here".

In the 7.3-rc5 driver, `ublk_ctrl_add_dev()` applies these rules in order (6.17 has the same list minus the integrity, descriptor-size and batch items):

1. **Privilege.** A caller with `CAP_SYS_ADMIN` always gets a privileged device: `UBLK_F_UNPRIVILEGED_DEV` is cleared. A caller without it must set `UBLK_F_UNPRIVILEGED_DEV`, or the command fails with `-EPERM`.
2. **Recovery combinations.** The recovery bits must be one of: none, `USER_RECOVERY`, `USER_RECOVERY | USER_RECOVERY_REISSUE`, or `USER_RECOVERY | USER_RECOVERY_FAIL_IO`. Anything else (for example `REISSUE` without `USER_RECOVERY`, or both modifiers) is `-EINVAL`.
3. **Quiesce.** `UBLK_F_QUIESCE` without `UBLK_F_USER_RECOVERY` is `-EINVAL`.
4. **Unprivileged restrictions.** For an unprivileged device the kernel silently drops `USER_RECOVERY` and `USER_RECOVERY_REISSUE`, and rejects `USER_COPY`, `SUPPORT_ZERO_COPY` or `AUTO_BUF_REG` with `-EINVAL`, because each of those would let an untrusted server leave kernel memory uninitialized in a READ.
5. **Integrity.** `UBLK_F_INTEGRITY` without `UBLK_F_USER_COPY` is `-EINVAL`.
6. **Descriptor size.** With `UBLK_F_IO_DESC_SIZE`, `io_desc_size` must be at least 24, a multiple of 8, and at most 256, or `-EINVAL`. Without it, the kernel sets `io_desc_size` to 24.
7. **Masking.** `flags &= UBLK_F_ALL`. `UBLK_F_INTEGRITY` is only in `UBLK_F_ALL` when the kernel is built with `CONFIG_BLK_DEV_INTEGRITY`.
8. **Forced capability bits.** The kernel ORs in `CMD_IOCTL_ENCODE` (6.4+), `URING_CMD_COMP_IN_TASK` (6.5+), `PER_IO_DAEMON` (6.16+) and `BUF_REG_OFF_DAEMON` (6.17+), and since 7.0 also `SAFE_STOP_DEV`. These report what the driver does; they do not select anything.
9. **Batch cleanup.** With `UBLK_F_BATCH_IO`, `PER_IO_DAEMON` and `NEED_GET_DATA` are cleared again: batch mode has no per-I/O daemons and no get-data step.
10. **Copy-mode cleanup.** `NEED_GET_DATA` is cleared if `USER_COPY`, `SUPPORT_ZERO_COPY` or `AUTO_BUF_REG` is set, since none of those modes copies WRITE data at all.
11. **Zoned.** `UBLK_F_ZONED` requires a kernel built with `CONFIG_BLK_DEV_ZONED` and either `USER_COPY` or `SUPPORT_ZERO_COPY`; otherwise `-EINVAL`.

### GET_FEATURES

{{< since "6.5" >}} {{< uapi "UBLK_U_CMD_GET_FEATURES" >}} returns the kernel's whole `UBLK_F_ALL` mask: every flag this kernel knows, independent of any device. Point `addr` at an 8-byte buffer and set `len` to exactly `UBLK_FEATURES_LEN` (8); any other length is `-EINVAL`. The driver handles it before looking up a device or checking permissions, so `dev_id` is ignored and anyone who can open `/dev/ublk-control` may ask.

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

On kernels older than 6.5 the opcode is not recognized. Because the old driver looks the device up by `dev_id` before dispatching, the error is typically `-ENODEV` rather than `-EOPNOTSUPP`; treat any failure as "no GET_FEATURES". (With a `dev_id` that names an existing device, 6.3 and 6.4 return `-ENOTSUPP`, 524; 6.0 to 6.2 return `-EPERM` to a caller without `CAP_SYS_ADMIN`.)

The two mechanisms answer different questions. `GET_FEATURES` says what the driver supports in general, which is what you want before deciding how to configure a device. The `ADD_DEV` result says what this particular device got after validation, privilege checks and kernel configuration, which is what you must actually honor.

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

These decide how request data moves between the block request and the server. Exactly one mode applies to a device: the default copy, `NEED_GET_DATA`, `USER_COPY`, or zero copy (`SUPPORT_ZERO_COPY` and/or `AUTO_BUF_REG`). `SHMEM_ZC` is a fast path layered on top. The [data copy chapter](/guide/data-copy/) covers each mode in full; this is the short version.

- {{< uapi "UBLK_F_SUPPORT_ZERO_COPY" >}} (bit 0). The bit was reserved in 6.0 but did nothing useful until 6.15 added `UBLK_U_IO_REGISTER_IO_BUF` and `UBLK_U_IO_UNREGISTER_IO_BUF`. The server installs a request's pages into an io_uring sparse buffer table and points `*_FIXED` operations at them; the server's own code never touches the data. With this flag, `FETCH_REQ` and `COMMIT_AND_FETCH_REQ` must pass `addr = 0`; a non-zero address is `-EINVAL`.
- {{< uapi "UBLK_F_NEED_GET_DATA" >}} (bit 2). A WRITE first arrives with `UBLK_IO_RES_NEED_GET_DATA` (1) and no data; the server answers with `UBLK_U_IO_NEED_GET_DATA` carrying a buffer address, and only then does the kernel copy. One extra round trip per WRITE. It exists for servers that cannot pre-allocate per-tag buffers; new servers should not use it.
- {{< uapi "UBLK_F_USER_COPY" >}} (bit 7). The kernel never copies. The server reads WRITE data with `pread()` and supplies READ data with `pwrite()` on `/dev/ublkcN`, at an offset that encodes queue, tag and byte offset. Fetch and commit pass `addr = 0`.
- {{< uapi "UBLK_F_AUTO_BUF_REG" >}} (bit 11). Like zero copy, but the kernel registers the request buffer into the server's io_uring before delivering the request and unregisters it at commit, removing two commands per I/O. The buffer index travels in the `FETCH_REQ` / `COMMIT_AND_FETCH_REQ` SQE's `addr` field as a `struct ublk_auto_buf_reg`.
- {{< uapi "UBLK_F_BUF_REG_OFF_DAEMON" >}} (bit 14). Lets any task, not only the tag's daemon, issue `REGISTER_IO_BUF` and `UNREGISTER_IO_BUF`; `UNREGISTER_IO_BUF` then ignores `q_id` and `tag`. Forced on since it was introduced in 6.17, so it only tells you the kernel behaves this way.
- {{< uapi "UBLK_F_SHMEM_ZC" >}} (bit 19). The server registers shared memory (memfd, hugetlbfs) with `UBLK_U_CMD_REG_BUF`. When an O_DIRECT request's pages are inside a registered buffer, the descriptor carries `UBLK_IO_F_SHMEM_ZC` and `addr` encodes buffer index and offset instead of a server address. Requests that do not match fall back to the normal path silently. Unlike the other zero-copy modes, the kernel accepts it on unprivileged devices.

## Recovery flags

These decide what happens when the server process exits while the device is live. The [recovery chapter](/guide/recovery/) has the state machine and the full sequence.

- {{< uapi "UBLK_F_USER_RECOVERY" >}} (bit 3). Without it, a server exit stops the device: `/dev/ublkbN` disappears and every pending request fails. With it, the device stays, moves to `UBLK_S_DEV_QUIESCED`, queues new I/O, and fails the requests the server had already received. A new server takes over with `START_USER_RECOVERY` and `END_USER_RECOVERY`.
- {{< uapi "UBLK_F_USER_RECOVERY_REISSUE" >}} (bit 4). Same, but requests the dead server had received are requeued and later reissued to the new server. That can execute a write twice, so use it only for backends where a repeated write is harmless.
- {{< uapi "UBLK_F_USER_RECOVERY_FAIL_IO" >}} (bit 9, 6.13). The device stays but enters `UBLK_S_DEV_FAIL_IO`: all I/O, old and new, fails immediately until a new server recovers it. Applications see errors instead of hanging.
- {{< uapi "UBLK_F_QUIESCE" >}} (bit 12, 6.16). Enables `UBLK_U_CMD_QUIESCE_DEV`, which quiesces a live device on request so a new server can take over without the old one crashing. `data[0]` is a timeout in milliseconds, 0 meaning forever; the command returns `-EBUSY` if no tag on some queue goes idle in time. The kernel only accepts this flag together with `USER_RECOVERY`.

Recovery flags are not available for unprivileged devices: the kernel clears `USER_RECOVERY` and `REISSUE` rather than failing `ADD_DEV`, so check the returned flags.

## Protocol and threading flags

- {{< uapi "UBLK_F_URING_CMD_COMP_IN_TASK" >}} (bit 1). In the original driver this chose between two ways of completing an I/O command in the daemon's context (`task_work_add` versus `io_uring_cmd_complete_in_task`), mainly for benchmarking. Current drivers always use the io_uring path and force the bit on, so requesting it changes nothing (the `task_work_add` path was removed in 6.5; 6.1 to 6.4 already forced the bit for modular builds).
- {{< uapi "UBLK_F_CMD_IOCTL_ENCODE" >}} (bit 6, 6.4). Marks a kernel that understands the ioctl-encoded `UBLK_U_CMD_*` and `UBLK_U_IO_*` opcodes, for example `UBLK_U_CMD_ADD_DEV = _IOWR('u', 0x04, struct ublksrv_ctrl_cmd) = 0xc0207504`. The driver forces the bit on; whether the old raw opcodes (`UBLK_CMD_ADD_DEV = 0x04`) are still accepted depends on `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES`. Commands added after 6.4, such as `GET_FEATURES`, exist only in encoded form. Use the encoded opcodes. The bit has been forced on since 6.4.
- {{< uapi "UBLK_F_PER_IO_DAEMON" >}} (bit 13, 6.16). The task that issues `FETCH_REQ` for a (queue, tag) pair becomes that I/O's daemon, and only it may issue later commands for the pair. Before 6.16 the daemon was per queue, so one thread had to own every tag of a queue. With per-I/O daemons a server can spread one queue's tags across several threads. Forced on, except on batch-mode devices, which have no daemons at all.
- {{< uapi "UBLK_F_BATCH_IO" >}} (bit 15, 7.0). Replaces the per-tag `FETCH_REQ` / `COMMIT_AND_FETCH_REQ` / `NEED_GET_DATA` commands with per-queue batch commands: `PREP_IO_CMDS`, `COMMIT_IO_CMDS` and a multishot `FETCH_IO_CMDS`. There are no per-I/O daemons in batch mode; any task may handle any tag. See [Batch I/O](/guide/batch-io/).

## Device feature flags

- {{< uapi "UBLK_F_UNPRIVILEGED_DEV" >}} (bit 5, 6.3). Lets a user without `CAP_SYS_ADMIN` create a device they own. Every control command except `ADD_DEV` must then carry the path of `/dev/ublkcN` so the kernel can check the caller's permission on it. Unprivileged devices cannot use user copy, zero copy or recovery, and the kernel suppresses partition scanning whenever any queue is served by a task without `CAP_SYS_ADMIN`. See [Unprivileged devices](/guide/unprivileged/).
- {{< uapi "UBLK_F_ZONED" >}} (bit 8, 6.6). Exposes a host-managed zoned device: zone open, close, finish, reset and reset-all, zone append, and `REPORT_ZONES` requests reach the server. Requires `UBLK_PARAM_TYPE_ZONED` parameters, a non-zero `chunk_sectors`, and `USER_COPY` or `SUPPORT_ZERO_COPY`. See [Zoned devices](/guide/zoned/).
- {{< uapi "UBLK_F_UPDATE_SIZE" >}} (bit 10, 6.16). Advertises `UBLK_U_CMD_UPDATE_SIZE`, which sets a started device's capacity to `data[0]` 512-byte sectors and notifies userspace with a resize uevent. Neither 6.17 nor 7.3-rc5 checks the flag when handling the command, so treat it as a capability bit and request it anyway. On a device that has not been started, 7.3-rc5 returns `-ENODEV`.
- {{< uapi "UBLK_F_INTEGRITY" >}} (bit 16, 7.0). Requests can carry an integrity (protection information) buffer, flagged with `UBLK_IO_F_INTEGRITY` and copied with `pread`/`pwrite` using `UBLKSRV_IO_INTEGRITY_FLAG` in the offset. Requires `USER_COPY` and `UBLK_PARAM_TYPE_INTEGRITY`. See [Integrity metadata](/guide/integrity/).
- {{< uapi "UBLK_F_SAFE_STOP_DEV" >}} (bit 17, 7.0). Advertises `UBLK_U_CMD_TRY_STOP_DEV` (`0xc0207517`), a stop that fails with `-EBUSY` if anything has `/dev/ublkbN` open, instead of tearing the disk out from under a mounted filesystem. If nothing has it open, the kernel blocks new opens and stops the device exactly like `STOP_DEV`. A device that is not started returns `-ENODEV`. 7.3-rc5 forces the bit on and does not check it when handling the command.
- {{< uapi "UBLK_F_NO_AUTO_PART_SCAN" >}} (bit 18, 7.0). Turns off the partition scan that normally follows `START_DEV`. Since 6.19 the scan no longer runs inside `START_DEV`: the kernel suppresses it while adding the disk, to avoid a deadlock if the server fails mid-scan, and then schedules it asynchronously for trusted servers (earlier kernels scanned synchronously inside `add_disk`, so the server saw READs while `START_DEV` was in flight). With this flag the asynchronous scan is skipped and the suppression is lifted, so a later manual rescan (`partprobe`, `BLKRRPART`) still works. Partition scanning stays off permanently when any queue is served by a task without `CAP_SYS_ADMIN` (since 7.0-rc4).
- {{< uapi "UBLK_F_IO_DESC_SIZE" >}} (bit 20, 7.3). The server chooses the size of each descriptor slot in `ublksrv_ctrl_dev_info.io_desc_size`, the 16-bit field at offset 6 that was `pad0` before 7.3: at least 24 (`sizeof(struct ublksrv_io_desc)`), a multiple of 8, at most 256. Without the flag the kernel reports 24 there. The descriptor for tag *t* is at `t * io_desc_size`, a queue's mmap length is `round_up(queue_depth * io_desc_size, PAGE_SIZE)`, and the per-queue mmap stride becomes `round_up(UBLK_MAX_QUEUE_DEPTH * io_desc_size, PAGE_SIZE)`. In 7.3 the kernel fills only the 24-byte `struct ublksrv_io_desc` in each slot; the rest is padding, which lets a server avoid false sharing between descriptors served by different threads and leaves room for future fields.

## go-ublk

go-ublk calls `GET_FEATURES` before every `ADD_DEV` and compares the request against it. A requested feature the kernel does not list fails `Create` with an error that wraps `syscall.EOPNOTSUPP` and names the missing flags, before anything is created. The flags `ADD_DEV` returns are checked again, so a feature the kernel silently clears is an error too, never a quietly weaker device. On kernels without `GET_FEATURES` (before 6.5) the check happens only after `ADD_DEV`.

Each `DeviceParams` option maps to its flag: `Recovery` to the `USER_RECOVERY` family, `EnableZeroCopy`, `EnableUserCopy`, `EnableUnprivileged`, `EnableZoned`, `Integrity`, `NeedGetData`, `BatchIO`, `ThreadsPerQueue` (`PER_IO_DAEMON`), `SharedMemoryZeroCopy`, `SafeStop`, `NoPartitionScan` and `IODescSize`. Three flags are requested whenever the kernel has them, because they cost nothing: `UPDATE_SIZE` (for `Device.Resize`), `QUIESCE` on recoverable devices (for `Device.Detach`), and `AUTO_BUF_REG` with zero copy. go-ublk always sends ioctl-encoded opcodes and does not request `CMD_IOCTL_ENCODE` or `URING_CMD_COMP_IN_TASK`, which the kernel forces on.

`ublk.Probe()` reports what the running kernel supports, and `Device.Features()` what a device was granted.

## Reference table

Every feature flag with its value, introducing release and go-ublk status. Expand a row for details.

{{< uapi-table kind="feature" >}}
