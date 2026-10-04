---
title: "Kernel version history"
linkTitle: "Kernel versions"
description: "What each Linux release added to ublk, from 6.0 to 7.3, derived from the UAPI header of every release."
weight: 130
---

ublk entered mainline in Linux 6.0 and has added to its UAPI in most releases since. Everything on this page is derived by diffing `include/uapi/linux/ublk_cmd.h` across every release from v6.0 to v7.3-rc5, so "since" means "first release whose header defines it". Two cautions apply. A symbol in the header is not always a working feature: `UBLK_F_SUPPORT_ZERO_COPY` was defined in 6.0 and did nothing useful until 6.15. And a release with no header change can still change driver behavior; the few such changes this guide relies on are listed separately below.

Distribution kernels backport freely, in both directions. Ubuntu's 6.17 kernels picked up later mainline ublk changes, and with them a crash that no mainline release had (see [known kernel bugs](/guide/kernel-bugs/)). When it matters, probe the running kernel rather than trusting its version string: `UBLK_U_CMD_GET_FEATURES` from 6.5 on, the flags `ADD_DEV` hands back, and `-EOPNOTSUPP` or `-EINVAL` from commands the kernel does not know.

## Minimum kernel for each feature

| You need | First release |
|---|---|
| ublk at all: the 8 original control commands, FETCH/COMMIT, copy mode, `NEED_GET_DATA`, basic and discard parameters | 6.0 |
| User recovery with failed or reissued in-flight I/O (`USER_RECOVERY`, `USER_RECOVERY_REISSUE`) | 6.1 |
| Unprivileged devices, `GET_DEV_INFO2`, the read-only `DEVT` parameter | 6.3 |
| ioctl-encoded commands (`UBLK_U_CMD_*`, `UBLK_U_IO_*`) and `UBLK_F_CMD_IOCTL_ENCODE` | 6.4 |
| `GET_FEATURES`, `UBLK_F_USER_COPY` (pread/pwrite on `/dev/ublkcN`) | 6.5 |
| Zoned devices | 6.6 |
| `DEL_DEV_ASYNC` | 6.9 |
| `USER_RECOVERY_FAIL_IO` and the `FAIL_IO` state | 6.13 |
| Working zero copy (`REGISTER_IO_BUF` / `UNREGISTER_IO_BUF`), DMA-alignment and segment parameters | 6.15 |
| `AUTO_BUF_REG`, `QUIESCE` / `QUIESCE_DEV`, `UPDATE_SIZE`, `PER_IO_DAEMON` | 6.16 |
| `BUF_REG_OFF_DAEMON` | 6.17 |
| Batch I/O, integrity metadata, `SAFE_STOP_DEV` / `TRY_STOP_DEV`, `NO_AUTO_PART_SCAN` | 7.0 |
| Shared-memory zero copy (`SHMEM_ZC`, `REG_BUF` / `UNREG_BUF`) | 7.1 |
| Variable descriptor size (`IO_DESC_SIZE`) | 7.3 |

go-ublk always sends ioctl-encoded commands, so it cannot work below 6.4, its minimum. Newer features are negotiated per device. The [compatibility matrix](/reference/matrix/) shows which kernels the conformance suite has passed on.

## Release by release

