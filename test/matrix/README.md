# Kernel matrix

Boots go-ublk's real-device tests under many Linux kernels and distro kernels,
in QEMU, without root on the host, and writes machine-readable results. The
same scripts run on the WSL rig (QEMU under rootless docker, TCG) and in
GitHub Actions (`.github/workflows/kernel-matrix.yml`, KVM).

## How a run works

1. **Fetch** (`fetch.py`) downloads each kernel's packages into
   `$MATRIX_HOME/cache/downloads/<id>/` with a `fetch.json` (source URL,
   sha256 per package, status). Two sources:
   - Ubuntu mainline builds (kernel.ubuntu.com/mainline): the latest point
     release of every series from v6.0 plus the newest -rc. A series whose
     latest amd64 build failed falls back to an earlier point release, and the
     failure is recorded.
   - Distro kernels (`distros.tsv`), fetched with the distro's own package
     manager inside the distro's own docker image (`apt-get download`,
     `dnf download`, `zypper download`, `pacman -Sw`). Superseded Ubuntu
     builds come from Launchpad (`method launchpad`), so an exact kernel such
     as 7.0.0-38 stays reproducible after it leaves the archive.
2. **Extract** (`extract.sh` + `kmods.py`) unpacks the packages and keeps only
   the kernel and the modules a guest needs: `ublk_drv`, ext4/xfs and their
   dependency closure (soft deps included), decompressed and re-`depmod`ed so
   any kmod can load them. The result is `$MATRIX_HOME/cache/kernels/<id>/`:
   `vmlinuz`, `modules.cpio` and `kinfo.json` (kver, whether ublk_drv exists:
   `module`, `builtin` or `false`, relevant `CONFIG_` values).
3. **Build** the guest:
   - `build-userland.sh`: a minimal Alpine root (busybox, bash, coreutils,
     util-linux, procps, e2fsprogs, xfsprogs, fio, kmod), the matrix
     `guest/init` and a pass-through `sudo`, built with `apk --root` inside an
     alpine container and packed by `mkcpio.py` (uncompressed newc, root-owned,
     device nodes without mknod).
   - `build-payload.sh`: static (`CGO_ENABLED=0`) linux binaries from the
     checkout: `ublk-suite` (test/suite), `ublk-probe` (GET_FEATURES),
     `ublk-mem`, `ublk-loop`, `verify`, every package's unit tests
     (`go test -c`) and the integration test binary, plus `payload/run` and
     the repo's `scripts/vm-verify.sh` and `scripts/vm-loop-e2e.sh`.
4. **Boot** (`run-one.sh`, `run-matrix.sh`): the initrd is
   `userland.cpio + payload.cpio + modules.cpio` concatenated. QEMU boots it
   with `-kernel/-initrd`, two serial ports and no other devices. `/init`
   mounts proc/sys/devtmpfs, loads `ublk_drv`, runs `/payload/run`, dumps
   `dmesg`, and powers off. ttyS0 is the console (kernel log plus test
   output, saved as `console.log`). ttyS1 carries only result JSON lines
   (`results.log`), so results never interleave with printk. A guest that
   outlives `TIMEOUT` is killed and recorded as `timeout`: a hung kernel is a
   result. The test that was in flight is named.
5. **Report** (`report.py`): `parse` turns one guest's logs into `run.json`,
   flagging oopses, BUGs, hung tasks, soft lockups and panics from the console.
   `aggregate` merges runs into `matrix.json` (the docs-site schema, exactly),
   `summary.md`, `runs-detail.json` (srcversion, feature names, wall time,
   CONFIG values, oops lines) and `kernels.json` (the fetch manifest).

Run status, per kernel: `pass`, `fail` (any test failed, or an oops),
`timeout`, `no-ublk` (the kernel ships no ublk_drv), `boot-failed` (init
never reported), `fetch-failed`.

## Payload contract

`/payload/run` writes one JSON object per line on stdout. Each line is either
a result, `{"test", "status": "pass|fail|skip|error|timeout", "duration_s",
"detail"}`, or metadata, `{"meta": {...}}`, which is merged into the run
record. Human output goes to stderr. A `=== RUN <name>` line on stderr before
each test lets the host name the hung test. Any directory with an executable
`run` that follows this contract can replace the payload.

The current payload runs these groups (select some with `TESTS=`):

