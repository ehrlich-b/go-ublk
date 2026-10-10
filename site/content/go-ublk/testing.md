---
title: "Testing and compatibility"
linkTitle: "Testing & compatibility"
description: "How go-ublk is tested, from unit tests to crash and power-fail oracles, and which kernels it is verified on."
weight: 70
---

Data-correctness harnesses include injected-fault controls: a passing byte comparison matters only if the verifier can detect corruption.

## Layers

| Layer | What it covers | Where it runs |
|---|---|---|
| Unit tests | UAPI struct layouts and every constant against fixtures compiled from the 7.3-rc5 C header; ioctl encodings; the io_uring core (SQE layouts, real rings, registration, CQ overflow, a 500-ring leak test); every control command against a fake driver; the queue engine against a fake-kernel model of ublk_drv (copy, user copy, NEED_GET_DATA, zero copy with auto and manual registration, batch I/O with partial commits, shared memory, stop and abandon, errno mapping, a deterministic lost-wakeup test); both example backends | `make test-unit`, any Linux box; CI on every push |
| Race and static checks | `go test -race`, `gofmt`, `go vet` | CI |
| Fuzzing | The UAPI decoders, the control-plane decoders, and `FuzzEngine`, which drives the real engine through random request, completion and shutdown scripts against the fake kernel | CI (15 s per target), `make test-uapi-fuzz` |
| Conformance suite | `ublk-suite`: about 60 real-kernel tests: integrity across queue/depth/block-size combinations, boundaries, flush, FUA, discard and write-zeroes up to 5 GiB, errno propagation, ext4 and xfs, every feature (recovery after SIGKILL, Detach/Recover handoff, zero copy, batch I/O, zoned, integrity, shared memory, unprivileged, resize, safe stop, partition scan), teardown under load, leaks, concurrent creates, chaos. One JSON result per test | `make suite`, then run the binary as root on a disposable machine |
| Kernel matrix | `test/matrix` boots `ublk-suite` under mainline kernels 6.0–7.3-rc and the current kernels of Ubuntu, Debian, Fedora, RHEL-family, openSUSE, Arch and Amazon Linux, in QEMU (TCG locally, KVM in CI) | [compatibility matrix](/reference/matrix/) |
| Kernel integration | Device creation through the public API on a real kernel, including full-size (1 MiB) requests on both startup paths | disposable VM, root, `GO_UBLK_DISPOSABLE_TEST=1 make test-large-io-kernel` |
| End to end | I/O through `/dev/ublkbN` with `dd` and `fio` | `make vm-simple-e2e`, `make vm-e2e` |
| Integrity sweep | A shadow-copy oracle drives random reads and writes and compares every byte, across queues 1/2/4/8 × depth 1/64/128 × buffered/O_DIRECT (24 combinations) | `make vm-verify` |
| Example backends | `ublk-loop` offset mapping across teardown, discard space reclaim, write-cache attributes, read-only; `ublk-mem --zip` round trips (14 checks) | `make vm-loop-e2e` |
| Teardown churn | Repeated create, load, SIGINT, teardown; checks for leaked devices, D-state tasks and reference counts | `scripts/vm-churn.sh` |
| Crash consistency | SIGKILL the daemon mid-write, recover, verify | `make vm-crash` |
| Power-fail consistency | Hard-reset the VM (`sysrq-b`) mid-write, reboot, verify | `make vm-powerfail` |
| Shutdown storm | Normal reboot with a mounted ext4 under load; hunts for oopses, lost writeback and wedged reboots | `make vm-shutdown-storm` |
| Soak | Hours of crc32c-verified fio on ext4 through the shipped systemd units; a crash or an upgrade handoff every few minutes in the first half, then the server's RSS and open fds sampled for leaks; ends with `e2fsck` and a clean stop. A selftest first proves the verifier catches one corrupted block | `make vm-soak` |
| Stress and performance | Alternating e2e and benchmark runs; fio against a loop baseline | `make vm-stress`, `make vm-benchmark` |

### The crash oracle

Crash oracles must recover expected data after the writer or machine dies:

- Blocks contain a magic number, block index, generation, and derived payload. Wrong location means aliased; payload/header disagreement means torn. Neither needs remembered state.
- Rewrite one region by generation, fdatasync each pass, then advance a disk durability witness using fsync/rename/directory fsync. Post-crash blocks older than the witness are lost acknowledged writes. Require several flushed generations before crashing; witness 1 proves nothing.
- Leave another region unsynced, with one writer per block, to ensure unflushed I/O without mistaking write races for tears.
- Self-tests inject lost/aliased/torn blocks into a plain file. Power-fail checks also require an overclaiming witness to reject the surviving image.

