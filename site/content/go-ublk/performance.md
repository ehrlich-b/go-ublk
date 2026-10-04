---
title: "Performance"
linkTitle: "Performance"
description: "Measured throughput, what bounds it, and the knobs that matter."
weight: 60
---

The short version: with a RAM backend, go-ublk's I/O path sustains around a million 4 KiB IOPS on a four-vCPU VM, so for any backend that touches a disk or a network, the backend is the bottleneck, not the library. The rest of this page is the evidence and the caveats.

## Measurements

### The ceiling of the ublk path

`fio`, 4 KiB random I/O, `--direct=1`, four jobs at queue depth 32 each, against `ublk-mem` (RAM, so no backend cost) on an arm64 Apple M4 VM with 4 vCPUs:

| Workload | 1 queue | 4 queues |
|---|---|---|
| randread | 801k IOPS | **1.37M IOPS** (93 µs average latency) |
| randwrite | 680k IOPS | **816k IOPS** (155 µs average latency) |

Four queues scale reads about 1.7x and writes about 1.2x over one. This is an upper bound for the library's own overhead on that machine: request delivery, descriptor reads, the data copy, and completion. It is not an end-to-end number for a real backend.

### Against the kernel loop driver

One run on Ubuntu 24.04.5 with kernel 7.0.0-34 (4 vCPUs, 4 GiB RAM), go-ublk with 4 queues of depth 64. `fio` with 4 KiB direct I/O, libaio, queue depth 64 per job, 10 seconds per workload. Both devices RAM-backed, 256 MiB (the loop device over a file in tmpfs):

| Workload | go-ublk | loop (RAM) | go-ublk / loop |
|---|---|---|---|
| 4K random read, 1 job | 321k IOPS | 299k IOPS | 108% |
| 4K random read, 4 jobs | 658k IOPS | 827k IOPS | 80% |
| 4K random write, 4 jobs | 647k IOPS | 799k IOPS | 81% |

A single sequential run on a shared host, taken before the large-I/O buffer change, so read the percentages as rough. The loop driver is entirely in-kernel; reaching 80% of it with a userspace server is the point.

> [!NOTE]
> Older figures of "~100k IOPS" that circulated for this project were buffered I/O on different hardware and are superseded. Benchmark block devices with `--direct=1`; buffered numbers mostly measure the page cache.

## What bounds throughput

**Backend latency, per queue.** Each queue goroutine calls the backend synchronously, one request at a time (see [concurrency](/go-ublk/backends/#concurrency)). A queue's request rate is therefore at most 1 / (backend latency + per-request overhead). With RAM that is microseconds and the numbers above apply. With a backend that takes 200 µs per call, one queue tops out near 5,000 IOPS; with 2 ms, near 500. The only lever today is more queues, and the kernel allows at most one per CPU. An asynchronous backend interface is on the [roadmap](/go-ublk/roadmap/).

**One copy per request.** go-ublk uses ublk's default copy mode: the kernel copies write data into the request buffer before delivery and copies read data out at commit. That is one `memcpy` of the payload per request, cheap at 4 KiB and noticeable in large sequential transfers. Zero copy is not implemented.

**System calls.** Each queue reaps all available completions and submits all commits with one `io_uring_enter`, so under load the syscall cost per request is a fraction of a call. At low queue depth it approaches one syscall per request in each direction.

**Bookkeeping.** The default metrics observer reads the clock twice per request. It is small next to any real backend.

## Knobs

| Knob | Default | When to change it |
|---|---|---|
| `NumQueues` | one per CPU | Lower it to cap the CPU the server uses. Raising it past the CPU count does nothing; the kernel clamps |
| `QueueDepth` | 128 | Deeper queues let the kernel park more requests per queue but do not add backend concurrency. Lower it to save buffer memory |
| `MaxIOSize` | 1 MiB | Larger means fewer, bigger requests for sequential workloads, at `QueueDepth × MaxIOSize` of buffer address space per queue. Smaller saves memory; the kernel splits requests to fit |
| `LogicalBlockSize` | 512 | 4096 for a backend with 4 KiB native blocks, so the kernel never sends sub-4K I/O |
| `VolatileCache` | true | False only if every completed write is already durable; saves flush round trips. Never set it false to go faster |
| `CPUAffinity` | none | Pins queue threads. Useful on large machines to keep queues off CPUs reserved for other work |
| `Options.Debug` | false | Keep it off when measuring; debug logging serializes on one lock |

## Measuring your own backend

Use the block device the way your workload will, with direct I/O, and compare against a baseline you trust. A starting point:

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

`--rw=randwrite` with `--verify=crc32c` adds a correctness check for free. The repository's `benchmarks/` directory has fio job files for 4K random read and write, 128K sequential, and latency percentiles, and `make vm-benchmark` runs the loop-device comparison above on a test VM.

To separate library overhead from backend cost, run the same job against `ublk-mem` and against your backend; the difference is your backend.
