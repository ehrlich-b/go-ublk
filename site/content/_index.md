---
title: "ublk"
description: "How Linux ublk userspace block devices work, from the control plane to zero copy, and the documentation for go-ublk, a pure-Go ublk library."
heroTitle: "Linux block devices in pure Go"
heroLede: "The pre-v0.2.0 inline engine reached **1.37M 4 KiB random-read IOPS** on a four-vCPU Apple M4 VM with a RAM backend. No cgo or liburing. [Measurements and conditions](/go-ublk/performance/); benchmarks of the current default engine are pending."
heroLinks:
  - label: "Learn how ublk works"
    url: "/guide/"
    primary: true
  - label: "Get started with go-ublk"
    url: "/go-ublk/getting-started/"
  - label: "UAPI reference"
    url: "/reference/uapi/"
---

<div class="cards">
<a class="card" href="/guide/">
<p class="card-title">The ublk guide</p>
<p>Language-agnostic: architecture, control commands, FETCH/COMMIT, copy modes, batch I/O, recovery, and kernel versions.</p>
</a>
<a class="card" href="/go-ublk/">
<p class="card-title">go-ublk</p>
<p>Implement a <code>Backend</code> with <code>io.ReaderAt</code>-style methods or a raw request <code>Handler</code>. The library handles io_uring, the kernel protocol, recovery, and device lifecycle.</p>
</a>
<a class="card" href="/reference/">
<p class="card-title">Reference</p>
<p>Kernel UAPI, introducing releases, go-ublk support, and test results per kernel.</p>
</a>
</div>

<div class="prose-narrow">

## How it fits together

ublk has served userspace block devices through io_uring since Linux 6.0. Applications use `/dev/ublkbN` like any disk: `ublk_drv` delivers request descriptors to the server, which handles storage in files, network services, compressed RAM, or object stores. Committing a result re-arms the slot. No socket protocol or SCSI emulation is required. Follow a request through the [architecture](/guide/architecture/).

## Quick start with go-ublk

This null device discards writes and reads zeros. Pass its five-method backend to `CreateAndServe`; use the RAM or file example below for a filesystem.

```go
package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/ehrlich-b/go-ublk"
)

type null struct{ size int64 }

func (n null) ReadAt(p []byte, off int64) (int, error)  { clear(p); return len(p), nil }
func (n null) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }
func (n null) Size() int64                              { return n.size }
func (n null) Flush() error                             { return nil }
func (n null) Close() error                             { return nil }

func main() {
	// Serve until a signal; cancelling the context stops the device gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	dev, err := ublk.CreateAndServe(ctx, ublk.DefaultParams(null{1 << 30}), nil)
	if err != nil {
		panic(err)
	}
	<-dev.Done()
	dev.Close() // delete the device
}
```

```sh
sudo modprobe ublk_drv
go build -o nulldisk . && sudo ./nulldisk &
sudo dd if=/dev/ublkb0 of=/dev/null bs=4K count=1 iflag=direct
```

[Getting started](/go-ublk/getting-started/): requirements, RAM and file servers, and cleanup after a killed process.

## Status

go-ublk v0.2.0 implements the Linux 7.3-rc5 control commands, features, and parameters, including recovery, zero copy, batch I/O, zoned devices, and integrity metadata.

- The [compatibility matrix](/reference/matrix/) records integrity, queue/block-size combinations, filesystems, supported features, crash recovery, upgrade handoffs, teardown, and chaos tests across dozens of kernels.
- The queue engine is unit-tested and fuzzed against a driver model.
- Recovery preserves devices and mounted filesystems across crashes and upgrades, tested under systemd with verified writes and zero I/O errors.
- [Known kernel bugs](/guide/kernel-bugs/) records failures affecting ublk servers.

The pre-1.0 API may change between minor releases. See [releases](/go-ublk/releases/) and [roadmap](/go-ublk/roadmap/).

</div>
