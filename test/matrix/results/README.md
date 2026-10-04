# Kernel-matrix results

Summarized output of real runs on the WSL rig: QEMU TCG, 2 vCPU,
possible_cpus=8, initramfs boot. Raw console logs are not committed.

`matrix.json` is the v0.2.0 release-candidate sweep, pinned to go-ublk
4d9712f (`go_ublk_commit`). Each of the 47 kernels that ship ublk_drv ran the
full payload (probe, unit tests, large-io, integrity sweep, loop e2e, the
whole ublk-suite), and so did the RHEL 10 `+io_uring` variant rows. The 13
kernels without ublk_drv keep their rows from the 0aa26d9 sweep, since their
result cannot change. Two full-distro rows (boot `full-vm`) come from the
stock Ubuntu 24.04 cloud image under its own systemd: as shipped it has no
ublk_drv (`no-ublk`), and with linux-modules-extra it passes.
`runs-detail.json` records the commit of every row.

Reading the statuses:

- RHEL 10 default rows `fail`: io_uring is disabled out of the box
  (kernel.io_uring_disabled). Every failure is the same EPERM, and go-ublk's
  message names the sysctl. The `+io_uring` rows, booted with
  `sysctl.kernel.io_uring_disabled=0`, pass.
- `ubuntu-24.04-hwe-6.17.0-40` `fail`: the negative control. It is the Ubuntu
  build with the ADD_DEV NULL dereference in `ublk_init_queues`, which the
  oops detector catches. The guest was wedged by the oops, so the operator
  terminated it; that termination is the "guest died" error on verify-sweep.
- `kernel-log/io_uring-counted_by-ubsan` (status skip) is a known kernel bug,
  not a product failure: a UBSAN `__counted_by` false positive in
  `io_buffer_register_bvec` under zero copy. See the kernel-bugs guide page.

| Path | What |
|---|---|
| `matrix.json` | Current results, in the schema `site/layouts/shortcodes/matrix.html` renders. It is copied to `site/data/matrix.json` |
| `summary.md` | The same, as a table, with the failing tests' details |
| `runs-detail.json` | Per run: commit, wall time, boot time, kernel command line, ublk_drv srcversion, decoded feature names, kernel CONFIG values, oops lines, known kernel bugs |
| `kernels.json` | Fetch manifest: source URLs and sha256 of every package, plus ublk_drv presence and config per kernel |
| `targeted/` | Reproduction runs: ctx-cancel-idle repeats, the 6.10 integrity-sweep repeats, wzcheck (BLKZEROOUT) before and after the 4 GiB cap, and the 6.17.0-40 negative control with more CPUs |
| `oops-arch-7.2.8-ublk_queue_rq.txt` | Kernel NULL dereference in `ublk_queue_rq`, triggered by START_DEV on a stopped device |
| `evidence-restart-after-stop/` | Run m2 (595536b): what Start after Stop did to 31 kernels before go-ublk refused it |
| `evidence-pre-engine-615c9a7/` | Run m3 (615c9a7): partial sweep from before the new engine |
| `evidence-11f8769/` | Run m4 (11f8769): partial sweep with the new engine, before the 4 GiB zeroout cap |
