# go-ublk

Pure Go implementation of Linux ublk (userspace block driver) - create high-performance block devices in userspace without kernel modules.

## What is ublk?

ublk is a Linux kernel framework (introduced in 6.1) that allows userspace programs to implement block devices. Unlike NBD or FUSE, ublk uses io_uring for high-performance async I/O, making it suitable for production storage systems.

## Features

- **Pure Go** - No cgo dependencies
- **High Performance** - Uses io_uring for zero-copy I/O operations
- **Multiple Backends** - RAM, file, null, and read-only zip backends included
- **Production Ready** - Proper error handling, graceful shutdown, signal handling
- **Flexible** - Simple Backend interface for custom implementations

## Requirements

- Linux kernel ≥ 6.1 (ublk support)
- Linux kernel ≥ 5.19 recommended (better io_uring features)
- Root privileges (or CAP_SYS_ADMIN) for device creation
- io_uring support in kernel

## Status

**⚠️ CRITICAL CORRECTION: Data plane not implemented**

- ✅ **Phase 1-2 Complete**: Foundation and Control Plane implemented
- ❌ **Phase 3 Incomplete**: Data plane I/O processing is stubbed (not functional)
- ✅ **Control Plane Working**: Device creation/deletion works on Linux 6.11
- ⚠️ **Pre-Alpha Status**: Core I/O functionality not yet implemented

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

# Stop with Ctrl+C - device will be cleaned up automatically
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
    "github.com/ehrlich-b/go-ublk/backend/mem"
)

func main() {
    // Create a 512MB memory backend
    backend := mem.New(512 << 20)
    
    params := ublk.DeviceParams{
        Backend:          backend,
        LogicalBlockSize: 512,
        QueueDepth:       128,
        NumQueues:        1,
    }
    
    opts := &ublk.Options{
        Logger: log.Default(),
    }
    
    ctx := context.Background()
    device, err := ublk.CreateAndServe(ctx, params, opts)
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Device created: %s", device.BlockPath())
    log.Printf("Character device: %s", device.CharPath())
    
    // Block until context is cancelled or signal received
    <-ctx.Done()
    
    // Cleanup happens automatically via defer in CreateAndServe
}
```

## Implementing Custom Backends

```go
type MyBackend struct {
    // your fields
}

func (b *MyBackend) ReadAt(p []byte, off int64) (int, error) {
    // Read data into p from offset off
}

func (b *MyBackend) WriteAt(p []byte, off int64) (int, error) {
    // Write data from p at offset off
}

func (b *MyBackend) Flush() error {
    // Persist any cached writes
}

func (b *MyBackend) Trim(off, length int64) error {
    // Optional: handle discard/trim
    return nil
}

func (b *MyBackend) Size() int64 {
    // Return total size in bytes
}

func (b *MyBackend) Close() error {
    // Cleanup resources
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

❌ **INVALID CLAIMS RETRACTED**: Previous performance benchmarks were impossible due to non-functional data plane.

**Current Status**: 
- Data plane I/O processing is stubbed with `sched_yield` only
- No actual I/O operations are processed
- Performance testing must wait until core functionality is implemented
- Previous results were physically impossible and have been removed

## Testing

### Build and Test
```bash
# Build all components
make build

# Run unit tests  
make test-unit

# Test on real kernel (requires VM setup)
make test-vm
```

### VM Testing Setup
For full integration testing on real kernels:

1. **Setup test VM** with Linux 6.1+ and ublk support
2. **Configure SSH access** with password in `/tmp/devvm_pwd.txt`
3. **Update VM IP** in Makefile if different from `192.168.4.79`
4. **Run automated tests**: `make test-vm`

The VM tests verify:
- ✅ Kernel ublk module loading
- ✅ Device creation (`/dev/ublkb0`, `/dev/ublkc0`)
- ✅ Control plane operations (ADD_DEV, SET_PARAMS, START_DEV)
- ✅ Queue runner initialization
- ✅ Graceful shutdown and cleanup

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
| Performance | TBD* | Medium | Low | High |
| Zero-copy | TBD* | No | No | Yes |
| Userspace | Yes | Yes | Yes | No |
| Network capable | No | Yes | No | No |
| File systems | No | No | Yes | No |

*Performance characteristics unknown - data plane not yet implemented

## Troubleshooting

### Device not created
- Check kernel version: `uname -r` (need ≥ 6.1)
- Verify module loaded: `lsmod | grep ublk`
- Check dmesg for errors: `dmesg | tail -20`

### Permission denied
- Need root or CAP_SYS_ADMIN capability
- For unprivileged mode, need kernel ≥ 6.2 with UBLK_F_UNPRIVILEGED_DEV

### Poor performance
- Enable CPU affinity: `--cpu-affinity`
- Increase queue depth: `--qdepth=256`
- Use O_DIRECT for file backend: `--direct`

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## References

- [Linux kernel ublk documentation](https://docs.kernel.org/block/ublk.html)
- [io_uring documentation](https://kernel.dk/io_uring.pdf)
- [ublksrv (C reference implementation)](https://github.com/ublk-org/ublksrv)

## License

MIT