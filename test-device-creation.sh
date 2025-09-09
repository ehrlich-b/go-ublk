#!/bin/bash
set -e

echo "=== Testing Real Device Creation ==="

# Start device in background
echo "Starting ublk-mem with sudo..."
sudo timeout 10s ./ublk-mem --size=16M -v &
UBLK_PID=$!

echo "Waiting for device creation..."
sleep 3

echo "Checking if /dev/ublkb0 exists..."
if [ -b /dev/ublkb0 ]; then
    echo "✅ SUCCESS: /dev/ublkb0 exists and is a block device!"
    ls -la /dev/ublkb0
    
    echo "Getting device size..."
    sudo blockdev --getsize64 /dev/ublkb0 || echo "Could not get device size"
    
    echo "✅ REAL BLOCK DEVICE WORKING!"
else
    echo "❌ FAILURE: /dev/ublkb0 does not exist or is not a block device"
    echo "Device creation is NOT working"
fi

echo "Checking if /dev/ublkc0 exists..."
if [ -c /dev/ublkc0 ]; then
    echo "✅ SUCCESS: /dev/ublkc0 exists and is a character device!"
    ls -la /dev/ublkc0
else
    echo "❌ FAILURE: /dev/ublkc0 does not exist or is not a character device"
fi

# Clean up
echo "Cleaning up..."
sudo kill $UBLK_PID 2>/dev/null || true
wait $UBLK_PID 2>/dev/null || true

echo "Test completed."