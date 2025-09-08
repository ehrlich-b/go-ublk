# Go-Specific Implementation Challenges for ublk

## Overview

Implementing ublk in pure Go presents unique challenges due to:
- No cgo (pure Go constraint)
- GC and memory management
- Goroutine scheduling model
- Type safety vs kernel interfaces

## Challenge 1: Syscall Interface Without cgo

### Problem
- Need to interact with kernel via ioctl and io_uring
- Complex structure marshaling
- Platform-specific constants

### Solution
```go
// Use golang.org/x/sys for syscalls
import "golang.org/x/sys/unix"

// Define ioctl commands manually
const (
    UBLK_CMD_ADD_DEV = 0x04
    // ioctl encoding
    _IOC_WRITE = 1
    _IOC_READ  = 2
    _IOC_SIZEBITS = 14
    _IOC_DIRBITS  = 2
)

func ioctlCommand(dir, typ, nr, size uint32) uint32 {
    return (dir << 30) | (size << 16) | (typ << 8) | nr
}

// Direct syscall wrapper
func ublkIoctl(fd int, cmd uint32, arg unsafe.Pointer) error {
    _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(cmd), uintptr(arg))
    if errno != 0 {
        return errno
    }
    return nil
}
```

## Challenge 2: Structure Alignment and Padding

### Problem
- Go struct layout may not match C struct layout
- Padding and alignment differences
- Endianness considerations

### Solution
```go
// Explicitly define structure layout
type UblksrvCtrlCmd struct {
    DevID      uint32   // 0-3
    QueueID    uint16   // 4-5
    Len        uint16   // 6-7
    Addr       uint64   // 8-15 (aligned to 8)
    Data       [1]uint64 // 16-23
    DevPathLen uint16   // 24-25
    Pad        uint16   // 26-27
    Reserved   uint32   // 28-31
}

// Verify size at compile time
var _ [32]byte = [unsafe.Sizeof(UblksrvCtrlCmd{})]byte{}

// Use build tags for architecture-specific code
// +build amd64

// Manual serialization when needed
func (c *UblksrvCtrlCmd) Marshal() []byte {
    buf := make([]byte, 32)
    binary.LittleEndian.PutUint32(buf[0:4], c.DevID)
    binary.LittleEndian.PutUint16(buf[4:6], c.QueueID)
    binary.LittleEndian.PutUint16(buf[6:8], c.Len)
    binary.LittleEndian.PutUint64(buf[8:16], c.Addr)
    // ...
    return buf
}
```

## Challenge 3: Memory Management with GC

### Problem
- GC can move memory (invalidating kernel pointers)
- Need stable addresses for kernel communication
- Buffer lifecycle management

### Solution
```go
// Option 1: Use runtime.Pinner (Go 1.21+)
type StableBuffer struct {
    data []byte
    pin  runtime.Pinner
}

func NewStableBuffer(size int) *StableBuffer {
    buf := &StableBuffer{
        data: make([]byte, size),
    }
    buf.pin.Pin(&buf.data[0])
    return buf
}

// Option 2: Use mmap for stable memory
func allocateStableMemory(size int) ([]byte, error) {
    return unix.Mmap(-1, 0, size,
                     unix.PROT_READ|unix.PROT_WRITE,
                     unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
}

// Option 3: Pool of pre-allocated buffers
var bufferPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 1<<20) // 1MB
        // Keep reference to prevent GC
        runtime.KeepAlive(buf)
        return buf
    },
}
```

## Challenge 4: Goroutine Scheduling

### Problem
- Need predictable thread-to-CPU mapping
- Queue affinity requirements
- Avoid scheduler overhead in hot path

### Solution
```go
// Lock goroutine to OS thread
func runQueueWorker(queueID int, cpuMask []int) {
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()
    
    // Set CPU affinity
    if err := setCPUAffinity(cpuMask); err != nil {
        log.Printf("Failed to set CPU affinity: %v", err)
    }
    
    // Process queue without yielding
    for {
        processIO() // Hot path
        
        // Avoid runtime.Gosched() in hot path
        // Use non-blocking operations
    }
}

func setCPUAffinity(cpus []int) error {
    var cpuSet unix.CPUSet
    for _, cpu := range cpus {
        cpuSet.Set(cpu)
    }
    
    return unix.SchedSetaffinity(0, &cpuSet)
}
```

## Challenge 5: io_uring Integration

### Problem
- Need SQE128/CQE32 support
- Complex memory layout
- No standard library support

### Solution
```go
// Use giouring or implement minimal wrapper
import "github.com/pawelgaczynski/giouring"

// Wrapper for ublk-specific setup
type UblkRing struct {
    ring *giouring.Ring
}

func NewUblkRing(entries uint32) (*UblkRing, error) {
    ring, err := giouring.CreateRing(entries, &giouring.Params{
        Flags: giouring.IORING_SETUP_SQE128 | giouring.IORING_SETUP_CQE32,
    })
    if err != nil {
        return nil, err
    }
    
    return &UblkRing{ring: ring}, nil
}

// Type-safe command submission
func (r *UblkRing) SubmitCommand(cmd interface{}) error {
    sqe := r.ring.GetSQE()
    if sqe == nil {
        return errors.New("submission queue full")
    }
    
    // Encode command based on type
    switch v := cmd.(type) {
    case *AddDevCmd:
        encodeAddDev(sqe, v)
    case *FetchReqCmd:
        encodeFetchReq(sqe, v)
    default:
        return fmt.Errorf("unknown command type: %T", cmd)
    }
    
    return r.ring.Submit()
}
```

