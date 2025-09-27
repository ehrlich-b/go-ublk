#!/bin/bash
# Benchmarking with full debugging like simple-e2e
set -euo pipefail

echo "=== Simple go-ublk Performance Test with Debugging ==="
echo "Testing on kernel: $(uname -r)"
echo ""

# Check for required tools
for cmd in fio timeout; do
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
    sudo rm -f /tmp/ublk_benchmark.log 2>/dev/null || true
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

# Start ublk device in background with verbose logging
echo "Starting ublk memory device (64MB for benchmarking)..."
echo "Command: sudo ./ublk-mem --size=64M -v"
sudo env UBLK_DEVINFO_LEN=${UBLK_DEVINFO_LEN:-} ./ublk-mem --size=64M -v > /tmp/ublk_benchmark.log 2>&1 &
UBLK_PID=$!
echo "Started ublk-mem with PID $UBLK_PID"
sleep 0.2
TAIL_PID=""

echo "Waiting for device nodes to appear (up to 30s)..."
echo "Original highest device ID: $ORIG_MAX_ID"
for i in $(seq 1 30); do
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
        tail -n 100 /tmp/ublk_benchmark.log || true
        exit 1
    fi
fi

echo "Detected device ID: $DEVICE_ID (block: $DEVICE_BDEV)"
if [ "$DEVICE_ID" = "$ORIG_MAX_ID" ]; then
    echo "ERROR: No new device detected (highest ID still $DEVICE_ID)"
    tail -n 100 /tmp/ublk_benchmark.log || true
    exit 1
fi

echo "✅ Device nodes ready: $DEVICE_BDEV and $DEVICE_CDEV"

# Get trace buffer before benchmark
echo ""
echo "=== KERNEL TRACE BEFORE BENCHMARK ==="
sudo cat /sys/kernel/tracing/trace | tail -n 20 || echo "No trace available"

# Benchmark with timeout and trace monitoring
echo ""
echo "=== Running simple 4K read test with timeout ==="
echo "Command: timeout 30 fio --name=simple_test --filename=\"$DEVICE_BDEV\" --size=4M --ioengine=libaio --direct=1 --runtime=3 --time_based=1 --rw=randread --bs=4k --iodepth=1 --numjobs=1"

if timeout 30 sudo fio \
    --name=simple_test \
    --filename="$DEVICE_BDEV" \
    --size=4M \
    --ioengine=libaio \
    --direct=1 \
    --runtime=3 \
    --time_based=1 \
    --rw=randread \
    --bs=4k \
    --iodepth=1 \
    --numjobs=1 \
    --output-format=terse \
    | cut -d';' -f8,9 | awk -F';' '{printf "IOPS: %s, Bandwidth: %s KB/s\n", $1, $2}'; then
    echo "✅ Benchmark completed successfully"
else
    echo "❌ Benchmark failed or timed out"
    echo ""
    echo "=== KERNEL TRACE AFTER TIMEOUT ==="
    sudo cat /sys/kernel/tracing/trace | tail -n 50 || echo "No trace available"
    echo ""
    echo "=== UBLK LOG AFTER TIMEOUT ==="
    tail -n 50 /tmp/ublk_benchmark.log || true
    exit 1
fi

echo ""
echo "=== KERNEL TRACE AFTER BENCHMARK ==="
sudo cat /sys/kernel/tracing/trace | tail -n 20 || echo "No trace available"