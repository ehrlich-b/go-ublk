---
title: "The data plane"
linkTitle: "Data plane"
description: "Queues, tags, the descriptor mmap, and the FETCH_REQ / COMMIT_AND_FETCH_REQ cycle that moves every request through io_uring."
weight: 30
---

The classic per-tag protocol, available since Linux 6.0, delivers descriptors and commits I/O results through io_uring. [Batch I/O](/guide/batch-io/) (7.0) replaces it with per-queue commands; [copy modes](/guide/data-copy/) determine where data lives.

## Queues, tags and threads

`ADD_DEV` fixes `nr_hw_queues` blk-mq hardware queues, each with `queue_depth` request slots called tags. The pair (queue *q*, tag *t*) identifies an in-flight request in descriptors, commands, and user-copy offsets.

Each idle tag has an outstanding command. Request arrival fills its descriptor and completes the command; the server handles I/O, then commits and re-arms that tag. Unarmed tags receive nothing. Each queue has at most `queue_depth` in-flight requests; `START_DEV` waits for every tag to be armed.

Usually each queue has one thread and ring, subject to these rules:

- The task issuing `UBLK_U_IO_FETCH_REQ` owns subsequent commands for that tag; another task gets `EINVAL`. Before 6.16, one task must serve every tag in a queue. From 6.16, the forced `UBLK_F_PER_IO_DAEMON` bit permits splitting tags across threads. Schedulers that migrate work must lock the queue loop to an OS thread.
- `/dev/ublkcN` permits one `open()`. Share or `dup()` that fd across queue threads.

`UBLK_U_CMD_GET_QUEUE_AFFINITY` returns each queue's blk-mq CPU mask. Pinning its thread to those CPUs keeps processing near the issuing CPUs.

## The descriptor array

Map each queue's `queue_depth` descriptors read-only from `/dev/ublkcN`; the kernel writes them.

```c
/* include/uapi/linux/ublk_cmd.h */
struct ublksrv_io_desc {
    __u32 op_flags;        /* op in bits 0-7, UBLK_IO_F_* flags in bits 8-31 */
    union {
        __u32 nr_sectors;  /* length in 512-byte sectors */
        __u32 nr_zones;    /* UBLK_IO_OP_REPORT_ZONES only */
    };
    __u64 start_sector;    /* offset in 512-byte sectors */
    __u64 addr;            /* the server buffer for this request (copy mode) */
};                         /* 24 bytes */
```

Queue *q* uses a fixed offset stride, independent of chosen depth:

```c
size_t page   = sysconf(_SC_PAGESIZE);
size_t stride = round_up(UBLK_MAX_QUEUE_DEPTH * sizeof(struct ublksrv_io_desc), page);
                /* 4096 * 24 = 98304 bytes: 24 pages at 4 KiB, 131072 bytes at 64 KiB */
off_t  off    = UBLKSRV_CMD_BUF_OFFSET + q * stride;          /* UBLKSRV_CMD_BUF_OFFSET is 0 */
size_t len    = round_up(queue_depth * sizeof(struct ublksrv_io_desc), page);

struct ublksrv_io_desc *desc =
    mmap(NULL, len, PROT_READ, MAP_SHARED | MAP_POPULATE, char_fd, off);
```

The kernel derives the queue as `off / stride`. Writable mappings return `EPERM`; wrong length, out-of-range queue offset, or mapping from a process other than the first mapper returns `EINVAL`. Length must equal the page-rounded array size.

> [!CAUTION]
> Using `q * round_up(queue_depth * 24, page)` can put offsets below the fixed stride, mapping other queues onto queue 0. They read wrong descriptors, access wrong offsets, and commit unowned tags: silent corruption and uninterruptible hangs, affecting (N-1)/N of evenly distributed I/O on N queues. A multi-queue integrity sweep found this bug in go-ublk.

