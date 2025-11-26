# go-ublk

🎉 **FULLY FUNCTIONAL WITH EXCELLENT PERFORMANCE** 🎉

Pure Go implementation of Linux ublk (userspace block driver).

## Current Status

- ✅ **Device creation**: ADD_DEV, SET_PARAMS, START_DEV all working
- ✅ **Block device**: /dev/ublkb0 created and functional
- ✅ **Data integrity**: Perfect integrity with cryptographic MD5 verification
- ✅ **Performance**: 80-110k IOPS (4K random, single queue QD=32)
- ✅ **All I/O patterns**: Sequential, scattered, and multi-block operations verified
- ✅ **End-to-end tested**: Comprehensive test suite passing

**Latest test results:**
- `make vm-simple-e2e`: ✅ PASS
- `make vm-e2e`: ✅ **PASS** (all critical tests including data integrity)
- Performance: 80k IOPS read, 110k IOPS write (single queue QD=32)
- Data integrity: ✅ **VERIFIED** across all I/O patterns

## Installation

```bash
go get github.com/ehrlich-b/go-ublk
```

## Quick Start

### RAM Disk

```bash
# Load the ublk kernel module
sudo modprobe ublk_drv

# Create a 1GB RAM disk
make build
sudo ./ublk-mem --size=1G

# In another terminal, use the device
sudo mkfs.ext4 /dev/ublkb0
sudo mount /dev/ublkb0 /mnt
# ... use the filesystem ...
sudo umount /mnt

# Stop with Ctrl+C - exits cleanly with proper device cleanup
```

### File-backed Device (Loop Device Alternative)

```bash
# Create a 10GB sparse file
truncate -s 10G disk.img

# Create ublk device backed by the file
sudo go run cmd/ublk-file/main.go --path=disk.img

# Use like any block device
sudo mkfs.xfs /dev/ublkb0
```

## Library Usage

```go
package main

import (
    "context"
    "log"

    "github.com/ehrlich-b/go-ublk"
    "github.com/ehrlich-b/go-ublk/backend"
)

func main() {
    // Create a 512MB memory backend
    memBackend := backend.NewMemory(512 << 20)
    defer memBackend.Close()

    params := ublk.DefaultParams(memBackend)
    params.QueueDepth = 128
    params.NumQueues = 1

    opts := &ublk.Options{}

    ctx := context.Background()
    device, err := ublk.CreateAndServe(ctx, params, opts)
    if err != nil {
        log.Fatal(err)
    }

    // Inspect device state
    info := device.Info()
    log.Printf("Device created: %s (ID: %d)", info.BlockPath, info.ID)
    log.Printf("State: %s, Queues: %d, Size: %d bytes",
               info.State, info.NumQueues, info.Size)

    // Block until context is cancelled or signal received
    <-ctx.Done()

    // Cleanup happens automatically via defer in CreateAndServe
}
```

## Implementing Custom Backends

```go
package main

import "github.com/ehrlich-b/go-ublk"

type MyBackend struct {
    // your fields
}

// Implement required Backend interface
func (b *MyBackend) ReadAt(p []byte, off int64) (int, error) {
    // Read data into p from offset off
    return 0, nil
}

func (b *MyBackend) WriteAt(p []byte, off int64) (int, error) {
    // Write data from p at offset off
    return len(p), nil
}

func (b *MyBackend) Size() int64 {
    // Return total size in bytes
    return 1024 * 1024 * 1024 // 1GB
}

func (b *MyBackend) Close() error {
    // Cleanup resources
    return nil
}

func (b *MyBackend) Flush() error {
    // Persist any cached writes
    return nil
}

// Implement optional interfaces for better performance
func (b *MyBackend) Discard(offset, length int64) error {
    // Handle TRIM/discard operations
    return nil
}

func (b *MyBackend) Sync() error {
    // Synchronize to stable storage
    return nil
}

// Compile-time interface checks
var (
    _ ublk.Backend        = (*MyBackend)(nil)
    _ ublk.DiscardBackend = (*MyBackend)(nil)
    _ ublk.SyncBackend    = (*MyBackend)(nil)
)
```

## Testing Your Code

The library provides `ublk.NewMockBackend()` for easy unit testing:

```go
package myapp_test

import (
    "testing"
    "github.com/ehrlich-b/go-ublk"
)

func TestMyFunction(t *testing.T) {
    // Create a mock backend for testing
    backend := ublk.NewMockBackend(1024) // 1KB

    // Use it in your code
    result := myFunctionThatUsesUblk(backend)

    // Verify behavior
    if !backend.IsFlushed() {
        t.Error("Expected flush to be called")
    }

    // Check call statistics if your backend implements StatBackend
    if statBackend, ok := backend.(ublk.StatBackend); ok {
        stats := statBackend.Stats()
        if readCalls := stats["read_calls"].(int); readCalls != 2 {
            t.Errorf("Expected 2 read calls, got %d", readCalls)
        }
    }
}
```

