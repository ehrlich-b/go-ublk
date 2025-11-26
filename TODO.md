# TODO.md - Production Roadmap

**See also:** [review_todo.md](review_todo.md) - Comprehensive micro-level code review and cleanup tasks

---

## ✅ PERFORMANCE TARGET ACHIEVED

**Target:** ~50% of loop device ✅ **EXCEEDED**

**Current Results (2025-11-26 with 4 queues, depth=64):**
| Workload | go-ublk | Loop (RAM) | % of Loop | Status |
|----------|---------|------------|-----------|--------|
| 4K Random Read (1 job, QD=64) | 146k IOPS | 209k IOPS | **70%** | ✅ Target exceeded |
| 4K Random Read (4 jobs, QD=64) | **365k IOPS** | 122k IOPS | **300%** | ✅ 3x faster! |

**Multi-queue scaling is excellent:** 4 jobs = 2.5x performance of 1 job, while loop device degrades.

**Optimizations completed:**
- [x] Pre-allocated SQE structs in io_uring (avoid 128-byte allocation per I/O)
- [x] Pre-allocated result pool for CQE completions
- [x] Pre-allocated UblksrvIOCmd structs per tag
- [x] Moved hot path logging inside logger nil checks
- [x] Moved time.Now() behind observer nil check
- [x] Increased default queue depth to 64 (configurable with --depth flag)
- [x] Sharded memory backend (64KB shards for parallel access)
- [x] Multi-queue support (4 queues by default)

**Future optimization opportunities (not needed for 50% target):**
- Registered buffers for zero-copy I/O
- io_uring SQPOLL for reduced syscall overhead
- Buffer pool for >64KB allocations

---

## Current Status: Stable Working Prototype

go-ublk is a **pure Go** implementation of Linux ublk (userspace block device).

**What works:**
- Device lifecycle: ADD_DEV, SET_PARAMS, START_DEV, STOP_DEV, DEL_DEV
- Block device: /dev/ublkb0 appears and accepts I/O
- Data integrity: Verified via MD5 across all I/O patterns
- Multi-queue: 4 queues with sharded memory backend
- Stability: Passes stress tests (10x alternating e2e + benchmark)

---

## Phase 0: Code Cleanup ✅ COMPLETED

**See [review_todo.md](review_todo.md) for detailed cleanup status.**

### 0.1 Delete Dead Code ✅ DONE
- [x] Deleted unused `giouring` build tag files
- [x] Deleted unused logging domain methods (~100 lines)
- [x] Deleted deprecated `StopAndDelete()` function - use `device.Close()` instead
- [x] Removed hard-coded `useIoctl` field (always true, now always uses ioctl encoding)
- [x] Fixed stub fallback to return errors instead of fake success

### 0.2 Fix Bugs ✅ DONE
- [x] Fixed `directUnmarshal` in marshal.go (already using reflect correctly)
- [x] Fixed `waitLive` in backend.go (returns error on timeout)
- [x] Fixed `charFd` initialization bug (now correctly initialized to -1)
- [x] Fixed error string formatting (now joins all context parts with commas)

### 0.3 Code Quality Improvements ✅ DONE
- [x] Added `const NoQueue = -1` for error queue sentinel value
- [x] Added `const CharDeviceOpenRetries = 50` for device open retry count
- [x] Added runtime check for `numLatencyBuckets` consistency with `LatencyBuckets`
- [x] Simplified interfaces to Backend, DiscardBackend, Logger
- [x] DeviceParams duplication intentionally kept (blocked by circular imports)

### 0.4 Environment Variable Hacks ✅ DONE
- [x] Removed `UBLK_DEVINFO_LEN` env var hack
- [x] `UBLK_CTRL_ENC` only in Makefile (not in Go code)

### 0.5 Documentation ✅ DONE
- [x] Documented all magic timing constants with WHY comments
- [x] Hot-path logging moved to debug level
- [x] All unit tests passing

---

## Phase 1: Stabilization

### 1.1 io_uring Architecture
**Decision: Keep io_uring internal** (internal/uring)

Rationale:
- Pure Go implementation using `golang.org/x/sys/unix` syscalls
- Memory barriers via `atomic.AddInt64` (LOCK XADD on x86-64)
- Tightly coupled to ublk's URING_CMD requirements
- Interface types are ublk-specific (UblksrvCtrlCmd, UblksrvIOCmd)

The code is well-abstracted behind the `Ring` interface.

### 1.2 Testing Infrastructure ✅ DONE
- [x] Add `make test-unit` to CI/pre-commit (GitHub Actions in `.github/workflows/ci.yml`)
- [x] Add race detector to VM tests: `make vm-e2e-racedetect` or `RACE=1 make vm-e2e`
- [x] Document VM testing setup for contributors (`docs/VM_TESTING.md`)

---

