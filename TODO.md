# TODO.md - Production Roadmap

## Current Status: Stable Working Prototype

go-ublk is a **pure Go** implementation of Linux ublk (userspace block device).

**What works:**
- Device lifecycle: ADD_DEV, SET_PARAMS, START_DEV, STOP_DEV, DEL_DEV
- Block device: /dev/ublkb0 appears and accepts I/O
- Data integrity: Verified via MD5 across all I/O patterns
- Performance: ~500k IOPS (4K random read/write)
- Stability: Passes stress tests (10x alternating e2e + benchmark)

**Performance baseline (2025-11-25):**
| Workload | IOPS | Throughput |
|----------|------|------------|
| 4K Random Write | 504k | ~2.0 GB/s |
| 4K Random Read | 482k | ~1.9 GB/s |
| 128K Sequential | 9.5k | ~1.2 GB/s |

---

## Phase 0: Code Cleanup (IMMEDIATE)

**See [docs/REVIEW.md](docs/REVIEW.md) for detailed file-by-file analysis.**

### 0.1 Delete Dead Code (~400-500 lines)
- [ ] Delete `internal/uring/iouring.go` (unused `giouring` build tag)
- [ ] Delete from runner.go: `NewStubRunner`, `NewWaitingRunner`, `stubLoop`
- [ ] Delete from runner.go: `waitAndStartDataPlane`, `initializeDataPlane`
- [ ] Delete from control.go: `StartDataPlane` (deprecated)
- [ ] Delete from control.go: `StartDeviceAsync` (unused)
- [ ] Delete from errors.go: unused constructors (`NewDeviceError`, `NewQueueError`, `NewErrorWithErrno`)
- [ ] Delete from logger.go: unused domain methods (`ControlStart`, `IOStart`, `RingSubmit`, etc.)

### 0.2 Fix Bugs
- [ ] Fix `directUnmarshal` in marshal.go (wrong pointer arithmetic)
- [ ] Fix `waitLive` in backend.go (always returns nil)
- [ ] Fix `device.queues` mismatch (stores 0, creates 1)

### 0.3 Consolidate Interfaces
- [ ] Remove `internal/interfaces` package (use public interfaces everywhere)
- [ ] Remove `Logger = interfaces.Logger` alias in interfaces.go
- [ ] Merge `ctrl.DeviceParams` with public `DeviceParams`

### 0.4 Environment Variable Hacks
- [ ] Remove `UBLK_DEVINFO_LEN` env var hack in control.go
- [ ] Remove `UBLK_CTRL_ENC` if still present

### 0.5 Documentation
- [ ] Document magic timing constants (why 100ms? why 500ms?)
- [ ] Move hot-path logging to debug level

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

### 1.2 Testing Infrastructure
- [ ] Add `make test-unit` to CI/pre-commit
- [ ] Add race detector to VM tests: `go test -race`
- [ ] Document VM testing setup for contributors

---

## Phase 2: API Polish

### 2.1 Structured Error Handling
- [x] Create `Error` type with errno mapping (exists but over-engineered)
- [ ] Simplify to single error type (remove legacy `UblkError`)
- [ ] Support `errors.Is()` and `errors.As()`

### 2.2 Device Lifecycle API
Current API is monolithic:
```go
device, err := ublk.CreateAndServe(ctx, params, options)
```

Consider staged lifecycle for better control:
```go
device, err := ublk.Create(params, options)  // validate, allocate
err = device.Start(ctx)                       // start I/O processing
device.Stop()                                 // stop I/O, keep device
device.Close()                                // full cleanup
```

### 2.3 Observability
- [ ] Wire up existing Metrics to I/O loop (currently allocated but not populated)
- [ ] Expose metrics interface (compatible with Prometheus)
- [ ] Add latency histogram (P50, P99, P999)

---

## Phase 3: Performance

### 3.1 Multi-Queue Support
Currently single queue limits scaling:
- [ ] Add NumQueues parameter
- [ ] Per-queue goroutine with CPU affinity
- [ ] Benchmark linear scaling

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
