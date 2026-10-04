---
title: "The control plane"
linkTitle: "Control plane"
description: "Every ublk control command on /dev/ublk-control: encoding, payload, result, errors, and the device state machine they drive."
weight: 20
---

Everything about a device's existence, as opposed to its I/O, goes through `/dev/ublk-control`: creating the device, describing it, starting and stopping it, deleting it, and on newer kernels resizing, quiescing and recovering it. This chapter covers how a control command is built and then each command in turn.

## Issuing a control command

Control commands are io_uring passthrough commands. Open `/dev/ublk-control` read-write, create an io_uring with **`IORING_SETUP_SQE128`** (the driver rejects control commands from a ring without 128-byte SQEs), and for each command fill one SQE:

| SQE field | Value |
|---|---|
| `opcode` | `IORING_OP_URING_CMD` (46) |
| `fd` | the `/dev/ublk-control` file descriptor (or a registered file index with `IOSQE_FIXED_FILE`) |
| `cmd_op` | the encoded command, for example `UBLK_U_CMD_ADD_DEV` |
| `cmd[]` | a `struct ublksrv_ctrl_cmd`, at byte 48 of the SQE |
| `user_data` | anything; it comes back in the CQE |

The CQE's `res` is 0 on success or a negative errno. `UBLK_U_CMD_REG_BUF` is the only command that returns a positive value (a buffer index).

Several commands block in the kernel until something else happens: `START_DEV` until every tag has been fetched, `DEL_DEV` until the device's last reference is gone, `END_USER_RECOVERY` until the new server has fetched every tag, `QUIESCE_DEV` until its timeout. The driver never runs them inline from the submitting `io_uring_enter` (it returns `-EAGAIN` for non-blocking issue, so io_uring punts them to a worker thread), so the submitter is not stuck, but the CQE will not arrive until the condition is met. Wait with a timeout and expect `EINTR`-style interruptions. Do not issue a blocking control command from a thread whose progress it waits on: `START_DEV` sent from the only thread that was going to submit the fetches never completes.

### struct ublksrv_ctrl_cmd

| Offset | Field | Meaning |
|---|---|---|
| 0 | `__u32 dev_id` | Target device. For `ADD_DEV`, the requested ID or `U32_MAX` (-1) to let the kernel choose |
| 4 | `__u16 queue_id` | Must be -1 (`0xffff`) for commands that are not about one queue |
| 6 | `__u16 len` | Length of the buffer at `addr` |
| 8 | `__u64 addr` | User buffer, input or output depending on the command |
| 16 | `__u64 data[1]` | Inline argument: a PID, a timeout, a size, a queue ID, a buffer index |
| 24 | `__u16 dev_path_len` | Unprivileged devices and `GET_DEV_INFO2` only: length of the device path prefix at `addr`, including the NUL |
| 26 | `__u16 pad` | |
| 28 | `__u32 reserved` | |

The struct is 32 bytes. Any buffer you pass by `addr` must stay valid and unmoved until the CQE arrives, which matters in garbage-collected languages: pin it, and if you give up waiting, leak it rather than free it, because the kernel may still write to it.

### Legacy and ioctl-encoded opcodes

Linux 6.0 through 6.3 identified commands by small integers (`UBLK_CMD_ADD_DEV` is 0x04). Linux 6.4 introduced ioctl-style encodings built with `_IOR`/`_IOWR('u', nr, struct ublksrv_ctrl_cmd)`, named `UBLK_U_CMD_*`, and every command added since exists only in that form. The driver accepts both encodings unless the kernel was built without `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES`, in which case legacy numbers fail with `EOPNOTSUPP`. New servers should use the encoded form; supporting kernels older than 6.4 means also speaking the legacy numbers.

The kernel always reports `UBLK_F_CMD_IOCTL_ENCODE` in the negotiated flags of devices it creates, so a server that sees the bit knows the encoded form is understood. <!-- VERIFY: from which release the driver forces UBLK_F_CMD_IOCTL_ENCODE on in ADD_DEV (it does in 6.17) -->

## Commands

The table lists every command in the 7.3-rc5 header. "Blocks" means the CQE waits on another event.

