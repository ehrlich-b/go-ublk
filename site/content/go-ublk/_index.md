---
title: "go-ublk"
navTitle: "go-ublk"
description: "A Go library for Linux ublk block devices. Pure Go: no cgo, no liburing, builds with CGO_ENABLED=0."
weight: 20
---

go-ublk is a library for serving Linux block devices from Go. You implement a `Backend`, an interface shaped like `io.ReaderAt` and `io.WriterAt` plus `Size`, `Flush` and `Close`, and the library does the rest: it speaks the ublk control protocol on `/dev/ublk-control`, runs one io_uring per hardware queue on `/dev/ublkcN`, moves requests between the kernel and your backend, and tears the device down in the order the kernel needs.

```sh
go get github.com/ehrlich-b/go-ublk
```

It is written entirely in Go. The io_uring setup, the submission and completion rings, the memory barriers and the ublk UAPI structures are implemented in the module on top of `golang.org/x/sys/unix`, so a binary builds with `CGO_ENABLED=0` and cross-compiles like any other Go program. API documentation is on [pkg.go.dev](https://pkg.go.dev/github.com/ehrlich-b/go-ublk).

**It is a library.** The deliverable is the public API; storage backends belong to the program that imports it. The two programs in [`examples/`](https://github.com/ehrlich-b/go-ublk/tree/main/examples), a RAM disk and a losetup-style file exporter, exist to prove that contract and use nothing outside the public API.

> [!WARNING]
> go-ublk is a prototype approaching usable, not a production-hardened library. The data path is verified byte-exact on arm64 and x86_64 under crash and power-fail tests, but several lifecycle defects are open and user recovery is not implemented. Read [Testing and compatibility](/go-ublk/testing/) and [Deployment](/go-ublk/deployment/) before trusting it with data.
