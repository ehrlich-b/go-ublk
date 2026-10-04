---
title: "Known kernel bugs"
linkTitle: "Known kernel bugs"
description: "Kernel and distribution bugs that bite ublk servers: which builds have them, what they look like, and how to avoid them."
weight: 140
---

ublk is young and the driver changes every release, so the kernel you run matters as much as your server code. This page collects the kernel and distribution problems found while testing go-ublk on Ubuntu 6.17 and 7.0, on arm64 and x86_64, plus the fixes from upstream and Ubuntu changelogs that a server author should know about. None of them is specific to go-ublk: the reference servers hit the same ones. It also lists protocol rules that look like kernel bugs the first time they bite.

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
| Ubuntu `linux-azure` 6.17 | 6.17.0-1015 ([Launchpad bug 2154635](https://bugs.launchpad.net/bugs/2154635)) | <!-- VERIFY: first fixed linux-azure 6.17 build --> later builds |
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

That is a ublk command being completed twice. The working hypothesis: a *handled* signal makes the server thread run pending io_uring task work from `get_signal` while it is still alive, and the driver's teardown guard in `ublk_dispatch_req` only checks for an exiting task, so it completes a command that cancellation has also completed. The io_uring core commit `3539b1467e94` (include the dying ring in task_work cancel state) is related defense in depth. <!-- VERIFY: which release first contains 3539b1467e94 and whether the 845db023a8ae/f7700a4415af pair fully closes the io_req_uring_cleanup double completion -->

**How likely it is: unknown.** It happened once, during a systemd shutdown storm. It has not reproduced since: 150 teardown-under-load cycles plus a 24-combination integrity sweep on arm64 6.17.0-41, and 150 cycles on x86_64 6.17.0-1020-aws, a build that has none of the three fixes, were all clean. An earlier note claiming it reproduced "within 120 cycles" on arm64 was a test-harness bug: the harness parsed the "1" out of `USR1` and sent SIGINT to PID 1, rebooting the VM each time. Masking signals on the I/O threads does not prevent it in a Go server, because the Go runtime keeps unblocking the signals it manages.

If you can, run 7.1.y. If you cannot, the risk sits in graceful teardown under load, so stop devices when they are quiet where you can.

## Smaller fixes worth having

These come from the Ubuntu and stable changelogs. Each is a reason to stay current within a kernel line.

| Fix | What it changes | Where it landed |
|---|---|---|
| `1860c2f85922` ublk: reject max_sectors smaller than PAGE_SECTORS in parameter validation | A `max_sectors` below one page used to pass `SET_PARAMS` and trip a `WARN_ON_ONCE` at `START_DEV`; now `SET_PARAMS` fails with `-EINVAL` | stable 7.0.11; `linux-hwe-7.0` 7.0.0-28 |
| ublk: wait on ublk_dev_ready() instead of ub->completion (CVE-2026-68173) | How `START_DEV` and recovery wait for the queues to become ready <!-- VERIFY: user-visible effect of this fix --> | `linux-hwe-7.0` 7.0.0-38 |
| ublk: reset kernel-owned dev_info fields in ublk_ctrl_add_dev() (CVE-2026-74472) | `ADD_DEV` no longer keeps kernel-owned fields of the caller's `ublksrv_ctrl_dev_info` | `linux-hwe-7.0` 7.0.0-39 (proposed) |
| ublk: clear VM_MAYWRITE on read-only ublk char device mmap (CVE-2026-89793) | The read-only descriptor mapping can no longer be made writable later <!-- VERIFY: the exact exposure fixed --> | `linux-hwe-7.0` 7.0.0-39 (proposed) |
| ublk: fix use-after-free in ublk_partition_scan_work | Use-after-free in the asynchronous partition scan that Ubuntu backported into 6.17.0-24 | `linux-hwe-6.17` 6.17.0-41 |
| ublk: fix ublksrv pid handling for pid namespaces | Servers running in a PID namespace | `linux-hwe-6.17` 6.17.0-41 |
| ublk: fix deadlock when reading partition table (CVE-2025-68823) | Deadlock during the partition scan at `START_DEV` | `linux-hwe-6.17` 6.17.0-22 |
| ublk: clean up user copy references on ublk server exit (CVE-2025-71070) | Leaked request references after a user-copy server exits | `linux-hwe-6.17` 6.17.0-22 |

The `dev_info` fix is a reminder for servers too: zero `struct ublksrv_ctrl_dev_info` and set only the fields `ADD_DEV` documents as inputs.

## Missing modules

`ublk_drv` is a module (`CONFIG_BLK_DEV_UBLK=m` on the distributions tested), and it is not always installed.

- **Ubuntu on AWS.** It ships in `linux-modules-extra-*-aws`, not in the base AWS kernel image. Without that package there is no `/dev/ublk-control`: install `linux-modules-extra-$(uname -r)`.
- **WSL2.** Microsoft's WSL2 kernel (6.6.87.2-microsoft-standard-WSL2 at the time of writing) is built without `ublk_drv`. Test in a VM.
- **Not loaded.** The module is not auto-loaded on first open of `/dev/ublk-control`. <!-- VERIFY: whether ublk_drv declares a devname/misc alias that lets udev create /dev/ublk-control and autoload the module --> Run `modprobe ublk_drv`, or list it in `/etc/modules-load.d/`.

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
