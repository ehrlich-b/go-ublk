# go-ublk Code Review

**Review Date:** 2025-01-25
**Status:** Working prototype with significant tech debt
**Overall Assessment:** Functional but messy - needs substantial cleanup before production use

---

## Executive Summary

The codebase implements a pure Go ublk driver that **works** (~500k IOPS, 1.6-2.0 GB/s throughput). However, it suffers from:

1. **Dual implementations everywhere** - minimal.go vs iouring.go, stub vs real, etc.
2. **Interface indirection bloat** - internal/interfaces, public interfaces, Logger aliases
3. **Dead code** - Stub runners, async code paths, unused features
4. **Inconsistent patterns** - Some files use structured logging, others Printf
5. **Documentation debt** - Comments describe what code does, not why decisions were made

**Recommended priority:** Clean up before adding features. The working I/O loop is solid; everything else is scaffolding that grew organically.

---

## File-by-File Review

### Root Package Files

---

#### `backend.go` (515 lines)

**Purpose:** Main public API - Device struct, CreateAndServe, StopAndDelete

**Critical Functions:**

| Function | Lines | Verdict | Notes |
|----------|-------|---------|-------|
| `CreateAndServe` | 162-296 | OK | Main entry point, works correctly |
| `StopAndDelete` | 427-474 | OK | Cleanup works |
| `convertToCtrlParams` | 482-512 | CLEANUP | Just copies fields - could be simpler |
| `waitLive` | 17-34 | SUSPICIOUS | Always returns nil - dead logic? |
| `createController` | 477-479 | TRIVIAL | One-liner, could inline |

**Issues:**

1. **Line 33:** `waitLive` always returns `nil` even on timeout - this seems wrong
   ```go
   // Device may still be functional even if not visible
   return nil  // ???
   ```

2. **Lines 268-288:** Magic timing delays
   ```go
   time.Sleep(constants.QueueInitDelay)  // Why 100ms?
   time.Sleep(1 * time.Millisecond)      // Why 1ms? Comment says "instead of 250ms"
   ```

3. **Lines 214-226:** NumQueues logic is confusing
   ```go
   device.queues = params.NumQueues  // Store 0
   // Later...
   numQueues := params.NumQueues
   if numQueues == 0 {
       numQueues = 1  // Override to 1
   }
   // So device.queues stays 0 but we create 1 queue?
   ```

**Recommendation:** Fix the NumQueues confusion. Remove waitLive if it does nothing useful.

---

#### `constants.go` (16 lines)

**Purpose:** Re-export internal constants

**Verdict:** FINE but POINTLESS

This file just re-exports `constants.IOBufferSizePerTag`. Either:
- Move the constant here and delete internal/constants, OR
- Don't re-export and have callers use internal package

---

#### `errors.go` (230 lines)

**Purpose:** Structured error handling with errno mapping

**Verdict:** OVER-ENGINEERED

**Issues:**

1. **Dual error types:** `Error` (new) and `UblkError` (legacy) - pick one
2. **Line 46:** Error string formatting is bizarre:
   ```go
   if len(parts) > 0 {
       return fmt.Sprintf("ublk: %s (%s)", msg, fmt.Sprintf("%s", parts[0]))
   }
   ```
   Why `fmt.Sprintf("%s", parts[0])`? Just use `parts[0]`.

3. **Constructors that aren't used:**
   - `NewDeviceError` - unused
   - `NewQueueError` - unused
   - `NewErrorWithErrno` - unused

**Recommendation:** Delete unused constructors. Consolidate to one error type.

---

#### `interfaces.go` (99 lines)

**Purpose:** Backend interface definitions

**Verdict:** OK but DUPLICATED

The interfaces are well-designed (ReadAt, WriteAt, Flush, etc.), but `Logger` is just an alias to `interfaces.Logger`. Why have both?

```go
type Logger = interfaces.Logger  // Why not just use interfaces.Logger?
```

**Recommendation:** Either delete internal/interfaces entirely or delete the public aliases.

---

#### `metrics.go` (298 lines)

**Purpose:** Performance tracking with atomic counters

**Verdict:** OK - UNUSED

The metrics implementation is reasonable, but **nothing calls it**. The Observer interface exists, MetricsObserver is created in CreateAndServe, but then never invoked from the I/O path.

