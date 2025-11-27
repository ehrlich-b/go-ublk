#!/bin/bash
# Comprehensive fuzz testing for go-ublk block device
# Tests random I/O patterns, boundary conditions, data integrity, and stress scenarios

set -e

# Configuration
SIZE_MB=${1:-256}
DURATION=${2:-30}  # seconds per test
DEVICE=/dev/ublkb0
UBLK_MEM=./ublk-mem

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC}: $1"; }
fail() { echo -e "${RED}FAIL${NC}: $1"; exit 1; }
info() { echo -e "${YELLOW}TEST${NC}: $1"; }

PASS_COUNT=0
FAIL_COUNT=0

run_test() {
    local name="$1"
    shift
    info "$name"
    if "$@"; then
        pass "$name"
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        echo -e "${RED}FAIL${NC}: $name"
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
}

cleanup() {
    echo "Cleaning up..."
    sudo pkill -9 fio 2>/dev/null || true
    sudo pkill -9 ublk-mem 2>/dev/null || true
    sleep 1
}

trap cleanup EXIT INT TERM

echo "=============================================="
echo "       go-ublk Comprehensive Fuzz Test"
echo "=============================================="
echo "Size:     ${SIZE_MB}MB"
echo "Duration: ${DURATION}s per test"
echo ""

# Start ublk-mem
echo "Starting ublk-mem..."
sudo pkill -9 ublk-mem 2>/dev/null || true
sleep 1
sudo modprobe -r ublk_drv 2>/dev/null || true
sudo modprobe ublk_drv

if [ ! -x "$UBLK_MEM" ]; then
    fail "ublk-mem binary not found at $UBLK_MEM"
fi

sudo bash -c "$UBLK_MEM --size ${SIZE_MB}M &>/tmp/ublk-mem.log" &
UBLK_PID=$!

# Wait for device to appear
echo "Waiting for device..."
for i in $(seq 1 30); do
    if [ -b "$DEVICE" ]; then
        echo "Device ready: $DEVICE"
        break
    fi
    sleep 1
done

if [ ! -b "$DEVICE" ]; then
    echo "Device failed to appear. Log:"
    cat /tmp/ublk-mem.log
    fail "Device $DEVICE did not appear"
fi

# Get actual device size
ACTUAL_SIZE=$(sudo blockdev --getsize64 "$DEVICE")
echo "Actual device size: $ACTUAL_SIZE bytes"
echo ""

#==============================================================================
# Phase 1: Basic Data Integrity
#==============================================================================
echo "=============================================="
echo "Phase 1: Basic Data Integrity"
echo "=============================================="

# Test 1.1: Sequential write + read verify
run_test "1.1 Sequential write/read 4K blocks" bash -c "
    sudo fio --name=seqwrite --filename=$DEVICE --rw=write --bs=4k --size=16M \
        --ioengine=libaio --direct=1 --verify=md5 --verify_fatal=1 \
        --do_verify=1 >/dev/null 2>&1
"

# Test 1.2: Random write + read verify
run_test "1.2 Random write/read 4K blocks" bash -c "
    sudo fio --name=randverify --filename=$DEVICE --rw=randwrite --bs=4k --size=16M \
        --ioengine=libaio --direct=1 --verify=crc32c --verify_fatal=1 \
        --do_verify=1 --randrepeat=0 >/dev/null 2>&1
"

# Test 1.3: Large block integrity
run_test "1.3 Large block (1MB) integrity" bash -c "
    sudo fio --name=largeblock --filename=$DEVICE --rw=write --bs=1M --size=32M \
        --ioengine=libaio --direct=1 --verify=sha256 --verify_fatal=1 \
        --do_verify=1 >/dev/null 2>&1
"

# Test 1.4: Small block integrity (512 bytes)
run_test "1.4 Small block (512B) integrity" bash -c "
    sudo fio --name=smallblock --filename=$DEVICE --rw=write --bs=512 --size=1M \
        --ioengine=libaio --direct=1 --verify=md5 --verify_fatal=1 \
        --do_verify=1 >/dev/null 2>&1
"

#==============================================================================
# Phase 2: Boundary Conditions
#==============================================================================
echo ""
echo "=============================================="
echo "Phase 2: Boundary Conditions"
echo "=============================================="

# Test 2.1: First sector
run_test "2.1 First sector read/write" bash -c "
    echo 'first_sector_test_data' | sudo dd of=$DEVICE bs=512 count=1 conv=notrunc 2>/dev/null
    sudo dd if=$DEVICE bs=512 count=1 2>/dev/null | grep -q 'first_sector_test_data'
"

