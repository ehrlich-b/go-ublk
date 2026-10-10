---
title: "Performance"
linkTitle: "Performance"
description: "Measured throughput, what bounds it, and the knobs that matter."
weight: 60
---

The pre-v0.2.0 inline engine reached 1.37M 4 KiB random-read IOPS with a RAM backend on a four-vCPU VM. The current default engine has not been remeasured.

> [!NOTE]
> These measurements used inline backend calls, equivalent to today's `DeviceParams.Inline`. v0.2.0 defaults to a goroutine per request, adding handoff but permitting concurrent calls within a queue. Native-hardware comparisons of both modes, zero copy, and batch I/O remain pending; this release's emulated-CPU rig cannot supply useful performance measurements.

## Measurements

### The ceiling of the ublk path

fio: 4 KiB random direct I/O (`--direct=1`), four jobs at QD32 each, ublk-mem RAM backend, arm64 Apple M4 VM with four vCPUs:

| Workload | 1 queue | 4 queues |
|---|---|---|
| randread | 801k IOPS | **1.37M IOPS** (93 µs average latency) |
| randwrite | 680k IOPS | **816k IOPS** (155 µs average latency) |

Four queues improved reads about 1.7x and writes 1.2x. These results include request delivery, descriptor access, copying, completion, and RAM-backend work; they do not predict disk/network backend performance.

### Against the kernel loop driver

Reported historical run: Ubuntu 24.04.5, kernel 7.0.0-34, four vCPUs, 4 GiB RAM, four go-ublk queues of depth 64. fio used 4 KiB direct I/O, libaio, queue depth 64 per job, 10 s per workload. Both devices held 256 MiB in RAM; loop used a tmpfs file.

| Workload | go-ublk | loop (RAM) | go-ublk / loop |
|---|---|---|---|
| 4K random read, 1 job | 321k IOPS | 299k IOPS | 108% |
| 4K random read, 4 jobs | 658k IOPS | 827k IOPS | 80% |
| 4K random write, 4 jobs | 647k IOPS | 799k IOPS | 81% |

Workloads ran sequentially on a shared host before the large-I/O buffer change. Treat ratios to the in-kernel loop driver as approximate; raw results are not retained in this checkout.

> [!NOTE]
> Older ~100k IOPS figures used buffered I/O/different hardware and are superseded. Use --direct=1; buffered tests mostly measure page cache.

## What bounds throughput

By default, each queue permits up to `QueueDepth` concurrent backend calls; 2 ms calls need not cap it at 500 IOPS. `Inline` serializes calls at at most `1 / (backend latency + overhead)` per queue; reserve it for microsecond backends.

Default copy mode performs one kernel memcpy per payload: before WRITE delivery or at READ commit. Large sequential I/O can become copy-bound. File backends can use [zero copy](/go-ublk/backends/#zero-copy); cooperating applications can use shared-memory zero copy.

Queue threads reap available completions and submit commits together through `io_uring_enter`, amortizing syscall cost. `BatchIO` (7.0+) fetches up to 128 requests per completion and commits a round together.

The default metrics observer reads the clock twice per request; include that cost when profiling.

## Knobs

| Knob | Default | When to change it |
|---|---|---|
| `NumQueues` | one per CPU | Lower it to cap the CPU the server uses. Raising it past the CPU count does nothing; the kernel clamps |
| `QueueDepth` | 128 | Requests in flight per queue, and so backend calls in flight per queue. Lower it to save buffer memory |
| `Inline` | false | True for RAM-speed backends that never block: no goroutine hand-off, one request at a time per queue |
| `EnableZeroCopy` | false | For file-backed devices: no copy, fully asynchronous file I/O in the kernel |
| `BatchIO` | false | Fewer commands per request on 7.0+ |
| `ThreadsPerQueue` | 1 | More I/O threads per queue (6.16+), if one thread per queue is the bottleneck |
| `MaxIOSize` | 1 MiB | Larger means fewer, bigger requests for sequential workloads, at `QueueDepth × MaxIOSize` of buffer address space per queue. Smaller saves memory; the kernel splits requests to fit |
| `LogicalBlockSize` | 512 | 4096 for a backend with 4 KiB native blocks, so the kernel never sends sub-4K I/O |
| `VolatileCache` | true | False only if every completed write is already durable; saves flush round trips. Never set it false to go faster |
| `CPUAffinity` | the CPUs the kernel routes to each queue | Pins queue threads explicitly instead, e.g. to keep them off CPUs reserved for other work |
| `Options.Debug` | false | Keep it off when measuring |

## Measuring your own backend

Measure representative direct I/O against a trusted baseline:

```sh
# 4 KiB random read, 4 jobs x QD32, direct I/O
sudo fio --name=randread --filename=/dev/ublkb0 --rw=randread --bs=4k \
    --direct=1 --ioengine=io_uring --iodepth=32 --numjobs=4 \
    --time_based --runtime=30 --group_reporting

# 1 MiB sequential write
sudo fio --name=seqwrite --filename=/dev/ublkb0 --rw=write --bs=1m \
    --direct=1 --ioengine=io_uring --iodepth=8 --numjobs=1 \
    --time_based --runtime=30 --group_reporting
```

`--rw=randwrite --verify=crc32c` adds correctness checks. `benchmarks/` contains 4K random read/write, 128K sequential, and latency-percentile jobs; `make vm-benchmark` runs the loop comparison in a VM.

Compare the same workload against ublk-mem and your backend to assess backend cost under those conditions.