---
title: "Known kernel bugs"
linkTitle: "Known kernel bugs"
description: "Kernel and distribution bugs that bite ublk servers: which builds have them, what they look like, and how to avoid them."
weight: 140
---

ublk is young and the driver changes every release, so the kernel you run matters as much as your server code. This page collects the kernel and distribution problems found while testing go-ublk, first on Ubuntu 6.17 and 7.0 on arm64 and x86_64 and then across the mainline and distribution kernels of its [compatibility matrix](/reference/matrix/), plus the fixes from upstream and Ubuntu changelogs that a server author should know about. None of them is specific to go-ublk: the reference servers hit the same ones. It also lists protocol rules that look like kernel bugs the first time they bite.

Where something is unknown, this page says so. "Not reproduced" is not "fixed".

## Which kernel to run

| Situation | Run | Avoid |
|---|---|---|
| Ubuntu 24.04, generic | `linux-hwe-7.0` 7.0.0-30 or later (go-ublk is verified on 7.0.0-30 arm64 and 7.0.0-38 x86_64), or `linux-hwe-6.17` 6.17.0-41 or later | 6.17.0-35, -38 and -40 generic: `ADD_DEV` crashes the host |
| Ubuntu on AWS | `linux-aws-6.17` 6.17.0-1020 or later, with `linux-modules-extra-$(uname -r)` installed | 6.17.0-1019-aws |
| Need every known teardown fix, or plan to use user recovery | kernel.org 7.1.y or later, the only line with all three teardown fixes below | assuming a 7.0 build has them |
| kernel.org stable 6.18 | 6.18.6 or later | 6.18.4 and 6.18.5 |
| Windows developer machine | a Linux VM | WSL2: its kernel has no `ublk_drv` |

Once a kernel passes your tests, hold it (`apt-mark hold` on Ubuntu). A newer kernel that is merely installed becomes the boot default and is picked up silently at the next reboot, which is exactly how hosts drifted onto the broken 6.17 builds below.

## ADD_DEV NULL dereference on Ubuntu 6.17

**Symptom.** The first `ADD_DEV` on the machine never returns, and the kernel logs a NULL pointer dereference at address 0 in `ublk_init_queues` (`+0x4e` on x86_64, `+0x84` on arm64), called from `ublk_ctrl_add_dev` in an `iou-wrk` io-wq worker. Any server triggers it, with any parameters, down to one queue of depth one; the kernel selftests server and ublksrv crash identically. The worker dies while holding the driver's global control mutex, so every later `ADD_DEV` and `DEL_DEV` on the host blocks as well; only a reboot recovers.

**Cause.** Ubuntu backported upstream `529d4d632788` ("ublk: implement NUMA-aware memory allocation", a 6.19 change) without its prerequisite `011af85ccd87` ("ublk: reorder tag_set initialization before queue allocation"). The reworked queue initialization looks up each queue's NUMA node through the blk-mq tag set's CPU map, but without the reorder it runs before the tag set exists, so the map pointer is still NULL. kernel.org stable made the same split in 6.18.4 and 6.18.5. No mainline release was ever affected: releases before 6.19 have neither commit, and 6.19 has both.

