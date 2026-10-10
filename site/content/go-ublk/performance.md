---
title: "Performance"
linkTitle: "Performance"
description: "Measured dispatch costs, RAM throughput, and the settings that affect backend concurrency."
weight: 60
---

`DeviceParams.Inline` runs the backend on the queue's I/O thread. On the measured RAM workload, it reduced syscall and context-switch counts at queue depth one and changed throughput from [20.3k to 43.5k IOPS](/evidence/2026-10-10/go-dispatch-ab-results.tsv). The default dispatch runs a goroutine per request, keeping concurrent backend calls in flight when those calls block.

## Dispatch measurements

This A/B experiment used the same [Linux 6.12 KVM guest](/evidence/2026-10-10/go-dispatch-ab-metadata.txt) as the [comparative benchmarks](/reference/benchmarks/). The figures below are rounded medians from [3 interleaved rounds](/evidence/2026-10-10/go-dispatch-ab-metadata.txt), with a RAM backend. Device geometry was [2 queues at depth 64](/evidence/2026-10-10/go-dispatch-ab-metadata.txt); the fio workloads used queue depth one or [2 jobs at queue depth 32 each](/evidence/2026-10-10/go-dispatch-ab-results.tsv).

| Workload | Metric | Default goroutine per request | Inline |
|---|---|---:|---:|
| Queue depth one | IOPS | [20.3k](/evidence/2026-10-10/go-dispatch-ab-results.tsv) | [43.5k](/evidence/2026-10-10/go-dispatch-ab-results.tsv) |
| Queue depth one | p50 completion latency, µs | [42](/evidence/2026-10-10/go-dispatch-ab-results.tsv) | [16.5](/evidence/2026-10-10/go-dispatch-ab-results.tsv) |
| Queue depth one | Syscalls per I/O | [7.1](/evidence/2026-10-10/go-dispatch-ab-results.tsv) | [1.1](/evidence/2026-10-10/go-dispatch-ab-results.tsv) |
| Queue depth one | Context switches per I/O | [4.1](/evidence/2026-10-10/go-dispatch-ab-results.tsv) | [1.1](/evidence/2026-10-10/go-dispatch-ab-results.tsv) |
| Two jobs, depth thirty-two each | IOPS | [553k](/evidence/2026-10-10/go-dispatch-ab-results.tsv) | [689k](/evidence/2026-10-10/go-dispatch-ab-results.tsv) |
| Two jobs, depth thirty-two each | p99 completion latency, µs | [185](/evidence/2026-10-10/go-dispatch-ab-results.tsv) | [148](/evidence/2026-10-10/go-dispatch-ab-results.tsv) |

The [per-round TSV](/evidence/2026-10-10/go-dispatch-ab-results.tsv) also includes a pooled-goroutine dispatch experiment. Reusing goroutines didn't improve these workloads. Together, the pooled and inline results point to the cross-thread handoff as the dispatch cost that matters here. These are VM measurements; cross-CPU wakeups cost more here than on bare metal.

The [comparative baseline](/reference/benchmarks/) is a separate experiment and reports means. Its go-ublk rows use default dispatch; the inline medians above don't replace those means.

## Choosing a dispatch mode

Use inline dispatch for backends that never block, such as RAM. Each queue's thread executes a backend call before it can serve the next request. A blocking call holds up that queue.

Keep the default goroutine-per-request mode for backends that can wait on disk, network I/O, or another service. It preserves concurrency up to the queue's in-flight request limit. Backend latency and the application's required concurrency determine the useful dispatch mode.

`DeviceParams.Inline` is [already in the public API](https://github.com/ehrlich-b/go-ublk/blob/e4e39b07522919fbe2ba11bb594c9c05e998ae24/backend.go#L95-L100):

```go
params := ublk.DefaultParams(ramBackend)
params.Inline = true
```

> [!NOTE]
> The `-inline` flag measured in the `ublk-mem` example is on an unmerged branch. The public API setting is available now; the example flag isn't part of the published release. The pooled-dispatch flag in the evidence is experimental too.

## What bounds throughput

**Backend latency.** Default dispatch keeps backend calls concurrent. Inline dispatch serializes calls within each queue. Increasing queue depth only helps if the selected dispatch mode and backend can use the extra in-flight work.

**Copies.** In copy mode, the kernel copies write data into the request buffer before delivery and read data out at commit. File-backed devices can use [zero copy](/go-ublk/backends/#zero-copy), subject to kernel support and the [known UBSAN finding](/guide/kernel-bugs/#found-by-go-ublks-kernel-matrix).

**System calls and scheduling.** Queue threads batch available completions and commits in `io_uring_enter`. At queue depth one, the measured dispatch handoff adds scheduling work before the backend can return. `BatchIO` can reduce command counts on kernels that support it; this A/B run doesn't measure batch I/O.

## Settings

The defaults and feature requirements below come from the [published DeviceParams implementation](https://github.com/ehrlich-b/go-ublk/blob/e4e39b07522919fbe2ba11bb594c9c05e998ae24/backend.go).

| Setting | Default | When to change it |
|---|---|---|
| `NumQueues` | One per CPU | Match the server's CPU budget and backend concurrency. Use the queue count accepted by the kernel. |
| `QueueDepth` | [128](https://github.com/ehrlich-b/go-ublk/blob/e4e39b07522919fbe2ba11bb594c9c05e998ae24/backend.go#L85) | Lower it to reduce buffer memory, or tune it to the backend's useful in-flight limit. |
| `Inline` | false | Enable it for backends that never block. |
| `EnableZeroCopy` | false | Use it for asynchronous file-backed I/O when the kernel and backend support the path. |
| `BatchIO` | false | Measure it when reducing commands per request matters. |
| `ThreadsPerQueue` | [1](https://github.com/ehrlich-b/go-ublk/blob/e4e39b07522919fbe2ba11bb594c9c05e998ae24/backend.go#L154-L157) | Add I/O threads only when a queue's thread limits useful work. |
| `MaxIOSize` | [1 MiB](https://github.com/ehrlich-b/go-ublk/blob/e4e39b07522919fbe2ba11bb594c9c05e998ae24/backend.go#L89-L93) | Match sequential request sizes, allowing for `QueueDepth × MaxIOSize` buffer address space per queue. |
| `LogicalBlockSize` | [512 bytes](https://github.com/ehrlich-b/go-ublk/blob/e4e39b07522919fbe2ba11bb594c9c05e998ae24/backend.go#L87) | Match the backend's native block size. |
| `VolatileCache` | true | Disable it only when every completed write is already durable. |
| `CPUAffinity` | Kernel queue routing | Pin explicitly when the application reserves CPUs for other work. |
| `Options.Debug` | false | Keep debug logging off during measurements. |

## Measuring your backend

Use direct I/O and the queue depth your application needs. Record throughput, latency, and the CPU budget for both the client and server. Compare the RAM target with your backend on the same kernel and machine.

The [benchmark manifest](/evidence/2026-10-10/bench-baseline-manifest.json) records the comparative workloads and schedule. The [dispatch metadata](/evidence/2026-10-10/go-dispatch-ab-metadata.txt) records the separate experiment's runtime, server flags, and executable hash. Raw fio JSON and profiling logs aren't part of those hosted extracts.
