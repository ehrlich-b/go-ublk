---
title: "Language bindings"
linkTitle: "Language bindings"
description: "How each language uses the core, owns its queues, and handles request buffers."
weight: 10
---

Every binding below passed its RAM data-integrity runbook stages on [Linux 6.12](/evidence/2026-10-10/lib-ublk-validation.md). These runs qualify the RAM example's device I/O and lifecycle. The [validation record](/evidence/2026-10-10/lib-ublk-validation.md) lists the shared scope and the example restrictions.

| Language | Interface | Ownership and request handling |
|---|---|---|
| C | Opaque handles and fixed-width records through the C ABI | The caller supplies queue threads, drives step/poll, and explicitly completes borrowed requests. |
| C++ | Header-only wrapper with move-only handles and error codes | Request views expire on completion. Explicit STOP, drain, and close report lifecycle errors. Exceptions aren't required. |
| Rust | Raw FFI crate and an ownership wrapper | A request borrows its queue, and buffers borrow the request. Queues stay on their owning thread. Shutdown is explicit and fallible. |
| Go | cgo binding with batch dispatch | A dedicated goroutine stays locked to its queue's OS thread. C-backed slices are valid only in the handler callback. |
| Python | Standard-library ctypes binding | Each queue belongs to a Python thread. Handler memoryviews use staging copies and are released before completion. |
| Zig | Native module calling the core directly | Allocator-owned handles and borrowed request buffers follow the same queue and lifecycle rules. |
| lang | C ABI binding compiled through LLVM | Record accessors bridge the language's field layout to C. Consumer-owned C glue supplies signal handling and a control thread. |

## Shared contract

A queue has a fixed owner. Cross-thread wake is available, with destruction synchronized by the caller. Control timeouts require reconciliation before another control operation. Successful completion ends the buffer loan; failed completion retains it. Closing a handle can refuse while work or children remain, so callers must observe the result.

The native Zig and lang RAM examples each use a single queue. Other example geometry is recorded in [testing](/lib-ublk/testing/); the binding run on the baseline kernel doesn't qualify every binding on every kernel.

## Go binding and go-ublk

lib-ublk's Go binding uses cgo and the shared Zig engine. [go-ublk](/go-ublk/) has its own pure-Go engine and builds without cgo or liburing. Choose based on the engine and deployment model your application needs. The comparative [benchmark run](/reference/benchmarks/) measured the C RAM consumer of lib-ublk; it doesn't establish equal throughput across bindings.

## lang compiler findings

The binding uses LLVM to avoid the older backend's byte-store and entry-point issues, with explicit accessors for C record layouts and return values. Direct pointer indexing still needs typed-dereference workarounds; the ABI and layout checks are described in the [binding evidence](/evidence/2026-10-10/lib-ublk-validation.md).

**Source release pending.** The binding packages aren't available for installation yet.
