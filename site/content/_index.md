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
<p>Implement a small <code>Backend</code> interface shaped like <code>io.ReaderAt</code> and <code>io.WriterAt</code>, and get a <code>/dev/ublkbN</code>. The library handles io_uring, the kernel protocol and the device lifecycle.</p>
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
	"os"
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
	dev, err := ublk.CreateAndServe(context.Background(), ublk.DefaultParams(null{1 << 30}), nil)
	if err != nil {
		panic(err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	dev.Close() // STOP_DEV, drain in-flight I/O, DEL_DEV
}
```

```sh
sudo modprobe ublk_drv
go build -o nulldisk . && sudo ./nulldisk &
sudo mkfs.ext4 /dev/ublkb0 && sudo mount /dev/ublkb0 /mnt
```

[Getting started](/go-ublk/getting-started/) covers requirements, the two example servers, and how to clean up a device left behind by a killed process.

## Status

**go-ublk is a prototype that is approaching usable, not a production-hardened library.** What is true today:

- Single- and multi-queue I/O is verified byte-exact on arm64 and x86_64 on Ubuntu kernels 6.17 and 7.0, including O_DIRECT and concurrent read-after-write sweeps, crash consistency under SIGKILL, and power-fail consistency under a guest hard reset.
- Read, write, flush, discard and write-zeroes work; logical block sizes from 512 bytes to the page size work.
- Teardown of a busy device is clean, and a device leaked by a killed process can be reaped.
- Several lifecycle defects found by a 2026-10-03 code audit are open, user recovery is not implemented, and the daemon must run as a correctly ordered systemd unit to survive host reboots. See [Testing and compatibility](/go-ublk/testing/), [Deployment](/go-ublk/deployment/) and the [roadmap](/go-ublk/roadmap/).

The guide covers the entire kernel interface as of Linux 7.3-rc5, whether or not go-ublk uses a given feature yet. The [UAPI reference](/reference/uapi/) marks go-ublk's status item by item.

</div>
