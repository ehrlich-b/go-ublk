# Testing Strategy for go-ublk

## Overview

Comprehensive testing strategy covering unit tests, integration tests, performance benchmarks, and stress testing.

## Test Categories

### 1. Unit Tests (No Special Requirements)

**Scope**: Pure Go logic without kernel dependencies

**Coverage**:
- Backend interface implementations
- Data structure marshaling/unmarshaling
- Buffer management
- Error handling logic
- Utility functions

**Example**:
```go
// backend/mem/mem_test.go
func TestMemBackendReadWrite(t *testing.T) {
    backend := New(1024)
    data := []byte("test data")
    
    n, err := backend.WriteAt(data, 0)
    assert.NoError(t, err)
    assert.Equal(t, len(data), n)
    
    buf := make([]byte, len(data))
    n, err = backend.ReadAt(buf, 0)
    assert.NoError(t, err)
    assert.Equal(t, data, buf)
}
```

### 2. Integration Tests (Requires ublk Support)

**Scope**: Tests requiring kernel ublk support

**Build Tags**: `-tags=ublk`

**Coverage**:
- Device creation/deletion
- Control plane operations
- Basic I/O operations
- Error conditions
- Cleanup paths

**Example**:
```go
// +build ublk

func TestDeviceLifecycle(t *testing.T) {
    requireRoot(t)
    requireKernel(t, "6.1")
    
    ctx := context.Background()
    backend := mem.New(64 << 20) // 64MB
    
    device, err := ublk.CreateAndServe(ctx, Options{
        Backend: backend,
        Params: DeviceParams{
            LogicalBlockSize: 512,
            QueueDepth: 32,
            NumQueues: 1,
        },
    })
    require.NoError(t, err)
    defer device.Close()
    
    // Verify device exists
    _, err = os.Stat(device.Path)
    assert.NoError(t, err)
}
```

### 3. End-to-End Tests

**Scope**: Full system tests with filesystems

**Requirements**:
- Root access
- ublk kernel support
- Filesystem tools (mkfs, mount)

**Test Scenarios**:
```bash
#!/bin/bash
# test/e2e/filesystem_test.sh

# Create device
./ublk-mem --size=100M &
UBLK_PID=$!

# Wait for device
sleep 1

# Create filesystem
mkfs.ext4 /dev/ublkb0

# Mount and test
mount /dev/ublkb0 /mnt/test
echo "test" > /mnt/test/file.txt
cat /mnt/test/file.txt
umount /mnt/test

# Cleanup
kill $UBLK_PID
```

### 4. Performance Benchmarks

**Tools**: fio, dd, custom Go benchmarks

**Metrics**:
- IOPS (4K random read/write)
- Throughput (1M sequential read/write)
- Latency (percentiles: P50, P99, P99.9)
- CPU utilization

**fio Configuration**:
```ini
[global]
ioengine=io_uring
direct=1
runtime=30
time_based=1
group_reporting=1

[4k-random-read]
bs=4k
rw=randread
iodepth=32
numjobs=4

[4k-random-write]
bs=4k
rw=randwrite
iodepth=32
numjobs=4

[1m-sequential-read]
bs=1m
rw=read
iodepth=8
numjobs=1
```

### 5. Stress Tests

**Scenarios**:
- Long-running I/O workload
- Rapid device creation/deletion
- Queue overflow conditions
- Memory pressure
- Signal handling

**Example**:
```go
func TestStressDeviceChurn(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping stress test")
    }
    
    for i := 0; i < 100; i++ {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        
        device, err := ublk.CreateAndServe(ctx, testOptions())
        require.NoError(t, err)
        
        // Random operations
        time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
        
        err = device.Close()
        assert.NoError(t, err)
        
        cancel()
    }
}
```

## Test Environment Setup

### Local Development

```bash
# Install dependencies
sudo apt-get install -y \
    linux-headers-$(uname -r) \
    fio \
    blktrace

# Load ublk module
sudo modprobe ublk_drv

# Run tests
go test ./...
sudo go test -tags=ublk ./...
```

### CI Environment

#### GitHub Actions Workflow
```yaml
name: Test

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      - run: go test ./...

  integration-tests:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        kernel: ['6.1', '6.2', '6.5', '6.8']
    steps:
      - uses: actions/checkout@v3
      - name: Setup kernel ${{ matrix.kernel }}
        run: |
          # Use docker/VM with specific kernel
          docker run --privileged \
            -v $PWD:/workspace \
            kernel:${{ matrix.kernel }} \
            /workspace/scripts/run_tests.sh
```

