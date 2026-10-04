---
title: "Overview and architecture"
linkTitle: "Overview & architecture"
description: "What ublk is, how it compares to FUSE, NBD, TCMU and VDUSE, the three device nodes, and one request's path from an application to a ublk server and back."
weight: 10
---

ublk is a Linux block driver, `ublk_drv` (`drivers/block/ublk_drv.c`, config option `CONFIG_BLK_DEV_UBLK`), that hands every request on a block device to a userspace process. The block devices it creates are ordinary disks named `/dev/ublkbN`: you can partition them, put a filesystem on them, use them for swap or hand them to a VM. The process that serves them is called a **ublk server**. The driver was written by Ming Lei and merged in Linux 6.0.

The defining design choice is the transport. Requests and their completions travel as io_uring passthrough commands (`IORING_OP_URING_CMD`), not over a socket and not through `read()`/`write()` on a character device. A server keeps one command outstanding per in-flight request slot; the kernel completes that command when a request arrives; the server answers by submitting the next command, which carries the result and re-arms the slot. With io_uring batching, one system call can complete dozens of requests and wait for the next dozens.

## Why a userspace block device

The kernel's own documentation lists the usual reasons. A userspace block device can be written in any language and use any library. It can be debugged with ordinary tools, and when it crashes the machine does not. It can be updated without touching the kernel. That makes it the natural place for virtual disk formats (qcow2, VHD), network and object-storage backends, compression, encryption, deduplication, replication, and test devices that inject faults or simulate odd geometries.

Linux has several ways to do this. They sit at different layers:

| Mechanism | Layer | Transport between kernel and server | Notes |
|---|---|---|---|
| **ublk** | blk-mq block device | io_uring commands on a per-device char device, one ring per hardware queue | Copy, user copy, or zero copy. Linux 6.0+ |
| NBD | blk-mq block device | The NBD protocol over a socket | Server can be remote. Mature, simple, one more protocol hop |
| TCMU | SCSI target (LIO) | A shared-memory ring through UIO | Exposes a SCSI LUN through a fabric; SCSI command set |
| VDUSE | virtio device | vDPA bus, virtqueues in shared memory | Appears as virtio-blk through `virtio-vdpa` or to a VM through `vhost-vdpa` (Linux 5.15) |
| FUSE | VFS filesystem | `/dev/fuse` | A filesystem, not a block device |

ublk's advantages come from staying inside blk-mq and using io_uring end to end: the block layer's multiqueue model maps one to one onto server threads, a server can use the same io_uring for its backend I/O as for the ublk protocol, and recent kernels let the backend I/O use the request's pages directly ([zero copy](/guide/data-copy/)).

## The three device nodes

| Node | One per | Created by | What the server does with it |
|---|---|---|---|
| `/dev/ublk-control` | system | loading `ublk_drv` | Sends [control commands](/guide/control-plane/) (`ADD_DEV`, `SET_PARAMS`, `START_DEV`, ...) as io_uring commands |
| `/dev/ublkcN` | device | `ADD_DEV` | Opens it once, maps each queue's descriptor array from it, and issues [I/O commands](/guide/data-plane/) against it. In user-copy mode, also `pread`/`pwrite` on it |
| `/dev/ublkbN` | device | `START_DEV` | Nothing. This is the disk applications use. Partitions appear as `ublkbNpM` |

`N` is the device ID, chosen by the server or assigned by the kernel at `ADD_DEV`. The char device admits a single open at a time (a second `open()` fails with `EBUSY`), so a multi-threaded server opens it once and shares or duplicates the file descriptor.

## Control plane and data plane

The protocol has two halves, and a server usually gives them different threads:

- The **control plane** manages a device's life: create it, set its size and limits, start it, stop it, delete it, and on newer kernels quiesce, recover or resize it. Each command is an `IORING_OP_URING_CMD` submission on an io_uring whose target file is `/dev/ublk-control`, with a 32-byte `struct ublksrv_ctrl_cmd` in the submission entry. That needs a ring created with `IORING_SETUP_SQE128`.
- The **data plane** moves requests. The device has `nr_hw_queues` hardware queues of `queue_depth` tags each. A server typically runs one thread and one io_uring per queue. For each tag the kernel writes a 24-byte `struct ublksrv_io_desc` into a per-queue array that the server has mapped read-only from `/dev/ublkcN`, and signals it by completing the command the server left pending for that tag.

{{< diagram "architecture" "The control thread drives the device through `/dev/ublk-control`; each queue thread serves one hardware queue through `/dev/ublkcN`." >}}

## One request, end to end

Take a 4 KiB write at byte offset 1 MiB, on a device using the default copy mode.

