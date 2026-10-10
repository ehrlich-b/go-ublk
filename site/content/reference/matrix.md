---
title: "Compatibility matrix"
linkTitle: "Compatibility matrix"
description: "lib-ublk's RAM device qualification and go-ublk's per-kernel conformance results, with their separate test scopes."
weight: 20
notoc: true
wide: true
---

The libraries have different qualification scopes. lib-ublk's table records its core null/RAM device stages. go-ublk's rows below record its broader conformance suite, with skips and faults preserved.

## lib-ublk device stages

{{< lib-ublk-matrix >}}

These stages cover M0 and M1a, including RAM with [1/2/4/8 queues](/evidence/2026-10-10/lib-ublk-validation.md). The full core runbook passed [33/33 stages on Ubuntu 6.8 and Debian 6.12](/evidence/2026-10-10/lib-ublk-validation.md). Every [language binding](/lib-ublk/bindings/) passed its RAM data-integrity stages on [Linux 6.12](/evidence/2026-10-10/lib-ublk-validation.md); binding coverage on the other kernels isn't established here.

## go-ublk conformance runs

Each row is one boot of one kernel running the go-ublk test suite in a disposable virtual machine. Rows are grouped by kernel family. Expand a row for per-test results and the feature flags the kernel reported.

How to read a status:

| Status | Meaning |
|---|---|
| pass | Every test that ran passed. Skips are listed in the detail. |
| fail | At least one test failed. The detail says which and why. |
| timeout | The run did not finish in its time budget, which on this project usually means a hang. |
| no ublk_drv | The kernel booted but has no `ublk_drv` module, so nothing could run. |
| boot-failed | The guest never came up. Says nothing about ublk. |
| fetch-failed | The kernel package could not be downloaded. Says nothing about ublk. |

An **oops** badge means the kernel logged an oops or BUG during the run, whatever the test verdict. Kernel bugs that are known to bite ublk servers are described in [Known kernel bugs](/guide/kernel-bugs/), and the test layers themselves in [Testing and compatibility](/go-ublk/testing/).

The new [zero-copy UBSAN evidence](/evidence/2026-10-10/kernel-zero-copy-ubsan.md) records reports on [7.0-7.2](/evidence/2026-10-10/kernel-zero-copy-ubsan.md) and a clean observed run on [7.3-rc3](/evidence/2026-10-10/kernel-zero-copy-ubsan.md). A passing suite doesn't erase a sanitizer report; the likely counted_by ordering issue is described in the [kernel bug guide](/guide/kernel-bugs/#found-by-go-ublks-kernel-matrix).

{{< matrix >}}
