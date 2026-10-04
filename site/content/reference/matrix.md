---
title: "Compatibility matrix"
linkTitle: "Compatibility matrix"
description: "go-ublk test results per kernel and distribution: which kernels load ublk_drv, which pass the suite, and which oops."
weight: 20
notoc: true
wide: true
---

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

{{< matrix >}}
