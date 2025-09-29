#!/bin/bash
set -e

echo "=== HANG DEBUG TEST ==="
echo "This test will trigger the hang and capture stack traces"

# Start ublk-mem in background
echo "Starting ublk-mem..."
sudo ./ublk-mem --size=256M -v &
UBLK_PID=$!
echo "ublk-mem PID: $UBLK_PID"

# Wait for device to appear
echo "Waiting for device..."
for i in {1..30}; do
    if [ -b /dev/ublkb0 ]; then
        echo "✅ Device ready: /dev/ublkb0"
        break
    fi
    sleep 0.5
done

if [ ! -b /dev/ublkb0 ]; then
    echo "❌ Device did not appear"
    sudo kill $UBLK_PID 2>/dev/null || true
    exit 1
fi

# Start fio in background (this will hang)
echo "Starting fio with QD=4 (will trigger hang)..."
timeout 10 sudo fio --name=test --filename=/dev/ublkb0 --size=256M \
    --ioengine=libaio --direct=1 --runtime=5 --time_based=1 \
    --rw=randread --bs=4k --iodepth=4 --numjobs=1 &
FIO_PID=$!
echo "fio PID: $FIO_PID"

# Wait a bit for the hang to manifest
sleep 3

# Check if processes are still running
if ps -p $UBLK_PID > /dev/null 2>&1; then
    echo "ublk-mem still running (expected)"
else
    echo "❌ ublk-mem crashed!"
    exit 1
fi

if ps -p $FIO_PID > /dev/null 2>&1; then
    echo "fio still running (expected if hung)"

    # Send SIGUSR1 to dump stacks
    echo "Sending SIGUSR1 to ublk-mem to dump stacks..."
    sudo kill -USR1 $UBLK_PID
    sleep 1

    # Try to find the stack trace file
    echo "Looking for stack trace file..."
    ls -la ublk-stacks-*.txt 2>/dev/null || echo "No stack trace file found (might be owned by root)"
    sudo ls -la /root/ublk-stacks-*.txt 2>/dev/null || true

    # Get stack trace from stderr/syslog
    echo ""
    echo "=== Checking dmesg for stack traces ==="
    sudo dmesg | tail -50 | grep -A 50 "GOROUTINE" || echo "No goroutine traces in dmesg"
else
    echo "fio already exited"
fi

# Cleanup
echo "Cleaning up..."
sudo pkill -9 fio 2>/dev/null || true
sudo kill -INT $UBLK_PID 2>/dev/null || true
sleep 2
sudo pkill -9 ublk-mem 2>/dev/null || true

echo "=== Test complete ==="