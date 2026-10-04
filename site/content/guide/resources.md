---
title: "Other implementations and resources"
linkTitle: "Implementations & resources"
description: "Other ublk servers and libraries, the kernel sources and documentation, and where to look when the documentation runs out."
weight: 150
---

## ublk servers and libraries

| Project | Language | What it is |
|---|---|---|
| [ublksrv](https://github.com/ublk-org/ublksrv) | C | The original userspace side, by the driver's author: `libublksrv` for building servers, and the `ublk` command-line tool (`ublk add -t loop -f disk.img`, `ublk list`, `ublk del`) with built-in targets such as null and loop. <!-- VERIFY: current list of built-in ublksrv targets (null, loop; nbd and qcow2 were historically separate or experimental) --> The kernel documentation still links its old home, `github.com/ming1/ubdsrv`. |
| [libublk-rs](https://github.com/ublk-org/libublk-rs) | Rust | A Rust library for ublk servers with sync and async APIs. Its control code is a good second reference for the unprivileged device-path protocol and for which commands may run asynchronously. |
| [rublk](https://github.com/ublk-org/rublk) | Rust | A command-line ublk server built on libublk-rs. |
| kublk ([`tools/testing/selftests/ublk/`](https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/tools/testing/selftests/ublk)) | C | The server the kernel's own selftests drive, with null, loop, stripe and fault-injection targets. It is updated in the same patch series as the driver, so it is the first working example of every new feature: batch I/O, auto buffer registration, shared-memory zero copy, per-I/O daemons, recovery and quiesce. |
| nbdublk (libnbd) | C | An NBD client that exposes a remote NBD export as a ublk device, built on `libublksrv`; the kernel documentation cites it as an example. <!-- VERIFY: nbdublk still ships with libnbd and builds against current libublksrv --> |
| SPDK ublk target | C | SPDK can export its block devices to the host as ublk devices. <!-- VERIFY: SPDK ublk module status and the RPC names (ublk_create_target, ublk_start_disk) --> |
| [e2b-dev/ublk-go](https://github.com/e2b-dev/ublk-go) | Go | An Apache-2.0 Go library, pure Go per its README, which states Linux 6.0+ and testing on 6.17. Version 0.1.3 was current at the time of writing. |
| [go-ublk](https://github.com/ehrlich-b/go-ublk) | Go | This project: an MIT-licensed, pure-Go library. See [go-ublk](/go-ublk/). |

If you are writing a server in a new language, read kublk for the current protocol and libublk-rs or ublksrv for a long-lived, production-shaped design.

## Kernel sources and documentation

- **Kernel documentation:** [docs.kernel.org/block/ublk.html](https://docs.kernel.org/block/ublk.html), from `Documentation/block/ublk.rst`. Short, but authoritative on intent: the control commands, the recovery modes, zero copy, auto buffer registration, batch I/O and shared-memory zero copy.
- **UAPI header:** [`include/uapi/linux/ublk_cmd.h`](https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/include/uapi/linux/ublk_cmd.h). The comments above each flag are often the only specification. This guide tracks it as of 7.3-rc5; the [UAPI reference](/reference/uapi/) is generated from it.
- **Driver:** [`drivers/block/ublk_drv.c`](https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/drivers/block/ublk_drv.c), also browsable with cross-references on [Elixir](https://elixir.bootlin.com/linux/latest/source/drivers/block/ublk_drv.c). When the documentation and the header disagree with the driver, the driver wins.
- **Selftests:** the kublk directory above also holds the shell tests that exercise each feature, which double as usage examples.

### io_uring

ublk is an io_uring client before it is anything else, so a server author needs to be comfortable with raw rings.

- [io_uring_setup(2)](https://man7.org/linux/man-pages/man2/io_uring_setup.2.html) and [io_uring_enter(2)](https://man7.org/linux/man-pages/man2/io_uring_enter.2.html): ring setup flags (`IORING_SETUP_SQE128` is required for control commands), the mmap offsets, and the enter flags including `IORING_ENTER_EXT_ARG` for bounded waits.
- [liburing](https://github.com/axboe/liburing): the reference library. Even if you do not link it, its source is the clearest statement of the SQ/CQ memory-ordering rules.
- "Efficient IO with io_uring" by Jens Axboe ([kernel.dk/io_uring.pdf](https://kernel.dk/io_uring.pdf)): the design paper.
- "Lord of the io_uring" ([unixism.net/loti](https://unixism.net/loti/)): a tutorial built around liburing. <!-- VERIFY: unixism.net/loti is still online -->

## Reading the driver

`ublk_drv.c` is one file of a few thousand lines. These functions are the map; names are from the 6.17 source and mostly stable across releases.

| Function | What it does |
|---|---|
| `ublk_ctrl_uring_cmd` | Entry point for every command on `/dev/ublk-control`. Requires a 128-byte SQE, returns `-EAGAIN` to non-blocking issue so commands run from io-wq, checks the opcode type, answers `GET_FEATURES` without a device, looks the device up, runs the permission check, then dispatches on `_IOC_NR`. |
| `ublk_ctrl_uring_cmd_permission` | `CAP_SYS_ADMIN` for privileged devices; for unprivileged ones, the device-path prefix and inode permission check described in [unprivileged devices](/guide/unprivileged/). |
| `ublk_ctrl_add_dev` | Validates the requested `ublksrv_ctrl_dev_info` (queue limits, recovery flag combinations, unprivileged restrictions), masks the flags to what the driver supports and forces some on, clamps the queue count to the CPU count, rounds the buffer size down to a page, allocates queues and the tag set, and creates `/dev/ublkcN`. |
| `ublk_validate_params` | Every rule `SET_PARAMS` enforces: block-size shifts, `max_sectors` against the buffer size, single-segment discard, read-only `DEVT`, zoned, DMA-alignment and segment constraints. See [device parameters](/guide/parameters/). |
| `ublk_ctrl_start_dev` | Turns the parameters into `queue_limits`, waits for every tag to be fetched, checks the server PID, allocates the gendisk and adds it, which creates `/dev/ublkbN`. |
| `ublk_queue_rq`, `ublk_prep_req`, `ublk_setup_iod` | The blk-mq side: refuse or requeue requests when there is no server, then write the request's descriptor into the per-queue array. |
| `ublk_dispatch_req` | Runs in the server's task context: copies write data into the server's buffer (copy mode) and completes the tag's pending fetch or commit command, which is the CQE the server sees. |
| `__ublk_ch_uring_cmd` | Per-tag I/O commands on `/dev/ublkcN`: `FETCH_REQ`, `COMMIT_AND_FETCH_REQ`, `NEED_GET_DATA`, buffer registration, including the daemon-task and ownership checks behind most `-EINVAL` and `-EBUSY` results. Later kernels inline it into `ublk_ch_uring_cmd`. |
| `ublk_ch_mmap` | The descriptor mapping: read-only, one mapping per queue at the fixed stride, exact size, same process. |
| `__ublk_complete_rq` | Turns a committed result into a block-layer completion: a `READ` that reports 0 bytes becomes `-EIO`, a short read or write completes the reported part and requeues the rest, and non-data operations only need a non-negative result. |
| `ublk_ch_read_iter`, `ublk_ch_write_iter` | User copy: `pread`/`pwrite` on `/dev/ublkcN` at an offset that encodes queue, tag and byte offset. |
| `ublk_uring_cmd_cancel_fn`, `ublk_cancel_cmd` | io_uring cancellation of pending commands when a ring exits: the source of `UBLK_IO_RES_ABORT`. |
| `ublk_ch_release_work_fn` | What happens when the server's `/dev/ublkcN` is released: fail or requeue the requests it held, then stop the device or move it to `QUIESCED` or `FAIL_IO`. See [user recovery](/guide/recovery/). |
| `ublk_timeout` | Request timeouts: restart the timer for privileged devices, `SIGKILL` the server for unprivileged ones. |

To find when something changed, `git log -L :function_name:drivers/block/ublk_drv.c` on a kernel tree is faster than any changelog. The [kernel version history](/guide/kernel-versions/) lists the UAPI side release by release, and [known kernel bugs](/guide/kernel-bugs/) the fixes that matter in practice.
