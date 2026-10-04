---
title: "Writing a backend"
linkTitle: "Writing a backend"
description: "The Backend interface and its optional extensions: buffer ownership, concurrency, durability, and how errors reach the block layer."
weight: 20
---

A backend is where the device's bytes live. go-ublk asks it to read, write, flush and, optionally, discard or zero ranges; everything else (the io_uring protocol, descriptors, queue threads, teardown) is the library's job. This page is the contract.

## The interface

```go
type Backend interface {
	ReadAt(p []byte, off int64) (n int, err error)
	WriteAt(p []byte, off int64) (n int, err error)
	Size() int64
	Close() error
	Flush() error
}

// Optional. Implementing either one is what makes the device advertise the operation.
type DiscardBackend interface {
	Backend
	Discard(offset, length int64) error
}

type WriteZeroesBackend interface {
	Backend
	WriteZeroes(offset, length int64) error
}
```

| Method | Called for | When |
|---|---|---|
| `ReadAt` | `UBLK_IO_OP_READ` | every read |
| `WriteAt` | `UBLK_IO_OP_WRITE` | every write; never on a `ReadOnly` device |
| `Flush` | `UBLK_IO_OP_FLUSH` | only if `DeviceParams.VolatileCache` is true (the default) |
| `Discard` | `UBLK_IO_OP_DISCARD` | only if the backend implements `DiscardBackend` |
| `WriteZeroes` | `UBLK_IO_OP_WRITE_ZEROES` | only if the backend implements `WriteZeroesBackend` |
| `Size` | device creation; `Device.Size` | the capacity is read once, at creation |
| `Close` | never by go-ublk | the program calls it after `Device.Close` |

All offsets and lengths are in bytes.

## ReadAt and WriteAt

The shapes match `io.ReaderAt` and `io.WriterAt`, and so do the rules, with one strengthening: **a short transfer is a failure**.

- `off` and `len(p)` are multiples of `LogicalBlockSize`, `len(p)` is at most `MaxIOSize` (1 MiB by default), and `[off, off+len(p))` lies within `[0, Size())`. The kernel guarantees this; a backend may still check it.
- Success is `n == len(p)` with a nil error. For reads, `n == len(p)` with `io.EOF` also counts as success, as `io.ReaderAt` allows.
- Anything else fails the request: a non-nil error, or `n < len(p)` even with a nil error. There are no partial completions.
- A read must fill all of `p`. Space that was never written reads as zeros on a block device; if your storage is shorter than the device (a sparse file, an object that does not exist yet), zero the rest of `p` and return `len(p)`.

**The buffer is borrowed.** `p` is a slice of a per-request buffer that go-ublk maps once per queue and the kernel copies request data into and out of. It is valid only until the method returns: the next request on the same slot reuses it. Copy what you need to keep. Do not retain `p`, hand it to a goroutine that outlives the call, or write to it after returning.

`*os.File` almost satisfies the interface already, with two catches: its `ReadAt` returns `io.EOF` with a short count when the file is shorter than the device, which must become zeros, and `Size`, `Flush` and `Close` need wrapping. The file backend below handles both.

## Size

`Size` is read when the device is created, to validate the parameters and to set the capacity the kernel exports. It must be positive and a multiple of `LogicalBlockSize`. The device does not track later changes: go-ublk does not implement the kernel's resize command yet.

## Flush and durability

`Flush` must make every write that has already returned durable: on disk, replicated, acknowledged by the remote store, whatever durable means for your storage. The kernel sends it when something above needs that guarantee: a filesystem journal commit, an `fsync`, a barrier.

Whether `Flush` is ever called depends on `DeviceParams.VolatileCache`, which tells the kernel whether a completed write can still be lost:

| Backend | `VolatileCache` | `Flush` |
|---|---|---|
| File, buffered writes | `true` (default) | `f.Sync()` |
| File opened `O_DSYNC` | `false` | never called; return nil |
| RAM | `false` | never called; return nil |
| Network store with write-back buffering | `true` | push buffered writes and wait for acknowledgment |
| Anything you are unsure about | `true` | make it correct |

The default is `true` because the two mistakes are not symmetric. Claiming a cache that does not exist costs an occasional no-op call. Hiding one that does exist means the kernel never asks you to flush, filesystems believe their journal commits are durable, and a power failure loses acknowledged data with no error anywhere.

Per-write FUA is not implemented: go-ublk never advertises `UBLK_ATTR_FUA`, and `EnableFUA` only logs a warning. The block layer then emulates FUA writes as a write followed by a flush, so a correct `Flush` is all that durability needs.

## Close

go-ublk does not call `Close`. `Device.Close` tears down the kernel device and leaves the backend alone, because the program that created the backend owns it. Close the backend yourself after `Device.Close` returns, when no more calls can arrive:

```go
defer backend.Close()          // runs second
device, err := ublk.CreateAndServe(ctx, ublk.DefaultParams(backend), nil)
// ...
device.Close()                 // runs first: no backend calls after this returns
```

For a buffered file backend, `Close` is the last chance to `Sync`.

## Discard and write-zeroes

The optional interfaces are detected with a type assertion on the value in `DeviceParams.Backend` when the device is created. If a wrapper type hides the methods, the device does not advertise the operation, and `blkdiscard` reports "operation not supported".

**`Discard`** is advisory. The range's contents are no longer needed; deallocate them (punch a hole, drop chunks) or do nothing. Reads of the range afterwards may return anything. Returning nil without doing anything is correct.

**`WriteZeroes`** is not advisory: afterwards the range must read back as zeros. Punching a hole in a sparse file satisfies it; so does writing zeros.

