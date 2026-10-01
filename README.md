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

Measured at queue depth 128, a 64 KiB maximum maps 8 MiB per queue and the 1 MiB
default maps 128 MiB per queue. The anonymous mappings added no resident memory
before first touch; touching every page made the full 8 MiB or 128 MiB resident.
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

## Requirements

- Linux kernel >= 6.8
- `ublk_drv` module loaded
- Root or CAP_SYS_ADMIN

## References

- [Linux kernel ublk docs](https://docs.kernel.org/block/ublk.html)
- [ublksrv (C reference)](https://github.com/ublk-org/ublksrv)

## License

MIT
