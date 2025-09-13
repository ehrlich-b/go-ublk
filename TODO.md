# TODO.md - go-ublk Development Roadmap

## ⚠️ STATUS: Control Plane Protocol Issue; ADD_DEV = -EINVAL (Deep Investigation Needed)

**ACTUAL STATUS (2025-09-13, reanchor session)**: Major progress made on control plane implementation. All fundamental issues fixed but ADD_DEV still returns `-EINVAL` (-22). Verified correct parameters being sent: queues=1, depth=32, maxIO=1MB, flags=0x40. Need deeper kernel protocol investigation or reference implementation comparison.

**✅ FIXED IN THIS SESSION**:
- Module loading: ublk_drv properly loaded on VM
- URING_CMD SQE setup: addr field correctly set to buffer address (was hardcoded to 0)
- Device info buffer: properly populated with valid UblksrvCtrlDevInfo structure (was empty)
- NumQueues validation: fixed 0 → 1 to meet kernel requirements
- IOCTL encoding: verified correct (0xc0207504)
- Debugging: comprehensive logging shows correct parameter values being sent

**What's Working**: 
- ✅ Complete kernel interface (UAPI) definitions
- ✅ VM testing infrastructure with automation  
- ✅ Memory backend interface design
- ✅ CLI tools framework
- ✅ Data plane I/O processing logic implemented
- ✅ io_uring integration: rings mapped correctly (SQ/CQ/SQEs); `IORING_OP_URING_CMD` path is real and returns kernel results
- ✅ Deterministic control-plane test harness (no fallback), with make targets to test explicit variants

**What's NOT Working**:
- ❌ `ADD_DEV` = `-EINVAL` for current control struct/encoding (both 64/80 dev_info and raw/ioctl variants tested)
- ❌ `SET_PARAMS` = `-EINVAL` when encoding mismatched (needs unified mode post-ADD_DEV)
- ❌ Success logs previously printed before char device open (to be fixed post-control-plane)

**Status**: Pre-alpha. Control-plane fix (48‑byte control struct/encoding) is the immediate priority; data plane follows.

### Session Outcome (2025-09-09)
- Removed fallback; single-pass control ops reveal true kernel behavior
- Added make targets to test variants: `vm-e2e-64`, `vm-e2e-80`, `vm-e2e-64-raw`, `vm-e2e-80-raw`
- Observed: `ADD_DEV = -EINVAL` for current 32‑byte control payload copy → indicates need for 48‑byte control struct + ioctl size 48
- Data-plane CQE loop implemented previously; will be exercised after control-plane fix

## Architectural Review Findings (2025-09-08)

### ✅ Architectural Strengths
- **Clean Layered Architecture**: Well-separated control/data planes and backends
- **Pure Go Achievement**: Successfully avoided cgo, using syscalls and unsafe appropriately  
- **Idiomatic Go Patterns**: Proper interfaces, error handling, context usage
- **Production Foundation**: Resource cleanup, graceful shutdown, signal handling
- **Good Test Coverage**: Unit tests passing, VM infrastructure working
- **Clean Backend Interface**: Mirrors standard Go io.ReaderAt/WriterAt patterns
- **Comprehensive Documentation**: Excellent technical docs in /docs directory

### ⚠️ Technical Debt & Immediate Fixes Needed
1. **Memory Unmapping**: mmap'd regions not properly unmapped (runner.go:127)
2. **Missing Tests**: No unit tests for internal/queue package
3. **Integration Tests Stubbed**: Tests have TODOs instead of implementations
4. **Error Code Mapping**: Need proper errno to Go error conversion
5. **Resource Limits**: RLIMIT_MEMLOCK handling not implemented
6. **Makefile Updated**: ✅ Fixed build target for ublk-mem

### 🔧 Architectural Improvements for Next Phase
1. **Backend Registry**: Factory pattern for backend type registration
2. **Configuration Management**: Structured config file support (YAML/TOML)
3. **Observability**: Metrics, tracing, structured logging hooks
4. **Recovery Modes**: User recovery and crash resilience
5. **Performance**: CPU affinity, NUMA awareness, buffer pool optimization

## Immediate Priority Tasks (Phase 3.5 - Control Plane Fix, Then Data Plane)

### Performance Baseline (HIGH PRIORITY) ❌ INVALID RESULTS!
- [x] Create fio job files for standard tests (4K random, 128K sequential)
- [ ] **REDO: Benchmark go-ublk runtime overhead** - Previous results invalid due to non-functional data plane
- [ ] Implement real I/O processing first
- [ ] Measure actual latency distribution after I/O works
- [ ] Compare with kernel loop device as baseline
- [ ] Document real userspace overhead
- [ ] Profile CPU usage and context switches
- [ ] Test with different queue depths (1, 32)

