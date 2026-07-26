# TODO.md - Production Roadmap

## Current Status: Prototype — approaching usable, not yet production-hardened

go-ublk is a **pure Go** implementation of Linux ublk (userspace block device).

**Works (single- AND multi-queue):**
- Device lifecycle: ADD_DEV, SET_PARAMS, START_DEV, STOP_DEV, DEL_DEV
- Block I/O: Read, Write, Flush, Discard, Write-Zeroes. Read/Write are verified byte-exact.
  The kernel only delivers Flush when the device advertises a volatile write cache
  (`VolatileCache`, now the fail-safe default — see #13), Discard when the backend implements
  `DiscardBackend`, and Write-Zeroes when it implements `WriteZeroesBackend`. Before 2026-07-25
  none of the three was reachable, because SET_PARAMS sent no attrs and no discard block.
- **Logical block sizes 512 through PAGE_SIZE**, including 4Kn (#10). Sectors are counted in
  512-byte units throughout, as the UAPI requires; params outside the kernel's rules are
  rejected at Create instead of producing a device that reports the wrong capacity.
- **Multi-queue (≥2) now correct** on arm64: the descriptor-mmap-offset bug that caused
  data corruption and D-state hangs is fixed (Critical Bugs #1/#2). Verified Q=1/2/4/8,
  O_DIRECT, concurrent read-after-write — 0 hangs / 0 mismatches across many runs.
- Failed startup and killed daemons no longer wedge the host: startup tears down cleanly
  (#7) and `ublk-mem --del=all` reaps zombie devices (now public API: `ublk.ListDevices`,
  `ublk.DeleteDevice`).
- **Two examples, both on the public API only** (2026-07-26): `ublk-mem` (RAM, `--zip` for
  flate-compressed 64KB chunks — 128MB of compressible data in ~1MB, byte-exact) and
  `ublk-loop` (exports a file like losetup: sparse, punch-hole discard, and both durability
  modes — buffered + volatile cache, or `-sync` O_DSYNC + write-through). `make vm-loop-e2e`
  covers offset-in-file mapping across teardown, space reclaim, cache attrs and read-only:
  14/14 on arm64 6.17.0-41.
- **Graceful stop of a BUSY device now works** (#8): STOP_DEV runs while the ioLoops still
  drain in-flight I/O, and the control-plane completion wait is bounded. 50+ teardown-under-load
  cycles (Q=1/4/8, O_DIRECT fio, SIGINT mid-load) exit in ~0.13s with 0 leaks / 0 D-state hangs.

**x86_64 CONFIRMED (2026-07-24)** on a c6i.xlarge spot box, kernel `6.17.0-1020-aws`:
integrity sweep 24/24 byte-exact, 150/150 teardown-under-load cycles, 20/20 zero-I/O graceful
stops — 0 hangs, 0 leaks, refcount→0. Same binary on the same box booted into `6.17.0-1019-aws`
oopses instantly at ADD_DEV (see host-kernel caveats), which is the clean A/B proving the
multi-queue and teardown fixes are correct on x86 and that the remaining failure is the kernel's.

**Still unverified / open (see roadmap):**
- Crash / power-fail consistency: untested (matters a lot for BCDR).
- Host-reboot-under-load: the one teardown scenario never exercised — a systemd shutdown storm
  (SIGTERM to everything, filesystems unmounting) while a ublk device is serving I/O. This is the
  path the single real x86 oops came from, and it is a genuine production event (host reboots
  mid-backup), so it deserves its own test.

**Host kernel caveats (checked 2026-07-24) — these are KERNEL bugs, not go-ublk bugs:**
- **ADD_DEV NULL-deref on Ubuntu 6.17.0-{~29..40}:** their NUMA backport `529d4d632788` landed
  without its prerequisite `011af85ccd87`, so `ublk_init_queues()` runs before the tag set exists
  and derefs a NULL `mq_map`. Unconditional — any ublk server oopses the host on first device add.
  FIXED in `linux-aws-6.17` 6.17.0-1020 (already in noble-updates) and `linux-hwe-6.17` 6.17.0-41
  (noble-proposed, ready-for-promote); -35/-38/-40 generic are still broken. Confirmed live on
  x86 2026-07-24: on `6.17.0-1019-aws` (ublk_drv srcversion 6A00163FD3030280266148D) a plain
  `ublk-mem --queues=1 --depth=1` gives `BUG: kernel NULL pointer dereference, address: 0` at
  `ublk_init_queues+0x4e` in an `iou-wrk` worker via `ublk_ctrl_add_dev`, and ADD_DEV never
  returns; on `6.17.0-1020-aws` (srcversion 8F3FCC0225E19BF890B533B) the same binary is clean.
  Note `ublk_drv` ships in `linux-modules-extra-*-aws`, not the base AWS kernel image. Ubuntu's 6.18 line
  currently repeats the same omission — check before trusting a future 6.18 HWE.
- **Teardown double-completion (`io_req_uring_cleanup` NULL-deref):** one real oops on x86
  6.17.0-14, and the fixes are in kernel.org stable 7.1.y but in no Ubuntu 6.17/6.18 build.
  Severity is UNKNOWN, not "reliable": the arm64 "reproduces within ≤120 cycles" claim was a
  harness bug — `daemon_pid()` picked the "1" out of "USR1" and SIGINT'd pid 1 (systemd), i.e.
  the churn was rebooting the VM and blaming a traceless kernel panic. Fixed in
  `scripts/vm-churn.sh` + `scripts/vm-verify.sh`, which now also refuse to signal pid ≤ 1.
  x86 note: 150 churn cycles + 24 sweep combos on `6.17.0-1020-aws` — which carries NONE of the
  three upstream ublk teardown fixes — produced 0 incidents, so normal graceful stop does not
  appear to trigger it on x86 either.
- **Validated on 6.17.0-41-generic (arm64) with the fixed harness, 2026-07-24:** integrity sweep
  24/24 combos byte-exact (Q=1/2/4/8 x depth=1/64/128 x buffered/O_DIRECT), 150/150
  teardown-under-load cycles and 20/20 zero-I/O graceful stops clean — 0 hangs, 0 leaks,
  refcount→0, no reboot.

Honest O_DIRECT perf is now measured (see Phase 5): ~1.37M IOPS 4K randread / 816k randwrite
(RAM backend, Q=4) — the old "~100k IOPS" figures were buffered and are superseded.

**Minimum kernel:** 6.8+ (IOCTL encoding required). Fixes verified on arm64, kernel 6.17.

---

## Critical Bugs (found 2026-06-30)

1. **[FIXED — commit 9eb3b17] Multi-queue completion loss → unkillable I/O hang.**
   Root cause was NOT dropped io_uring completions: `mmapQueues` computed the per-queue
   descriptor mmap offset as `queueID * round_up(queue_depth*24, PAGE)`, but the kernel
   (`ublk_ch_mmap`) derives the queue as `phys_off / round_up(UBLK_MAX_QUEUE_DEPTH*24, PAGE)`
   — a FIXED stride keyed on `UBLK_MAX_QUEUE_DEPTH` (4096), independent of the actual depth.
   For any realistic depth that offset floored to q_id 0, so every queue ≥1 aliased queue 0's
   descriptor buffer → wrong descriptors → state-machine violations that killed the queue's
   ioLoop → its requests wedged in D-state. Rate scaled as (N−1)/N (2q ~50%, 4q ~70%),
   exactly the fraction of I/O landing on a non-zero queue. Fixed by using the kernel's fixed
   stride. Verified: Q=1/2/4/8, O_DIRECT, concurrent, on arm64 — 0 hangs across many runs.

2. **[FIXED — commit 9eb3b17] Intermittent data corruption, multi-queue.**
   Same root cause as #1 (descriptor aliasing): queues ≥1 read queue 0's descriptors and
   did I/O to the wrong offsets. Fixed by the same change. Verified byte-exact O_DIRECT
   read-after-write across all queues (taskset-pinned) — 0 mismatches across many runs.

3. **[FIXED — commit b3846fe] DEL_DEV teardown hang.**
   Four stacked bugs: `runner.Close()` never joined the ioLoop goroutine; `Device.Close()`
   tore down queue rings *before* STOP_DEV; the original `/dev/ublkcN` fd (dup'd to each queue)
   was never closed; the io_uring fixed-file registration wasn't unregistered before close.
   DEL_DEV blocked forever, masked by the example's 1s watchdog + `os.Exit(0)`.

4. **[OPEN — low, debug-only] Verbose logging stalls I/O under load.**
   `logging.Logger` holds one mutex across a blocking `write()`; `-v` + multi-queue starves
   the I/O goroutines. But `log()` filters by level BEFORE taking the lock, and the data-plane
   hot loop (`WaitForCompletion`→`handleCompletion`→…→`FlushSubmissions`) makes no log calls,
   so at the prod default (INFO) there is zero hot-path logging overhead. Only bites under `-v`.
   The wrapped stdlib `log.Logger` is already concurrent-safe, so the wrapper mutex is largely
   redundant. Real fix (async/buffered logging) is Phase 5 polish, not a prod blocker.

5. **[OPEN — minor] Memory fences use one shared global** (`barrierDummy`, `atomic.AddInt64(...,0)`)
   hammered by every queue — correct (a `LOCK`/`LDADDAL` RMW is a full hardware fence regardless
   of the address it touches), just a contention point. The multi-queue data path is now proven
   correct WITH this barrier, so changing it is pure perf with real memory-ordering risk on arm64.
   Leave unless profiling shows it matters. (Open question for review — see roadmap.)

6. **[FIXED — commit 5f23336] Requested queue count not reconciled with kernel.**
   The kernel clamps `nr_hw_queues` at ADD_DEV (notably to the online CPU count). We created a
   runner per *requested* queue, so asking for more queues than CPUs made the extra queues'
   descriptor mmap fail with EINVAL and wedged startup. Now we read back the kernel's actual
   `nr_hw_queues` after ADD_DEV and create exactly that many runners (`--queues=8` on a 4-CPU
   box → 4 queues, starts cleanly).

7. **[FIXED — commits 55303ca, 5f23336] Failed multi-queue startup wedged the process.**
   The creation-failure cleanup closed runners while their ioLoops were parked in an UNBOUNDED
   `io_uring_enter` (nothing woke them, since the device was never STARTed so STOP_DEV is a
   no-op), then called DEL_DEV while the original `/dev/ublkcN` fd was still open (DEL blocks on
   the refcount) → permanent hang requiring a reboot. Fixed two ways: (a) the data-plane wait is
   now BOUNDED (`IORING_ENTER_EXT_ARG`, 100ms) so ioLoops observe context cancellation and exit
   without kernel help; (b) the error path releases every char-device fd (dups + original) before
   DEL_DEV. A mid-startup failure now tears down in ~0.1s with no zombie device, no reboot.

8. **[FIXED — commit 8ae839c] Control-plane completion wait + LIVE-teardown ordering.**
   Two distinct defects shared one symptom (STOP-under-load hang / START flakiness):
   - **Control-plane wait.** `submitAndWait` did one `submitAndWaitRing(1,1)` then a fixed 5×10µs
     poll. `UBLK_F_URING_CMD_COMP_IN_TASK` defers the completion to task_work and Go's async
     preemption (SIGURG) interrupts the wait with EINTR, so the single wait + short poll could
     miss a completion it WOULD receive → START_DEV intermittently failed ("no completions
     available after retries"), and a stuck STOP blocked forever. Fixed with a bounded blocking
     RE-WAIT loop (reliable submit, then re-enter `io_uring_enter` across EINTR/ETIME until the
     CQE appears, 10s overall cap) plus a stale-completion drain — mirrors the data-plane
     `WaitForCompletion`. This alone made START reliable and converted the STOP hang from an
     unkillable D-state into a clean bounded timeout.
   - **Teardown ordering (the real STOP hang).** The kernel's `del_gendisk` drains in-flight I/O
     before STOP_DEV returns, and only the running ioLoops can complete that I/O
     (`COMMIT_AND_FETCH`). But `Close()`/`Stop()` (and the example's SIGINT handler) cancelled the
     ioLoops FIRST, stranding the in-flight requests so the drain never finished → STOP_DEV
     blocked. Reordered the LIVE path to **STOP_DEV → cancel ioLoops → join → DEL_DEV**.
     `teardownPartial` (creation-failure, device not yet LIVE) correctly keeps cancel-first (#7).
   Verified on arm64: START reliable; 50+ teardown-under-load cycles (Q=1/4/8, O_DIRECT fio,
   SIGINT mid-load) all exit in ~0.13s with 0 leaks / 0 D-state; 128MB O_DIRECT read-after-write
   byte-exact; idle stop 0.11s; unit tests green.

9. **[FIXED — 2026-07-26] Any discard over 1MB panicked the whole daemon.**
   `handleIORequest` acquired a data buffer sized from the request length for
   *every* op. A DISCARD's length is a range to deallocate, not bytes to move, so
   `blkdiscard` over a few MB — or an `fstrim` on a mounted filesystem — called
   `GetBuffer(32MB)`, which resliced a 1MB pooled buffer past its capacity:
   `panic: slice bounds out of range [:33554432] with capacity 1048576`. The
   process died, every device it served went to EIO, and the kernel logged
   `I/O error, dev ublkb0, sector 0 op 0x3:(DISCARD)`. Reachable by anything with
   write access to the device, and it existed from the moment discard was first
   advertised (commit ff53e7a) — the earlier discard test only used a small range.
   Fixed two ways: only READ/WRITE acquire a buffer, and `GetBuffer` now allocates
   exactly rather than reslicing a pooled buffer past its capacity. Found by the
   `ublk-loop` e2e, which discards 64MB.

10. **[FIXED — 2026-07-26] `LogicalBlockSize` other than 512 silently corrupted data.**
    The ublk UAPI counts sectors in 512-byte units everywhere
    (`ublk_param_basic.dev_sectors` and `max_sectors`,
    `ublksrv_io_desc.start_sector`/`nr_sectors`), but the control plane derived
    `DevSectors` from `LogicalBlockSize` and the data plane multiplied
    `StartSector` by it, while `submitCommitAndFetch` hardcoded `NrSectors << 9`
    — the internal disagreement that gave it away. A 4096-byte build of
    `ublk-loop` reported a 256MB device as 32MB and did I/O at 8x the intended
    offset (`FULL-READBACK MISMATCH at dev-offset 25096192: got 0x00 want 0x95`);
    a zero value divided by zero. Fixed by counting sectors in `uapi.SectorSize`
    everywhere and deleting the runner's `blockSize` field entirely so the
    conflation cannot come back. `validateParams` now enforces the kernel's real
    rule (power of two, 512..PAGE_SIZE) plus MaxIOSize and backend-size
    alignment. Verified on arm64: a 4Kn device reports 268435456 bytes and the
    shadow oracle is CLEAN at Q=4/depth=64/O_DIRECT, and the 512-byte sweep is
    still 24/24.

11. **[FIXED — 2026-07-26] Four of five optional backend interfaces were never called.**
    `WriteZeroesBackend`, `SyncBackend`, `StatBackend` and `ResizeBackend` were
    public, documented, and asserted by tests, but nothing in the I/O loop
    consulted them, so a user could implement one and never have it fire.
    Resolved by wiring the one with a kernel operation behind it and dropping the
    three without: `UBLK_IO_OP_WRITE_ZEROES` now dispatches to
    `WriteZeroesBackend`, and the write-zeroes limit is advertised only when the
    backend implements it (discard and write-zeroes share one param block but are
    independent capabilities). `SyncBackend`/`StatBackend`/`ResizeBackend` are
    removed: ublk has no range-sync op, statistics are the caller's own business
    since they hold the backend, and resize is a feature rather than a wiring
    task. Verified: `write_zeroes_max_bytes` is advertised, `blkdiscard -z`
    succeeds, the range reads back all zeros, and the backing file returns to
    sparse (the punch-hole path).

12. **[FIXED — 2026-07-26] The examples could not be compiled by a library user.**
    `examples/ublk-mem` imported `internal/ctrl` (to reap leaked devices) and
    `internal/logging` (for `-v`), neither reachable from outside the module — so
    the flagship example was not a valid demonstration of the public API.
    Reaping is now public (`ublk.ListDevices`, `ublk.DeleteDevice`), and
    `Options.Debug` plus an adapter that routes the internal logger's output
    through `Options.Logger` replaces the internal-logging import — so a caller's
    own logger now receives the control-plane and queue diagnostics instead of
    only the handful of messages `backend.go` emits directly. Neither example
    imports `internal/` any more.

13. **[DECIDED — 2026-07-26] Durability default flipped to advertising a volatile write cache.**
    `DefaultParams` now sets `VolatileCache: true`. The library cannot know
    whether a backend's completed write is durable, and the two mistakes are not
    symmetric: claiming a cache that does not exist costs a no-op flush
    round-trip, while hiding one that does exist loses data on power failure with
    no error anywhere. A backend that makes every write durable before returning
    should set it false, which also stops the kernel sending flushes it does not
    need — `ublk-mem` does exactly that (RAM has no cache below it) and
    `ublk-loop` sets it from its `-sync` flag. Per-IO FUA remains unadvertised
    until `UBLK_IO_F_FUA` is honored.

14. **[FIXED — 2026-07-26] `vm-simple-e2e.sh` killed unrelated processes, including its own caller.**
    `cleanup_force()` ran `ps aux | grep -E "(ublk-mem|timeout)"` and SIGKILLed
    every match, so any process whose command line merely mentioned those strings
    died with it — including the ssh session or shell running the script, which
    then looked exactly like the script hanging after "device stopped
    successfully". (An earlier note in this file claiming that hang was a real
    pre-existing defect was wrong; the script exits 0.) Now matched by exact
    process name via `pgrep -x`, D-state entries skipped, and the `killall -9
    ublk-mem timeout dd` on the I/O-hang path narrowed the same way.
    `vm-fuzz.sh`'s unanchored `pkill` patterns were tightened too.

---

## Production Roadmap (BCDR use)

Ordering principle for a backup/DR product: **correctness and recoverability first,
performance last.** Nothing holding customer data ships before Phase 2 closes.

### Phase 0 — Honesty + teardown — DONE
- [x] Fix DEL_DEV teardown hang (commit b3846fe)
- [x] Correct overclaimed "stable / ~100k IOPS / 10x stress" status in TODO.md + CLAUDE.md

### Phase 1 — Data-path correctness — mostly DONE (multi-queue now trustworthy on arm64)
- [x] Root-cause the multi-queue hang (bug #1): it was the per-queue descriptor mmap
      offset stride, NOT dropped io_uring completions (commit 9eb3b17)
- [x] **Core decision gate — RESOLVED: keep pure-Go.** The bug was a shallow one-line
      offset mistake (wrong stride constant), not a structural flaw in the hand-rolled
      io_uring core. The SQ/CQ accounting, barriers, and state machine were correct all
      along. No reason to take on cgo+liburing; pure-Go stays.
- [x] Fix multi-queue completion loss (#1) and corruption (#2) — one root cause (9eb3b17)
- [x] Fix queue-count-vs-CPU startup wedge and make failed startup tear down cleanly
      (commits 5f23336, 55303ca)
- [x] Verify on arm64: Q=1/2/4/8, O_DIRECT, concurrent read-after-write + burst =
      0 hangs / 0 mismatches across many runs
- [x] **Verify on x86_64** (prod arch) — done 2026-07-24 on `6.17.0-1020-aws`: sweep 24/24,
      churn 150/150, zero-I/O stops 20/20, plus the `-1019` A/B pinning the remaining
      failure on the kernel. See the x86_64 CONFIRMED block at the top.
- [ ] Multi-queue is now the reliable default (≤ CPU count); single-queue no longer required

### Phase 2 — Data-integrity discipline — BCDR-critical
- [ ] End-to-end checksums / verify-on-read at the application layer (never trust the block path)
- [ ] Correct + tested FLUSH / FUA / fsync durability semantics
- [ ] Crash / power-fail consistency harness: kill the daemon mid-write, verify no
      torn / lost / silently-wrong data on recovery

### Phase 3 — Resilience & recovery
- [ ] `UBLK_F_USER_RECOVERY` — recover a device across a daemon restart (not implemented)
- [ ] Daemon supervision + host fencing: a wedged daemon must not require a host reboot;
      auto-detect and clean up stuck devices (D-state currently needs a reboot)
- [ ] Graceful degradation on daemon crash (no permanent D-state on a customer host)

### Phase 4 — Testing infrastructure (close the gap that shipped "stable")
- [ ] Fault-injection suite: multi-queue, O_DIRECT, concurrent, long-running, under GC/memory pressure
- [ ] CI that runs the real failure modes (not buffered happy-path dd/fio), on x86_64 + arm64
- [ ] Fuzzing for UAPI marshal/unmarshal; invariant assertions around `unsafe`
- [ ] Graceful handling of kernel-version differences

### Phase 5 — Performance (only after correctness is proven)
- [x] Re-benchmark honestly (O_DIRECT + multi-queue). fio `--direct=1`, RAM backend, on the
      arm64 M4 Lima VM (4 vCPU) — this is the ublk-path ceiling, NOT end-to-end (a real
      file/network backend will be backend-bound):
      | Workload (4K, numjobs=4, QD=32) | Q=1 | Q=4 |
      |---|---|---|
      | randread  | 801k IOPS | **1.37M IOPS** (93µs avg) |
      | randwrite | 680k IOPS | **816k IOPS** (155µs avg) |
      Multi-queue scales ~1.7x read / ~1.2x write over single-queue here; more CPUs + real
      backends should widen that. Point: the ublk path is not the bottleneck. The old
      "~100k IOPS" figures were buffered and/or different hardware — superseded.
- [ ] Fix verbose-logging mutex stall (#4) and shared-global barrier contention (#5)
- [ ] Registered buffers / zero-copy; io_uring SQPOLL; hot-path profiling
- [ ] Async backend interface; NBD backend (a file backend now exists as `examples/ublk-loop`)
- [ ] NEED_GET_DATA path (older kernels); Discard/TRIM verification; Flush/FUA batching

---

## Testing Commands

```bash
# Unit tests (local)
make test-unit

# VM tests (requires VM setup)
make vm-reset          # Reset VM state
make vm-simple-e2e     # Basic I/O test
make vm-e2e            # Full test suite
make vm-verify         # Shadow-oracle integrity sweep (queues x depth x direct)
make vm-loop-e2e       # File-backend + compressed-backend behavior
make vm-benchmark      # Performance benchmark
make vm-stress         # 10x alternating e2e + benchmark
```

---

## Known Issues

### Slow Device Initialization
**Symptom:** Device takes `queue_depth * 250ms` to initialize (9+ seconds for QD=32)

**Cause:** Each FETCH_REQ takes ~250ms to complete during setup.

**Status:** Low priority - doesn't affect operation once device is running.

---

## Completed Work (Summary)

### Phase 0-2: Foundation (Complete)
- Code cleanup: Removed dead code, fixed bugs, improved constants
- API polish: Structured errors with `errors.Is()`/`errors.As()`, staged device lifecycle
- Observability: Metrics interface with latency histograms (P50/P99/P999)
- Testing infrastructure: Unit tests, VM testing, race detector support

### Phase 3: Performance (Mostly Complete)
- Multi-queue with sharded memory backend (64KB shards)
- Buffer pool for large allocations (700x faster than make)
- Batched io_uring submissions (5-10x improvement for parallel workloads)
- Pre-allocated structs on hot path

**Performance results (2025-11-26) — SUPERSEDED (buffered; see Phase 5 for honest O_DIRECT):**
| Workload | go-ublk | Loop (RAM) | % of Loop |
|----------|---------|------------|-----------|
| 4K Read (1 job, QD=64) | 85.5k IOPS | 220k IOPS | 39% |
| 4K Read (4 jobs, QD=64) | 98.9k IOPS | 116k IOPS | 85% |
| 4K Write (4 jobs, QD=64) | 90.1k IOPS | 98.6k IOPS | 91% |

---

## Historical Context

Major bugs fixed during development:
1. **START_DEV hang** - Submit FETCH_REQs before START_DEV
2. **IOCTL encoding** - Modern kernels require IOCTL-encoded commands
3. **SQE128 layout** - cmd area starts at byte 48, 80 bytes total
4. **Logging deadlock** - Thread-locked goroutines can't block on I/O
5. **EINTR handling** - Retry io_uring_enter on signal interruption
6. **Memory barriers** - Sfence before SQ tail update for SQE visibility
