---
title: "Getting started"
linkTitle: "Getting started"
description: "Requirements, installing go-ublk, your first device, the example servers, and cleaning up after a crash."
weight: 10
---

## Requirements

| Requirement | Detail |
|---|---|
| Linux kernel | 6.8 or newer is the documented minimum. go-ublk always sends ioctl-encoded commands, which kernels before 6.4 do not understand; 6.4 through 6.16 are expected to work but are not verified. Verified kernels are Ubuntu 6.17 and 7.0 builds on arm64 and x86_64; see [Testing and compatibility](/go-ublk/testing/) |
| Kernel module | `ublk_drv` loaded, so that `/dev/ublk-control` exists |
| Privileges | root, or `CAP_SYS_ADMIN`, to create devices |
| Go | 1.25 or newer (the module's `go` directive) |
| Architectures | amd64 and arm64 are tested. The code has no architecture-specific assembly |

> [!WARNING]
> Some Ubuntu 6.17 kernels (generic builds from about -24 through -40, and `6.17.0-1019-aws`) oops the host the first time any ublk server adds a device. This is a kernel packaging bug, not something a server can work around. See [Known kernel bugs](/guide/kernel-bugs/) before choosing a kernel.

Load the module and make it load at boot:

```sh
sudo modprobe ublk_drv
echo ublk_drv | sudo tee /etc/modules-load.d/ublk.conf
ls -l /dev/ublk-control
```

If `modprobe` cannot find the module, the kernel may ship it in a separate package (Ubuntu's AWS kernels put it in `linux-modules-extra-$(uname -r)`), or not build it at all (WSL2 kernels do not).

## Install

```sh
go get github.com/ehrlich-b/go-ublk
```

The package is Linux-only: it talks to io_uring and the ublk driver directly. It has no cgo, so `CGO_ENABLED=0` builds and cross-compilation from another OS work as for any Go program (`GOOS=linux GOARCH=arm64 go build`). It does not compile for other operating systems.

## Your first device

A RAM disk in about forty lines. The backend is a byte slice behind a lock; go-ublk calls it from one goroutine per hardware queue, so it must be safe for concurrent use.

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ehrlich-b/go-ublk"
)

type ramDisk struct {
	mu   sync.RWMutex
	data []byte
}

func (r *ramDisk) ReadAt(p []byte, off int64) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return copy(p, r.data[off:]), nil
}

func (r *ramDisk) WriteAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return copy(r.data[off:], p), nil
}

func (r *ramDisk) Size() int64  { return int64(len(r.data)) }
func (r *ramDisk) Flush() error { return nil } // nothing below RAM to flush to
func (r *ramDisk) Close() error { return nil }

func main() {
	backend := &ramDisk{data: make([]byte, 256<<20)}
	params := ublk.DefaultParams(backend)
	params.VolatileCache = false // a completed write to RAM is as durable as it gets

	device, err := ublk.CreateAndServe(context.Background(), params, nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s", device.Path)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	<-sig

	// Close stops the device while the queues are still serving, drains
	// in-flight I/O, then deletes it. Do not cancel the context first.
	if err := device.Close(); err != nil {
		log.Printf("close: %v", err)
	}
}
```

Build and run it as root, then use the device from another terminal:

```sh
go build -o ramdisk . && sudo ./ramdisk
```

```sh
sudo mkfs.ext4 /dev/ublkb0
sudo mount /dev/ublkb0 /mnt
echo hello | sudo tee /mnt/hello.txt
sudo umount /mnt
```

Press Ctrl-C in the first terminal to tear the device down. Two things in that program are load-bearing:

- **`device.Close()` before exit.** A process that exits without it leaves the device registered in the kernel with nothing serving it. It cannot do I/O, it pins the module, and it has to be deleted by hand (below).
- **Not cancelling the context before `Close`.** `Close` sends `STOP_DEV` while the queue goroutines are still running, because the kernel drains in-flight I/O through them before the stop completes. Cancelling the context first stops those goroutines and strands the drain. See [Device lifecycle](/go-ublk/lifecycle/).

`DefaultParams` gives 128-deep queues, one queue per CPU (the kernel caps it there anyway), 512-byte blocks, 1 MiB maximum requests and a volatile write cache. [Configuration](/go-ublk/configuration/) lists every field.

## The example servers

The repository has two complete servers built only on the public API. They are the best reference for a real backend.

```sh
git clone https://github.com/ehrlich-b/go-ublk && cd go-ublk
make build
```

**`ublk-mem`** is a RAM disk with sharded locks, and with `--zip` a compressed one (64 KiB chunks, flate):

```sh
sudo ./bin/ublk-mem --size=1G
sudo ./bin/ublk-mem --size=1G --zip --queues=4 --depth=64
```

**`ublk-loop`** exports a file the way `losetup` does: sparse allocation, discard that punches holes, write-zeroes, and both durability modes.

```sh
sudo ./bin/ublk-loop --file=/var/tmp/disk.img --size=10G          # buffered, flush = fsync
sudo ./bin/ublk-loop --file=/var/tmp/disk.img --sync              # O_DSYNC, write-through
sudo ./bin/ublk-loop --file=/var/tmp/disk.img --read-only
```

Both accept `-v` for debug logging and stop cleanly on SIGINT or SIGTERM.

## Cleaning up a leaked device

If a server is killed with SIGKILL, crashes, or exits without `Close`, its device stays registered: the kernel removes `/dev/ublkbN` and fails the I/O that was in flight, but `/dev/ublkcN` and the device ID remain until someone deletes them. Both examples can reap such devices:

```sh
sudo ./bin/ublk-mem --del=all     # every registered ublk device
sudo ./bin/ublk-mem --del=3       # just device 3
```

In your own program, the same takes two calls:

```go
ids, err := ublk.ListDevices()
if err != nil {
	return err
}
for _, id := range ids {
	if err := ublk.DeleteDevice(id); err != nil {
		log.Printf("device %d: %v", id, err)
	}
}
```

`DeleteDevice` is for devices nobody is serving. A device your process still owns should be closed with `Device.Close`, which drains I/O first. `ListDevices` probes IDs 0 through 63 only; see [Troubleshooting](/go-ublk/troubleshooting/).

## Next steps

- [Writing a backend](/go-ublk/backends/): the interface contract, concurrency, durability and errors.
- [Deployment](/go-ublk/deployment/): the systemd unit a production server needs, and why.
- [The ublk guide](/guide/): how the kernel side works, if you want to know what go-ublk is doing for you.
