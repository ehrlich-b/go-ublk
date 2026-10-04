---
title: "Batch I/O"
linkTitle: "Batch I/O"
description: "UBLK_F_BATCH_IO: per-queue PREP, COMMIT and multishot FETCH commands that replace the per-tag protocol."
weight: 80
---

The classic [data plane](/guide/data-plane/) spends one io_uring command per request in each direction: a `FETCH_REQ` or `COMMIT_AND_FETCH_REQ` sits armed on every tag, and the task that armed a tag is the only one allowed to touch it. {{< uapi "UBLK_F_BATCH_IO" >}}, added in Linux 7.0, replaces that with three per-queue commands that carry arrays:

| Command | Encoded opcode | Replaces | Shape |
|---|---|---|---|
| {{< uapi "UBLK_U_IO_PREP_IO_CMDS" >}} | `0xc0107525` | the startup `FETCH_REQ` for each tag | one-shot, array of tags |
| {{< uapi "UBLK_U_IO_FETCH_IO_CMDS" >}} | `0xc0107527` | the waiting half of every fetch | multishot, posts arrays of tags |
| {{< uapi "UBLK_U_IO_COMMIT_IO_CMDS" >}} | `0xc0107526` | the commit half of `COMMIT_AND_FETCH_REQ` | one-shot, array of results |

The gains are fewer commands and CQEs per request, and freedom from per-tag task affinity: in batch mode any task of the server may handle any tag, so several threads can share a queue and balance load between them. Everything described on this page was checked against the 7.3-rc5 driver source.

## Enabling batch mode

{{< since "7.0" >}} Request `UBLK_F_BATCH_IO` at `ADD_DEV` and check that it survives the round trip. The kernel then clears `UBLK_F_PER_IO_DAEMON` (there are no per-I/O daemons in batch mode) and `UBLK_F_NEED_GET_DATA` (batch mode has no get-data step).

On a batch device the per-tag commands are gone: `UBLK_U_IO_FETCH_REQ`, `UBLK_U_IO_COMMIT_AND_FETCH_REQ` and `UBLK_U_IO_NEED_GET_DATA` fail with `-EOPNOTSUPP`. `UBLK_U_IO_REGISTER_IO_BUF` and `UBLK_U_IO_UNREGISTER_IO_BUF` keep working, so fixed-buffer zero copy is still available. Batch mode combines with copy mode, user copy, zero copy and automatic buffer registration; see [Data copy modes](/guide/data-copy/).

The descriptor array, its mmap, and the meaning of every descriptor field are unchanged. Only the way tags are handed back and forth differs.

## The command header

All three commands put a 16-byte `struct ublk_batch_io` in the SQE's command area, so they fit an ordinary 64-byte SQE:

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

For `PREP_IO_CMDS` and `COMMIT_IO_CMDS`, `sqe->addr` points at an element buffer of `nr_elem * elem_bytes` bytes. The kernel checks that `q_id` is a valid queue (`-EINVAL`), that `nr_elem` does not exceed `queue_depth` (`-E2BIG`), and that every element's tag is below `queue_depth` (`-EINVAL`).

## Elements

Each element starts with an 8-byte header, followed by optional 8-byte fields selected by `flags`, in this order:

```c
struct ublk_elem_header {
	__u16 tag;        /* the request's tag */
	__u16 buf_index;  /* io_uring buffer index, used with UBLK_F_AUTO_BUF_REG */
	__s32 result;     /* completion result; COMMIT_IO_CMDS only */
};
/* if UBLK_BATCH_F_HAS_BUF_ADDR: __u64 buf_addr;  server buffer for this tag  */
/* if UBLK_BATCH_F_HAS_ZONE_LBA: __u64 zone_lba;  zone append result, sectors */
```

`elem_bytes` must equal exactly `8 + 8 * HAS_BUF_ADDR + 8 * HAS_ZONE_LBA`, so it is 8, 16 or 24; anything else is `-EINVAL`. The flags:

| Flag | Value | Meaning | Allowed when |
|---|---|---|---|
| `UBLK_BATCH_F_HAS_ZONE_LBA` | `1 << 0` | Each element carries a `zone_lba` | The device is zoned |
| `UBLK_BATCH_F_HAS_BUF_ADDR` | `1 << 1` | Each element carries a `buf_addr` | The device uses copy mode |
| `UBLK_BATCH_F_AUTO_BUF_REG_FALLBACK` | `1 << 2` | Apply `UBLK_AUTO_BUF_REG_FALLBACK` to every element's registration | The device uses `UBLK_F_AUTO_BUF_REG`; not with `HAS_BUF_ADDR` |

Unknown flag bits, or a flag used on a device that does not qualify, fail the command with `-EINVAL`. In copy mode `buf_addr` plays the role `ublksrv_io_cmd.addr` plays in the per-tag protocol: it names the buffer the tag's *next* request will use, and 0 is rejected. With automatic buffer registration, `buf_index` plays the role of the index packed into `sqe->addr`: it is the buffer table slot for the tag's next request.

## PREP_IO_CMDS: startup

Before `START_DEV`, the server prepares every tag of every queue with one or more `PREP_IO_CMDS`. Each element prepares one tag, exactly as a `FETCH_REQ` would: it records the tag's buffer address (copy mode) or registration index (automatic registration) and marks the tag ready. When every tag on every queue is ready, `START_DEV` can complete.

