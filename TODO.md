# TODO.md - Production Roadmap

## Current Status: Stable Working Prototype

go-ublk is a pure Go implementation of Linux ublk (userspace block device).

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

## Phase 1: Stabilization (Current)

### 1.1 Code Cleanup
- [ ] Remove debug logging to /tmp/ublk-fatal-error.log from runner.go
- [ ] Remove stderr debug print from cmd/ublk-mem/main.go
- [ ] Clean up untracked debug scripts and notes
- [ ] Ensure all tests pass after cleanup

### 1.2 Architecture Decision: io_uring
**Decision: Keep io_uring internal** (internal/uring)

Rationale:
- Tightly coupled to ublk's URING_CMD requirements
- Uses Cgo for memory barriers (not pure Go)
- Interface types are ublk-specific (UblksrvCtrlCmd, UblksrvIOCmd)
- No external demand for standalone io_uring-go yet

The code is well-abstracted behind the `Ring` interface. If demand emerges for standalone io_uring-go, we can fork and generalize later.

### 1.3 Testing Infrastructure
- [ ] Add `make test-unit` to CI/pre-commit
- [ ] Add race detector to VM tests: `go test -race`
- [ ] Document VM testing setup for contributors

---

## Phase 2: API Polish

### 2.1 Structured Error Handling
- [ ] Create `UblkError` type with errno mapping
- [ ] Support `errors.Is()` and `errors.As()`
- [ ] Actionable error messages with recovery hints

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
- [ ] Add atomic counters: ops/sec, bytes read/written, errors
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
- [ ] Buffer pool to eliminate >64KB dynamic allocation
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
6. **Memory barriers** - Mfence after loading cqTail for CQE visibility

All core functionality now works reliably.
