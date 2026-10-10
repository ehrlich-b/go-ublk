# Dispatch release-candidate decision

The retained behavior is the measured `77b8be0` candidate: goroutine dispatch
remains the library and example default. Bryan's default choice and final
Linux acceptance are still required. This closeout adds verification tooling;
it changes no dispatch implementation, default, wake mode, or copy mode.

The October 10 RUNLOG, 09:55–13:10, supersedes the epic's noisy pilot table.
The full ladder completed **264/264 observations**, three rounds, with
matched null geometry of two queues/depth 64. Go inline and Rust both recorded
**13.9 µs qd1 p50 and 751k depth IOPS**; C recorded **13.2 µs**. CPU cost was
4,028 / 4,007 / 3,982 ns/I/O respectively. These are earlier measured receipts,
not results of this closeout or evidence for a new binary hash. The earlier
138-observation baseline measured a 6% randread A/A floor.

Auto's RAM A/B recorded 42.9k qd1 / 662k depth versus inline's 43.2k / 667k.
On blocking loop, Auto recorded 18.9k / 461k versus goroutine's 19.0k / 457k;
forced inline lost 15% at depth. Pool offered no material benefit. MSG_RING
offered no gain, and spin lost 19% depth throughput. Keep pool/adaptive opt-in;
exclude rejected wake experiments from the recommended candidate. Adaptive's
crowded path still launches goroutines. Its singleton allocation result does
not establish zero allocations at depth or a performance improvement.

Recommendation, conditional on the gates: choose Auto for declared nonblocking
handlers and retain goroutine concurrency for others. A declaration includes
every operation and synchronous observer; an incorrect promise can stall a
queue. Bryan decides whether to change the default. G6 is the final matched
tail check. G4 batching and G7 copy-mode expansion stay outside this closeout.

`77b8be0` passed four Linux test binaries on 6.12. The old six-kernel matrix
belongs to an earlier head. Final six-kernel correctness and matched
performance are **REQUIRED-UNRUN** until the checker consumes complete raw
receipts for the frozen closeout manifest. Portable fixtures are not host
measurements. Nothing in this note authorizes a release or site deployment.

## Reproduction

From the standalone clone root, use the clone-local caches and offline settings
in the brief. On macOS prefix compute commands with `taskpolicy -b nice -n 15`.
The runner uses the same wrapper for its preparation subprocesses. Go operations
always use Make, `GOFLAGS=-p=2`, `GOMAXPROCS=2`, and `make -j1`.

```sh
make -j1 check-dispatch-linux
make -j1 test-dispatch-portable
make -j1 test-perf-cleanup
make -j1 dispatch-vm-binaries
bash -n scripts/closeout-dispatch.sh
bash scripts/closeout-dispatch.sh --dry-run
python3 scripts/test-closeout-dispatch.py
git diff --check
# Commit the verified candidate locally before freezing its source identity.
bash scripts/closeout-dispatch.sh --prepare --out .scratch/closeout
```

Preparation requires a clean committed candidate descended from the immutable
`refs/closeout/base-77b8be0`. It runs the local checks, cross-builds the suite
through `make suite`, and exports source, seven Linux amd64 executables, the
pinned benchmark harness, and the offline module cache. It refuses an existing
output directory. Preserve `manifest.sha256` independently when transferring
the bundle; hashes bind identity, not a signature or proof of build provenance.

The nested `.scratch/bench` clone must remain at `3dcf845`. No original bench
checkout, original untracked measurements, or existing kernel receipts are
modified or automatically admitted.

For the one quiet performance window, the coordinator must stage two clean
pinned source clones and their Linux binaries under this clone's `.scratch/`,
then provide `.scratch/closeout-reference-inputs.json` **before preparation**:

```json
{
  "ublksrv": {
    "checkout": ".scratch/references/ublksrv",
    "binaries": {
      "ublk": {"path": ".scratch/reference-bin/ublk", "sha256": "FULL_SHA256", "source_commit": "abbfea2b59184b26e7212ce3d4c47702450510df"},
      "ublk.null": {"path": ".scratch/reference-bin/ublk.null", "sha256": "FULL_SHA256", "source_commit": "abbfea2b59184b26e7212ce3d4c47702450510df"}
    },
    "libraries": {
      "libublksrv.so.0": {"path": ".scratch/reference-bin/libublksrv.so.0", "sha256": "FULL_SHA256", "source_commit": "abbfea2b59184b26e7212ce3d4c47702450510df"}
    }
  },
  "libublk-rs": {
    "checkout": ".scratch/references/libublk-rs",
    "binaries": {
      "ramdisk": {"path": ".scratch/reference-bin/ramdisk", "sha256": "FULL_SHA256", "source_commit": "479f3097e128d595877185781987d218fe78c047"},
      "null": {"path": ".scratch/reference-bin/null", "sha256": "FULL_SHA256", "source_commit": "479f3097e128d595877185781987d218fe78c047"}
    }
  }
}
```

These artifact/source assertions come from the coordinator. Preparation checks
their hashes and source pins and inspects the actual pinned CLI source. It
never fetches or builds competitors. Missing references still allow a
correctness-only package; its performance acceptance remains held. Preserve
that package and prepare a new one after staging references. Do not graft
new inputs or older outputs onto an existing manifest.
Stage the C library under its actual SONAME (the example uses `.so.0`); the
launcher and null helper remain together. Other system runtime dependencies,
including liburing/libudev, must already exist in the disposable guest.

Copy `guest-attestation.example.json` to `guest-attestation.json` beside the
manifest and fill exact hostname, kernel release and coordinator CPU/RAM
attestations on each separately authorized guest. The runner verifies Linux,
root, amd64, declared disposable opt-in, VM detection, fixed 4 GiB/no balloon,
assigned CPUs, empty ublk registrations, and all hashes before tests. Required
tools and `ublk_drv` must already be available; it performs no installs,
transport, VM operations, pinning, or kernel changes.

Run separately on **6.8, 6.12, 7.0, 7.2.6, 7.2.9, and 7.3-rc3**:

```sh
GO_UBLK_DISPOSABLE_TEST=1 bash scripts/closeout-dispatch.sh --guest \
  --manifest .scratch/closeout/manifest.json --out .scratch/closeout/results
python3 scripts/check-closeout.py --manifest .scratch/closeout/manifest.json \
  --results .scratch/closeout/results
```

Every guest runs `make -j1 test-unit`, `test-race`, `benchmark-dispatch`, the
four packaged test binaries, and the unfiltered `ublk-suite -scale 1` (actual
CLI inspected in `test/suite/main.go`). The runner captures full dmesg before
and after, proves the delta without clearing the log, and records absence of
remaining character registrations and block devices. It retains failures and
never deletes an unrelated device. The known 7.0–7.2 zero-copy
`io_buffer_register_bvec` UBSAN signature is classified separately; every
other warning remains a failure, including that signature on 7.3-rc3.

Only 6.12 collects the new quiet window: 48 fresh devices and 288 timed
observations, pilot and interleaved A/A, three rotated A/B rounds. RAM uses
1 queue/depth 128 to match the pinned Rust example; null and loop use 2/64.
The working range is always 512 MiB and the logical block size 512 bytes.
Upstream C/Rust null devices retain their explicitly labelled fixed 250 GiB
capacity; no whole-device capacity equality is claimed. Six unchanged harness
workloads retain CPU-ns/I/O and pooled p50/p99/p99.9 histograms. Kernel inline
completion latency never enters userspace round-trip acceptance.

The checker requires no workload regression beyond its measured A/A floor,
zero successful singleton hot-path allocations, a qd1 null p50 gap ≤10% to
both references, matched-depth throughput ≥Rust, and p99 ≤2×Rust. A/A noise
above 10% holds acceptance. RAM/null inline and Auto must beat twice the
measured qd1 noise floor; equivalent loop behavior is not called a win.
Equality at Rust's throughput bound is acceptance, not a superiority claim.
Missing kernels, altered hashes, incomplete rounds, invalid CPU intervals,
missing cleanup, or missing tail distributions return nonzero. Return each
kernel directory unchanged; run the checker once over the collected matrix.