**Issues:**

1. **Created but not used:** Metrics are allocated but never populated
2. **MetricsSnapshot.WriteBandwidth** - inconsistent naming (snake_case struct tag vs CamelCase field)

**Recommendation:** Either wire up metrics to the I/O loop or delete this code.

---

#### `testing.go` (265 lines)

**Purpose:** MockBackend for testing

**Verdict:** FINE

Standard mock implementation. All interface checks pass. Could add `t.Helper()` patterns but works as-is.

---

### Internal Packages

---

#### `internal/constants/constants.go` (48 lines)

**Purpose:** Configuration constants

**Verdict:** FINE but SHOULD CONSOLIDATE

These are reasonable defaults. However, having a separate package for 48 lines of constants is overkill.

**Suspicious:**
```go
DeviceStartupDelay = 500 * time.Millisecond  // Why 500ms?
QueueInitDelay = 100 * time.Millisecond       // Why 100ms?
```

These magic numbers need comments explaining *why* these specific values.

---

#### `internal/ctrl/control.go` (472 lines)

**Purpose:** Control plane - ADD_DEV, SET_PARAMS, START_DEV, etc.

**Critical Functions:**

| Function | Lines | Verdict | Notes |
|----------|-------|---------|-------|
| `NewController` | 29-53 | OK | Opens /dev/ublk-control, creates ring |
| `AddDevice` | 65-149 | MESSY | Too much debug logging, env var hacks |
| `SetParams` | 151-223 | MESSY | Buffer padding logic is confusing |
| `StartDevice` | 225-253 | OK | Works |
| `StartDeviceAsync` | 276-303 | UNUSED | Async path never called |
| `StartDataPlane` | 306-309 | DEPRECATED | Says so in comment |
| `buildFeatureFlags` | 430-455 | OK | Feature flag construction |
| `sizeToShift` | 465-471 | OK | log2 calculation |

**Issues:**

1. **Lines 96-107:** Environment variable hack for buffer sizes
   ```go
   if v := os.Getenv("UBLK_DEVINFO_LEN"); v != "" {
       // Pad to 80 bytes if requested
   }
   ```
   This should be a proper config option, not an env var.

2. **Line 130:** Hardcoded `c.useIoctl = true` overrides any configuration

3. **Lines 182-189:** Buffer padding to 128 bytes is cargo-culted:
   ```go
   if len(buf) < 128 {
       padded := make([]byte, 128)
       copy(padded, buf)
       buf = padded
       binary.LittleEndian.PutUint32(buf[0:4], 128)  // Overwrite len field
   }
   ```

**Recommendation:** Remove env var hacks. Document why 128-byte padding is needed (if it is).

---

#### `internal/ctrl/types.go` (76 lines)

**Purpose:** DeviceParams and DeviceInfo structs

**Verdict:** DUPLICATES PUBLIC API

`DeviceParams` here mirrors the public `DeviceParams` in backend.go. The conversion function `convertToCtrlParams` just copies fields between them.

**Recommendation:** Use one DeviceParams struct. The internal/public split adds no value here.

---

#### `internal/queue/runner.go` (890 lines)

**Purpose:** The I/O processing loop - **the most critical file**

**Critical Functions:**

| Function | Lines | Verdict | Notes |
|----------|-------|---------|-------|
| `NewRunner` | 70-159 | OK | Creates ring, mmaps descriptors |
| `Start` | 162-175 | OK | Spawns ioLoop goroutine |
| `Prime` | 179-198 | OK | Submits initial FETCH_REQs |
| `ioLoop` | 238-293 | **CRITICAL** | Main I/O loop - works |
| `submitInitialFetchReq` | 296-334 | OK | Per-tag state tracking |
| `processRequests` | 434-479 | OK | Handles CQEs |
| `handleCompletion` | 482-560 | OK | State machine transitions |
| `loadDescriptor` | 563-573 | **CRITICAL** | Atomic loads for descriptor reads |
| `processIOAndCommit` | 576-601 | OK | Read desc, do I/O, commit |
| `handleIORequest` | 626-721 | OK | Backend dispatch |
| `submitCommitAndFetch` | 724-777 | OK | Result submission |
| `mmapQueues` | 780-827 | **CRITICAL** | Memory mapping setup |
| `waitAndStartDataPlane` | 337-394 | UNUSED | Dead code path |
| `initializeDataPlane` | 397-431 | UNUSED | Dead code path |
| `NewStubRunner` | 856-874 | UNUSED | Stub for testing |
| `NewWaitingRunner` | 831-854 | UNUSED | Dead code path |
| `stubLoop` | 877-889 | UNUSED | Stub implementation |

