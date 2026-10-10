---
title: "Data copy modes and zero copy"
linkTitle: "Data copy modes"
description: "How request data reaches the server: the default copy, NEED_GET_DATA, USER_COPY, zero copy with REGISTER_IO_BUF and AUTO_BUF_REG, and shared-memory zero copy."
weight: 70
---

Block-request data lives in the page cache for buffered I/O or the application's pages for O_DIRECT. [Flags](/guide/features/) chosen at `ADD_DEV` select how the server accesses it, with six variants:

| Mode | Flag | Since | How data moves |
|---|---|---|---|
| Copy (default) | none | 6.0 | The driver copies between the request and a per-tag server buffer |
| Get-data | `UBLK_F_NEED_GET_DATA` | 6.0 | Like copy, but the server supplies a WRITE's buffer after seeing the request |
| User copy | `UBLK_F_USER_COPY` | 6.5 | The server copies with `pread`/`pwrite` on `/dev/ublkcN` |
| Fixed-buffer zero copy | `UBLK_F_SUPPORT_ZERO_COPY` | 6.15 | The request's pages go into an io_uring buffer table; the server points I/O at them |
| Automatic registration | `UBLK_F_AUTO_BUF_REG` | 6.16 | Same, but the kernel registers and unregisters for you |
| Shared-memory zero copy | `UBLK_F_SHMEM_ZC` | 7.1 | Requests whose pages lie in memory shared with the server arrive by reference |

Only READ and WRITE (and, on zoned devices, zone append and zone reports) carry data. FLUSH, DISCARD and WRITE_ZEROES never do, in any mode.

## Copy mode

With no copy flag, ublk uses copy mode. Start here.

Allocate a buffer per tag, at least the `max_io_buf_bytes` returned by `ADD_DEV`, which rounds the requested size down to a page multiple. Put its address in `ublksrv_io_cmd.addr` on each `FETCH_REQ` and `COMMIT_AND_FETCH_REQ`; 0 is `-EINVAL`. The address applies to the next request, so each commit may change the buffer.

```text
WRITE                                   READ
kernel: request arrives                 kernel: request arrives
kernel: copy request pages -> buffer    kernel: complete the waiting command
kernel: complete the waiting command    server: fill buffer, then
server: write buffer to the backend             COMMIT_AND_FETCH_REQ(result = bytes)
server: COMMIT_AND_FETCH_REQ(result)    kernel: copy result bytes buffer -> request
```

Copies run in the tag's daemon task, the thread that issued `FETCH_REQ`, because the kernel pins pages through that task's address space. WRITEs copy before command completion; READs copy during `COMMIT_AND_FETCH_REQ`. This is one reason commands must come from the tag's daemon.

Copy-mode rules:

- Use the descriptor's `nr_sectors`. If the driver pins only part of a WRITE buffer, it reduces this count to the copied length. Committing those bytes requeues the rest; pinning nothing requeues the entire request.
- READ results count bytes filled. A short copy-mode READ requeues the remainder; 0 becomes `-EIO`. Copying at most `result` bytes prevents exposing stale kernel memory, so unprivileged servers can use this mode.
- Commit the full byte count or a negative errno. In 6.17, short READs and WRITEs requeued the remainder in every mode. In 7.3-rc5, only copy-mode READs complete partially: a short WRITE, or any non-negative user-copy or zero-copy result, completes the entire request successfully.
- Buffer space is `queue_depth × max_io_buf_bytes`: 128 MiB at depth 128 and the 1 MiB default. Anonymous mappings consume physical memory when touched; busy queues touch every buffer.
- The kernel performs one `memcpy` per request. Its cost is small beside a 4 KiB round trip; large sequential transfers consume memory bandwidth here and again in a copying backend.

go-ublk uses this mode by default, with one anonymous mapping per queue sliced into per-tag buffers.

## NEED_GET_DATA

{{< since "6.0" >}} {{< uapi "UBLK_F_NEED_GET_DATA" >}} avoids pre-allocating WRITE buffers. `FETCH_REQ` may use `addr = 0`; WRITEs arrive as `UBLK_IO_RES_NEED_GET_DATA` (1), with a valid length and offset but no data copied.

Allocate a buffer and issue {{< uapi "UBLK_U_IO_NEED_GET_DATA" >}} (`0xc0107522`) for the same `q_id` and `tag`, with its address in `addr`. The kernel copies the data and completes with `UBLK_IO_RES_OK`; finish the WRITE with `COMMIT_AND_FETCH_REQ`.