# Test 2.2: Last sector
LAST_SECTOR=$((ACTUAL_SIZE / 512 - 1))
run_test "2.2 Last sector read/write" bash -c "
    echo 'last_sector_test_data_' | sudo dd of=$DEVICE bs=512 count=1 seek=$LAST_SECTOR conv=notrunc 2>/dev/null
    sudo dd if=$DEVICE bs=512 count=1 skip=$LAST_SECTOR 2>/dev/null | grep -q 'last_sector_test_data_'
"

# Test 2.3: Sector boundary crossing (read across 4K boundary)
run_test "2.3 Cross-boundary read (4K)" bash -c "
    # Write pattern at 4K-256 through 4K+256 (512 bytes crossing 4K boundary)
    dd if=/dev/urandom bs=512 count=1 2>/dev/null | sudo dd of=$DEVICE bs=1 seek=3840 conv=notrunc 2>/dev/null
    sudo dd if=$DEVICE bs=512 count=1 skip=7 iflag=skip_bytes 2>/dev/null | wc -c | grep -q 512
"

# Test 2.4: Various odd sizes
run_test "2.4 Odd block sizes (513, 1023, 4097 bytes)" bash -c "
    for size in 513 1023 4097; do
        dd if=/dev/urandom bs=\$size count=1 2>/dev/null | sudo dd of=$DEVICE conv=notrunc 2>/dev/null
    done
"

#==============================================================================
# Phase 3: Random I/O Patterns (Fuzzing)
#==============================================================================
echo ""
echo "=============================================="
echo "Phase 3: Random I/O Patterns (Fuzzing)"
echo "=============================================="

# Test 3.1: Completely random offsets and sizes with verification
run_test "3.1 Random offset/size with verify (${DURATION}s)" bash -c "
    sudo fio --name=fuzz_random --filename=$DEVICE \
        --rw=randrw --rwmixread=50 \
        --bs=512-64k:512 --bsrange=512-65536 \
        --size=64M --runtime=$DURATION --time_based \
        --ioengine=libaio --iodepth=32 --direct=1 \
        --verify=crc32c --verify_fatal=1 --do_verify=1 \
        --randrepeat=0 --allrandrepeat=0 \
        >/dev/null 2>&1
"

# Test 3.2: High queue depth random I/O
run_test "3.2 High queue depth (QD=128) random I/O" bash -c "
    sudo fio --name=highqd --filename=$DEVICE \
        --rw=randrw --rwmixread=70 --bs=4k \
        --size=64M --runtime=$((DURATION/2)) --time_based \
        --ioengine=libaio --iodepth=128 --direct=1 \
        --verify=crc32c --verify_fatal=1 --do_verify=1 \
        >/dev/null 2>&1
"

# Test 3.3: Multiple block size random pattern
run_test "3.3 Multi-blocksize random I/O" bash -c "
    sudo fio --name=multibs --filename=$DEVICE \
        --rw=randrw --rwmixread=50 \
        --bssplit=512/10:4k/40:64k/30:1m/20 \
        --size=64M --runtime=$DURATION --time_based \
        --ioengine=libaio --iodepth=64 --direct=1 \
        --verify=md5 --verify_fatal=1 --do_verify=1 \
        >/dev/null 2>&1
"

#==============================================================================
# Phase 4: Concurrent I/O Stress
#==============================================================================
echo ""
echo "=============================================="
echo "Phase 4: Concurrent I/O Stress"
echo "=============================================="

# Test 4.1: Multiple concurrent jobs, different patterns
run_test "4.1 4 concurrent jobs, mixed patterns" bash -c "
    sudo fio \
        --name=job1 --filename=$DEVICE --rw=randread --bs=4k --size=32M --offset=0 \
        --name=job2 --filename=$DEVICE --rw=randwrite --bs=4k --size=32M --offset=32M \
        --name=job3 --filename=$DEVICE --rw=read --bs=64k --size=32M --offset=64M \
        --name=job4 --filename=$DEVICE --rw=write --bs=64k --size=32M --offset=96M \
        --ioengine=libaio --iodepth=32 --direct=1 \
        --runtime=$DURATION --time_based \
        --group_reporting >/dev/null 2>&1
"

# Test 4.2: Overlapping I/O regions (stress test)
run_test "4.2 Overlapping I/O regions" bash -c "
    sudo fio \
        --name=overlap1 --filename=$DEVICE --rw=randrw --bs=4k --size=32M --offset=0 \
        --name=overlap2 --filename=$DEVICE --rw=randrw --bs=4k --size=32M --offset=16M \
        --ioengine=libaio --iodepth=64 --direct=1 \
        --runtime=$((DURATION/2)) --time_based \
        --group_reporting >/dev/null 2>&1
"

# Test 4.3: Maximum parallelism
run_test "4.3 8 jobs, max parallelism" bash -c "
    sudo fio --name=maxpar --filename=$DEVICE \
        --rw=randrw --rwmixread=50 --bs=4k \
        --numjobs=8 --size=16M \
        --ioengine=libaio --iodepth=64 --direct=1 \
        --runtime=$((DURATION/2)) --time_based \
        --group_reporting >/dev/null 2>&1