| Kernel line | Broken | Fixed |
|---|---|---|
| Ubuntu `linux-hwe-6.17` (generic) | confirmed by crash: 6.17.0-35, -40; by changelog: -38. The backport first appears in the 6.17.0-24 changelog, so -24 to -29 are suspect. | 6.17.0-41; 6.17.0-42 has been in noble-updates and security since 2026-08-05 |
| Ubuntu `linux-aws-6.17` | 6.17.0-1019 | 6.17.0-1020 |
| Ubuntu `linux-azure-6.17` | 6.17.0-1015 through -1020 (the backport first appears in the -1014 changelog; [Launchpad bug 2154635](https://bugs.launchpad.net/bugs/2154635)) | 6.17.0-1021 (noble-updates since 2026-07-23); -1022 current |
| kernel.org stable 6.18 | 6.18.4, 6.18.5 | 6.18.6 |
| Ubuntu 6.18 (`linux` 6.18.0-9.9, at last check) | lists the NUMA commit without the reorder | check the changelog of any 6.18 build before trusting it |

Ubuntu 6.17 builds from before the backport (6.17.0-7, -8 and -14 were checked) are not affected by this bug, though they lack later fixes.

**Checking a build.** Look for the prerequisite in the package changelog:

```sh
apt changelog linux-image-$(uname -r) 2>/dev/null \
  | grep -c 'reorder tag_set initialization before queue allocation'
```

The broken module in -35, -40 and -1019-aws had `srcversion` `6A00163FD3030280266148D` (`modinfo ublk_drv | grep srcversion`), but compare against a known-good build of your own kernel rather than relying on one hash.

**Workaround.** None in userspace: the dereference happens before anything the server sent is examined. Run a fixed or pre-backport build, or rebuild `ublk_drv` out of tree with `011af85ccd87` applied; the module builds against the distribution's kernel headers and replaces the stock one without a kernel rebuild.

## Teardown races fixed in 7.1

Three driver fixes from the 7.0 and 7.1 cycles harden what happens when a server's commands are cancelled during teardown or recovery. All touch only `drivers/block/ublk_drv.c`.

| Commit | Subject | Upstream | Stable | Ubuntu `linux-hwe-7.0` |
|---|---|---|---|---|
| `845db023a8ae` | ublk: don't issue uring_cmd from fallback task work | v7.1-rc3 | none (no `Cc: stable`) | absent through 7.0.0-39 |
| `0842186d2c4e` | ublk: reset per-IO canceled flag on each fetch (CVE-2026-53124) | 7.1 | 7.0.10 | from 7.0.0-28 |
| `f7700a4415af` | ublk: fix use-after-free in `ublk_cancel_cmd()` | v7.1-rc3 | none (no `Cc: stable`) | absent through 7.0.0-39 |

7.0.y reached end of life at 7.0.14 without the first and third, so every 7.0-based kernel lacks them unless a distribution backports them by hand. kernel.org 7.1.y has all three. `0842186d2c4e` fixes a recovery-specific hang: a server that dies after fetching only part of a queue's tags could leave those fetches uncancellable forever. `f7700a4415af` also covers the `USER_RECOVERY` reset path, which matters before you build on [user recovery](/guide/recovery/).

**The one observed crash.** On x86_64 Ubuntu 6.17.0-14, a graceful stop of a busy device produced:

```text
BUG: kernel NULL pointer dereference, address: 0000000000000008
RIP: io_req_uring_cleanup+0x18
 io_uring_cmd_done <- ublk_dispatch_req <- ublk_cmd_tw_cb
 <- io_uring_cmd_work <- task_work_run <- get_signal
```

That is a ublk command being completed twice. The working hypothesis: a *handled* signal makes the server thread run pending io_uring task work from `get_signal` while it is still alive, and the driver's teardown guard in `ublk_dispatch_req` only checks for an exiting task, so it completes a command that cancellation has also completed. The io_uring core commit `3539b1467e94` (include the dying ring in task_work cancel state, 6.17-rc7) is related defense in depth, but mainline 6.17 already contains it, so the kernel that oopsed had it. Whether `845db023a8ae` and `f7700a4415af` (7.1) close this double completion has not been verified.

**How likely it is: unknown.** It happened once, during a systemd shutdown storm. It has not reproduced since: 150 teardown-under-load cycles plus a 24-combination integrity sweep on arm64 6.17.0-41, and 150 cycles on x86_64 6.17.0-1020-aws, a build that has none of the three fixes, were all clean. An earlier note claiming it reproduced "within 120 cycles" on arm64 was a test-harness bug: the harness parsed the "1" out of `USR1` and sent SIGINT to PID 1, rebooting the VM each time. Masking signals on the I/O threads does not prevent it in a Go server, because the Go runtime keeps unblocking the signals it manages.

If you can, run 7.1.y. If you cannot, the risk sits in graceful teardown under load, so stop devices when they are quiet where you can.

## Found by go-ublk's kernel matrix

Running one conformance suite under dozens of kernels turned up these. They affect any ublk server, not only go-ublk.

**Write-zeroes of 4 GiB or more is silently truncated before 6.11.** If a device advertises `max_write_zeroes_sectors` above `UINT32_MAX >> 9`, a zeroout that fits the limit is built as one bio whose 32-bit `bi_size` holds `nr_sects << 9`. `blkdiscard -z -l 5G` then succeeds, the server receives a 1 GiB write-zeroes, and the last 4 GiB keep their old contents. Seen on mainline 6.4, 6.6, 6.9 and 6.10, Ubuntu's 6.8 (the default 24.04 kernel) and openSUSE Leap 15.6; correct on 6.11.11 and later. A server should cap both `max_write_zeroes_sectors` and `max_discard_sectors` at `UINT32_MAX >> 9`, rounded down to the logical block size, on every kernel. go-ublk does so since v0.2.0.

**`START_DEV` after `STOP_DEV` is not safe.** The control protocol appears to allow starting a stopped device again (see [the control plane](/guide/control-plane/)), but in practice it fails with `EBUSY` (Fedora 6.19 and 7.2.8, mainline 7.0.14), wedges the control plane (6.10 to 6.12), or oopses. On Arch's 7.2.8-arch1-2 the result was a NULL dereference in `ublk_queue_rq` called from `ublk_partition_scan_work`, a partition-scan read reaching a queue whose per-I/O state is gone. Delete a stopped device and add a new one.

**`QUIESCE_DEV` on a batch device before 7.3-rc3.** Draining a `UBLK_F_BATCH_IO` device with `QUIESCE_DEV` before a handoff leaves the queues' `force_abort` set, and I/O issued across the handoff fails: on Ubuntu 7.0.0-38 a writer got `EIO` in every attempt. Fixed by "ublk: clear force_abort in ublk_queue_reset_io_flags()" (`8a14be55bdc6`, 7.3-rc3, stable 7.2.7). On earlier kernels, hand off batch devices without `QUIESCE_DEV`: let go of the device and let `UBLK_F_USER_RECOVERY_REISSUE` requeue what was outstanding.

**Provided-buffer rings cannot be registered on Ubuntu's 6.8 kernels.** On Ubuntu 6.8.0-146 (24.04 GA) and 6.8.0-138 (22.04 HWE), `IORING_REGISTER_PBUF_RING` fails with `-EINVAL` for every valid registration: user memory or `IOU_PBUF_RING_MMAP`, any ring size. A registration with a non-zero reserved field, which the kernel must reject, is accepted instead. The changelog for 6.8.0-146 includes a backport of "io_uring/kbuf: use mem_is_zero()", which replaced the reserved-field check, and the backported check is inverted. Mainline 6.8.12, 6.9, Ubuntu's 6.11 and 7.0 are not affected. ublk itself uses provided-buffer rings only for batch I/O, which needs 7.0, but a server whose backend uses them for its own io_uring I/O (liburing's `io_uring_setup_buf_ring`, for example) fails on these kernels.

**UBSAN `array-index-out-of-bounds` in `io_buffer_register_bvec`.** Zero copy, through `UBLK_U_IO_REGISTER_IO_BUF` or automatic buffer registration, logs this on Fedora 42 (6.19.14) and Fedora 43 and 44 (7.2.8):

```text
UBSAN: array-index-out-of-bounds in io_uring/rsrc.c:1070:12
index 0 is out of range for type 'bio_vec [*]'
```

It is a false positive in io_uring, still present in 7.3-rc5. `struct io_mapped_ubuf` declares `bvec[] __counted_by(nr_bvecs)`, `io_alloc_imu` does not initialize `nr_bvecs`, and `io_buffer_register_bvec` fills `bvec[]` before it sets the count. With a compiler that understands `__counted_by` and `CONFIG_UBSAN_BOUNDS`, as on Fedora, every registration of a request that carries data is flagged. The array is allocated for `blk_rq_nr_phys_segments(rq)` entries, so nothing is written out of bounds. The only practical risk is a machine booted with `panic_on_warn`, where UBSAN reports panic. There, avoid zero copy until the kernel sets `nr_bvecs` before the loop.

## Smaller fixes worth having

These come from the Ubuntu and stable changelogs. Each is a reason to stay current within a kernel line.

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

The `dev_info` fix is a reminder for servers too: zero `struct ublksrv_ctrl_dev_info` and set only the fields `ADD_DEV` documents as inputs.

## Missing modules

`ublk_drv` is a module (`CONFIG_BLK_DEV_UBLK=m` on the distributions tested), and it is not always installed.

- **Ubuntu on AWS.** It ships in `linux-modules-extra-*-aws`, not in the base AWS kernel image. Without that package there is no `/dev/ublk-control`: install `linux-modules-extra-$(uname -r)`.
- **RHEL 10 family** (RHEL, CentOS Stream, AlmaLinux and Rocky 10). `ublk_drv` is there, but io_uring is disabled by default through the `kernel.io_uring_disabled` sysctl, so every ublk operation fails with a permission error. With `sysctl kernel.io_uring_disabled=0` go-ublk's whole suite passes on their 6.12 kernels. The RHEL 9 family (5.14) has no `ublk_drv` at all.
- **WSL2.** Microsoft's WSL2 kernel (6.6.87.2-microsoft-standard-WSL2 at the time of writing) is built without `ublk_drv`. Test in a VM.
- **Not loaded.** The module is not auto-loaded on first open of `/dev/ublk-control`: the control node is a misc device with a dynamic minor and the driver declares no device-name alias (checked in 6.17 and 7.3-rc5), so `/dev/ublk-control` only exists once the module is loaded. Run `modprobe ublk_drv`, or list it in `/etc/modules-load.d/`.

`modinfo ublk_drv` tells you whether the module exists for the running kernel; `ls -l /dev/ublk-control` whether it is loaded.

## Protocol rules that look like kernel bugs

Each of these has cost someone a hung machine. They are working as designed.

**`DEL_DEV` blocks while `/dev/ublkcN` is open.** It waits, interruptibly, until the last reference to the device is gone, and every open file on the char node holds one: the original descriptor, every `dup`, and any copy registered with io_uring as a fixed file. Close them all before `DEL_DEV`, or use `DEL_DEV_ASYNC` (6.9) if you cannot wait. See the [control plane](/guide/control-plane/).

**`STOP_DEV` needs a live server.** Stopping the device runs `del_gendisk`, which waits for every in-flight request to complete, and only your queue threads can complete them. Stop the device first, keep the queues serving until `STOP_DEV` returns, then shut the queues down and delete. Cancelling the queues first leaves `STOP_DEV` waiting forever on a busy device.

**The descriptor mmap stride is fixed.** Queue `q`'s descriptors live at offset `q * round_up(UBLK_MAX_QUEUE_DEPTH * sizeof(struct ublksrv_io_desc), PAGE_SIZE)`: 98304 bytes with 4 KiB pages, 131072 with 64 KiB pages, whatever the queue depth. A stride computed from the actual depth rounds every queue down to queue 0, so the other queues read queue 0's descriptors, which shows up as data corruption and unkillable I/O hangs that scale with the queue count. See the [data plane](/guide/data-plane/).

**Sectors are 512 bytes.** `start_sector`, `nr_sectors`, `dev_sectors` and `max_sectors` are in 512-byte units on every device, including 4Kn devices.

**Range operations complete with 0.** For `FLUSH`, `DISCARD` and `WRITE_ZEROES` the kernel only checks that the result is not negative. Reporting the byte count instead overflows the 32-bit result for any range of 2 GiB or more, and the kernel then fails the request with `EIO`; a whole-device discard from `mkfs` hits this on any device larger than 2 GiB. See [I/O operations](/guide/io-operations/).

**One opener.** `/dev/ublkcN` admits one open file at a time; a second `open` fails with `-EBUSY`, and the process that opened it is the only valid PID for `START_DEV` and `END_USER_RECOVERY`.

**Unprivileged servers die on timeouts.** For an [unprivileged device](/guide/unprivileged/), a request timeout makes the kernel `SIGKILL` the server.

## Reboot hangs: deployment, not the kernel

A ublk server that runs as a bare background process, with a filesystem mounted on its device, can lose that filesystem's final writeback at every reboot and wedge the reboot outright about one time in five: systemd kills the server at the start of shutdown, before the filesystem unmounts. Ordering the mount after the server's systemd unit removes both. This is a deployment requirement for any ublk server, described with a working unit in [go-ublk deployment](/go-ublk/deployment/).