**Issues:**

1. **Lines 337-431:** `waitAndStartDataPlane` and `initializeDataPlane` are dead code. The real initialization happens in `NewRunner`. These were probably from an earlier design.

2. **Lines 831-889:** `NewWaitingRunner`, `NewStubRunner`, `stubLoop` - more dead code. Delete.

3. **Lines 679-687:** Dynamic buffer allocation is scary:
   ```go
   if length > maxBufferSize {
       dynamicBuffer = make([]byte, length)
       buffer = dynamicBuffer
   }
   ```
   This allocates on the hot path. Should pre-allocate or reject oversized requests.

4. **Lines 641-662:** Logging on every I/O is noisy:
   ```go
   r.logger.Printf("[Q%d:T%02d] %s %dB @ sector %d", ...)
   ```
   This should be debug-level only.

5. **Line 241:** `runtime.LockOSThread()` is correct but should document why (ublk kernel requirement).

**State Machine Analysis:**

The tag state machine is correct:
```
TagStateInFlightFetch → TagStateOwned → TagStateInFlightCommit → (back to TagStateOwned)
```

The mutex per tag (`tagMutexes`) prevents double submission correctly.

**Recommendation:** Delete dead code (waitAndStartDataPlane, *StubRunner, etc.). Move hot-path logging to debug level.

---

#### `internal/uapi/constants.go` (159 lines)

**Purpose:** Kernel UAPI constant definitions

**Verdict:** OK

Clean mapping of kernel constants. The ioctl encoding functions are correct.

---

#### `internal/uapi/marshal.go` (312 lines)

**Purpose:** Struct serialization for kernel communication

**Critical Functions:**

| Function | Lines | Verdict | Notes |
|----------|-------|---------|-------|
| `Marshal` | 10-24 | OK | Type switch dispatcher |
| `marshalCtrlCmd` | 44-57 | OK | 32-byte serialization |
| `marshalIOCmd` | 78-87 | OK | 16-byte serialization |
| `marshalParams` | 104-155 | OK | Variable-length params |
| `directMarshal` | 206-217 | SCARY | Uses unsafe.Pointer |
| `directUnmarshal` | 220-231 | BROKEN | Bug at line 221-227 |

**Issues:**

1. **Lines 221-227:** `directUnmarshal` is buggy:
   ```go
   func directUnmarshal(data []byte, v interface{}) error {
       size := int(unsafe.Sizeof(v))  // BUG: This is size of interface, not pointed-to type!
       if len(data) < size {
           return ErrInsufficientData
       }
       dst := (*[1 << 20]byte)(unsafe.Pointer(&v))  // BUG: &v is address of interface value!
       copy(dst[:size], data[:size])
       return nil
   }
   ```
   This doesn't work correctly. It should use `reflect.ValueOf(v).Elem()` like `directMarshal` does.

2. **Line 213:** Magic number `1 << 20` (1MB) is arbitrary.

**Recommendation:** Fix `directUnmarshal` or delete it if unused.

---

#### `internal/uapi/structs.go` (207 lines)

**Purpose:** Kernel struct definitions with compile-time size checks

**Verdict:** GOOD

The compile-time size assertions are excellent:
```go
var _ [32]byte = [unsafe.Sizeof(UblksrvCtrlCmd{})]byte{}
var _ [64]byte = [unsafe.Sizeof(UblksrvCtrlDevInfo{})]byte{}
var _ [24]byte = [unsafe.Sizeof(UblksrvIODesc{})]byte{}
var _ [16]byte = [unsafe.Sizeof(UblksrvIOCmd{})]byte{}
```

This catches struct packing issues at compile time.

---

#### `internal/uring/minimal.go` (896 lines)

**Purpose:** Pure Go io_uring implementation

**Critical Functions:**

