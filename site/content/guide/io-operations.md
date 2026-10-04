---
title: "I/O operations and flags"
linkTitle: "I/O operations & flags"
description: "Every UBLK_IO_OP_* a server can receive, the per-request flags, how to report results, and the durability contract behind FLUSH and FUA."
weight: 40
---

The low eight bits of a descriptor's `op_flags` are the operation; bits 8 to 31 are flags. Which operations a device ever receives is decided by what it advertised in [`SET_PARAMS`](/guide/parameters/): the block layer only sends flushes to a device with a volatile write cache, discards to one with a discard limit, and so on. This chapter lists what can arrive, what each one means, and how to answer it.

```c
unsigned op    = desc->op_flags & 0xff;   /* ublksrv_get_op()    */
unsigned flags = desc->op_flags >> 8;     /* ublksrv_get_flags() returns them shifted down */
```

Note the shift: `ublksrv_get_flags()` returns `op_flags >> 8`, while the `UBLK_IO_F_*` constants are defined in their `op_flags` positions (`UBLK_IO_F_FUA` is `1 << 13`). Test `desc->op_flags & UBLK_IO_F_FUA`, or shift the constant.

## Operations

| Op | Value | Since | When the kernel sends it | Data |
|---|---|---|---|---|
| `UBLK_IO_OP_READ` | 0 | 6.0 | Always | Server fills `nr_sectors << 9` bytes |
| `UBLK_IO_OP_WRITE` | 1 | 6.0 | Unless the device is read-only | Server consumes `nr_sectors << 9` bytes |
| `UBLK_IO_OP_FLUSH` | 2 | 6.0 | Only with `UBLK_ATTR_VOLATILE_CACHE` | None; `nr_sectors` is 0 |
| `UBLK_IO_OP_DISCARD` | 3 | 6.0 | Only if `max_discard_sectors` > 0 | None; a range |
| `UBLK_IO_OP_WRITE_SAME` | 4 | 6.0 | Never on current kernels | |
| `UBLK_IO_OP_WRITE_ZEROES` | 5 | 6.0 | Only if `max_write_zeroes_sectors` > 0 | None; a range |
| `UBLK_IO_OP_ZONE_OPEN` | 10 | 6.6 | [Zoned](/guide/zoned/) devices | None |
| `UBLK_IO_OP_ZONE_CLOSE` | 11 | 6.6 | Zoned devices | None |
| `UBLK_IO_OP_ZONE_FINISH` | 12 | 6.6 | Zoned devices | None |
| `UBLK_IO_OP_ZONE_APPEND` | 13 | 6.6 | Zoned devices | Like a write; the server returns where it wrote |
| `UBLK_IO_OP_ZONE_RESET_ALL` | 14 | 6.6 | Zoned devices | None |
| `UBLK_IO_OP_ZONE_RESET` | 15 | 6.6 | Zoned devices | None |
| `UBLK_IO_OP_REPORT_ZONES` | 18 | 6.6 | Zoned devices, from the driver itself | Server writes `struct blk_zone` entries; `nr_zones` replaces `nr_sectors` |

`WRITE_SAME` is in the UAPI because it existed in the block layer when ublk was merged; the block layer dropped its write-same operation in 5.18, before ublk merged in 6.0, and the driver maps nothing to it. Operations the driver does not translate, such as secure erase, fail in the kernel and never reach the server.

### READ and WRITE

Byte offset `start_sector << 9`, length `nr_sectors << 9`, both multiples of the logical block size and the length at most `max_sectors << 9`. In the default copy mode the data is in the tag's buffer: placed there by the kernel before delivery for a write, expected there at commit for a read.

A write's data must be consumed (or copied) before you commit, because the commit hands the buffer back for the tag's next request. A read's buffer must be filled completely: the kernel copies the number of bytes you commit, and with user copy or zero copy, a read committed with fewer bytes than it claims would expose stale kernel memory, which is why those modes require a trusted, privileged server.

### FLUSH

A flush asks that every write the device has **completed** before the flush was issued be on stable storage when the flush completes. It does not cover writes still in flight alongside it, and it carries no range. A backend over a file implements it as `fdatasync` (or `fsync`); over a network store, as whatever makes acknowledged writes durable; over RAM, as nothing.