Both receive **ranges, not buffers**, and the ranges can be enormous: by default the device advertises the largest discard the block layer allows, so `mkfs` and `blkdiscard` on a large device send multi-gigabyte requests. Never allocate a buffer the size of `length`.

The kernel's `UBLK_IO_F_NOUNMAP` flag ("zero the range but keep it allocated") is not passed to `WriteZeroes`. A backend that punches holes for write-zeroes will deallocate even when the caller asked it not to. That matters only for callers that preallocate on purpose; `fallocate(FALLOC_FL_ZERO_RANGE)` on the device is the usual source.

`MaxDiscardSectors` caps both operations, and setting it to 0 turns both off. `DiscardGranularity` and `DiscardAlignment` describe your allocation unit; the defaults are 4096.

## Concurrency

go-ublk runs one goroutine per hardware queue, each locked to an OS thread, and each calls the backend directly. With `NumQueues` queues, up to `NumQueues` backend calls can be in progress at once, in any mix of methods. **A backend must be safe for concurrent use.** A single mutex is correct; sharded locks, as in the `ublk-mem` example, or lock-free positional I/O, as in `ublk-loop`, scale better.

Within one queue, requests are handled **one at a time**: the queue goroutine takes a batch of completed fetches from io_uring, calls the backend for each in turn, and then submits all the commits in one system call. `QueueDepth` controls how many requests the kernel can have outstanding on a queue, not how many backend calls run concurrently. The consequences:

- Per-queue throughput is bounded by backend latency. A backend that takes 1 ms per call serves at most about 1,000 requests per second per queue, whatever the depth.
- A backend call that blocks stalls its whole queue, and every request the kernel has routed to it.
- Throughput scales with queues, which the kernel caps at the number of CPUs.

An asynchronous backend interface, where one queue can have many backend operations in flight, is on the [roadmap](/go-ublk/roadmap/). Until then, latency-bound backends (network, object storage) want as many queues as the machine has CPUs, and fast backends want the default.

The block layer does not serialize requests to overlapping ranges, so two queues can write the same block at the same time. The result may be either write, as on any disk, but the backend's own data structures must survive it. A compressed or copy-on-write backend that rewrites a chunk needs a lock covering the chunk.

Keep calls bounded. A call that never returns cannot be cancelled, and teardown waits for the queue goroutine only for a limited time before releasing the memory it uses (see [Roadmap](/go-ublk/roadmap/), open defect #19). Network backends should use timeouts and return errors rather than hang. Never call `Device.Close` from inside a backend method: `Close` waits for the queue goroutine that is running your method.

## Errors

Any failure, an error or a short count, completes the request with `-EIO`. go-ublk does not pass errnos through, so a thin-provisioned backend that runs out of space produces an I/O error, not `ENOSPC`, in the application. There are no retries in the library.

A panic in a backend method is not recovered. It crashes the server, which fails the in-flight I/O and leaves the device registered until it is [reaped](/go-ublk/getting-started/#cleaning-up-a-leaked-device). Recover inside your methods if a panic is survivable.

Failed operations are counted in [metrics](/go-ublk/lifecycle/#metrics) and reported to an `Observer` with `success == false`.

## Memory

Each queue maps `QueueDepth × MaxIOSize` bytes of anonymous memory for request buffers: 128 MiB per queue with the defaults. The mapping costs no resident memory until pages are touched, and only buffers the kernel actually fills become resident. A backend that never sees large requests never touches most of it. If memory is tight, lower `MaxIOSize` (the kernel splits larger requests) or `QueueDepth`.

## Example: a file backend

A condensed version of `examples/ublk-loop`, which exports a regular file:

```go
type fileBackend struct {
	f    *os.File
	size int64
}

func (b *fileBackend) ReadAt(p []byte, off int64) (int, error) {
	n, err := b.f.ReadAt(p, off)
	if errors.Is(err, io.EOF) { // file shorter than the device: unwritten space is zeros
		clear(p[n:])
		return len(p), nil
	}
	return n, err
}

func (b *fileBackend) WriteAt(p []byte, off int64) (int, error) { return b.f.WriteAt(p, off) }
func (b *fileBackend) Size() int64                              { return b.size }
func (b *fileBackend) Flush() error                             { return b.f.Sync() }

func (b *fileBackend) Close() error {
	if err := b.f.Sync(); err != nil {
		b.f.Close()
		return err
	}
	return b.f.Close()
}

// Discard punches a hole, returning the space to the filesystem.
func (b *fileBackend) Discard(off, length int64) error {
	err := unix.Fallocate(int(b.f.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, off, length)
	if errors.Is(err, unix.EOPNOTSUPP) {
		return nil // this filesystem cannot deallocate; discard is advisory
	}
	return err
}
```

`os.File.ReadAt` and `WriteAt` are `pread` and `pwrite`, which carry their own offsets, so every queue can call them at once without locking. With buffered writes the device must keep `VolatileCache = true`, which is what makes the kernel send the `Flush` that becomes `fsync`. The full example also handles write-zeroes, read-only mode, `O_DSYNC` write-through, and filesystems that cannot punch holes.

## Testing a backend

Most of a backend can be tested without a kernel: call the methods directly, concurrently, with block-aligned offsets, and check the bytes. The package exports `ublk.NewMockBackend(size)`, an in-memory backend that implements the optional interfaces and counts calls, for testing code that is written against `ublk.Backend`.

End to end, run the server in a disposable VM and drive `/dev/ublkbN` with `fio --verify`, `mkfs` plus a filesystem workload, and a kill-and-check crash test. [Testing and compatibility](/go-ublk/testing/) describes the harnesses go-ublk uses on itself, which work the same way on any backend.
