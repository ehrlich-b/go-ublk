# go-ublk

A Go library for building Linux block devices in userspace. Its ublk and
io_uring bindings are written in Go, with no C, liburing, or cgo dependency.
The module uses `golang.org/x/sys` for Linux system calls.

ublk is like FUSE, but for block devices instead of filesystems. The kernel
forwards block I/O to your userspace program via io_uring; you implement the
read/write handlers, and go-ublk handles io_uring, kernel communication, and the
device lifecycle.

## Usage

Implement the `Backend` interface (it matches `io.ReaderAt`/`io.WriterAt`) and call `CreateAndServe`:

```go
package main

import (
    "context"
    "log"
    "os/signal"
    "syscall"

    "github.com/ehrlich-b/go-ublk"
)

// NullBackend discards writes and returns zeros on read
type NullBackend struct{ size int64 }

func (b *NullBackend) ReadAt(p []byte, off int64) (int, error) {
    clear(p)
    return len(p), nil
}
func (b *NullBackend) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }
func (b *NullBackend) Size() int64                              { return b.size }
func (b *NullBackend) Flush() error                             { return nil }
func (b *NullBackend) Close() error                             { return nil }

func main() {
    signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT)
    defer stop()

    backend := &NullBackend{size: 1 << 30} // 1GB
    params := ublk.DefaultParams(backend)

    device, err := ublk.CreateAndServe(context.Background(), params, nil)
    if err != nil {
        log.Fatal(err)
    }

    <-signalCtx.Done()
    if err := device.Close(); err != nil {
        log.Printf("close device: %v", err)
    }
}
```

`ReadAt` and `WriteAt` receive a borrowed per-request buffer. A backend may use
it only until the method returns and must not retain it. The return values follow
`io.ReaderAt` and `io.WriterAt`: short transfers are failures, while a full read
may return `io.EOF`.

`DeviceParams.MaxIOSize` defaults to 1 MiB. It must be at least one OS page,
page-aligned, aligned to the logical block size, and no larger than
`math.MaxInt32`. The runner uses the capacity returned by `ADD_DEV` and maps
`QueueDepth * MaxIOSize` bytes for each queue.

Measured on WSL kernel `6.6.87.2-microsoft-standard-WSL2` at queue depth 128, a
64 KiB maximum maps 8 MiB per queue and the 1 MiB default maps 128 MiB per queue.
The anonymous mappings added no resident memory before first touch; touching every page made the full 8 MiB or 128 MiB resident.
Those figures exclude backend storage, the Go heap, descriptors, and io_uring.

## Try It

The repo includes a RAM-backed block device example:

```bash
# Load the kernel module
sudo modprobe ublk_drv

# Build and run
make build
sudo ./bin/ublk-mem --size=1G

# In another terminal: use it like any block device
sudo mkfs.ext4 /dev/ublkb0
sudo mount /dev/ublkb0 /mnt
# ...
sudo umount /mnt
```

## Performance

These measurements predate the large-I/O buffer fix.

One run on an Ubuntu 24.04.5 VM with kernel 7.0.0-34-generic (4 vCPUs, 4 GiB RAM, 4 ublk queues, depth 64).
fio used 4 KiB direct I/O, libaio, queue depth 64 per job, and 10 seconds per workload. Both devices were
RAM-backed with 256 MiB capacity.

| Workload | go-ublk | Loop (RAM) | % of Loop |
|----------|---------|------------|-----------|
| 4K Read (1 job) | 321k IOPS | 299k IOPS | 108% |
| 4K Read (4 jobs) | 658k IOPS | 827k IOPS | 80% |
| 4K Write (4 jobs) | 647k IOPS | 799k IOPS | 81% |

This was a single sequential run on a shared host, so the percentages are rough comparisons, not capacity
estimates. The four-job workloads reached about 80% of the loop baseline.

## Requirements

- Linux kernel >= 6.8
- `ublk_drv` module loaded
- Root or CAP_SYS_ADMIN

The userspace validation and kernel/device coverage differ. See
[the compatibility matrix](docs/compatibility.md) for tested headers, execution
environments, historical device results, and the remaining acceptance gates.
The 2026-10-02 WSL run tested userspace code without an available ublk driver;
it does not establish device support for WSL or Linux 6.6.

## References

- [Linux kernel ublk docs](https://docs.kernel.org/block/ublk.html)
- [ublksrv (C reference)](https://github.com/ublk-org/ublksrv)

## License

MIT