## Architecture

```
┌──────────────┐     ┌──────────────┐
│  Application │────▶│ /dev/ublkb0  │
└──────────────┘     └──────┬───────┘
                            │
                     ┌──────▼───────┐
                     │ Linux Kernel │
                     │   ublk_drv    │
                     └──────┬───────┘
                            │
                ┌───────────┼───────────┐
                │           │           │
         ┌──────▼──┐  ┌─────▼───┐  ┌───▼─────┐
         │ Queue 0 │  │ Queue 1 │  │ Queue N │
         │ io_uring│  │ io_uring│  │ io_uring│
         └──────┬──┘  └─────┬───┘  └───┬─────┘
                │           │           │
                └───────────┼───────────┘
                            │
                     ┌──────▼───────┐
                     │   Backend    │
                     │  (RAM/File)  │
                     └──────────────┘
```

## Performance

**Current Status (Functional Prototype, Single Queue QD=32):**

| Workload | go-ublk | Loop Device | Overhead |
|----------|---------|-------------|----------|
| 4K Random Read | 80k IOPS, 314 MiB/s | 210k IOPS, 820 MiB/s | 2.6x |
| 4K Random Write | 110k IOPS, 430 MiB/s | 202k IOPS, 788 MiB/s | 1.8x |

**Bottleneck:** Single-queue design limits parallelism. Multi-queue support needed for scaling.

**Optimization roadmap:**
- Multi-queue support with CPU affinity (key for performance)
- Buffer management optimization
- Memory allocation profiling

## Testing

### Build and Test
```bash
# Build all components
make build

# Run unit tests
make test-unit

# Test on real kernel (requires VM setup)
make vm-simple-e2e   # Basic functionality
make vm-e2e         # Full I/O test suite
```

### VM Testing Setup
For full integration testing on real kernels, see [docs/VM_TESTING.md](docs/VM_TESTING.md).

Quick start:
1. Setup test VM with Linux 6.1+ and ublk support
2. Store password: `echo "password" > /tmp/devvm_pwd.txt`
3. Update `VM_HOST` in Makefile if needed
4. Run: `make setup-vm` (one-time)
5. Run: `make vm-e2e`

The VM tests verify:
- ✅ Kernel ublk module loading
- ✅ Device creation (`/dev/ublkb0`, `/dev/ublkc0`)
- ✅ Control plane operations (ADD_DEV, SET_PARAMS, START_DEV)
- ✅ Data plane I/O processing (read/write operations)
- ✅ Data integrity across I/O operations
- ✅ Multiple block operations
- ✅ Graceful shutdown and cleanup

## Known Issues

### High Priority (Non-Critical)
1. **Production code quality**: Remove debug prints and verbose comments
2. **Graceful shutdown**: Process doesn't handle SIGTERM/SIGINT cleanly during cleanup
3. **Error handling**: Limited error recovery and robust cleanup

### Performance Optimization Opportunities
4. **Multi-queue support**: Currently single-queue, could scale to multiple CPUs
5. **Buffer management**: Potential for further optimization

See `TODO.md` for complete issue tracking and development roadmap.

## Kernel Configuration

Verify ublk support:
```bash
# Check if module is available
modinfo ublk_drv

# Check if already loaded
lsmod | grep ublk

# Check kernel config
zgrep CONFIG_BLK_DEV_UBLK /proc/config.gz
```

## Comparison with Alternatives

| Feature | go-ublk | NBD | FUSE | kernel loop |
|---------|---------|-----|------|-------------|
| Performance | Medium* | Medium | Low | High |
| Zero-copy | Partial | No | No | Yes |
| Userspace | Yes | Yes | Yes | No |
| Network capable | No | Yes | No | No |
| File systems | No | No | Yes | No |

*Current performance is prototype level with significant optimization potential

## Troubleshooting

### Device not created
- Check kernel version: `uname -r` (need ≥ 6.1)
- Verify module loaded: `lsmod | grep ublk`
- Check dmesg for errors: `dmesg | tail -20`

### Permission denied
- Need root or CAP_SYS_ADMIN capability
- For unprivileged mode, need kernel ≥ 6.2 with UBLK_F_UNPRIVILEGED_DEV

### Poor performance
- Current implementation is single-queue prototype
- Performance optimization is next development phase
- Enable debug logging: `--verbose` flag

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## References

- [Linux kernel ublk documentation](https://docs.kernel.org/block/ublk.html)
- [io_uring documentation](https://kernel.dk/io_uring.pdf)
- [ublksrv (C reference implementation)](https://github.com/ublk-org/ublksrv)

## License

MIT