**CRITICAL**: Previous performance claims were impossible - data plane is stubbed with sched_yield only!

### CRITICAL: Control Plane Fix (MANDATORY)
- [ ] Switch `UblksrvCtrlCmd` to 48 bytes (match kernel `ublksrv_ctrl_cmd`)
- [ ] Update marshal/unmarshal + compile-time size checks to 48
- [ ] Update `UblkCtrlCmd()` ioctl encoding size to 48
- [ ] Copy 48‑byte payload into `SQE128.cmd[80]` for control ops
- [ ] Re-test `ADD_DEV` variants via make targets:
  - `make -B ublk-mem vm-e2e-64`
  - `make -B ublk-mem vm-e2e-80`
  - If needed: `vm-e2e-64-raw` / `vm-e2e-80-raw`
- [ ] Lock-in encoding mode after `ADD_DEV` and apply uniformly to `SET_PARAMS`/`START`/`STOP`/`DEL`/`GET*`

### End-to-End I/O Verification (post-fix)
- [x] Create end-to-end test with dd commands ✅ - test-e2e.sh
- [x] Implement data-plane completions (CQE wait/parse)
- [ ] **MANDATORY: test-e2e.sh MUST PASS before any functionality claims**
- [ ] **MANDATORY: test-e2e.sh MUST PASS before any performance testing**  
- [ ] Add end-to-end test to CI/automated testing
- [ ] Document that ALL development must verify with test-e2e.sh

**RULE: NEVER claim functionality works without test-e2e.sh passing**

### Technical Debt  
- [x] Fix memory unmapping in queue/runner.go Close() method ✅
- [ ] Add unit tests for internal/queue package  
- [ ] Implement proper errno to Go error mapping
- [ ] Add RLIMIT_MEMLOCK checking and setting
- [ ] Create error recovery documentation

## Phase 0: Technical Preparation [COMPLETED]
- [x] Research and Documentation
  - [x] Study kernel ublk implementation (block/ublk_drv.c)
  - [x] Document all UAPI constants and structures (docs/ublk-uapi-reference.md)
  - [x] Map io_uring URING_CMD usage for ublk (docs/uring-cmd-encoding.md)
  - [x] Understand descriptor array memory layout (docs/memory-management.md)
  - [x] Document control command sequences (docs/ublk-technical-reference.md)
  - [x] Study data plane state machines
  - [x] Document Go-specific implementation challenges (docs/go-implementation-challenges.md)

- [ ] Environment Setup
  - [x] Determine minimum kernel version (6.1 baseline, targeting 6.11)
  - [ ] Setup test VMs with different kernel versions
  - [x] Document kernel config requirements (CONFIG_BLK_DEV_UBLK, CONFIG_IO_URING)
  - [ ] Test ublk_drv module availability

- [ ] Testing Strategy
  - [ ] Create kernel feature detection script
  - [ ] Setup automated test environment (VMs/containers)
  - [ ] Define test matrix (kernel versions × features)
  - [ ] Create test data generators
  - [ ] Setup performance baseline measurements

- [ ] Technical Decisions
  - [ ] Evaluate io_uring Go libraries (giouring vs alternatives)
  - [ ] Decide on error handling strategy
  - [x] Plan memory management approach (docs/memory-management.md)
  - [ ] Design logging/debugging framework
  - [ ] Choose benchmarking tools (fio configurations)

- [ ] Reference Implementation Study
  - [ ] Analyze ublksrv C implementation
  - [ ] Study SPDK ublk module
  - [ ] Review existing Go block device projects
  - [ ] Document key learnings and patterns

## Phase 1: Foundation [COMPLETED]
- [x] Project setup
  - [x] Initialize go.mod with module path
  - [x] Setup directory structure
  - [x] Add golangci-lint configuration
  - [x] Create Makefile with common targets

- [x] Kernel interface mapping
  - [x] Create internal/uapi package
  - [x] Define all ublk constants from kernel headers
  - [x] Define packed structs (ublksrv_ctrl_dev_info, ublksrv_io_desc, etc.)
  - [x] Add syscall wrappers for mmap/munmap

- [x] io_uring abstraction
  - [x] Evaluate giouring vs other pure-Go libraries
  - [x] Create internal/uring wrapper package
  - [x] Implement ring creation with SQE128/CQE32 support
  - [x] Add URING_CMD SQE builder
  - [x] Implement feature probing

## Phase 2: Control Plane [COMPLETED]
- [x] Basic control operations
  - [x] Open /dev/ublk-control
  - [x] Implement ADD_DEV command
  - [x] Implement SET_PARAMS command
  - [x] Implement START_DEV command
  - [x] Implement STOP_DEV command
  - [x] Implement DEL_DEV command

