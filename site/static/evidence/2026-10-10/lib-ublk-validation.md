# lib-ublk validation record

Recorded 2026-10-10. This public extract combines the test coordinator's verified
kernel and binding results with the core and binding documentation. Kernel and
binding rows are coordinator-reported runbook results; raw guest logs aren't
included in this extract. Property results have a separate campaign ledger.

## Engine and scope

The engine is a Zig core with a C ABI and a caller-owned step/poll API. There are
no hidden core threads. Each queue stays on one Linux owner thread. Control calls
are caller-serialized. Timeouts retain kernel-visible command storage until
reconciliation, and successful request completion ends the buffer loan.

The qualified device stages cover M0's null target and M1a's RAM target with
1/2/4/8 queues, using the copy data path. RAM retains data for the device lifetime.
READ and WRITE transfer the full range; DISCARD and WRITE_ZEROES clear the range.
FLUSH has no payload and makes no persistence promise. Required unprivileged,
recovery, and zero-copy modes remain unsupported.

Source release pending. A license hasn't been selected.

## Core kernel matrix

| Kernel family | Kernel | M0/M1a device stages |
|---|---|---|
| Ubuntu | 6.8 | pass |
| Debian | 6.12 | pass |
| Ubuntu | 7.0 | pass |
| Mainline | 7.2.6 | pass |
| Fedora | 7.2.9 | pass |
| Mainline | 7.3-rc3 | pass |

Revision 49cfa6b passed M0 on Debian 13 / 6.12.111: 17/17 runbook stages,
36 unit tests, 14 Linux tests, and ten lifecycle reruns. Revision 827b90a failed
multi-queue startup because of independent exclusive character-device opens.
Corrected revision ee36776 passed M1a on Debian 6.12.111: 33/33 stages,
63 unit tests, 22 Linux tests, queues 1/2/4/8, CRC32C fio verification,
and 100 four-queue lifecycles without new kernel warnings.

Revision 972b11b passed all device stages on the six listed kernels. The full
33-stage core runbook passed on Ubuntu 6.8 and Debian 6.12 after the portable
dmesg correction. Device-stage passes on the other rows don't establish the
full binding matrix or every kernel feature.

## Binding qualification on Linux 6.12

| Language | RAM data-integrity runbook stages | Interface |
|---|---|---|
| C | pass | C ABI with caller-owned queue threads |
| C++ | pass | Header-only, move-only handles and fallible lifecycle |
| Rust | pass | Raw FFI and ownership wrapper with borrowed requests |
| Go | pass | cgo, fixed-thread queue goroutine, callback-scoped C buffers |
| Python | pass | ctypes, thread-owned queues, staging-copy memoryviews |
| Zig | pass | Native module calling the core directly |
| lang | pass | C ABI through the LLVM backend |

The C++/Zig/Rust RAM qualification was reported at 2f0303c, with 39/39 selected
runbook stages. The later verified binding results include Go, Python, and lang.
The native Zig and lang RAM examples each use one queue. This record doesn't
qualify every language on every core-kernel row.

The shared data-integrity stages compare direct writes and reads, including
device-end samples, and check shutdown and resource cleanup. Core stages add
CRC32C fio verification, lifecycle repetitions, and new-kernel-fault checks.
Requested missing bindings, skipped stages, timeouts, mismatches, and leaks
don't count as passes.

## Fixed defects exposed by kernel testing

| Defect | Fix |
|---|---|
| Primed FETCHes remained after failed START | STOP even after partial priming or rejected START, then drive owners until abort CQEs drain. |
| FLUSH's sentinel sector overflowed byte-address arithmetic | Decode FLUSH without interpreting wire sector/range fields as an addressed payload. |
| A new character-device open for each queue returned EBUSY | Share one open file description through duplicated per-queue descriptors. |
| Newer kernels returned additive capability flags the library rejected | Admit PER_IO_DAEMON, BUF_REG_OFF_DAEMON, and SAFE_STOP_DEV while retaining rejection of unknown bits and unrequested I/O modes. |
| A udev/blkid probe raced a held-request test | Arm after successful client open and match only the intended direct READ, claiming it once across queues. |

## lang binding and compiler checks

The lang binding uses LLVM's byte stores and ordinary C main entry point. Explicit
record accessors accommodate the language's eight-byte field slots, and errno
returns normalize C's low 32 bits. Typed dereferences replace direct pointer
indexing that emitted invalid LLVM IR. C fixtures check record layouts and
adjacent canaries; consumer-owned C glue supplies signals and the control thread.

## Property campaign

Committed source 5f4a920e1a8a0da6b5ee32e91c33715fe78305a4 passed 200,000 seeded
cases each for decoding, completions, lifecycle against a model, and records,
with zero failures. Seed ranges and timings are in lib-ublk-property-tests.md.
Coverage-guided fuzzing is outside this qualification.

## Comparative benchmark environment

The measured environment was Linux 6.12 in a KVM guest on a Ryzen 9 6900HX,
pinned to four dedicated physical cores. Server and fio shared a four-CPU budget.
The comparative baseline used three interleaved rounds. The 4 KiB random-read
A/A noise floor was 6%; quiet-window and matched-peer gates remained incomplete.
The measured lib-ublk executable was the C RAM consumer at source revision
e0743b3eb034a8343f7762ea25f44f26193e67ce.
