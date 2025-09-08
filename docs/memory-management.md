# Memory Management for ublk

## Overview

ublk requires careful memory management for:
1. Descriptor arrays (mmap'd from kernel)
2. I/O buffers (user-allocated or kernel-mapped)
3. io_uring rings (shared memory)
4. Command structures

## Descriptor Array Management

### mmap Layout
```go
// Descriptor array is mmap'd from /dev/ublkc<ID>
const UBLKSRV_IO_BUF_OFFSET = 0x80000000

type DescriptorArray struct {
    fd     int
    size   int
    memory unsafe.Pointer
}

func mapDescriptors(fd int, queues, depth int) (*DescriptorArray, error) {
    size := queues * depth * int(unsafe.Sizeof(UblksrvIoDesc{}))
    
    // mmap at specific offset
    mem, err := syscall.Mmap(fd, UBLKSRV_IO_BUF_OFFSET, size,
                            syscall.PROT_READ|syscall.PROT_WRITE,
                            syscall.MAP_SHARED)
    if err != nil {
        return nil, err
    }
    
    return &DescriptorArray{
        fd:     fd,
        size:   size,
        memory: unsafe.Pointer(&mem[0]),
    }, nil
}

func (d *DescriptorArray) Get(queueID, tag int, queueDepth int) *UblksrvIoDesc {
    index := queueID * queueDepth + tag
    descSize := unsafe.Sizeof(UblksrvIoDesc{})
    offset := uintptr(index) * descSize
    
    return (*UblksrvIoDesc)(unsafe.Pointer(uintptr(d.memory) + offset))
}

func (d *DescriptorArray) Unmap() error {
    return syscall.Munmap((*[1 << 30]byte)(d.memory)[:d.size])
}
```

### Descriptor Structure Access
```go
type UblksrvIoDesc struct {
    OpFlags     uint32  // op: bits 0-7, flags: bits 8-31
    NrSectors   uint32  // Number of sectors (or nr_zones)
    StartSector uint64  // Starting sector
    Addr        uint64  // Buffer address in userspace
}

func (d *UblksrvIoDesc) GetOp() uint8 {
    return uint8(d.OpFlags & 0xff)
}

func (d *UblksrvIoDesc) GetFlags() uint32 {
    return d.OpFlags >> 8
}

func (d *UblksrvIoDesc) SetResult(result int32) {
    // Result is written back to a different location
    // This needs special handling
}
```

## I/O Buffer Management

### Buffer Allocation Strategies

#### Strategy 1: Pre-allocated Buffers
```go
type BufferPool struct {
    buffers [][]byte
    size    int
}

func NewBufferPool(queues, depth, bufSize int) *BufferPool {
    pool := &BufferPool{
        buffers: make([][]byte, queues*depth),
        size:    bufSize,
    }
    
    for i := range pool.buffers {
        // Allocate aligned memory
        pool.buffers[i] = allocateAligned(bufSize, 4096)
    }
    
    return pool
}

func (p *BufferPool) GetBuffer(queueID, tag int, queueDepth int) []byte {
    index := queueID * queueDepth + tag
    return p.buffers[index]
}
```

#### Strategy 2: Dynamic Allocation
```go
type DynamicBufferManager struct {
    cache sync.Pool
    size  int
}

func NewDynamicBufferManager(bufSize int) *DynamicBufferManager {
    return &DynamicBufferManager{
        size: bufSize,
        cache: sync.Pool{
            New: func() interface{} {
                return allocateAligned(bufSize, 4096)
            },
        },
    }
}

func (m *DynamicBufferManager) Get() []byte {
    return m.cache.Get().([]byte)
}

func (m *DynamicBufferManager) Put(buf []byte) {
    m.cache.Put(buf)
}
```

### Memory Alignment
```go
// Allocate page-aligned memory
func allocateAligned(size, align int) []byte {
    // Over-allocate to ensure alignment
    buf := make([]byte, size+align)
    
    // Find aligned offset
    ptr := uintptr(unsafe.Pointer(&buf[0]))
    offset := (align - (ptr % uintptr(align))) % uintptr(align)
    
    return buf[offset : offset+size]
}

// Using cgo-free approach with syscalls
func allocateDirectMemory(size int) ([]byte, error) {
    // Use mmap for direct memory allocation
    mem, err := syscall.Mmap(-1, 0, size,
                            syscall.PROT_READ|syscall.PROT_WRITE,
                            syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS)
    if err != nil {
        return nil, err
    }
    return mem, nil
}
```

## Memory Pinning

### Preventing GC Movement
```go
import "runtime"

type PinnedBuffer struct {
    data []byte
    pin  runtime.Pinner
}

func NewPinnedBuffer(size int) *PinnedBuffer {
    buf := &PinnedBuffer{
        data: make([]byte, size),
    }
    
    // Pin the buffer to prevent GC movement
    buf.pin.Pin(&buf.data[0])
    
    return buf
}

func (b *PinnedBuffer) Release() {
    b.pin.Unpin()
}

func (b *PinnedBuffer) Addr() uintptr {
    return uintptr(unsafe.Pointer(&b.data[0]))
}
```

## RLIMIT_MEMLOCK Handling

### Checking and Setting Limits
```go
func ensureMemlockLimit(required uint64) error {
    var rlimit syscall.Rlimit
    
    // Get current limit
    if err := syscall.Getrlimit(syscall.RLIMIT_MEMLOCK, &rlimit); err != nil {
        return err
    }
    
    if rlimit.Cur < required {
        // Try to increase limit
        rlimit.Cur = required
        if rlimit.Max < required {
            rlimit.Max = required
        }
        
        if err := syscall.Setrlimit(syscall.RLIMIT_MEMLOCK, &rlimit); err != nil {
            // May need CAP_IPC_LOCK or root
            return fmt.Errorf("insufficient RLIMIT_MEMLOCK: need %d, have %d: %w",
                             required, rlimit.Cur, err)
        }
    }
    
    return nil
}

// Calculate required memory lock size
func calculateMemlockSize(queues, depth, bufSize int) uint64 {
    // Descriptor array
    descSize := queues * depth * 32 // sizeof(ublksrv_io_desc)
    
    // I/O buffers
    bufferSize := queues * depth * bufSize
    
    // io_uring rings (approximate)
    ringSize := queues * (4096 * 4) // SQ + CQ + SQEs + CQEs
    
    return uint64(descSize + bufferSize + ringSize)
}
```

## io_uring Memory Management

### Ring Memory Setup
```go
type RingMemory struct {
    sq     *SubmissionQueue
    cq     *CompletionQueue
    sqes   []SQE
    cqes   []CQE
}

func setupRingMemory(entries int) (*RingMemory, error) {
    // Allocate ring structures
    // This is typically handled by the io_uring library
    // but understanding the layout is important
    
    sqSize := unsafe.Sizeof(SubmissionQueue{})
    cqSize := unsafe.Sizeof(CompletionQueue{})
    sqeSize := unsafe.Sizeof(SQE128{}) * uintptr(entries)
    cqeSize := unsafe.Sizeof(CQE32{}) * uintptr(entries)
    
    totalSize := sqSize + cqSize + sqeSize + cqeSize
    
    // Memory would be mmap'd from kernel
    // This is pseudo-code for illustration
    memory := allocateDirectMemory(int(totalSize))
    
    return &RingMemory{
        // Initialize pointers into memory
    }, nil
}
```

## Zero-Copy Considerations

### When Zero-Copy is Possible
```go
func canUseZeroCopy(flags uint64, blockSize int) bool {
    return (flags & UBLK_F_SUPPORT_ZERO_COPY) != 0 && 
           blockSize == 4096
}

// Zero-copy buffer mapping
type ZeroCopyBuffer struct {
    kernelAddr uintptr
    userAddr   uintptr
    size       int
}

func mapZeroCopyBuffer(kernelAddr uintptr, size int) (*ZeroCopyBuffer, error) {
    // Map kernel buffer into userspace
    // This requires special kernel support
    userAddr, err := mapKernelBuffer(kernelAddr, size)
    if err != nil {
        return nil, err
    }
    
    return &ZeroCopyBuffer{
        kernelAddr: kernelAddr,
        userAddr:   userAddr,
        size:       size,
    }, nil
}
```

## Memory Safety in Go

### Safe Access Patterns
```go
// Unsafe but necessary for kernel interaction
func readDescriptor(ptr unsafe.Pointer) UblksrvIoDesc {
    // Direct memory read
    return *(*UblksrvIoDesc)(ptr)
}

// Safer wrapper
type SafeDescriptorArray struct {
    raw    *DescriptorArray
    queues int
    depth  int
}

func (s *SafeDescriptorArray) Get(queueID, tag int) (*UblksrvIoDesc, error) {
    if queueID >= s.queues || tag >= s.depth {
        return nil, fmt.Errorf("invalid queue/tag: %d/%d", queueID, tag)
    }
    
    return s.raw.Get(queueID, tag, s.depth), nil
}
```

### Cleanup and Resource Management
```go
type MemoryManager struct {
    descriptors *DescriptorArray
    buffers     *BufferPool
    rings       []*Ring
}

func (m *MemoryManager) Cleanup() {
    // Clean up in reverse order of allocation
    for _, ring := range m.rings {
        ring.Close()
    }
    
    if m.buffers != nil {
        m.buffers.Destroy()
    }
    
    if m.descriptors != nil {
        m.descriptors.Unmap()
    }
}

// Use defer for cleanup
func processDevice() error {
    mm := &MemoryManager{}
    defer mm.Cleanup()
    
    // Setup and use memory
    // ...
    
    return nil
}
```

## Performance Optimizations

### CPU Cache Considerations
```go
// Align structures to cache lines
type CacheAlignedDesc struct {
    desc UblksrvIoDesc
    _    [64 - unsafe.Sizeof(UblksrvIoDesc{})]byte // Padding
}

// Batch operations to improve cache usage
func batchProcessDescriptors(descs []*UblksrvIoDesc) {
    // Process in batches that fit in L1/L2 cache
    const batchSize = 64
    
    for i := 0; i < len(descs); i += batchSize {
        end := min(i+batchSize, len(descs))
        processBatch(descs[i:end])
    }
}
```

### NUMA Awareness
```go
// Allocate memory on specific NUMA node
func allocateOnNUMANode(size int, node int) ([]byte, error) {
    // This requires platform-specific syscalls
    // Example for Linux:
    return syscallNumaAlloc(size, node)
}

// Bind memory to CPU
func bindMemoryToCPU(mem []byte, cpu int) error {
    // Set memory policy for NUMA optimization
    return setMemoryAffinity(mem, cpu)
}
```

## Memory Debugging

### Tracking Allocations
```go
type MemoryTracker struct {
    allocated map[uintptr]int
    mu        sync.Mutex
}

func (t *MemoryTracker) Track(ptr unsafe.Pointer, size int) {
    t.mu.Lock()
    defer t.mu.Unlock()
    t.allocated[uintptr(ptr)] = size
}

func (t *MemoryTracker) Untrack(ptr unsafe.Pointer) {
    t.mu.Lock()
    defer t.mu.Unlock()
    delete(t.allocated, uintptr(ptr))
}

func (t *MemoryTracker) Report() {
    t.mu.Lock()
    defer t.mu.Unlock()
    
    total := 0
    for _, size := range t.allocated {
        total += size
    }
    
    fmt.Printf("Total allocated: %d bytes in %d allocations\n", 
               total, len(t.allocated))
}
```

## Best Practices

1. **Pre-allocate buffers**: Avoid allocation in hot path
2. **Use buffer pools**: Reuse memory to reduce GC pressure
3. **Pin critical buffers**: Prevent GC movement for kernel-shared memory
4. **Check RLIMIT_MEMLOCK**: Ensure sufficient locked memory
5. **Align memory**: Use page-aligned buffers for better performance
6. **Clean up properly**: Always unmap and free resources
7. **Monitor memory usage**: Track allocations in development
8. **Handle errors gracefully**: Check all memory operations

## Common Pitfalls

1. **GC moving memory**: Kernel has stale pointers
2. **Insufficient RLIMIT_MEMLOCK**: mmap/mlock failures
3. **Memory leaks**: Not unmapping or freeing buffers
4. **Alignment issues**: Unaligned access causing crashes
5. **Race conditions**: Concurrent access to shared memory
6. **Buffer overflow**: Writing beyond allocated size

## Testing Memory Management

```go
func TestMemoryManagement(t *testing.T) {
    // Test allocation
    buf := allocateAligned(4096, 4096)
    assert.Equal(t, 0, int(uintptr(unsafe.Pointer(&buf[0]))%4096))
    
    // Test descriptor mapping
    da, err := mapDescriptors(fd, 4, 128)
    assert.NoError(t, err)
    defer da.Unmap()
    
    // Test buffer pool
    pool := NewBufferPool(4, 128, 65536)
    buf1 := pool.GetBuffer(0, 0, 128)
    assert.NotNil(t, buf1)
    assert.Equal(t, 65536, len(buf1))
}
```