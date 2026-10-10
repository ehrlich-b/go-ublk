---
title: "Known kernel bugs"
linkTitle: "Known kernel bugs"
description: "Kernel and distribution bugs that bite ublk servers: which builds have them, what they look like, and how to avoid them."
weight: 140
---

These are kernel and distribution failures observed with go-ublk on Ubuntu 6.17/7.0 (arm64 and x86_64), then across the [compatibility matrix](/reference/matrix/). They include failures also seen with reference servers and protocol rules that can resemble driver bugs. Build/CVE tables retain upstream and Ubuntu changelog notes; those assignments have not been independently reverified here.

An unreproduced failure is not a verified fix.

## Which kernel to run

| Situation | Run | Avoid |
|---|---|---|
| Ubuntu 24.04, generic | `linux-hwe-7.0` 7.0.0-30 or later (go-ublk is verified on 7.0.0-30 arm64 and 7.0.0-38 x86_64), or `linux-hwe-6.17` 6.17.0-41 or later | 6.17.0-35, -38 and -40 generic: `ADD_DEV` crashes the host |
| Ubuntu on AWS | `linux-aws-6.17` 6.17.0-1020 or later, with `linux-modules-extra-$(uname -r)` installed | 6.17.0-1019-aws |
| Need every known teardown fix, or plan to use user recovery | kernel.org 7.1.y or later, the only line with all three teardown fixes below | assuming a 7.0 build has them |
| kernel.org stable 6.18 | 6.18.6 or later | 6.18.4 and 6.18.5 |
| Windows developer machine | a Linux VM | WSL2: its kernel has no `ublk_drv` |

Hold tested kernels (`apt-mark hold` on Ubuntu). Installing a newer kernel can change the next boot's default; hosts reached the broken 6.17 builds this way.

## ADD_DEV NULL dereference on Ubuntu 6.17

The first `ADD_DEV` hangs with a NULL dereference at address 0 in `ublk_init_queues` (`+0x4e` x86_64, `+0x84` arm64), called from `ublk_ctrl_add_dev` in an `iou-wrk` worker. Reproductions include one queue at depth one, the kernel selftest server, and ublksrv. CPU topology matters: the matrix uses `possible_cpus=8` to expose the -40 failure. The worker dies holding the global control mutex, blocking later ADD_DEV/DEL_DEV calls until reboot.

Ubuntu backported `529d4d632788` ("ublk: implement NUMA-aware memory allocation", 6.19) without `011af85ccd87` ("ublk: reorder tag_set initialization before queue allocation"). Queue initialization consults the tag set's CPU map before it exists. Stable 6.18.4/6.18.5 made the same split. Mainline releases before 6.19 have neither commit; 6.19 has both.

