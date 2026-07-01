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

## Remaining Work

### Performance Optimization

**Memory:**
- [ ] Registered buffers for zero-copy I/O
- [ ] io_uring SQPOLL for kernel-side polling
- [ ] Profile and optimize remaining hot paths

**Backends:**
- [ ] Async backend interface for non-blocking I/O
- [ ] File backend (backed by real file)
- [ ] NBD backend (network block device passthrough)

### Production Hardening

**Safety:**
- [ ] Fuzzing for UAPI marshal/unmarshal
- [ ] Invariant assertions around unsafe operations
- [ ] Graceful handling of kernel version differences

**Feature Completeness:**
- [ ] NEED_GET_DATA path for older kernel compatibility
- [ ] Discard/TRIM support verification
- [ ] Flush/FUA batching

**Documentation:**
- [ ] Architecture overview
- [ ] Backend implementation guide
- [ ] Performance tuning guide

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