| Function | Lines | Verdict | Notes |
|----------|-------|---------|-------|
| `NewMinimalRing` | 169-258 | OK | Setup with SQE128/CQE32 |
| `SubmitCtrlCmd` | 459-536 | OK | Control command submission |
| `SubmitIOCmd` | 549-586 | OK | I/O command submission |
| `WaitForCompletion` | 588-659 | **CRITICAL** | CQ processing with EINTR handling |
| `submitAndWait` | 685-753 | OK | Synchronous submit |
| `submitOnlyCmd` | 798-836 | OK | Async submit |
| `processCompletion` | 839-889 | OK | CQE extraction |
| `Sfence` / `Mfence` | (barrier.go) | **CRITICAL** | Memory barriers |

**Issues:**

1. **Lines 851-859:** Retry loop with magic delays:
   ```go
   const maxRetries = 5
   const retryDelay = 10 * time.Microsecond
   for i := 0; i < maxRetries; i++ {
       // ...
       time.Sleep(retryDelay)
   }
   ```
   Why 5 retries? Why 10us? These need justification.

2. **Lines 98-118:** `AsyncHandle.Wait` has polling loop that could be more efficient.

3. **Lines 593-626:** `WaitForCompletion` drain function is complex. The memory barrier placement looks correct after recent fixes.

**Memory Barrier Analysis:**

The EINTR fix uses `Sfence()` before `atomic.StoreUint32(sqTail, newTail)`. This is correct:
- Ensures SQE writes are visible before kernel sees updated tail
- Uses LOCK XADD which has full fence semantics on x86-64

---

#### `internal/uring/iouring.go` (247 lines, build tag: giouring)

**Purpose:** Alternative io_uring via iceber/iouring-go library

**Verdict:** UNUSED

This file has build tag `giouring` which is never enabled. The code uses an external Go library for io_uring instead of raw syscalls.

**Recommendation:** Delete. The minimal.go implementation works fine.

---

#### `internal/uring/interface.go` (255 lines)

**Purpose:** Ring interface and stub implementation

**Critical Functions:**

| Function | Lines | Verdict | Notes |
|----------|-------|---------|-------|
| `NewRing` | 91-114 | OK | Factory with fallbacks |
| `stubRing.*` | 117-254 | PROBLEMATIC | Returns fake success |

**Issues:**

1. **Lines 174-227:** `stubRing.SubmitCtrlCmd` returns fake success for all operations:
   ```go
   case uapi.UBLK_CMD_ADD_DEV:
       return &stubResult{value: int32(devID)}, nil  // Fake success!
   ```
   This masks real failures during development.

2. **Line 112:** Falls back to stub silently:
   ```go
   logger.Warn("using stub ring - this breaks actual functionality")
   ```
   This should probably be an error, not a warning.

**Recommendation:** Make stub usage fatal or very loud. It's too easy to accidentally use stubs.

---

#### `internal/logging/logger.go` (346 lines)

**Purpose:** Structured logging with zerolog

**Verdict:** OVER-ENGINEERED

**Issues:**

1. **Async writer complexity:** Lines 52-108 implement a buffered async writer that drops messages when full. This is clever but adds complexity.

2. **Printf compatibility layer:** Lines 255-269 add `Debugf`, `Infof`, etc. that duplicate the structured methods.

3. **Domain-specific methods that aren't used:**
   - `ControlStart`, `ControlSuccess`, `ControlError` - unused
   - `IOStart`, `IOComplete`, `IOError` - unused
   - `RingSubmit`, `RingComplete` - unused
   - `MemoryMap`, `MemoryUnmap` - unused

**Recommendation:** Delete unused domain methods. Simplify async writer if message dropping is acceptable.

---

### Backend Package

---

#### `backend/mem.go` (144 lines)

**Purpose:** RAM-backed storage implementation

**Verdict:** FINE

Clean implementation of all backend interfaces. The `sync.RWMutex` usage is correct.

Minor issue: `Discard` zeroes bytes in a loop instead of using `copy` with a zero slice or `for range` pattern.

---

### Command Package

---

#### `cmd/ublk-mem/main.go` (212 lines)

**Purpose:** CLI for memory-backed ublk device

**Verdict:** OK but MESSY

**Issues:**

1. **Lines 107-134:** SIGUSR1 handler for stack dumps is useful for debugging but verbose.

