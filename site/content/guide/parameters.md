---
title: "Device parameters"
linkTitle: "Device parameters"
description: "struct ublk_params field by field: basic limits, discard, devt, zoned, DMA alignment, segments and integrity, with the kernel's validation rules."
weight: 50
---

`UBLK_U_CMD_SET_PARAMS` gives the kernel everything it needs to build the block device at `START_DEV`: capacity, block sizes, request-size limits, attributes, and optional blocks for discard, zones, DMA alignment, segments and integrity. The kernel turns them into the disk's `queue_limits`, which is what you later see under `/sys/block/ublkbN/queue/`.

## Layout and versioning

```c
struct ublk_params {
    __u32 len;     /* bytes of this structure the caller is passing */
    __u32 types;   /* UBLK_PARAM_TYPE_* bits: which blocks below are valid */
    struct ublk_param_basic     basic;      /* offset   8, 32 bytes */
    struct ublk_param_discard   discard;    /* offset  40, 20 bytes */
    struct ublk_param_devt      devt;       /* offset  60, 16 bytes */
    struct ublk_param_zoned     zoned;      /* offset  76, 32 bytes */
    struct ublk_param_dma_align dma;        /* offset 108,  8 bytes */
    struct ublk_param_segment   seg;        /* offset 120, 16 bytes (4 bytes padding before) */
    struct ublk_param_integrity integrity;  /* offset 136, 16 bytes */
};                                          /* 152 bytes as of 7.0 */
```

| Type bit | Block | Since | Settable |
|---|---|---|---|
| `UBLK_PARAM_TYPE_BASIC` (1 << 0) | `basic` | 6.0 | yes, and mandatory |
| `UBLK_PARAM_TYPE_DISCARD` (1 << 1) | `discard` | 6.0 | yes |
| `UBLK_PARAM_TYPE_DEVT` (1 << 2) | `devt` | 6.3 | no: read-only, filled by `GET_PARAMS` |
| `UBLK_PARAM_TYPE_ZONED` (1 << 3) | `zoned` | 6.6 | yes, with `UBLK_F_ZONED` |
| `UBLK_PARAM_TYPE_DMA_ALIGN` (1 << 4) | `dma` | 6.15 | yes |
| `UBLK_PARAM_TYPE_SEGMENT` (1 << 5) | `seg` | 6.15 | yes |
| `UBLK_PARAM_TYPE_INTEGRITY` (1 << 6) | `integrity` | 7.0 | yes, with `UBLK_F_INTEGRITY` |

The structure only grows at the end, so a server compiled against an older header sends a smaller `len` and a newer kernel reads what it sent. The other direction is the trap: the kernel copies at most its own `sizeof(struct ublk_params)` and then clears every `types` bit it does not know, **without an error**. On a 6.14 kernel, a `SET_PARAMS` with `UBLK_PARAM_TYPE_SEGMENT` succeeds and the segment limits are silently ignored. If a block matters, read the parameters back with `GET_PARAMS` and check `types`.

`len` must be non-zero, no larger than the control header's `len`, and `types` must be non-zero. `SET_PARAMS` is accepted only before the disk exists; after `START_DEV` it fails with `EACCES`. A validation failure returns `EINVAL` and clears every parameter, including ones that were valid.

## basic

```c
struct ublk_param_basic {
    __u32 attrs;               /* UBLK_ATTR_* */
    __u8  logical_bs_shift;
    __u8  physical_bs_shift;
    __u8  io_opt_shift;
    __u8  io_min_shift;
    __u32 max_sectors;
    __u32 chunk_sectors;
    __u64 dev_sectors;
    __u64 virt_boundary_mask;
};
```

| Field | Meaning | Rule |
|---|---|---|
| `logical_bs_shift` | log2 of the logical block size: the smallest addressable unit | 9 to `PAGE_SHIFT` (512 bytes to the page size) |
| `physical_bs_shift` | log2 of the physical block size: the unit the backend writes atomically or efficiently | at least `logical_bs_shift` |
| `io_min_shift`, `io_opt_shift` | log2 of the minimum and optimal I/O sizes; hints exported to filesystems and tools | none (an `io_min_shift` below the physical block size is raised to it; `io_opt_shift` 0 is reported as no optimal size from 6.13, and as 1 byte in `optimal_io_size` before) |
| `max_sectors` | largest request, in 512-byte sectors | at most `max_io_buf_bytes >> 9` from `ADD_DEV` |
| `chunk_sectors` | boundary requests must not cross, in sectors; 0 for none | required (the zone size) on zoned devices |
| `dev_sectors` | capacity in 512-byte sectors | should be a multiple of the logical block size |
| `virt_boundary_mask` | requests' scatter-gather segments must not cross this boundary (NVMe-style PRP constraints); 0 for none | |
| `attrs` | `UBLK_ATTR_*`, below | |

`dev_sectors` and `max_sectors` count 512-byte sectors even on a 4Kn device. A 1 GiB device with 4096-byte blocks has `dev_sectors = 2097152` and `logical_bs_shift = 12`.

### Attributes

| Attribute | Effect |
|---|---|
| `UBLK_ATTR_READ_ONLY` (1 << 0) | The disk is created read-only (`set_disk_ro`); writes fail in the block layer |
| `UBLK_ATTR_ROTATIONAL` (1 << 1) | Marks the device rotational (`/sys/block/ublkbN/queue/rotational` = 1), which changes I/O scheduler and filesystem heuristics |
| `UBLK_ATTR_VOLATILE_CACHE` (1 << 2) | The device has a write-back cache: the kernel will send `FLUSH` |
| `UBLK_ATTR_FUA` (1 << 3) | The device honors per-write FUA. Ignored without `UBLK_ATTR_VOLATILE_CACHE` |

