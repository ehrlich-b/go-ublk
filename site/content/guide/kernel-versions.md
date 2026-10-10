---
title: "Kernel version history"
linkTitle: "Kernel versions"
description: "What each Linux release added to ublk, from 6.0 to 7.3, derived from the UAPI header of every release."
weight: 130
---

ublk entered mainline in 6.0. Header diffs through v7.3-rc5 establish first definitions, not necessarily working features: SUPPORT_ZERO_COPY existed in 6.0 but worked only from 6.15. Driver behavior can also change without UAPI additions; relevant cases appear below.

Distributions backport changes: Ubuntu 6.17 acquired later features and an [ADD_DEV crash](/guide/kernel-bugs/). Probe GET_FEATURES (6.5+), negotiated ADD_DEV flags, and unknown-command errors (`-EOPNOTSUPP`/`-EINVAL`) instead of relying on version strings.

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

go-ublk requires 6.4 for ioctl-encoded commands and negotiates newer features per device. See [tested kernels](/reference/matrix/).

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

Linux 6.0-6.3 used raw `cmd_op` numbers: ADD_DEV 4, FETCH_REQ 0x20. Linux 6.4 introduced `_IOR`/`_IOWR('u', nr, struct ...)`, encoding type, direction, and size. The header discourages new legacy users; later commands have only encoded forms.

Dispatch normally uses `nr`, accepting type byte 'u' or 0. Disabling `CONFIG_BLKDEV_UBLK_LEGACY_OPCODES` rejects legacy commands with `-EOPNOTSUPP`; it defaults on, but Ubuntu and Amazon Linux 2023 `kernel6.18` disable it. GET_FEATURES and batch commands match full encoded values. Pre-6.4 support needs legacy commands; go-ublk and the kernel selftest server require 6.4 and use encoded forms.

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

Encoded 0x20 denotes the 32-byte control struct; 0x10 denotes the 16-byte I/O or batch struct. DEL_DEV_ASYNC uses `_IOR` despite mutating state; copy the encoding.

## Zero copy before 6.15

`UBLK_F_SUPPORT_ZERO_COPY` has occupied bit 0 since 6.0. Through 6.15, its header comment described an unimplemented page-remapping scheme requiring 4 KiB blocks; drivers through 6.14 cleared it. Working 6.15 zero copy uses REGISTER_IO_BUF and io_uring fixed buffers. Check negotiated flags; see [copy modes](/guide/data-copy/).

## Behavior changes without a header change

Driver-source/changelog findings without header changes:

- Kernel-set bits: CMD_IOCTL_ENCODE (6.4+), URING_CMD_COMP_IN_TASK (6.5+, or modular 6.1-6.4), PER_IO_DAEMON (6.16+), BUF_REG_OFF_DAEMON (6.17+), SAFE_STOP_DEV (7.0+). Unknown bits clear, as do SUPPORT_ZERO_COPY before 6.15, UNPRIVILEGED_DEV for CAP_SYS_ADMIN callers, and unprivileged recovery flags. NEED_GET_DATA clears with user copy (6.5+), zero copy (6.15+), auto registration (6.16+), or batch (7.0+); batch also clears PER_IO_DAEMON. Honor returned flags.
- START_DEV/END_USER_RECOVERY require `data[0]` equal to the char-device opener's thread-group ID (`-EINVAL` otherwise), since "ublk: validate ublk server pid" (6.17, stable 6.16.1). Earlier kernels accepted any positive PID.
- `ublks_max` caps unprivileged devices since 6.15; in 6.3-6.14 it capped all devices. See [unprivileged limits](/guide/unprivileged/).
- `1860c2f85922` ("ublk: reject max_sectors smaller than PAGE_SECTORS in parameter validation") changes sub-page max_sectors from SET_PARAMS success followed by START_DEV WARN_ON_ONCE to upfront EINVAL. It reached stable 7.0.11 and Ubuntu linux-hwe-7.0 7.0.0-28.
- Ubuntu linux-hwe-6.17 6.17.0-24 backported "ublk: implement NUMA-aware memory allocation" and "ublk: scan partition in async way", introducing the [ADD_DEV crash and partition-scan use-after-free](/guide/kernel-bugs/).

## Common distribution kernels

| Distribution | Kernel | ublk |
|---|---|---|
| Ubuntu 22.04 GA | 5.15 | none |
| Ubuntu 24.04 GA | 6.8 | yes, with ioctl encoding and user copy; no zero copy. The module is in `linux-modules-extra`, which cloud images lack |
| Ubuntu 24.04 HWE | the 7.0 track since 2026-07-16 (`linux-hwe-7.0`, 7.0.0-38 current); 6.17 before that, and 6.14 and 6.11 before it | see [known kernel bugs](/guide/kernel-bugs/) before choosing a build |
| Debian 12 | 6.1 | not built (`CONFIG_BLK_DEV_UBLK` unset); the 6.12 kernels in bookworm-security and bookworm-backports have it |
| Debian 13 | 6.12 (7.2 in trixie-backports) | yes, up to `DEL_DEV_ASYNC` |
| WSL2 (Microsoft kernel 6.6) | 6.6 | `ublk_drv` not built |

The [matrix](/reference/matrix/) records actual suite results by kernel.