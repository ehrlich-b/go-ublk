#!/bin/bash
# Simple VM benchmark test for go-ublk
# Runs basic fio tests to establish baseline performance

set -e

echo "=== go-ublk Baseline Performance Test ==="
echo "Testing on kernel: $(uname -r)"
echo "Date: $(date)"
echo ""

# Check if fio is available
if ! command -v fio >/dev/null 2>&1; then
    echo "Installing fio..."
    sudo apt-get update >/dev/null 2>&1
    sudo apt-get install -y fio >/dev/null 2>&1
fi

DEVICE_SIZE="256M"
DEVICE_BDEV="/dev/ublkb0"
UBLK_PID=""

cleanup() {
    echo ""
    echo "Cleaning up..."
    if [ -n "$UBLK_PID" ] && kill -0 "$UBLK_PID" 2>/dev/null; then
        sudo kill -SIGINT "$UBLK_PID" 2>/dev/null || sudo kill -9 "$UBLK_PID" 2>/dev/null || true
        sleep 2
    fi
}
trap cleanup EXIT

# Start ublk device
echo "Starting ublk memory device ($DEVICE_SIZE)..."
sudo ./ublk-mem --size="$DEVICE_SIZE" &
UBLK_PID=$!

# Wait for device to appear
echo "Waiting for device to appear..."
for i in {1..30}; do
    if [ -b "$DEVICE_BDEV" ]; then
        echo "✅ Device ready: $DEVICE_BDEV"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "❌ Device failed to appear after 30 seconds"
        exit 1
    fi
    sleep 1
done

# Give the device a moment to fully initialize
sleep 2

echo ""
echo "=== Benchmark Results ==="
echo ""

# Test 1: 4K Random Read (QD=1) - Latency focused
echo "🔍 4K Random Read (QD=1) - Latency Test"
echo "Purpose: Measures single-threaded random read latency"
sudo fio \
    --name=4k_read_qd1 \
    --filename="$DEVICE_BDEV" \
    --size="$DEVICE_SIZE" \
    --ioengine=libaio \
    --direct=1 \
    --runtime=15 \
    --time_based=1 \
    --rw=randread \
    --bs=4k \
    --iodepth=1 \
    --numjobs=1 \
    --output-format=normal \
    --group_reporting=1 \
    --lat_percentiles=1 \
    --clat_percentiles=1 \
    --percentile_list=50:95:99 \
    2>/dev/null | grep -E "(read:|lat \(|clat percentiles)"

echo ""

# Test 2: 4K Random Read (QD=32) - Throughput focused
echo "🚀 4K Random Read (QD=32) - Throughput Test"
echo "Purpose: Measures maximum random read IOPS"
sudo fio \
    --name=4k_read_qd32 \
    --filename="$DEVICE_BDEV" \
    --size="$DEVICE_SIZE" \
    --ioengine=libaio \
    --direct=1 \
    --runtime=15 \
    --time_based=1 \
    --rw=randread \
    --bs=4k \
    --iodepth=32 \
    --numjobs=1 \
    --output-format=normal \
    --group_reporting=1 \
    2>/dev/null | grep -E "(read:|IOPS=|BW=)" | head -3

echo ""

# Test 3: 4K Random Write (QD=1) - Latency focused
echo "✏️  4K Random Write (QD=1) - Latency Test"
echo "Purpose: Measures single-threaded random write latency"
sudo fio \
    --name=4k_write_qd1 \
    --filename="$DEVICE_BDEV" \
    --size="$DEVICE_SIZE" \
    --ioengine=libaio \
    --direct=1 \
    --runtime=15 \
    --time_based=1 \
    --rw=randwrite \
    --bs=4k \
    --iodepth=1 \
    --numjobs=1 \
    --output-format=normal \
    --group_reporting=1 \
    --lat_percentiles=1 \
    --clat_percentiles=1 \
    --percentile_list=50:95:99 \
    2>/dev/null | grep -E "(write:|lat \(|clat percentiles)"

echo ""

# Test 4: 128K Sequential Read - Large I/O bandwidth
echo "📊 128K Sequential Read - Bandwidth Test"
echo "Purpose: Measures large block sequential read performance"
sudo fio \
    --name=128k_seq_read \
    --filename="$DEVICE_BDEV" \
    --size="$DEVICE_SIZE" \
    --ioengine=libaio \
    --direct=1 \
    --runtime=15 \
    --time_based=1 \
    --rw=read \
    --bs=128k \
    --iodepth=4 \
    --numjobs=1 \
    --output-format=normal \
    --group_reporting=1 \
    2>/dev/null | grep -E "(read:|IOPS=|BW=)" | head -3

echo ""

# Test 5: Mixed workload (70% read, 30% write)
echo "🔄 Mixed Workload (70R/30W, 4K, QD=8)"
echo "Purpose: Simulates realistic application I/O pattern"
sudo fio \
    --name=mixed_workload \
    --filename="$DEVICE_BDEV" \
    --size="$DEVICE_SIZE" \
    --ioengine=libaio \
    --direct=1 \
    --runtime=15 \
    --time_based=1 \
    --rw=randrw \
    --rwmixread=70 \
    --bs=4k \
    --iodepth=8 \
    --numjobs=1 \
    --output-format=normal \
    --group_reporting=1 \
    2>/dev/null | grep -E "(read:|write:|IOPS=|BW=)" | head -6

echo ""
echo "=== Baseline Performance Summary ==="
echo "✅ All tests completed successfully"
echo ""
echo "Key Metrics Summary:"
echo "- 4K Random Read (QD=1):  Focus on latency numbers"
echo "- 4K Random Read (QD=32): Focus on IOPS numbers"
echo "- 4K Random Write (QD=1): Focus on latency numbers"
echo "- 128K Sequential Read:    Focus on bandwidth (MB/s)"
echo "- Mixed Workload:          Realistic application simulation"
echo ""
echo "Next steps:"
echo "1. Compare these numbers against kernel loop device baseline"
echo "2. Profile and optimize performance bottlenecks"
echo "3. Test with different queue depths and block sizes"
echo ""
echo "Note: These are UNOPTIMIZED prototype performance numbers"
echo "Significant improvements are expected through optimization"