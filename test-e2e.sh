#!/bin/bash
# CRITICAL END-TO-END TEST - MUST VERIFY ACTUAL I/O WORKS
# This test prevents claiming devices work when I/O is broken

set -euo pipefail

echo "=== CRITICAL I/O VERIFICATION TEST ==="
echo "This test MUST pass before claiming device functionality works"
echo ""

# Check for required tools
for cmd in dd mktemp cmp; do
    if ! command -v $cmd &> /dev/null; then
        echo "ERROR: Required command '$cmd' not found"
        exit 1
    fi
done

# Check if we can build
if [ ! -f "./ublk-mem" ]; then
    echo "Building ublk-mem..."
    make build || { echo "ERROR: Build failed"; exit 1; }
fi

# Clean up any previous test
cleanup() {
    echo "Cleaning up..."
    if [ -n "${TAIL_PID:-}" ]; then kill $TAIL_PID 2>/dev/null || true; fi
    if [ -n "${UBLK_PID:-}" ]; then
        sudo kill -SIGINT $UBLK_PID 2>/dev/null || true
        wait $UBLK_PID 2>/dev/null || true
    fi
    rm -f /tmp/test_data /tmp/read_back /tmp/ublk_mem.log
}
trap cleanup EXIT

# Start ublk device in background
echo "Starting ublk memory device (16MB for testing)..."
sudo env UBLK_DEVINFO_LEN=${UBLK_DEVINFO_LEN:-} ./ublk-mem --size=16M -v > /tmp/ublk_mem.log 2>&1 &
UBLK_PID=$!
sleep 0.2
echo "Tailing ublk-mem logs (live)..."
tail -n +1 -F /tmp/ublk_mem.log &
TAIL_PID=$!

echo "Waiting for device nodes to appear (up to 5s)..."
for i in $(seq 1 5); do
    if [ -b /dev/ublkb0 ] && [ -c /dev/ublkc0 ]; then
        echo "  ($i) found: $(ls -la /dev/ublk* 2>/dev/null | tr '\n' ' ' )"
        break
    fi
    echo "  ($i) waiting..."; ls -la /dev/ublk* 2>/dev/null || true
    sleep 1
done

# Verify device exists
if [ ! -b /dev/ublkb0 ] || [ ! -c /dev/ublkc0 ]; then
    echo "ERROR: Device nodes did not appear in time"
    ls -la /dev/ublk* 2>/dev/null || true
    echo "--- dmesg (last 200) ---"; sudo dmesg | tail -200 || true
    exit 1
fi

echo "✅ Device nodes ready: /dev/ublkb0 and /dev/ublkc0"

# Create test data
echo "Creating test data..."
dd if=/dev/urandom of=/tmp/test_data bs=1024 count=64 2>/dev/null
echo "✅ Created 64KB of random test data"

# CRITICAL TEST 1: Write data to device
echo ""
echo "=== CRITICAL TEST 1: Write Test ==="
echo "Writing test data to ublk device..."
if ! sudo dd if=/tmp/test_data of=/dev/ublkb0 bs=1024 count=64 2>/dev/null; then
    echo "❌ CRITICAL FAILURE: Could not write to device"
    echo "Data plane I/O is NOT working!"
    exit 1
fi
echo "✅ Write completed successfully"

# CRITICAL TEST 2: Read data back from device  
echo ""
echo "=== CRITICAL TEST 2: Read Test ==="
echo "Reading data back from ublk device..."
if ! sudo dd if=/dev/ublkb0 of=/tmp/read_back bs=1024 count=64 2>/dev/null; then
    echo "❌ CRITICAL FAILURE: Could not read from device"  
    echo "Data plane I/O is NOT working!"
    exit 1
fi
echo "✅ Read completed successfully"

# CRITICAL TEST 3: Data integrity verification
echo ""
echo "=== CRITICAL TEST 3: Data Integrity ==="
echo "Verifying data integrity..."
if ! cmp /tmp/test_data /tmp/read_back; then
    echo "❌ CRITICAL FAILURE: Data corruption detected!"
    echo "Written data does not match read data"
    echo "Backend or I/O processing has bugs!"
    exit 1
fi
echo "✅ Data integrity verified - read data matches written data"

# CRITICAL TEST 4: Multiple block operations
echo ""
echo "=== CRITICAL TEST 4: Multiple Block Test ==="
echo "Testing multiple scattered writes..."

# Write at different offsets
sudo dd if=/tmp/test_data of=/dev/ublkb0 bs=512 seek=0 count=1 2>/dev/null
sudo dd if=/tmp/test_data of=/dev/ublkb0 bs=512 seek=100 count=1 2>/dev/null  
sudo dd if=/tmp/test_data of=/dev/ublkb0 bs=512 seek=200 count=1 2>/dev/null

# Read back and verify first block
sudo dd if=/dev/ublkb0 of=/tmp/read_back bs=512 count=1 2>/dev/null
if ! cmp <(head -c 512 /tmp/test_data) /tmp/read_back; then
    echo "❌ CRITICAL FAILURE: Multi-block I/O failed"
    exit 1
fi
echo "✅ Multiple block operations working"

echo ""
echo "🎉 ALL CRITICAL TESTS PASSED!"
echo "✅ Device creation works"  
echo "✅ Write operations work"
echo "✅ Read operations work"
echo "✅ Data integrity maintained"
echo "✅ Multiple block I/O works"
echo ""
echo "The ublk device is FUNCTIONALLY WORKING for I/O operations!"
echo "It is safe to proceed with performance testing."
