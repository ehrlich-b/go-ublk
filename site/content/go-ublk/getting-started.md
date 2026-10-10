---
title: "Getting started"
linkTitle: "Getting started"
description: "Requirements, installing go-ublk, your first device, the example servers, and cleaning up after a crash."
weight: 10
---

## Requirements

| Requirement | Detail |
|---|---|
| Linux kernel | 6.4 or newer: go-ublk always sends ioctl-encoded commands, which older kernels do not understand. Newer features need newer kernels and are reported by `ublk.Probe()`. The [compatibility matrix](/reference/matrix/) lists every kernel the conformance suite has run on |
| Kernel module | `ublk_drv` loaded, so that `/dev/ublk-control` exists |
| Privileges | root, or `CAP_SYS_ADMIN`, to create devices; or an unprivileged user with the udev rule in `examples/ublk-chown` |
| Go | 1.25 or newer (the module's `go` directive) |
| Architectures | amd64 and arm64 are tested. The code has no architecture-specific assembly |

> [!WARNING]
> Ubuntu generic 6.17 builds around -24 through -40 and 6.17.0-1019-aws can oops during ADD_DEV. Check [kernel bugs](/guide/kernel-bugs/) before choosing a build; userspace cannot fix the packaging error.

Load ublk now and at boot:

```sh
sudo modprobe ublk_drv
echo ublk_drv | sudo tee /etc/modules-load.d/ublk.conf
ls -l /dev/ublk-control
```

Missing module: check separate packages, e.g. Ubuntu AWS `linux-modules-extra-$(uname -r)`. Checked WSL2 kernels omit it.

## Install

```sh
go get github.com/ehrlich-b/go-ublk
```

Linux-only, without cgo. Use CGO_ENABLED=0 and cross-compile from other systems, e.g. `GOOS=linux GOARCH=arm64 go build`.

## Your first device

A roughly forty-line RAM disk uses a locked byte slice: requests run concurrently, one goroutine each.

```go
package main

import (
	"context"
	"log"
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

	// The device serves until this context is cancelled; cancelling it
	// stops the device gracefully, draining in-flight I/O first.
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	device, err := ublk.CreateAndServe(ctx, params, nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s", device.Path)

	<-device.Done() // a signal arrived, or a queue failed
	if err := device.Err(); err != nil {
		log.Printf("device failed: %v", err)
	}
	if err := device.Close(); err != nil { // delete it
		log.Printf("close: %v", err)
	}
}
```

Run as root; use the device from another terminal:

```sh
go build -o ramdisk . && sudo ./ramdisk
```

```sh
sudo mkfs.ext4 /dev/ublkb0
sudo mount /dev/ublkb0 /mnt
echo hello | sudo tee /mnt/hello.txt
sudo umount /mnt
```

After unmounting, Ctrl-C stops the server. Two requirements:

- Close before exit, or registration pins the module with no serving process. Delete the orphan, or [recover it](/go-ublk/lifecycle/#detach-and-recover) if enabled.
- Handle SIGHUP alongside SIGINT/SIGTERM: logind sends SIGTERM then SIGHUP on session exit, potentially killing a draining server. Use a [systemd service](/go-ublk/deployment/) for long-lived devices.

Defaults: one queue per CPU (kernel-capped), depth 128, 512-byte blocks, 1 MiB requests, volatile cache. See [configuration](/go-ublk/configuration/).

## The example servers

Two reference servers use only the public API:

```sh
git clone https://github.com/ehrlich-b/go-ublk && cd go-ublk
make build
```

ublk-mem uses sharded locks; --zip adds flate compression in 64 KiB chunks:

```sh
sudo ./bin/ublk-mem --size=1G
sudo ./bin/ublk-mem --size=1G --zip --queues=4 --depth=64
```

ublk-loop exports files like losetup: sparse allocation, hole-punch discard, zeroes, and both durability modes.

```sh
sudo ./bin/ublk-loop --file=/var/tmp/disk.img --size=10G          # buffered, flush = fsync
sudo ./bin/ublk-loop --file=/var/tmp/disk.img --sync              # O_DSYNC, write-through
sudo ./bin/ublk-loop --file=/var/tmp/disk.img --read-only
```

Both accept -v for debug and stop on SIGINT/SIGTERM.

## Cleaning up a leaked device

Without recovery, SIGKILL, crash, or exit without Close removes the block node and fails I/O, retaining the char node and ID. Both examples can delete orphans:

```sh
sudo ./bin/ublk-mem --del=all     # every registered ublk device
sudo ./bin/ublk-mem --del=3       # just device 3
```

Programmatic cleanup:

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

DeleteDevice cleans unserved devices; use Device.Close for local devices. ListDevices uses sysfs, falling back to IDs 0-63 when unavailable. See [troubleshooting](/go-ublk/troubleshooting/).

## Next steps

- [Backend contracts](/go-ublk/backends/): concurrency, durability, errors.
- [Deployment](/go-ublk/deployment/): systemd units and ordering.
- [ublk guide](/guide/): kernel protocol.