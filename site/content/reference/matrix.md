---
title: "Compatibility matrix"
linkTitle: "Compatibility matrix"
description: "go-ublk test results per kernel and distribution: which kernels load ublk_drv, which pass the suite, and which oops."
weight: 20
notoc: true
wide: true
---

The 67 rows record attempted kernel/configuration runs in disposable VMs, grouped by kernel family. Expand a row for individual tests and reported feature flags.

Statuses:

| Status | Meaning |
|---|---|
| pass | Every test that ran passed. Skips are listed in the detail. |
| fail | At least one test failed. The detail says which and why. |
| timeout | The run did not finish in its time budget, which on this project usually means a hang. |
| no ublk_drv | The kernel booted without `ublk_drv`. Device tests were skipped; unit tests may still run. |
| boot-failed | The guest never came up. Says nothing about ublk. |
| fetch-failed | The kernel package could not be downloaded. Says nothing about ublk. |

An **oops** badge records a kernel oops or BUG regardless of the verdict. See [Known kernel bugs](/guide/kernel-bugs/) and [Testing and compatibility](/go-ublk/testing/).

{{< matrix >}}
