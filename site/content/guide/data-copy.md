---
title: "Data copy modes and zero copy"
linkTitle: "Data copy modes"
description: "How request data reaches the server: the default copy, NEED_GET_DATA, USER_COPY, zero copy with REGISTER_IO_BUF and AUTO_BUF_REG, and shared-memory zero copy."
weight: 70
---

A block request's data lives in pages the block layer owns: the page cache for buffered I/O, the application's own pages for O_DIRECT. A ublk server runs in a separate process, so something has to bridge the two address spaces. ublk offers five ways to do it, chosen per device at `ADD_DEV` time with [feature flags](/guide/features/):

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

This is what you get with no copy flag set, and what most servers should start with.

The server allocates one buffer per tag, at least `max_io_buf_bytes` long. Use the value `ADD_DEV` returned, not the one you requested: the kernel rounds it down to a page multiple. The buffer's address goes in `ublksrv_io_cmd.addr` of every `FETCH_REQ` and `COMMIT_AND_FETCH_REQ`. Each command names the buffer for the *next* request on that tag, so a server may hand over a different buffer on every commit. An `addr` of 0 is `-EINVAL`.

```text
WRITE                                   READ
kernel: request arrives                 kernel: request arrives
kernel: copy request pages -> buffer    kernel: complete the waiting command
kernel: complete the waiting command    server: fill buffer, then
server: write buffer to the backend             COMMIT_AND_FETCH_REQ(result = bytes)
server: COMMIT_AND_FETCH_REQ(result)    kernel: copy result bytes buffer -> request
```

Both copies run in the context of the tag's daemon task, the thread that issued `FETCH_REQ`, because the kernel pins the server's pages through that task's address space. The WRITE copy happens just before the command completes; the READ copy happens while the kernel processes your `COMMIT_AND_FETCH_REQ`. That is one reason a tag's commands must come from its daemon task.

Details that matter:

- **Use `nr_sectors` from the descriptor.** If the driver can pin only part of the server buffer for a WRITE, it shrinks `nr_sectors` to what it copied; when you commit that many bytes, the rest of the request is requeued and delivered again. If it can pin nothing, the whole request is requeued and retried.
- **READ results are byte counts.** Commit the number of bytes you placed in the buffer. In copy mode a short count completes that much of the request and requeues the remainder; a READ that commits 0 is turned into `-EIO`. The kernel never copies more than `result` bytes, so a server cannot leak stale kernel memory into a READ. That makes copy mode safe for untrusted, unprivileged servers.
- **Do not rely on partial completion.** How a short result is treated depends on the kernel and the mode. In 6.17 a short READ or WRITE result requeued the remainder in every mode. In 7.3-rc5 only a copy-mode READ is completed partially; a short WRITE result, or any non-negative result in user copy or zero copy, completes the whole request as successful. Commit either the full byte count or a negative errno.
- **Memory.** A queue needs `queue_depth × max_io_buf_bytes` of buffer space, 128 MiB for depth 128 at the 1 MiB default. Anonymous mappings cost nothing until touched, but a busy queue touches them all.
- **Cost.** One `memcpy` per request inside the kernel. For 4 KiB random I/O that is noise next to the round trip; for large sequential I/O it is memory bandwidth you pay twice, once here and once in the backend.

go-ublk uses this mode by default, with one anonymous mapping per queue sliced into per-tag buffers.

## NEED_GET_DATA

{{< since "6.0" >}} {{< uapi "UBLK_F_NEED_GET_DATA" >}} lets a server avoid pre-allocating WRITE buffers. `FETCH_REQ` may pass `addr = 0`. When a WRITE arrives, the command completes with `UBLK_IO_RES_NEED_GET_DATA` (1) instead of 0, and nothing has been copied yet. The descriptor is valid, so the server knows the length and offset.

The server then allocates a buffer and issues {{< uapi "UBLK_U_IO_NEED_GET_DATA" >}} (`0xc0107522`) for the same `q_id` and `tag`, with the buffer in `addr`. The kernel copies the data into it and completes that command with `UBLK_IO_RES_OK`. From there it is an ordinary WRITE, finished with `COMMIT_AND_FETCH_REQ`.

