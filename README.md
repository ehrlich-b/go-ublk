# go-ublk

A Go library for building Linux block devices in userspace. Pure Go, dependency-free, no cgo.

ublk is like FUSE, but for block devices instead of filesystems. The kernel forwards block I/O to your userspace program via io_uring - you just implement read/write handlers. go-ublk handles the io_uring setup, kernel communication, and device lifecycle.

As far as I can tell, this is the only pure-Go ublk implementation available.

## Usage

Implement the `Backend` interface (it matches `io.ReaderAt`/`io.WriterAt`) and call `CreateAndServe`:

```go
package main

import (
    "context"
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
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT)
    defer stop()

    backend := &NullBackend{size: 1 << 30} // 1GB
    params := ublk.DefaultParams(backend)

    device, _ := ublk.CreateAndServe(ctx, params, nil)
    defer device.Close()

    <-ctx.Done()
}
```

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

Local benchmarks on Ubuntu 24.04 VM (2 vCPUs, 8GB RAM, i7-8700K host, 4 queues, depth=64):

| Workload | go-ublk | Loop (RAM) | % of Loop |
|----------|---------|------------|-----------|
| 4K Read (1 job) | 85k IOPS | 220k IOPS | 39% |
| 4K Read (4 jobs) | 99k IOPS | 116k IOPS | 85% |
| 4K Write (4 jobs) | 90k IOPS | 99k IOPS | 91% |

Multi-queue workloads reach 85-91% of kernel loop device throughput.

## Requirements

- Linux kernel >= 6.8
- `ublk_drv` module loaded
- Root or CAP_SYS_ADMIN

## References

- [Linux kernel ublk docs](https://docs.kernel.org/block/ublk.html)
- [ublksrv (C reference)](https://github.com/ublk-org/ublksrv)

## License

MIT