The last two define the [durability contract](/guide/io-operations/). If a completed write can be lost in a power failure, set `UBLK_ATTR_VOLATILE_CACHE` and implement flush.

## discard

```c
struct ublk_param_discard {
    __u32 discard_alignment;
    __u32 discard_granularity;
    __u32 max_discard_sectors;
    __u32 max_write_zeroes_sectors;
    __u16 max_discard_segments;
    __u16 reserved0;
};
```

This one block enables two independent operations. `max_discard_sectors` > 0 enables `UBLK_IO_OP_DISCARD`; `max_write_zeroes_sectors` > 0 enables `UBLK_IO_OP_WRITE_ZEROES`. Without the block, or with both at zero, the block layer reports neither, and `blkdiscard` fails with "operation not supported".

| Field | Meaning | Rule |
|---|---|---|
| `discard_granularity` | the backend's allocation unit in bytes; discards are aligned and sized to it | non-zero whenever the block is present, even for write-zeroes only |
| `discard_alignment` | offset in bytes of the first granularity-aligned unit | |
| `max_discard_sectors` | largest discard in 512-byte sectors; 0 disables discard | `0xffffffff` means "as large as the block layer allows" |
| `max_write_zeroes_sectors` | largest write-zeroes; 0 disables it | |
| `max_discard_segments` | ranges per discard request | must be exactly 1 when discard is enabled |

Advertise only what the backend implements. A server that advertises discard and then drops discards is honest (discard is advisory); one that advertises write-zeroes and does not zero the range corrupts data.

## devt

```c
struct ublk_param_devt {
    __u32 char_major, char_minor;   /* /dev/ublkcN */
    __u32 disk_major, disk_minor;   /* /dev/ublkbN; 0 until START_DEV */
};
```

Read-only. Including `UBLK_PARAM_TYPE_DEVT` in a `SET_PARAMS` fails with `EINVAL`. `GET_PARAMS` always fills it. Useful for finding the device nodes inside a container or without udev.

## zoned

```c
struct ublk_param_zoned {
    __u32 max_open_zones;
    __u32 max_active_zones;
    __u32 max_zone_append_sectors;
    __u8  reserved[20];
};
```

Valid only on a device created with `UBLK_F_ZONED`, and required on one. `max_zone_append_sectors` must be non-zero; the open and active limits may not exceed the number of zones (capacity divided by `basic.chunk_sectors`). See [Zoned devices](/guide/zoned/).

## dma_align

```c
struct ublk_param_dma_align {
    __u32 alignment;   /* a mask: required alignment minus one */
    __u8  pad[4];
};
```

The memory alignment the block layer must guarantee for request buffers, as a mask (`alignment + 1` must be a power of two, and `alignment` smaller than the page size). Without the block, the kernel uses a mask of 3 (4-byte alignment). It matters for [zero copy](/guide/data-copy/), where request pages are handed straight to the backend: a backend file opened `O_DIRECT` typically needs 512-byte alignment (mask 511).

## segment

```c
struct ublk_param_segment {
    __u64 seg_boundary_mask;   /* segments must not cross (mask + 1)-byte boundaries */
    __u32 max_segment_size;
    __u16 max_segments;
    __u8  pad[2];
};
```

Scatter-gather limits for the request, mostly to make zero-copy requests acceptable to the backend without being split again. `seg_boundary_mask + 1` must be a power of two and at least 4096 (`UBLK_MIN_SEGMENT_SIZE`); `max_segment_size` must be at least 4096. The header warns that setting any of the three to 0 is undefined. Without the block, the kernel allows up to 65535 segments of unlimited size.

## integrity

```c
struct ublk_param_integrity {
    __u32 flags;                   /* LBMD_PI_CAP_* from linux/fs.h */
    __u16 max_integrity_segments;  /* 0 means no limit */
    __u8  interval_exp;
    __u8  metadata_size;           /* must be non-zero */
    __u8  pi_offset;
    __u8  csum_type;               /* LBMD_PI_CSUM_* */
    __u8  tag_size;
    __u8  pad[5];
};
```

Valid only with `UBLK_F_INTEGRITY` (Linux 7.0). Describes per-block metadata and protection information; see [Integrity metadata](/guide/integrity/).

## A worked example

A 64 GiB device with 4096-byte logical and physical blocks, 1 MiB maximum requests, a write-back cache, discard and write-zeroes on a 4 KiB allocation unit:

```c
struct ublk_params p = {
    .len   = sizeof(p),
    .types = UBLK_PARAM_TYPE_BASIC | UBLK_PARAM_TYPE_DISCARD,
    .basic = {
        .attrs              = UBLK_ATTR_VOLATILE_CACHE,
        .logical_bs_shift   = 12,
        .physical_bs_shift  = 12,
        .io_min_shift       = 12,
        .io_opt_shift       = 20,
        .max_sectors        = (1 << 20) >> 9,             /* 2048 */
        .dev_sectors        = (64ULL << 30) >> 9,         /* 134217728 */
    },
    .discard = {
        .discard_granularity      = 4096,
        .max_discard_sectors      = 0xffffffff,
        .max_write_zeroes_sectors = 0xffffffff,
        .max_discard_segments     = 1,
    },
};
```

`max_sectors` must not exceed what `ADD_DEV` returned in `max_io_buf_bytes`, so compute it from the negotiated value, not the requested one.
