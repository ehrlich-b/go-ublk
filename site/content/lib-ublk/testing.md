---
title: "Testing and kernel matrix"
linkTitle: "Testing and kernel matrix"
description: "Real-kernel RAM qualification, seeded properties, and defects found during kernel testing."
weight: 20
---

## Real-kernel qualification

The M0 and M1a device stages cover a null target, a RAM target, and [1/2/4/8 queues](/evidence/2026-10-10/lib-ublk-validation.md). The corrected implementation passed the device stages on the kernels below. The [validation record](/evidence/2026-10-10/lib-ublk-validation.md) identifies the earlier failed revisions and the scope of each reported result.

{{< lib-ublk-matrix >}}

The full core runbook passed [33/33 stages on Ubuntu 6.8 and Debian 6.12](/evidence/2026-10-10/lib-ublk-validation.md). Every binding passed its RAM data-integrity stages on [Linux 6.12](/evidence/2026-10-10/lib-ublk-validation.md). Binding coverage on the other kernels hasn't been established by these results.

The runbook checks direct write/read comparisons, CRC32C fio verification, lifecycle cleanup, leaked devices and mappings, and new kernel warnings. A skip or timeout doesn't count as a pass. RAM qualification covers volatile storage; it doesn't establish durability across a crash.

## Seeded property campaign

The [committed campaign ledger](/evidence/2026-10-10/lib-ublk-property-tests.md) records independent seeds for each case, with these results:

| Property | Cases | Failures |
|---|---:|---:|
| Request decoding | [200,000](/evidence/2026-10-10/lib-ublk-property-tests.md) | [0](/evidence/2026-10-10/lib-ublk-property-tests.md) |
| Completion sequences | [200,000](/evidence/2026-10-10/lib-ublk-property-tests.md) | [0](/evidence/2026-10-10/lib-ublk-property-tests.md) |
| Lifecycle against a model | [200,000](/evidence/2026-10-10/lib-ublk-property-tests.md) | [0](/evidence/2026-10-10/lib-ublk-property-tests.md) |
| Versioned record byte round trips | [200,000](/evidence/2026-10-10/lib-ublk-property-tests.md) | [0](/evidence/2026-10-10/lib-ublk-property-tests.md) |

These are bounded randomized properties with replayable seeds. They supplement deterministic models and real-kernel testing. Coverage-guided fuzzing isn't qualified by this campaign.

## Defects exposed by real kernels

The [validation record](/evidence/2026-10-10/lib-ublk-validation.md) records these fixes:

| Defect | Correction |
|---|---|
| Failed START left primed FETCH commands to drain | Run STOP after partial priming or failed START, and keep the owners driving until abort completions drain. |
| FLUSH's sentinel sector overflowed address arithmetic | Decode FLUSH without treating its wire range as an addressed payload. |
| Independent character-device opens failed with multiple queues | Open once, and duplicate the surviving file description for later queues. |
| Additive capability flags returned by newer kernels were rejected | Admit compatible capability flags while continuing to reject unrequested I/O modes and unknown bits. |
| A udev probe raced a held-request test | Arm the hold after client open, and match the intended direct request exactly. |

The [comparative benchmarks](/reference/benchmarks/) have a separate measurement scope. The [kernel bug guide](/guide/kernel-bugs/) tracks findings in the kernel itself.