```c
if (cqe->res == UBLK_IO_RES_NEED_GET_DATA) {
	io->buf = alloc_buffer(desc->nr_sectors << 9);
	submit_io_cmd(UBLK_U_IO_NEED_GET_DATA, q_id, tag, /*result*/ 0, io->buf);
	return;                         /* the data arrives with the next CQE (res = 0) */
}
```

Issue `NEED_GET_DATA` only when requested, or receive `-EINVAL`. READ commits still require a non-zero `addr`. `ADD_DEV` clears the flag if any non-copy mode is requested.

Each WRITE costs an extra command and `io_uring_enter` round trip. Kernel documentation reserves this compatibility path for servers that cannot pre-allocate buffers; new servers should avoid it.

## User copy

{{< since "6.5" >}} {{< uapi "UBLK_F_USER_COPY" >}} moves copying out of FETCH/COMMIT. Both commands require `addr = 0`, except zone append uses the field for its LBA. The server accesses in-flight data through `/dev/ublkcN` at an offset identifying the request:

```c
static inline __u64 ublk_user_copy_pos(__u16 q_id, __u16 tag, __u32 offset)
{
	return UBLKSRV_IO_BUF_OFFSET            /* 0x80000000 */
	     + ((__u64)q_id << UBLK_QID_OFF)    /* bit 41, 12 bits: 4096 queues */
	     + ((__u64)tag  << UBLK_TAG_OFF)    /* bit 25, 16 bits */
	     + offset;                          /* 25 bits: up to 32 MiB per request */
}

/* WRITE: fetch the data the application wrote */
pread(ublkc_fd, buf, desc->nr_sectors << 9, ublk_user_copy_pos(q_id, tag, 0));
/* READ: supply the data, then commit the byte count */
pwrite(ublkc_fd, buf, len, ublk_user_copy_pos(q_id, tag, 0));
```

From the server's perspective, `pread` copies out of WRITE/zone-append requests; `pwrite` copies into READ/zone-report requests. The wrong direction returns `-EACCES`.

Rules in the 6.17 driver:

- Copy after delivery and before commit. Otherwise, or past the request's end, the call returns `-EINVAL`; `UBLK_S_DEV_DEAD` returns `-EACCES`.
- Partial copies can address any byte, split transfers, or scatter into buffers. The return value counts bytes copied.
- Any thread may copy; there is no daemon-task check. Workers can copy while queue threads fetch.
- io_uring `IORING_OP_READ`/`IORING_OP_WRITE` work like `pread`/`pwrite`. Fixed buffers fail with `-EACCES`: the driver requires a user-backed iterator.
- OR `UBLKSRV_IO_INTEGRITY_FLAG` (`1ULL << 62`) into the position to access [integrity metadata](/guide/integrity/).

Use user copy to place bytes in a cache, network send buffer, or compression input instead of a per-tag slot. It is required for [integrity](/guide/integrity/) and, unless using zero copy, [zoned devices](/guide/zoned/).

> [!WARNING]
> User copy trusts READ results. Committing N bytes after writing fewer exposes the request pages' previous contents. It requires a trusted server and is refused for unprivileged devices.

## Zero copy with io_uring fixed buffers

{{< since "6.15" >}} {{< uapi "UBLK_F_SUPPORT_ZERO_COPY" >}} existed in the first UAPI header but became functional with {{< uapi "UBLK_U_IO_REGISTER_IO_BUF" >}} (`0xc0107523`) and {{< uapi "UBLK_U_IO_UNREGISTER_IO_BUF" >}} (`0xc0107524`) in 6.15. The driver installs request pages (bio vectors) in an io_uring buffer-table slot for operations accepting fixed buffers.

Register a sparse buffer table on the backend's io_uring (`io_uring_register_buffers_sparse()` in liburing), with a slot per in-flight request. FETCH/COMMIT use `addr = 0`.

Per request:

```text
CQE for (q_id, tag)                      descriptor is valid
REGISTER_IO_BUF   q_id, tag, addr = idx  request pages installed at buffer index idx
WRITE_FIXED / READ_FIXED (buf_index idx) backend I/O against a file, socket or
  or URING_CMD with IORING_URING_CMD_FIXED   NVMe passthrough, straight from the request
UNREGISTER_IO_BUF q_id, tag, addr = idx  slot released
COMMIT_AND_FETCH_REQ result, addr = 0    request completes
```

