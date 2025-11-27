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

## Library Usage

```go
package main

import (
    "context"
    "log"

    "github.com/ehrlich-b/go-ublk"
    "github.com/ehrlich-b/go-ublk/backend"
)

func main() {
    ctx := context.Background()

    memBackend := backend.NewMemory(512 << 20) // 512MB
    defer memBackend.Close()

    params := ublk.DefaultParams(memBackend)
    device, err := ublk.CreateAndServe(ctx, params, &ublk.Options{})
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Device: %s", device.Info().BlockPath)
    <-ctx.Done()
}
```

## Implementing a Backend

Only the `Backend` interface is required:

```go
type Backend interface {
    ReadAt(p []byte, off int64) (n int, err error)
    WriteAt(p []byte, off int64) (n int, err error)
    Size() int64
    Close() error
    Flush() error
}
```

See [examples/README.md](examples/README.md) for a complete walkthrough with a `/dev/null` implementation and optional interfaces.

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
