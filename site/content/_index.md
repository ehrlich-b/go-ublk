---
title: "ublk"
description: "The Linux ublk guide, go-ublk's pure-Go engine, lib-ublk's Zig core and language bindings, and comparative benchmarks with evidence."
heroTitle: "Userspace block devices, with Go and Zig libraries"
heroLede: "**ublk** serves Linux block devices from a userspace process through io_uring. This site explains the kernel protocol and documents **go-ublk**, a pure-Go library, and **lib-ublk**, a Zig core with a C ABI and language bindings. The benchmarks report measured RAM workloads with their evidence. lib-ublk's source release is pending."
heroLinks:
  - label: "Learn how ublk works"
    url: "/guide/"
    primary: true
  - label: "Get started with go-ublk"
    url: "/go-ublk/getting-started/"
  - label: "Explore lib-ublk"
    url: "/lib-ublk/"
  - label: "Read the benchmarks"
    url: "/reference/benchmarks/"
---

<div class="cards">
<a class="card" href="/guide/">
<p class="card-title">The ublk guide</p>
<p>The kernel protocol, from control commands to request delivery, copy modes, recovery, and feature availability. For server authors working in any language.</p>
</a>
<a class="card" href="/go-ublk/">
<p class="card-title">go-ublk</p>
<p>A pure-Go engine with a <code>Backend</code> or raw request <code>Handler</code> interface. No cgo or liburing. The library handles io_uring, feature negotiation, and the device lifecycle.</p>
</a>
<a class="card" href="/lib-ublk/">
<p class="card-title">lib-ublk</p>
<p>A Zig core with a C ABI and caller-owned step/poll queues. Bindings for C, C++, Rust, Go, Python, Zig, and lang. Source release pending.</p>
</a>
<a class="card" href="/reference/benchmarks/">
<p class="card-title">Comparative benchmarks</p>
<p>An overhead ladder from the kernel floor through userspace null and RAM targets. Completion latency, throughput, and CPU cost, with evidence and qualification limits.</p>
</a>
</div>

<div class="prose-narrow">

## How it fits together

An application reads and writes `/dev/ublkbN` like any disk. The kernel's `ublk_drv` turns each block request into a descriptor and completes an io_uring command that your server left waiting. The server handles the request against its backend, then commits the result and re-arms the slot. The [architecture chapter](/guide/architecture/) walks a request through the path. The [UAPI reference](/reference/uapi/) lists the kernel commands and features.

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

## Library status and testing

[go-ublk v0.2.0](https://github.com/ehrlich-b/go-ublk/commit/e4e39b07522919fbe2ba11bb594c9c05e998ae24) implements the kernel interface through [Linux 7.3-rc5](https://github.com/ehrlich-b/go-ublk/blob/e4e39b07522919fbe2ba11bb594c9c05e998ae24/backend.go), including recovery and zero copy. Its API can change between minor releases. [Releases](/go-ublk/releases/) and [testing](/go-ublk/testing/) describe its current scope.

lib-ublk's RAM and multi-queue device stages passed on [six kernels](/evidence/2026-10-10/lib-ublk-validation.md). Its bindings passed RAM data-integrity stages on [Linux 6.12](/evidence/2026-10-10/lib-ublk-validation.md), and the property campaign passed [200,000 cases per property](/evidence/2026-10-10/lib-ublk-property-tests.md). Its [source release is pending](/lib-ublk/status/), and a license hasn't been selected.

The [compatibility matrix](/reference/matrix/) keeps the libraries' test scopes separate. [Known kernel bugs](/guide/kernel-bugs/) includes the zero-copy UBSAN finding and the source analysis behind it.

</div>
