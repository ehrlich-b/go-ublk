---
title: "Comparative benchmarks"
linkTitle: "Benchmarks"
description: "The ublk overhead ladder: kernel floors, userspace transport, and RAM targets, with latency and CPU costs."
weight: 30
wide: true
---

The overhead ladder separates the kernel floor, userspace transport, and RAM data work. This completed run used [Linux 6.12.111 in a KVM guest on 4 dedicated physical cores](/evidence/2026-10-10/ladder-report.md) of a [Ryzen 9 6900HX](/evidence/2026-10-10/ladder-report.md). Fio ran on [vCPUs 0-3, and the server on 4-7](/evidence/2026-10-10/ladder-manifest.json), with disjoint CPU sets. There are [3 interleaved rounds per target and workload, with 264/264 valid observations](/evidence/2026-10-10/ladder-report.md).

## Measured transport cost

- ublksrv, libublk-rs, and inline go-ublk add about [13 µs of completion latency at qd1](/evidence/2026-10-10/ladder-report.md) over the kernel null_blk floor. At depth, random reads cost about [1,100 extra CPU-ns/IO, or 40%](/evidence/2026-10-10/ladder-report.md) over that floor.
- go-ublk with `DeviceParams.Inline` ties the C and Rust reference servers on transport cost in this run: [13.89 µs qd1 p50 and 4,027.94 CPU-ns/IO for random reads](/evidence/2026-10-10/ladder-report.md). Its default goroutine mode adds about [35 µs over the kernel floor](/evidence/2026-10-10/ladder-report.md).
- lib-ublk's serving path is currently the slowest at depth, with about [20 µs qd1 p50 and a 300k IOPS ceiling](/evidence/2026-10-10/ladder-report.md). Work on the serving path is ongoing.
- At depth, the C, Rust, and inline Go null targets reach the kernel rate: about [751k-757k IOPS for random reads, against null_blk's 698k](/evidence/2026-10-10/ladder-report.md). Fio limits throughput there; CPU-ns/IO separates their transport costs.

**No winner claims.** The [report's qualification gates](/evidence/2026-10-10/ladder-report.md) still lack quiet-window attestation and complete peer controls. These are VM measurements; cross-CPU wakeups cost more here than on bare metal. Null targets discard writes. lib-ublk's null example also zero-fills reads, and ublksrv's RAM target includes loop/tmpfs backend I/O. Kernel workers can run outside the userspace CPU sets. Rust's RAM geometry is unmatched.

## Reading the ladder

L0 is null_blk without stored data. L1 adds kernel RAM storage through brd or memory-backed null_blk. L2 measures the userspace null targets, and L3 adds retaining-RAM targets.

The requested device geometry was [2 queues at depth 64](/evidence/2026-10-10/ladder-manifest.json). All userspace targets accepted it except libublk-rs's RAM example, which fixes [1 queue at depth 128](/evidence/2026-10-10/ladder-report.md). Its null example accepted the requested geometry. brd has kernel-managed geometry.

**Every qd1 p50 column comes from the separate [4 KiB random-read run with one job at depth 1](/evidence/2026-10-10/ladder-manifest.json)**, including the write and mixed tables. Kernel devices complete inline through io_uring; their reported [roughly 0.1 µs](/evidence/2026-10-10/ladder-report.md) measures completion latency alone and excludes submission. The tables show **inline** for those rows. The userspace p50 values also measure completion latency; they aren't full round-trip times.

IOPS and CPU-ns/IO come from each named workload at [2 jobs × queue depth 32](/evidence/2026-10-10/ladder-manifest.json). Values are means of the primary runs; latency values are means of run-level quantiles. CPU-ns/IO includes fio and server user/system CPU plus attributed residual kernel CPU per completed I/O. It excludes kernel workers outside the benchmark cgroups, unattributed interrupts, and other guest or host work. The [report](/evidence/2026-10-10/ladder-report.md) gives the accounting method and sample standard deviations.

## 4 KiB random reads

| Rung | Target | qd1 p50 µs | 2×qd32 IOPS | CPU-ns/IO |
|---|---|---:|---:|---:|
| L0 | [null_blk (`memory_backed=0`)](/evidence/2026-10-10/ladder-report.md) | inline | [697,574.16](/evidence/2026-10-10/ladder-report.md) | [2,872.83](/evidence/2026-10-10/ladder-report.md) |
| L1 | brd | inline | [748,901.96](/evidence/2026-10-10/ladder-report.md) | [2,673.59](/evidence/2026-10-10/ladder-report.md) |
| L1 | null_blk (memory-backed) | inline | [518,206.85](/evidence/2026-10-10/ladder-report.md) | [3,869.35](/evidence/2026-10-10/ladder-report.md) |
| L2 | ublksrv (null) | [13.16](/evidence/2026-10-10/ladder-report.md) | [756,632.54](/evidence/2026-10-10/ladder-report.md) | [3,981.74](/evidence/2026-10-10/ladder-report.md) |
| L2 | libublk-rs (null) | [13.89](/evidence/2026-10-10/ladder-report.md) | [751,428.28](/evidence/2026-10-10/ladder-report.md) | [4,006.86](/evidence/2026-10-10/ladder-report.md) |
| L2 | go-ublk (null, inline) | [13.89](/evidence/2026-10-10/ladder-report.md) | [750,904.23](/evidence/2026-10-10/ladder-report.md) | [4,027.94](/evidence/2026-10-10/ladder-report.md) |
| L2 | go-ublk (null, default goroutine) | [35.41](/evidence/2026-10-10/ladder-report.md) | [627,270.72](/evidence/2026-10-10/ladder-report.md) | [5,701.59](/evidence/2026-10-10/ladder-report.md) |
| L2 | lib-ublk (null, zero-filled reads) | [19.75](/evidence/2026-10-10/ladder-report.md) | [301,149.68](/evidence/2026-10-10/ladder-report.md) | [7,045.35](/evidence/2026-10-10/ladder-report.md) |
| L3 | ublksrv (loop over tmpfs) | [20.44](/evidence/2026-10-10/ladder-report.md) | [367,384.13](/evidence/2026-10-10/ladder-report.md) | [5,680.39](/evidence/2026-10-10/ladder-report.md) |
| L3 | libublk-rs (RAM, unmatched) | [14.61](/evidence/2026-10-10/ladder-report.md) | [656,377.06](/evidence/2026-10-10/ladder-report.md) | [4,309.44](/evidence/2026-10-10/ladder-report.md) |
| L3 | go-ublk (RAM, inline) | [14.53](/evidence/2026-10-10/ladder-report.md) | [601,819.01](/evidence/2026-10-10/ladder-report.md) | [4,532.49](/evidence/2026-10-10/ladder-report.md) |
| L3 | go-ublk (RAM, default goroutine) | [36.78](/evidence/2026-10-10/ladder-report.md) | [568,548.05](/evidence/2026-10-10/ladder-report.md) | [6,920.21](/evidence/2026-10-10/ladder-report.md) |
| L3 | lib-ublk (RAM) | [20.35](/evidence/2026-10-10/ladder-report.md) | [283,428.33](/evidence/2026-10-10/ladder-report.md) | [7,200.78](/evidence/2026-10-10/ladder-report.md) |

## 4 KiB random writes

| Rung | Target | qd1 p50 µs | 2×qd32 IOPS | CPU-ns/IO |
|---|---|---:|---:|---:|
| L0 | [null_blk (`memory_backed=0`)](/evidence/2026-10-10/ladder-report.md) | inline | [665,755.53](/evidence/2026-10-10/ladder-report.md) | [3,008.38](/evidence/2026-10-10/ladder-report.md) |
| L1 | brd | inline | [689,646.76](/evidence/2026-10-10/ladder-report.md) | [2,903.98](/evidence/2026-10-10/ladder-report.md) |
| L1 | null_blk (memory-backed) | inline | [506,170.70](/evidence/2026-10-10/ladder-report.md) | [3,960.83](/evidence/2026-10-10/ladder-report.md) |
| L2 | ublksrv (null) | [13.16](/evidence/2026-10-10/ladder-report.md) | [712,006.11](/evidence/2026-10-10/ladder-report.md) | [4,223.56](/evidence/2026-10-10/ladder-report.md) |
| L2 | libublk-rs (null) | [13.89](/evidence/2026-10-10/ladder-report.md) | [706,629.18](/evidence/2026-10-10/ladder-report.md) | [4,257.47](/evidence/2026-10-10/ladder-report.md) |
| L2 | go-ublk (null, inline) | [13.89](/evidence/2026-10-10/ladder-report.md) | [706,909.94](/evidence/2026-10-10/ladder-report.md) | [4,279.23](/evidence/2026-10-10/ladder-report.md) |
| L2 | go-ublk (null, default goroutine) | [35.41](/evidence/2026-10-10/ladder-report.md) | [606,231.64](/evidence/2026-10-10/ladder-report.md) | [5,959.48](/evidence/2026-10-10/ladder-report.md) |
| L2 | lib-ublk (null, zero-filled reads) | [19.75](/evidence/2026-10-10/ladder-report.md) | [275,901.24](/evidence/2026-10-10/ladder-report.md) | [7,828.04](/evidence/2026-10-10/ladder-report.md) |
| L3 | ublksrv (loop over tmpfs) | [20.44](/evidence/2026-10-10/ladder-report.md) | [367,190.94](/evidence/2026-10-10/ladder-report.md) | [5,792.74](/evidence/2026-10-10/ladder-report.md) |
| L3 | libublk-rs (RAM, unmatched) | [14.61](/evidence/2026-10-10/ladder-report.md) | [670,473.17](/evidence/2026-10-10/ladder-report.md) | [4,362.64](/evidence/2026-10-10/ladder-report.md) |
| L3 | go-ublk (RAM, inline) | [14.53](/evidence/2026-10-10/ladder-report.md) | [591,022.87](/evidence/2026-10-10/ladder-report.md) | [4,640.91](/evidence/2026-10-10/ladder-report.md) |
| L3 | go-ublk (RAM, default goroutine) | [36.78](/evidence/2026-10-10/ladder-report.md) | [558,978.76](/evidence/2026-10-10/ladder-report.md) | [7,142.78](/evidence/2026-10-10/ladder-report.md) |
| L3 | lib-ublk (RAM) | [20.35](/evidence/2026-10-10/ladder-report.md) | [251,613.09](/evidence/2026-10-10/ladder-report.md) | [8,092.99](/evidence/2026-10-10/ladder-report.md) |

## 4 KiB mixed random I/O

[70% reads and 30% writes](/evidence/2026-10-10/ladder-manifest.json).

| Rung | Target | qd1 p50 µs | 2×qd32 IOPS | CPU-ns/IO |
|---|---|---:|---:|---:|
| L0 | [null_blk (`memory_backed=0`)](/evidence/2026-10-10/ladder-report.md) | inline | [673,483.59](/evidence/2026-10-10/ladder-report.md) | [2,976.25](/evidence/2026-10-10/ladder-report.md) |
| L1 | brd | inline | [722,591.23](/evidence/2026-10-10/ladder-report.md) | [2,770.78](/evidence/2026-10-10/ladder-report.md) |
| L1 | null_blk (memory-backed) | inline | [514,477.61](/evidence/2026-10-10/ladder-report.md) | [3,895.04](/evidence/2026-10-10/ladder-report.md) |
| L2 | ublksrv (null) | [13.16](/evidence/2026-10-10/ladder-report.md) | [731,771.42](/evidence/2026-10-10/ladder-report.md) | [4,111.79](/evidence/2026-10-10/ladder-report.md) |
| L2 | libublk-rs (null) | [13.89](/evidence/2026-10-10/ladder-report.md) | [725,685.49](/evidence/2026-10-10/ladder-report.md) | [4,146.05](/evidence/2026-10-10/ladder-report.md) |
| L2 | go-ublk (null, inline) | [13.89](/evidence/2026-10-10/ladder-report.md) | [730,038.46](/evidence/2026-10-10/ladder-report.md) | [4,145.17](/evidence/2026-10-10/ladder-report.md) |
| L2 | go-ublk (null, default goroutine) | [35.41](/evidence/2026-10-10/ladder-report.md) | [605,853.07](/evidence/2026-10-10/ladder-report.md) | [5,864.79](/evidence/2026-10-10/ladder-report.md) |
| L2 | lib-ublk (null, zero-filled reads) | [19.75](/evidence/2026-10-10/ladder-report.md) | [278,881.15](/evidence/2026-10-10/ladder-report.md) | [7,531.09](/evidence/2026-10-10/ladder-report.md) |
| L3 | ublksrv (loop over tmpfs) | [20.44](/evidence/2026-10-10/ladder-report.md) | [362,288.20](/evidence/2026-10-10/ladder-report.md) | [5,790.70](/evidence/2026-10-10/ladder-report.md) |
| L3 | libublk-rs (RAM, unmatched) | [14.61](/evidence/2026-10-10/ladder-report.md) | [629,258.92](/evidence/2026-10-10/ladder-report.md) | [4,489.11](/evidence/2026-10-10/ladder-report.md) |
| L3 | go-ublk (RAM, inline) | [14.53](/evidence/2026-10-10/ladder-report.md) | [598,136.86](/evidence/2026-10-10/ladder-report.md) | [4,609.34](/evidence/2026-10-10/ladder-report.md) |
| L3 | go-ublk (RAM, default goroutine) | [36.78](/evidence/2026-10-10/ladder-report.md) | [560,703.84](/evidence/2026-10-10/ladder-report.md) | [7,041.87](/evidence/2026-10-10/ladder-report.md) |
| L3 | lib-ublk (RAM) | [20.35](/evidence/2026-10-10/ladder-report.md) | [262,404.24](/evidence/2026-10-10/ladder-report.md) | [7,728.19](/evidence/2026-10-10/ladder-report.md) |

## Earlier shared-CPU baseline

The earlier [retaining-RAM baseline](/evidence/2026-10-10/bench-baseline-report.md) used the same VM with fio and the server sharing a [4-CPU budget](/evidence/2026-10-10/bench-baseline-manifest.json). Its random and sequential workload tables remain available in the report. That CPU placement differs from the disjoint ladder run, so the two sets of means aren't interchangeable.

## Go dispatch experiment

A separate [interleaved A/B run](/go-ublk/performance/#dispatch-measurements) on the same VM compares go-ublk's default goroutine-per-request dispatch with `DeviceParams.Inline`. At queue depth one, RAM throughput changed from [20.3k to 43.5k IOPS](/evidence/2026-10-10/go-dispatch-ab-results.tsv). Those are medians from a separate experiment; the ladder tables report their own measured means.

## Evidence files

- [Overhead ladder report](/evidence/2026-10-10/ladder-report.md): all workload means, dispersion, CPU accounting, rung deltas, qualification gates, source revisions, and binary hashes.
- [Overhead ladder manifest](/evidence/2026-10-10/ladder-manifest.json): disjoint CPU placement, workload geometry, seed, runtime, warmup, and interleaved schedule.
- [Shared-CPU baseline report](/evidence/2026-10-10/bench-baseline-report.md) and [manifest](/evidence/2026-10-10/bench-baseline-manifest.json): the earlier retaining-RAM experiment.
- [Go dispatch results](/evidence/2026-10-10/go-dispatch-ab-results.tsv) and [metadata](/evidence/2026-10-10/go-dispatch-ab-metadata.txt): per-round latency, syscall, and context-switch measurements.

The hosted reports and metadata are sanitized extracts. Raw fio JSON and guest lifecycle logs aren't included in these downloads.