The block layer only issues flushes to a device that claims a volatile write cache. Claim one if, and only if, a completed write can be lost by a power failure; see [the durability contract](#the-durability-contract) below.

### DISCARD

A hint that the range's contents are no longer needed. The server may deallocate the space (punch a hole, issue TRIM below it, drop the blocks) or ignore it. After a discard, reads of the range may return the old data, zeros, or anything else: Linux does not promise zeroing for discard.

The range can be far larger than `max_io_buf_bytes`, up to `max_discard_sectors`. Do not size a buffer from a discard's length; there is no data. The kernel only sends single-range discards (the parameters require `max_discard_segments` to be 1).

### WRITE_ZEROES

The range must read back as zeros afterwards. That is a guarantee, unlike discard. With `UBLK_IO_F_NOUNMAP` set, the space must also stay allocated (write zeros, or use a zeroing primitive that keeps the blocks provisioned). Without it the server may deallocate as long as reads return zeros, for example by punching a hole in a sparse file.

As with discard, the length is a range and can be gigabytes.

## Flags

| Flag | Bit | Meaning for the server |
|---|---|---|
| `UBLK_IO_F_FAILFAST_DEV` | 8 | The submitter prefers a fast failure over device-level retries |
| `UBLK_IO_F_FAILFAST_TRANSPORT` | 9 | The same, for transport errors (a multipath layer above can try another path) |
| `UBLK_IO_F_FAILFAST_DRIVER` | 10 | The same, for driver-level retries |
| `UBLK_IO_F_META` | 11 | Filesystem metadata. A priority hint; no semantic change |
| `UBLK_IO_F_FUA` | 13 | Forced unit access: this write must be durable before you commit it. Only sent if the device advertises `UBLK_ATTR_FUA` |
| `UBLK_IO_F_NOUNMAP` | 15 | On `WRITE_ZEROES`: keep the range allocated |
| `UBLK_IO_F_SWAP` | 16 | Swap I/O. Memory reclaim may be waiting on it |
| `UBLK_IO_F_NEED_REG_BUF` | 17 | `UBLK_F_AUTO_BUF_REG` fallback: automatic buffer registration failed; register it yourself (6.16+) |
| `UBLK_IO_F_INTEGRITY` | 18 | The request carries an [integrity](/guide/integrity/) buffer (7.0+) |
| `UBLK_IO_F_SHMEM_ZC` | 19 | `addr` encodes a [shared-memory buffer](/guide/data-copy/) index and offset (7.1+) |

The first seven are translations of block-layer `REQ_*` flags and exist since 6.0. The failfast flags are hints: a server with retry logic (a network backend reconnecting, say) can skip it and fail quickly when they are set.

`UBLK_IO_F_SWAP` deserves a warning. The kernel documentation gives no guidance on ublk-backed swap and the driver does nothing special for it beyond setting this flag. As with any userspace block device, if the server must allocate memory to complete a swap-out while the system is short of memory, the system can deadlock on itself. Servers that support swap need preallocated memory and a backend that does not allocate on the I/O path.

## Reporting results

The commit's `result` field:

| Operation | Success | Partial | Failure |
|---|---|---|---|
| READ, WRITE, ZONE_APPEND | `nr_sectors << 9` | avoid: in 6.17 a short count completes that many bytes and the rest is re-issued; in 7.3-rc5 only a copy-mode read is completed partially, and other short results complete the whole request. A read that returns 0 bytes becomes `-EIO` | negative errno |
| FLUSH, DISCARD, WRITE_ZEROES, zone management | any value ≥ 0 | not applicable | negative errno |
| REPORT_ZONES | bytes of zone report written (`nr_zones × 64`); never 0 on kernels before 7.3, where it re-dispatches the request | | negative errno |

For range operations, do not echo the length back: `nr_sectors << 9` for a 4 GiB discard does not fit in the signed 32-bit result, and a negative result is an error.

The errno reaches the application as a block status. The useful ones:

| errno | Block status | What the application typically sees |
|---|---|---|
| `-EIO` | `BLK_STS_IOERR` | `EIO`; filesystems may remount read-only or shut down |
| `-ENOSPC` | `BLK_STS_NOSPC` | `ENOSPC`, for thin-provisioned backends that ran out of space |
| `-EOPNOTSUPP` | `BLK_STS_NOTSUPP` | `EOPNOTSUPP`; for example, an unsupported discard |
| `-ETIMEDOUT` | `BLK_STS_TIMEOUT` | `ETIMEDOUT` |
| `-ENOLINK` | `BLK_STS_TRANSPORT` | a transport error, which multipath above can retry elsewhere |
| `-EAGAIN` | `BLK_STS_AGAIN` | for non-blocking submitters, a retryable failure |
| `-EILSEQ` | `BLK_STS_PROTECTION` | `EILSEQ`, an integrity (protection information) error |
| `-ENODATA` | `BLK_STS_MEDIUM` | a medium error |
| `-EREMOTEIO` | `BLK_STS_TARGET` | a critical target error |
| `-EINVAL` | `BLK_STS_INVAL` | 6.11 and later; `BLK_STS_IOERR` before |

Every release since 6.0 passes a negative result through `errno_to_blk_status`, and any errno not in its table becomes `BLK_STS_IOERR`.

## The durability contract

Two attributes in `SET_PARAMS` tell the block layer what a completed write means, and they decide which flush and FUA operations the server ever sees:

| Device advertises | Flushes | FUA writes | The server promises |
|---|---|---|---|
| neither | completed by the block layer, never sent | the FUA bit is dropped | **every committed write is already durable** |
| `UBLK_ATTR_VOLATILE_CACHE` | sent as `UBLK_IO_OP_FLUSH` | emulated: write, then a flush | a flush makes all previously completed writes durable |
| `UBLK_ATTR_VOLATILE_CACHE` and `UBLK_ATTR_FUA` | sent | sent with `UBLK_IO_F_FUA` | both of the above, and a FUA write is durable when committed |

`UBLK_ATTR_FUA` without a volatile cache has no effect.

The two possible mistakes are not symmetric. Advertising a cache you do not have costs a no-op flush round trip now and then. Not advertising a cache you do have means the kernel never asks you to flush, filesystems believe their journal commits are on disk when they are only in your page cache or your network buffer, and a power failure loses acknowledged data with no error anywhere. When in doubt, advertise the cache and implement flush.

Advertise FUA only if you honor `UBLK_IO_F_FUA` on every write that carries it. A backend that can make a single write durable more cheaply than a full flush (an `O_DSYNC` write to one file, a write with a sync flag over the network) gains from FUA; one that cannot should leave it off and let the block layer emulate it with flushes, which are correct, just slower.
