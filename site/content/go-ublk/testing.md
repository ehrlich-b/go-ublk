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
| Unit tests | UAPI struct layouts and marshaling against offsets taken from the real kernel headers; ioctl encoding; SQE128/CQE32 field offsets; ring index wraparound; the per-tag state machine; the descriptor mmap stride; request dispatch and result codes; lifecycle and teardown ordering with fakes; real io_uring round trips (no ublk); metrics percentiles; both example backends | `make test-unit`, any Linux box; CI on every push |
| Race and static checks | `go test -race`, `gofmt`, `go vet` | CI |
| Fuzzing | Bounded fuzzing of the fixed-layout UAPI decoders and the parameter decoder | CI (15 s per target), `make test-uapi-fuzz` |
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

All runs show no oops, no stuck tasks and no leaked devices unless noted. Timings and IOPS from the emulated (TCG) run are meaningless.

The documented minimum is Linux 6.8. Kernels from 6.4 (the first with ioctl-encoded commands) through 6.16 are expected to work but have not been verified. The [compatibility matrix](/reference/matrix/) collects broader automated runs across distributions and kernel versions as they complete.

## What is not covered

- A real host power cut. The guest hard reset drops the guest's page cache, not the host's cache of the virtual disk.
- Long soak tests, memory-pressure and GC-pressure fault injection.
- Real hardware outside VMs, and continuous testing against a real kernel in CI (CI runs unit tests only).
- Kernel features go-ublk does not use yet (see the [roadmap](/go-ublk/roadmap/)).

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
