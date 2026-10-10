---
title: "lib-ublk"
navTitle: "lib-ublk"
description: "A Zig ublk engine with a C ABI and caller-owned step/poll queues. Source release pending."
weight: 25
---

lib-ublk serves Linux userspace block devices through a Zig core with a C ABI. Applications drive each queue with a nonblocking step call or a bounded poll call. The core starts no hidden threads, so the application owns the queue threads and chooses how to wait for work.

Bindings cover C, C++, Rust, Go, Python, Zig, and lang, my own language through its LLVM backend. The [language bindings](/lib-ublk/bindings/) share the core's lifecycle and request contract. The native Zig package calls the core directly; the other bindings use the C ABI.

The tested targets are a null device and a RAM disk. RAM retains writes for the device's lifetime, and discard and write-zeroes clear the requested range. FLUSH completes without a persistence promise. The [validation record](/evidence/2026-10-10/lib-ublk-validation.md) describes this scope.

## Driving a queue

Open and prime every accepted queue before starting the device. Each queue stays on its owning Linux thread from open through close. A step returns request tokens; the application handles the requests, completes them, and steps again. The readiness API exposes borrowed descriptors and a prepare-wait check for integration with an event loop.

Control calls are serialized by the application. Queue owners keep driving while START and STOP complete. After STOP, drain the queue completions, close the queues, delete the device, and close its handles. A timed-out control command retains its storage until reconciliation resolves the late result.

## Qualification

The RAM and multi-queue device stages passed on [six kernels](/evidence/2026-10-10/lib-ublk-validation.md), and every listed binding passed its RAM data-integrity runbook stages on [Linux 6.12](/evidence/2026-10-10/lib-ublk-validation.md). [Testing and the kernel matrix](/lib-ublk/testing/) separate those kernel results from the seeded property campaign.

**Source release pending.** A license hasn't been selected. [Status and scope](/lib-ublk/status/) records the current limits; [benchmarks](/reference/benchmarks/) reports the measured RAM workloads.
