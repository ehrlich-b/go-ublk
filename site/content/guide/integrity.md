---
title: "Integrity metadata"
linkTitle: "Integrity metadata"
description: "UBLK_F_INTEGRITY: per-request protection information and metadata buffers, the integrity parameters, and how a server reads and writes them."
weight: 120
---

Some block devices store a few bytes of metadata alongside every data interval: an 8- or 16-byte protection information (PI) tuple with a checksum and reference tag, as in T10 DIF/DIX and NVMe end-to-end protection, or opaque per-block metadata. Linux carries that metadata through the block layer as an integrity payload attached to each bio, separate from the data.

{{< uapi "UBLK_F_INTEGRITY" >}}, added in Linux 7.0, lets a ublk server expose such a device: it describes the metadata format at `SET_PARAMS` time, receives metadata with WRITEs and supplies it with READs. This chapter was checked against the 7.3-rc5 driver and the kernel's selftest server; the feature is young, so expect details to move.

## Requirements

- `UBLK_F_INTEGRITY` at `ADD_DEV`. The kernel only offers the flag when built with `CONFIG_BLK_DEV_INTEGRITY`, so check the returned flags or `GET_FEATURES`.
- `UBLK_F_USER_COPY` as well. Integrity buffers can only be reached through `pread`/`pwrite` on `/dev/ublkcN`; `ADD_DEV` rejects `UBLK_F_INTEGRITY` without user copy with `-EINVAL`. Copy mode and zero copy have no path for the metadata. See [Data copy modes](/guide/data-copy/).
- `UBLK_PARAM_TYPE_INTEGRITY` (`1 << 6`) parameters in `SET_PARAMS`. Sending them on a device without `UBLK_F_INTEGRITY` is `-EINVAL`.

## Parameters

`struct ublk_param_integrity` is 16 bytes at offset 136 of `struct ublk_params`. Its fields mirror the block layer's integrity profile and use the `LBMD_PI_*` constants from `<linux/fs.h>`:

| Field | Type | Meaning | Rule (7.3-rc5) |
|---|---|---|---|
| `flags` | `__u32` | `LBMD_PI_CAP_INTEGRITY`: the device stores and checks PI. `LBMD_PI_CAP_REFTAG`: the reference tag is meaningful | No other bits; `REFTAG` needs a checksum type |
| `max_integrity_segments` | `__u16` | Most integrity segments per request | 0 means no limit |
| `interval_exp` | `__u8` | log2 of the protection interval in bytes | From 9 to `logical_bs_shift` |
| `metadata_size` | `__u8` | Metadata bytes per interval | Must be non-zero |
| `pi_offset` | `__u8` | Offset of the PI tuple within each interval's metadata | `pi_offset + tuple size <= metadata_size` |
| `csum_type` | `__u8` | `LBMD_PI_CSUM_NONE`, `_IP`, `_CRC16_T10DIF`, `_CRC64_NVME` | One of these four |
| `tag_size` | `__u8` | Size of the tag field in the PI tuple <!-- VERIFY: exact meaning of blk_integrity.tag_size (application tag vs storage tag size) --> | Passed through to the block layer |
| `pad[5]` | | | |

The checksum type fixes the PI tuple size: 0 bytes for `NONE` (metadata without PI), 8 for IP and CRC16 T10-DIF, 16 for CRC64 NVMe. At `START_DEV` the kernel turns these values into the disk's integrity profile: `LBMD_PI_CAP_INTEGRITY` becomes the block layer's device-capable flag and `LBMD_PI_CAP_REFTAG` its reference-tag flag.

A typical NVMe-style format with 8 bytes of T10-DIF PI per 512-byte sector:

```c
params.types |= UBLK_PARAM_TYPE_INTEGRITY;
params.integrity = (struct ublk_param_integrity){
	.flags         = LBMD_PI_CAP_INTEGRITY | LBMD_PI_CAP_REFTAG,
	.interval_exp  = 9,                       /* one tuple per 512 bytes */
	.metadata_size = 8,
	.pi_offset     = 0,
	.csum_type     = LBMD_PI_CSUM_CRC16_T10DIF,
};
```

## Per-request metadata

A request that carries an integrity payload has `UBLK_IO_F_INTEGRITY` (`1 << 18`) set in its descriptor's `op_flags`. Not every request does; check the flag rather than assuming.

The metadata length follows from the data length: one `metadata_size` chunk per protection interval, so `(nr_sectors << 9 >> interval_exp) * metadata_size` bytes. Address it exactly like the data, with `UBLKSRV_IO_INTEGRITY_FLAG` (`1ULL << 62`) ORed into the user-copy position. The direction rules are the data's: `pread` takes a WRITE's metadata out of the request, `pwrite` puts a READ's metadata into it. This is the selftest server's code, lightly condensed:

```c
if (desc->op_flags & UBLK_IO_F_INTEGRITY) {
	size_t len = ((size_t)desc->nr_sectors << 9 >> interval_exp) * metadata_size;
	__u64 pos  = ublk_user_copy_pos(q_id, tag, 0) | UBLKSRV_IO_INTEGRITY_FLAG;

	if (op == UBLK_IO_OP_WRITE)
		n = pread(ublkc_fd, io->integrity_buf, len, pos);   /* store with the data */
	else if (op == UBLK_IO_OP_READ)
		n = pwrite(ublkc_fd, io->integrity_buf, len, pos);  /* return what was stored */
}
```

Using `UBLKSRV_IO_INTEGRITY_FLAG` on a device without `UBLK_F_INTEGRITY` fails with `-EINVAL`; an offset beyond the metadata length fails the same way. The data and metadata copies are independent, so a server can fetch them in either order and in pieces.

What the server owes the kernel is simple to state: persist each interval's metadata with its data, and return it unchanged on READ. Where the metadata comes from depends on the block layer and the application. For PI formats the block layer can generate tuples on write and verify them on read, controlled per disk under `/sys/block/ublkbN/integrity/`; applications can also supply their own metadata. A server that returns wrong PI on a READ will see the reader get an I/O error when verification is on. <!-- VERIFY: default write_generate/read_verify behavior for a ublk disk with a PI csum_type, and which userspace interfaces (io_uring PI attributes, others) let applications pass metadata to a ublk device -->

## Testing

The kernel's selftest server `kublk` implements integrity over its user-copy path. Its options map directly onto the parameters: `--metadata_size`, `--pi_offset`, `--csum_type ip|t10dif|nvme`, `--tag_size`, `--integrity_capable` (sets `LBMD_PI_CAP_INTEGRITY`) and `--integrity_reftag` (sets `LBMD_PI_CAP_REFTAG`). It refuses integrity without user copy, and refuses the other integrity options without `--metadata_size`.

## go-ublk

go-ublk does not support integrity metadata yet. It also lacks user copy, which integrity requires. See the [roadmap](/go-ublk/roadmap/).
