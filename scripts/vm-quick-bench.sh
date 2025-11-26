#!/bin/bash
# Quick benchmark on VM to measure go-ublk overhead

set -e

echo "=== go-ublk Overhead Benchmark ==="
echo "Testing on kernel: $(uname -r)"
echo ""

# Check for fio and jq
if ! command -v fio &> /dev/null; then
    echo "Installing fio..."
    sudo apt-get update && sudo apt-get install -y fio
fi
if ! command -v jq &> /dev/null; then
    echo "Installing jq..."
    sudo apt-get update && sudo apt-get install -y jq
fi

# Helper function to run fio and extract metrics
run_fio_test() {
    local device=$1
    local rw=$2
    local iodepth=$3
    local name=$4

    echo "=== $name ==="

    local output=$(sudo fio --name=test --filename=$device --size=256M \
        --ioengine=libaio --direct=1 --runtime=10 --time_based=1 \
        --rw=$rw --bs=4k --iodepth=$iodepth --numjobs=1 \
        --output-format=json 2>/dev/null)

    if [ "$rw" = "randread" ] || [ "$rw" = "read" ]; then
        local iops=$(echo "$output" | jq -r '.jobs[0].read.iops')
        local bw=$(echo "$output" | jq -r '.jobs[0].read.bw')
        local lat_mean=$(echo "$output" | jq -r '.jobs[0].read.clat_ns.mean')
        local lat_p50=$(echo "$output" | jq -r '.jobs[0].read.clat_ns.percentile["50.000000"]')
        local lat_p99=$(echo "$output" | jq -r '.jobs[0].read.clat_ns.percentile["99.000000"]')
    else
        local iops=$(echo "$output" | jq -r '.jobs[0].write.iops')
        local bw=$(echo "$output" | jq -r '.jobs[0].write.bw')
        local lat_mean=$(echo "$output" | jq -r '.jobs[0].write.clat_ns.mean')
        local lat_p50=$(echo "$output" | jq -r '.jobs[0].write.clat_ns.percentile["50.000000"]')
        local lat_p99=$(echo "$output" | jq -r '.jobs[0].write.clat_ns.percentile["99.000000"]')
    fi

    # Convert values
    local iops_k=$(echo "scale=1; $iops / 1000" | bc)
    local bw_mbs=$(echo "scale=1; $bw / 1024" | bc)
    local lat_mean_us=$(echo "scale=1; $lat_mean / 1000" | bc)
    local lat_p50_us=$(echo "scale=1; $lat_p50 / 1000" | bc)
    local lat_p99_us=$(echo "scale=1; $lat_p99 / 1000" | bc)

    echo "  IOPS:        ${iops_k}k"
    echo "  Bandwidth:   ${bw_mbs} MiB/s"
    echo "  Avg Latency: ${lat_mean_us} us"
    echo "  P50 Latency: ${lat_p50_us} us"
    echo "  P99 Latency: ${lat_p99_us} us"
    echo ""
}

# Start ublk device
# Using multi-queue with depth=64 for optimal performance
echo "Starting ublk memory device (256MB, multi-queue, depth=64)..."
sudo ./ublk-mem --size=256M --depth=64 &
UBLK_PID=$!
sleep 3

# Verify device exists
if [ ! -b /dev/ublkb0 ]; then
    echo "Failed to create ublk device"
    sudo kill $UBLK_PID 2>/dev/null || true
    exit 1
fi

echo "Device created at /dev/ublkb0"
echo ""

# Run ublk tests with QD=64 to utilize full queue depth
run_fio_test /dev/ublkb0 randread 1 "ublk 4K Random Read (QD=1)"
run_fio_test /dev/ublkb0 randread 64 "ublk 4K Random Read (QD=64)"
run_fio_test /dev/ublkb0 randwrite 64 "ublk 4K Random Write (QD=64)"

# Stop ublk device
echo "Stopping ublk device..."
sudo kill -SIGINT $UBLK_PID
wait $UBLK_PID 2>/dev/null || true
sleep 1

# Create RAM-backed loop device for fair comparison
echo ""
echo "=== Creating RAM-backed loop device for baseline ==="
sudo mkdir -p /tmp/ramdisk
sudo mount -t tmpfs -o size=300M tmpfs /tmp/ramdisk 2>/dev/null || true
dd if=/dev/zero of=/tmp/ramdisk/loop_test.img bs=1M count=256 status=none
LOOP_DEV=$(sudo losetup --find --show /tmp/ramdisk/loop_test.img)
echo "Loop device created at $LOOP_DEV (RAM-backed)"
echo ""

# Run loop tests with QD=64 for fair comparison
run_fio_test $LOOP_DEV randread 1 "loop 4K Random Read (QD=1)"
run_fio_test $LOOP_DEV randread 64 "loop 4K Random Read (QD=64)"
run_fio_test $LOOP_DEV randwrite 64 "loop 4K Random Write (QD=64)"

# Cleanup
sudo losetup -d $LOOP_DEV
sudo umount /tmp/ramdisk 2>/dev/null || true

echo "=== Benchmark Complete ==="
echo "Compare the IOPS and latency between ublk and loop device above."
echo "The difference shows the go-ublk userspace overhead."
