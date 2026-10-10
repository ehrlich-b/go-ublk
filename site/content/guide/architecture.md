---
title: "Overview and architecture"
linkTitle: "Overview & architecture"
description: "What ublk is, how it compares to FUSE, NBD, TCMU and VDUSE, the three device nodes, and one request's path from an application to a ublk server and back."
weight: 10
---

Linux's `ublk_drv` (`drivers/block/ublk_drv.c`, `CONFIG_BLK_DEV_UBLK`) sends block requests to a userspace ublk server. Its `/dev/ublkbN` disks support partitions, filesystems, swap, and VM use. Ming Lei wrote the driver; it merged in 6.0.

Requests and completions use io_uring passthrough commands (`IORING_OP_URING_CMD`). Each slot holds a waiting command; arrival completes it, and the server submits a result plus the next fetch. Batching can complete and await dozens of requests per syscall, without socket or character-device read/write request transport.

## Why a userspace block device

Kernel documentation motivates userspace for language/library choice, ordinary debugging tools, process-level crash isolation, and updates independent of the kernel. Uses include qcow2/VHD disks, network/object storage, compression, encryption, deduplication, replication, and fault/geometry test devices.

Linux alternatives operate at different layers:

| Mechanism | Layer | Transport between kernel and server | Notes |
|---|---|---|---|
| **ublk** | blk-mq block device | io_uring commands on a per-device char device, one ring per hardware queue | Copy, user copy, or zero copy. Linux 6.0+ |
| NBD | blk-mq block device | The NBD protocol over a socket | Server can be remote. Mature, simple, one more protocol hop |
| TCMU | SCSI target (LIO) | A shared-memory ring through UIO | Exposes a SCSI LUN through a fabric; SCSI command set |
| VDUSE | virtio device | vDPA bus, virtqueues in shared memory | Appears as virtio-blk through `virtio-vdpa` or to a VM through `vhost-vdpa` (Linux 5.15) |
| FUSE | VFS filesystem | `/dev/fuse` | A filesystem, not a block device |

blk-mq queues map to server threads, and the same ring can handle protocol and backend I/O. Newer kernels let backend I/O use request pages directly through [zero copy](/guide/data-copy/).

## The three device nodes

| Node | One per | Created by | What the server does with it |
|---|---|---|---|
| `/dev/ublk-control` | system | loading `ublk_drv` | Sends [control commands](/guide/control-plane/) (`ADD_DEV`, `SET_PARAMS`, `START_DEV`, ...) as io_uring commands |
| `/dev/ublkcN` | device | `ADD_DEV` | Opens it once, maps each queue's descriptor array from it, and issues [I/O commands](/guide/data-plane/) against it. In user-copy mode, also `pread`/`pwrite` on it |
| `/dev/ublkbN` | device | `START_DEV` | Nothing. This is the disk applications use. Partitions appear as `ublkbNpM` |

N is the requested or automatically assigned ADD_DEV ID. The char node permits one open (`EBUSY` otherwise); threads share or duplicate that fd.

## Control plane and data plane

Servers usually separate control and data threads:

- Control manages creation, size/limits, start/stop/delete, and newer quiesce/recovery/resize commands. Each IORING_OP_URING_CMD targets `/dev/ublk-control` with a 32-byte ublksrv_ctrl_cmd, requiring IORING_SETUP_SQE128.
- Data uses nr_hw_queues queues of queue_depth tags, typically with one thread/ring each. The kernel writes each tag's 24-byte ublksrv_io_desc in the server's read-only char-device mapping, then completes its pending command.

{{< diagram "architecture" "The control thread drives the device through `/dev/ublk-control`; each queue thread serves one hardware queue through `/dev/ublkcN`." >}}

## One request, end to end

Example: a 4 KiB write at 1 MiB in default copy mode.

At setup, ADD_DEV creates `/dev/ublkc0`; SET_PARAMS sets capacity/limits. Open the char node, create queue rings, map descriptors, allocate per-tag buffers, and issue UBLK_U_IO_FETCH_REQ with each buffer address. START_DEV waits for every tag, then creates `/dev/ublkb0`.

1. `pwrite(fd, buf, 4096, 1048576)` targets `/dev/ublkb0`. The block layer allocates, say, tag 7 on queue 2, selected by submitting CPU.
2. `queue_rq` fills that descriptor: UBLK_IO_OP_WRITE, start_sector 2048, nr_sectors 8, addr naming tag 7's buffer. Sectors are always 512 bytes, regardless of logical block size.
3. The driver copies data in the queue thread's context and completes tag 7's FETCH_REQ; this context requirement binds the tag to its fetching task.
4. Queue 2 reaps result 0 with user_data identifying tag 7, reads its descriptor, and writes 4096 bytes from the buffer to storage.
5. UBLK_U_IO_COMMIT_AND_FETCH_REQ with result 4096 completes the write and becomes tag 7's next waiting fetch. The application's pwrite returns.

READs copy nothing before delivery; commit copies server-filled bytes into request pages. [User copy](/guide/data-copy/) uses pread/pwrite on `/dev/ublkcN`; fixed-buffer zero copy bypasses server memory.

Each `io_uring_enter` can reap multiple completions and submit their commits, amortizing syscall cost.

## Who is responsible for what

The driver handles requests/tags, merging/splitting to configured limits, partition scanning, descriptors, and copy-mode transfers. It does not interpret or cache data, or retry backend operations.

The server must:

- Answer each request. Privileged-device timeouts reset indefinitely; uncommitted I/O leaves callers in uninterruptible sleep until completion or server exit.
- Serve throughout STOP_DEV, whose drain needs those queues. Stopping threads first hangs shutdown.
- Issue a tag's commands from its fetching task. Before 6.16, this ownership applies to the entire queue.

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

- CONFIG_BLK_DEV_UBLK, usually the ublk_drv module. `modprobe ublk_drv` creates `/dev/ublk-control`. Distributions may package it separately, e.g. Ubuntu AWS linux-modules-extra; checked WSL2 kernels omit it.
- Enabled io_uring: kernel.io_uring_disabled (6.6+) can disable it or restrict access to a group.
- CAP_SYS_ADMIN unless using [unprivileged devices](/guide/unprivileged/).
- Feature negotiation across kernel releases: [version history](/guide/kernel-versions/) records introductions; [control commands](/guide/control-plane/) detect support at runtime.