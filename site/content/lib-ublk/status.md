---
title: "Status and scope"
linkTitle: "Status and scope"
description: "What's qualified, what remains unsupported, and the source-release status."
weight: 30
---

**Source release pending.** A license hasn't been selected, and there's no public source repository or installation package yet.

## Qualified scope

lib-ublk is experimental. The [validation record](/evidence/2026-10-10/lib-ublk-validation.md) covers the Zig core, C ABI, caller-owned step/poll queues, null and RAM targets, and language bindings. The [kernel matrix](/lib-ublk/testing/) records the passed device stages separately from binding qualification.

The [seeded property campaign](/evidence/2026-10-10/lib-ublk-property-tests.md) checks request decoding, completion sequences, lifecycle, and record layouts. The [benchmark report](/evidence/2026-10-10/bench-baseline-report.md) records measured RAM performance for a pinned C consumer. It makes no winner claim.

## Current limits

The tested core data path uses copy buffers. Required unprivileged, recovery, and zero-copy modes remain unsupported. A preferred capability can fall back visibly; a required unsupported capability fails. Kernel-advertised features and implemented modes are separate.

Null discards writes. RAM retains them only while the device exists. Neither target supplies durable storage, and a successful RAM FLUSH isn't a persistence guarantee. The experimental ABI can change before a stable release.

The current results don't establish production readiness, performance across every binding, or kernel support beyond the recorded cells.