| Command | `cmd_op` | Since | Argument | Blocks |
|---|---|---|---|---|
| `UBLK_U_CMD_ADD_DEV` | `0xc0207504` | 6.0 (encoded 6.4) | `addr` → `ublksrv_ctrl_dev_info` | |
| `UBLK_U_CMD_SET_PARAMS` | `0xc0207508` | 6.0 | `addr` → `ublk_params` | |
| `UBLK_U_CMD_GET_PARAMS` | `0x80207509` | 6.0 | `addr` → `ublk_params` | |
| `UBLK_U_CMD_START_DEV` | `0xc0207506` | 6.0 | `data[0]` = server PID | until all tags fetched |
| `UBLK_U_CMD_STOP_DEV` | `0xc0207507` | 6.0 | | while I/O drains |
| `UBLK_U_CMD_DEL_DEV` | `0xc0207505` | 6.0 | | until the last reference drops |
| `UBLK_U_CMD_GET_DEV_INFO` | `0x80207502` | 6.0 | `addr` → `ublksrv_ctrl_dev_info` | |
| `UBLK_U_CMD_GET_QUEUE_AFFINITY` | `0x80207501` | 6.0 | `data[0]` = queue, `addr` → CPU mask | |
| `UBLK_U_CMD_START_USER_RECOVERY` | `0xc0207510` | 6.1 | | |
| `UBLK_U_CMD_END_USER_RECOVERY` | `0xc0207511` | 6.1 | `data[0]` = new server PID | until all tags fetched |
| `UBLK_U_CMD_GET_DEV_INFO2` | `0x80207512` | 6.3 | path prefix + `ublksrv_ctrl_dev_info` | |
| `UBLK_U_CMD_GET_FEATURES` | `0x80207513` | 6.5 | `addr` → `__u64` | |
| `UBLK_U_CMD_DEL_DEV_ASYNC` | `0x80207514` | 6.9 | | |
| `UBLK_U_CMD_UPDATE_SIZE` | `0xc0207515` | 6.16 | `data[0]` = new size in sectors | |
| `UBLK_U_CMD_QUIESCE_DEV` | `0xc0207516` | 6.16 | `data[0]` = timeout in ms | up to the timeout |
| `UBLK_U_CMD_TRY_STOP_DEV` | `0xc0207517` | 7.0 | | |
| `UBLK_U_CMD_REG_BUF` | `0xc0207518` | 7.1 | `addr` → `ublk_shmem_buf_reg` | |
| `UBLK_U_CMD_UNREG_BUF` | `0xc0207519` | 7.1 | `data[0]` = buffer index | |

The legacy opcodes of the 6.0 and 6.1 commands are their low byte (`ADD_DEV` 0x04, `DEL_DEV` 0x05, `START_DEV` 0x06, `STOP_DEV` 0x07, `SET_PARAMS` 0x08, `GET_PARAMS` 0x09, `GET_QUEUE_AFFINITY` 0x01, `GET_DEV_INFO` 0x02, recovery 0x10 and 0x11, `GET_DEV_INFO2` 0x12).

### ADD_DEV

Creates a device and its char device `/dev/ublkcN`. `addr` points at a 64-byte `struct ublksrv_ctrl_dev_info` that the server fills in and the kernel overwrites with the negotiated result:

| Offset | Field | In | Out |
|---|---|---|---|
| 0 | `__u16 nr_hw_queues` | requested queues | clamped to the number of CPU IDs |
| 2 | `__u16 queue_depth` | 1 to 4096 | unchanged |
| 4 | `__u16 state` | ignored | `UBLK_S_DEV_DEAD` |
| 6 | `__u16 io_desc_size` | with `UBLK_F_IO_DESC_SIZE` (7.3): the descriptor size the server wants, 24 to 256 and a multiple of 8 | the descriptor size in use; 24 without the flag. Before 7.3 this field was padding |
| 8 | `__u32 max_io_buf_bytes` | largest request the server will handle | rounded down to a page |
| 12 | `__u32 dev_id` | must equal the header's `dev_id` | the assigned ID |
| 16 | `__s32 ublksrv_pid` | | |
| 24 | `__u64 flags` | requested `UBLK_F_*` | the flags the kernel actually enabled |
| 32 | `__u64 ublksrv_flags` | server's own use; the kernel stores and returns it | |
| 40 | `__u32 owner_uid`, `owner_gid` | ignored | the creating user's IDs |

