---
title: "Batch I/O"
linkTitle: "Batch I/O"
description: "UBLK_F_BATCH_IO: per-queue PREP, COMMIT and multishot FETCH commands that replace the per-tag protocol."
weight: 80
---

In the classic [data plane](/guide/data-plane/), each tag holds a FETCH_REQ or COMMIT_AND_FETCH_REQ owned by one task. {{< uapi "UBLK_F_BATCH_IO" >}} (7.0) replaces per-request commands/completions with three per-queue array commands:

| Command | Encoded opcode | Replaces | Shape |
|---|---|---|---|
| {{< uapi "UBLK_U_IO_PREP_IO_CMDS" >}} | `0xc0107525` | the startup `FETCH_REQ` for each tag | one-shot, array of tags |
| {{< uapi "UBLK_U_IO_FETCH_IO_CMDS" >}} | `0xc0107527` | the waiting half of every fetch | multishot, posts arrays of tags |
| {{< uapi "UBLK_U_IO_COMMIT_IO_CMDS" >}} | `0xc0107526` | the commit half of `COMMIT_AND_FETCH_REQ` | one-shot, array of results |

Batch mode reduces commands and CQEs and lets any server task handle any tag, balancing a queue across threads. The protocol below follows the 7.3-rc5 driver.

## Enabling batch mode

{{< since "7.0" >}} Request `UBLK_F_BATCH_IO` at ADD_DEV and check the reply. The kernel clears `UBLK_F_PER_IO_DAEMON` and `UBLK_F_NEED_GET_DATA`: batch mode has neither per-tag daemons nor get-data.

Per-tag `UBLK_U_IO_FETCH_REQ`, `UBLK_U_IO_COMMIT_AND_FETCH_REQ`, and `UBLK_U_IO_NEED_GET_DATA` return `-EOPNOTSUPP`. REGISTER_IO_BUF/UNREGISTER_IO_BUF still work. Batch supports [copy, user copy, zero copy, and automatic registration](/guide/data-copy/).

Descriptor layout and mmap are unchanged; only tag handoff changes.

## The command header

Each command's 16-byte `struct ublk_batch_io` fits the command area of a 64-byte SQE:

```c
struct ublk_batch_io {
	__u16 q_id;        /* queue this command operates on */
	__u16 flags;       /* UBLK_BATCH_F_*; must be 0 for FETCH_IO_CMDS */
	__u16 nr_elem;     /* elements in the buffer at sqe->addr */
	__u8  elem_bytes;  /* size of each element */
	__u8  reserved;
	__u64 reserved2;
};
```

For PREP_IO_CMDS/COMMIT_IO_CMDS, `sqe->addr` addresses `nr_elem * elem_bytes`. Invalid queue/tag returns `-EINVAL`; `nr_elem > queue_depth` returns `-E2BIG`.

## Elements

Elements start with an 8-byte header, followed by flag-selected 8-byte fields in this order:

```c
struct ublk_elem_header {
	__u16 tag;        /* the request's tag */
	__u16 buf_index;  /* io_uring buffer index, used with UBLK_F_AUTO_BUF_REG */
	__s32 result;     /* completion result; COMMIT_IO_CMDS only */
};
/* if UBLK_BATCH_F_HAS_BUF_ADDR: __u64 buf_addr;  server buffer for this tag  */
/* if UBLK_BATCH_F_HAS_ZONE_LBA: __u64 zone_lba;  zone append result, sectors */
```

`elem_bytes` must equal `8 + 8 * HAS_BUF_ADDR + 8 * HAS_ZONE_LBA` (8/16/24), or `-EINVAL`.

| Flag | Value | Meaning | Allowed when |
|---|---|---|---|
| `UBLK_BATCH_F_HAS_ZONE_LBA` | `1 << 0` | Each element carries a `zone_lba` | The device is zoned |
| `UBLK_BATCH_F_HAS_BUF_ADDR` | `1 << 1` | Each element carries a `buf_addr` | The device uses copy mode |
| `UBLK_BATCH_F_AUTO_BUF_REG_FALLBACK` | `1 << 2` | Apply `UBLK_AUTO_BUF_REG_FALLBACK` to every element's registration | The device uses `UBLK_F_AUTO_BUF_REG`; not with `HAS_BUF_ADDR` |

Unknown or inapplicable flags return `-EINVAL`. Copy-mode `buf_addr` names the next request's buffer, like per-tag `ublksrv_io_cmd.addr`; zero is rejected. For automatic registration, `buf_index` selects the next request's buffer-table slot, replacing the index in `sqe->addr`.

## PREP_IO_CMDS: startup

Before START_DEV, prepare every queue/tag with one or more PREP_IO_CMDS. Each element records a copy buffer address or registration index and marks its tag ready, as FETCH_REQ does. Startup waits for all tags.

PREP is atomic: failure reverts previously prepared elements and returns the error; success returns 0. Tags can be prepared only once, so retry the entire failed set.

