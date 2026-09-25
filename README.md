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

One run on an Ubuntu 24.04.5 VM with kernel 7.0.0-34-generic (4 vCPUs, 4 GiB RAM, 4 ublk queues, depth 64). fio used 4 KiB direct I/O, libaio, queue depth 64 per job, and 10 seconds per workload. Both devices were RAM-backed with 256 MiB capacity.

| Workload | go-ublk | Loop (RAM) | % of Loop |
|----------|---------|------------|-----------|
| 4K Read (1 job) | 321k IOPS | 299k IOPS | 108% |
| 4K Read (4 jobs) | 658k IOPS | 827k IOPS | 80% |
| 4K Write (4 jobs) | 647k IOPS | 799k IOPS | 81% |

This was a single sequential run on a shared host, so the percentages are rough comparisons, not capacity estimates. The four-job workloads reached about 80% of the loop baseline.

## Requirements

- Linux kernel >= 6.8
- `ublk_drv` module loaded
- Root or CAP_SYS_ADMIN

## References

- [Linux kernel ublk docs](https://docs.kernel.org/block/ublk.html)
- [ublksrv (C reference)](https://github.com/ublk-org/ublksrv)

## License

MIT
