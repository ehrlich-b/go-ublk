---
title: "Writing a backend"
linkTitle: "Writing a backend"
description: "The Backend interface and its optional extensions: buffer ownership, concurrency, durability, and how errors reach the block layer."
weight: 20
---

A backend reads, writes, flushes, and optionally discards or zeroes stored bytes. go-ublk handles io_uring, descriptors, queue threads, and device teardown.

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

These follow `io.ReaderAt`/`io.WriterAt`, except short transfers fail:

- The kernel supplies block-aligned `off`/`len(p)`, length at most `MaxIOSize` (default 1 MiB), and a range within `[0, Size())`. Backends may validate too.
- Success requires `n == len(p)` and nil error; a full READ with `io.EOF` also succeeds. Other errors or short counts fail, even with nil error.
- Fill every READ byte. Unwritten space, missing objects, or storage shorter than the device must return zeros for the remainder and count `len(p)`.

The per-request buffer `p` comes from a per-queue mapping used for kernel copies. It expires when the method returns, then the next request reuses the slot. Copy bytes you need to retain; never keep the slice, pass it to work outliving the call, or modify it after returning.

Wrap `*os.File` to zero-fill short EOF reads and provide Size, Flush, and Close, as below.

## Size

At creation, `Size` validates parameters and sets exported capacity; it must be positive and block-aligned. Later backend size changes require an explicit `Device.Resize` call.

## Flush and durability

`Flush` makes all completed writes durable, whether that means disk persistence, replication, or remote acknowledgment. Filesystem journal commits, `fsync`, and barriers request this guarantee.

`DeviceParams.VolatileCache` tells the kernel whether completed writes can be lost and therefore need flushing:

| Backend | `VolatileCache` | `Flush` |
|---|---|---|
| File, buffered writes | `true` (default) | `f.Sync()` |
| File opened `O_DSYNC` | `false` | never called; return nil |
| RAM | `false` | never called; return nil |
| Network store with write-back buffering | `true` | push buffered writes and wait for acknowledgment |
| Anything you are unsure about | `true` | make it correct |

The default `true` may cost a no-op flush. Incorrectly choosing `false` suppresses required flushes, letting filesystems acknowledge journal commits that a power failure can erase without errors.

FUA (Force Unit Access) defaults off; the block layer emulates it with write-then-flush. To make single writes durable more cheaply, implement `FUABackend.WriteAtFUA` and set `DeviceParams.EnableFUA`. O_DSYNC writes and journal commits then use WriteAtFUA without flushing the entire device.

## Close

The program owns the backend; go-ublk never calls its `Close`. Close it after `Device.Close` succeeds and callbacks have stopped. If device closure fails, keep the backend available for serving and retry teardown.

```go
defer backend.Close()          // runs second
device, err := ublk.CreateAndServe(ctx, ublk.DefaultParams(backend), nil)
// ...
device.Close()                 // runs first: no backend calls after this returns
```

For a buffered file backend, `Close` is the last chance to `Sync`.

## Discard and write-zeroes

Creation type-asserts `DeviceParams.Backend` for optional methods. Wrappers hiding them suppress advertisement; `blkdiscard` then reports "operation not supported".

`Discard` is advisory: punch holes, drop chunks, or return nil without action. Subsequent reads may return anything.

`WriteZeroes` must make subsequent reads return zeros, through hole punching or explicit writes.

Both take ranges with no data buffers. The default advertises the block layer's largest allowed discard; `mkfs` and `blkdiscard` can send multi-gigabyte ranges. Never allocate `length` bytes.

