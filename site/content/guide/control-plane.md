---
title: "The control plane"
linkTitle: "Control plane"
description: "Every ublk control command on /dev/ublk-control: encoding, payload, result, errors, and the device state machine they drive."
weight: 20
---

`/dev/ublk-control` handles device creation, inspection, startup, shutdown, and deletion, plus resizing, quiescing, and recovery on newer kernels.

## Issuing a control command

Open `/dev/ublk-control` read-write and create an io_uring with `IORING_SETUP_SQE128`: the driver requires 128-byte SQEs for control commands. Fill each passthrough SQE as follows:

| SQE field | Value |
|---|---|
| `opcode` | `IORING_OP_URING_CMD` (46) |
| `fd` | the `/dev/ublk-control` file descriptor (or a registered file index with `IOSQE_FIXED_FILE`) |
| `cmd_op` | the encoded command, for example `UBLK_U_CMD_ADD_DEV` |
| `cmd[]` | a `struct ublksrv_ctrl_cmd`, at byte 48 of the SQE |
| `user_data` | anything; it comes back in the CQE |

CQE `res` is 0 on success or a negative errno, except `UBLK_U_CMD_REG_BUF` can return a positive buffer index.

`START_DEV` and `END_USER_RECOVERY` wait for every tag's fetch; `DEL_DEV` waits for the last device reference; `QUIESCE_DEV` waits up to its timeout. The driver returns `-EAGAIN` on non-blocking issue, moving these commands to io_uring workers. Submission returns, but the CQE waits for the condition. Use timeouts and handle `EINTR`-style interruptions. Never wait on the thread needed to satisfy the condition: `START_DEV` cannot finish before that thread submits the fetches.

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

The struct is 32 bytes. Keep `addr` buffers valid and unmoved until the CQE, including in garbage-collected languages. Pin them; if abandoning a wait, leak rather than free memory the kernel may still access.

### Legacy and ioctl-encoded opcodes

Linux 6.0-6.3 used small command integers (`UBLK_CMD_ADD_DEV = 0x04`). Linux 6.4 introduced `UBLK_U_CMD_*`, encoded with `_IOR`/`_IOWR('u', nr, struct ublksrv_ctrl_cmd)`; later commands exist only in this form. Both work unless `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES` is disabled, when legacy commands return `EOPNOTSUPP`. Use encoded commands; pre-6.4 support requires legacy numbers too.

Since 6.4, negotiated `UBLK_F_CMD_IOCTL_ENCODE` confirms encoded-command support.

## Commands

Commands in the 7.3-rc5 header; "Blocks" identifies what delays the CQE.

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

Legacy commands use the low byte: 0x01/0x02 for affinity/info, 0x04-0x09 for ADD/DEL/START/STOP/SET/GET_PARAMS, 0x10/0x11 for recovery, and 0x12 for `GET_DEV_INFO2`.

### ADD_DEV

Creates `/dev/ublkcN`. Pass a 64-byte `struct ublksrv_ctrl_dev_info` through `addr`; the kernel overwrites it with negotiated values:

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

Read back every field before allocating queues or buffers. The kernel clamps queues to CPU count, rounds down buffer size, clears unknown flags, and clears `NEED_GET_DATA` in user-copy/zero-copy modes. In 6.17 it forces `CMD_IOCTL_ENCODE`, `URING_CMD_COMP_IN_TASK`, `PER_IO_DAEMON`, and `BUF_REG_OFF_DAEMON`; 7.3-rc5 also sets `SAFE_STOP_DEV` and clears `PER_IO_DAEMON` for batch devices. These returned flags govern the device's lifetime. Using requested sizes instead can map nonexistent descriptor arrays or overrun buffers. See [negotiation order](/guide/features/).

`ADD_DEV` fails with:

- `EINVAL`: depth/queues outside 1-4096, header `queue_id` other than -1, mismatched header/info `dev_id`, ID above the maximum, invalid [recovery flags](/guide/recovery/), `QUIESCE` without `USER_RECOVERY`, `ZONED` without user/zero copy, `INTEGRITY` without `USER_COPY`, invalid `io_desc_size` with `IO_DESC_SIZE`, or a forbidden unprivileged copy mode.
- `EPERM`: no `CAP_SYS_ADMIN` and no `UNPRIVILEGED_DEV`. Privileged callers have that flag cleared and receive ordinary devices.
- `EEXIST`: requested ID occupied (automatic assignment picks the lowest free ID). `EACCES`: unprivileged-device limit reached. Also `ENOMEM`.

### SET_PARAMS and GET_PARAMS

`SET_PARAMS` supplies capacity, block sizes, maximum request size, read-only/cache attributes, discard/zoned limits, and other [device parameters](/guide/parameters/). Its `addr` points to `struct ublk_params`, beginning with `len` (bytes supplied) and `types` (`UBLK_PARAM_TYPE_*` bits identifying valid substructures). The `basic` block is mandatory.

Set parameters repeatedly before the first `START_DEV`. Once the disk exists, `SET_PARAMS` returns `EACCES` until it is removed. Validation failure returns `EINVAL` and clears all parameters, also preventing `START_DEV`.

The kernel silently limits copying to its `sizeof(struct ublk_params)` and masks unknown `types`. For example, 6.14 accepts `UBLK_PARAM_TYPE_SEGMENT` without enforcing segment limits. Verify required parameters with `GET_PARAMS`: initialize `len`; the kernel copies `min(len, sizeof)` bytes and fills the read-only `devt` block.

### START_DEV

Exposes `/dev/ublkbN`. `data[0]` must be the PID (thread-group ID) that opened `/dev/ublkcN`; a mismatch or missing basic parameters returns `EINVAL`.

`START_DEV` waits interruptibly for every tag of every queue to have an outstanding `FETCH_REQ`. It then allocates the disk, applies parameters/attributes, and calls `add_disk`, emitting udev events. Before 6.19 (stable 6.18 before 6.18.4), partition scanning runs synchronously here: serve reads of the first sectors while startup is pending. From 6.19, a work item scans asynchronously. In either case, queue threads must fetch and serve before startup completes. Starting a live device returns `EEXIST`.

Partition scanning is suppressed for queues served by unprivileged tasks, or with `UBLK_F_NO_AUTO_PART_SCAN` (7.0+).

### STOP_DEV

Removes `/dev/ublkbN` and returns to `UBLK_S_DEV_DEAD`. `del_gendisk` flushes mounted-filesystem data and drains in-flight I/O, so **keep serving queues until STOP_DEV returns**. The driver then completes pending fetches with `UBLK_IO_RES_ABORT` (`-ENODEV`), telling queue threads to exit. The command returns 0.

Shutdown order:

```text
STOP_DEV            (queues still serving; returns after the drain)
stop queue threads  (their fetches complete with -ENODEV)
close io_urings, munmap descriptors, close /dev/ublkcN
DEL_DEV
```

### TRY_STOP_DEV

{{< since "7.0" >}} Stops only with no `/dev/ublkbN` openers; otherwise returns `EBUSY`. No disk returns `ENODEV`. On success it blocks new opens and runs `STOP_DEV`, allowing "detach if idle" without disrupting a mounted filesystem. `UBLK_F_SAFE_STOP_DEV` advertises it; 7.3-rc5 reports the flag on every device.

### DEL_DEV and DEL_DEV_ASYNC

`DEL_DEV` stops a live device, removes `/dev/ublkcN`, and waits for its last reference before freeing the ID for reuse. References include char-device fds, duplicates, descriptor mappings, and io_uring fixed-file registrations. Keeping any of these while waiting deadlocks the caller. The wait is interruptible: a signal returns `EINTR`.

`DEL_DEV_ASYNC` (6.9) removes without waiting; the ID becomes reusable after the last reference drops.

