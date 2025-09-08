# TODO.md - go-ublk Development Roadmap

## Phase 0: Technical Preparation [CURRENT]
- [ ] Research and Documentation
  - [ ] Study kernel ublk implementation (block/ublk_drv.c)
  - [ ] Document all UAPI constants and structures
  - [ ] Map io_uring URING_CMD usage for ublk
  - [ ] Understand descriptor array memory layout
  - [ ] Document control command sequences
  - [ ] Study data plane state machines

- [ ] Environment Setup
  - [ ] Determine minimum kernel version (6.1 baseline, 6.2+ for unprivileged)
  - [ ] Setup test VMs with different kernel versions
  - [ ] Document kernel config requirements (CONFIG_BLK_DEV_UBLK, CONFIG_IO_URING)
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
  - [ ] Plan memory management approach
  - [ ] Design logging/debugging framework
  - [ ] Choose benchmarking tools (fio configurations)

- [ ] Reference Implementation Study
  - [ ] Analyze ublksrv C implementation
  - [ ] Study SPDK ublk module
  - [ ] Review existing Go block device projects
  - [ ] Document key learnings and patterns

## Phase 1: Foundation
- [ ] Project setup
  - [ ] Initialize go.mod with module path
  - [ ] Setup directory structure
  - [ ] Add golangci-lint configuration
  - [ ] Create Makefile with common targets

- [ ] Kernel interface mapping
  - [ ] Create internal/uapi package
  - [ ] Define all ublk constants from kernel headers
  - [ ] Define packed structs (ublksrv_ctrl_dev_info, ublksrv_io_desc, etc.)
  - [ ] Add syscall wrappers for mmap/munmap

- [ ] io_uring abstraction
  - [ ] Evaluate giouring vs other pure-Go libraries
  - [ ] Create internal/uring wrapper package
  - [ ] Implement ring creation with SQE128/CQE32 support
  - [ ] Add URING_CMD SQE builder
  - [ ] Implement feature probing

## Phase 2: Control Plane
- [ ] Basic control operations
  - [ ] Open /dev/ublk-control
  - [ ] Implement ADD_DEV command
  - [ ] Implement SET_PARAMS command
  - [ ] Implement START_DEV command
  - [ ] Implement STOP_DEV command
  - [ ] Implement DEL_DEV command

- [ ] Feature negotiation
  - [ ] Parse kernel capabilities
  - [ ] Handle UBLK_F_UNPRIVILEGED_DEV
  - [ ] Handle UBLK_F_NEED_GET_DATA
  - [ ] Handle UBLK_F_PER_IO_DAEMON
  - [ ] Queue affinity support

## Phase 3: Data Plane
- [ ] Queue runner implementation
  - [ ] Per-queue goroutine management
  - [ ] mmap descriptor array from /dev/ublkc<ID>
  - [ ] Implement FETCH_REQ submission
  - [ ] Implement COMMIT_AND_FETCH_REQ loop
  - [ ] Handle different I/O operations (READ/WRITE/FLUSH/DISCARD)

- [ ] Buffer management
  - [ ] Default path with pre-allocated buffers
  - [ ] NEED_GET_DATA path for writes
  - [ ] Buffer registration optimization

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