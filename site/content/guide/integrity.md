---
title: "Integrity metadata"
linkTitle: "Integrity metadata"
description: "UBLK_F_INTEGRITY: per-request protection information and metadata buffers, the integrity parameters, and how a server reads and writes them."
weight: 120
---

Block devices can store metadata per data interval: 8/16-byte protection-information tuples with checksum/reference tag (T10 DIF/DIX, NVMe), or opaque bytes. Linux attaches these separately from data as each bio's integrity payload.

{{< uapi "UBLK_F_INTEGRITY" >}} (7.0) describes the format at SET_PARAMS, receives metadata with WRITEs, and supplies it with READs. Details follow the 7.3-rc5 driver/selftest server and may change.

## Requirements

- Request UBLK_F_INTEGRITY at ADD_DEV; CONFIG_BLK_DEV_INTEGRITY is required. Check negotiated flags or GET_FEATURES.
- Also request UBLK_F_USER_COPY, or ADD_DEV returns `-EINVAL`. Metadata requires pread/pwrite on `/dev/ublkcN`; [copy/zero-copy modes](/guide/data-copy/) cannot carry it.
- SET_PARAMS needs UBLK_PARAM_TYPE_INTEGRITY (`1 << 6`); sending it without the feature returns `-EINVAL`.

## Parameters

`struct ublk_param_integrity`: 16 bytes at ublk_params offset 136, mirroring the block-layer profile with `<linux/fs.h>` LBMD_PI_* constants:

| Field | Type | Meaning | Rule (7.3-rc5) |
|---|---|---|---|
| `flags` | `__u32` | `LBMD_PI_CAP_INTEGRITY`: the device stores and checks PI. `LBMD_PI_CAP_REFTAG`: the reference tag is meaningful | No other bits; `REFTAG` needs a checksum type |
| `max_integrity_segments` | `__u16` | Most integrity segments per request | 0 means no limit |
| `interval_exp` | `__u8` | log2 of the protection interval in bytes | From 9 to `logical_bs_shift` |
| `metadata_size` | `__u8` | Metadata bytes per interval | Must be non-zero |
| `pi_offset` | `__u8` | Offset of the PI tuple within each interval's metadata | `pi_offset + tuple size <= metadata_size` |
| `csum_type` | `__u8` | `LBMD_PI_CSUM_NONE`, `_IP`, `_CRC16_T10DIF`, `_CRC64_NVME` | One of these four |
| `tag_size` | `__u8` | Bytes of tag space available to applications in each tuple (the application tag, plus reference or storage-tag bytes the format does not check), reported as `integrity/tag_size` | Passed through to the block layer |
| `pad[5]` | | | |

PI tuple sizes: NONE 0, IP/CRC16 T10-DIF 8, CRC64 NVMe 16 bytes. START_DEV builds the disk profile, mapping LBMD_PI_CAP_INTEGRITY/REFTAG to device-capable/reference-tag flags.

NVMe-style T10-DIF: 8 PI bytes per 512-byte sector:

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

Check descriptor op_flags for UBLK_IO_F_INTEGRITY (`1 << 18`); some requests lack metadata.

Metadata length is `(nr_sectors << 9 >> interval_exp) * metadata_size`. OR UBLKSRV_IO_INTEGRITY_FLAG (`1ULL << 62`) into the user-copy position. pread retrieves WRITE metadata; pwrite supplies READ metadata. Condensed selftest code:

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

Missing UBLK_F_INTEGRITY or an offset beyond metadata returns `-EINVAL`. Data and metadata copies are independent, permitting either order and partial transfers.

Persist metadata with its interval's data and return it unchanged. The block layer or application supplies PI. With a checksum, write_generate/read_verify default to 1 under `/sys/block/ublkbN/integrity/`; bad READ PI returns EILSEQ while verification is enabled. Applications can exchange metadata through io_uring PI attributes (6.14+); FS_IOC_GETLBMD_CAP (6.17+) reports its format.

## Testing

kublk supports integrity through user copy: --metadata_size, --pi_offset, --csum_type ip|t10dif|nvme, --tag_size, --integrity_capable (LBMD_PI_CAP_INTEGRITY), and --integrity_reftag (LBMD_PI_CAP_REFTAG). It requires user copy and --metadata_size for the other options.

## go-ublk

DeviceParams.Integrity (7.0+) configures MetadataSize/IntervalSize, Checksum (IntegrityCsumIP, IntegrityCsumCRC16 T10-DIF, IntegrityCsumCRC64NVMe, or none), RefTag, PIOffset, and TagSize. User copy enables automatically. Backends implement IntegrityBackend.ReadIntegrity/WriteIntegrity; Handlers receive Request.Integrity.

Return 0xff metadata for unwritten intervals: the all-ones T10 application tag disables checking, allowing READs before data exists. Integrity excludes zero copy and unprivileged devices.