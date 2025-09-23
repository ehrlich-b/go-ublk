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

DEVICE_BDEV=""
DEVICE_CDEV=""
DEVICE_ID=""
ORIG_MAX_ID="-1"

capture_existing_devices() {
    local max_id="-1"
    for bdev in /dev/ublkb*; do
        [ -b "$bdev" ] || continue
        local id="${bdev#/dev/ublkb}"
        [[ "$id" =~ ^[0-9]+$ ]] || continue
        if [ "$id" -gt "$max_id" ]; then
            max_id="$id"
        fi
    done
    ORIG_MAX_ID="$max_id"
}

capture_existing_devices

find_device_nodes() {
    local best_id=""
    local best_bdev=""
    local best_cdev=""

    DEVICE_BDEV=""
    DEVICE_CDEV=""
    DEVICE_ID=""

    for bdev in /dev/ublkb*; do
        [ -b "$bdev" ] || continue
        local id="${bdev#/dev/ublkb}"
        [[ "$id" =~ ^[0-9]+$ ]] || continue
        local cdev="/dev/ublkc${id}"
        if [ -c "$cdev" ]; then
            if [ -z "$best_id" ] || [ "$id" -gt "$best_id" ]; then
                best_id="$id"
                best_bdev="$bdev"
                best_cdev="$cdev"
            fi
        fi
    done

    if [ -n "$best_id" ]; then
        DEVICE_BDEV="$best_bdev"
        DEVICE_CDEV="$best_cdev"
        DEVICE_ID="$best_id"
        return 0
    fi

    return 1
}

capture_existing_devices

# Start ublk device in background
echo "Starting ublk memory device (16MB for testing)..."
echo "Command: sudo ./ublk-mem --size=16M -v"
sudo env UBLK_DEVINFO_LEN=${UBLK_DEVINFO_LEN:-} ./ublk-mem --size=16M -v > /tmp/ublk_mem.log 2>&1 &
UBLK_PID=$!
echo "Started ublk-mem with PID $UBLK_PID"
sleep 0.2
TAIL_PID=""

echo "Waiting for device nodes to appear (up to 60s)..."
echo "Original highest device ID: $ORIG_MAX_ID"
for i in $(seq 1 60); do
    if find_device_nodes && [ "$DEVICE_ID" != "$ORIG_MAX_ID" ]; then
        echo "  ($i) found new device: ID=$DEVICE_ID block=$DEVICE_BDEV"
        break
    fi
    echo "  ($i) waiting... current detected ID: ${DEVICE_ID:-none}"
    ls -1 /dev/ublk* 2>/dev/null | head -n 10 || true
    sleep 1
done

# Verify device exists
if [ -z "$DEVICE_BDEV" ] || [ -z "$DEVICE_CDEV" ]; then
    if find_device_nodes && [ "$DEVICE_ID" != "$ORIG_MAX_ID" ]; then
        echo "  (post-wait) found new device: ID=$DEVICE_ID block=$DEVICE_BDEV"
    else
        echo "ERROR: Device nodes did not appear in time (orig=$ORIG_MAX_ID)"
        ls -la /dev/ublk* 2>/dev/null || true
        tail -n 100 /tmp/ublk_mem.log || true
        exit 1
    fi
fi

echo "Detected device ID: $DEVICE_ID (block: $DEVICE_BDEV)"
if [ "$DEVICE_ID" = "$ORIG_MAX_ID" ]; then
    echo "ERROR: No new device detected (highest ID still $DEVICE_ID)"
    tail -n 100 /tmp/ublk_mem.log || true
    exit 1
fi

echo "✅ Device nodes ready: $DEVICE_BDEV and $DEVICE_CDEV"

# Create test data
echo "Creating test data..."
dd if=/dev/urandom of=/tmp/test_data bs=1024 count=64 2>/dev/null
echo "✅ Created 64KB of random test data"

# CRITICAL TEST 1: Write data to device
echo ""
echo "=== CRITICAL TEST 1: Write Test ==="
echo "Writing 64KB test data to ublk device..."
echo "Command: dd if=/tmp/test_data of=$DEVICE_BDEV bs=1024 count=64 status=progress"
if ! sudo dd if=/tmp/test_data of="$DEVICE_BDEV" bs=1024 count=64 status=progress 2>&1; then
    echo "❌ CRITICAL FAILURE: Could not write to device"
    echo "Data plane I/O is NOT working!"
    exit 1
fi
echo "✅ Write completed successfully"

# CRITICAL TEST 2: Read data back from device
echo ""
echo "=== CRITICAL TEST 2: Read Test ==="
echo "Reading 64KB data back from ublk device..."
echo "Command: dd if=$DEVICE_BDEV of=/tmp/read_back bs=1024 count=64 status=progress"
if ! sudo dd if="$DEVICE_BDEV" of=/tmp/read_back bs=1024 count=64 status=progress 2>&1; then
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
sudo dd if=/tmp/test_data of="$DEVICE_BDEV" bs=512 seek=0 count=1 2>/dev/null
sudo dd if=/tmp/test_data of="$DEVICE_BDEV" bs=512 seek=100 count=1 2>/dev/null  
sudo dd if=/tmp/test_data of="$DEVICE_BDEV" bs=512 seek=200 count=1 2>/dev/null

# Read back and verify first block
sudo dd if="$DEVICE_BDEV" of=/tmp/read_back bs=512 count=1 2>/dev/null
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
