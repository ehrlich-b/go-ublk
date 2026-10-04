---
title: "Zoned devices"
linkTitle: "Zoned devices"
description: "UBLK_F_ZONED: exposing a zoned block device, zone operations, REPORT_ZONES and zone append."
weight: 110
---

A zoned block device divides its capacity into fixed-size zones. Sequential zones must be written at their write pointer and reset before they are rewritten; conventional zones behave like an ordinary disk. SMR hard drives and NVMe ZNS SSDs work this way, and zoned-aware filesystems (f2fs, btrfs in zoned mode) and applications write to them directly.

{{< uapi "UBLK_F_ZONED" >}}, added in Linux 6.6, lets a ublk server present such a device. Typical uses are emulating zoned hardware for testing, and fronting a log-structured or append-only backend whose natural interface already looks like zones.

## Requirements

A zoned ublk device needs all of the following, or `ADD_DEV`, `SET_PARAMS` or `START_DEV` fails:

| Requirement | Where | If missing |
|---|---|---|
| `UBLK_F_ZONED` in the `ADD_DEV` flags | `ADD_DEV` | not a zoned device |
| A kernel built with `CONFIG_BLK_DEV_ZONED` | `ADD_DEV` | `-EINVAL` |
| `UBLK_F_USER_COPY` or `UBLK_F_SUPPORT_ZERO_COPY` | `ADD_DEV` | `-EINVAL` |
| `UBLK_PARAM_TYPE_ZONED` parameters, and only on a zoned device | `SET_PARAMS` | `-EINVAL` |
| `basic.chunk_sectors` set to the zone size, a power of two | `SET_PARAMS` | `-EINVAL` |
| `zoned.max_zone_append_sectors` non-zero | `SET_PARAMS` | `-EINVAL` |

Why user copy or zero copy: a zone append returns the sector it landed at, and the kernel takes it from `ublksrv_io_cmd.zone_append_lba`, which shares a union with `addr`. In copy mode `addr` must carry the next buffer, so there is no room. Automatic buffer registration alone does not qualify.

6.17 only checked that `chunk_sectors` was non-zero; 7.3-rc5 requires a power of two. The driver computes the zone count as `dev_sectors >> log2(chunk_sectors)` in every version, so a trailing partial zone is simply not counted. Make the capacity a whole number of zones.

## Zoned parameters

`struct ublk_param_zoned` is 32 bytes at offset 76 of `struct ublk_params` (see [Device parameters](/guide/parameters/)):

```c
struct ublk_param_zoned {
	__u32 max_open_zones;           /* 0 = no limit; must be <= number of zones */
	__u32 max_active_zones;         /* 0 = no limit; must be <= number of zones */
	__u32 max_zone_append_sectors;  /* largest zone append, 512-byte sectors; non-zero */
	__u8  reserved[20];
};
```

Zone size itself comes from `ublk_param_basic.chunk_sectors`, in 512-byte sectors like every other sector count in ublk.

## Starting a zoned device

`START_DEV` does more for a zoned device. After applying the limits, and before the disk becomes visible, the kernel revalidates the zone layout, and that means sending `REPORT_ZONES` requests to your server. If they fail, `START_DEV` fails.

Your queue threads therefore have to be serving while `START_DEV` is still in flight on the control ring. A server whose queues are already fetching in their own threads gets this for free. A single-threaded server that issues `START_DEV` synchronously and only then starts processing completions will deadlock.

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

READ, WRITE and FLUSH arrive as on any device. Writes to a sequential zone must start at its write pointer; the block layer orders writes per zone, but the server is the device, so it must track write pointers and fail a misplaced write rather than silently accept it.

On a device created without `UBLK_F_ZONED` the kernel fails zone requests itself; they never reach the server.

### Zone append

A zone append writes data at a zone's current write pointer and tells the caller where it went. The descriptor's `start_sector` is the start of the target zone, not the write position. Read the data as for a WRITE (with `pread()` on `/dev/ublkcN` in user-copy mode), write it at the zone's write pointer, advance the pointer, and commit with both the byte count and the landing sector:

```c
struct ublksrv_io_cmd *cmd = sqe_cmd(sqe);
cmd->q_id   = q_id;
cmd->tag    = tag;
cmd->result = nr_bytes;                /* or -errno */
cmd->zone_append_lba = landed_sector;  /* absolute, 512-byte sectors */
submit(UBLK_U_IO_COMMIT_AND_FETCH_REQ);
```

The kernel copies `zone_append_lba` into the request's sector before completing it. In user-copy mode `addr` must otherwise be 0 at commit; zone append is the one exception, because the union holds the LBA. With [batch I/O](/guide/batch-io/) the LBA travels in the element's `zone_lba` field, with `UBLK_BATCH_F_HAS_ZONE_LBA` set on the commit.

### REPORT_ZONES

Zone reports are driver-internal requests: no application issues them as I/O. The kernel creates one when someone asks for the zone layout (the `BLKREPORTZONE` ioctl, a filesystem mounting, the revalidation at `START_DEV`), allocates a zeroed buffer of up to `max_hw_sectors` bytes, and sends the server a descriptor with:

- `start_sector`: the first sector of the first zone to report;
- `nr_zones` (the union member that is `nr_sectors` for other ops): the most zones to report.

The server fills the request buffer with an array of `struct blk_zone` from `<linux/blkzoned.h>` (64 bytes each), one per zone starting at `start_sector`, and writes it into the request with `pwrite()` at offset 0 of the tag's user-copy position. To report fewer zones than asked, end the array with a zeroed entry; the kernel stops at the first zone whose `len` is 0. A large report may be split into several requests, each with its own `start_sector`.

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

Commit the number of bytes you wrote, or a negative errno. <!-- VERIFY: REPORT_ZONES result semantics in user-copy mode. 7.3-rc5 completes the request on any non-negative result; 6.17 passed the result to blk_update_request, so a count shorter than the kernel's report buffer requeued the remainder. Confirm what rublk or other zoned servers commit. -->

## A minimal zone model

The server is the zoned device, so it owns the state machine. For each sequential zone keep a write pointer and a condition:

| Operation | Effect on the zone |
|---|---|
| WRITE at `wp` | `wp += len`; `EMPTY` or `CLOSED` becomes `IMP_OPEN`; `FULL` when `wp` reaches the capacity |
| WRITE elsewhere | fail with an error |
| ZONE_APPEND | as WRITE at `wp`; return the old `wp` as the LBA |
| ZONE_OPEN / ZONE_CLOSE | `EXP_OPEN` / `CLOSED` (a closed empty zone returns to `EMPTY`) |
| ZONE_FINISH | `wp` = end of zone, `FULL` |
| ZONE_RESET | `wp` = zone start, `EMPTY`; the zone's data may be discarded |
| ZONE_RESET_ALL | reset every sequential zone |

Enforce `max_open_zones` and `max_active_zones` if you advertised them. Conventional zones (`BLK_ZONE_TYPE_CONVENTIONAL`, condition `BLK_ZONE_COND_NOT_WP`) accept writes anywhere and ignore zone operations.

The kernel's selftest server has no zoned target. The Rust `rublk` server has one and is a useful reference. <!-- VERIFY: that rublk ships a zoned target, and its name on the command line -->

## go-ublk

go-ublk does not support zoned devices yet. `DeviceParams.EnableZoned` exists but is not wired to anything: it sets no flag and sends no zoned parameters. Zoned support also needs user copy, which go-ublk does not implement either. See the [roadmap](/go-ublk/roadmap/).
