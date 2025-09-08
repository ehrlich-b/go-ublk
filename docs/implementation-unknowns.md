# Implementation Unknowns and Research Questions

## Critical Unknowns

### 1. io_uring URING_CMD Encoding

**Question**: How exactly is the ublk command data encoded in the SQE cmd[80] area?

**What we know**:
- Uses IORING_OP_URING_CMD opcode
- Has 80 bytes of command-specific space with SQE128
- Some versions use "ioctl encoding" (UBLK_F_CMD_IOCTL_ENCODE)

**What we need to determine**:
- Exact byte layout of ublksrv_ctrl_cmd in cmd area
- Differences between ioctl vs native encoding
- Endianness considerations
- Padding/alignment requirements

**Research approach**:
- Study ublksrv implementation
- Use strace on ublksrv to see actual SQE contents
- Write test program to probe different encodings

### 2. Descriptor Array Memory Layout

**Question**: What is the exact mmap layout and access pattern?

**What we know**:
- mmap from /dev/ublkc<ID> at offset 0x80000000
- Array of ublksrv_io_desc structures
- Indexed by (queue_id * queue_depth + tag)

**What we need to determine**:
- Total mmap size calculation
- Per-queue vs global layout
- Synchronization requirements (memory barriers?)
- Cache line considerations

**Research approach**:
- Examine kernel source for mmap handler
- Test with different queue configurations
- Profile memory access patterns

### 3. NEED_GET_DATA Flow

**Question**: How exactly does the two-phase write work?

**What we know**:
- First phase: write request arrives without data
- Second phase: after NEED_GET_DATA, request redelivered with data

**What we need to determine**:
- How to specify buffer address for kernel copy
- State tracking between phases
- Error handling in two-phase flow
- Performance implications

**Research approach**:
- Test with UBLK_F_NEED_GET_DATA enabled
- Compare with default single-phase path
- Benchmark overhead

### 4. Queue Affinity Implementation

**Question**: How to properly implement CPU affinity for queue threads?

**What we know**:
- GET_QUEUE_AFFINITY returns CPU mask per queue
- Should match kernel's blk-mq mapping

**What we need to determine**:
- CPU mask format and parsing
- goroutine → OS thread → CPU binding
- NUMA considerations
- Impact on Go scheduler

**Research approach**:
- Test on multi-socket systems
- Profile cross-CPU traffic
- Compare with and without affinity

### 5. Error Recovery Semantics

**Question**: How to handle partial failures and recovery?

**What we know**:
- UBLK_F_USER_RECOVERY exists
- START_USER_RECOVERY/END_USER_RECOVERY commands

**What we need to determine**:
- When can recovery be initiated?
- How to restore queue state?
- In-flight I/O handling
- Timeout mechanisms

**Research approach**:
- Inject failures at different points
- Test recovery sequences
- Study kernel recovery code

## Go-Specific Challenges

### 1. Unsafe Memory Management

**Challenge**: Managing memory shared with kernel

**Considerations**:
- GC interaction with mmap'd memory
- Ensuring buffers don't move
- Proper cleanup on panic
- Race conditions

**Approach**:
- Use runtime.Pinner for critical buffers
- Careful unsafe.Pointer usage
- Defer cleanup in all paths

### 2. Syscall Performance

**Challenge**: Minimizing syscall overhead from Go

**Considerations**:
- Go runtime overhead per syscall
- Batching opportunities
- SQPOLL vs interrupt mode
- goroutine scheduling

**Approach**:
- Benchmark different strategies
- Consider runtime.LockOSThread
- Profile syscall patterns

### 3. goroutine vs OS Thread Model

**Challenge**: Mapping queue runners to execution model

**Options**:
1. One goroutine per queue (simple)
2. One OS thread per queue (predictable)
3. Worker pool model (complex)

**Trade-offs**:
- Latency vs throughput
- CPU efficiency
- Complexity

## Testing Challenges

### 1. Kernel Version Matrix

**Challenge**: Supporting multiple kernel versions

**Approach**:
- Set up CI with different kernels
- Feature detection at runtime
- Graceful degradation
- Clear error messages

### 2. Root Requirement

**Challenge**: Most operations need root

**Solutions**:
- Use VMs/containers for CI
- Implement unprivileged mode support
- Provide mock/stub backends
- Document permission requirements

### 3. Performance Validation

**Challenge**: Ensuring competitive performance

**Metrics needed**:
- IOPS at various block sizes
- Latency percentiles
- CPU utilization
- Memory usage

**Comparison targets**:
- kernel loop device
- ublksrv (C implementation)
- NBD

## Implementation Order Questions

### Option 1: Bottom-up
1. io_uring wrapper
2. UAPI definitions
3. Control plane
4. Data plane
5. Public API

**Pros**: Can test each layer
**Cons**: May need refactoring

### Option 2: Top-down
1. Public API design
2. Mock implementation
3. Replace mocks with real code

**Pros**: API-first design
**Cons**: Late integration testing

### Option 3: Vertical Slice
1. Minimal working example
2. Expand functionality
3. Optimize

**Pros**: Early validation
**Cons**: May accumulate tech debt

## Documentation Gaps

### From Kernel Docs
- Exact SQE/CQE layouts for URING_CMD
- State machine edge cases
- Feature interaction matrix
- Performance tuning guidance

### From ublksrv
- Design decisions and trade-offs
- Why certain approaches chosen
- Known issues/limitations
- Performance characteristics

## Performance Targets

### Minimum Acceptable
- 50% of kernel loop device performance
- <1ms P99 latency for 4K I/O
- <10% CPU overhead vs C implementation

### Stretch Goals
- Match kernel loop device performance
- Sub-100μs P50 latency
- Support 1M+ IOPS

## Risk Assessment

### High Risk Areas
1. **Memory corruption**: Shared memory with kernel
2. **Deadlocks**: Queue state management
3. **Performance**: May not meet targets
4. **Compatibility**: Kernel version differences

### Mitigation Strategies
- Extensive testing
- Careful code review
- Performance profiling
- Clear documentation

## Next Steps

### Immediate Research
1. Build minimal C prototype using ublk
2. Trace ublksrv with strace/bpftrace
3. Test io_uring Go libraries
4. Benchmark baseline performance

### Proof of Concept
1. Create minimal ublk device
2. Implement single-queue RAM backend
3. Measure performance
4. Validate approach

### Decision Points
1. Which io_uring library?
2. goroutine model?
3. Error handling strategy?
4. API design freeze

## Questions for Community

### For Kernel Developers
- Is URING_CMD encoding documented?
- Any planned API changes?
- Performance recommendations?

### For Go Community
- Experience with io_uring from Go?
- Unsafe memory management patterns?
- High-performance I/O examples?

### For ublk Users
- Most important features?
- Performance requirements?
- Use cases we should consider?