**Setup, once.** The server sends `ADD_DEV`, which creates `/dev/ublkc0`, and `SET_PARAMS`, which sets the capacity and limits. It opens `/dev/ublkc0`. Each queue thread creates an io_uring, maps its queue's descriptor array, allocates one buffer per tag, and submits `UBLK_U_IO_FETCH_REQ` for every tag, passing that tag's buffer address. Then the control thread sends `START_DEV`, which waits until every tag of every queue has been fetched and then creates `/dev/ublkb0`.

1. An application calls `pwrite(fd, buf, 4096, 1048576)` on `/dev/ublkb0`. The block layer builds a request and allocates a tag, say tag 7 on hardware queue 2 (the queue is chosen by the submitting CPU).
2. `ublk_drv`'s `queue_rq` fills descriptor 7 of queue 2: operation `UBLK_IO_OP_WRITE`, `start_sector` 2048, `nr_sectors` 8, and `addr` set to the buffer the server registered for that tag. Sectors are always 512 bytes in this protocol, whatever the device's logical block size.
3. The driver copies the request's data into the server's buffer and completes the `FETCH_REQ` that tag 7 had pending. The copy runs in the context of the server's queue thread, which is why each tag is bound to the task that fetched it.
4. Queue 2's thread reaps a completion whose user data identifies tag 7 and whose result is 0. It reads descriptor 7, sees a write of 8 sectors at sector 2048, and writes 4096 bytes from tag 7's buffer to wherever its storage lives.
5. The thread submits `UBLK_U_IO_COMMIT_AND_FETCH_REQ` for tag 7 with result 4096. The driver ends the block request successfully, and the same command stays pending as tag 7's next fetch. The application's `pwrite` returns.

A read is the mirror image: no copy before delivery, and at commit the driver copies the bytes the server put in the tag's buffer into the request's pages. With [user copy](/guide/data-copy/) the server moves the bytes itself with `pread`/`pwrite` on `/dev/ublkcN`; with zero copy the bytes never pass through the server's memory at all.

In practice a queue thread reaps many completions per `io_uring_enter` and submits all their commits in one batch, so the per-request syscall cost amortizes to a fraction of a call.

## Who is responsible for what

The kernel owns the block-device side: request allocation and tags, merging and splitting to the limits you set, partition scanning, the descriptor array, and (in copy mode) moving the data. It does not interpret the data, cache it, or retry it.

The server owns everything else, and in particular three obligations that are easy to miss:

- **Answer every request.** For a normal privileged device the kernel never times a request out. A request the server fetched and never committed stays in flight, and whatever issued it waits in uninterruptible sleep, until the server commits it or exits.
- **Keep serving during `STOP_DEV`.** Removing the disk drains in-flight I/O, and only the server can complete it. A server that stops its queue threads before sending `STOP_DEV` hangs its own shutdown.
- **Use the right task.** The task that fetched a tag is the only one allowed to issue commands for it. Before Linux 6.16 that rule applies per queue rather than per tag.

## Limits

| Limit | Value | Source |
|---|---|---|
| Hardware queues per device | 4096 (`UBLK_MAX_NR_QUEUES`), and at most the number of CPU IDs | `ADD_DEV` clamps `nr_hw_queues` to `nr_cpu_ids` and returns the result |
| Tags per queue | 4096 (`UBLK_MAX_QUEUE_DEPTH`) | `ADD_DEV` rejects 0 or more than 4096 |
| Bytes per request | `max_io_buf_bytes` from `ADD_DEV`, rounded down to a page | `ADD_DEV` applies no other cap (6.17, 7.3-rc5); the user-copy offset encoding leaves 25 bits (32 MiB) per tag |
| Sector unit | 512 bytes, always | Descriptors and parameters count 512-byte sectors |
| Logical block size | 512 bytes to `PAGE_SIZE`, power of two | `SET_PARAMS` validation |
| Device IDs | 0 to 2<sup>20</sup> - 1 | Minor-number space; unprivileged devices are further capped by the `ublks_max` module parameter |

## Requirements

- A kernel with `CONFIG_BLK_DEV_UBLK`, usually as the module `ublk_drv`: `modprobe ublk_drv` creates `/dev/ublk-control`. Distribution kernels generally build it, sometimes in a separate package (on Ubuntu's AWS kernels it is in `linux-modules-extra`). WSL2 kernels do not include it.
- io_uring enabled. The `kernel.io_uring_disabled` sysctl (Linux 6.6+) can disable it for everyone or restrict it to a group.
- `CAP_SYS_ADMIN` for the control commands, unless you use [unprivileged devices](/guide/unprivileged/).
- For a server that targets more than one kernel release, a plan for features that come and go. The [kernel version history](/guide/kernel-versions/) lists when each one appeared; the [control plane](/guide/control-plane/) chapter covers detecting them at run time.