The command is all-or-nothing. If any element fails, the elements already prepared by this command are reverted and the error is returned; success returns 0. Because each tag can be prepared only once, a server that retries after a failure must prepare the same set again.

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

`FETCH_IO_CMDS` is a multishot command fed by an io_uring provided-buffer ring. The kernel requires:

- `IORING_URING_CMD_MULTISHOT` in the SQE's `uring_cmd_flags` (`-EINVAL` otherwise);
- `flags == 0` and `elem_bytes == sizeof(__u16)` in the header;
- the provided-buffer group ID in `sqe->buf_index`.

Whenever requests are waiting on the queue, the kernel takes one buffer from the group, fills it with an array of `__u16` tags, and posts a CQE whose `res` is the number of bytes written: two per tag, at most 128 tags per CQE, and never more than fit in the buffer. The CQE carries the buffer ID in its flags as usual for provided buffers, and `IORING_CQE_F_MORE` while the command stays armed. Before posting, the kernel has already written each tag's descriptor in the mmap and, in copy mode, copied WRITE data into the tag's buffer, so the server handles each tag exactly as after a classic fetch completion.

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

Points to get right:

- **Several fetch commands per queue are allowed**, from different tasks and different rings. Only one is active at a time: it drains the queue's backlog, then hands over to the next waiting fetch command. This is the load-balancing mechanism. A task that posts a fetch command with a small buffer takes small batches; one with a large buffer takes large batches.
- **The command can end.** If no buffer is available in the group, or the CQE cannot be posted, the kernel puts the undelivered tags back, completes the fetch command with the error (typically `-ENOBUFS`) and no `IORING_CQE_F_MORE`. Keep the buffer ring stocked and re-arm when the flag is missing. On device stop or quiesce, waiting fetch commands complete with `UBLK_IO_RES_ABORT` (`-ENODEV`).
- **A CQE can carry zero tags.** If every tag selected for a batch had to be requeued or failed during preparation, the kernel returns the buffer with `res == 0`.
- **Size the completion queue for it.** Multishot commands post many CQEs; the kernel's selftest server adds `2 × queue_depth` CQ entries per thread in batch mode.

## COMMIT_IO_CMDS: completing requests

When backend I/O finishes, the server reports results in bulk. Each element names a tag, its `result` (a byte count for READ and WRITE, 0 for other successful operations, or a negative errno, exactly as in the [per-tag protocol](/guide/io-operations/)), and, depending on `flags`, the buffer for the tag's next request or the LBA a zone append landed at.

For each element the kernel completes the block request (in copy mode, copying READ data out of the server's buffer) and returns the tag to the pool that `FETCH_IO_CMDS` draws from. There is nothing to re-arm per tag.

The result is the number of element **bytes** processed. Elements are handled in order and processing stops at the first failure, so:

- a return equal to `nr_elem * elem_bytes` means everything was committed;
- a smaller positive return means the first `ret / elem_bytes` elements were committed and the rest were not, starting with the one that failed;
- a negative return means the very first element failed and nothing was committed.

Per-element failures include `-EBUSY` for a tag the server does not currently own (a double commit, or a tag never delivered) and `-EINVAL` for a missing buffer address in copy mode. The server must deal with the uncommitted remainder itself; the kernel will not retry it.

```c
int ret = submit_and_wait(UBLK_U_IO_COMMIT_IO_CMDS, q, elems, n, elem_bytes);
int done = ret < 0 ? 0 : ret / elem_bytes;
if (done < n)
	handle_failed_commit(q, &elems[done], n - done, ret < 0 ? ret : -EIO);
```

## Threads and rings

In batch mode the kernel does not record a daemon task per tag, so any thread of the server may prepare, fetch or commit any tag of any queue. A typical design runs a few threads, each with its own io_uring, each posting a `FETCH_IO_CMDS` on the queues it serves and accumulating one `COMMIT_IO_CMDS` per queue per loop iteration. The kernel's selftest server works this way, and allows the thread count to differ from the queue count.

Automatic buffer registration needs more care. The kernel registers a request's buffer into the ring that issued the fetch command which delivered it, and unregisters it only when the commit arrives on that same ring. A commit from a different ring leaves the buffer registered, and the request never completes unless the server unregisters it by hand. The selftest server therefore refuses to run batch mode with automatic registration and more threads than queues. <!-- VERIFY: that the selftest's "threads <= queues" restriction for AUTO_BUF_REG + BATCH_IO exists for this same-ring reason -->

## go-ublk

`DeviceParams.BatchIO` (kernel 7.0+) serves the device with batch I/O. Each I/O thread sends `PREP_IO_CMDS` once for its tags, keeps one multishot `FETCH_IO_CMDS` armed into a provided-buffer ring of 16 tag buffers (128 tags each, the per-CQE maximum), and sends one `COMMIT_IO_CMDS` per loop round for everything completed since the last, with up to four commit buffers in flight. Tags are treated as returned to the kernel when their commit is submitted, because the multishot fetch can deliver a tag again before the commit's completion arrives.

It works with copy mode (elements carry `UBLK_BATCH_F_HAS_BUF_ADDR`), user copy, and zero copy (elements carry the tag as `buf_index` with `UBLK_BATCH_F_AUTO_BUF_REG_FALLBACK`). It cannot be combined with `NeedGetData`, which the kernel clears for batch devices, or with `ThreadsPerQueue` above 1. See the [configuration reference](/go-ublk/configuration/).