Read every field back. The kernel can give you fewer queues than you asked for (one per CPU at most), a smaller buffer size, and a different feature set: it clears flags it does not know, clears `UBLK_F_NEED_GET_DATA` when a user-copy or zero-copy mode is set, and forces on flags that describe its own behavior (in 6.17: `UBLK_F_CMD_IOCTL_ENCODE`, `UBLK_F_URING_CMD_COMP_IN_TASK`, `UBLK_F_PER_IO_DAEMON` and `UBLK_F_BUF_REG_OFF_DAEMON`; 7.3-rc5 adds `UBLK_F_SAFE_STOP_DEV` and withholds `UBLK_F_PER_IO_DAEMON` from batch-I/O devices). [Feature flags](/guide/features/) lists the negotiation rules in order. The flags it returns are the contract for the device's lifetime. A server that sizes its queue threads or buffers from what it requested instead of what came back will map descriptor arrays that do not exist or overrun buffers.

`ADD_DEV` fails with:

- `EINVAL` for a depth or queue count of 0 or above 4096, a header `queue_id` other than -1, a header `dev_id` that differs from `info.dev_id`, an ID above the maximum, an invalid combination of [recovery flags](/guide/recovery/), `UBLK_F_QUIESCE` without `UBLK_F_USER_RECOVERY`, `UBLK_F_ZONED` without a user-copy or zero-copy mode, `UBLK_F_INTEGRITY` without `UBLK_F_USER_COPY`, an out-of-range `io_desc_size` with `UBLK_F_IO_DESC_SIZE`, or (for unprivileged devices) a copy mode that unprivileged servers may not use.
- `EPERM` without `CAP_SYS_ADMIN`, unless `UBLK_F_UNPRIVILEGED_DEV` is set. With the capability, that flag is silently cleared and the device is an ordinary privileged one.
- `EEXIST` if the requested ID is taken (an auto-assigned ID is the lowest free one), `EACCES` when the unprivileged-device limit is reached, `ENOMEM`.

### SET_PARAMS and GET_PARAMS

`SET_PARAMS` describes the block device that `START_DEV` will create: capacity, block sizes, maximum request size, attributes such as read-only and volatile cache, discard limits, zoned limits and so on. `addr` points at a `struct ublk_params` whose first two fields are `len` (bytes of the structure the server is passing) and `types` (a bitmask of `UBLK_PARAM_TYPE_*` saying which sub-structures are valid). The `basic` block is mandatory. [Device parameters](/guide/parameters/) covers every field and validation rule.

Parameters can be set any number of times before the first `START_DEV`, and not after it: once the disk exists the command fails with `EACCES` until the disk is gone again. A failed validation returns `EINVAL` and clears all parameters, so a later `START_DEV` fails too.

The kernel copies at most its own `sizeof(struct ublk_params)` and masks `types` to the types it knows, without telling you. A server built against a newer header that sets, say, `UBLK_PARAM_TYPE_SEGMENT` on a 6.14 kernel gets a device without segment limits and a successful return. Read the result back with `GET_PARAMS` (set `len` in the buffer first; the kernel copies `min(len, sizeof)` bytes and always fills the read-only `devt` block) if a parameter matters.

### START_DEV

Exposes `/dev/ublkbN`. `data[0]` must be the PID (thread-group ID) of the process that opened `/dev/ublkcN`; anything else, or a missing basic parameter block, is `EINVAL`.

The order matters. `START_DEV` first waits, interruptibly, until **every tag of every queue has a `FETCH_REQ` outstanding**, then allocates the disk with the limits from `SET_PARAMS`, applies the attributes, and calls `add_disk`, which emits udev events and triggers a partition scan (reads of the first sectors). So the queue threads must be submitting their fetches before or while `START_DEV` is pending, and must be serving reads from that moment. On older kernels the partition scan runs inside `add_disk`, so the server receives reads while `START_DEV` is still in flight; 7.3-rc5 defers it to a work item that runs after the command returns. <!-- VERIFY: first release with the asynchronous partition scan (present in 7.3-rc5, absent in 6.17) --> A device already live returns `EEXIST`.

Partition scanning is suppressed for devices whose queues are served by unprivileged tasks, and from Linux 7.0 can be turned off with `UBLK_F_NO_AUTO_PART_SCAN`.

### STOP_DEV