## Phase 2: API Polish

### 2.1 Structured Error Handling ✅ DONE
- [x] Create `Error` type with errno mapping
- [x] Simplify to single error type (removed legacy `UblkError` string type)
- [x] Support `errors.Is()` and `errors.As()` via sentinel errors

### 2.2 Device Lifecycle API ✅ DONE
Implemented staged lifecycle for better control:
```go
device, err := ublk.Create(params, options)  // validate, allocate
err = device.Start(ctx)                       // start I/O processing
device.Stop()                                 // stop I/O, keep device
device.Close()                                // full cleanup
```

- [x] Implement `Create()` function for device creation without starting I/O
- [x] Implement `Start()` method to begin I/O processing
- [x] Implement `Stop()` method to stop I/O but keep device registered
- [x] Implement `Close()` method for full cleanup
- [x] Deprecate `StopAndDelete()` in favor of `Close()`
- [x] Delete deprecated `StopAndDelete()` function - all code now uses `device.Close()`
- [x] Add `DeviceStateClosed` state for fully closed devices
- [x] Add unit tests for lifecycle state machine

### 2.3 Observability ✅ DONE
- [x] Wire up existing Metrics to I/O loop via Observer interface
- [x] Expose metrics interface (Observer pattern, compatible with custom backends)
- [x] Add latency histogram with P50, P99, P999 percentiles

---

## Phase 3: Performance

### 3.1 Multi-Queue Support ✅ DONE
- [x] Add NumQueues parameter (auto-detects CPU count when 0)
- [x] Per-queue goroutine with CPU affinity support
- [x] `--queues` CLI flag for ublk-mem
- [x] Multi-queue device initialization (all queues start, START_DEV completes)
- [x] Multi-queue I/O handling (reads and writes working)
- [x] Root cause analysis: backend mutex contention (2025-11-26)
- [x] Fix: Implemented sharded memory backend (64KB shards)

**Resolution:** Memory backend now uses sharded locking (64KB per shard).
Concurrent I/O to different memory regions no longer contends on a single mutex.

**Performance improvement (multi-queue with sharded backend vs old single-mutex):**
- Write: 53k IOPS (+37% from 39k)
- Read: 66k IOPS (+17% from 57k)

**Remaining gap to 50% target (~100k IOPS) likely due to:**
- Go runtime overhead (goroutine scheduling, GC pauses)
- io_uring submission latency in userspace
- Memory copy overhead (kernel ↔ userspace)
- Context switches between kernel and userspace

### 3.2 Memory Optimization
- [ ] Buffer pool to eliminate >64KB dynamic allocation on hot path
- [ ] Consider registered buffers for zero-copy
- [ ] Profile and optimize hot paths

### 3.3 Backend Improvements
- [ ] Async backend interface for non-blocking I/O
- [ ] File backend (backed by real file)
- [ ] NBD backend (network block device passthrough)

---

## Phase 4: Production Hardening

### 4.1 Safety & Robustness
- [ ] Fuzzing for UAPI marshal/unmarshal
- [ ] Invariant assertions around unsafe operations
- [ ] Graceful handling of kernel version differences
- [ ] **Kernel compatibility testing matrix**
  - [ ] Define minimum supported kernel version (6.1? 6.6?)
  - [ ] Test on: Ubuntu 22.04 LTS (5.15), 24.04 LTS (6.8)
  - [ ] Test on: Fedora 39 (6.6), Fedora 40 (6.8)
  - [ ] Test on: Arch (latest stable), Debian stable
  - [ ] Document ublk feature availability by kernel version
  - [ ] CI matrix testing across kernel versions (GitHub Actions or similar)

### 4.2 Feature Completeness
- [ ] NEED_GET_DATA path for kernel compatibility
- [ ] Discard/TRIM support
- [ ] Flush/FUA batching

### 4.3 Documentation
- [ ] Architecture overview
- [ ] Backend implementation guide
- [ ] Performance tuning guide

---

## Known Issues

### Slow Device Initialization
**Symptom:** Device takes `queue_depth * 250ms` to initialize (9+ seconds for QD=32)

**Cause:** Each FETCH_REQ takes ~250ms to complete during setup.

**Workaround:** Currently sleep during device creation. Not a runtime issue.

**Status:** Low priority - doesn't affect operation once device is running.

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

## Historical Context

Major bugs fixed during development:
1. **START_DEV hang** - Submit FETCH_REQs before START_DEV
2. **IOCTL encoding** - Modern kernels require IOCTL-encoded commands
3. **SQE128 layout** - cmd area starts at byte 48, 80 bytes total
4. **Logging deadlock** - Thread-locked goroutines can't block on I/O
5. **EINTR handling** - Retry io_uring_enter on signal interruption
6. **Memory barriers** - Sfence before SQ tail update for SQE visibility

All core functionality now works reliably.