A dead server leaves the device registered as `DEAD`, or `QUIESCED`/`FAIL_IO` with recovery, pinning the module. Any `CAP_SYS_ADMIN` process can delete it by ID.

### GET_DEV_INFO and GET_DEV_INFO2

Copy `ublksrv_ctrl_dev_info` to `addr`, with `len >= 64`. Missing IDs return `ENODEV`; there is no list command, so enumeration probes device IDs.

`GET_DEV_INFO2` (6.3) prepends the char-device path for the [unprivileged permission check](/guide/unprivileged/). It requires the path even for privileged devices: callers may not yet know the device type.

### GET_QUEUE_AFFINITY

Returns blk-mq's CPU mask for queue `data[0]` through `addr`. `len` must cover every possible CPU ID and be a multiple of `sizeof(unsigned long)`. Servers can pin queue threads to the CPUs routing I/O to that queue.

### GET_FEATURES

{{< since "6.5" >}} Writes the kernel's 64-bit `UBLK_F_*` mask to `addr`; `len` must equal `UBLK_FEATURES_LEN` (8). It needs no device. Before 6.5, probe by requesting flags through `ADD_DEV` and inspecting the reply.

### UPDATE_SIZE

{{< since "6.16" >}} Resizes a started device to `data[0]` 512-byte sectors and emits a resize uevent. `UBLK_F_UPDATE_SIZE` advertises it, but the driver does not check the flag. Before startup, 7.3-rc5 returns `ENODEV`; 6.17 lacks that check. Prepare the added range before growing; stop relying on removed space only after shrinking.

### QUIESCE_DEV, START_USER_RECOVERY, END_USER_RECOVERY

These commands replace a departed or departing server while retaining `/dev/ublkbN`. `QUIESCE_DEV` (6.16, `UBLK_F_QUIESCE`) parks a live device with a timeout in `data[0]` milliseconds; `START_USER_RECOVERY`/`END_USER_RECOVERY` (6.1, `UBLK_F_USER_RECOVERY`) bracket takeover. See [recovery states and sequences](/guide/recovery/).

### REG_BUF and UNREG_BUF

{{< since "7.1" >}} Register/unregister shared memory for `UBLK_F_SHMEM_ZC`. `REG_BUF` reads `struct ublk_shmem_buf_reg` (address, length, flags) through `addr` and returns an index; `UNREG_BUF` takes it in `data[0]`. See [data copy modes](/guide/data-copy/).

## Device states

The `state` field of `ublksrv_ctrl_dev_info` takes four values:

| State | Value | Meaning |
|---|---|---|
| `UBLK_S_DEV_DEAD` | 0 | No disk. A device that was added but not started, or was stopped |
| `UBLK_S_DEV_LIVE` | 1 | `/dev/ublkbN` exists and a server is serving it |
| `UBLK_S_DEV_QUIESCED` | 2 | Recovery enabled, no server; new I/O waits (6.1+) |
| `UBLK_S_DEV_FAIL_IO` | 3 | Recovery enabled with fail-fast, no server; new I/O fails (6.13+) |

{{< diagram "device-states" "`DEL_DEV` is accepted from any state. Without a recovery flag, a server exit stops the device; with one, the device waits for a new server." >}}

Delete stopped devices and add new ones. `STOP_DEV` leaves queues counted as fully fetched: new fetches fail with `-EBUSY`, and another process cannot map descriptors. Forcing `START_DEV` can expose a disk with no armed fetches. The matrix observed `-EBUSY`, control-plane hangs (6.10-6.12), and a NULL dereference in `ublk_queue_rq` (Arch 7.2.8). See [kernel findings](/guide/kernel-bugs/#found-by-go-ublks-kernel-matrix).

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

`ctrl_cmd` fills an `IORING_OP_URING_CMD` SQE with `cmd_op` and the header, submits it, and waits for `cqe->res`. The [data plane](/guide/data-plane/) implements `start_queue_threads`.