2. **Lines 147-163:** Cleanup with timeout is good defensive code.

3. **Line 48:** `params.QueueDepth = 32` overrides default of 128 - should document why.

---

## Architecture Issues

### 1. Interface Indirection Explosion

```
Public API:
  ublk.Backend
  ublk.Logger
  ublk.Observer

Internal:
  internal/interfaces.Backend
  internal/interfaces.Logger

Aliased:
  ublk.Logger = interfaces.Logger

Used in:
  internal/queue (uses interfaces.Backend)
  internal/ctrl (uses interfaces.Backend)
```

**Verdict:** Pick one layer. Either everything uses public interfaces, or everything uses internal.

### 2. Dual Implementations

| Component | Implementation 1 | Implementation 2 | Used? |
|-----------|-----------------|------------------|-------|
| io_uring | minimal.go | iouring.go | minimal.go only |
| Runner | NewRunner | NewStubRunner | NewRunner only |
| Ring | minimalRing | stubRing | minimalRing only |

**Verdict:** Delete unused implementations.

### 3. Magic Numbers Without Context

```go
time.Sleep(constants.QueueInitDelay)       // 100ms - why?
time.Sleep(constants.DeviceStartupDelay)   // 500ms - why?
time.Sleep(1 * time.Millisecond)           // 1ms - comment says "instead of 250ms"
const maxRetries = 5                        // why 5?
const retryDelay = 10 * time.Microsecond   // why 10us?
```

**Verdict:** Document the reasoning behind timing values.

---

## Recommended Cleanup Order

### Phase 1: Delete Dead Code
1. Delete `internal/uring/iouring.go` (unused build tag)
2. Delete `NewStubRunner`, `NewWaitingRunner`, `stubLoop` from runner.go
3. Delete `waitAndStartDataPlane`, `initializeDataPlane` from runner.go
4. Delete `StartDataPlane` from control.go (deprecated)
5. Delete unused error constructors from errors.go
6. Delete unused logging methods from logger.go

### Phase 2: Consolidate Interfaces
1. Either use internal/interfaces everywhere OR delete it
2. Remove the `Logger = interfaces.Logger` alias
3. Merge ctrl.DeviceParams with public DeviceParams

### Phase 3: Fix Bugs
1. Fix `directUnmarshal` in marshal.go
2. Fix `waitLive` to actually return errors
3. Fix `device.queues` not matching actual queue count

### Phase 4: Improve Quality
1. Add comments explaining timing constants
2. Move I/O logging to debug level
3. Wire up metrics to I/O loop (or delete metrics)
4. Make stub usage fatal instead of warn

---

## Summary Table

| File | Lines | Verdict | Action |
|------|-------|---------|--------|
| backend.go | 515 | OK | Fix NumQueues bug |
| constants.go | 16 | OK | Consider consolidating |
| errors.go | 230 | BLOATED | Delete unused constructors |
| interfaces.go | 99 | DUPLICATED | Consolidate with internal |
| metrics.go | 298 | UNUSED | Wire up or delete |
| testing.go | 265 | OK | Keep |
| internal/ctrl/control.go | 472 | MESSY | Remove env var hacks |
| internal/ctrl/types.go | 76 | DUPLICATED | Merge with public |
| internal/queue/runner.go | 890 | OK + DEAD CODE | Delete ~200 lines dead code |
| internal/uapi/constants.go | 159 | OK | Keep |
| internal/uapi/marshal.go | 312 | BUG | Fix directUnmarshal |
| internal/uapi/structs.go | 207 | GOOD | Keep |
| internal/uring/minimal.go | 896 | OK | Document magic numbers |
| internal/uring/iouring.go | 247 | UNUSED | Delete |
| internal/uring/interface.go | 255 | OK | Make stub fatal |
| internal/logging/logger.go | 346 | BLOATED | Delete unused methods |
| backend/mem.go | 144 | OK | Keep |
| cmd/ublk-mem/main.go | 212 | OK | Keep |

**Total lines to delete:** ~400-500 (dead code, unused implementations)
**Bugs to fix:** 2-3 (directUnmarshal, waitLive, NumQueues)
**Bloat to remove:** ~200 lines (unused error constructors, logging methods)