Link operations with `IOSQE_IO_LINK` to submit the chain together. Each buffer-using operation holds a reference, so unregistering during backend I/O is safe. Completion waits for both the server's commit and unregistration from every ring holding the buffer.

The server cannot access the bytes to checksum, compress, encrypt, deduplicate, or inspect them. Use copy mode or user copy for those operations.

Before 6.17, only the tag's daemon could register or unregister. {{< uapi "UBLK_F_BUF_REG_OFF_DAEMON" >}} (6.17, forced on) permits any task; `UNREGISTER_IO_BUF` then identifies the slot by index alone, ignoring `q_id` and `tag`.

For an O_DIRECT file or raw backend, advertise its memory alignment through `UBLK_PARAM_TYPE_DMA_ALIGN` (kernel default: 4 bytes) and matching `UBLK_PARAM_TYPE_SEGMENT` limits. Otherwise requests may fail or split. See [device parameters](/guide/parameters/).

> [!WARNING]
> Fill every byte of a READ you report. Like user copy, fixed-buffer zero copy requires `CAP_SYS_ADMIN` and is rejected for unprivileged devices.

## Automatic buffer registration

{{< since "6.16" >}} {{< uapi "UBLK_F_AUTO_BUF_REG" >}} removes both registration commands. Before delivery, the kernel registers the buffer at the server's chosen index on the ring that fetched the tag; the next commit unregisters it. Backend I/O can start immediately and concurrently.

Pack the index in the fetch/commit **SQE's `addr`**, as `struct ublk_auto_buf_reg`, rather than in `struct ublksrv_io_cmd`:

```c
struct ublk_auto_buf_reg {
	__u16 index;      /* buffer table slot for the next request on this tag */
	__u8  flags;      /* 0 or UBLK_AUTO_BUF_REG_FALLBACK */
	__u8  reserved0;  /* must be 0 */
	__u32 reserved1;  /* must be 0 */
};
/* sqe->addr bits 0-15 index, 16-23 flags, 24-31 reserved0, 32-63 reserved1 */
sqe->addr = ublk_auto_buf_reg_to_sqe_addr(&(struct ublk_auto_buf_reg){ .index = slot });
```

Non-zero reserved fields or unknown flags return `-EINVAL`. As in copy mode, a commit's value applies to the next request.

Registration constraints:

- Fetch, commit, and the sparse buffer table must share an io_uring (`io_ring_ctx`). A commit on another ring requires explicit `UNREGISTER_IO_BUF`; otherwise the request never completes.
- Registration may fail with `-EBUSY` (occupied slot), `-EINVAL` (missing table or out-of-range index), or `-ENOMEM`. Without fallback, the block request fails before delivery. With `UBLK_AUTO_BUF_REG_FALLBACK`, delivery succeeds with `UBLK_IO_F_NEED_REG_BUF` in `op_flags`. Use user copy or `REGISTER_IO_BUF`; the latter also needs `UBLK_F_SUPPORT_ZERO_COPY`, which the kernel selftest server sets for fallback.
- A table holds at most 16K entries; one ring serving many deep queues can exhaust it.
- Only requests carrying data are registered.

## Shared-memory zero copy

{{< since "7.1" >}} {{< uapi "UBLK_F_SHMEM_ZC" >}} avoids copying and per-request registration when application and server map the same physical pages.

Map a shared region, such as a client memfd received over a unix socket with `SCM_RIGHTS`, or a hugetlbfs file both processes open. Register it with {{< uapi "UBLK_U_CMD_REG_BUF" >}} (`0xc0207518`):

```c
struct ublk_shmem_buf_reg reg = {
	.addr  = (__u64)(uintptr_t)base,  /* server's mapping of the shared memory */
	.len   = size,                    /* page-aligned; default maximum 4 GiB */
	.flags = 0,                       /* or UBLK_SHMEM_BUF_READ_ONLY */
};
ctrl.addr = (__u64)(uintptr_t)&reg;
ctrl.len  = sizeof(reg);              /* 24 */
int index = ctrl_cmd(ring, UBLK_U_CMD_REG_BUF, &ctrl);   /* >= 0: buffer index */
```

The kernel pins pages long-term and records their frame numbers in a per-device tree. In 7.3-rc5, missing `UBLK_F_SHMEM_ZC` returns `-EOPNOTSUPP`; unaligned `addr`/`len`, zero length, length above 4 GiB, non-zero `reserved`, or unknown flags return `-EINVAL`. Registration works before or after `START_DEV`, freezing a live queue during tree updates. `UBLK_SHMEM_BUF_READ_ONLY` pins without write access, permits write-sealed memfds, and matches only WRITE requests: READs must write into the buffer. {{< uapi "UBLK_U_CMD_UNREG_BUF" >}} (`0xc0207519`) takes the index in `data[0]`.

