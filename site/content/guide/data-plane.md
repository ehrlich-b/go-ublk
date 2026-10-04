---
title: "The data plane"
linkTitle: "Data plane"
description: "Queues, tags, the descriptor mmap, and the FETCH_REQ / COMMIT_AND_FETCH_REQ cycle that moves every request through io_uring."
weight: 30
---

The data plane is where a server spends its life: waiting for requests, reading their descriptors, doing the I/O, and committing results. It is a small protocol with sharp edges. This chapter describes the classic per-tag protocol that every kernel since 6.0 speaks; [batch I/O](/guide/batch-io/) (Linux 7.0) replaces it with per-queue commands, and the [copy modes](/guide/data-copy/) change where request data lives.

## Queues, tags and threads

`ADD_DEV` fixes two numbers: `nr_hw_queues` and `queue_depth`. The block device gets that many blk-mq hardware queues, each with that many **tags**. A tag is a request slot: a request in flight on queue *q* occupies one tag *t*, and the pair (*q*, *t*) identifies it everywhere in the protocol, in the descriptor array, in the I/O commands, and in user-copy offsets.

For every tag the server keeps one io_uring command outstanding while it is idle. When a request arrives on (*q*, *t*), the kernel describes it in a descriptor and completes that command. The server handles the request and submits the next command for the same tag, which both reports the result and re-arms the slot. A tag the server has not re-armed cannot receive a request, so at most `queue_depth` requests per queue are in flight, and `START_DEV` will not create the disk until every tag of every queue is armed.

The usual shape is one thread and one io_uring per queue. Two rules constrain it:

- **The daemon rule.** The task that issues `UBLK_U_IO_FETCH_REQ` for a tag becomes that tag's daemon, and only it may issue later commands for that tag; anything else gets `EINVAL`. Before Linux 6.16 the binding is per queue: all tags of a queue must be served by one task. From 6.16 (`UBLK_F_PER_IO_DAEMON`, which the kernel reports automatically) each tag can have its own daemon, so one queue's tags can be spread over several threads. In a language with a scheduler that migrates work between OS threads, the queue loop must be locked to one OS thread.
- **One open of `/dev/ublkcN`.** The char device admits a single `open()`. Open it once and share or `dup()` the descriptor among queue threads.

`UBLK_U_CMD_GET_QUEUE_AFFINITY` tells you which CPUs blk-mq maps to each queue. Pinning queue *q*'s thread to those CPUs keeps a request on the CPU that issued it.

## The descriptor array

Each queue has an array of `queue_depth` descriptors that the kernel writes and the server reads through a read-only shared mapping of `/dev/ublkcN`.

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

Queue *q*'s array lives at a **fixed stride** in the char device's offset space, independent of the queue depth you chose:

```c
size_t page   = sysconf(_SC_PAGESIZE);
size_t stride = round_up(UBLK_MAX_QUEUE_DEPTH * sizeof(struct ublksrv_io_desc), page);
                /* 4096 * 24 = 98304 bytes: 24 pages at 4 KiB, 131072 bytes at 64 KiB */
off_t  off    = UBLKSRV_CMD_BUF_OFFSET + q * stride;          /* UBLKSRV_CMD_BUF_OFFSET is 0 */
size_t len    = round_up(queue_depth * sizeof(struct ublksrv_io_desc), page);

struct ublksrv_io_desc *desc =
    mmap(NULL, len, PROT_READ, MAP_SHARED | MAP_POPULATE, char_fd, off);
```

The kernel derives the queue number from the offset as `off / stride` and requires the length to be exactly the queue's rounded array size. It rejects a writable mapping with `EPERM`, an offset past the last queue or a wrong length with `EINVAL`, and a mapping from a second process with `EINVAL` (all mappings must come from the process that mapped first).

> [!CAUTION]
> Computing the offset as `q * round_up(queue_depth * 24, page)` is the classic bug. With any realistic depth that offset is smaller than the fixed stride, the kernel floors it to queue 0, and every queue maps queue 0's descriptors. Queue 0 works; every other queue reads the wrong descriptors, does I/O at the wrong offsets, and commits tags the kernel did not hand it. The symptoms are silent data corruption and requests stuck forever in uninterruptible sleep, at a rate of (N-1)/N of the I/O on an N-queue device. go-ublk shipped with exactly this bug until it was found by a multi-queue integrity sweep.

With `UBLK_F_IO_DESC_SIZE` (Linux 7.3) the server chooses the descriptor size at `ADD_DEV` through the `io_desc_size` field of `ublksrv_ctrl_dev_info` (24 to 256 bytes, a multiple of 8), and everything above scales with it: descriptor *t* is at `t * io_desc_size`, the per-queue length is `round_up(queue_depth * io_desc_size, page)`, and the stride is `round_up(UBLK_MAX_QUEUE_DEPTH * io_desc_size, page)`. Without the flag the kernel reports 24. The first 24 bytes keep the layout above and the rest is padding: the flag lets a server pad descriptors (for example to a 64-byte cache line) to avoid false sharing when several threads serve one queue, and leaves room for future descriptor fields; nothing in 7.3 writes past byte 24. A server that hard-codes 24 must not request the flag.

A descriptor is valid from the moment its tag's command completes until the server commits the tag. The CQE is the synchronization point: read the descriptor after reaping the completion, not before, and in languages with a weak memory model use acquire loads or an equivalent barrier for the read. Treat the fields as untrusted input for bounds checks (a request beyond your capacity is a kernel bug, but a server should fail it, not crash).