```c
if (cqe->res == UBLK_IO_RES_NEED_GET_DATA) {
	io->buf = alloc_buffer(desc->nr_sectors << 9);
	submit_io_cmd(UBLK_U_IO_NEED_GET_DATA, q_id, tag, /*result*/ 0, io->buf);
	return;                         /* the data arrives with the next CQE (res = 0) */
}
```

Rules: issue `NEED_GET_DATA` exactly when the kernel asked for it, or the command fails with `-EINVAL`. READs still need a buffer at commit time: `COMMIT_AND_FETCH_REQ` with `addr = 0` is rejected for a READ even in this mode. The flag is cleared at `ADD_DEV` if any non-copy mode is also requested.

The cost is an extra command and an extra `io_uring_enter` round trip per WRITE. The kernel documentation describes it as a compatibility path for existing servers that cannot adopt pre-allocated buffers; new servers should not use it.

## User copy

{{< since "6.5" >}} With {{< uapi "UBLK_F_USER_COPY" >}} the kernel never copies. `FETCH_REQ` and `COMMIT_AND_FETCH_REQ` must pass `addr = 0` (zone append, which reuses the field for an LBA, is the only exception). Instead, while a request is in flight the server reads and writes its data through the character device, at an offset that names the request:

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

The direction is from the server's point of view: `pread` copies *out of* the request and is valid for WRITE and zone append requests; `pwrite` copies *into* the request and is valid for READ and zone report requests. The wrong direction fails with `-EACCES`.

Further rules, from the 6.17 driver:

- The request must be in flight: delivered to the server and not yet committed. Otherwise, or if the offset is past the end of the request, the call fails with `-EINVAL`. A device in `UBLK_S_DEV_DEAD` returns `-EACCES`.
- Partial copies are fine. The offset field addresses any byte of the request, so a server can copy in pieces or scatter into several buffers; the return value is the number of bytes copied.
- Any thread may copy. Unlike the I/O commands there is no daemon-task check, so a worker pool can move data while the queue thread keeps fetching.
- io_uring `IORING_OP_READ` and `IORING_OP_WRITE` on the character device work like `pread`/`pwrite`. Registered (fixed) buffers do not: the driver requires a user-backed iterator and returns `-EACCES` otherwise.
- To reach a request's integrity buffer instead of its data, OR `UBLKSRV_IO_INTEGRITY_FLAG` (`1ULL << 62`) into the position. See [Integrity metadata](/guide/integrity/).

User copy is the right choice when the server wants data to land directly in its own structures (a cache, a network send buffer, a compression input) rather than in a fixed per-tag slot, and it is a prerequisite for [zoned devices](/guide/zoned/) (or zero copy) and for [integrity](/guide/integrity/).

> [!WARNING]
> In user copy the kernel takes the READ result on trust. If the server commits N bytes but wrote fewer, the reader receives whatever the request pages held before. That is why user copy is refused for unprivileged devices and why the kernel documentation says a server using it must be trusted.

## Zero copy with io_uring fixed buffers

{{< since "6.15" >}} {{< uapi "UBLK_F_SUPPORT_ZERO_COPY" >}} was defined in the very first UAPI header but did nothing until 6.15 added {{< uapi "UBLK_U_IO_REGISTER_IO_BUF" >}} (`0xc0107523`) and {{< uapi "UBLK_U_IO_UNREGISTER_IO_BUF" >}} (`0xc0107524`). The mechanism is io_uring's kernel-buffer support: the driver installs the request's own pages (its bio vectors) into a slot of an io_uring buffer table, and any io_uring operation that accepts a fixed buffer can then read or write them directly.

Setup: register a sparse buffer table on the io_uring that will do the backend I/O (`io_uring_register_buffers_sparse()` in liburing), with enough slots for every request you will have in flight on that ring. `FETCH_REQ` and `COMMIT_AND_FETCH_REQ` pass `addr = 0`.

Per request:

