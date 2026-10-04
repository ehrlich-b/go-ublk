---
title: "Roadmap"
linkTitle: "Roadmap"
description: "What go-ublk does not do yet, what is being built, and the open defects, in priority order."
weight: 100
---

The ordering principle is the one a backup and disaster-recovery product needs: **correctness and recoverability first, performance last.** Nothing on this page is implemented unless it says so. The [UAPI reference](/reference/uapi/) tracks go-ublk's status for every kernel item individually.

## Open defects

Found by a code audit on 2026-10-03 unless noted. These come before new features.

| # | Defect | Impact |
|---|---|---|
| 17 | Every io_uring the library creates stays mapped until process exit (the ring fd is closed but its mappings are not) | About four rings leak per device create/close cycle and three per `ListDevices` call; a daemon that creates devices on demand grows without bound. Measured |
| 18 | `Device.Close` ignores a failed `STOP_DEV` and tears the queues down anyway | `STOP_DEV` on a mounted, dirty device can outlast the 10-second control timeout; tearing down then strands in-flight I/O |
| 19 | A queue's memory is released after a 2-second join timeout even if its goroutine is still running | A backend call slower than 2 s at teardown can fault or corrupt memory. Leaking is the safe failure |
| 20 | A queue goroutine can exit on an unexpected completion while the device stays live, and nothing notices | I/O on that queue hangs until `STOP_DEV` |
| 21 | Buffers passed to asynchronous control commands are not pinned for the whole time the kernel may use them | A timed-out `ADD_DEV` can let a late kernel write land in freed memory |
| 15 | The shutdown-ordering wedge is solved by deployment (a systemd unit), but its trigger is unexplained: the daemon is reported "blocked by coredump" | Hypothesis to test: an unhandled SIGHUP from logind kills the daemon mid-`STOP_DEV`. The examples do not handle SIGHUP yet |
| 4 | Debug logging holds one lock across a blocking write | Only with `Options.Debug`; can stall queues under load |
| 5 | All memory fences share one global variable | Correct; a possible contention point, to change only if profiling shows it |

## Kernel features

Being built now, by parallel work streams:

- **User recovery** (`UBLK_F_USER_RECOVERY`, with reissue and fail-fast variants, and `QUIESCE_DEV`): restart or upgrade the server without removing `/dev/ublkbN` or the filesystem on it. This is the most important missing feature for production use; today a server crash takes the device with it. See [User recovery and quiesce](/guide/recovery/) for how the kernel side works.
- **Asynchronous backends**: let one queue have many backend operations in flight, so latency-bound backends are not limited to one request per queue at a time.
- **Zero copy**: `UBLK_U_IO_REGISTER_IO_BUF` and `UBLK_F_AUTO_BUF_REG`, so backend I/O can use the request's pages directly.
- **Batch I/O** (`UBLK_F_BATCH_IO`, Linux 7.0).
- **Full UAPI coverage** as of Linux 7.3-rc5: every command, flag and parameter block marshaled and tested, with runtime feature detection through `GET_FEATURES`.

Not yet started, roughly in priority order:

- Per-write FUA (`UBLK_ATTR_FUA` and `UBLK_IO_F_FUA`), so a backend that can make one write durable cheaply does not need a full flush.
- Online resize (`UPDATE_SIZE`).
- Passing errnos from the backend to the block layer instead of always `EIO`.
- Unprivileged devices, user copy, zoned devices, integrity metadata, shared-memory zero copy, and `NEED_GET_DATA` for older kernels. The corresponding `DeviceParams` switches are placeholders today; see [Configuration](/go-ublk/configuration/#kernel-feature-switches).
- `TRY_STOP_DEV`, `DEL_DEV_ASYNC`, `NO_AUTO_PART_SCAN`, and a public `GET_DEV_INFO`.

## Testing and releases

- **A kernel and distribution matrix**: the test suite run against every kernel family that can be booted, with results published on the [compatibility matrix](/reference/matrix/). In progress.
- **Fuzzing** beyond the UAPI decoders: the data path and control-plane error handling. In progress.
- **Well-tested releases**: tagged versions with the verification behind each one recorded in the [changelog](/go-ublk/releases/).
- Real-kernel tests in CI, not only unit tests.
- A real host power cut, as opposed to a guest reset, in the power-fail test.
- Soak tests, and fault injection under memory and GC pressure.

## Deployment

- Ship the systemd service and mount units with the examples, and make the examples handle SIGHUP. See [Deployment](/go-ublk/deployment/).
- Daemon supervision: detect and clean up stuck devices so that a wedged server never requires a host reboot.

## Performance

Only after the above. Candidates: io_uring `SQPOLL`, registered buffers, hot-path profiling, and an NBD backend example.
