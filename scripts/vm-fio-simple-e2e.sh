#!/bin/bash
# Simple fio test with maximum verbosity for debugging Direct I/O issue
# Tests both buffered and direct I/O to isolate the problem

set -euo pipefail

# Function to check for D state processes and report them
check_d_state_processes() {
    echo "=== CHECKING FOR D STATE PROCESSES ==="
    local d_procs=$(ps aux | awk '$8 ~ /D/ {print $2, $11}' || true)
    if [ -n "$d_procs" ]; then
        echo "❌ FOUND PROCESSES IN D STATE (uninterruptible sleep):"
        echo "$d_procs"
        echo "This indicates I/O is hanging in the kernel!"
        return 1
    else
        echo "✅ No D state processes found"
        return 0
    fi
}

# Function to cleanup what we can (D state processes can't be killed)
cleanup_force() {
    echo "=== CLEANUP (skipping D state processes) ==="
    # Only kill processes that aren't in D state (exclude this script)
    local killable_pids=$(ps aux | grep -E "(ublk-mem|timeout.*dd|timeout.*fio|^fio)" | grep -v grep | grep -v "vm-fio-simple" | awk '$8 !~ /D/ {print $2}' || true)
    if [ -n "$killable_pids" ]; then
        echo "Killing non-D state processes: $killable_pids"
        sudo kill -9 $killable_pids 2>/dev/null || true
    fi

    # Check if fio is in D state and report it (but don't try to kill)
    local fio_d_state=$(ps aux | grep -E "^fio|[[:space:]]fio" | grep -v grep | grep -v "vm-fio-simple" | awk '$8 ~ /D/ {print $2}' || true)
    if [ -n "$fio_d_state" ]; then
        echo "❌ fio processes in D state (can't be killed): $fio_d_state"
        echo "This indicates kernel I/O hang - VM reset required"
    fi

    sudo rm -f /tmp/fio_test_* 2>/dev/null || true
}

echo "=== FIO SIMPLE DEBUG TEST ==="
echo "Testing both buffered and direct I/O to isolate the issue"
echo ""

# Clean up any previous test
cleanup() {
    echo "Cleaning up..."
    if [ -n "${UBLK_PID:-}" ]; then
        sudo kill -SIGINT $UBLK_PID 2>/dev/null || true
        sleep 2
        # Check if it's still running and force kill if needed
        if kill -0 $UBLK_PID 2>/dev/null; then
            echo "ublk-mem didn't exit cleanly, force killing..."
            sudo kill -9 $UBLK_PID 2>/dev/null || true
        fi
    fi
    cleanup_force
}
trap cleanup EXIT

# Start with clean slate
echo "=== INITIAL CLEANUP ==="
cleanup_force

# Ensure ublk driver is loaded so /dev/ublk-control exists
echo "Ensuring ublk_drv kernel module is loaded..."
if ! sudo modprobe ublk_drv 2>/dev/null; then
    echo "ERROR: Failed to load ublk_drv kernel module"
    exit 1
fi

# Start ublk device in background with MAXIMUM verbosity
echo "Starting ublk-mem with maximum verbosity..."
echo "All logs will go to stdout for immediate visibility"
sudo ./ublk-mem --size=16M -v &
UBLK_PID=$!
echo "Started ublk-mem with PID $UBLK_PID"

# Add PID to kernel trace filtering
echo "Adding PID $UBLK_PID to kernel trace filter for precise monitoring..."
if [ -x /tmp/add_pid_filter.sh ]; then
    /tmp/add_pid_filter.sh $UBLK_PID
else
    echo "  (helper /tmp/add_pid_filter.sh not present; skipping trace filter setup)"
fi

# DON'T clear trace buffer - we want to see ALL operations from device creation onwards
echo "Keeping trace buffer to capture device creation + I/O operations..."

# Wait for device to appear (much shorter timeout)
DEVICE=""
echo "Waiting for device to appear..."
for i in $(seq 1 10); do
    if [ -b /dev/ublkb* ] 2>/dev/null; then
        DEVICE=$(ls /dev/ublkb* | head -1)
        echo "Device found: $DEVICE"
        break
    fi
    echo "  ($i/10) waiting..."
    sleep 1
done

if [ ! -b "$DEVICE" ]; then
    echo "ERROR: Device did not appear"
    exit 1
fi

echo "✅ Device ready: $DEVICE"
sleep 2  # Give it a moment to stabilize

# First test: Simple dd to confirm basic I/O works
echo ""
echo "=== TEST 1: DD WRITE/READ (BUFFERED I/O - BASELINE) ==="
echo "Creating test pattern..."
echo "TEST PATTERN FOR DD" > /tmp/dd_test_data
truncate -s 4096 /tmp/dd_test_data