```text
CQE for (q_id, tag)                      descriptor is valid
REGISTER_IO_BUF   q_id, tag, addr = idx  request pages installed at buffer index idx
WRITE_FIXED / READ_FIXED (buf_index idx) backend I/O against a file, socket or
  or URING_CMD with IORING_URING_CMD_FIXED   NVMe passthrough, straight from the request
UNREGISTER_IO_BUF q_id, tag, addr = idx  slot released
COMMIT_AND_FETCH_REQ result, addr = 0    request completes
```

Operations can be linked with `IOSQE_IO_LINK` so the whole chain is submitted at once. Each io_uring operation that uses the buffer holds its own reference, so unregistering while a backend operation is still running is safe; the request completes only after the buffer has been unregistered from every ring that registered it **and** the server has committed.

What zero copy cannot do: the server never has the data in its address space. It cannot checksum, compress, encrypt, deduplicate or inspect it; it can only direct I/O at it. A server that needs the bytes should use copy mode or user copy.

Threading: before 6.17 only the tag's daemon task could register or unregister. {{< uapi "UBLK_F_BUF_REG_OFF_DAEMON" >}} (6.17, always set by the kernel that has it) allows any task, and then `UNREGISTER_IO_BUF` identifies the slot by index alone, ignoring `q_id` and `tag`.

Alignment matters here more than anywhere else. With zero copy the request's pages go straight to the backend, so if the backend is an O_DIRECT file or a raw device with alignment requirements, advertise them: set `UBLK_PARAM_TYPE_DMA_ALIGN` to the backend's memory alignment (the kernel default is 4 bytes) and `UBLK_PARAM_TYPE_SEGMENT` limits that match the backend, or requests will fail or be split. See [Device parameters](/guide/parameters/).

> [!WARNING]
> As with user copy, a zero-copy server must fill every byte of a READ it reports. The kernel requires `CAP_SYS_ADMIN` for these modes and rejects them for unprivileged devices.

## Automatic buffer registration

{{< since "6.16" >}} {{< uapi "UBLK_F_AUTO_BUF_REG" >}} removes the two registration commands. Before delivering a request, the kernel registers its buffer into the buffer table of the io_uring that issued the tag's `FETCH_REQ` or `COMMIT_AND_FETCH_REQ`, at an index the server chose; when the next `COMMIT_AND_FETCH_REQ` for that tag arrives, it unregisters it. The server's backend I/O no longer depends on a registration command completing first, so it can be submitted immediately and concurrently.

The index is not in `struct ublksrv_io_cmd`. It travels in the **SQE's own `addr` field** of the fetch or commit command, as a packed `struct ublk_auto_buf_reg`:

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

Non-zero reserved fields or unknown flags fail the command with `-EINVAL`. As in copy mode, the value given with a commit applies to the next request the tag receives.

Requirements and pitfalls:

- The sparse buffer table must live on the same io_uring (`io_ring_ctx`) that issues the tag's fetch and commit commands. If a commit arrives on a different ring, the kernel does not unregister automatically; the server must issue `UNREGISTER_IO_BUF` itself or the request never completes.
- Registration can fail, for example if the slot is still occupied. <!-- VERIFY: io_buffer_register_bvec failure causes (occupied slot, index beyond table size) and their errnos --> Without a fallback, the kernel fails the block request with an I/O error and the server never sees it. With `UBLK_AUTO_BUF_REG_FALLBACK` in `flags`, the command completes normally with `UBLK_IO_F_NEED_REG_BUF` set in the descriptor's `op_flags`, and the server must get at the data some other way: register it with `REGISTER_IO_BUF` (which requires `UBLK_F_SUPPORT_ZERO_COPY` as well; the kernel selftest server sets both for its fallback mode) or use user copy.
- An io_uring buffer table holds at most 16K entries. One ring serving many devices with deep queues can run out.
- Only requests with data are registered.

## Shared-memory zero copy

{{< since "7.1" >}} {{< uapi "UBLK_F_SHMEM_ZC" >}} takes a different approach: if the application doing I/O and the server map the same physical pages, there is nothing to copy and nothing to register per request.

The server maps a shared region (a memfd received from the client over a unix socket with `SCM_RIGHTS`, or a hugetlbfs file both sides open) and registers it with the control command {{< uapi "UBLK_U_CMD_REG_BUF" >}} (`0xc0207518`):

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

