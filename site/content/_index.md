---
title: "ublk"
description: "How Linux ublk userspace block devices work, from the control plane to zero copy, and the documentation for go-ublk, a pure-Go ublk library."
heroTitle: "Userspace block devices, explained and implemented in Go"
heroLede: "**ublk** is the Linux framework for serving a block device from a userspace process: think *FUSE for block devices*, built on io_uring and in mainline since Linux 6.0. This site is a guide to how it works, for anyone writing a ublk server in any language, and the home of **go-ublk**, a ublk library in pure Go with no cgo and no liburing."
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
<p>Architecture, every control command, the FETCH and COMMIT data plane, copy modes and zero copy, batch I/O, user recovery, and which kernel added what. Language-agnostic.</p>
</a>
<a class="card" href="/go-ublk/">
<p class="card-title">go-ublk</p>
<p>Implement a small <code>Backend</code> interface shaped like <code>io.ReaderAt</code> and <code>io.WriterAt</code> — or a raw request <code>Handler</code> — and get a <code>/dev/ublkbN</code>. The library handles io_uring, the kernel protocol, recovery and the device lifecycle.</p>
</a>
<a class="card" href="/reference/">
<p class="card-title">Reference</p>
<p>The full kernel UAPI surface with the release that introduced each item and its go-ublk status, plus a compatibility matrix of test runs per kernel.</p>
</a>
</div>

<div class="prose-narrow">

## How it fits together

An application reads and writes `/dev/ublkbN` like any disk. The kernel's `ublk_drv` turns each block request into a small descriptor and completes an io_uring command that your server left waiting. Your server does the I/O however it likes (a file, a network protocol, compressed RAM, an object store), then commits the result with the next io_uring command, which also re-arms the slot. There is no socket protocol, no SCSI emulation, and no context switch per request beyond io_uring's own batching. The [architecture chapter](/guide/architecture/) walks one request through the whole path.

## Quick start with go-ublk

A complete block device that discards writes and reads zeros. Implement five methods and hand the backend to `CreateAndServe`:

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
sudo mkfs.ext4 /dev/ublkb0 && sudo mount /dev/ublkb0 /mnt
```

[Getting started](/go-ublk/getting-started/) covers requirements, the two example servers, and how to clean up a device left behind by a killed process.

## Status

**go-ublk v0.2.0 implements the whole kernel interface as of Linux 7.3-rc5** — every control command, feature and parameter block, from user recovery and zero copy to batch I/O, zoned devices and integrity metadata — and is tested on real kernels:

- A real-kernel conformance suite (data integrity across queue and block-size combinations, filesystems, every feature, crash recovery, live upgrade handoffs, teardown under load, chaos) runs under dozens of mainline and distribution kernels; see the [compatibility matrix](/reference/matrix/).
- The queue engine is unit-tested and fuzzed against a model of the kernel driver.
- User recovery keeps a device — and the filesystem mounted on it — across a server crash or upgrade, verified under systemd with a verifying writer and zero I/O errors.
- Kernel bugs found along the way, and the ones that bite ublk servers in general, are tracked in [known kernel bugs](/guide/kernel-bugs/).

It is pre-1.0: the API can still change between minor releases. See [Releases](/go-ublk/releases/) and the [roadmap](/go-ublk/roadmap/).

</div>
