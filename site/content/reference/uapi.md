---
title: "UAPI reference"
linkTitle: "UAPI reference"
description: "Every ublk control command, I/O command, feature flag, parameter type, I/O operation and flag, device state, structure and constant, with the Linux release that introduced it and its go-ublk status."
weight: 10
notoc: true
wide: true
---

One row per item in the kernel's `include/uapi/linux/ublk_cmd.h`. Filter by kind or by go-ublk status, or search by name. Every row has a stable anchor, so `/reference/uapi/#ublk_f_user_recovery` links straight to a flag. Expand **Details** for semantics, dependencies, the introducing commit and go-ublk notes.

**Since** is the first mainline release whose header defines the item. A few items were defined before the kernel implemented them (`UBLK_F_SUPPORT_ZERO_COPY` was reserved in 6.0 but only works from 6.15), and the ioctl-encoded `UBLK_U_*` commands date from 6.4 even where the legacy opcode is older; the details say so where it matters. The [kernel version history](/guide/kernel-versions/) tells the same story release by release.

**go-ublk** status: *supported* means the library uses or exposes the item today; *partial* means some of its semantics; *missing* means not yet; *n/a* means there is nothing for a server to do.

{{< uapi-table >}}