| Release | UAPI additions |
|---|---|
| **6.0** | Control: `GET_QUEUE_AFFINITY` (0x01), `GET_DEV_INFO` (0x02), `ADD_DEV` (0x04), `DEL_DEV` (0x05), `START_DEV` (0x06), `STOP_DEV` (0x07), `SET_PARAMS` (0x08), `GET_PARAMS` (0x09). I/O: `FETCH_REQ` (0x20), `COMMIT_AND_FETCH_REQ` (0x21), `NEED_GET_DATA` (0x22); results `UBLK_IO_RES_OK`, `UBLK_IO_RES_NEED_GET_DATA`, `UBLK_IO_RES_ABORT`. Flags `SUPPORT_ZERO_COPY` (reserved), `URING_CMD_COMP_IN_TASK`, `NEED_GET_DATA`. Parameters `BASIC`, `DISCARD`; attributes `READ_ONLY`, `ROTATIONAL`, `VOLATILE_CACHE`, `FUA`. Ops `READ`, `WRITE`, `FLUSH`, `DISCARD`, `WRITE_SAME`, `WRITE_ZEROES`; I/O flags `FAILFAST_DEV/TRANSPORT/DRIVER`, `META`, `FUA`, `NOUNMAP`, `SWAP`. States `DEAD`, `LIVE`. `UBLK_MAX_QUEUE_DEPTH` (4096), `UBLKSRV_CMD_BUF_OFFSET` (0), `UBLKSRV_IO_BUF_OFFSET` (0x80000000). |
| **6.1** | `START_USER_RECOVERY` (0x10), `END_USER_RECOVERY` (0x11); flags `USER_RECOVERY`, `USER_RECOVERY_REISSUE`; state `QUIESCED`. |
| 6.2 | none |
| **6.3** | `GET_DEV_INFO2` (0x12); flag `UNPRIVILEGED_DEV`; parameter `DEVT` with `struct ublk_param_devt`. `struct ublksrv_ctrl_cmd` changes from `data[2]` to `data[1]` plus `dev_path_len`, `pad`, `reserved` (same 32 bytes). `struct ublksrv_ctrl_dev_info` replaces `reserved0` with `owner_uid` and `owner_gid`. |
| **6.4** | ioctl-encoded forms of every command so far: `UBLK_U_CMD_*` for the 11 control commands and `UBLK_U_IO_*` for the 3 I/O commands. Flag `CMD_IOCTL_ENCODE`. |
| **6.5** | `UBLK_U_CMD_GET_FEATURES` (nr 0x13, encoded form only) and `UBLK_FEATURES_LEN` (8). Flag `USER_COPY`, with the pread/pwrite offset layout: `UBLK_IO_BUF_OFF`/`UBLK_IO_BUF_BITS` (25), `UBLK_TAG_OFF`/`UBLK_TAG_BITS` (16), `UBLK_QID_OFF`/`UBLK_QID_BITS` (12), `UBLK_MAX_NR_QUEUES` (4096), `UBLKSRV_IO_BUF_TOTAL_BITS`/`_SIZE`. |
| **6.6** | Flag `ZONED`; ops `ZONE_OPEN` (10), `ZONE_CLOSE` (11), `ZONE_FINISH` (12), `ZONE_APPEND` (13), `ZONE_RESET_ALL` (14), `ZONE_RESET` (15), `REPORT_ZONES` (18). `ublksrv_io_desc.nr_sectors` gains the union member `nr_zones`; `ublksrv_io_cmd.addr` gains `zone_append_lba`. Parameter `ZONED` with `struct ublk_param_zoned`. |
| 6.7, 6.8 | none |
| **6.9** | `UBLK_U_CMD_DEL_DEV_ASYNC` (0x14). |
| 6.10 to 6.12 | none |
| **6.13** | Flag `USER_RECOVERY_FAIL_IO`; state `FAIL_IO`. |
| 6.14 | none |
| **6.15** | `UBLK_U_IO_REGISTER_IO_BUF` (0x23), `UBLK_U_IO_UNREGISTER_IO_BUF` (0x24), which make `SUPPORT_ZERO_COPY` real. Parameters `DMA_ALIGN` (`struct ublk_param_dma_align`) and `SEGMENT` (`struct ublk_param_segment`, `UBLK_MIN_SEGMENT_SIZE`). |
| **6.16** | `UBLK_U_CMD_UPDATE_SIZE` (0x15), `UBLK_U_CMD_QUIESCE_DEV` (0x16). Flags `UPDATE_SIZE`, `AUTO_BUF_REG`, `QUIESCE`, `PER_IO_DAEMON`. I/O flag `NEED_REG_BUF`. `struct ublk_auto_buf_reg` with `UBLK_AUTO_BUF_REG_FALLBACK` and the `sqe->addr` conversion helpers. |
| **6.17** | Flag `BUF_REG_OFF_DAEMON`. |
| 6.18, 6.19 | none |
| **7.0** | `UBLK_U_CMD_TRY_STOP_DEV` (0x17). Batch I/O: `UBLK_U_IO_PREP_IO_CMDS` (0x25), `UBLK_U_IO_COMMIT_IO_CMDS` (0x26), `UBLK_U_IO_FETCH_IO_CMDS` (0x27), `struct ublk_batch_io`, `struct ublk_elem_header`, `UBLK_BATCH_F_*`. Flags `BATCH_IO`, `INTEGRITY`, `SAFE_STOP_DEV`, `NO_AUTO_PART_SCAN`. I/O flag `INTEGRITY`. Parameter `INTEGRITY` with `struct ublk_param_integrity`. `UBLKSRV_IO_INTEGRITY_FLAG` (bit 62 of the user-copy offset). |
| **7.1** | `UBLK_U_CMD_REG_BUF` (0x18), `UBLK_U_CMD_UNREG_BUF` (0x19), `struct ublk_shmem_buf_reg`, `UBLK_SHMEM_BUF_READ_ONLY`. Flag `SHMEM_ZC`; I/O flag `SHMEM_ZC`; `UBLK_SHMEM_ZC_*` address encoding and helpers. |
| 7.2 | none |
| **7.3-rc1** | Flag `IO_DESC_SIZE`; `ublksrv_ctrl_dev_info.pad0` becomes `io_desc_size`. No further UAPI changes through 7.3-rc5. |