Removes `/dev/ublkbN` and returns the device to `UBLK_S_DEV_DEAD`. Internally this is `del_gendisk`, which flushes and drains: a mounted filesystem is synced through the device, and every in-flight request has to complete before the command returns. **The server must keep its queues running while `STOP_DEV` is in progress**; the drain is served by the same threads. After the disk is gone the driver completes every pending fetch with `UBLK_IO_RES_ABORT` (`-ENODEV`), which is the signal for the queue threads to exit. The command itself returns 0.

The safe shutdown order for a server is therefore:

```text
STOP_DEV            (queues still serving; returns after the drain)
stop queue threads  (their fetches complete with -ENODEV)
close io_urings, munmap descriptors, close /dev/ublkcN
DEL_DEV
```

### TRY_STOP_DEV

{{< since "7.0" >}} Stops the device only if nothing has `/dev/ublkbN` open. It fails with `EBUSY` while the disk has openers and `ENODEV` if the device has no disk; otherwise it blocks new opens and performs a normal `STOP_DEV`. Belongs to `UBLK_F_SAFE_STOP_DEV`, which 7.3-rc5 reports on every device. Useful for "detach if idle" without pulling a disk out from under a mounted filesystem.

### DEL_DEV and DEL_DEV_ASYNC

`DEL_DEV` removes `/dev/ublkcN` and frees the device ID. If the device is still live it is stopped first. The command then **waits until the device's last reference is dropped**, so that the ID can be reused as soon as it returns. Every open file description of `/dev/ublkcN` holds such a reference, including ones a server forgot it had (a duplicated fd, a descriptor array still mapped, an io_uring with the fd registered). Sending `DEL_DEV` from a process that still holds one is a self-deadlock; the wait is interruptible, so the command returns `EINTR` if the process is signalled.

`DEL_DEV_ASYNC` (Linux 6.9) does the same removal without waiting. The ID becomes reusable whenever the last reference goes away.

A device whose server died without deleting it stays registered in state `DEAD` (or, with recovery enabled, `QUIESCED` or `FAIL_IO`) and keeps the module pinned. Any process with `CAP_SYS_ADMIN` can delete it by ID.

### GET_DEV_INFO and GET_DEV_INFO2

Copy the device's current `ublksrv_ctrl_dev_info` to `addr` (`len` at least 64). Fails with `ENODEV` for an ID that does not exist, which is also how a server enumerates devices: there is no list command, so tools probe IDs.

`GET_DEV_INFO2` (Linux 6.3) is the same command with the char-device path prepended to the buffer, for the [unprivileged](/guide/unprivileged/) permission check. The kernel requires the path for `GET_DEV_INFO2` even on privileged devices, because the caller may not know which kind it is asking about.

### GET_QUEUE_AFFINITY

Returns the set of CPUs that blk-mq maps to queue `data[0]`, as a CPU bitmask in the buffer at `addr`. `len` must be a multiple of `sizeof(unsigned long)` and large enough for every possible CPU ID. A server uses it to pin each queue thread to the CPUs whose I/O lands on that queue, so a request is served on the CPU that issued it.

### GET_FEATURES

{{< since "6.5" >}} Writes the 64-bit set of `UBLK_F_*` features this kernel supports into an 8-byte buffer (`len` must be exactly `UBLK_FEATURES_LEN`, 8). It does not take a device, so it is the way to probe a kernel before creating one. On kernels before 6.5 it fails, and the fallback is to request the flags you want in `ADD_DEV` and inspect what comes back.

### UPDATE_SIZE

{{< since "6.16" >}} Changes the capacity of a started device to `data[0]` 512-byte sectors and notifies the block layer, which emits a resize uevent. It belongs to `UBLK_F_UPDATE_SIZE`, though the driver does not check the flag. Only send it to a started device: 7.3-rc5 returns `ENODEV` otherwise, and the 6.17 driver does not check at all. The server must be ready to serve the new range before it grows the device and must stop relying on the old range only after it shrinks it.

### QUIESCE_DEV, START_USER_RECOVERY, END_USER_RECOVERY

The recovery commands move a device whose server has exited, or is about to be replaced, back to a live server without removing `/dev/ublkbN`. `QUIESCE_DEV` (6.16, needs `UBLK_F_QUIESCE`) takes a timeout in milliseconds in `data[0]` and parks a live device so that a new server can take over; `START_USER_RECOVERY` and `END_USER_RECOVERY` (6.1, need `UBLK_F_USER_RECOVERY`) bracket the takeover. They are covered with the states they produce in [User recovery and quiesce](/guide/recovery/).

