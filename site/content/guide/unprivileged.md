---
title: "Unprivileged devices"
linkTitle: "Unprivileged devices"
description: "UBLK_F_UNPRIVILEGED_DEV: letting a non-root user create and serve ublk devices, the device-path permission check, and the limits that come with it."
weight: 100
---

Every ublk control command normally requires `CAP_SYS_ADMIN`. {{< uapi "UBLK_F_UNPRIVILEGED_DEV" >}} {{< since "6.3" >}} lets an ordinary user create a device, serve it and use its block node, which is what makes ublk usable inside containers and by per-user services. The kernel compensates for trusting less: it checks file permissions on the device's char node for every command, caps how many such devices exist, and refuses the features that would let a buggy or hostile server leak kernel memory.

## The moving parts

1. `/dev/ublk-control` must be openable by the user. By default it is root-only.
2. The user sends `ADD_DEV` with `UBLK_F_UNPRIVILEGED_DEV` set in `ublksrv_ctrl_dev_info.flags`. Without `CAP_SYS_ADMIN` and without the flag, `ADD_DEV` fails with `-EPERM`. With `CAP_SYS_ADMIN`, the kernel clears the flag and creates an ordinary privileged device, so a server running as root never gets an unprivileged device even if it asks for one. Check the flags `ADD_DEV` writes back.
3. The kernel stores the caller's UID and GID (mapped into the initial user namespace) in `owner_uid` and `owner_gid`. Whatever the server put in those fields is overwritten: the creator always owns the device.
4. The kernel does not change the ownership of the new `/dev/ublkcN` and `/dev/ublkbN` nodes. A udev rule has to give them to the owner.
5. Every later control command for the device carries the path of its char node, and the kernel checks that the caller has permission to open that node.

## Udev configuration

Two rules are needed: one that opens up the control node, and one that hands each new device's nodes to its owner. A minimal sketch:

```text
# /etc/udev/rules.d/99-ublk.rules (sketch)
KERNEL=="ublk-control", MODE="0666"
KERNEL=="ublk[bc]*", ACTION=="add", RUN+="/usr/local/sbin/ublk-chown %k"
```

The helper looks the device up with `UBLK_U_CMD_GET_DEV_INFO2` and `chown`s both nodes to `owner_uid:owner_gid`. ublksrv ships a rule and a helper script that do this. <!-- VERIFY: names and contents of ublksrv's udev rule and chown helper (believed to be a 99-ublk-dev.rules file and ublk_chown.sh) --> Making the control node world-writable lets any user add devices; the `ublks_max` limit below is what bounds that.

## The device-path prefix

The kernel cannot tell from a device ID alone whether the caller should be allowed to touch that device, so for an unprivileged device each control command must name the device's char node, and the kernel applies ordinary file-permission checks to it. The path goes at the front of the command's buffer:

```text
header.addr ─► ┌──────────────────────────────┬───────────────────────────┐
               │ "/dev/ublkc3\0" (zero padded) │ the command's own payload │
               └──────────────────────────────┴───────────────────────────┘
               │◄──────── dev_path_len ───────►│◄── len - dev_path_len ───►│
```

The fields of `struct ublksrv_ctrl_cmd` involved:

| Field | Meaning |
|---|---|
| `addr` | Start of the buffer: path first, payload after it |
| `len` | Path plus payload, in bytes |
| `dev_path_len` | Bytes reserved for the path, including the terminating NUL. Must be nonzero, at most `PATH_MAX`, and at most `len`. |
| `data[0]` | Unchanged; inline arguments such as a PID or timeout stay here |

The kernel copies `dev_path_len` bytes from `addr`, stopping at the first NUL, resolves the path (following symlinks), and checks two things:

- the path names a character device whose device number is this ublk device's char node, otherwise `-EPERM`;
- the caller passes `inode_permission` on it: read access for `GET_DEV_INFO`, `GET_DEV_INFO2`, `GET_QUEUE_AFFINITY`, `GET_PARAMS` and `GET_FEATURES`; read and write access for `START_DEV`, `STOP_DEV`, `DEL_DEV`, `SET_PARAMS`, `START_USER_RECOVERY`, `END_USER_RECOVERY`, `UPDATE_SIZE` and `QUIESCE_DEV`.

If both pass, the kernel advances `addr` and shrinks `len` by `dev_path_len` before running the command, so the payload must start exactly `dev_path_len` bytes in. Commands without a payload (`START_DEV`, `STOP_DEV`, `DEL_DEV`) still carry the path, with `len == dev_path_len`. 7.3-rc5 adds `TRY_STOP_DEV`, `REG_BUF` and `UNREG_BUF` to the read-and-write list. A command code that is not in either list fails with `-EINVAL` on an unprivileged device; in both 6.17 and 7.3-rc5 that includes `DEL_DEV_ASYNC`.

Two commands never carry the path: `ADD_DEV`, because the device does not exist yet, and `UBLK_U_CMD_GET_FEATURES`, which the kernel answers before looking up any device.

libublk-rs reserves a fixed 32-byte, zero-padded slot for the path and sets `dev_path_len = 32`, which keeps the payload 8-byte aligned. That is a good layout to copy:

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

