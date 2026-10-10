---
title: "Zoned devices"
linkTitle: "Zoned devices"
description: "UBLK_F_ZONED: exposing a zoned block device, zone operations, REPORT_ZONES and zone append."
weight: 110
---

Zoned devices divide capacity into fixed-size zones. Sequential zones require writes at a write pointer and reset before rewriting; conventional zones permit ordinary random I/O. SMR disks and NVMe ZNS SSDs expose these semantics to applications and zoned f2fs/btrfs.

{{< uapi "UBLK_F_ZONED" >}} (6.6) supports zoned emulation for testing and log-structured or append-only backends.

## Requirements

Creation/startup requirements:

| Requirement | Where | If missing |
|---|---|---|
| `UBLK_F_ZONED` in the `ADD_DEV` flags | `ADD_DEV` | not a zoned device |
| A kernel built with `CONFIG_BLK_DEV_ZONED` | `ADD_DEV` | `-EINVAL` |
| `UBLK_F_USER_COPY` or `UBLK_F_SUPPORT_ZERO_COPY` | `ADD_DEV` | `-EINVAL` |
| `UBLK_PARAM_TYPE_ZONED` parameters, and only on a zoned device | `SET_PARAMS` | `-EINVAL` |
| `basic.chunk_sectors` set to the zone size, a power of two | `SET_PARAMS` | `-EINVAL` |
| `zoned.max_zone_append_sectors` non-zero | `SET_PARAMS` | `-EINVAL` |

Zone append returns its sector in `ublksrv_io_cmd.zone_append_lba`, sharing the addr union needed by copy mode's next buffer. Use USER_COPY or SUPPORT_ZERO_COPY; automatic registration alone is insufficient.

6.17 requires non-zero `chunk_sectors`; 7.3-rc5 requires a power of two. All versions count zones as `dev_sectors >> log2(chunk_sectors)`, omitting partial trailing zones. Align capacity to whole zones.

## Zoned parameters

`struct ublk_param_zoned`: 32 bytes at ublk_params offset 76; see [parameters](/guide/parameters/).

```c
struct ublk_param_zoned {
	__u32 max_open_zones;           /* 0 = no limit; must be <= number of zones */
	__u32 max_active_zones;         /* 0 = no limit; must be <= number of zones */
	__u32 max_zone_append_sectors;  /* largest zone append, 512-byte sectors; non-zero */
	__u8  reserved[20];
};
```

`ublk_param_basic.chunk_sectors` gives zone size in 512-byte sectors.

## Starting a zoned device

Before exposing a zoned disk, `START_DEV` applies limits and revalidates layout through `REPORT_ZONES`. Failed reports fail startup.

Serve queues while `START_DEV` waits on the control ring. A single thread waiting synchronously before processing completions deadlocks.

## Operations

| Op | Value | Data | Descriptor fields | Result |
|---|---|---|---|---|
| `UBLK_IO_OP_ZONE_OPEN` | 10 | none | `start_sector` = zone start | 0 or -errno |
| `UBLK_IO_OP_ZONE_CLOSE` | 11 | none | `start_sector` = zone start | 0 or -errno |
| `UBLK_IO_OP_ZONE_FINISH` | 12 | none | `start_sector` = zone start | 0 or -errno |
| `UBLK_IO_OP_ZONE_APPEND` | 13 | written data | `start_sector` = zone start, `nr_sectors` = length | bytes, plus `zone_append_lba` |
| `UBLK_IO_OP_ZONE_RESET_ALL` | 14 | none | whole device | 0 or -errno |
| `UBLK_IO_OP_ZONE_RESET` | 15 | none | `start_sector` = zone start | 0 or -errno |
| `UBLK_IO_OP_REPORT_ZONES` | 18 | report written by the server | `start_sector`, `nr_zones` | bytes written |

READ/WRITE/FLUSH work normally. The block layer orders sequential-zone writes, but the server must track write pointers and reject misplaced writes.

Without `UBLK_F_ZONED`, the kernel rejects zone operations before delivery.

### Zone append

Append targets the zone named by `start_sector`, writes at its current pointer, advances it, and returns the landing sector. Read payload as for WRITE (pread on `/dev/ublkcN` in user-copy mode); commit byte count and LBA:

