# TODO.md - go-ublk Development Roadmap

## ⚠️ STATUS: DEEP KERNEL PROTOCOL ISSUE IDENTIFIED

**ACTUAL STATUS**: Control plane working, FETCH_REQ commands submitting, but kernel protocol incomplete

**What's Working**: 
- ✅ Complete kernel interface (UAPI) definitions
- ✅ VM testing infrastructure with automation  
- ✅ Memory backend interface design
- ✅ CLI tools framework
- ✅ Data plane I/O processing logic implemented
- ✅ **io_uring syscalls confirmed working** - real kernel communication happening
- ✅ **Control plane functional** - ADD_DEV, SET_PARAMS, START_DEV all succeed
- ✅ **FETCH_REQ commands submitting** - 32 commands per queue submitted successfully
- ✅ **Fixed architectural ordering** - START_DEV → FETCH_REQ → queue runners

**What's NOT Working**:
- ❌ **CRITICAL**: Device nodes (/dev/ublkb*, /dev/ublkc*) still not created despite successful FETCH_REQ
- ❌ **Kernel protocol gap**: Missing some step or parameter that triggers device node creation
- ❌ End-to-end I/O blocked by missing device nodes

**Status**: Pre-alpha, **advanced debugging stage - kernel protocol investigation needed**

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

## Immediate Priority Tasks (Phase 3.5 - Cleanup & Performance Baseline)

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

### CRITICAL: End-to-End I/O Verification (MANDATORY)
- [x] Create end-to-end test with dd commands ✅ - test-e2e.sh
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

## Phase 3: Data Plane (Full Implementation) [NOT COMPLETED ❌]
- [ ] Queue runner implementation
  - [x] Per-queue goroutine management
  - [x] mmap descriptor array from /dev/ublkc<ID>
  - [x] Implement FETCH_REQ submission (stub only)
  - [ ] Implement real completion processing (currently just sched_yield)
  - [ ] Handle different I/O operations (READ/WRITE/FLUSH/DISCARD) - not implemented

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
- None yet

## Notes
- Keep kernel version requirements in mind
- Test on both privileged and unprivileged modes
- Ensure clean shutdown and resource cleanup
- Consider NUMA and CPU affinity from the start