Each cycle checks that blocked writers receive errors, orphan cleanup leaks nothing, the same image can restart and stop gracefully, and no oops occurs. Boot ID must stay unchanged except on hard-reset runs, where a change proves the reset.

### Suspect the harness first

Nine apparent product hangs were harness faults, including parsing `1` from `USR1` and SIGINTing systemd PID 1, and cleanup killing its own ssh session. Harnesses now reject PID <= 1, match exact process names, and test their verdicts against failure controls. Check the harness before attributing a new hang.

## Verified kernels

| Date | Kernel | Arch | Environment | Result |
|---|---|---|---|---|
| 2026-07-24 | `6.17.0-1020-aws` | x86_64 | EC2 c6i.xlarge | Integrity sweep 24/24; 150/150 teardown-under-load cycles; 20/20 idle stops; no leaks or hangs |
| 2026-07-24 | `6.17.0-1019-aws` | x86_64 | same instance | Kernel oops on the first `ADD_DEV`: a [kernel packaging bug](/guide/kernel-bugs/). Same binary, A/B against -1020 |
| 2026-07-24 | `6.17.0-41-generic` | arm64 | Lima VM, Apple M4 | Sweep 24/24; 150/150 churn; 20/20 idle stops |
| 2026-07-26 | `6.17.0-41-generic` | arm64 | Lima VM | Crash: 8 SIGKILL cycles, 0 lost / 0 torn / 0 aliased. Power-fail: 4 hard resets, same |
| 2026-08-22 | `6.17.0-41-generic` | arm64 | Lima VM | Re-verified: unit, simple e2e, sweep 24/24, loop e2e 14/14, crash 6/6, 40-cycle churn; shutdown storm (23 reboots) |
| 2026-08-22 | `7.0.0-30-generic` (`linux-hwe-7.0`) | arm64 | Lima VM | Unit, simple e2e, sweep 24/24, loop e2e 14/14, crash 6/6 |
| 2026-10-03 | `7.0.0-38-generic` (`linux-hwe-7.0`) | x86_64 | QEMU (TCG) guest | Unit 9/9 packages, full-size I/O test, sweep 24/24, loop e2e 14/14, 1-8 GiB discards and a 3 GiB write-zeroes |
| 2026-10-04 | `7.0.0-38-generic` (`linux-hwe-7.0`) | x86_64 | QEMU (TCG) guest | Reboots under load with the shipped units, ext4 mounted and ~300 MB dirty: 3 of 5 lost writeback until the mount unit was ordered `Before=user.slice`, then 8 of 8 clean (no I/O errors, clean `e2fsck`) |
| 2026-10-04 | `7.0.0-38-generic` (`linux-hwe-7.0`) | x86_64 | QEMU (TCG) guest | 4-hour soak (`make vm-soak`) through the shipped units: 23 crash and upgrade handoffs, all recovered in 1.3–6.1 s; fio verified every block; server RSS 13.4 to 13.8 MB and fds 15 to 15 over two steady hours; `e2fsck` clean. The only kernel warning was the VM's virtual display (bochs vblank timeout) |
| 2026-10-04 | `7.0.0-38-generic` (`linux-hwe-7.0`) | x86_64 | QEMU (TCG) guest | v0.2.0 engine: `ublk-suite` 54/54 applicable tests (2 skipped: features newer than 7.0); systemd recovery under a verifying fio: 2 upgrade handoffs and 1 SIGKILL, 0 I/O errors |

Unless noted, runs had no oops, stuck tasks, or leaked devices. TCG timings/IOPS do not measure native performance.

These manual runs complement the automated [matrix](/reference/matrix/). Linux 6.4 is the minimum for ioctl-encoded commands; pre-6.11 kernels need v0.2.0's write-zeroes cap (Critical Bug #24).

## What is not covered

- Host power cuts: guest resets retain the host's virtual-disk cache.
- Soaks beyond the recorded hours, memory/GC pressure fault injection.
- Native hardware performance: v0.2.0 matrix CPUs were emulated.
- Zero-copy/zoned/shared-memory recovery: Recover supports the first two, but tests cover default/batch/integrity only. Recovered shared-memory registrations cannot be served.

## Running the tests

Unit tests need Linux; macOS uses `make vm-test-unit` to cross-compile and run in a VM. Configure VM targets through Makefile.local (VM_SSH/VM_SCP or VM_HOST/VM_USER); see Makefile.local.example and docs/VM_TESTING.md. They load modules, mutate devices, kill processes, and reboot: use a disposable VM.

```sh
make test-unit          # Linux
make vm-test-unit       # from macOS
make vm-verify          # integrity sweep
make vm-crash           # crash consistency
make vm-powerfail       # power-fail consistency
make vm-shutdown-storm STORM_CYCLES=5
```

`test/verify` and `test/crash` also test custom backends or any other block device.