Passing the structure by pointer permits an unprivileged control payload's device-path prefix. The kernel accepts this mode for unprivileged devices without explaining why. One interpretation: an unfilled READ exposes only the client's already-shared buffer. `REG_BUF` pins up to 4 GiB per buffer long-term, with no memlock accounting visible in the driver.

If an O_DIRECT request lies entirely within one registered buffer, its descriptor has `UBLK_IO_F_SHMEM_ZC` (bit 19) in `op_flags`, and `addr` encodes a reference:

```c
if (ublksrv_get_flags(desc) & (UBLK_IO_F_SHMEM_ZC >> 8)) {
	__u16 idx = ublk_shmem_zc_index(desc->addr);   /* bits 32-47 */
	__u32 off = ublk_shmem_zc_offset(desc->addr);  /* bits 0-31; 48-63 reserved */
	void *data = shmem_table[idx].base + off;      /* the application's bytes */
	/* WRITE: persist data; READ: fill data; then commit the byte count */
}
```

`ublksrv_get_flags()` returns `op_flags >> 8`, but `UBLK_IO_F_*` constants use the full field. Test `desc->op_flags & UBLK_IO_F_SHMEM_ZC` directly or shift as above.

Matched requests copy nothing, even on copy-mode devices. A non-zero result completes the entire request; commit the full byte count. Unmatched requests silently use the ordinary data path, which the server must still implement. Clients must:

- allocate I/O buffers in the shared region;
- use O_DIRECT, since page-cache pages cannot match;
- keep each request contiguous within one registered buffer.

The kernel selftest server accepts memfds through `/run/ublk/ublkbN.sock` or a hugetlbfs file specified on the command line.

## Choosing a mode

| Mode | Server sees the bytes | Copies per request | Extra commands | Unprivileged | Effort |
|---|---|---|---|---|---|
| Copy | Yes, in its per-tag buffer | One, in the kernel | None | Yes | Lowest |
| Get-data | Yes | One, in the kernel | One per WRITE | Yes | Low; legacy |
| User copy | Yes, wherever it copies to | One, in `pread`/`pwrite` | One syscall per transfer | No | Moderate |
| Fixed-buffer zero copy | No | None | Register and unregister | No | High |
| Automatic registration | No | None | None | No | Moderate |
| Shared-memory zero copy | Yes, in the shared mapping | None when the client cooperates | None per request | Yes | Moderate, plus client changes |

Start with copy mode. Use zero copy for pass-through file, block-device, or socket I/O when profiling justifies it; use user copy for custom buffer placement, zoned devices, or integrity. kublk, the kernel selftest server, rejects combinations of `NEED_GET_DATA`, `USER_COPY`, `SUPPORT_ZERO_COPY`, and `AUTO_BUF_REG` except the fallback pairing above.

## go-ublk

go-ublk implements every mode on this page:

| Mode | `DeviceParams` | Notes |
|---|---|---|
| Copy | the default | One anonymous mapping per queue, sliced into per-tag buffers of `MaxIOSize` |
| Get-data | `NeedGetData` | Supported for completeness; with fixed per-tag buffers it only adds a round trip per write |
| User copy | `EnableUserCopy` | `pread`/`pwrite` on `/dev/ublkcN` at the request's offset; turned on automatically for zoned devices and integrity |
| Zero copy | `EnableZeroCopy` with a `ZeroCopyBackend` (6.15+) | The backend names a file descriptor and base offset; the I/O thread serves each request with `READ_FIXED`/`WRITE_FIXED`, `FSYNC` and `FALLOCATE` on its own ring, from a buffer the kernel registers automatically (`AUTO_BUF_REG`, 6.16+) or that go-ublk registers with `REGISTER_IO_BUF` |
| Shared-memory zero copy | `SharedMemoryZeroCopy` with `Device.RegisterSharedMemory` (7.1+) | Requests flagged `UBLK_IO_F_SHMEM_ZC` arrive with `Request.Data` pointing into the registered region; nothing is copied |

Zero copy cannot be combined with user copy, get-data, unprivileged devices, zoned devices or integrity, and shared-memory zero copy cannot be combined with zero copy. See [configuration](/go-ublk/configuration/#data-copy-modes) and [backends](/go-ublk/backends/#zero-copy).
