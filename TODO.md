# TODO.md - Production Roadmap

## Current Status: Prototype — NOT production-ready

go-ublk is a **pure Go** implementation of Linux ublk (userspace block device).

**Works (single-queue):**
- Device lifecycle: ADD_DEV, SET_PARAMS, START_DEV, STOP_DEV, DEL_DEV
- Block I/O: Read, Write, Flush, Discard (single queue)

**Broken / unverified:**
- **Multi-queue data path loses I/O under concurrent load** (see Critical Bugs below).
  The "~100k IOPS / passes 10x stress" numbers were buffered benchmarks that mask it.
- Crash / power-fail consistency: untested (matters a lot for BCDR).

**Minimum kernel:** 6.8+ (IOCTL encoding required). Bugs below reproduced on arm64, kernel 6.17.

---

## Critical Bugs (found 2026-06-30)

1. **[OPEN — CRITICAL] Multi-queue completion loss → unkillable I/O hang.**
   With ≥2 queues under concurrent load, I/O completions are dropped: the block request
   never finishes and the issuing process wedges in **D-state (survives SIGKILL)**. Rate
   scales with queue count — 1q reliable, 2q ~50% (7/14), 4q ~70% (10/14). All queue
   goroutines stay alive (no deadlock/crash); the completion just never returns.
   Root area: hand-rolled io_uring submit/complete accounting in `internal/uring/minimal.go`.
   Exact dropped-completion line not yet pinned. **Workaround: `--queues=1`.** Disqualifying
   for prod until fixed. (`vm-simple-e2e.sh` already special-cases "dd in D state" — this is old.)

2. **[OPEN — CRITICAL] Intermittent data corruption, multi-queue.**
   Read-after-write occasionally returns wrong bytes (~1/10, buffered). Same root area as #1;
   page cache hides it in most buffered workloads.

3. **[FIXED — commit b3846fe] DEL_DEV teardown hang.**
   Four stacked bugs: `runner.Close()` never joined the ioLoop goroutine; `Device.Close()`
   tore down queue rings *before* STOP_DEV; the original `/dev/ublkcN` fd (dup'd to each queue)
   was never closed; the io_uring fixed-file registration wasn't unregistered before close.
   DEL_DEV blocked forever, masked by the example's 1s watchdog + `os.Exit(0)`.

4. **[OPEN] Verbose logging stalls I/O under load.**
   `logging.Logger` holds one mutex across a blocking `write()`; `-v` + multi-queue starves
   the I/O goroutines (regression of historical bug #4).

5. **[OPEN — minor] Memory fences use one shared global** (`barrierDummy`, `atomic.AddInt64(...,0)`)
   hammered by every queue — correct on arm64 but a needless contention point.

---

## Production Roadmap (BCDR use)

Ordering principle for a backup/DR product: **correctness and recoverability first,
performance last.** Nothing holding customer data ships before Phase 2 closes.

### Phase 0 — Honesty + teardown — DONE
- [x] Fix DEL_DEV teardown hang (commit b3846fe)
- [x] Correct overclaimed "stable / ~100k IOPS / 10x stress" status in TODO.md + CLAUDE.md

### Phase 1 — Data-path correctness — BLOCKER (nothing else matters until done)
- [ ] Pin the exact dropped-completion line for the multi-queue hang (bug #1) in
      `internal/uring/minimal.go` — add per-tag submit/complete counters, reproduce on VM
- [ ] **Core decision gate:** given whether the bug is a shallow accounting slip or
      structural, decide — harden the pure-Go io_uring core, or replace it with cgo+liburing.
      Default for prod BCDR: don't hand-own io_uring unless pure-Go is a hard requirement.
- [ ] Fix multi-queue completion loss (#1) and corruption (#2) — likely one root cause
- [ ] Verify: multi-queue + O_DIRECT + concurrent long-run = 0 hangs / 0 mismatches, on
      **both arm64 and x86_64**
- [ ] Interim: operate single-queue only (reliable today); scale with multiple
      single-queue devices, not multi-queue, until this phase closes

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
- [ ] Re-benchmark honestly (O_DIRECT + multi-queue) — current numbers are buffered
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

**Performance results (2025-11-26):**
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
