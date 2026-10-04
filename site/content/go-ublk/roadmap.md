---
title: "Roadmap"
linkTitle: "Roadmap"
description: "What go-ublk does not do yet, what is next, and the open defects, in priority order."
weight: 100
---

The ordering principle is the one a backup and disaster-recovery product needs: **correctness and recoverability first, performance last.** The [UAPI reference](/reference/uapi/) tracks go-ublk's status for every kernel item individually.

## Done in v0.2.0

The October 2026 overhaul (see the [changelog](/go-ublk/releases/)) closed what this page used to list as the priorities:

- **User recovery**: `RecoveryReissue`/`RecoveryQueue`/`RecoveryFailIO`, `Detach` (with `QUIESCE_DEV`), `Recover`, device tags. Tested against SIGKILL and live upgrade handoffs under load.
- **Asynchronous backends**: a goroutine per request by default, and a raw `Handler` that can complete from anywhere.
- **Full control-plane coverage** of the 7.3-rc5 UAPI: every command typed and tested, `GET_FEATURES` negotiation that refuses rather than silently degrades, unprivileged devices, every parameter block.
- **Lifecycle defects** #17–#23: the ring leak, `STOP_DEV` failures, use-after-unmap, silent queue death, unpinned control buffers, the context-cancel wedge, and the restart crash.
- **Zoned devices** (host-managed, through a `Handler`, with zone reports and zone append).
- **Zero copy** for file-backed devices (`REGISTER_IO_BUF`, `AUTO_BUF_REG`): fixed-buffer file I/O on the queue's ring, no copy through Go memory.
- Per-write FUA, `UPDATE_SIZE`, errno pass-through, `TRY_STOP_DEV`, `NO_AUTO_PART_SCAN`, user copy, `NEED_GET_DATA`, per-I/O threads, a public `GET_DEV_INFO`, the shipped systemd units with SIGHUP handling.

## Kernel features not yet implemented

In priority order:

- **Shared-memory zero copy** — `UBLK_F_SHMEM_ZC` and `REG_BUF` (7.1): requests whose pages live in memory the server registered arrive without a copy. The control commands exist; the data path does not use them.
- **Batch I/O** — `UBLK_F_BATCH_IO` (7.0): fetch and commit many requests per command with multishot `FETCH_IO_CMDS`.
- **Integrity metadata** — `UBLK_F_INTEGRITY` (7.0): protection-information buffers, through user copy.
- **`UBLK_F_IO_DESC_SIZE`** (7.3): larger descriptors. The engine already honors a descriptor stride; nothing requests the feature yet.

## Testing and releases

- **The kernel and distribution matrix** runs the conformance suite under every bootable kernel; results are on the [compatibility matrix](/reference/matrix/). Next: run it in CI on every release tag (a workflow with KVM exists and is untested).
- An unprivileged end-to-end test (serving I/O as a non-root user with the udev rules in place).
- A real host power cut, as opposed to a guest reset, in the power-fail test.
- Soak tests over days, and fault injection under memory and GC pressure.
- Longer fuzzing runs of the engine state machine and the UAPI decoders.

## Open defects

| # | Defect | Impact |
|---|---|---|
| 15 | The shutdown-ordering wedge is solved by deployment (a systemd unit), and the examples now handle SIGHUP — the likeliest trigger — but the fix has not been re-run through the shutdown-storm test | Run the server as a systemd unit as documented |
| 4 | Debug logging holds one lock across a blocking write | Only with `Options.Debug` |

## Performance

Only after the above. Candidates: io_uring `SQPOLL`, measuring and tuning the goroutine-per-request dispatch against `Inline`, and batch I/O.
