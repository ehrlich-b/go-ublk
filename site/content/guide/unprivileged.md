---
title: "Unprivileged devices"
linkTitle: "Unprivileged devices"
description: "UBLK_F_UNPRIVILEGED_DEV: letting a non-root user create and serve ublk devices, the device-path permission check, and the limits that come with it."
weight: 100
---

{{< uapi "UBLK_F_UNPRIVILEGED_DEV" >}} {{< since "6.3" >}} lets users without `CAP_SYS_ADMIN` create, serve, and access devices, including in containers and per-user services. The kernel checks char-node permissions, limits device count, and rejects modes that could expose kernel memory.

## The moving parts

1. Grant access to `/dev/ublk-control`, root-only by default.
2. Set `ublksrv_ctrl_dev_info.flags` to include `UBLK_F_UNPRIVILEGED_DEV`. Without it or `CAP_SYS_ADMIN`, `ADD_DEV` returns `-EPERM`. Privileged callers have the flag cleared; inspect returned flags.
3. The kernel overwrites `owner_uid`/`owner_gid` with the creator's IDs in the initial user namespace.
4. Use udev to transfer `/dev/ublkcN` and `/dev/ublkbN` ownership; the kernel does not chown them.
5. Later device-control commands carry the char path for permission checks.

## Udev configuration

Expose the control node, then transfer device nodes to their owners:

```text
# /etc/udev/rules.d/99-ublk.rules (sketch)
KERNEL=="ublk-control", MODE="0666"
KERNEL=="ublk[bc]*", ACTION=="add", RUN+="/usr/local/sbin/ublk-chown %k"
```

The helper queries `GET_DEV_INFO2` and chowns both nodes to `owner_uid`:`owner_gid`. ublksrv supplies `utils/ublk_dev.rules` and `utils/ublk_chown.sh` calling `ublk_user_id`; go-ublk supplies `examples/ublk-chown` and `99-ublk-unprivileged.rules`. World-writable control permits any user to add devices, bounded by `ublks_max`.

## The device-path prefix

A device ID is insufficient for authorization. Prefix unprivileged control buffers with the char-node path for inode permission checks:

```text
header.addr ─► ┌──────────────────────────────┬───────────────────────────┐
               │ "/dev/ublkc3\0" (zero padded) │ the command's own payload │
               └──────────────────────────────┴───────────────────────────┘
               │◄──────── dev_path_len ───────►│◄── len - dev_path_len ───►│
```

Relevant `struct ublksrv_ctrl_cmd` fields:

| Field | Meaning |
|---|---|
| `addr` | Start of the buffer: path first, payload after it |
| `len` | Path plus payload, in bytes |
| `dev_path_len` | Bytes reserved for the path, including the terminating NUL. Must be nonzero, at most `PATH_MAX`, and at most `len`. |
| `data[0]` | Unchanged; inline arguments such as a PID or timeout stay here |

The kernel reads up to `dev_path_len` bytes, stopping at NUL, resolves symlinks, then requires:

- a character node matching this device's number, or `-EPERM`;
- `inode_permission` read access for `GET_DEV_INFO`, `GET_DEV_INFO2`, `GET_QUEUE_AFFINITY`, `GET_PARAMS`, and `GET_FEATURES`; read/write for `START_DEV`, `STOP_DEV`, `DEL_DEV`, `SET_PARAMS`, `START_USER_RECOVERY`, `END_USER_RECOVERY`, `UPDATE_SIZE`, and `QUIESCE_DEV`.

After validation, advance addr and reduce len by `dev_path_len`. Payload-free `START_DEV`/`STOP_DEV`/`DEL_DEV` still need `len == dev_path_len`. 7.3-rc5 adds `TRY_STOP_DEV`/`REG_BUF`/`UNREG_BUF` to read/write permissions. Unlisted commands return `-EINVAL`, including `DEL_DEV_ASYNC` in 6.17 and 7.3-rc5.

`ADD_DEV` has no existing device to check; `UBLK_U_CMD_GET_FEATURES` runs before device lookup. Neither takes a path prefix.

libublk-rs uses a zero-padded 32-byte path slot (`dev_path_len = 32`), preserving 8-byte payload alignment:

```c
/* control command for an unprivileged device */
#define PATH_SLOT 32
uint8_t buf[PATH_SLOT + sizeof(payload)] = { 0 };
snprintf((char *)buf, PATH_SLOT, "/dev/ublkc%u", dev_id);
memcpy(buf + PATH_SLOT, &payload, sizeof(payload));

struct ublksrv_ctrl_cmd c = {
        .dev_id       = dev_id,
        .queue_id     = (__u16)-1,
        .addr         = (__u64)buf,
        .len          = sizeof(buf),
        .dev_path_len = PATH_SLOT,
};
/* for a read command, the result lands at buf + PATH_SLOT */
```

## GET_DEV_INFO2 and compatibility

`UBLK_CMD_GET_DEV_INFO2` (legacy 0x12, encoded `UBLK_U_CMD_GET_DEV_INFO2`) returns the same ublksrv_ctrl_dev_info as `GET_DEV_INFO` but always requires a readable char path. This works before the caller knows whether the device is unprivileged. With udev owner-only permissions, only the owner can retrieve info, as kernel documentation describes. Privileged devices require `CAP_SYS_ADMIN` and the path check.

Kernel compatibility guidance:

- Supporting unprivileged devices: use `GET_DEV_INFO2`, falling back to `GET_DEV_INFO` before 6.3, where unprivileged devices cannot exist.
- Without unprivileged support: use `GET_DEV_INFO`.

libublk-rs falls back from `GET_DEV_INFO2` on any error.

## Limits and restrictions

`ublks_max` defaults to 64; reaching it returns `-EACCES`. Adjust `/sys/module/ublk_drv/parameters/ublks_max` up to the minor-number limit. Since 6.15 it counts only unprivileged devices; 6.3-6.14 counted all devices. Before 6.7 it was set only at module load.

USER_COPY, SUPPORT_ZERO_COPY, or AUTO_BUF_REG with UNPRIVILEGED_DEV returns `-EINVAL`: unfilled successful READs could expose kernel memory. Use copy or `NEED_GET_DATA`, optionally with shared-memory zero copy. See [copy modes](/guide/data-copy/).

USER_RECOVERY/USER_RECOVERY_REISSUE are silently cleared so an untrusted server cannot stall I/O indefinitely by dying. See [recovery](/guide/recovery/).

Any serving task without `CAP_SYS_ADMIN` suppresses partition scanning, preventing untrusted input to kernel parsers; the check runs at each fetch. {{< uapi "UBLK_F_NO_AUTO_PART_SCAN" >}} (7.0+) suppresses scanning explicitly.

Request timeout sends `SIGKILL` to an unprivileged server; privileged devices reset the timer. The block-layer default is 30 s, with no driver override. Root can change `/sys/block/ublkbN/queue/io_timeout`; the unprivileged owner cannot. Bound backend waits accordingly.

Without `CAP_SYS_ADMIN`, opening `/dev/ublkbN` requires both UID and GID to match its owner, regardless of node permissions. This prevents a user from exposing crafted data to others.

`START_DEV`/`END_USER_RECOVERY` data[0] must match the char-device opener's thread-group ID. PID namespaces need "ublk: fix ublksrv pid handling for pid namespaces" (6.19, stable 6.18.8, Ubuntu linux-hwe-6.17 6.17.0-41). Unfixed 6.17-6.18.7 compares local and initial-namespace PIDs and returns `-EINVAL`, even for privileged servers.

## Containers

A container can manage devices only where char nodes are visible and accessible, the kernel documentation's container-aware model. Expose `/dev/ublk-control` and device nodes, and permit runtime device numbers in cgroups: char major is dynamic (`ublk-char` in `/proc/devices`), minor is device ID; control uses misc major 10/dynamic minor; block nodes use blkext major 259/dynamic minors.

## go-ublk

Set `DeviceParams.EnableUnprivileged`. go-ublk prefixes device-control payloads and waits up to five seconds for udev ownership before `SET_PARAMS`. Install `examples/ublk-chown` (queries `GET_DEV_INFO` and chowns nodes) and `99-ublk-unprivileged.rules`. Without the rule, `Create` returns an explanatory `EACCES`.

go-ublk excludes fixed-buffer zero copy, zoned mode, and integrity for unprivileged devices. Its suite verifies I/O from a non-root server.