`WriteZeroes` does not receive `UBLK_IO_F_NOUNMAP` (keep allocation while zeroing). Hole-punching backends therefore deallocate even for intentional preallocation, such as `fallocate(FALLOC_FL_ZERO_RANGE)` on the device. A [Handler](#the-handler-interface) receives `FlagNoUnmap`.

`MaxDiscardSectors` caps both operations, and setting it to 0 turns both off. `DiscardGranularity` and `DiscardAlignment` describe your allocation unit; the defaults are 4096.

## Concurrency

Each hardware queue has an I/O thread and ring, or several with `ThreadsPerQueue`. Requests run on separate goroutines: up to `QueueDepth` calls per queue, `NumQueues × QueueDepth` total, mixing methods. Completion returns to the queue thread through an armed eventfd, then commits to the kernel. Backends must support concurrency: one mutex works; `ublk-mem` shards locks and `ublk-loop` uses lock-free positional I/O.

A 1 ms backend call therefore need not limit a queue to 1,000 IOPS or block later requests.

`DeviceParams.Inline` calls the backend on the queue thread, avoiding goroutine handoff. Use it for nonblocking RAM-speed operations completing well below a microsecond. It serves one request at a time; a blocked call stalls its queue.

Overlapping writes can run concurrently and leave either result. Protect backend structures; compressed/copy-on-write chunks need locks covering each rewritten chunk.

Bound calls with timeouts, especially over networks. A hung call cannot be cancelled: STOP_DEV waits until `Options.StopTimeout`, and Close fails while retaining memory the call could access. Never call `Device.Close` inside a backend method; it waits for that method too.

## Errors

Errors complete requests as follows:

- Wrapped or direct `syscall.Errno` passes through, e.g. `ENOSPC` for exhausted thin provisioning. Drivers using `errno_to_blk_status` preserve common ENOSPC/ETIMEDOUT/EOPNOTSUPP errors; older kernels return EIO for all failures.
- `context.DeadlineExceeded`/`os.ErrDeadlineExceeded` map to ETIMEDOUT; `errors.ErrUnsupported` to EOPNOTSUPP.
- Other errors and short counts map to EIO. Inspect with `ublk.Errno(err)`.

The library does not retry; an error fails that request while the queue keeps serving.

Recovered backend panics produce EIO and a log entry if a logger is configured. The server continues, but backend state may be damaged; fix the panic.

Failed operations are counted in [metrics](/go-ublk/lifecycle/#metrics) and reported to an `Observer` with `success == false`.

## Integrity metadata

For `DeviceParams.Integrity`, implement `IntegrityBackend` to store per-block metadata, e.g. an 8-byte T10-DIF tuple per 512 bytes:

```go
func (b *store) WriteIntegrity(meta []byte, off int64) error // metadata for the blocks at off
func (b *store) ReadIntegrity(meta []byte, off int64) error  // return what was stored
```

ReadAt/WriteAt move data; these methods move matching metadata. With a checksum type, the kernel generates protection information on writes and verifies reads, returning `EILSEQ` for wrong tuples. Unwritten blocks need all `0xff` metadata (T10 escape), or reads, including partition scans, fail verification. Handlers receive `Request.Integrity`.

## Zero copy

For linear file/block-device storage without copying, implement `ZeroCopyBackend`:

```go
func (b *fileBackend) ZeroCopyFile() (fd int, base int64) { return int(b.f.Fd()), 0 }
```

Set `DeviceParams.EnableZeroCopy` (6.15+). The kernel registers request pages in the queue's buffer table; go-ublk submits fixed-buffer I/O at `base + offset` on that ring. No ReadAt/WriteAt/Flush calls occur and bytes never enter Go memory. Flush uses `fdatasync`, discard `fallocate(PUNCH_HOLE)`, zeroes `fallocate(ZERO_RANGE)`, and FUA `RWF_DSYNC`, honoring EnableFUA. Asynchronous file operations can fill the queue depth.

Creation requires file length at least `base + Size()`. Zero copy excludes user copy, NeedGetData, and unprivileged devices. `ublk-loop -zero-copy` enables it.

## The Handler interface

For raw requests and asynchronous completion, set `DeviceParams.Handler` instead of Backend:

```go
h := ublk.HandlerFunc(func(r *ublk.Request) {
	switch r.Op {
	case ublk.OpRead:
		go func() { // complete later, from any goroutine
			n, err := store.ReadAt(r.Data, r.Offset)
			r.CompleteN(n, err)
		}()
	case ublk.OpWrite:
		durable := r.Flags&ublk.FlagFUA != 0
		r.Complete(store.Write(r.Data, r.Offset, durable))
	case ublk.OpFlush:
		r.Complete(store.Sync())
	default:
		r.Complete(syscall.EOPNOTSUPP)
	}
})
params := ublk.DeviceParams{Handler: h, Size: store.Size(), /* ... */}
```

`Request` carries OpRead/OpWrite/OpFlush/OpDiscard/OpWriteZeroes or zoned operations; FlagFUA/FlagNoUnmap, fail-fast, and swap hints; byte offset/length; and `Data` for data operations.

- Call Complete(err), CompleteN(n, err), or CompleteZoneAppend(sector, err) once. Double completion panics; omission leaves the request in flight until teardown.
- Request and Data expire at completion and are reused by the tag.
- Only copy-mode READs support partial completion and resubmission. Short WRITEs and user-copy/zero-copy READs must fail: the kernel treats non-negative results as full success. CompleteN enforces this.
- Set HandlerDiscard/HandlerWriteZeroes to receive those operations and EnableFUA for FUA.
- EnableZoned adds zone operations. Answer OpReportZones with `r.ReportZones(zones)` (a short list ends it), and OpZoneAppend with `r.CompleteZoneAppend(sector, err)`, identifying the written sector. `zonedMem` in `test/suite/tests_v1.go` is an approximately eighty-line host-managed example.

## Memory

Each queue maps `QueueDepth × MaxIOSize` anonymous bytes: 128 MiB by default. Only touched pages become resident; small requests leave much of the mapping untouched. Reduce QueueDepth or MaxIOSize to save memory; the kernel splits larger requests.

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

`os.File.ReadAt`/`WriteAt` use pread/pwrite offsets, permitting concurrent queues without locks. Buffered writes require `VolatileCache = true` so kernel Flush becomes fsync. The full example also handles write-zeroes, read-only mode, O_DSYNC write-through, and filesystems without hole punching.

## Testing a backend

Test methods directly with concurrent, block-aligned calls and byte comparisons. `ublk.NewMockBackend(size)` provides in-memory storage, optional interfaces, and call counts for code consuming `ublk.Backend`.

In a disposable VM, exercise `/dev/ublkbN` with `fio --verify`, `mkfs` and filesystem workloads, and kill-and-check crash tests. The [test harnesses](/go-ublk/testing/) work with custom backends.