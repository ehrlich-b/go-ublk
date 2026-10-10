---
title: "Device parameters"
linkTitle: "Device parameters"
description: "struct ublk_params field by field: basic limits, discard, devt, zoned, DMA alignment, segments and integrity, with the kernel's validation rules."
weight: 50
---

`UBLK_U_CMD_SET_PARAMS` supplies START_DEV capacity, block/request sizes, attributes, and discard/zoned/DMA/segment/integrity limits. These become queue_limits exposed at `/sys/block/ublkbN/queue/`.

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

The structure grows at its end, permitting older callers' smaller len. Older kernels copy only their sizeof(ublk_params) and silently mask unknown types: 6.14 accepts UBLK_PARAM_TYPE_SEGMENT without its limits. Verify required blocks through GET_PARAMS.

Require non-zero len/types, with len no greater than the control header's. SET_PARAMS after START_DEV returns EACCES. Invalid parameters return EINVAL and clear every block.

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
| `dev_sectors` | capacity in 512-byte sectors | must be a multiple of logical block size / 512 |
| `virt_boundary_mask` | requests' scatter-gather segments must not cross this boundary (NVMe-style PRP constraints); 0 for none | |
| `attrs` | `UBLK_ATTR_*`, below | |

dev_sectors/max_sectors always count 512-byte sectors. A 1 GiB, 4096-byte-block device uses dev_sectors = 2097152 and logical_bs_shift = 12.

### Attributes

| Attribute | Effect |
|---|---|
| `UBLK_ATTR_READ_ONLY` (1 << 0) | The disk is created read-only (`set_disk_ro`); writes fail in the block layer |
| `UBLK_ATTR_ROTATIONAL` (1 << 1) | Marks the device rotational (`/sys/block/ublkbN/queue/rotational` = 1), which changes I/O scheduler and filesystem heuristics |
| `UBLK_ATTR_VOLATILE_CACHE` (1 << 2) | The device has a write-back cache: the kernel will send `FLUSH` |
| `UBLK_ATTR_FUA` (1 << 3) | The device honors per-write FUA. Ignored without `UBLK_ATTR_VOLATILE_CACHE` |

For [durability](/guide/io-operations/), set UBLK_ATTR_VOLATILE_CACHE and implement flush whenever power loss could erase completed writes.

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

Non-zero max_discard_sectors enables UBLK_IO_OP_DISCARD; non-zero max_write_zeroes_sectors enables UBLK_IO_OP_WRITE_ZEROES. Without the block or with both zero, neither is advertised; blkdiscard returns "operation not supported".

| Field | Meaning | Rule |
|---|---|---|
| `discard_granularity` | the backend's allocation unit in bytes; discards are aligned and sized to it | non-zero whenever the block is present, even for write-zeroes only |
| `discard_alignment` | offset in bytes of the first granularity-aligned unit | |
| `max_discard_sectors` | largest discard in 512-byte sectors; 0 disables discard | `0xffffffff` means "as large as the block layer allows" |
| `max_write_zeroes_sectors` | largest write-zeroes; 0 disables it | |
| `max_discard_segments` | ranges per discard request | must be exactly 1 when discard is enabled |

Discard may be ignored; advertised write-zeroes must zero data. Cap both limits at UINT32_MAX >> 9, rounded down to logical-block sectors, to avoid pre-6.11 truncation (see [kernel bugs](/guide/kernel-bugs/)).

## devt

```c
struct ublk_param_devt {
    __u32 char_major, char_minor;   /* /dev/ublkcN */
    __u32 disk_major, disk_minor;   /* /dev/ublkbN; 0 until START_DEV */
};
```

SET_PARAMS with UBLK_PARAM_TYPE_DEVT returns EINVAL; GET_PARAMS always fills it. Use the numbers to find nodes in containers or without udev.

## zoned

```c
struct ublk_param_zoned {
    __u32 max_open_zones;
    __u32 max_active_zones;
    __u32 max_zone_append_sectors;
    __u8  reserved[20];
};
```

Required with UBLK_F_ZONED and invalid otherwise. max_zone_append_sectors must be non-zero; open/active limits cannot exceed capacity / basic.chunk_sectors. See [zoned devices](/guide/zoned/).

## dma_align

```c
struct ublk_param_dma_align {
    __u32 alignment;   /* a mask: required alignment minus one */
    __u8  pad[4];
};
```

Request-buffer alignment mask: alignment + 1 must be a power of two, alignment below page size. Default mask 3 means 4 bytes. For [zero copy](/guide/data-copy/), match the backend: O_DIRECT files typically need 512-byte alignment (mask 511).

## segment

```c
struct ublk_param_segment {
    __u64 seg_boundary_mask;   /* segments must not cross (mask + 1)-byte boundaries */
    __u32 max_segment_size;
    __u16 max_segments;
    __u8  pad[2];
};
```

These scatter-gather limits avoid re-splitting zero-copy requests. seg_boundary_mask + 1 must be a power of two >= 4096 (UBLK_MIN_SEGMENT_SIZE); max_segment_size >= 4096. Zero for any field is undefined. Without this block, defaults allow 65535 unlimited-size segments.

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

Per-block protection metadata for UBLK_F_INTEGRITY (7.0); see [integrity](/guide/integrity/).

## A worked example

64 GiB, 4096-byte logical/physical blocks, 1 MiB maximum requests, write-back cache, discard/zeroes, 4 KiB allocation units:

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
        .max_discard_sectors      = 0x7ffff8, /* (UINT32_MAX >> 9) rounded to 4 KiB */
        .max_write_zeroes_sectors = 0x7ffff8,
        .max_discard_segments     = 1,
    },
};
```

Compute max_sectors from ADD_DEV's returned max_io_buf_bytes, not the requested size.