| Kernel line | Broken | Fixed |
|---|---|---|
| Ubuntu `linux-hwe-6.17` (generic) | confirmed by crash: 6.17.0-35, -40; by changelog: -38. The backport first appears in the 6.17.0-24 changelog, so -24 to -29 are suspect. | 6.17.0-41; 6.17.0-42 has been in noble-updates and security since 2026-08-05 |
| Ubuntu `linux-aws-6.17` | 6.17.0-1019 | 6.17.0-1020 |
| Ubuntu `linux-azure-6.17` | 6.17.0-1015 through -1020 (the backport first appears in the -1014 changelog; [Launchpad bug 2154635](https://bugs.launchpad.net/bugs/2154635)) | 6.17.0-1021 (noble-updates since 2026-07-23); -1022 current |
| kernel.org stable 6.18 | 6.18.4, 6.18.5 | 6.18.6 |
| Ubuntu 6.18 (`linux` 6.18.0-9.9, at last check) | lists the NUMA commit without the reorder | check the changelog of any 6.18 build before trusting it |

Checked pre-backport builds 6.17.0-7, -8, and -14 avoid this bug but lack later fixes.

Check for the prerequisite in the package changelog:

```sh
apt changelog linux-image-$(uname -r) 2>/dev/null \
  | grep -c 'reorder tag_set initialization before queue allocation'
```

Broken -35, -40, and -1019-aws modules had `srcversion` `6A00163FD3030280266148D` (`modinfo ublk_drv | grep srcversion`). Compare with a known-good build of the same kernel; one hash is insufficient.

Use a fixed or pre-backport build. Userspace cannot repair the missing initialization. Alternatively, apply `011af85ccd87` and build `ublk_drv` out of tree against the distribution headers, replacing the module without rebuilding the kernel.

## Teardown races fixed in 7.1

Three 7.0/7.1-cycle fixes address command cancellation during teardown or recovery, all in `drivers/block/ublk_drv.c`:

| Commit | Subject | Upstream | Stable | Ubuntu `linux-hwe-7.0` |
|---|---|---|---|---|
| `845db023a8ae` | ublk: don't issue uring_cmd from fallback task work | v7.1-rc3 | none (no `Cc: stable`) | absent through 7.0.0-39 |
| `0842186d2c4e` | ublk: reset per-IO canceled flag on each fetch (CVE-2026-53124) | 7.1 | 7.0.10 | from 7.0.0-28 |
| `f7700a4415af` | ublk: fix use-after-free in `ublk_cancel_cmd()` | v7.1-rc3 | none (no `Cc: stable`) | absent through 7.0.0-39 |

7.0.y ended at 7.0.14 without the first and third fixes; 7.0-based distributions need separate backports. 7.1.y has all three. `0842186d2c4e` fixes uncancellable fetches after a server dies with only part of a queue fetched. `f7700a4415af` also fixes the [USER_RECOVERY reset path](/guide/recovery/).

A busy device's graceful stop on x86_64 Ubuntu 6.17.0-14 produced one crash:

```text
BUG: kernel NULL pointer dereference, address: 0000000000000008
RIP: io_req_uring_cleanup+0x18
 io_uring_cmd_done <- ublk_dispatch_req <- ublk_cmd_tw_cb
 <- io_uring_cmd_work <- task_work_run <- get_signal
```

The working hypothesis is double completion: a handled signal runs pending task work through `get_signal` while the server thread is alive; `ublk_dispatch_req` checks only for an exiting task and completes an already-cancelled command. Related io_uring fix `3539b1467e94` (dying-ring task_work cancel state, 6.17-rc7) was already present. Whether `845db023a8ae` and `f7700a4415af` (7.1) prevent this failure remains unverified.

Frequency is unknown. The crash occurred during a systemd shutdown storm; subsequent tests were clean: 150 loaded teardowns plus a 24-combination integrity sweep on arm64 6.17.0-41, and 150 teardowns on x86_64 6.17.0-1020-aws, which lacks all three fixes. The earlier "within 120 cycles" arm64 claim was a harness bug: parsing `1` from `USR1` caused SIGINT to PID 1 and rebooted the VM. Go's runtime re-enables managed signals, so masking I/O-thread signals does not eliminate this proposed trigger.

Prefer 7.1.y; otherwise stop devices while quiet to reduce exposure to the observed teardown trigger.

## Found by go-ublk's kernel matrix

These matrix findings concern the kernel paths used by ublk servers:

**Write-zeroes can truncate silently before 6.11.** Above `UINT32_MAX >> 9`, an advertised `max_write_zeroes_sectors` lets one bio's 32-bit `bi_size` overflow at `nr_sects << 9`. `blkdiscard -z -l 5G` succeeds but delivers only 1 GiB, leaving 4 GiB unchanged. Observed on mainline 6.4/6.6/6.9/6.10, Ubuntu 6.8 (24.04 GA), and openSUSE Leap 15.6; correct on 6.11.11 and later. On every kernel, cap both `max_write_zeroes_sectors` and `max_discard_sectors` at `UINT32_MAX >> 9`, rounded down to logical block size. go-ublk v0.2.0 does this.

**Delete stopped devices; add new ones.** Restarting via [START_DEV after STOP_DEV](/guide/control-plane/) returns `EBUSY` on Fedora 6.19/7.2.8 and mainline 7.0.14, hangs the control plane on 6.10-6.12, or crashes. Arch 7.2.8-arch1-2 NULL-dereferenced in `ublk_queue_rq` from `ublk_partition_scan_work`: a partition read reached a queue with missing per-I/O state.

**Batch handoff before 7.3-rc3 can fail I/O.** `QUIESCE_DEV` leaves `force_abort` set on `UBLK_F_BATCH_IO` queues. On Ubuntu 7.0.0-38, a concurrent writer received `EIO` every attempt. Fix: "ublk: clear force_abort in ublk_queue_reset_io_flags()" (`8a14be55bdc6`, 7.3-rc3, stable 7.2.7). Earlier kernels require handoff without QUIESCE_DEV: release the device and let `UBLK_F_USER_RECOVERY_REISSUE` requeue outstanding I/O.

**Provided-buffer registration fails on Ubuntu 6.8.** On 6.8.0-146 (24.04 GA) and 6.8.0-138 (22.04 HWE), valid `IORING_REGISTER_PBUF_RING` calls return `-EINVAL` for any size, with user memory or `IOU_PBUF_RING_MMAP`. Invalid non-zero reserved fields are accepted instead. Ubuntu's 6.8.0-146 backport of "io_uring/kbuf: use mem_is_zero()" inverted the check. Mainline 6.8.12/6.9 and Ubuntu 6.11/7.0 are unaffected. ublk batch I/O needs these rings only on 7.0+, but backends using them, e.g. through liburing's `io_uring_setup_buf_ring`, fail on these builds.

**UBSAN `array-index-out-of-bounds` in `io_buffer_register_bvec`.** Zero copy, through `UBLK_U_IO_REGISTER_IO_BUF` or automatic buffer registration, logs this on Fedora 42 (6.19.14) and Fedora 43 and 44 (7.2.8):

```text
UBSAN: array-index-out-of-bounds in io_uring/rsrc.c:1070:12
index 0 is out of range for type 'bio_vec [*]'
```

The recorded cause is a false positive, present through 7.3-rc5: `io_alloc_imu` leaves `nr_bvecs` uninitialized, then `io_buffer_register_bvec` fills `io_mapped_ubuf.bvec[] __counted_by(nr_bvecs)` before setting the count. Compilers supporting `__counted_by` with `CONFIG_UBSAN_BOUNDS` flag data-bearing registrations, although allocation covers `blk_rq_nr_phys_segments(rq)` entries. With `panic_on_warn`, the warning can panic the host; avoid zero copy there until the kernel sets the count before filling the array.

## Smaller fixes worth having

These historical Ubuntu/stable changelog entries identify fixes to check when choosing a build; package status is not a live update.

| Fix | What it changes | Where it landed |
|---|---|---|
| `d369735e02ef` ublk: fix mmap for 64K page size | Multi-queue devices could not map their descriptors on 64K-page kernels (some arm64 builds) | 6.13; stable 6.1.120, 6.6.64, 6.12.2 |
| `25966fc09769` ublk: fix NULL dereference on `UPDATE_SIZE` without a disk (CVE-2026-43364) | `UPDATE_SIZE` on a device that was never started oopsed 6.16 to 6.19; resize only started devices (go-ublk's `Resize` requires a running device) | 7.0-rc4; stable 6.18.20, 6.19.9 |
| `1860c2f85922` ublk: reject max_sectors smaller than PAGE_SECTORS in parameter validation | A `max_sectors` below one page used to pass `SET_PARAMS` and trip a `WARN_ON_ONCE` at `START_DEV`; now `SET_PARAMS` fails with `-EINVAL` | stable 7.0.11; `linux-hwe-7.0` 7.0.0-28 |
| ublk: wait on ublk_dev_ready() instead of ub->completion (CVE-2026-68173) | After a server crash, `END_USER_RECOVERY` could mark the device live before every queue had fetched again (when the preceding `START_USER_RECOVERY` had failed), stranding a requeued request (for example ext4's flush) so `fsync` and teardown hung. Recovery and `START_DEV` now wait for real queue readiness | `linux-hwe-7.0` 7.0.0-38 |
| ublk: reset kernel-owned dev_info fields in ublk_ctrl_add_dev() (CVE-2026-74472) | `ADD_DEV` no longer keeps kernel-owned fields of the caller's `ublksrv_ctrl_dev_info` | `linux-hwe-7.0` 7.0.0-39 (proposed) |
| ublk: clear VM_MAYWRITE on read-only ublk char device mmap (CVE-2026-89793) | The descriptor array is mapped read-only, but `mprotect()` could make it writable, letting a server (an unprivileged one in particular) corrupt the kernel-written descriptors (`op_flags`, `nr_sectors`, `start_sector`, `addr`); `mprotect` now fails with `EACCES` | `linux-hwe-7.0` 7.0.0-39 (proposed) |
| ublk: fix use-after-free in ublk_partition_scan_work | Use-after-free in the asynchronous partition scan that Ubuntu backported into 6.17.0-24 | `linux-hwe-6.17` 6.17.0-41 |
| ublk: fix ublksrv pid handling for pid namespaces | Servers running in a PID namespace | `linux-hwe-6.17` 6.17.0-41 |
| ublk: fix deadlock when reading partition table (CVE-2025-68823) | Deadlock during the partition scan at `START_DEV` | `linux-hwe-6.17` 6.17.0-22 |
| ublk: clean up user copy references on ublk server exit (CVE-2025-71070) | Leaked request references after a user-copy server exits | `linux-hwe-6.17` 6.17.0-22 |

Zero `struct ublksrv_ctrl_dev_info`; fill only documented `ADD_DEV` inputs.

## Missing modules

Tested distributions build `CONFIG_BLK_DEV_UBLK=m`; the module may be absent or unloaded.

- Ubuntu AWS needs `linux-modules-extra-*-aws`, absent from the base image. Install `linux-modules-extra-$(uname -r)` for `/dev/ublk-control`.
- RHEL/CentOS Stream/AlmaLinux/Rocky 10 ship the module but disable io_uring through `kernel.io_uring_disabled`, producing permission errors. The tested 6.12 builds pass with `sysctl kernel.io_uring_disabled=0`. RHEL 9's 5.14 kernels lack `ublk_drv`.
- WSL2's checked `6.6.87.2-microsoft-standard-WSL2` kernel lacks the driver; use a Linux VM.
- Opening `/dev/ublk-control` cannot auto-load it: the misc-device minor is dynamic and the driver has no device-name alias (checked in 6.17/7.3-rc5). Use `modprobe ublk_drv` or `/etc/modules-load.d/`.

`modinfo ublk_drv` tells you whether the module exists for the running kernel; `ls -l /dev/ublk-control` whether it is loaded.

## Protocol rules that look like kernel bugs

Violating these protocol rules can hang a host:

**Close char-device references before DEL_DEV.** Its interruptible wait includes the original fd, every `dup`, and io_uring fixed-file registrations. Use `DEL_DEV_ASYNC` (6.9) to avoid waiting. See [control plane](/guide/control-plane/).

**Keep serving during STOP_DEV.** `del_gendisk` waits for in-flight requests, which only the queue threads can finish. Stop, drain, then shut queues and delete. Cancelling queues first can hang STOP_DEV indefinitely.

**Use the fixed descriptor stride.** With default-size descriptors, queue `q` starts at `q * round_up(UBLK_MAX_QUEUE_DEPTH * sizeof(struct ublksrv_io_desc), PAGE_SIZE)`: 98304 bytes on 4 KiB pages, 131072 on 64 KiB pages, regardless of depth. Using actual queue depth can map other queues onto queue 0, causing corruption and unkillable hangs as queue count rises. See [data plane](/guide/data-plane/).

**Sectors are 512 bytes.** `start_sector`, `nr_sectors`, `dev_sectors` and `max_sectors` are in 512-byte units on every device, including 4Kn devices.

**FLUSH, DISCARD, and WRITE_ZEROES complete with 0.** The kernel only checks for negative results. Byte counts at 2 GiB overflow the signed 32-bit result into `EIO`; `mkfs` whole-device discard exposes this above 2 GiB. See [I/O operations](/guide/io-operations/).

**One opener.** `/dev/ublkcN` admits one open file at a time; a second `open` fails with `-EBUSY`, and the process that opened it is the only valid PID for `START_DEV` and `END_USER_RECOVERY`.

**Unprivileged servers die on timeouts.** For an [unprivileged device](/guide/unprivileged/), a request timeout makes the kernel `SIGKILL` the server.

## Reboot hangs: deployment, not the kernel

A bare background server can die before unmount writes finish. Tests lost final writeback every reboot and hung about one reboot in five. Order the mount after the server's systemd unit, and stop login sessions before unmount (`Before=user.slice` on the mount). Open files can otherwise prevent unmount, letting systemd kill the server beneath a mounted filesystem. See the working units in [deployment](/go-ublk/deployment/).