| group | what |
|---|---|
| `probe` | `uname -r`, `UBLK_U_CMD_GET_FEATURES` bits, ublk_drv srcversion (`cmd/ublk-probe`; the library has no GET_FEATURES call) |
| `suite` | `ublk-suite -scale $SCALE`, which streams one result per conformance test |
| `unit` | every package's unit tests, as root, against the guest's real io_uring |
| `largeio` | `TestDisposableLargeIOPublicRunnerPaths` (integration tag) |
| `verify` | `scripts/vm-verify.sh`: the shadow-oracle integrity sweep, Q 1/2/4/8 x depth 1/64/128 x buffered/O_DIRECT |
| `loop` | `scripts/vm-loop-e2e.sh`: ublk-loop file mapping, discard, write-cache modes, read-only, ublk-mem --zip |
| `wzcheck` | opt-in: one 5 GiB BLKZEROOUT on a sparse 6 GiB ublk-mem --zip device. It records write_zeroes_max_bytes, the write I/O and sector deltas (one WRITE_ZEROES vs a zero-page fallback), and whether patterns planted across the range were zeroed |

After each device group, a hygiene check reaps leftover daemons and devices
and samples D-state processes three times. A leak, or a process stuck in D in
every sample, is a failure of that group.

## Running on the WSL rig (TCG, rootless docker)

The rig has docker but no qemu, cpio or rpm2cpio, so those steps run in the
toolbox image. Put this in `Makefile.local`:

```make
MATRIX_RUNNER = docker run --rm -v $(MATRIX_HOME):$(MATRIX_HOME) -v $(CURDIR):$(CURDIR) \
                -w $(CURDIR) -e MATRIX_HOME=$(MATRIX_HOME) goublk-matrix
```

Then:

```bash
make matrix-toolbox                      # once: docker build -t goublk-matrix test/matrix
make matrix-fetch                        # every mainline series + every distros.tsv row
make matrix-fetch DISTROS='fedora-* arch' MAINLINE_LATEST=3
make matrix-extract
make matrix-run JOBS=5                   # all extracted kernels
make matrix-run KERNELS='mainline-7.* ubuntu-24.04-*' TESTS='probe suite'
make matrix-report                       # latest run -> test/matrix/results/
```

`make matrix-run` blocks. For a long run on the rig, start it detached so it
does not depend on the ssh session (the WSL distro still has to stay up, so
hold a keepalive session):

```bash
M=$HOME/goublk-matrix
docker run -d --name goublk-matrix-run -v $M:$M -e MATRIX_HOME=$M -e JOBS=5 \
  -w $M/src goublk-matrix bash test/matrix/run-matrix.sh
```

Knobs (environment or make variables): `ACCEL=tcg|kvm`, `QEMU=` (binary or
wrapper), `JOBS` (concurrent guests, default 4), `MEM` (MiB per guest,
default 2048), `SMP` (default 2), `TIMEOUT` (seconds per guest, default 3000
under TCG and 1200 under KVM), `SCALE` (ublk-suite `-scale`, default 0.25
under TCG and 1 under KVM), `PROFILE=quick` (shorter integrity sweep),
`TESTS`, `SUITE_RUN` / `SUITE_SKIP` (ublk-suite `-run` / `-skip` regexps, no
spaces), `EXTRA_APPEND` (kernel command line) and `ID_SUFFIX` (records a
variant as its own row). For example, the RHEL 10 rows with io_uring turned
on:

```bash
ID_SUFFIX=+io_uring EXTRA_APPEND=sysctl.kernel.io_uring_disabled=0 \
  bash test/matrix/run-matrix.sh 'centos-stream-10 almalinux-10 rocky-10'
```

When a guest wedges (a leaked device, or a task in D state in every sample),
the payload prints diagnostics to the console before moving on:
`/proc/partitions`, the stuck tasks' `/proc/PID/stack`, the ublk lines of the
kernel log, and a sysrq-w dump of every blocked task.

Under TCG, a guest reaches `/init` in about 2 s. The full payload takes
minutes, mostly in the integrity sweep and the unit tests. Budget about
2.3 GB of host RAM per concurrent guest (2 GB guest plus QEMU and a 256 MB
TCG translation cache).

## Running in CI (KVM)

`.github/workflows/kernel-matrix.yml` makes `/dev/kvm` world-accessible with
a udev rule, installs qemu and the unpacking tools natively (no toolbox, so
`MATRIX_RUNNER` stays empty), caches `cache/kernels`, and runs a modest kernel
set by default, or the full set via `workflow_dispatch` with
`kernels: full`. A second job runs the payload natively on the runner's own
kernel (`make matrix-native`, recorded as boot `full-vm` and accel `native`).

## Results

`test/matrix/results/` holds the summarized output of real runs:
`matrix.json`, `summary.md`, `runs-detail.json` and `kernels.json`. Raw
console logs stay under `$MATRIX_HOME/runs/<run-id>/<kernel-id>/`.

## Full-distro mode

See `full-distro/README.md`: a cloud image booting its own systemd, for the
systemd/udev integration tests that an initramfs cannot exercise.
