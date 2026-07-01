# TODO.md - Production Roadmap

## Current Status: Prototype — approaching usable, not yet production-hardened

go-ublk is a **pure Go** implementation of Linux ublk (userspace block device).

**Works (single- AND multi-queue):**
- Device lifecycle: ADD_DEV, SET_PARAMS, START_DEV, STOP_DEV, DEL_DEV
- Block I/O: Read, Write, Flush, Discard
- **Multi-queue (≥2) now correct** on arm64: the descriptor-mmap-offset bug that caused
  data corruption and D-state hangs is fixed (Critical Bugs #1/#2). Verified Q=1/2/4/8,
  O_DIRECT, concurrent read-after-write — 0 hangs / 0 mismatches across many runs.
- Failed startup and killed daemons no longer wedge the host: startup tears down cleanly
  (#7) and `ublk-mem --del=all` reaps zombie devices.

**Still unverified / open (see roadmap):**
- **Graceful stop of a BUSY device hangs** — control-plane STOP_DEV completion is unreliable
  under load (Critical Bug #8). Idle stop is fine; stopping mid-I/O wedges. Top priority next.
- x86_64 confirmation of the multi-queue fix (validated on arm64 so far; fix is arch-independent).
- Crash / power-fail consistency: untested (matters a lot for BCDR).

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

8. **[OPEN — CRITICAL under load] Control-plane completion wait is unreliable.**
   The hand-rolled control-plane `submitAndWait` (`submitAndWaitRing(1,1)` then a fixed 5×10µs
   `processCompletion` poll) does not reliably observe its command's completion. Two observed
   failure modes, same root:
   - **START_DEV**: intermittently returns "no completions available after retries" → device
     creation fails (cleanly, thanks to #7). Low rate at idle.
   - **STOP_DEV**: under active I/O load, `device.Close()` hangs in `StopDevice` →
     `submitAndWaitRing(1,1)` (io_uring_enter blocks forever); the graceful-shutdown watchdog
     eventually force-exits, leaking the device and leaving the issuing app wedged in D-state
     (`blk_mq_get_tag`). **Graceful stop of a busy device is currently broken.**
   Pre-existing: reproduced identically on the pre-bounded-wait binary (4b45061), so NOT caused
   by this session's data-plane changes. Likely `UBLK_F_URING_CMD_COMP_IN_TASK` defers the
   completion to task_work such that `io_uring_enter(min_complete=1)` doesn't observe it under
   load. Proposed fix: make `submitAndWait` a bounded blocking RE-WAIT loop
   (`io_uring_enter(0,1)` + EINTR/ETIME retry, with a timeout) until the completion appears —
   mirrors the now-robust data-plane `WaitForCompletion`. NOT applied: touches the shared control
   path (every ADD/SET/START/STOP/DEL), so wants review before landing — but there is now a
   RELIABLE repro (teardown under fio load), so it is verifiable. **Top priority for next session.**

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
- [ ] **Verify on x86_64** (prod arch) — repeat the same suite on an AWS spot box; the fix
      is arch-independent by construction but confirm (weak-ordering bug classes differ)
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
- [ ] Async backend interface; File backend; NBD backend
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
