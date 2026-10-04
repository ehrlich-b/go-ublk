---
title: "Performance"
linkTitle: "Performance"
description: "Measured throughput, what bounds it, and the knobs that matter."
weight: 60
---

The short version: with a RAM backend, go-ublk's I/O path sustained around a million 4 KiB IOPS on a four-vCPU VM, so for any backend that touches a disk or a network, the backend is the bottleneck, not the library. The rest of this page is the evidence and the caveats.

> [!NOTE]
> The figures below were measured with the engine before v0.2.0, which called the backend inline on each queue's thread — what `DeviceParams.Inline` does today. The v0.2.0 default runs each request on its own goroutine, which adds a hand-off per request but lets one queue keep many backend calls in flight. Re-measuring both modes, and zero copy and batch I/O, on real hardware is pending; the test rig of this release only had an emulated CPU, where timings mean nothing.

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

**Backend latency, per in-flight request.** By default each request runs on its own goroutine, so a queue keeps up to `QueueDepth` backend calls in flight, and a backend that takes 2 ms per call is not limited to 500 requests per second per queue. With `Inline`, each queue's thread calls the backend one request at a time, so its rate is at most 1 / (backend latency + overhead): use it only for backends that complete in microseconds.

**Copies.** In the default copy mode the kernel copies write data into the request buffer before delivery and read data out at commit — one `memcpy` of the payload per request, cheap at 4 KiB and noticeable in large sequential transfers. A file-backed device can avoid it entirely with [zero copy](/go-ublk/backends/#zero-copy), and applications that share memory with the server with shared-memory zero copy.

**System calls.** Each I/O thread reaps all available completions and submits all commits with one `io_uring_enter`, so under load the syscall cost per request is a fraction of a call. `BatchIO` (7.0+) goes further: one command fetches up to 128 requests and one commits a whole round.

**Bookkeeping.** The default metrics observer reads the clock twice per request. It is small next to any real backend.

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
