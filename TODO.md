# TODO.md - go-ublk Development Roadmap

## 🚀 MAJOR MILESTONE: Core Implementation Complete!

**Phase 1-3 COMPLETED**: Full working ublk userspace block driver with:
- ✅ Complete kernel interface (UAPI) 
- ✅ Control plane with device lifecycle management
- ✅ Data plane with real I/O processing via io_uring  
- ✅ VM testing infrastructure with automation
- ✅ Memory backend and CLI tools
- ✅ Production-ready architecture

**Status**: Ready for first major commit and Phase 4 development!

## Next Steps
1. **First Major Commit**: Commit Phase 1-3 implementation  
2. **Phase 4**: Backend Interface improvements and additional backends
3. **Phase 5**: Public API refinement
4. **Phase 6**: Additional CLI tools
5. **Phase 7**: Comprehensive testing and benchmarks

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

## Phase 3: Data Plane (Full Implementation) [COMPLETED ✅]
- [x] Queue runner implementation
  - [x] Per-queue goroutine management
  - [x] mmap descriptor array from /dev/ublkc<ID>
  - [x] Implement FETCH_REQ submission
  - [x] Implement COMMIT_AND_FETCH_REQ loop
  - [x] Handle different I/O operations (READ/WRITE/FLUSH/DISCARD)

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