`UBLK_CMD_GET_DEV_INFO2` (legacy opcode `0x12`, encoded `UBLK_U_CMD_GET_DEV_INFO2`) arrived with unprivileged devices. It returns the same `struct ublksrv_ctrl_dev_info` as `GET_DEV_INFO`, but it always carries the device path, even for a privileged device, so only a caller who can read the device's char node gets an answer. The kernel documentation phrases this as "only the user owning the requested device can retrieve the device info", which holds once udev has given the nodes to the owner. That matters because a server that does not yet know whether a device is unprivileged cannot know whether `GET_DEV_INFO` needs the path; `GET_DEV_INFO2` is correct either way. For a privileged device the caller needs `CAP_SYS_ADMIN` and must pass the path check as well.

The kernel documentation spells out the compatibility rule:

- A server that supports unprivileged devices should always send `GET_DEV_INFO2`. If it fails because the kernel predates 6.3, fall back to `GET_DEV_INFO`; unprivileged devices cannot exist on that kernel anyway.
- A server that does not support them just sends `GET_DEV_INFO`, and never gets to use the feature.

libublk-rs implements exactly this: try `GET_DEV_INFO2`, fall back to `GET_DEV_INFO` on any error.

## Limits and restrictions

**Device count.** The `ublks_max` module parameter, default 64, caps the number of unprivileged devices. `ADD_DEV` fails with `-EACCES` once it is reached. It is writable at runtime through `/sys/module/ublk_drv/parameters/ublks_max`, up to the driver's minor-number limit. In the 6.17 driver it counts only unprivileged devices; privileged devices are limited only by the minor space. <!-- VERIFY: when ublks_max changed from capping every device to capping only unprivileged ones; e2b's ublk-go README describes 64 as a system-wide default -->

**Copy and zero-copy modes.** `ADD_DEV` with `UBLK_F_UNPRIVILEGED_DEV` together with `UBLK_F_USER_COPY`, `UBLK_F_SUPPORT_ZERO_COPY` or `UBLK_F_AUTO_BUF_REG` fails with `-EINVAL`. In those modes the server is responsible for filling read buffers, and a server that returned success without writing them would hand uninitialized kernel memory to the reader. Unprivileged devices always use the default copy mode (or `NEED_GET_DATA`). See [data copy modes](/guide/data-copy/).

**No recovery.** `UBLK_F_USER_RECOVERY` and `UBLK_F_USER_RECOVERY_REISSUE` are cleared silently: an untrusted server must not be able to stall I/O indefinitely by dying. See [user recovery](/guide/recovery/).

**No partition scan.** If any task serving the device lacks `CAP_SYS_ADMIN`, the kernel suppresses the partition scan when the disk is added, so an untrusted server cannot feed the kernel's partition parsers. The check runs on every fetch. Kernels from 7.0 also offer {{< uapi "UBLK_F_NO_AUTO_PART_SCAN" >}} for turning the scan off explicitly.

**Request timeouts kill the server.** For an unprivileged device, the block layer's request timeout handler sends `SIGKILL` to the server process when a request times out; a privileged device just restarts the timer. The driver does not set its own timeout, so the block layer default applies. <!-- VERIFY: default blk-mq request timeout for ublk devices (30 s) and that it is adjustable via /sys/block/ublkbN/queue/io_timeout --> An unprivileged server must complete every request within that window, which rules out unbounded waits on a slow backend.

**Who can open the block device.** A user without `CAP_SYS_ADMIN` can open an unprivileged device's `/dev/ublkbN` only if both their UID and GID match the device's owner, whatever the node's permissions say. The rule stops one user from creating a device, granting access to others, and serving them crafted data.

**PIDs.** As for any device, `data[0]` of `START_DEV` and `END_USER_RECOVERY` must be the thread-group ID of the process that opened `/dev/ublkcN`. A server in a PID namespace needs a kernel with "ublk: fix ublksrv pid handling for pid namespaces", which Ubuntu's 6.17 kernels carry from 6.17.0-41. <!-- VERIFY: which mainline release contains the pid-namespace fix, and the exact failure on kernels without it -->

## Containers

Because permission is decided by the path the caller presents and by ordinary inode checks, a device created inside a container can be managed only from where its char node is visible and accessible. The kernel documentation calls this container-aware: a device created in one container can be controlled and accessed only inside that container. The container still needs `/dev/ublk-control` and the device's nodes to exist in its `/dev`, and its device cgroup, if any, must allow them. <!-- VERIFY: ublk char devices use a dynamically allocated major ("ublk-char"), so device-cgroup rules must be written against the runtime major number -->

## go-ublk

`DeviceParams.EnableUnprivileged` creates an unprivileged device. go-ublk prefixes every control command's payload with the device's char path, and waits up to five seconds for udev to give `/dev/ublkcN` to its owner before `SET_PARAMS`, since the node starts out root-owned. The repository ships the udev side: `examples/ublk-chown`, a small helper that reads the owner with `GET_DEV_INFO` and `chown`s the nodes, and `99-ublk-unprivileged.rules`, which runs it for every new device. Without the rule, `Create` fails with `EACCES` and says so.

Unprivileged devices cannot use zero copy, zoned mode or integrity. The conformance suite creates one as a non-root user and serves verified I/O through it.