### Test VMs

#### Vagrant Configuration
```ruby
Vagrant.configure("2") do |config|
  # Different kernel versions
  config.vm.define "kernel-6.1" do |vm|
    vm.box = "ubuntu/jammy64"
    vm.provision "shell", inline: <<-SHELL
      apt-get update
      apt-get install -y linux-image-6.1.0
      reboot
    SHELL
  end
  
  config.vm.define "kernel-6.8" do |vm|
    vm.box = "ubuntu/noble64"
    # Latest kernel by default
  end
end
```

## Test Data Generation

### Block Patterns
```go
// test/patterns/patterns.go

// Sequential pattern
func Sequential(size int) []byte {
    data := make([]byte, size)
    for i := range data {
        data[i] = byte(i % 256)
    }
    return data
}

// Random pattern
func Random(size int) []byte {
    data := make([]byte, size)
    rand.Read(data)
    return data
}

// Compressible pattern
func Compressible(size int) []byte {
    data := make([]byte, size)
    pattern := []byte("COMPRESSIBLE")
    for i := 0; i < size; i++ {
        data[i] = pattern[i%len(pattern)]
    }
    return data
}
```

## Debugging Tests

### Enable Debug Logging
```bash
UBLK_DEBUG=1 go test -v ./...
```

### Kernel Tracing
```bash
# Enable ublk events
echo 1 > /sys/kernel/debug/tracing/events/ublk/enable

# Run test
go test -run TestSpecific

# View trace
cat /sys/kernel/debug/tracing/trace
```

### strace Analysis
```bash
sudo strace -f -e trace=io_uring_enter,mmap,ioctl \
    ./ublk-mem --size=1G
```

## Test Coverage Goals

### Code Coverage
- Minimum: 70% overall
- Target: 85% for critical paths
- Exclude: Generated code, test utilities

### Feature Coverage
- All backend types
- All control commands
- All I/O operations
- Error conditions
- Edge cases

## Regression Testing

### Test Case Database
```yaml
# test/regression/cases.yaml
- name: "Issue #123: Panic on zero-size device"
  test: TestZeroSizeDevice
  fixed_in: v0.2.0
  
- name: "Issue #456: Memory leak in queue runner"
  test: TestQueueMemoryLeak
  fixed_in: v0.3.0
```

### Automated Regression Runs
```bash
# Run all regression tests
go test -tags=regression ./test/regression/...
```

## Performance Regression Detection

### Baseline Metrics
```json
{
  "4k_random_read_iops": 100000,
  "4k_random_write_iops": 80000,
  "1m_seq_read_mbps": 1000,
  "1m_seq_write_mbps": 800,
  "cpu_usage_percent": 20
}
```

### Continuous Benchmarking
```go
func BenchmarkCompareBaseline(b *testing.B) {
    baseline := loadBaseline()
    current := runBenchmarks()
    
    for metric, value := range current {
        if value < baseline[metric]*0.9 {
            b.Errorf("Performance regression: %s decreased by >10%%", metric)
        }
    }
}
```

## Mock/Stub Strategy

### Mock Kernel Interface
```go
// internal/mock/kernel.go
type MockKernel struct {
    devices map[int]*MockDevice
}

func (m *MockKernel) AddDev(params DeviceParams) (*DeviceInfo, error) {
    // Simulate kernel behavior
}
```

### Test Doubles
- Stub backends for unit tests
- Mock io_uring for control flow tests
- Fake filesystem for I/O tests

## Test Documentation

### Test Plan Template
```markdown
## Test: <Name>

### Purpose
Brief description

### Setup
Required configuration

### Steps
1. Step one
2. Step two

### Expected Results
- Result one
- Result two

### Cleanup
Cleanup steps
```

## Known Test Limitations

### Environment Dependencies
- Requires Linux kernel ≥ 6.1
- Root access for most tests
- Specific kernel configs
- Hardware resources (RAM, CPU)

### Flaky Tests
- Document and isolate flaky tests
- Use retry mechanisms carefully
- Investigate root causes

## Test Metrics

### Track Over Time
- Test execution time
- Pass/fail rates
- Coverage trends
- Performance metrics

### Reporting
```bash
# Generate test report
go test -json ./... | go-test-report

# Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```