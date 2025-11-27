# go-ublk

Pure Go implementation of Linux ublk (userspace block device).

Think FUSE, but for block devices instead of filesystems. ublk lets you implement block devices entirely in userspace using io_uring for communication with the kernel.

## Requirements

- Linux kernel >= 6.8 (tested on Ubuntu 24.04 LTS)
- `ublk_drv` kernel module loaded
- Root or CAP_SYS_ADMIN capability

### Tested Kernels (provisional, VM-tested)

| Kernel | Distro | Status |
|--------|--------|--------|
| 6.8.0-31 | Ubuntu 24.04 LTS | ✅ Works |
| 6.11.0-24 | Ubuntu 24.04 HWE | ✅ Works |

## Performance

Benchmarks on Ubuntu 24.04 VM (4 queues, depth=64, batched io_uring submissions):

| Workload | go-ublk | Loop (RAM) | % of Loop |
|----------|---------|------------|-----------|
| 4K Read (1 job, QD=64) | 85k IOPS | 220k IOPS | 39% |
| 4K Read (4 jobs, QD=64) | 99k IOPS | 116k IOPS | 85% |
| 4K Write (4 jobs, QD=64) | 90k IOPS | 99k IOPS | 91% |

Multi-queue workloads achieve 85-91% of kernel loop device performance.

## Quick Start

```bash
# Load the kernel module
sudo modprobe ublk_drv

# Build and run a RAM disk
make build
sudo ./bin/ublk-mem --size=1G

# In another terminal
sudo mkfs.ext4 /dev/ublkb0
sudo mount /dev/ublkb0 /mnt
# ... use the filesystem ...
sudo umount /mnt

# Stop with Ctrl+C
```

## API

Implement the `Backend` interface and call `CreateAndServe`:

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

The `Backend` interface matches Go's `io.ReaderAt`/`io.WriterAt` plus `Size`, `Flush`, and `Close`.

## Testing

```bash
make build       # Build binaries
make test-unit   # Run unit tests
make vm-e2e      # Full integration test (requires VM setup)
```

## References

- [Linux kernel ublk docs](https://docs.kernel.org/block/ublk.html)
- [ublksrv (C reference implementation)](https://github.com/ublk-org/ublksrv)

## License

MIT