```c
struct { struct ublk_elem_header h; __u64 buf_addr; } prep[DEPTH];   /* copy mode */
for (int t = 0; t < DEPTH; t++)
	prep[t] = (typeof(prep[0])){ .h.tag = t, .buf_addr = (__u64)(uintptr_t)buf[t] };

struct ublk_batch_io *b = sqe_cmd(sqe);
*b = (struct ublk_batch_io){ .q_id = q, .flags = UBLK_BATCH_F_HAS_BUF_ADDR,
                             .nr_elem = DEPTH, .elem_bytes = sizeof(prep[0]) };
sqe->addr = (__u64)(uintptr_t)prep;
submit(UBLK_U_IO_PREP_IO_CMDS);                     /* res == 0 or -errno */
```

## FETCH_IO_CMDS: receiving requests

FETCH_IO_CMDS is multishot with an io_uring provided-buffer ring. Required fields:

- `IORING_URING_CMD_MULTISHOT` in the SQE's `uring_cmd_flags` (`-EINVAL` otherwise);
- `flags == 0` and `elem_bytes == sizeof(__u16)` in the header;
- the provided-buffer group ID in `sqe->buf_index`.

For queued requests, the kernel fills a provided buffer with `__u16` tags. CQE `res` counts bytes (two per tag), limited by buffer size and 128 tags per CQE. Flags identify the buffer and carry `IORING_CQE_F_MORE` while armed. Descriptors and copy-mode WRITE data are ready before the CQE, as with per-tag fetch.

```c
/* CQE for FETCH_IO_CMDS on queue q */
__u16 *tags = provided_buf(cqe);                  /* from the buffer ID in cqe->flags */
for (int i = 0; i < cqe->res / 2; i++) {
	const struct ublksrv_io_desc *d = &descs[q][tags[i]];
	start_backend_io(q, tags[i], d);              /* complete later via COMMIT_IO_CMDS */
}
recycle_provided_buf(cqe);
if (!(cqe->flags & IORING_CQE_F_MORE))
	rearm_fetch(q);                               /* the command has ended */
```

Fetch rules:

- Multiple tasks/rings may post fetches per queue. One is active, drains the backlog, then hands off to the next; buffer sizes control batch sizes.
- Missing buffers or failed CQE posting requeue undelivered tags and end the command, usually with `-ENOBUFS`, without IORING_CQE_F_MORE. Stock buffers and re-arm when MORE disappears. Stop/quiesce aborts waiting fetches with `UBLK_IO_RES_ABORT` (`-ENODEV`).
- If preparation fails or requeues every selected tag, the CQE returns the buffer with `res == 0`.
- Size the CQ for multishot traffic; the kernel selftest server adds `2 × queue_depth` entries per thread.

## COMMIT_IO_CMDS: completing requests

Commit completed I/O in arrays: tag, `result`, and flag-selected next buffer or zone-append LBA. [Results](/guide/io-operations/) are READ/WRITE byte counts, 0 for other successful operations, or negative errno.

Each element completes its request, copying READ data in copy mode, and returns the tag to FETCH_IO_CMDS. No per-tag re-arm is needed.

The result counts processed element bytes. Processing stops at the first failure:

- `nr_elem * elem_bytes`: all committed;
- smaller positive result: first `ret / elem_bytes` committed; remainder starts at the failed element;
- negative: first element failed; none committed.

An unowned tag (never delivered or double-committed) returns `-EBUSY`; missing copy buffer returns `-EINVAL`. Handle the uncommitted remainder; the kernel will not retry.

```c
int ret = submit_and_wait(UBLK_U_IO_COMMIT_IO_CMDS, q, elems, n, elem_bytes);
int done = ret < 0 ? 0 : ret / elem_bytes;
if (done < n)
	handle_failed_commit(q, &elems[done], n - done, ret < 0 ? ret : -EIO);
```

## Threads and rings

Any server thread can prepare, fetch, or commit any queue's tags. The selftest server uses one ring per thread, posting fetches on served queues and collecting commits per queue/iteration. Thread count need not match queue count.

Automatic registration installs buffers on the delivering fetch's ring; commit must use that ring to unregister them. Otherwise unregister explicitly, or requests never complete. Across threads, an element's chosen index may collide with another tag on the next delivering ring (`-EBUSY`). The selftest server disallows more threads than queues with batch auto-registration; upstream gives no reason.

## go-ublk

`DeviceParams.BatchIO` (7.0+) sends PREP once, maintains one multishot FETCH into 16 buffers of 128 tags, and sends one COMMIT per loop round, with up to four commit buffers in flight. Submission returns tag ownership to the kernel: FETCH may redeliver a tag before its commit CQE arrives.

Copy-mode elements use `UBLK_BATCH_F_HAS_BUF_ADDR`; zero-copy elements use tag as `buf_index` with `UBLK_BATCH_F_AUTO_BUF_REG_FALLBACK`. User copy also works. NeedGetData and ThreadsPerQueue above 1 are forbidden. See [configuration](/go-ublk/configuration/).