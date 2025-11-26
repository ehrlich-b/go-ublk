#!/bin/bash
# Simple single I/O test with maximum verbosity for debugging
# This does JUST one write and one read to minimize noise
# Handles hanging I/O (D state) as a common debugging outcome

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
    # Only kill processes that aren't in D state
    local killable_pids=$(ps aux | grep -E "(ublk-mem|timeout)" | grep -v grep | awk '$8 !~ /D/ {print $2}' || true)
    if [ -n "$killable_pids" ]; then
        echo "Killing non-D state processes: $killable_pids"
        sudo kill -9 $killable_pids 2>/dev/null || true
    fi

    # Check if dd is in D state and report it (but don't try to kill)
    local dd_d_state=$(ps aux | grep dd | grep -v grep | awk '$8 ~ /D/ {print $2}' || true)
    if [ -n "$dd_d_state" ]; then
        echo "❌ dd processes in D state (can't be killed): $dd_d_state"
        echo "This indicates kernel I/O hang - VM reset required"
    fi

    sudo rm -f /tmp/simple_test_data /tmp/simple_read_back 2>/dev/null || true
}

echo "=== SIMPLE SINGLE I/O DEBUG TEST ==="
echo "This test performs exactly ONE write and ONE read for debugging"
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
# sudo bash -c ': > /sys/kernel/tracing/trace'  # DISABLED - we want all traces!

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

# Create simple test data - just 512 bytes (1 block)
echo "Creating 512-byte test pattern..."
echo "HELLO UBLK WORLD - THIS IS A SIMPLE TEST PATTERN" > /tmp/simple_test_data
# Pad to exactly 512 bytes
truncate -s 512 /tmp/simple_test_data
echo "✅ Test data created (512 bytes)"

# DON'T clear kernel trace before I/O - we want to see control operations + I/O together!
echo "Keeping kernel trace buffer to see full sequence..."
# sudo bash -c 'echo > /sys/kernel/tracing/trace' || true  # DISABLED!

echo ""
echo "=== PERFORMING SINGLE WRITE ==="
echo "Writing 512 bytes to block 0..."
echo "Command: sudo dd if=/tmp/simple_test_data of=$DEVICE bs=512 count=1 status=progress"

# Perform the write with timeout and background monitoring
echo "Starting dd write with 15 second timeout..."

# Use a different approach - run timeout in background and monitor it
(timeout 15 sudo dd if=/tmp/simple_test_data of="$DEVICE" bs=512 count=1 status=progress 2>&1; echo "DD_EXIT_CODE=$?" > /tmp/dd_result) &
TIMEOUT_PID=$!

# Monitor the timeout process (10 second max)
for i in $(seq 1 10); do
    if ! kill -0 $TIMEOUT_PID 2>/dev/null; then
        # Process finished
        if [ -f /tmp/dd_result ]; then
            source /tmp/dd_result
            if [ ${DD_EXIT_CODE:-1} -eq 0 ]; then
                echo "✅ Write completed"
                break
            else
                echo "❌ WRITE FAILED (exit code: $DD_EXIT_CODE)"
            fi
        fi
        break
    elif [ $i -eq 10 ]; then
        # Timed out
        echo "❌ WRITE TIMED OUT (10 seconds) - I/O hanging!"

        # Check for D state processes
        check_d_state_processes || true

        echo "Dumping FILTERED kernel trace (ublk-only):"
        sudo cat /sys/kernel/tracing/trace || true
        echo ""
        echo "Dumping dmesg for kernel errors:"
        sudo dmesg | tail -n 10 || true

        # Don't try to cleanup gracefully when I/O is broken - force exit
        echo "❌ TEST FAILED: I/O hangs detected - VM reset recommended"
        echo "Force killing all processes due to I/O hang..."
        sudo killall -9 ublk-mem timeout dd 2>/dev/null || true
        # Disable cleanup trap to prevent hanging
        trap - EXIT
        exit 1
    else
        echo "  ($i/10) waiting for write completion..."
        sleep 1
    fi
done

echo ""
echo "=== KERNEL TRACE AFTER WRITE ==="
sudo cat /sys/kernel/tracing/trace | tail -n 20 || true

echo ""
echo "=== PERFORMING SINGLE READ ==="
echo "Reading 512 bytes from block 0..."
echo "Command: sudo dd if=$DEVICE of=/tmp/simple_read_back bs=512 count=1 status=progress"

# Perform the read with timeout
if ! timeout 15 sudo dd if="$DEVICE" of=/tmp/simple_read_back bs=512 count=1 status=progress 2>&1; then
    if [ $? -eq 124 ]; then
        echo "❌ READ TIMED OUT (15 seconds) - I/O hanging!"
    else
        echo "❌ READ FAILED"
    fi
    echo "Dumping trace buffer after read failure/timeout:"
    sudo cat /sys/kernel/tracing/trace | tail -n 30 || true
    echo "Dumping dmesg for kernel errors:"
    sudo dmesg | tail -n 20 || true
    exit 1
fi
echo "✅ Read completed"

echo ""
echo "=== KERNEL TRACE AFTER READ ==="
sudo cat /sys/kernel/tracing/trace | tail -n 20 || true

echo ""
echo "=== DATA VERIFICATION ==="
if cmp /tmp/simple_test_data /tmp/simple_read_back; then
    echo "✅ Data integrity verified - read data matches written data"
    echo "🎉 SIMPLE I/O TEST PASSED!"
else
    echo "❌ Data corruption detected!"
    echo "Original data:"
    hexdump -C /tmp/simple_test_data | head -5
    echo "Read back data:"
    hexdump -C /tmp/simple_read_back | head -5
    exit 1
fi

echo ""
echo "=== FINAL STATUS ==="
echo "✅ Device creation: SUCCESS"
echo "✅ Single write: SUCCESS"
echo "✅ Single read: SUCCESS"
echo "✅ Data integrity: SUCCESS"
echo ""
echo "Basic I/O functionality is working!"