## Challenge 6: Error Handling

### Problem
- Kernel errors as negative errno
- Go idioms vs C-style error codes
- Panic safety in kernel interactions

### Solution
```go
// Wrap kernel errors
type KernelError int

func (e KernelError) Error() string {
    return unix.ErrnoName(unix.Errno(-e))
}

// Convert CQE results
func handleCQEResult(res int32) error {
    if res < 0 {
        return KernelError(res)
    }
    return nil
}

// Panic recovery for kernel operations
func safeKernelOp(fn func() error) (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("kernel operation panicked: %v", r)
        }
    }()
    return fn()
}
```

## Challenge 7: Type Safety vs Performance

### Problem
- Type assertions and interfaces add overhead
- Generic operations vs specialized paths
- Balance safety and speed

### Solution
```go
// Use generics for type safety (Go 1.18+)
type Command[T any] interface {
    Encode(*SQE)
    Decode(*CQE) T
}

// Specialized fast paths
func fastPathRead(desc *UblksrvIoDesc, buf []byte) error {
    // Direct memory operations without interfaces
    sectors := desc.NrSectors
    offset := desc.StartSector * 512
    
    // Avoid allocations
    return backend.ReadAt(buf[:sectors*512], int64(offset))
}

// Batch operations to amortize overhead
func processBatch(descs []UblksrvIoDesc) {
    // Process multiple descriptors with single type check
    for i := range descs {
        processOne(&descs[i]) // Pass pointer, avoid copy
    }
}
```

## Challenge 8: Testing Without Root

### Problem
- Most ublk operations require root
- CI/CD limitations
- Mock testing complexity

### Solution
```go
// Interface for testability
type UblkDevice interface {
    AddDev(params DeviceParams) error
    StartDev() error
    StopDev() error
}

// Mock implementation
type MockDevice struct {
    mock.Mock
}

func (m *MockDevice) AddDev(params DeviceParams) error {
    args := m.Called(params)
    return args.Error(0)
}

// Build tags for test types
// +build integration

func TestRealDevice(t *testing.T) {
    requireRoot(t)
    // Real device tests
}

// Regular tests use mocks
func TestDeviceLogic(t *testing.T) {
    dev := &MockDevice{}
    dev.On("AddDev", mock.Anything).Return(nil)
    // Test business logic
}
```

## Challenge 9: Platform Compatibility

### Problem
- Linux-specific features
- Different kernel versions
- Architecture differences

### Solution
```go
// Build constraints
// +build linux
// +build amd64 arm64

// Feature detection
type Features struct {
    HasSQE128   bool
    HasZeroCopy bool
    MinKernel   KernelVersion
}

func detectFeatures() Features {
    var f Features
    
    // Probe kernel version
    f.MinKernel = getKernelVersion()
    
    // Try operations to detect support
    if err := probeS QE128(); err == nil {
        f.HasSQE128 = true
    }
    
    return f
}

// Graceful degradation
func createDevice(params DeviceParams) error {
    features := detectFeatures()
    
    if !features.HasSQE128 {
        return errors.New("kernel too old: need SQE128 support")
    }
    
    // Adjust parameters based on features
    if features.HasZeroCopy {
        params.Flags |= UBLK_F_SUPPORT_ZERO_COPY
    }
    
    return doCreateDevice(params)
}
```

## Challenge 10: Performance Profiling

### Problem
- Need to identify Go-specific bottlenecks
- GC impact on latency
- Scheduler interference

### Solution
```go
// Use pprof for profiling
import _ "net/http/pprof"

// GC tuning
func tuneGC() {
    // Reduce GC frequency for better latency
    debug.SetGCPercent(200)
    
    // Pre-allocate to reduce allocations
    buffers := make([][]byte, maxBuffers)
    for i := range buffers {
        buffers[i] = make([]byte, bufferSize)
    }
}

// Measure critical sections
func measureLatency(op string, fn func()) {
    start := time.Now()
    fn()
    latency := time.Since(start)
    
    metrics.Record(op, latency)
    
    if latency > threshold {
        log.Printf("Slow operation %s: %v", op, latency)
    }
}

// Use runtime metrics
func monitorRuntime() {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    fmt.Printf("Alloc: %d MB, GC: %d\n", 
               m.Alloc/1024/1024, m.NumGC)
}
```

## Best Practices Summary

1. **Use unsafe carefully**: Document all unsafe usage
2. **Verify struct layouts**: Use compile-time size checks
3. **Pin memory when needed**: Prevent GC movement
4. **Lock OS threads**: For CPU-bound operations
5. **Pool resources**: Reduce allocations
6. **Handle errors explicitly**: Don't ignore kernel errors
7. **Test with multiple Go versions**: Ensure compatibility
8. **Profile regularly**: Monitor performance
9. **Document platform requirements**: Be clear about Linux-only
10. **Provide escape hatches**: Allow tuning for different workloads

## Gotchas to Avoid

1. **Don't assume struct layout**: Always verify
2. **Don't ignore RLIMIT**: Check memory limits
3. **Don't block scheduler**: Use runtime.LockOSThread
4. **Don't leak goroutines**: Proper cleanup
5. **Don't ignore alignment**: Can cause crashes
6. **Don't mix Go and kernel memory**: Keep them separate
7. **Don't assume error types**: Check errno values
8. **Don't forget endianness**: Use encoding/binary
9. **Don't ignore GC pressure**: Monitor and tune
10. **Don't skip error paths**: Test failure scenarios