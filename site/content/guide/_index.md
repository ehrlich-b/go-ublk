---
title: "The ublk guide"
navTitle: "ublk guide"
description: "How Linux ublk works, for anyone writing a ublk server in any language: architecture, control and data planes, every feature flag, and the kernel history behind them."
weight: 10
---

ublk lets a userspace process implement a Linux block device. Its driver, `ublk_drv`, entered mainline in Linux 6.0; additions in most releases since include recovery, unprivileged devices, user copy, zoned devices, two generations of zero copy, batch I/O, and integrity metadata.

This guide is for server authors using C, Rust, Go, or any language that can issue io_uring commands. It assumes familiarity with block devices and io_uring, but no driver knowledge. Examples use C-flavored pseudocode against the UAPI structures every server marshals.

The sources are the Linux 7.3-rc5 UAPI header, driver, single-page kernel documentation, and reference servers. Version changes are noted inline and in the [kernel history](/guide/kernel-versions/); go-ublk-specific behavior is labeled.

Start with [architecture](/guide/architecture/), [control](/guide/control-plane/), and [data](/guide/data-plane/), in that order. Later chapters stand alone.