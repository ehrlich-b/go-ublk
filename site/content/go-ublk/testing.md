---
title: "Testing and compatibility"
linkTitle: "Testing & compatibility"
description: "How go-ublk is tested, from unit tests to crash and power-fail oracles, and which kernels it is verified on."
weight: 70
---

A block device that returns the wrong bytes is worse than one that returns errors, so go-ublk's testing is organized around one question: can this test fail? Every harness below that judges data correctness was first shown to catch an injected fault.

## Layers

| Layer | What it covers | Where it runs |
|---|---|---|
| Unit tests | UAPI struct layouts and every constant against fixtures compiled from the 7.3-rc5 C header; ioctl encodings; the io_uring core (SQE layouts, real rings, registration, CQ overflow, a 500-ring leak test); every control command against a fake driver; the queue engine against a fake-kernel model of ublk_drv (copy, user copy, NEED_GET_DATA, zero copy with auto and manual registration, batch I/O with partial commits, shared memory, stop and abandon, errno mapping, a deterministic lost-wakeup test); both example backends | `make test-unit`, any Linux box; CI on every push |
| Race and static checks | `go test -race`, `gofmt`, `go vet` | CI |
| Fuzzing | The UAPI decoders, the control-plane decoders, and `FuzzEngine`, which drives the real engine through random request, completion and shutdown scripts against the fake kernel | CI (15 s per target), `make test-uapi-fuzz` |
| Conformance suite | `ublk-suite`: about 60 real-kernel tests — integrity across queue/depth/block-size combinations, boundaries, flush, FUA, discard and write-zeroes up to 5 GiB, errno propagation, ext4 and xfs, every feature (recovery after SIGKILL, Detach/Recover handoff, zero copy, batch I/O, zoned, integrity, shared memory, unprivileged, resize, safe stop, partition scan), teardown under load, leaks, concurrent creates, chaos. One JSON result per test | `make suite`, then run the binary as root on a disposable machine |
| Kernel matrix | `test/matrix` boots `ublk-suite` under mainline kernels 6.0–7.3-rc and the current kernels of Ubuntu, Debian, Fedora, RHEL-family, openSUSE, Arch and Amazon Linux, in QEMU (TCG locally, KVM in CI) | [compatibility matrix](/reference/matrix/) |
| Kernel integration | Device creation through the public API on a real kernel, including full-size (1 MiB) requests on both startup paths | disposable VM, root, `GO_UBLK_DISPOSABLE_TEST=1 make test-large-io-kernel` |
| End to end | I/O through `/dev/ublkbN` with `dd` and `fio` | `make vm-simple-e2e`, `make vm-e2e` |
| Integrity sweep | A shadow-copy oracle drives random reads and writes and compares every byte, across queues 1/2/4/8 × depth 1/64/128 × buffered/O_DIRECT (24 combinations) | `make vm-verify` |
| Example backends | `ublk-loop` offset mapping across teardown, discard space reclaim, write-cache attributes, read-only; `ublk-mem --zip` round trips (14 checks) | `make vm-loop-e2e` |
| Teardown churn | Repeated create, load, SIGINT, teardown; checks for leaked devices, D-state tasks and reference counts | `scripts/vm-churn.sh` |
| Crash consistency | SIGKILL the daemon mid-write, recover, verify | `make vm-crash` |
| Power-fail consistency | Hard-reset the VM (`sysrq-b`) mid-write, reboot, verify | `make vm-powerfail` |
| Shutdown storm | Normal reboot with a mounted ext4 under load; hunts for oopses, lost writeback and wedged reboots | `make vm-shutdown-storm` |
| Stress and performance | Alternating e2e and benchmark runs; fio against a loop baseline | `make vm-stress`, `make vm-benchmark` |

### The crash oracle

The crash and power-fail tests have to judge data after the process that wrote it (or the whole machine) has died, so the expected content cannot live in memory:

- Every block is **self-describing**: a magic number, its own block index, a generation number, and a payload derived from both. A block found at the wrong offset is *aliased*; a block whose payload disagrees with its header is *torn*. Neither verdict needs remembered state.
- One region is rewritten generation by generation, each pass followed by `fdatasync`, and only then is a **durability witness** advanced on disk (fsync, rename, directory fsync, so the witness is itself crash-safe). After the crash, any block older than the witness is a *lost* acknowledged write. The driver waits for several flushed generations before crashing, because with a witness of 1 the requirement is vacuous.
- A second region is never synced, so every crash lands with unflushed writes in flight, striped one writer per block so "torn" is a real verdict, not a race.
- A self-test injects a lost, an aliased and a torn block into a plain file and checks that each is caught, and the power-fail verification re-checks the surviving image against an over-claiming witness and requires it to fail.

Each cycle also asserts recovery: the writer blocked on the dead device gets an error rather than hanging, no device leaks after reaping, the device comes back on the same image, a graceful stop works afterwards, there is no oops, and the boot ID is unchanged (or changed, for the hard-reset runs, which is how they prove the reset happened).

### Suspect the harness first

Nine "product hangs" in this project's history were test-harness accidents, including a churn script that parsed the `1` out of `USR1` and sent SIGINT to PID 1 (which reboots a systemd VM) and an e2e script whose cleanup killed its own ssh session. The harnesses now refuse to signal PID 1 or below, match processes by exact name, and each judgment comes with a control that shows it can fail. Treat a new hang the same way before blaming the library or the kernel.

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
| 2026-10-04 | `7.0.0-38-generic` (`linux-hwe-7.0`) | x86_64 | QEMU (TCG) guest | v0.2.0 engine: `ublk-suite` 54/54 applicable tests (2 skipped: features newer than 7.0); systemd recovery under a verifying fio — 2 upgrade handoffs and 1 SIGKILL, 0 I/O errors |

All runs show no oops, no stuck tasks and no leaked devices unless noted. Timings and IOPS from the emulated (TCG) run are meaningless.

These are the hand-run verifications; the [compatibility matrix](/reference/matrix/) has the automated runs of the conformance suite across mainline and distribution kernels. The minimum is Linux 6.4, the first kernel with ioctl-encoded commands; kernels before 6.11 needed the write-zeroes limit fix in v0.2.0 (Critical Bug #24).

## What is not covered

- A real host power cut. The guest hard reset drops the guest's page cache, not the host's cache of the virtual disk.
- Long soak tests, memory-pressure and GC-pressure fault injection.
- Real hardware outside VMs: the v0.2.0 runs used emulated CPUs, so they say nothing about performance.
- Recovery of zero-copy, zoned and shared-memory devices: `Recover` supports the first two, but only the default, batch and integrity configurations are tested; shared-memory regions cannot be served after a recovery.

## Running the tests

Unit tests need Linux; on macOS, `make vm-test-unit` cross-compiles them and runs them on the test VM. Everything with `vm-` in its name runs against a VM configured in `Makefile.local` (`VM_SSH`, `VM_SCP`, or `VM_HOST`/`VM_USER`; see `Makefile.local.example` and `docs/VM_TESTING.md`). These targets load modules, create and delete ublk devices, kill processes and reboot the machine: use a disposable VM, never a host you care about.

```sh
make test-unit          # Linux
make vm-test-unit       # from macOS
make vm-verify          # integrity sweep
make vm-crash           # crash consistency
make vm-powerfail       # power-fail consistency
make vm-shutdown-storm STORM_CYCLES=5
```

The oracles in `test/verify` and `test/crash` are standalone programs and work against any block device, so they can test your own backend as well as go-ublk's examples.
