---
title: "The ublk guide"
navTitle: "ublk guide"
description: "How Linux ublk works, for anyone writing a ublk server in any language: architecture, control and data planes, every feature flag, and the kernel history behind them."
weight: 10
---

ublk lets a userspace process implement a Linux block device. The kernel side, `ublk_drv`, has been in mainline since Linux 6.0 and has grown a feature in almost every release since: user recovery, unprivileged devices, user copy, zoned devices, two generations of zero copy, batch I/O and integrity metadata. The official documentation is a single page in the kernel tree, and the rest of the knowledge lives in the UAPI header, the driver source and the reference servers. This guide collects it in one place.

It is written for people building a ublk server, in C, Rust, Go or anything else that can issue io_uring commands. It assumes you know what a block device is and have at least seen io_uring; it does not assume you have read the driver. Code samples are C-flavored pseudocode against the UAPI structures, because those are what every language ends up marshaling.

Kernel facts here are taken from the upstream header as of Linux 7.3-rc5, the driver source, the kernel's own ublk documentation, and the reference servers. Where behavior changed between releases, the text says which release, and the [kernel version history](/guide/kernel-versions/) lists every addition in one table. Where the guide describes go-ublk specifically, it says so; everything else applies to any ublk server.

Read the first three chapters in order. After that, each chapter stands alone.
