# TODO.md - Current Status

## ✅ FIXED: Logging Deadlock in Thread-Locked Goroutines (2025-09-29)

**ROOT CAUSE**:
- Goroutines locked to OS threads (required for io_uring) CANNOT block on I/O
- log/slog writes synchronously to stderr, blocking on `syscall.Write()`
- Under concurrent load, multiple threads trying to log simultaneously caused deadlock
- Threads couldn't reschedule because they're locked to OS threads for io_uring

**THE FIX**:
- Replaced log/slog with zerolog + **non-blocking async writer**
- Async writer uses buffered channel (1000 messages) with dedicated goroutine
- **Critical: `select` with `default` case drops messages if buffer full**
- Logging calls NEVER block - either queue or drop, never wait for I/O

**THE PROOF**:
- WITHOUT fix (log/slog): 100% hang rate with `-v` flag under any concurrency
- WITH fix (zerolog + async): **1.6 GB/s @ QD=32 with `-v` flag enabled!**
- Tested 5x benchmark runs + 3x manual tests - 100% success rate

**KEY INSIGHT**:
When goroutines are locked to OS threads (`runtime.LockOSThread()`), they cannot block on I/O or mutexes without risking deadlock. Any logging in hot paths must be completely non-blocking.

**TESTING STATUS**:
- `make vm-simple-e2e`: ✅ PASS
- `make vm-e2e`: ✅ PASS
- `make vm-benchmark`: ✅ PASS - 400-500k IOPS, 1.6-2.0 GB/s
- `make vm-benchmark-race`: ✅ PASS - 5/5 runs succeeded
- All tests pass with `-v` verbose logging enabled!

## What Was Fixed to Get Here:

### 1. The START_DEV Hang Issue (SOLVED ✅)
- **Root cause**: Kernel waits for FETCH_REQs before completing START_DEV
- **Solution**: Submit FETCH_REQs before START_DEV, proper async handling
- **Result**: Device creation works reliably

### 2. The IOCTL Encoding Issue (SOLVED ✅)
- **Root cause**: Modern kernels require IOCTL-encoded commands
- **Solution**: Proper IOCTL encoding for all control and I/O commands
- **Result**: All kernel commands now accepted

### 3. The SQE Layout Issue (SOLVED ✅)
- **Root cause**: Incorrect SQE128 structure layout
- **Solution**: Fixed cmd area to start at byte 48 (80 bytes total)
- **Result**: Kernel properly receives URING_CMD payloads

### 4. The I/O Processing Issue (SOLVED ✅)
- **Root cause**: Complex state machine issues in queue handling
- **Solution**: Simplified I/O processing, proper COMMIT_AND_FETCH flow
- **Result**: Full read/write operations working with data integrity

### 5. Code Quality Issues (SOLVED ✅)
- **Root cause**: Debug cruft, inconsistent logging, AI-generated comments
- **Solution**: Professional cleanup, proper logging framework, clean comments
- **Result**: Production-ready codebase foundation

## Next Phase: Production Readiness

### 6. The Data Corruption Bug (SOLVED ✅)
- **Root cause**: Test logic bug - reference file not initialized before scattered writes
- **Solution**: Initialize both ublk device and reference file with zeros before testing
- **Result**: Perfect MD5 verification across all I/O patterns, comprehensive data integrity

## Recent Improvements (2025-09-25):

### Library Quality Improvements (COMPLETED ✅)
- **Restructured Public API**: Moved Backend interfaces to root package for clean API
- **Testing Support**: Created public MockBackend for easy unit testing
- **Device Inspection**: Added device state inspection methods (State(), IsRunning(), Info())
- **Constants Management**: Removed hardcoded values, centralized all constants
- **Code Cleanup**: Removed all debug prints and verbose comments
- **Professional Logging**: Cleaned up error messages and logging

### Graceful Shutdown (FIXED ✅)
- **Issue**: WaitForCompletion blocked indefinitely, preventing clean exit on SIGINT
- **Solution**: Cancel context first, then close queue runners, then STOP_DEV/DELETE_DEV
- **Result**: Process now exits cleanly on SIGINT/SIGTERM without timeout needed

## Next Phase: Production-Grade Library (Informed by Peer Review)

**STRATEGIC INSIGHT**: We're at an inflection point from "working prototype" to "production-grade library". The core functionality is solid, but we need deliberate API hardening, observability, and safety improvements.

### SHORT-TERM: API Stability & Foundation (1-2 weeks)
1. **Structured Error Handling**
   - Replace plain string errors with structured UblkError type
   - Add errno mapping and errors.Is/As support
   - Provide actionable error diagnostics for callers

2. **API Lifecycle Separation**
   - Replace monolithic CreateAndServe with staged lifecycle
   - `Create()` → `Start()` → `Stop()` pattern for better control
   - Enable validation, dry-run, deferred start scenarios

3. **Testing Foundation**
   - Add unit tests for queue state machine (success + error injection)
   - Add race detector CI job and staticcheck
   - Remove duplicate iouring.go.disabled file to prevent divergence

4. **Basic Observability**
   - Add atomic counters: ops/sec, bytes/sec, error counts
   - Provide metrics export interface (prepare for Prometheus)

### MEDIUM-TERM: Scalability & Production Features (2-5 weeks)
5. **Multi-Queue Scaling**
   - Currently single queue limits throughput ceiling
   - Add CPU affinity and NUMA awareness
   - Benchmark linear scaling characteristics

6. **Memory & Performance Optimization**
   - Buffer pool to eliminate >64KB dynamic allocation path
   - Reduce per-request syscall overhead
   - Add asynchronous backend operation interface