With `UBLK_F_IO_DESC_SIZE` (7.3), ADD_DEV's `ublksrv_ctrl_dev_info.io_desc_size` selects 24-256 bytes in multiples of 8. Tag *t* is at `t * io_desc_size`; mapping length is `round_up(queue_depth * io_desc_size, page)`; stride is `round_up(UBLK_MAX_QUEUE_DEPTH * io_desc_size, page)`. Default: 24. Only those first 24 bytes are written in 7.3. Padding, e.g. to a 64-byte cache line, avoids false sharing across threads and reserves future fields. Servers hard-coding 24 must not request this flag.

Read descriptors after reaping the CQE, with acquire loads or an equivalent barrier on weak memory models. They remain valid until commit. Bounds-check fields; a kernel-supplied range beyond capacity should fail the request, not crash the server.

## The I/O commands

Submit `IORING_OP_URING_CMD` to `/dev/ublkcN` on the queue's ring. Its 16-byte `struct ublksrv_io_cmd` starts at SQE byte 48 and fits a normal 64-byte SQE; `IORING_SETUP_SQE128` is unnecessary.

```c
struct ublksrv_io_cmd {
    __u16 q_id;
    __u16 tag;
    __s32 result;              /* COMMIT only: bytes transferred or -errno */
    union {
        __u64 addr;            /* FETCH/COMMIT: buffer for the tag's next request */
        __u64 zone_append_lba; /* COMMIT of a zone append: the LBA written */
    };
};
```

| Command | `cmd_op` | Since | Purpose |
|---|---|---|---|
| `UBLK_U_IO_FETCH_REQ` | `0xc0107520` | 6.0 (encoded 6.4) | Arm a tag the first time. Registers the issuing task as the tag's daemon |
| `UBLK_U_IO_COMMIT_AND_FETCH_REQ` | `0xc0107521` | 6.0 (encoded 6.4) | Complete the tag's current request with `result` and re-arm the tag |
| `UBLK_U_IO_NEED_GET_DATA` | `0xc0107522` | 6.0 (encoded 6.4) | Supply a buffer for a write delivered without data (`UBLK_F_NEED_GET_DATA`) |
| `UBLK_U_IO_REGISTER_IO_BUF` | `0xc0107523` | 6.15 | Register the request's pages in an io_uring buffer table (zero copy) |
| `UBLK_U_IO_UNREGISTER_IO_BUF` | `0xc0107524` | 6.15 | Unregister them |
| `UBLK_U_IO_PREP_IO_CMDS`, `COMMIT_IO_CMDS`, `FETCH_IO_CMDS` | `0xc0107525`-`27` | 7.0 | [Batch I/O](/guide/batch-io/), which replaces the first three |

Legacy opcodes 0x20/0x21/0x22 require `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES`, as control commands do.

Encode the tag in SQE `user_data`. Rings serving multiple queues or backend I/O also need queue and operation type to route completions.

In copy mode, `addr` names at least `max_io_buf_bytes` of server memory: the kernel copies WRITEs in before delivery and READs out at commit. Each commit may change the buffer. User-copy/zero-copy modes require 0, except zone append carries its LBA.

## The FETCH / COMMIT cycle

{{< diagram "tag-states" "The life of one tag. The kernel owns the tag while a command is pending; the server owns it between a completion and its commit." >}}

A pending command completes with:

| `cqe->res` | Name | Meaning | Server action |
|---|---|---|---|
| 0 | `UBLK_IO_RES_OK` | A request is waiting in the tag's descriptor | Handle it, then commit |
| 1 | `UBLK_IO_RES_NEED_GET_DATA` | A write arrived without its data (`UBLK_F_NEED_GET_DATA` only) | Send `NEED_GET_DATA` with a buffer; the next completion is `UBLK_IO_RES_OK` with the data copied |
| -19 | `UBLK_IO_RES_ABORT` (`-ENODEV`) | The tag is retired: the device is stopping, being quiesced, or the ring is going away | Do not submit anything more for this tag |

Other negatives reject the command: `EINVAL` for bad queue/tag, wrong task, or wrong copy-mode address; `EBUSY` for an unowned tag, e.g. double commit. These indicate server/kernel state disagreement. Stop serving that queue and tear down the device.

