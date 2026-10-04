# Kernel-matrix results

Summarized output of real runs on the WSL rig: QEMU TCG, 2 vCPU, initramfs
boot. Raw console logs are not committed.

| Path | What |
|---|---|
| `matrix.json` | Current results: every kernel, against the go-ublk commit in its `go_ublk_commit` field. The schema is the one `site/layouts/shortcodes/matrix.html` renders |
| `summary.md` | The same, as a table, with the failing tests' details |
| `runs-detail.json` | Per run: wall time, boot time, ublk_drv srcversion, decoded feature names, kernel CONFIG values, oops lines, extra command line |
| `kernels.json` | Fetch manifest: source URLs and sha256 of every package, plus ublk_drv presence and config per kernel |
| `targeted/` | Reproduction runs: ctx-cancel-idle repeats, the 6.10 integrity sweep repeats, and wzcheck (BLKZEROOUT) before and after the 4 GiB cap |
| `oops-arch-7.2.8-ublk_queue_rq.txt` | Kernel NULL dereference in `ublk_queue_rq`, triggered by START_DEV on a stopped device |
| `evidence-restart-after-stop/` | Run m2 (595536b): what Start after Stop did to 31 kernels before go-ublk refused it |
| `evidence-pre-engine-615c9a7/` | Run m3 (615c9a7): partial sweep from before the new engine |
| `evidence-11f8769/` | Run m4 (11f8769): partial sweep with the new engine, before the 4 GiB zeroout cap. It is where the Fedora UBSAN report under zero copy was found |
