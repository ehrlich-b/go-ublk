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
        sleep 2
        # Check if it's still running and force kill if needed
        if kill -0 $UBLK_PID 2>/dev/null; then
            echo "ublk-mem didn't exit cleanly, force killing..."
            sudo kill -9 $UBLK_PID 2>/dev/null || true
        fi
    fi
    sudo rm -f /tmp/test_data /tmp/read_back /tmp/ublk_mem.log /tmp/pattern* /tmp/*multiblock* /tmp/file_reference /tmp/regular_file_test 2>/dev/null || true
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
echo "Creating random test data..."
dd if=/dev/urandom of=/tmp/test_data bs=1024 count=64 2>/dev/null
echo "✅ Created 64KB of random test data"

# Also write the same data to a regular file for MD5 comparison
cp /tmp/test_data /tmp/file_reference
echo "✅ Created reference file for MD5 comparison"

# CRITICAL TEST 1: Write data to device
echo ""
echo "=== CRITICAL TEST 1: Write Test ==="
echo "Writing 64KB test data to ublk device..."
echo "Command: dd if=/tmp/test_data of=$DEVICE_BDEV bs=1024 count=64 oflag=direct status=progress"
if ! sudo dd if=/tmp/test_data of="$DEVICE_BDEV" bs=1024 count=64 oflag=direct status=progress 2>&1; then
    echo "❌ CRITICAL FAILURE: Could not write to device"
    echo "Data plane I/O is NOT working!"
    exit 1
fi
echo "✅ Write completed successfully"

# CRITICAL TEST 2: Read data back from device
echo ""
echo "=== CRITICAL TEST 2: Read Test ==="
echo "Reading 64KB data back from ublk device..."
echo "Command: dd if=$DEVICE_BDEV of=/tmp/read_back bs=1024 count=64 iflag=direct status=progress"
if ! sudo dd if="$DEVICE_BDEV" of=/tmp/read_back bs=1024 count=64 iflag=direct status=progress 2>&1; then
    echo "❌ CRITICAL FAILURE: Could not read from device"
    echo "Data plane I/O is NOT working!"
    exit 1
fi
echo "✅ Read completed successfully"

# CRITICAL TEST 3: Data integrity verification
echo ""
echo "=== CRITICAL TEST 3: Data Integrity ==="
echo "Verifying data integrity with MD5 comparison..."

# Calculate MD5 of original random data
ORIGINAL_MD5=$(md5sum /tmp/test_data | cut -d' ' -f1)
echo "Original data MD5: $ORIGINAL_MD5"

# Calculate MD5 of data read back from ublk device
UBLK_MD5=$(md5sum /tmp/read_back | cut -d' ' -f1)
echo "Ublk read MD5:     $UBLK_MD5"

# Also write and read from regular file for comparison
echo "Writing same data to regular file for verification..."
cp /tmp/test_data /tmp/regular_file_test
FILE_MD5=$(md5sum /tmp/regular_file_test | cut -d' ' -f1)
echo "Regular file MD5:  $FILE_MD5"

# Compare all three MD5 hashes
if [ "$ORIGINAL_MD5" != "$UBLK_MD5" ]; then
    echo "❌ CRITICAL FAILURE: Ublk device data corruption detected!"
    echo "Original MD5:  $ORIGINAL_MD5"
    echo "Ublk MD5:      $UBLK_MD5"
    echo "Data integrity failed - backend or I/O processing has bugs!"
    exit 1
fi

if [ "$ORIGINAL_MD5" != "$FILE_MD5" ]; then
    echo "❌ CRITICAL FAILURE: Regular file data corruption detected!"
    echo "This indicates filesystem or storage issues"
    exit 1
fi

if [ "$UBLK_MD5" != "$FILE_MD5" ]; then
    echo "❌ CRITICAL FAILURE: Ublk vs file MD5 mismatch!"
    echo "Ublk MD5: $UBLK_MD5"
    echo "File MD5: $FILE_MD5"
    exit 1
fi

echo "✅ Data integrity verified - all MD5 hashes match:"
echo "  Original:     $ORIGINAL_MD5"
echo "  Ublk device:  $UBLK_MD5"
echo "  Regular file: $FILE_MD5"

# CRITICAL TEST 4: Multiple block operations with MD5 verification
echo ""
echo "=== CRITICAL TEST 4: Multiple Block Test ==="
echo "Testing multiple scattered writes with full verification..."

# Create multiple test patterns
echo "Creating multiple test patterns..."
dd if=/dev/urandom of=/tmp/pattern1 bs=512 count=2 2>/dev/null  # 1KB
dd if=/dev/urandom of=/tmp/pattern2 bs=512 count=4 2>/dev/null  # 2KB
dd if=/dev/urandom of=/tmp/pattern3 bs=512 count=8 2>/dev/null  # 4KB

# Initialize both reference file and ublk device with zeros to ensure identical starting state
echo "Initializing reference file and ublk device with zeros..."
dd if=/dev/zero of=/tmp/reference_multiblock bs=512 count=40 2>/dev/null
sudo dd if=/dev/zero of="$DEVICE_BDEV" bs=512 count=40 oflag=direct 2>/dev/null

# Write patterns to different locations on both ublk and regular file
echo "Writing patterns to different offsets..."
# Pattern 1 at offset 0
sudo dd if=/tmp/pattern1 of="$DEVICE_BDEV" bs=512 seek=0 count=2 conv=notrunc oflag=direct 2>/dev/null
dd if=/tmp/pattern1 of=/tmp/reference_multiblock bs=512 seek=0 count=2 conv=notrunc 2>/dev/null

# Pattern 2 at offset 8KB (sector 16)
sudo dd if=/tmp/pattern2 of="$DEVICE_BDEV" bs=512 seek=16 count=4 conv=notrunc 2>/dev/null
dd if=/tmp/pattern2 of=/tmp/reference_multiblock bs=512 seek=16 count=4 conv=notrunc 2>/dev/null

# Pattern 3 at offset 16KB (sector 32)
sudo dd if=/tmp/pattern3 of="$DEVICE_BDEV" bs=512 seek=32 count=8 conv=notrunc 2>/dev/null
dd if=/tmp/pattern3 of=/tmp/reference_multiblock bs=512 seek=32 count=8 conv=notrunc 2>/dev/null

# Read back the entire areas and compare
echo "Reading back and verifying scattered data..."
sudo dd if="$DEVICE_BDEV" of=/tmp/multiblock_readback bs=512 count=40 2>/dev/null

# Calculate MD5 of the reference vs readback
MULTI_REF_MD5=$(md5sum /tmp/reference_multiblock | cut -d' ' -f1)
MULTI_UBLK_MD5=$(md5sum /tmp/multiblock_readback | cut -d' ' -f1)

echo "Multi-block reference MD5: $MULTI_REF_MD5"
echo "Multi-block ublk MD5:      $MULTI_UBLK_MD5"

if [ "$MULTI_REF_MD5" != "$MULTI_UBLK_MD5" ]; then
    echo "❌ CRITICAL FAILURE: Multi-block data integrity failed!"
    echo "Scattered write/read operations have data corruption"
    echo "Reference MD5: $MULTI_REF_MD5"
    echo "Ublk MD5:      $MULTI_UBLK_MD5"
    exit 1
fi

echo "✅ Multiple block operations with MD5 verification working"

echo ""
echo "🎉 ALL CRITICAL TESTS PASSED!"
echo "✅ Device creation works"
echo "✅ Write operations work"
echo "✅ Read operations work"
echo "✅ Data integrity verified with MD5 hashing"
echo "✅ Multiple block I/O with scattered writes verified"
echo "✅ Ublk device matches regular file behavior exactly"
echo ""
echo "The ublk device is FUNCTIONALLY WORKING for I/O operations!"
echo "Data integrity is cryptographically verified across all test scenarios."
echo "It is safe to proceed with performance testing."