```c
struct ublksrv_io_cmd *cmd = sqe_cmd(sqe);
cmd->q_id   = q_id;
cmd->tag    = tag;
cmd->result = nr_bytes;                /* or -errno */
cmd->zone_append_lba = landed_sector;  /* absolute, 512-byte sectors */
submit(UBLK_U_IO_COMMIT_AND_FETCH_REQ);
```

The kernel copies `zone_append_lba` into the completed request's sector. This is user-copy commit's exception to addr = 0. [Batch I/O](/guide/batch-io/) carries the LBA in `zone_lba` with `UBLK_BATCH_F_HAS_ZONE_LBA`.

### REPORT_ZONES

The driver creates `REPORT_ZONES` for `BLKREPORTZONE`, filesystem mounts, and `START_DEV` revalidation, rather than application I/O. It allocates a zeroed buffer and supplies:

- `start_sector`: the first sector of the first zone to report;
- `nr_zones` (the union member that is `nr_sectors` for other ops): the most zones to report.

Fill `struct blk_zone` entries from `<linux/blkzoned.h>` (64 bytes each), starting at `start_sector`, and pwrite them at byte offset 0 of the tag's user-copy position. End short reports with a zeroed entry; len = 0 terminates parsing. Large reports split across requests with advancing `start_sector`.

```c
struct blk_zone z[nr_zones];
memset(z, 0, sizeof(z));
for (unsigned i = 0, s = desc->start_sector; i < desc->nr_zones && s < dev_sectors; i++, s += zone_sectors) {
	z[i].start    = s;                     /* all in 512-byte sectors */
	z[i].len      = zone_sectors;
	z[i].capacity = zone_sectors;          /* usable size, <= len */
	z[i].wp       = zones[s / zone_sectors].wp;
	z[i].type     = BLK_ZONE_TYPE_SEQWRITE_REQ;   /* or BLK_ZONE_TYPE_CONVENTIONAL */
	z[i].cond     = zones[s / zone_sectors].cond; /* EMPTY, IMP_OPEN, EXP_OPEN, CLOSED, FULL, ... */
}
ssize_t n = pwrite(ublkc_fd, z, sizeof(z), ublk_user_copy_pos(q_id, tag, 0));
commit(q_id, tag, n < 0 ? -EIO : (int)n);
```

Commit bytes written, normally `nr_zones × 64` (rublk uses pwrite's result), or negative errno. Before 7.3, `blk_update_request` re-dispatches a zero result indefinitely and re-dispatches the remainder after a short result. From 7.3, any non-negative result completes the report.

## A minimal zone model

Track each sequential zone's write pointer and condition:

| Operation | Effect on the zone |
|---|---|
| WRITE at `wp` | `wp += len`; `EMPTY` or `CLOSED` becomes `IMP_OPEN`; `FULL` when `wp` reaches the capacity |
| WRITE elsewhere | fail with an error |
| ZONE_APPEND | as WRITE at `wp`; return the old `wp` as the LBA |
| ZONE_OPEN / ZONE_CLOSE | `EXP_OPEN` / `CLOSED` (a closed empty zone returns to `EMPTY`) |
| ZONE_FINISH | `wp` = end of zone, `FULL` |
| ZONE_RESET | `wp` = zone start, `EMPTY`; the zone's data may be discarded |
| ZONE_RESET_ALL | reset every sequential zone |

Enforce advertised `max_open_zones`/`max_active_zones`. Conventional zones (`BLK_ZONE_TYPE_CONVENTIONAL`, `BLK_ZONE_COND_NOT_WP`) accept arbitrary writes and ignore zone operations.

The kernel selftest server lacks a zoned target; Rust rublk provides `rublk add zoned` as a reference.

## go-ublk

Set `DeviceParams.EnableZoned` and `DeviceParams.Zoned` (zone size, open/active limits, maximum append size) for host-managed zones (6.6+). go-ublk enables user copy to return append LBAs. A `Handler` receives `OpZoneOpen`, `OpZoneClose`, `OpZoneFinish`, `OpZoneReset`, `OpZoneResetAll`, and `OpZoneAppend` (complete with `Request.CompleteZoneAppend(lba, err)`); it receives `OpReportZones` (`Request.ReportZones`). The suite uses in-memory zoned storage. go-ublk zoned devices exclude zero copy and unprivileged mode.