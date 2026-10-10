---
title: "UAPI reference"
linkTitle: "UAPI reference"
description: "Every ublk control command, I/O command, feature flag, parameter type, I/O operation and flag, device state, structure and constant, with the Linux release that introduced it and its go-ublk status."
weight: 10
notoc: true
wide: true
---

Each `include/uapi/linux/ublk_cmd.h` item has a row and stable anchor, e.g. `/reference/uapi/#ublk_f_user_recovery`. Filter by kind/status or search names. **Details** gives semantics, dependencies, introducing commits, and go-ublk notes.

**Since** means first defined in a mainline header, sometimes before implementation: `UBLK_F_SUPPORT_ZERO_COPY` was reserved in 6.0 but works from 6.15. Encoded `UBLK_U_*` commands date from 6.4; legacy opcodes may be older. Details and the [kernel version history](/guide/kernel-versions/) explain these differences.

**go-ublk** status: *supported* = used/exposed; *partial* = some semantics implemented; *missing* = unimplemented; *n/a* = no server action needed.

{{< uapi-table >}}