Tags can complete in any order. Overlap backend I/O, ideally on the same ring, and commit each tag when it finishes. Synchronous processing limits a queue to one active backend request regardless of depth.

### A queue thread

```c
void queue_thread(int q, int char_fd, struct ublksrv_ctrl_dev_info *info) {
    struct io_uring ring;
    io_uring_queue_init(2 * info->queue_depth, &ring,
                        IORING_SETUP_SINGLE_ISSUER | IORING_SETUP_DEFER_TASKRUN |
                        IORING_SETUP_COOP_TASKRUN);
    struct ublksrv_io_desc *desc = map_descriptors(char_fd, q, info->queue_depth);
    void **buf = alloc_page_aligned_buffers(info->queue_depth, info->max_io_buf_bytes);

    for (int t = 0; t < info->queue_depth; t++)
        prep_io_cmd(&ring, char_fd, UBLK_U_IO_FETCH_REQ, q, t, 0, buf[t]);
    io_uring_submit(&ring);

    int live = info->queue_depth;
    while (live > 0) {
        io_uring_submit_and_wait(&ring, 1);           /* flushes pending commits too */
        struct io_uring_cqe *cqe; unsigned head, n = 0;
        io_uring_for_each_cqe(&ring, head, cqe) {
            int t = (int)cqe->user_data;
            n++;
            if (cqe->res == UBLK_IO_RES_ABORT) { live--; continue; }
            if (cqe->res < 0) { report_bug(t, cqe->res); live--; continue; }
            const struct ublksrv_io_desc *d = &desc[t];  /* valid until we commit */
            int res = handle(d->op_flags & 0xff, d->op_flags >> 8,
                             d->start_sector << 9, (size_t)d->nr_sectors << 9, buf[t]);
            prep_io_cmd(&ring, char_fd, UBLK_U_IO_COMMIT_AND_FETCH_REQ, q, t, res, buf[t]);
        }
        io_uring_cq_advance(&ring, n);
    }
    /* every tag aborted: the device was stopped. Unmap, close the ring. */
}
```

The loop reaps available completions and submits their commits in one `io_uring_enter`, batching system-call cost under load.

## Units: always 512-byte sectors

`start_sector`, `nr_sectors`, `dev_sectors`, and `max_sectors` always use 512-byte sectors. A 4Kn device (4096-byte logical blocks) receives offsets/lengths in multiples of eight sectors. Multiplying by logical block size instead misplaces I/O by 8x and understates capacity by 8x; a 4Kn test caught this go-ublk bug.

## Results

Commit results:

- READ/WRITE: full `nr_sectors << 9` bytes or a negative errno. A zero-byte READ becomes `-EIO`. In 6.17, short reads/writes requeued the remainder. In 7.3-rc5, only copy-mode reads complete partially; short writes and non-negative user-copy/zero-copy results report full success. Fail incomplete transfers.
- FLUSH/DISCARD/WRITE_ZEROES: any non-negative value succeeds. Use 0: range lengths overflow signed 32-bit results at 2 GiB and become errors.
- Errors: negative errnos map through block status to application errors, including EIO, ENOSPC, and EOPNOTSUPP.

[I/O operations and flags](/guide/io-operations/) covers each operation and the durability rules.

## Shutdown and failure

Stop completes armed tags with `UBLK_IO_RES_ABORT`. Commit server-owned requests first, since STOP_DEV waits for them; exit the queue thread after all tags abort.

Process death tears down rings and cancels commands. On the last `/dev/ublkcN` reference, the driver either stops the disk and fails uncommitted requests, or retains it for [recovery](/guide/recovery/), failing, requeueing, or holding I/O according to flags. Both leave the device registered until `DEL_DEV`.

Privileged-device timeouts reset indefinitely: an uncommitted request leaves its submitter in uninterruptible sleep until commit or server exit. Unprivileged-device timeouts instead send `SIGKILL`, preventing an untrusted server from hanging the host.