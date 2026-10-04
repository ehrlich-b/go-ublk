---
title: "go-ublk"
navTitle: "go-ublk"
description: "A Go library for Linux ublk block devices. Pure Go: no cgo, no liburing, builds with CGO_ENABLED=0."
weight: 20
---

go-ublk is a library for serving Linux block devices from Go. You implement a `Backend`, an interface shaped like `io.ReaderAt` and `io.WriterAt` plus `Size`, `Flush` and `Close` — or a raw request `Handler` — and the library does the rest: it negotiates features and speaks the ublk control protocol on `/dev/ublk-control`, runs an I/O thread with its own io_uring per hardware queue on `/dev/ublkcN`, runs your backend concurrently, keeps the device alive across server restarts if you ask, and tears it down in the order the kernel needs.

```sh
go get github.com/ehrlich-b/go-ublk
```

It is written entirely in Go. The io_uring setup, the submission and completion rings, the memory barriers and the ublk UAPI structures are implemented in the module on top of `golang.org/x/sys/unix`, so a binary builds with `CGO_ENABLED=0` and cross-compiles like any other Go program. API documentation is on [pkg.go.dev](https://pkg.go.dev/github.com/ehrlich-b/go-ublk).

**It is a library.** The deliverable is the public API; storage backends belong to the program that imports it. The two programs in [`examples/`](https://github.com/ehrlich-b/go-ublk/tree/main/examples), a RAM disk and a losetup-style file exporter, exist to prove that contract and use nothing outside the public API.

> [!NOTE]
> go-ublk v0.2.0 implements the whole ublk kernel interface as of Linux 7.3-rc5 and is tested by a real-kernel conformance suite under many kernels — see [Testing and compatibility](/go-ublk/testing/) and the [compatibility matrix](/reference/matrix/). It is pre-1.0, so the API may still change. Read [Deployment](/go-ublk/deployment/) before running it under a filesystem that matters.