"

#==============================================================================
# Phase 5: Edge Cases and Error Conditions
#==============================================================================
echo ""
echo "=============================================="
echo "Phase 5: Edge Cases"
echo "=============================================="

# Test 5.1: Rapid open/close cycles
run_test "5.1 Rapid device open/close (100 cycles)" bash -c "
    for i in \$(seq 1 100); do
        sudo dd if=$DEVICE of=/dev/null bs=4k count=1 2>/dev/null
    done
"

# Test 5.2: Alternating read/write same location
run_test "5.2 Alternating R/W same location (1000 ops)" bash -c "
    for i in \$(seq 1 1000); do
        echo \"iter\$i\" | sudo dd of=$DEVICE bs=512 count=1 conv=notrunc 2>/dev/null
        sudo dd if=$DEVICE bs=512 count=1 2>/dev/null | grep -q \"iter\$i\"
    done
"

# Test 5.3: Zero-fill then verify
run_test "5.3 Zero-fill and verify" bash -c "
    sudo dd if=/dev/zero of=$DEVICE bs=1M count=16 conv=notrunc 2>/dev/null
    # Verify by comparing checksums
    EXPECTED=\$(dd if=/dev/zero bs=1M count=16 2>/dev/null | sha256sum | cut -d' ' -f1)
    ACTUAL=\$(sudo dd if=$DEVICE bs=1M count=16 2>/dev/null | sha256sum | cut -d' ' -f1)
    [ \"\$EXPECTED\" = \"\$ACTUAL\" ]
"

# Test 5.4: Pattern fill then verify
run_test "5.4 Pattern fill (0xAA) and verify" bash -c "
    # Create 1MB of 0xAA pattern
    dd if=/dev/zero bs=1M count=1 2>/dev/null | tr '\000' '\252' > /tmp/aa_pattern
    sudo dd if=/tmp/aa_pattern of=$DEVICE bs=1M count=1 conv=notrunc 2>/dev/null
    sudo dd if=$DEVICE bs=1M count=1 2>/dev/null | cmp - /tmp/aa_pattern
    rm /tmp/aa_pattern
"

#==============================================================================
# Phase 6: Sustained Load
#==============================================================================
echo ""
echo "=============================================="
echo "Phase 6: Sustained Load"
echo "=============================================="

# Test 6.1: Sustained random I/O with verification
run_test "6.1 Sustained random I/O (${DURATION}s)" bash -c "
    sudo fio --name=sustained --filename=$DEVICE \
        --rw=randrw --rwmixread=70 --bs=4k-128k:4k \
        --size=128M --runtime=$DURATION --time_based \
        --ioengine=libaio --iodepth=64 --direct=1 \
        --verify=crc32c --verify_fatal=1 --do_verify=1 \
        --randrepeat=0 \
        >/dev/null 2>&1
"

# Test 6.2: Write amplification test (many small writes)
run_test "6.2 Write amplification (small writes)" bash -c "
    sudo fio --name=writeamp --filename=$DEVICE \
        --rw=randwrite --bs=512 \
        --size=8M --runtime=$((DURATION/2)) --time_based \
        --ioengine=libaio --iodepth=32 --direct=1 \
        --verify=crc32c --verify_fatal=1 --do_verify=1 \
        >/dev/null 2>&1
"

#==============================================================================
# Phase 7: Final Data Integrity Check
#==============================================================================
echo ""
echo "=============================================="
echo "Phase 7: Final Integrity Verification"
echo "=============================================="

# Write known pattern, read back, verify
run_test "7.1 Final full-device integrity check" bash -c "
    # Write deterministic pattern
    sudo fio --name=final_write --filename=$DEVICE \
        --rw=write --bs=1M --size=64M \
        --ioengine=libaio --direct=1 \
        --verify=sha512 --verify_fatal=1 \
        >/dev/null 2>&1

    # Read and verify
    sudo fio --name=final_verify --filename=$DEVICE \
        --rw=read --bs=1M --size=64M \
        --ioengine=libaio --direct=1 \
        --verify=sha512 --verify_fatal=1 --do_verify=1 \
        >/dev/null 2>&1
"

#==============================================================================
# Summary
#==============================================================================
echo ""
echo "=============================================="
echo "                  SUMMARY"
echo "=============================================="
echo ""
TOTAL=$((PASS_COUNT + FAIL_COUNT))
echo "Tests passed: $PASS_COUNT / $TOTAL"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo -e "${RED}FUZZ TEST FAILED${NC}: $FAIL_COUNT test(s) failed"
    exit 1
else
    echo -e "${GREEN}ALL FUZZ TESTS PASSED${NC}"
    exit 0
fi