The kernel pins the pages long-term and records their page frame numbers in a per-device tree. In 7.3-rc5 the command fails with `-EOPNOTSUPP` unless the device has `UBLK_F_SHMEM_ZC`, and with `-EINVAL` if `addr` or `len` is not page-aligned, `len` is 0 or above 4 GiB, `reserved` is non-zero, or `flags` has bits other than `UBLK_SHMEM_BUF_READ_ONLY`. It works before or after `START_DEV`; on a live device the kernel freezes the queue while it updates the tree. `UBLK_SHMEM_BUF_READ_ONLY` pins without write access, which works with a write-sealed memfd, and such a buffer only ever matches WRITE requests, since a READ would need the kernel side to write into it. {{< uapi "UBLK_U_CMD_UNREG_BUF" >}} (`0xc0207519`) takes the index in `data[0]`.

The registration structure is passed by pointer rather than inline so that an unprivileged device can prefix the control payload with the device path, and the kernel does accept shared-memory zero copy on unprivileged devices. That is consistent with the trust argument above: a request only takes this path when its pages already belong to memory the server has mapped, so a READ the server fails to fill exposes nothing but the client's own shared buffer. <!-- VERIFY: rationale for allowing SHMEM_ZC on unprivileged devices; 7.3-rc5 ADD_DEV does not reject it and REG_BUF/UNREG_BUF are in the unprivileged permission table -->

When an O_DIRECT request's data lies entirely within one registered buffer, the descriptor arrives with `UBLK_IO_F_SHMEM_ZC` (bit 19) in `op_flags`, and `addr` holds a reference instead of a server address:

```c
if (ublksrv_get_flags(desc) & (UBLK_IO_F_SHMEM_ZC >> 8)) {
	__u16 idx = ublk_shmem_zc_index(desc->addr);   /* bits 32-47 */
	__u32 off = ublk_shmem_zc_offset(desc->addr);  /* bits 0-31; 48-63 reserved */
	void *data = shmem_table[idx].base + off;      /* the application's bytes */
	/* WRITE: persist data; READ: fill data; then commit the byte count */
}
```

Note the shift: `ublksrv_get_flags()` returns `op_flags >> 8`, while the `UBLK_IO_F_*` constants are defined against the full `op_flags`, so test `desc->op_flags & UBLK_IO_F_SHMEM_ZC` directly or shift as above.

For a matched request the kernel copies nothing in either direction, even on a copy-mode device, and a non-zero result completes the whole request, so commit the full byte count. Requests that do not match fall back silently to the device's ordinary mode, so the server still needs a complete normal data path; shared memory is an optimization on top. It only helps cooperating clients:

- the client must allocate its I/O buffers from the shared region;
- it must use O_DIRECT, since buffered I/O goes through page-cache pages that can never match;
- each request's data must be contiguous within a single registered buffer.

The kernel's selftest server implements both setups: a listener on `/run/ublk/ublkbN.sock` that accepts memfds from clients, and a hugetlbfs file named on the command line.

## Choosing a mode

| Mode | Server sees the bytes | Copies per request | Extra commands | Unprivileged | Effort |
|---|---|---|---|---|---|
| Copy | Yes, in its per-tag buffer | One, in the kernel | None | Yes | Lowest |
| Get-data | Yes | One, in the kernel | One per WRITE | Yes | Low; legacy |
| User copy | Yes, wherever it copies to | One, in `pread`/`pwrite` | One syscall per transfer | No | Moderate |
| Fixed-buffer zero copy | No | None | Register and unregister | No | High |
| Automatic registration | No | None | None | No | Moderate |
| Shared-memory zero copy | Yes, in the shared mapping | None when the client cooperates | None per request | Yes | Moderate, plus client changes |

Start with copy mode. Move to zero copy when the server is a pass-through to a file, block device or socket and profiling shows the copy matters. Choose user copy when you need the bytes but not in a fixed slot, or when zoned or integrity support requires it. kublk, the kernel's selftest server, refuses to combine more than one of `NEED_GET_DATA`, `USER_COPY`, `SUPPORT_ZERO_COPY` and `AUTO_BUF_REG`, except for the fallback pairing described above.

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
