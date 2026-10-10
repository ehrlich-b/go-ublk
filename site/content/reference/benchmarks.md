---
title: "Comparative benchmarks"
linkTitle: "Benchmarks"
description: "RAM-backed block devices on a shared CPU budget, with measurement evidence and qualification limits."
weight: 30
wide: true
---

These measurements compare retained-RAM block devices on [Linux 6.12.111](/evidence/2026-10-10/bench-baseline-report.md) in a KVM guest pinned to [4 dedicated physical cores](/evidence/2026-10-10/lib-ublk-validation.md) of a [Ryzen 9 6900HX](/evidence/2026-10-10/bench-baseline-report.md). The server and fio share a [4-CPU budget](/evidence/2026-10-10/bench-baseline-manifest.json). Results are means from [3 interleaved rounds](/evidence/2026-10-10/bench-baseline-manifest.json); throughput dispersion is the sample standard deviation.

**No winner claims.** The [4 KiB random-read A/A noise floor is 6%](/evidence/2026-10-10/bench-baseline-report.md), and the quiet-window and matched-peer gates are incomplete. libublk-rs's unmodified ramdisk example fixes [1 queue at depth 128](/evidence/2026-10-10/bench-baseline-report.md), so it's unmatched. ublksrv uses a loop target over tmpfs with an extra backend I/O path; kernel-baseline workers can run outside the userspace CPU set. These are VM observations, where cross-CPU wakeups cost more than on bare metal.

## Implementations and geometry

All targets retain writes in RAM. null_blk is memory-backed here; a write-dropping null target isn't part of these tables. The requested server geometry was [2 queues at depth 64](/evidence/2026-10-10/bench-baseline-manifest.json). Source revisions and binary hashes are in the [report](/evidence/2026-10-10/bench-baseline-report.md).

