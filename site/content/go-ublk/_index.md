---
title: "go-ublk"
navTitle: "go-ublk"
description: "A Go library for Linux ublk block devices. Pure Go: no cgo, no liburing, builds with CGO_ENABLED=0."
weight: 20
---

Serve Linux block devices from Go. Supply a `Backend` (`io.ReaderAt`/`io.WriterAt` plus `Size`, `Flush`, `Close`) or a raw request `Handler`. go-ublk negotiates features on `/dev/ublk-control`, runs an I/O thread and io_uring per hardware queue on `/dev/ublkcN`, dispatches concurrent backend calls, handles teardown, and optionally preserves devices across server restarts.

```sh
go get github.com/ehrlich-b/go-ublk
```

The UAPI structures, io_uring setup, rings, and memory barriers are pure Go atop `golang.org/x/sys/unix`. Build with `CGO_ENABLED=0` and cross-compile normally. See the [API documentation](https://pkg.go.dev/github.com/ehrlich-b/go-ublk).

Your program owns storage. The [RAM disk and file exporter](https://github.com/ehrlich-b/go-ublk/tree/main/examples) demonstrate the public API.

> [!NOTE]
> v0.2.0 covers the 7.3-rc5 UAPI; [Testing](/go-ublk/testing/) and the [matrix](/reference/matrix/) record real-kernel coverage. The pre-1.0 API may change. Read [Deployment](/go-ublk/deployment/) before serving a filesystem that matters.