#!/usr/bin/env bash
set -euo pipefail

echo "Mounting debugfs/tracefs if needed..."
sudo mkdir -p /sys/kernel/debug || true
sudo mount -t debugfs none /sys/kernel/debug 2>/dev/null || true
sudo mkdir -p /sys/kernel/debug/tracing || true
sudo mount -t tracefs nodev /sys/kernel/debug/tracing 2>/dev/null || true

echo "Raising printk loglevel..."
echo 8 | sudo tee /proc/sys/kernel/printk >/dev/null

echo "Reloading ublk_drv (best-effort) ..."
sudo modprobe -r ublk_drv 2>/dev/null || true
sudo modprobe ublk_drv 2>/dev/null || true

echo "Setting up TARGETED tracing for CONTROL OPERATIONS ONLY..."
echo 0 | sudo tee /sys/kernel/debug/tracing/tracing_on >/dev/null
: | sudo tee /sys/kernel/debug/tracing/trace >/dev/null

echo "Enabling dynamic debug for ublk module..."
{
  echo 'module ublk_drv +p'
  echo 'file drivers/block/ublk* +p'
} | sudo tee /sys/kernel/debug/dynamic_debug/control >/dev/null || true

echo "Enabling ONLY ioctl syscalls (for control commands)..."
# Only ioctl syscalls to see ADD_DEV, SET_PARAMS, START_DEV
for p in syscalls:sys_enter_ioctl syscalls:sys_exit_ioctl; do
  if [ -e "/sys/kernel/debug/tracing/events/${p}/enable" ]; then
    echo 1 | sudo tee "/sys/kernel/debug/tracing/events/${p}/enable" >/dev/null
  fi
done

echo "Setting up function tracing for SPECIFIC ublk control functions..."
echo function | sudo tee /sys/kernel/debug/tracing/current_tracer >/dev/null

# Set up function filtering for ONLY specific ublk functions (CORRECT NAMES!)
echo "Adding ublk functions to ftrace filter..."
sudo bash -c 'echo > /sys/kernel/debug/tracing/set_ftrace_filter' || true
# Actual function names from /proc/kallsyms (handle .isra.0 suffixes):
for func in ublk_ctrl_uring_cmd ublk_ch_uring_cmd ublk_queue_rq; do
  if sudo bash -c "echo $func >> /sys/kernel/debug/tracing/set_ftrace_filter" 2>/dev/null; then
    echo "  ✓ Added $func"
  else
    echo "  ✗ Failed to add $func"
  fi
done
# Handle .isra.0 functions with wildcards since exact names might vary
for func in "ublk_ctrl_add_dev*" "ublk_ctrl_set_params*" "ublk_ctrl_start_dev*"; do
  if sudo bash -c "echo $func >> /sys/kernel/debug/tracing/set_ftrace_filter" 2>/dev/null; then
    echo "  ✓ Added $func"
  else
    echo "  ✗ Failed to add $func"
  fi
done

# NO io_uring tracing initially (causes CPU churn)
# NO block layer tracing initially (that's where it hangs anyway)

echo "Setting up PID filtering for our ublk-mem process..."
sudo bash -c 'echo > /sys/kernel/debug/tracing/set_ftrace_pid' || true

# Filter ioctl events by device file
for event in syscalls:sys_enter_ioctl syscalls:sys_exit_ioctl; do
  if [ -e "/sys/kernel/debug/tracing/events/${event}/filter" ]; then
    echo 'comm == "ublk-mem"' | sudo tee "/sys/kernel/debug/tracing/events/${event}/filter" >/dev/null || true
  fi
done

echo "Setting smaller trace buffer for focused output..."
echo 256 | sudo tee /sys/kernel/debug/tracing/buffer_size_kb >/dev/null || true

echo 1 | sudo tee /sys/kernel/debug/tracing/tracing_on >/dev/null

echo "✓ FIXED ublk tracing enabled with CORRECT function names!"
echo "Tracing: ublk_ctrl_uring_cmd, ublk_ctrl_add_dev, ublk_ctrl_set_params, ublk_ctrl_start_dev"
echo "Also: ublk_ch_uring_cmd (FETCH_REQ), ublk_queue_rq (block I/O)"

# Create a helper script to add PID filtering once ublk-mem starts
cat > /tmp/add_pid_filter.sh << 'EOF'
#!/bin/bash
if [ -n "$1" ]; then
  echo $1 | sudo tee -a /sys/kernel/debug/tracing/set_ftrace_pid >/dev/null
  echo "Added PID $1 to ftrace filter"
fi
EOF
chmod +x /tmp/add_pid_filter.sh
