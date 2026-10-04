# go-ublk

A Go library for building Linux block devices in userspace with
[ublk](https://docs.kernel.org/block/ublk.html). Pure Go: its io_uring and ublk
bindings use `golang.org/x/sys` only — no cgo, no liburing — so it builds with
`CGO_ENABLED=0`.

ublk is like FUSE, but for block devices: the kernel forwards block I/O to your
process over io_uring, you serve it, and `/dev/ublkbN` behaves like any disk.

**Documentation: [ublk.ehrlich.dev](https://ublk.ehrlich.dev)** — a guide to ublk
itself and the go-ublk reference.

## Features

- **Backends** as `ReadAt`/`WriteAt`/`Flush` (plus optional discard, write-zeroes,
  FUA and integrity interfaces), or a raw `Handler` that sees every operation and
  flag and can complete requests from any goroutine.
- **Concurrent by default**: each request runs on its own goroutine, so a
  latency-bound backend (network, object storage) keeps every queue full.
- **User recovery**: devices survive their server. A crashed or upgraded server
  hands the device to a new process (`Detach`, `Recover`); with `RecoveryReissue`
  no error reaches the application. Tested under systemd with a mounted
  filesystem.
- **Zero copy** for file-backed devices: data moves between the request and the
  file with io_uring fixed buffers, never through Go memory. Also shared-memory
  zero copy (`SHMEM_ZC`).
- **The whole kernel interface** as of Linux 7.3-rc5: every control command,
  feature and parameter block — batch I/O, zoned devices, integrity metadata
  (T10-DIF), user copy, unprivileged devices, online resize, safe stop, and the
  rest. Features are negotiated with the kernel; a missing one is an error, never
  a silently weaker device.
- **Production lifecycle**: graceful stop that drains in-flight I/O, `Done`/`Err`
  supervision, context cancellation as a clean stop, systemd units with correct
  mount ordering.

## Quick start

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
func (r *ramDisk) Flush() error { return nil }
func (r *ramDisk) Close() error { return nil }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	params := ublk.DefaultParams(&ramDisk{data: make([]byte, 1<<30)})
	params.VolatileCache = false // RAM: a completed write is as durable as it gets

	// Serves until ctx is cancelled, which stops the device gracefully.
	dev, err := ublk.CreateAndServe(ctx, params, nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s", dev.Path)
	<-dev.Done()
	if err := dev.Close(); err != nil {
		log.Print(err)
	}
}
```

```sh
sudo modprobe ublk_drv
go build -o ramdisk . && sudo ./ramdisk
# elsewhere: sudo mkfs.ext4 /dev/ublkb0 && sudo mount /dev/ublkb0 /mnt
```

Survive restarts with recovery:

```go
params.Recovery = ublk.RecoveryReissue // in-flight I/O is reissued to the next server
params.Tag = myTag                     // find the device again after a restart
// ... later, in the new process:
ids, _ := ublk.FindDevices(myTag)
dev, err := ublk.Recover(ctx, ids[0], ublk.DefaultParams(backend), nil)
```

The [examples](examples/) are complete servers on the public API: `ublk-mem` (RAM,
optionally compressed) and `ublk-loop` (a file, with `-zero-copy` and
`-recovery`), plus [systemd units](examples/systemd/) that keep the device and its
mount across crashes and upgrades.

## Requirements

- Linux 6.4 or newer with `ublk_drv` (`modprobe ublk_drv`); newer features need
  newer kernels and are reported by `ublk.Probe()`. Some distributions disable
  io_uring by default (`kernel.io_uring_disabled` on RHEL 10).
- Root or `CAP_SYS_ADMIN`, or unprivileged devices with the udev rule in
  [`examples/ublk-chown`](examples/ublk-chown/).
- Go 1.25.

## Testing

Unit tests run anywhere Linux runs (`make test-unit`, `make test-race`), including
a fake-kernel model of the ublk driver that the queue engine is tested and fuzzed
against (`make test-uapi-fuzz`). `ublk-suite` (`make suite`) is a real-kernel
conformance suite — data integrity, every feature, crash recovery, teardown under
load, chaos — and the [kernel matrix](test/matrix/) boots it under dozens of
mainline and distribution kernels. Results are on the
[compatibility page](https://ublk.ehrlich.dev/reference/matrix/).

## License

MIT
