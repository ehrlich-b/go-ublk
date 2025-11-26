# go-ublk

Pure Go implementation of Linux ublk (userspace block device).

Think FUSE, but for block devices instead of filesystems. ublk lets you implement block devices entirely in userspace using io_uring for communication with the kernel.

## Requirements

- Linux kernel >= 6.1
- `ublk_drv` kernel module loaded
- Root or CAP_SYS_ADMIN capability

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
