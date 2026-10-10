---
title: "I/O operations and flags"
linkTitle: "I/O operations & flags"
description: "Every UBLK_IO_OP_* a server can receive, the per-request flags, how to report results, and the durability contract behind FLUSH and FUA."
weight: 40
---

Descriptor `op_flags` bits 0-7 select the operation; 8-31 are flags. [`SET_PARAMS`](/guide/parameters/) determines delivered operations: flush requires a volatile cache, discard a non-zero limit.

```c
unsigned op    = desc->op_flags & 0xff;   /* ublksrv_get_op()    */
unsigned flags = desc->op_flags >> 8;     /* ublksrv_get_flags() returns them shifted down */
```

`ublksrv_get_flags()` shifts down by 8; `UBLK_IO_F_*` constants do not (`UBLK_IO_F_FUA = 1 << 13`). Test `desc->op_flags & UBLK_IO_F_FUA` or shift the constant too.

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

The UAPI retains WRITE_SAME, although the block layer removed it in 5.18, before ublk's 6.0 merge. The driver maps nothing to it. Untranslated operations, such as secure erase, fail before reaching the server.

### READ and WRITE

Offset is `start_sector << 9`, length `nr_sectors << 9`: both block-aligned, length at most `max_sectors << 9`. Copy mode delivers WRITE bytes in the tag buffer and expects READ bytes there at commit.

Consume or copy WRITE data before commit releases its buffer for reuse. Fill every claimed READ byte; user-copy/zero-copy servers that report unfilled bytes can expose stale kernel memory, so those modes require trusted privileged servers.

### FLUSH

FLUSH makes previously completed writes durable. It covers no range and excludes concurrent in-flight writes. File backends use fdatasync/fsync, network stores their durability operation, and RAM a no-op.

Flushes arrive only with a volatile cache: advertise one when completed writes can be lost on power failure. See [durability](#the-durability-contract).

### DISCARD

DISCARD marks unneeded data. Punch holes, issue TRIM, drop blocks, or ignore it; later reads may return old data, zeros, or anything else.

Ranges can exceed max_io_buf_bytes, up to max_discard_sectors; allocate no data buffer. Only single-range discards arrive (`max_discard_segments = 1`).

### WRITE_ZEROES

WRITE_ZEROES must leave zeros. With `UBLK_IO_F_NOUNMAP`, retain allocation through writes or a zeroing primitive; otherwise hole punching is allowed if reads return zeros.

Like discard, this is a potentially multi-gigabyte range.

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

The first seven flags translate block-layer REQ_* flags present since 6.0. Failfast hints can suppress server retries, such as network reconnection.

ublk swap has no documented special handling beyond `UBLK_IO_F_SWAP`. Allocating while completing swap-out can deadlock memory reclaim. Supporting swap requires preallocated memory and no backend I/O-path allocations.

## Reporting results

The commit's `result` field:

| Operation | Success | Partial | Failure |
|---|---|---|---|
| READ, WRITE, ZONE_APPEND | `nr_sectors << 9` | avoid: in 6.17 a short count completes that many bytes and the rest is re-issued; in 7.3-rc5 only a copy-mode read is completed partially, and other short results complete the whole request. A read that returns 0 bytes becomes `-EIO` | negative errno |
| FLUSH, DISCARD, WRITE_ZEROES, zone management | any value ≥ 0 | not applicable | negative errno |
| REPORT_ZONES | bytes of zone report written (`nr_zones × 64`); never 0 on kernels before 7.3, where it re-dispatches the request | | negative errno |

Return 0 for range operations: a 4 GiB byte count overflows signed 32-bit `result` and becomes an error.

Errnos map through block status to application errors:

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

Since 6.0, `errno_to_blk_status` maps negative results; unrecognized errnos become BLK_STS_IOERR.

## The durability contract

SET_PARAMS attributes define completed-write durability and delivery of flush/FUA:

| Device advertises | Flushes | FUA writes | The server promises |
|---|---|---|---|
| neither | completed by the block layer, never sent | the FUA bit is dropped | **every committed write is already durable** |
| `UBLK_ATTR_VOLATILE_CACHE` | sent as `UBLK_IO_OP_FLUSH` | emulated: write, then a flush | a flush makes all previously completed writes durable |
| `UBLK_ATTR_VOLATILE_CACHE` and `UBLK_ATTR_FUA` | sent | sent with `UBLK_IO_F_FUA` | both of the above, and a FUA write is durable when committed |

`UBLK_ATTR_FUA` without a volatile cache has no effect.

An unnecessary cache flag costs no-op flushes. A missing required flag loses acknowledged writes on power failure: filesystems treat cached journal commits as durable. When unsure, advertise the cache and implement flush.

Honor `UBLK_IO_F_FUA` on every flagged write. Advertise it when making one write durable, e.g. O_DSYNC or a network sync flag, is cheaper than full flush. Otherwise let the block layer emulate it correctly with flushes.