## Legacy and ioctl-encoded opcodes

Linux 6.0 to 6.3 identified commands by bare numbers in the SQE's `cmd_op` (`UBLK_CMD_ADD_DEV` is 4, `UBLK_IO_FETCH_REQ` is 0x20). Linux 6.4 introduced ioctl-style encodings built with `_IOR`/`_IOWR('u', nr, struct ...)`, so that a command carries its type, direction and argument size like any ioctl, and the header now says not to use the bare numbers in new code. Every command added after 6.4 exists only in encoded form.

The driver dispatches on the `nr` field and accepts both forms, as long as the type byte is `'u'` or 0. Whether the bare numbers work at all is a build option: with `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES` disabled, anything without the `'u'` type fails with `-EOPNOTSUPP`. The option defaults to on; Ubuntu kernels and Amazon Linux 2023's `kernel6.18` are built with it off. (`UBLK_U_CMD_GET_FEATURES` and the batch I/O commands are matched on their full encoded value, never on `nr` alone.) A server that supports kernels older than 6.4 has to send legacy numbers there and encoded commands everywhere else; one that requires 6.4 can always send encoded commands, which is what go-ublk and the kernel selftests server do.

| Command | Legacy nr (since) | Encoded value (since) |
|---|---|---|
| `GET_QUEUE_AFFINITY` | 0x01 (6.0) | `0x80207501` (6.4) |
| `GET_DEV_INFO` | 0x02 (6.0) | `0x80207502` (6.4) |
| `ADD_DEV` | 0x04 (6.0) | `0xc0207504` (6.4) |
| `DEL_DEV` | 0x05 (6.0) | `0xc0207505` (6.4) |
| `START_DEV` | 0x06 (6.0) | `0xc0207506` (6.4) |
| `STOP_DEV` | 0x07 (6.0) | `0xc0207507` (6.4) |
| `SET_PARAMS` | 0x08 (6.0) | `0xc0207508` (6.4) |
| `GET_PARAMS` | 0x09 (6.0) | `0x80207509` (6.4) |
| `START_USER_RECOVERY` | 0x10 (6.1) | `0xc0207510` (6.4) |
| `END_USER_RECOVERY` | 0x11 (6.1) | `0xc0207511` (6.4) |
| `GET_DEV_INFO2` | 0x12 (6.3) | `0x80207512` (6.4) |
| `GET_FEATURES` | none | `0x80207513` (6.5) |
| `DEL_DEV_ASYNC` | none | `0x80207514` (6.9) |
| `UPDATE_SIZE` | none | `0xc0207515` (6.16) |
| `QUIESCE_DEV` | none | `0xc0207516` (6.16) |
| `TRY_STOP_DEV` | none | `0xc0207517` (7.0) |
| `REG_BUF` | none | `0xc0207518` (7.1) |
| `UNREG_BUF` | none | `0xc0207519` (7.1) |
| I/O `FETCH_REQ` | 0x20 (6.0) | `0xc0107520` (6.4) |
| I/O `COMMIT_AND_FETCH_REQ` | 0x21 (6.0) | `0xc0107521` (6.4) |
| I/O `NEED_GET_DATA` | 0x22 (6.0) | `0xc0107522` (6.4) |
| I/O `REGISTER_IO_BUF` | none | `0xc0107523` (6.15) |
| I/O `UNREGISTER_IO_BUF` | none | `0xc0107524` (6.15) |
| I/O `PREP_IO_CMDS` | none | `0xc0107525` (7.0) |
| I/O `COMMIT_IO_CMDS` | none | `0xc0107526` (7.0) |
| I/O `FETCH_IO_CMDS` | none | `0xc0107527` (7.0) |