### REG_BUF and UNREG_BUF

{{< since "7.1" >}} Register and unregister a shared-memory buffer for `UBLK_F_SHMEM_ZC`. `REG_BUF` takes a `struct ublk_shmem_buf_reg` (address, length, flags) through `addr` and returns the buffer index; `UNREG_BUF` takes the index in `data[0]`. See [Data copy modes](/guide/data-copy/).

## Device states

The `state` field of `ublksrv_ctrl_dev_info` takes four values:

| State | Value | Meaning |
|---|---|---|
| `UBLK_S_DEV_DEAD` | 0 | No disk. A device that was added but not started, or was stopped |
| `UBLK_S_DEV_LIVE` | 1 | `/dev/ublkbN` exists and a server is serving it |
| `UBLK_S_DEV_QUIESCED` | 2 | Recovery enabled, no server; new I/O waits (6.1+) |
| `UBLK_S_DEV_FAIL_IO` | 3 | Recovery enabled with fail-fast, no server; new I/O fails (6.13+) |

{{< diagram "device-states" "`DEL_DEV` is accepted from any state. Without a recovery flag, a server exit stops the device; with one, the device waits for a new server." >}}

The driver's state machine looks as if it allows starting a device again after a stop: once the old disk is released, a server could reopen `/dev/ublkcN`, fetch every tag and send `START_DEV`. Don't. Tested across kernels, it fails with `-EBUSY`, wedges the control plane (6.10 to 6.12), or oopses (Arch 7.2.8); see [known kernel bugs](/guide/kernel-bugs/#found-by-go-ublks-kernel-matrix). Delete the device and add a new one.

## Putting it together

A minimal server's control thread, error handling elided:

```c
int ctrl = open("/dev/ublk-control", O_RDWR);
struct io_uring ring;
io_uring_queue_init(32, &ring, IORING_SETUP_SQE128);

struct ublksrv_ctrl_dev_info info = {
    .nr_hw_queues     = 4,
    .queue_depth      = 128,
    .max_io_buf_bytes = 1 << 20,
    .dev_id           = -1,                 /* let the kernel choose */
    .flags            = 0,                  /* plain copy mode */
};
ctrl_cmd(&ring, ctrl, UBLK_U_CMD_ADD_DEV,
         &(struct ublksrv_ctrl_cmd){ .dev_id = -1, .queue_id = -1,
                                     .addr = (__u64)&info, .len = sizeof(info) });
/* info now holds the negotiated queues, depth, buffer size, flags and ID */

struct ublk_params p = { .len = sizeof(p), .types = UBLK_PARAM_TYPE_BASIC,
    .basic = { .logical_bs_shift = 9, .physical_bs_shift = 12,
               .io_min_shift = 9, .io_opt_shift = 12,
               .max_sectors = info.max_io_buf_bytes >> 9,
               .dev_sectors = size_bytes >> 9,
               .attrs = UBLK_ATTR_VOLATILE_CACHE } };
ctrl_cmd(&ring, ctrl, UBLK_U_CMD_SET_PARAMS,
         &(struct ublksrv_ctrl_cmd){ .dev_id = info.dev_id, .queue_id = -1,
                                     .addr = (__u64)&p, .len = sizeof(p) });

start_queue_threads(&info);    /* open /dev/ublkcN, mmap, FETCH_REQ every tag */
ctrl_cmd(&ring, ctrl, UBLK_U_CMD_START_DEV,
         &(struct ublksrv_ctrl_cmd){ .dev_id = info.dev_id, .queue_id = -1,
                                     .data = { getpid() } });

wait_for_shutdown_signal();

ctrl_cmd(&ring, ctrl, UBLK_U_CMD_STOP_DEV, &(struct ublksrv_ctrl_cmd){ .dev_id = info.dev_id, .queue_id = -1 });
join_queue_threads();          /* they exit on -ENODEV; close every fd and mapping */
ctrl_cmd(&ring, ctrl, UBLK_U_CMD_DEL_DEV, &(struct ublksrv_ctrl_cmd){ .dev_id = info.dev_id, .queue_id = -1 });
```

`ctrl_cmd` here prepares one `IORING_OP_URING_CMD` SQE with the given `cmd_op` and header, submits it, and waits for its CQE, returning `cqe->res`. The [data plane](/guide/data-plane/) chapter fills in `start_queue_threads`.