7. **Feature Completeness**
   - Implement NEED_GET_DATA path for kernel compatibility
   - Enhanced Discard/TRIM and Write Zeroes support
   - Robust Flush/FUA handling with batching

8. **Safety & Robustness**
   - Add invariant assertions around unsafe memory operations
   - Implement "paranoid mode" with strict validation
   - Fuzzing for UAPI marshal/unmarshal paths

### LONG-TERM: Advanced Performance (5+ weeks)
9. **Advanced Optimization**
   - Adaptive queue depth based on latency feedback
   - Zero-copy enhancements with registered buffers
   - NUMA-aware memory allocation

10. **Benchmarking & Validation**
    - Comparative benchmarks vs loop device, NBD, FUSE
    - Automated regression detection
    - Performance tuning documentation

### Testing Commands:
```bash
# Basic functionality (now working)
make vm-simple-e2e  # ✅ PASS
make vm-e2e         # ✅ PASS

# Performance testing (next)
make vm-perf        # TODO: implement
make vm-compare     # TODO: vs loop device
```

## Peer Review Key Insights (2025-09-26)

**ARCHITECTURE VALIDATION**: Peer review confirms our layered design is strong:
- Clean separation: control plane, queue runner, io_uring abstraction, protocol structs
- Correct SQE128/command area handling (critical past pitfalls now fixed)
- Deterministic queue tag state machine preventing race conditions

**CRITICAL API ISSUES IDENTIFIED**:
- Single `CreateAndServe` API conflates creation + start; needs staged lifecycle
- Missing device error state reporting to callers (no error channels)
- Large `DeviceParams` struct will break API when adding features
- Plain string errors lack structured errno mapping for actionable diagnostics

**MISSING PRODUCTION ESSENTIALS**:
- No observability: missing I/O counters, latency metrics, error rates
- Limited error taxonomy: no `errors.Is/As` support or kernel errno mapping
- Testing gaps: queue state machine lacks unit tests, no race detection
- Safety concerns: extensive unsafe code without guard rails or fuzzing

**PERFORMANCE BOTTLENECKS**:
- Single queue limits scaling to multiple cores
- Fixed 64KB buffers trigger dynamic allocation for large I/O
- Synchronous per-request processing prevents overlapped operations

**IMMEDIATE QUICK WINS** (Ready to implement):
- Remove duplicate `iouring.go.disabled` file
- Fix README mock backend reference mismatch
- Add structured error type with errno mapping
- Introduce basic atomic counters for ops/bytes/errors
- Add queue state machine unit tests
- Consolidate logging interfaces

## Current Status: **FUNCTIONAL PROTOTYPE WITH EXCELLENT PERFORMANCE**
The core ublk implementation is fully functional with excellent performance and verified data integrity. Strategic investment in API ergonomics, observability, and safety will prevent costly refactors and accelerate adoption.

## Performance Benchmarks

**Benchmark Results (2025-09-26)**:
- **4K Random Read (QD=1)**: 63.4k IOPS, 248 MB/s
- **4K Random Read (QD=32)**: 62.7k IOPS, 245 MB/s
- **4K Random Write (QD=1)**: 51.8k IOPS, 202 MB/s
- **128K Sequential Read**: 9,463 IOPS, 1,183 MB/s
- **Mixed 70/30 R/W**: 81k read IOPS, 34.7k write IOPS

## 🔴 CRITICAL BUG: 250ms Per FETCH_REQ Processing Delay

**Impact**: Device initialization takes `queue_depth * 250ms` (8+ seconds for default config)

**Symptoms**:
- queue_depth=1: ~1.25 seconds to initialize
- queue_depth=32: ~9 seconds to initialize
- Scales exactly linearly: 250ms per FETCH_REQ

**Investigation Results**:
- ✅ Confirmed: Each FETCH_REQ takes exactly 250ms to process
- ❌ Not caused by: Individual vs batch submission (tried batching - no improvement)
- ❌ Not caused by: Memory allocation or device size
- ❌ Not caused by: Our submission code

**Hypothesis**:
- Kernel might be timing out each FETCH_REQ after 250ms when no I/O arrives
- Or there's a deliberate 250ms delay/timer in kernel ublk processing
- Or our io_uring usage pattern triggers serialization

**Current Workaround**:
- Sleep for `queue_depth * 250ms + 1s` during device creation
- This makes device initialization very slow but functional

**TODO**:
- [ ] Check kernel ublk source for 250ms timers/timeouts
- [ ] Test if submitting FETCH_REQs AFTER device is ready helps
- [ ] Investigate if WaitForCompletion(0) blocking causes serialization
- [ ] Compare with C implementation timing


## Historical Debug Information (RESOLVED)

The following issues were major blockers but have been completely resolved:

### RESOLVED: FETCH_REQ Phantom Completions
- **Was**: FETCH_REQ would complete immediately with empty descriptors
- **Now**: FETCH_REQ properly blocks until real I/O arrives
- **Fix**: Correct SQE128 layout and proper IOCTL encoding

### RESOLVED: START_DEV Infinite Hang
- **Was**: START_DEV would hang forever, never completing
- **Now**: START_DEV completes immediately after FETCH_REQs are submitted
- **Fix**: Submit FETCH_REQs before START_DEV, use proper async patterns

### RESOLVED: Kernel -EINVAL Errors
- **Was**: All URING_CMD operations rejected with -EINVAL
- **Now**: All kernel operations accepted and working
- **Fix**: IOCTL encoding + correct SQE structure layout

The debugging journey is complete. All core functionality now works.