| Implementation | Target | Accepted queues/depth | Revision |
|---|---|---|---|
| brd | In-kernel RAM disk | Kernel-managed | [Kernel build and module hash](/evidence/2026-10-10/bench-baseline-report.md) |
| null_blk | In-kernel, memory-backed | [2/64](/evidence/2026-10-10/bench-baseline-report.md) | [Kernel build and module hash](/evidence/2026-10-10/bench-baseline-report.md) |
| go-ublk | RAM example, default dispatch | [2/64](/evidence/2026-10-10/bench-baseline-report.md) | [1c800535eef6](/evidence/2026-10-10/bench-baseline-report.md) |
| lib-ublk | C RAM consumer, copy mode | [2/64](/evidence/2026-10-10/bench-baseline-report.md) | [e0743b3eb034](/evidence/2026-10-10/bench-baseline-report.md) |
| ublksrv | Loop over tmpfs | [2/64](/evidence/2026-10-10/bench-baseline-report.md) | [abbfea2b5918](https://github.com/ublk-org/ublksrv/commit/abbfea2b59184b26e7212ce3d4c47702450510df) |
| libublk-rs (unmatched) | Unmodified ramdisk example | [1/128](/evidence/2026-10-10/bench-baseline-report.md) | [479f3097e128](https://github.com/ublk-org/libublk-rs/commit/479f3097e128d595877185781987d218fe78c047) |

## Queue depth one and round-trip cost

[4 KiB random reads, one job at queue depth 1](/evidence/2026-10-10/bench-baseline-manifest.json). Effective time per I/O is `1,000,000 / IOPS`, in microseconds. It includes submission and completion; fio's completion-latency p50 and p99 are separate columns. The latencies reported here are means of the run-level quantiles, rather than percentiles pooled across rounds.

| Implementation | IOPS mean ± sd | Effective µs/I/O | Completion p50 µs | Completion p99 µs |
|---|---:|---:|---:|---:|
| brd | [390,597.38 ± 3,118.39](/evidence/2026-10-10/bench-baseline-report.md) | [2.56](/evidence/2026-10-10/bench-baseline-report.md) | [0.11](/evidence/2026-10-10/bench-baseline-report.md) | [0.13](/evidence/2026-10-10/bench-baseline-report.md) |
| null_blk (memory-backed) | [276,263.46 ± 3,374.41](/evidence/2026-10-10/bench-baseline-report.md) | [3.62](/evidence/2026-10-10/bench-baseline-report.md) | [0.11](/evidence/2026-10-10/bench-baseline-report.md) | [0.12](/evidence/2026-10-10/bench-baseline-report.md) |
| go-ublk | [22,985.96 ± 97.44](/evidence/2026-10-10/bench-baseline-report.md) | [43.50](/evidence/2026-10-10/bench-baseline-report.md) | [36.10](/evidence/2026-10-10/bench-baseline-report.md) | [56.75](/evidence/2026-10-10/bench-baseline-report.md) |
| lib-ublk | [38,219.41 ± 277.64](/evidence/2026-10-10/bench-baseline-report.md) | [26.16](/evidence/2026-10-10/bench-baseline-report.md) | [20.44](/evidence/2026-10-10/bench-baseline-report.md) | [29.48](/evidence/2026-10-10/bench-baseline-report.md) |
| ublksrv (loop over tmpfs) | [38,293.42 ± 408.02](/evidence/2026-10-10/bench-baseline-report.md) | [26.11](/evidence/2026-10-10/bench-baseline-report.md) | [20.35](/evidence/2026-10-10/bench-baseline-report.md) | [30.76](/evidence/2026-10-10/bench-baseline-report.md) |
| libublk-rs (unmatched) | [49,752.79 ± 774.90](/evidence/2026-10-10/bench-baseline-report.md) | [20.10](/evidence/2026-10-10/bench-baseline-report.md) | [14.53](/evidence/2026-10-10/bench-baseline-report.md) | [23.00](/evidence/2026-10-10/bench-baseline-report.md) |

The in-kernel RAM disks cost about [2.6-3.6 µs per I/O](/evidence/2026-10-10/bench-baseline-report.md) at queue depth one. The lowest measured userspace round-trip cost on this VM is about [20 µs](/evidence/2026-10-10/bench-baseline-report.md), from libublk-rs's unmatched ramdisk example. This measures the full userspace path on this VM; it doesn't isolate language or FFI cost.

## Random I/O

[4 KiB requests, 2 jobs at queue depth 32 each](/evidence/2026-10-10/bench-baseline-manifest.json). The mixed workload is [70% reads and 30% writes](/evidence/2026-10-10/bench-baseline-manifest.json). Throughput is the arithmetic mean, and latency columns are means of each run's I/O-weighted completion quantiles, as described in the [report](/evidence/2026-10-10/bench-baseline-report.md).

| Workload | Implementation | IOPS mean ± sd | Completion p50 µs | Completion p99 µs |
|---|---|---:|---:|---:|
| Read | brd | [766,865.82 ± 4,968.26](/evidence/2026-10-10/bench-baseline-report.md) | [80.04](/evidence/2026-10-10/bench-baseline-report.md) | [100.52](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | null_blk (memory-backed) | [544,246.95 ± 20,552.98](/evidence/2026-10-10/bench-baseline-report.md) | [112.47](/evidence/2026-10-10/bench-baseline-report.md) | [142.51](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | go-ublk | [559,235.16 ± 63,628.85](/evidence/2026-10-10/bench-baseline-report.md) | [94.38](/evidence/2026-10-10/bench-baseline-report.md) | [391.17](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | lib-ublk | [288,525.05 ± 3,654.98](/evidence/2026-10-10/bench-baseline-report.md) | [181.93](/evidence/2026-10-10/bench-baseline-report.md) | [436.22](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | ublksrv (loop over tmpfs) | [371,241.43 ± 10,898.70](/evidence/2026-10-10/bench-baseline-report.md) | [156.67](/evidence/2026-10-10/bench-baseline-report.md) | [246.78](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | libublk-rs (unmatched) | [649,358.42 ± 51,942.16](/evidence/2026-10-10/bench-baseline-report.md) | [95.74](/evidence/2026-10-10/bench-baseline-report.md) | [138.41](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | brd | [709,066.43 ± 5,878.78](/evidence/2026-10-10/bench-baseline-report.md) | [86.19](/evidence/2026-10-10/bench-baseline-report.md) | [105.30](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | null_blk (memory-backed) | [510,865.23 ± 2,451.67](/evidence/2026-10-10/bench-baseline-report.md) | [120.66](/evidence/2026-10-10/bench-baseline-report.md) | [143.70](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | go-ublk | [564,917.56 ± 59,862.36](/evidence/2026-10-10/bench-baseline-report.md) | [96.43](/evidence/2026-10-10/bench-baseline-report.md) | [239.62](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | lib-ublk | [254,681.93 ± 1,852.10](/evidence/2026-10-10/bench-baseline-report.md) | [230.40](/evidence/2026-10-10/bench-baseline-report.md) | [615.77](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | ublksrv (loop over tmpfs) | [371,706.58 ± 6,841.73](/evidence/2026-10-10/bench-baseline-report.md) | [174.42](/evidence/2026-10-10/bench-baseline-report.md) | [246.10](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | libublk-rs (unmatched) | [672,654.42 ± 18,111.73](/evidence/2026-10-10/bench-baseline-report.md) | [88.92](/evidence/2026-10-10/bench-baseline-report.md) | [129.71](/evidence/2026-10-10/bench-baseline-report.md) |
| Mixed | brd | [744,896.21 ± 4,605.36](/evidence/2026-10-10/bench-baseline-report.md) | [82.77](/evidence/2026-10-10/bench-baseline-report.md) | [100.52](/evidence/2026-10-10/bench-baseline-report.md) |
| Mixed | null_blk (memory-backed) | [528,157.22 ± 1,453.66](/evidence/2026-10-10/bench-baseline-report.md) | [116.57](/evidence/2026-10-10/bench-baseline-report.md) | [136.19](/evidence/2026-10-10/bench-baseline-report.md) |
| Mixed | go-ublk | [562,469.21 ± 66,902.00](/evidence/2026-10-10/bench-baseline-report.md) | [94.04](/evidence/2026-10-10/bench-baseline-report.md) | [253.95](/evidence/2026-10-10/bench-baseline-report.md) |
| Mixed | lib-ublk | [272,672.96 ± 10,247.33](/evidence/2026-10-10/bench-baseline-report.md) | [199.68](/evidence/2026-10-10/bench-baseline-report.md) | [531.11](/evidence/2026-10-10/bench-baseline-report.md) |
| Mixed | ublksrv (loop over tmpfs) | [361,391.55 ± 1,901.42](/evidence/2026-10-10/bench-baseline-report.md) | [165.55](/evidence/2026-10-10/bench-baseline-report.md) | [269.65](/evidence/2026-10-10/bench-baseline-report.md) |
| Mixed | libublk-rs (unmatched) | [661,882.10 ± 14,165.26](/evidence/2026-10-10/bench-baseline-report.md) | [94.38](/evidence/2026-10-10/bench-baseline-report.md) | [124.76](/evidence/2026-10-10/bench-baseline-report.md) |

## Sequential I/O

[128 KiB requests, one job at queue depth 8](/evidence/2026-10-10/bench-baseline-manifest.json). Throughput is in MiB/s.

| Workload | Implementation | MiB/s mean ± sd | Completion p50 µs | Completion p99 µs |
|---|---|---:|---:|---:|
| Read | brd | [9,474.19 ± 166.03](/evidence/2026-10-10/bench-baseline-report.md) | [90.28](/evidence/2026-10-10/bench-baseline-report.md) | [122.37](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | null_blk (memory-backed) | [6,418.74 ± 17.47](/evidence/2026-10-10/bench-baseline-report.md) | [147.11](/evidence/2026-10-10/bench-baseline-report.md) | [191.49](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | go-ublk | [11,589.88 ± 493.15](/evidence/2026-10-10/bench-baseline-report.md) | [72.53](/evidence/2026-10-10/bench-baseline-report.md) | [177.15](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | lib-ublk | [8,860.57 ± 294.66](/evidence/2026-10-10/bench-baseline-report.md) | [96.77](/evidence/2026-10-10/bench-baseline-report.md) | [186.03](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | ublksrv (loop over tmpfs) | [6,080.95 ± 136.59](/evidence/2026-10-10/bench-baseline-report.md) | [162.82](/evidence/2026-10-10/bench-baseline-report.md) | [262.83](/evidence/2026-10-10/bench-baseline-report.md) |
| Read | libublk-rs (unmatched) | [10,327.52 ± 267.79](/evidence/2026-10-10/bench-baseline-report.md) | [85.16](/evidence/2026-10-10/bench-baseline-report.md) | [149.16](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | brd | [7,080.15 ± 103.82](/evidence/2026-10-10/bench-baseline-report.md) | [122.03](/evidence/2026-10-10/bench-baseline-report.md) | [165.55](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | null_blk (memory-backed) | [5,811.51 ± 17.08](/evidence/2026-10-10/bench-baseline-report.md) | [163.50](/evidence/2026-10-10/bench-baseline-report.md) | [213.33](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | go-ublk | [8,047.54 ± 373.92](/evidence/2026-10-10/bench-baseline-report.md) | [109.06](/evidence/2026-10-10/bench-baseline-report.md) | [246.78](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | lib-ublk | [6,745.41 ± 192.39](/evidence/2026-10-10/bench-baseline-report.md) | [140.29](/evidence/2026-10-10/bench-baseline-report.md) | [184.66](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | ublksrv (loop over tmpfs) | [4,780.24 ± 528.82](/evidence/2026-10-10/bench-baseline-report.md) | [199.00](/evidence/2026-10-10/bench-baseline-report.md) | [339.29](/evidence/2026-10-10/bench-baseline-report.md) |
| Write | libublk-rs (unmatched) | [7,095.53 ± 308.39](/evidence/2026-10-10/bench-baseline-report.md) | [133.97](/evidence/2026-10-10/bench-baseline-report.md) | [205.82](/evidence/2026-10-10/bench-baseline-report.md) |

## Go dispatch experiment

A separate [interleaved A/B run](/go-ublk/performance/#dispatch-measurements) on the same VM compares go-ublk's default goroutine-per-request dispatch with `DeviceParams.Inline`. At queue depth one, RAM throughput changed from [20.3k to 43.5k IOPS](/evidence/2026-10-10/go-dispatch-ab-results.tsv). Those results are medians from a separate experiment; the go-ublk rows above retain the measured baseline means.

## Evidence files

- [Benchmark report](/evidence/2026-10-10/bench-baseline-report.md): all workload means, dispersion, CPU measurements, A/A controls, source revisions, and binary hashes.
- [Benchmark manifest](/evidence/2026-10-10/bench-baseline-manifest.json): workload geometry, seed, runtime, warmup, and interleaved schedule.
- [Go dispatch results](/evidence/2026-10-10/go-dispatch-ab-results.tsv) and [metadata](/evidence/2026-10-10/go-dispatch-ab-metadata.txt): per-round latency, syscall, and context-switch measurements.

The hosted report and metadata are sanitized extracts. Raw fio JSON and guest lifecycle logs aren't included in these downloads.
