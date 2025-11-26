# VM Testing Guide

This document describes how to set up and run VM-based integration tests for go-ublk.

## Why VM Testing?

ublk requires:
- Linux kernel 6.1+ with ublk support
- Root privileges or CAP_SYS_ADMIN
- The `ublk_drv` kernel module

Testing in a VM isolates these requirements from your development machine and provides a clean, reproducible environment.

## VM Requirements

### Kernel Requirements
- Linux kernel >= 6.1 (ublk was introduced in 6.0, stabilized in 6.1)
- `CONFIG_BLK_DEV_UBLK=m` or `CONFIG_BLK_DEV_UBLK=y`
- io_uring support (`CONFIG_IO_URING=y`)

### Recommended VM Setup
- Ubuntu 22.04+ or Debian 12+
- 2GB+ RAM
- 10GB+ disk space
- Network accessible from host

### Verify ublk Support
```bash
# Check if module is available
modinfo ublk_drv

# Load the module
sudo modprobe ublk_drv

# Verify control device exists
ls -la /dev/ublk-control
```

## SSH Configuration

The test infrastructure uses `sshpass` for automated SSH access.

### 1. Install sshpass on Host
```bash
# Ubuntu/Debian
sudo apt install sshpass

# macOS
brew install hudochenkov/sshpass/sshpass
```

### 2. Create Password File
Store your VM password in a file:
```bash
echo "your_vm_password" > /tmp/devvm_pwd.txt
chmod 600 /tmp/devvm_pwd.txt
```

### 3. Update Makefile (if needed)
Edit the Makefile to match your VM's IP address:
```makefile
VM_HOST=192.168.4.79  # Change to your VM's IP
VM_USER=behrlich       # Change to your VM username
```

### 4. Test SSH Connection
```bash
./scripts/vm-ssh.sh "echo 'SSH works!'"
```

## One-Time VM Setup

Run this once to configure passwordless sudo for ublk operations:
```bash
make setup-vm
```

This configures sudoers rules for:
- `modprobe ublk_drv`
- `modprobe -r ublk_drv`
- Running the ublk-mem binary

## Test Commands

### Basic Tests
```bash
make vm-simple-e2e    # Quick single I/O test (~30s)
make vm-e2e           # Full test suite (~60s)
make vm-benchmark     # Performance benchmark (~120s)
```

### Stress Tests
```bash
make vm-stress        # 10x alternating e2e + benchmark
```

### Race Detector Tests
```bash
make vm-e2e-racedetect        # Full tests with race detector
make vm-simple-e2e-racedetect # Simple test with race detector
# Or manually:
RACE=1 make vm-e2e
```

### Reliability Tests
```bash
make vm-e2e-race      # Run vm-e2e 5 times
make vm-benchmark-race # Run benchmark 5 times
```

### Debug Targets
```bash
make vm-reset         # Hard reset VM and reload ublk module
make kernel-trace     # View kernel trace buffer
make vm-hang-debug    # Debug hanging with stack traces
```

## Test Output

### Successful Test
```
🧪 Running e2e I/O test on VM...
✅ Basic read test PASSED
✅ Sequential write test PASSED
✅ MD5 integrity test PASSED
✅ Multi-block test PASSED
✅ All tests passed!
```

### Performance Baseline
Expected performance on a typical VM:
| Workload | IOPS | Throughput |
|----------|------|------------|
| 4K Random Write | ~500k | ~2.0 GB/s |
| 4K Random Read | ~480k | ~1.9 GB/s |
| 128K Sequential | ~9.5k | ~1.2 GB/s |

## Troubleshooting

### "Connection refused" or timeout
- Verify VM is running and IP is correct
- Check VM firewall allows SSH (port 22)
- Verify password file exists: `cat /tmp/devvm_pwd.txt`

### "ublk_drv module not found"
- Kernel may not have ublk support
- Try: `modinfo ublk_drv` to check availability
- May need to recompile kernel with `CONFIG_BLK_DEV_UBLK=m`

### Device creation fails
- Check dmesg on VM: `dmesg | tail -20`
- Verify /dev/ublk-control exists
- May need: `sudo modprobe ublk_drv`

### Test hangs
1. Run `make vm-reset` to clean up
2. Check for zombie processes: `pgrep -a ublk`
3. Inspect kernel trace: `make kernel-trace`
4. Debug with stack traces: `make vm-hang-debug`

### Race detector build fails
The race detector requires CGO. Ensure:
- GCC is installed on host
- Go toolchain has CGO support
- Not cross-compiling to incompatible architecture

## CI Integration

The project includes GitHub Actions CI that runs:
- Unit tests on every push/PR
- Unit tests with race detector
- Formatting and vet checks

VM tests are NOT run in CI (require real kernel). Run manually before merging significant changes.

## Adding New Tests

Test scripts are in the repository root:
- `test-e2e.sh` - Full integration test suite
- `test-benchmark.sh` - Performance benchmarks
- `scripts/vm-simple-e2e.sh` - Simple single I/O test

To add a new test:
1. Create test script (see existing scripts for patterns)
2. Add Makefile target that copies and runs script
3. Document in this file
