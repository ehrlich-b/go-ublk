#!/bin/bash

# VM Testing Script for go-ublk
# Run this script ON the VM after copying the binary

set -e

echo "=== go-ublk VM Test Script ==="
echo ""

echo "1. Checking system requirements..."
echo "Kernel version: $(uname -r)"
echo ""

echo "2. Checking ublk module availability..."
if modinfo ublk_drv >/dev/null 2>&1; then
    echo "✓ ublk_drv module available"
    modinfo ublk_drv | head -3
else
    echo "✗ ublk_drv module not available"
    exit 1
fi
echo ""

echo "3. Loading ublk_drv module..."
if sudo modprobe ublk_drv; then
    echo "✓ ublk_drv module loaded"
else
    echo "✗ Failed to load ublk_drv module"
    exit 1
fi
echo ""

echo "4. Checking /dev/ublk-control..."
if [ -e /dev/ublk-control ]; then
    echo "✓ /dev/ublk-control exists"
    ls -la /dev/ublk-control
else
    echo "✗ /dev/ublk-control not found"
    exit 1
fi
echo ""

echo "5. Testing ublk-mem binary..."
if [ ! -f "./ublk-mem" ]; then
    echo "✗ ublk-mem binary not found in current directory"
    echo "Make sure you've copied the binary to the VM first"
    exit 1
fi

echo "✓ ublk-mem binary found"
echo ""

echo "6. Running ublk-mem test (will timeout after 10 seconds)..."
echo "Command: sudo timeout 10s ./ublk-mem --size=64M -v"
echo "Expected: Should create device, show paths, then timeout"
echo ""

# Run with timeout to prevent hanging
if timeout 10s sudo ./ublk-mem --size=64M -v; then
    echo ""
    echo "✓ Test completed successfully!"
else
    exit_code=$?
    if [ $exit_code -eq 124 ]; then
        echo ""
        echo "✓ Test timed out as expected (device was running)"
    else
        echo ""
        echo "✗ Test failed with exit code $exit_code"
        exit 1
    fi
fi

echo ""
echo "=== Test Results ==="
echo "If you saw device creation messages and paths like:"
echo "  Device created: /dev/ublkb0"
echo "  Character device: /dev/ublkc0"
echo "Then the control plane is working!"
echo ""
echo "Next steps:"
echo "  - Check if /dev/ublkb0 exists: ls -la /dev/ublkb*"
echo "  - Try basic filesystem test: sudo mkfs.ext4 /dev/ublkb0"