The `0x20` in control-command values is the size of `struct ublksrv_ctrl_cmd` (32 bytes); the `0x10` in I/O command values is the 16-byte `struct ublksrv_io_cmd` or `struct ublk_batch_io`. `DEL_DEV_ASYNC` is encoded `_IOR` even though it changes state; copy the value, do not derive it.

## Zero copy before 6.15

`UBLK_F_SUPPORT_ZERO_COPY` has been bit 0 since 6.0. Through 6.15 the header described it as a scheme that would remap the driver's request pages into the server's address space, requiring 4 KiB blocks; that was never implemented, and through 6.14 the driver cleared the flag from the negotiated set. In 6.15 the bit took on its current meaning, io_uring fixed-buffer zero copy through `REGISTER_IO_BUF`. A server that sets it must check the flags `ADD_DEV` returns, and treat it as meaningful only from 6.15. See [data copy modes](/guide/data-copy/).

## Behavior changes without a header change

These change what a server observes but appear in no header diff. Only changes this guide could confirm in driver source or changelogs are listed.

- **Flags the kernel turns on by itself.** The driver always sets `UBLK_F_CMD_IOCTL_ENCODE` (6.4+) and `UBLK_F_URING_CMD_COMP_IN_TASK` (6.5+; 6.1–6.4 only for a modular build), plus `UBLK_F_PER_IO_DAEMON` (6.16+), `UBLK_F_BUF_REG_OFF_DAEMON` (6.17+) and `UBLK_F_SAFE_STOP_DEV` (7.0+) in the flags `ADD_DEV` returns, whatever the server asked for, and clears every bit it does not know. It also clears bits it cannot honor: `SUPPORT_ZERO_COPY` through 6.14, `UNPRIVILEGED_DEV` for a `CAP_SYS_ADMIN` caller, the recovery flags on unprivileged devices, `NEED_GET_DATA` with user copy (6.5+), zero copy (6.15+), automatic registration (6.16+) or batch I/O (7.0+), and `PER_IO_DAEMON` with batch I/O. Treat the returned flags as the truth.
- **Server PID validation.** In 6.17, `START_DEV` and `END_USER_RECOVERY` fail with `-EINVAL` unless `data[0]` equals the thread-group ID of the process that opened `/dev/ublkcN` ("ublk: validate ublk server pid", 6.17, stable 6.16.1). Older kernels accepted any positive PID.
- **`ublks_max`.** Since 6.15 the module parameter caps only unprivileged devices; from 6.3 to 6.14 it capped every device. See [unprivileged devices](/guide/unprivileged/).
- **`max_sectors` validation.** `SET_PARAMS` with `max_sectors` below one page's worth of sectors used to pass validation and then trip a `WARN_ON_ONCE` in the block layer at `START_DEV`. `1860c2f85922` ("ublk: reject max_sectors smaller than PAGE_SECTORS in parameter validation") rejects it with `-EINVAL` up front; it reached stable as 7.0.11 and Ubuntu's `linux-hwe-7.0` at 7.0.0-28.
- **Ubuntu 6.17.** The `linux-hwe-6.17` 6.17.0-24 changelog backports "ublk: implement NUMA-aware memory allocation" and "ublk: scan partition in async way", which is where the `ADD_DEV` crash and a partition-scan use-after-free described in [known kernel bugs](/guide/kernel-bugs/) came from.

## Common distribution kernels

| Distribution | Kernel | ublk |
|---|---|---|
| Ubuntu 22.04 GA | 5.15 | none |
| Ubuntu 24.04 GA | 6.8 | yes, with ioctl encoding and user copy; no zero copy. The module is in `linux-modules-extra`, which cloud images lack |
| Ubuntu 24.04 HWE | the 7.0 track since 2026-07-16 (`linux-hwe-7.0`, 7.0.0-38 current); 6.17 before that, and 6.14 and 6.11 before it | see [known kernel bugs](/guide/kernel-bugs/) before choosing a build |
| Debian 12 | 6.1 | not built (`CONFIG_BLK_DEV_UBLK` unset); the 6.12 kernels in bookworm-security and bookworm-backports have it |
| Debian 13 | 6.12 (7.2 in trixie-backports) | yes, up to `DEL_DEV_ASYNC` |
| WSL2 (Microsoft kernel 6.6) | 6.6 | `ublk_drv` not built |

The [compatibility matrix](/reference/matrix/) records what go-ublk's test suite actually did on each kernel it was run against.