## The I/O commands

I/O commands are `IORING_OP_URING_CMD` submissions on a queue's io_uring, targeting the `/dev/ublkcN` descriptor, with a 16-byte `struct ublksrv_io_cmd` at byte 48 of the SQE. Sixteen bytes fit in a normal 64-byte SQE, so data-plane rings do not need `IORING_SETUP_SQE128`.

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

The legacy opcodes 0x20, 0x21 and 0x22 still work where `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES` is enabled, as with control commands.

The `user_data` of the SQE is yours. Encode at least the tag, and if one ring serves several queues or carries backend I/O too, enough to route the completion (queue, tag, and whether it is a ublk command or a backend operation).

In the default copy mode, `addr` must be the address of a buffer of at least `max_io_buf_bytes` in the server's address space; the kernel copies write data into it before delivering the request and reads read data out of it at commit. You may pass a different buffer on every commit. In user-copy and zero-copy modes `addr` must be 0 (except for the zone-append LBA).

## The FETCH / COMMIT cycle

{{< diagram "tag-states" "The life of one tag. The kernel owns the tag while a command is pending; the server owns it between a completion and its commit." >}}

The completion of a tag's pending command carries one of three results:

| `cqe->res` | Name | Meaning | Server action |
|---|---|---|---|
| 0 | `UBLK_IO_RES_OK` | A request is waiting in the tag's descriptor | Handle it, then commit |
| 1 | `UBLK_IO_RES_NEED_GET_DATA` | A write arrived without its data (`UBLK_F_NEED_GET_DATA` only) | Send `NEED_GET_DATA` with a buffer; the next completion is `UBLK_IO_RES_OK` with the data copied |
| -19 | `UBLK_IO_RES_ABORT` (`-ENODEV`) | The tag is retired: the device is stopping, being quiesced, or the ring is going away | Do not submit anything more for this tag |

Any other negative value means the command itself was refused: `EINVAL` for a bad queue or tag, a command from the wrong task, or a buffer address that does not match the copy mode; `EBUSY` for a command on a tag the server does not currently own, such as a second commit. Those are server bugs. The tag's state on the server side no longer matches the kernel's, so the safest response is to stop serving the queue and tear the device down.

Tags are independent. A server may handle requests in any order, hold several at once, and commit each whenever its backend work finishes; nothing requires commits in fetch order. That is how a single queue thread overlaps backend latency: submit the backend I/O (on the same io_uring, ideally), and commit the tag from that operation's completion. A server that handles requests one at a time, synchronously, gets at most one request in flight per queue no matter how deep the queue is.

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

The loop reaps every available completion, prepares a commit for each, and submits them all in one `io_uring_enter`. Under load that is one system call for many requests in each direction.

## Units: always 512-byte sectors

`start_sector` and `nr_sectors` count 512-byte sectors, and so do `dev_sectors` and `max_sectors` in the parameters, **whatever the device's logical block size**. A 4Kn device (logical block size 4096) receives requests whose offsets and lengths are multiples of 8 sectors, still expressed in 512-byte units. Multiplying by the logical block size instead puts every I/O at eight times its intended offset and reports a capacity eight times too small; go-ublk had that bug too, until a 4Kn test caught it.

## Results

What to put in `result` when committing:

- **Read and write:** the full length, `nr_sectors << 9`. A read that commits 0 is turned into `-EIO`. Do not commit a short positive count: its meaning has changed between releases. In 6.17 a short read or write completes that many bytes and the block layer re-issues the rest; in 7.3-rc5 only a copy-mode read is completed partially, and a short write, or any non-negative result in user-copy or zero-copy mode, completes the whole request as successful. If you cannot transfer everything, fail the request.
- **Flush, discard, write-zeroes:** any non-negative value means success. Do not report the range length: discards can be gigabytes, and `nr_sectors << 9` overflows a signed 32-bit result at 2 GiB, which the kernel then treats as an error.
- **Failure:** a negative errno. The block layer maps it to a block status (`-EIO` to an I/O error, `-ENOSPC` to no-space, `-EOPNOTSUPP` to not-supported, and so on), which is what the application ultimately sees.

[I/O operations and flags](/guide/io-operations/) covers each operation and the durability rules.

## Shutdown and failure

When the device is stopped, every armed tag's command completes with `UBLK_IO_RES_ABORT`, and a queue thread exits once all its tags have. Tags the server owns at that moment still need their commits: `STOP_DEV` waits for them.

If the server process dies, its io_uring instances are torn down and the kernel cancels their pending commands. When the last reference to `/dev/ublkcN` goes away, the driver decides what happens to the device. Without a recovery flag it stops the device: requests the dead server had fetched but not committed fail, and the disk is removed. With [user recovery](/guide/recovery/) enabled, the disk stays, and requests are failed, requeued or held depending on the flags until a new server takes over. Either way the device remains registered until someone sends `DEL_DEV`.

There is no request timeout for a privileged device: the kernel resets the block layer's timer indefinitely, so a request the server never commits blocks its submitter forever, in uninterruptible sleep, until the server commits it or exits. For an unprivileged device a request timeout kills the server process with `SIGKILL` instead, because an untrusted server must not be able to hang the host.
