#!/bin/bash
# Quick benchmark on VM to measure go-ublk overhead

set -e

echo "=== go-ublk Overhead Benchmark ==="
echo "Testing on kernel: $(uname -r)"
echo ""

# Check for fio
if ! command -v fio &> /dev/null; then
    echo "Installing fio..."
    sudo apt-get update && sudo apt-get install -y fio
fi

# Start ublk device
echo "Starting ublk memory device (256MB)..."
sudo ./ublk-mem --size=256M &
UBLK_PID=$!
sleep 2

# Verify device exists
if [ ! -b /dev/ublkb0 ]; then
    echo "Failed to create ublk device"
    sudo kill $UBLK_PID 2>/dev/null || true
    exit 1
fi

echo "Device created at /dev/ublkb0"
echo ""

# Quick 4K random read test
echo "=== Testing ublk 4K Random Read (10 seconds) ==="
sudo fio --name=test --filename=/dev/ublkb0 --size=256M \
    --ioengine=libaio --direct=1 --runtime=10 --time_based=1 \
    --rw=randread --bs=4k --iodepth=1 --numjobs=1 \
    --output-format=terse --terse-version=5 | awk -F';' '{
        print "  IOPS: " $49
        print "  Avg Latency: " $40/1000 " us"
        print "  P50 Latency: " $67/1000 " us"  
        print "  P99 Latency: " $85/1000 " us"
    }'

echo ""
echo "=== Testing ublk 4K Random Read (QD=32) ==="
sudo fio --name=test --filename=/dev/ublkb0 --size=256M \
    --ioengine=libaio --direct=1 --runtime=10 --time_based=1 \
    --rw=randread --bs=4k --iodepth=32 --numjobs=1 \
    --output-format=terse --terse-version=5 | awk -F';' '{
        print "  IOPS: " $49
        print "  Avg Latency: " $40/1000 " us"
        print "  Bandwidth: " $48/1024 " MB/s"
    }'

# Stop ublk device
echo ""
echo "Stopping ublk device..."
sudo kill -SIGINT $UBLK_PID
wait $UBLK_PID 2>/dev/null || true

# Create loop device for comparison
echo ""
echo "=== Creating loop device for baseline ==="
dd if=/dev/zero of=/tmp/loop_test.img bs=1M count=256 status=none
LOOP_DEV=$(sudo losetup --find --show /tmp/loop_test.img)
echo "Loop device created at $LOOP_DEV"

echo ""
echo "=== Testing loop 4K Random Read (10 seconds) ==="
sudo fio --name=test --filename=$LOOP_DEV --size=256M \
    --ioengine=libaio --direct=1 --runtime=10 --time_based=1 \
    --rw=randread --bs=4k --iodepth=1 --numjobs=1 \
    --output-format=terse --terse-version=5 | awk -F';' '{
        print "  IOPS: " $49
        print "  Avg Latency: " $40/1000 " us"
        print "  P50 Latency: " $67/1000 " us"
        print "  P99 Latency: " $85/1000 " us"
    }'

echo ""
echo "=== Testing loop 4K Random Read (QD=32) ==="
sudo fio --name=test --filename=$LOOP_DEV --size=256M \
    --ioengine=libaio --direct=1 --runtime=10 --time_based=1 \
    --rw=randread --bs=4k --iodepth=32 --numjobs=1 \
    --output-format=terse --terse-version=5 | awk -F';' '{
        print "  IOPS: " $49
        print "  Avg Latency: " $40/1000 " us"
        print "  Bandwidth: " $48/1024 " MB/s"
    }'

# Cleanup
sudo losetup -d $LOOP_DEV
rm -f /tmp/loop_test.img

echo ""
echo "=== Benchmark Complete ==="
echo "Compare the IOPS and latency between ublk and loop device above"
echo "The difference shows the go-ublk userspace overhead"