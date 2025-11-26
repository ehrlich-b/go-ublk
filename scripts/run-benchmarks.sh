#!/bin/bash
# Benchmark script for go-ublk
# Compares ublk memory backend with kernel loop device

set -e

UBLK_SIZE="1G"
RESULTS_DIR="results/$(date +%Y%m%d-%H%M%S)"
UBLK_DEV="/dev/ublkb0"
LOOP_DEV=""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== go-ublk Benchmark Suite ===${NC}"
echo "This will benchmark ublk memory backend and compare with kernel loop device"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
    echo -e "${RED}Please run as root (required for ublk device creation)${NC}"
    exit 1
fi

# Check for required tools
for tool in fio iostat; do
    if ! command -v $tool &> /dev/null; then
        echo -e "${RED}Required tool '$tool' not found. Please install it.${NC}"
        exit 1
    fi
done

# Create results directory
mkdir -p "$RESULTS_DIR"
echo -e "${GREEN}Results will be saved to: $RESULTS_DIR${NC}"

# Function to run benchmark
run_benchmark() {
    local device=$1
    local test_name=$2
    local output_prefix=$3
    
    echo -e "${YELLOW}Running $test_name on $device...${NC}"
    
    # Start iostat in background
    iostat -x 1 > "$RESULTS_DIR/${output_prefix}_iostat.txt" &
    IOSTAT_PID=$!
    
    # Run fio benchmark
    fio --output="$RESULTS_DIR/${output_prefix}_fio.json" \
        --output-format=json \
        --filename="$device" \
        "benchmarks/${test_name}.fio"
    
    # Stop iostat
    kill $IOSTAT_PID 2>/dev/null || true
    
    # Extract key metrics
    echo -e "${GREEN}Results for $test_name on $device:${NC}"
    python3 -c "
import json
import sys

with open('$RESULTS_DIR/${output_prefix}_fio.json', 'r') as f:
    data = json.load(f)
    
for job in data['jobs']:
    name = job['jobname']
    read = job.get('read', {})
    write = job.get('write', {})
    
    if read.get('iops'):
        print(f'  {name} READ:')
        print(f'    IOPS: {read[\"iops\"]:.0f}')
        print(f'    BW: {read[\"bw\"]:.0f} KB/s')
        print(f'    Latency (us): avg={read[\"lat_ns\"][\"mean\"]/1000:.1f}, p50={read[\"clat_ns\"][\"percentile\"][\"50.000000\"]/1000:.1f}, p99={read[\"clat_ns\"][\"percentile\"][\"99.000000\"]/1000:.1f}')
    
    if write.get('iops'):
        print(f'  {name} WRITE:')
        print(f'    IOPS: {write[\"iops\"]:.0f}')
        print(f'    BW: {write[\"bw\"]:.0f} KB/s')
        print(f'    Latency (us): avg={write[\"lat_ns\"][\"mean\"]/1000:.1f}, p50={write[\"clat_ns\"][\"percentile\"][\"50.000000\"]/1000:.1f}, p99={write[\"clat_ns\"][\"percentile\"][\"99.000000\"]/1000:.1f}')
" 2>/dev/null || echo "  (Install python3 for detailed results parsing)"
    echo ""
}

# Start ublk device
echo -e "${GREEN}Starting ublk memory device ($UBLK_SIZE)...${NC}"
./ublk-mem --size="$UBLK_SIZE" &
UBLK_PID=$!
sleep 2

# Verify device exists
if [ ! -b "$UBLK_DEV" ]; then
    echo -e "${RED}Failed to create ublk device${NC}"
    kill $UBLK_PID 2>/dev/null || true
    exit 1
fi

echo -e "${GREEN}ublk device created at $UBLK_DEV${NC}"

# Run benchmarks on ublk
echo -e "${GREEN}=== Benchmarking ublk Memory Backend ===${NC}"
run_benchmark "$UBLK_DEV" "latency-test" "ublk_latency"
run_benchmark "$UBLK_DEV" "4k-random-read" "ublk_4k_read"
run_benchmark "$UBLK_DEV" "4k-random-write" "ublk_4k_write"
run_benchmark "$UBLK_DEV" "128k-sequential" "ublk_128k_seq"

# Stop ublk device
echo -e "${YELLOW}Stopping ublk device...${NC}"
kill -SIGINT $UBLK_PID
wait $UBLK_PID 2>/dev/null || true

# Create loop device for comparison
echo -e "${GREEN}=== Creating loop device for comparison ===${NC}"
dd if=/dev/zero of=/tmp/loop_test.img bs=1M count=1024 status=none
LOOP_DEV=$(losetup --find --show /tmp/loop_test.img)
echo -e "${GREEN}Loop device created at $LOOP_DEV${NC}"

# Run benchmarks on loop device
echo -e "${GREEN}=== Benchmarking Kernel Loop Device ===${NC}"
run_benchmark "$LOOP_DEV" "latency-test" "loop_latency"
run_benchmark "$LOOP_DEV" "4k-random-read" "loop_4k_read"
run_benchmark "$LOOP_DEV" "4k-random-write" "loop_4k_write"
run_benchmark "$LOOP_DEV" "128k-sequential" "loop_128k_seq"

# Cleanup loop device
losetup -d "$LOOP_DEV"
rm -f /tmp/loop_test.img

# Generate summary
echo -e "${GREEN}=== Benchmark Complete ===${NC}"
echo "Results saved to: $RESULTS_DIR"
echo ""
echo "To analyze results in detail:"
echo "  - FIO JSON outputs: $RESULTS_DIR/*_fio.json"
echo "  - iostat logs: $RESULTS_DIR/*_iostat.txt"
echo ""
echo -e "${YELLOW}Summary comparison will be added in next version${NC}"