- [x] Feature negotiation
  - [x] Parse kernel capabilities
  - [x] Handle UBLK_F_UNPRIVILEGED_DEV
  - [x] Handle basic feature flags
  - [x] Basic CreateAndServe implementation

## Phase 2.5: Minimal Data Plane for Testing [COMPLETED]
- [x] Create basic queue runner implementation
  - [x] Single-queue, single-thread implementation
  - [x] mmap descriptor array from /dev/ublkcN
  - [x] Simple FETCH_REQ → handle I/O → COMMIT_AND_FETCH_REQ loop (stub implementation)
  - [x] Basic READ/WRITE operation support (stub)
  - [x] Minimal error handling
- [x] Create hello world test program
  - [x] Simple RAM disk implementation (ublk-mem)
  - [x] Basic CLI for testing
  - [x] End-to-end control plane validation test
- [x] VM testing infrastructure
  - [x] Automated deployment via Makefile
  - [x] VM test script with kernel requirements check
  - [x] Passwordless sudo setup for ublk operations
  - [x] Control plane validation (ADD_DEV, SET_PARAMS, START_DEV working)

## Phase 3: Data Plane (Full Implementation) [BLOCKED on Control Fix]
- [ ] Queue runner implementation
  - [x] Per-queue goroutine management
  - [x] mmap descriptor array from /dev/ublkc<ID>
  - [x] Implement FETCH_REQ submission (stub only)
  - [x] Implement real completion processing (io_uring_enter + CQE loop)
  - [ ] Handle READ/WRITE/FLUSH/DISCARD via backend

- [x] Buffer management
  - [x] Default path with pre-allocated buffers
  - [ ] NEED_GET_DATA path for writes (future enhancement)
  - [ ] Buffer registration optimization (future enhancement)

## Phase 4: Backend Interface
- [ ] Define Backend interface
  - [ ] ReadAt/WriteAt methods
  - [ ] Flush/Trim operations
  - [ ] Size negotiation

- [ ] Implement basic backends
  - [ ] Memory backend (RAM disk)
  - [ ] Null backend (discard writes, zero reads)
  - [ ] File backend (loop device equivalent)
  - [ ] Read-only zip backend

## Phase 5: Public API
- [ ] Main device API
  - [ ] CreateAndServe function
  - [ ] StopAndDelete function
  - [ ] Device params structure
  - [ ] Options and configuration

- [ ] Error handling
  - [ ] Define error types
  - [ ] Graceful degradation
  - [ ] Cleanup on failure

## Phase 6: CLI Tools
- [ ] ublk-mem command
- [ ] ublk-null command
- [ ] ublk-file command
- [ ] ublk-zip command
- [ ] Common flags and signal handling

## Phase 7: Testing & Validation
- [ ] Unit tests
  - [ ] UAPI marshal/unmarshal
  - [ ] Backend implementations
  - [ ] Control operations

- [ ] Integration tests
  - [ ] Device creation/deletion
  - [ ] Basic I/O operations
  - [ ] Filesystem creation and mount
  - [ ] Stress testing

- [ ] Performance benchmarks
  - [ ] Compare with kernel loop device
  - [ ] Measure latency and throughput
  - [ ] Profile CPU and memory usage

## Phase 8: Documentation
- [ ] API documentation
- [ ] Usage examples
- [ ] Kernel requirements guide
- [ ] Troubleshooting guide

## Phase 9: CI/CD
- [ ] GitHub Actions workflow
- [ ] Automated testing on multiple kernel versions
- [ ] Release process

## Future Work (Post v1)
- [ ] User recovery support
- [ ] Zoned block device operations
- [ ] Advanced caching strategies
- [ ] Container runtime integration
- [ ] NVMe passthrough backend

## Known Issues & Blockers
- `ADD_DEV = -EINVAL` due to control struct/encoding mismatch → fix with 48‑byte ctrl struct
- `SET_PARAMS = -EINVAL` if encoding differs from `ADD_DEV` mode → unify encoding after `ADD_DEV`
- Success log should follow char device open; move final ready log after `/dev/ublkc<ID>` open

## Next Steps
- Implement `WaitForCompletion(timeout)` to fetch CQEs and drive `processRequests()`
- Submit initial FETCH_REQ per tag after opening `/dev/ublkc<ID>`; on COMMIT, chain COMMIT_AND_FETCH_REQ
- Apply encoding/len selection to STOP_DEV/DEL_DEV; verify clean teardown
- Add a small capability probe (try GET_DEV_INFO and inspect returned size/fields) to lock encoding mode per kernel
- Minimal e2e: create FS, write/read few MB, validate checksum; gate performance work behind passing e2e

## Notes
- Keep kernel version requirements in mind
- Test on both privileged and unprivileged modes
- Ensure clean shutdown and resource cleanup
- Consider NUMA and CPU affinity from the start
