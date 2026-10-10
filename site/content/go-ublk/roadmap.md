---
title: "Roadmap"
linkTitle: "Roadmap"
description: "What go-ublk does not do yet, what is next, and the open defects, in priority order."
weight: 100
---

Priorities for backup and disaster recovery: **correctness and recoverability first, performance last.** The [UAPI reference](/reference/uapi/) tracks individual kernel items.

## Done in v0.2.0

The October 2026 [release](/go-ublk/releases/) completed:

- **Recovery**: `RecoveryReissue`/`RecoveryQueue`/`RecoveryFailIO`, `Detach` with `QUIESCE_DEV`, `Recover`, and device tags. Tested SIGKILL and live upgrades under load.
- **Asynchronous backends**: goroutine-per-request dispatch and a `Handler` that completes from anywhere.
- **7.3-rc5 control plane**: typed, tested commands, strict feature negotiation, unprivileged devices, every parameter block.
- **Lifecycle fixes #17–#23**: ring leaks, `STOP_DEV` failures, use-after-unmap, silent queue death, unpinned control buffers, context-cancel wedges, restart crashes.
- **Shared memory**: `SHMEM_ZC`, `REG_BUF`/`UNREG_BUF`; larger descriptors through `IO_DESC_SIZE`.
- **Batch I/O**: `PREP_IO_CMDS`, multishot `FETCH_IO_CMDS` with a provided-buffer ring, batched `COMMIT_IO_CMDS`.
- **Integrity**: kernel-verified T10-DIF, IP and NVMe CRC64 metadata.
- **Zoned devices**: host-managed zones, reports and append through a `Handler`.
- **File zero copy**: `REGISTER_IO_BUF`/`AUTO_BUF_REG`, fixed-buffer I/O on the queue ring without Go-memory copies.
- Per-write FUA, `UPDATE_SIZE`, errno pass-through, `TRY_STOP_DEV`, `NO_AUTO_PART_SCAN`, user copy, `NEED_GET_DATA`, per-I/O threads, public `GET_DEV_INFO`, systemd units and SIGHUP handling.

## Kernel features not yet implemented

Every 7.3-rc5 feature flag, control/I/O command and parameter block is implemented; see the [UAPI reference](/reference/uapi/). Some combinations are excluded, including shared-memory and file zero copy together. [Configuration](/go-ublk/configuration/) lists the restrictions.

## Testing and releases

- Run the [kernel/distribution matrix](/reference/matrix/) in CI on every release tag. The KVM workflow exists but is untested.
- Test non-root I/O with installed udev rules; the existing unprivileged test simulates udev ownership changes.
- Test a real host power cut; guest resets leave the host cache intact.
- Run multi-day soaks and memory/GC-pressure fault injection.
- Fuzz the engine state machine and UAPI decoders longer.

## Open defects

| # | Defect | Impact |
|---|---|---|
| 15 | Shutdown ordering is fixed and later storm runs passed. The original teardown hang remains unexplained; SIGHUP is a suspected trigger, now handled by the examples | Run the server as a systemd unit as documented |

## Performance

After correctness: measure `SQPOLL` and tune goroutine-per-request dispatch against `Inline` and batch I/O.