echo "Writing 4KB with dd (buffered)..."
if timeout 5 sudo dd if=/tmp/dd_test_data of="$DEVICE" bs=4096 count=1 status=progress 2>&1; then
    echo "✅ DD Write completed"
else
    echo "❌ DD Write failed/timed out"
    exit 1
fi

echo "Reading 4KB with dd (buffered)..."
if timeout 5 sudo dd if="$DEVICE" of=/tmp/dd_read_back bs=4096 count=1 status=progress 2>&1; then
    echo "✅ DD Read completed"
    if cmp /tmp/dd_test_data /tmp/dd_read_back; then
        echo "✅ DD Data integrity verified"
    else
        echo "❌ DD Data corruption!"
        exit 1
    fi
else
    echo "❌ DD Read failed/timed out"
    exit 1
fi

echo ""
echo "=== KERNEL TRACE AFTER DD (BUFFERED) ==="
sudo cat /sys/kernel/tracing/trace | tail -n 20 || true

# Test 2: FIO with buffered I/O (no direct flag)
echo ""
echo "=== TEST 2: FIO BUFFERED I/O (NO DIRECT FLAG) ==="
echo "Running fio with buffered I/O (4KB single operation)..."

# Create minimal fio job file for buffered I/O
cat > /tmp/fio_buffered.ini <<EOF
[buffered-test]
filename=$DEVICE
size=4k
rw=write
bs=4k
numjobs=1
iodepth=1
ioengine=psync
# NO direct=1 flag - using buffered I/O
EOF

echo "FIO job file:"
cat /tmp/fio_buffered.ini

echo "Starting fio with 10 second timeout..."
if timeout 10 sudo fio /tmp/fio_buffered.ini 2>&1; then
    echo "✅ FIO buffered write completed"
else
    EXIT_CODE=$?
    if [ $EXIT_CODE -eq 124 ]; then
        echo "❌ FIO BUFFERED I/O TIMED OUT!"
        check_d_state_processes || true
    else
        echo "❌ FIO buffered write failed (exit code: $EXIT_CODE)"
    fi
    echo "Kernel trace after failure:"
    sudo cat /sys/kernel/tracing/trace | tail -n 30 || true
fi

echo ""
echo "=== KERNEL TRACE AFTER FIO BUFFERED ==="
sudo cat /sys/kernel/tracing/trace | tail -n 20 || true

# Test 3: FIO with direct I/O (the problematic case)
echo ""
echo "=== TEST 3: FIO DIRECT I/O (SUSPECTED PROBLEM) ==="
echo "Running fio with direct I/O (4KB single operation)..."

# Create minimal fio job file for direct I/O
cat > /tmp/fio_direct.ini <<EOF
[direct-test]
filename=$DEVICE
size=4k
rw=write
bs=4k
numjobs=1
iodepth=1
ioengine=libaio
direct=1
# Using direct I/O with libaio
EOF

echo "FIO job file:"
cat /tmp/fio_direct.ini

echo "Starting fio direct I/O with 10 second timeout..."
echo "THIS IS WHERE WE EXPECT IT TO HANG..."
if timeout 10 sudo fio /tmp/fio_direct.ini 2>&1; then
    echo "✅ FIO direct write completed (SURPRISING!)"
else
    EXIT_CODE=$?
    if [ $EXIT_CODE -eq 124 ]; then
        echo "❌ FIO DIRECT I/O TIMED OUT (AS EXPECTED)!"
        echo "This confirms the Direct I/O handling issue"
        check_d_state_processes || true
    else
        echo "❌ FIO direct write failed (exit code: $EXIT_CODE)"
    fi
    echo "Kernel trace after direct I/O timeout:"
    sudo cat /sys/kernel/tracing/trace | tail -n 50 || true
fi

echo ""
echo "=== FINAL KERNEL TRACE ==="
sudo cat /sys/kernel/tracing/trace | tail -n 50 || true

echo ""
echo "=== FINAL DMESG CHECK ==="
sudo dmesg | tail -n 20 || true

echo ""
echo "=== TEST SUMMARY ==="
echo "1. DD (buffered I/O): Expected to PASS ✅"
echo "2. FIO (buffered I/O): Expected to PASS ✅"
echo "3. FIO (direct I/O): Expected to HANG ❌"
echo ""
echo "If tests 1 & 2 pass but test 3 hangs, we've confirmed"
echo "the issue is specifically with Direct I/O handling."
