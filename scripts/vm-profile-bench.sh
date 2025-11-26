#!/bin/bash
# Profiling benchmark on VM to identify bottlenecks

set -e

echo "=== go-ublk Profiling Benchmark ==="
echo "Testing on kernel: $(uname -r)"
echo ""

# Check for fio
if ! command -v fio &> /dev/null; then
    echo "Installing fio..."
    sudo apt-get update && sudo apt-get install -y fio
fi

# Start ublk device with CPU profiling
echo "Starting ublk memory device (256MB) with CPU profiling..."
sudo ./ublk-mem --size=256M --cpuprofile=/tmp/ublk-cpu.prof --memprofile=/tmp/ublk-mem.prof &
UBLK_PID=$!
sleep 3

# Verify device exists
if [ ! -b /dev/ublkb0 ]; then
    echo "Failed to create ublk device"
    sudo kill $UBLK_PID 2>/dev/null || true
    exit 1
fi

echo "Device created at /dev/ublkb0"
echo "Running 30-second fio benchmark for profiling..."
echo ""

# Run a longer benchmark to get good profile data
sudo fio --name=profile_test --filename=/dev/ublkb0 --size=256M \
    --ioengine=libaio --direct=1 --runtime=30 --time_based=1 \
    --rw=randrw --rwmixread=50 --bs=4k --iodepth=32 --numjobs=4 \
    --group_reporting

echo ""
echo "Stopping ublk device to flush profile data..."
sudo kill -SIGINT $UBLK_PID
sleep 2

# Wait for process to fully exit and write profiles
wait $UBLK_PID 2>/dev/null || true

echo ""
echo "=== Profile files created ==="
ls -la /tmp/ublk-*.prof 2>/dev/null || echo "No profile files found"
echo ""
echo "To analyze on host machine:"
echo "  scp VM:/tmp/ublk-cpu.prof ."
echo "  go tool pprof -http=:8080 ./ublk-mem ./ublk-cpu.prof"
