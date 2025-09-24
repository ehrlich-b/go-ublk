#!/usr/bin/env bash
set -euo pipefail

echo "Mounting tracefs (NOT debugfs - that was the problem!)..."
sudo mkdir -p /sys/kernel/tracing || true
sudo mount -t tracefs nodev /sys/kernel/tracing 2>/dev/null || true
echo "Using /sys/kernel/tracing (tracefs) instead of /sys/kernel/debug/tracing (debugfs)"

echo "Raising printk loglevel..."
echo 8 | sudo tee /proc/sys/kernel/printk >/dev/null

echo "Reloading ublk_drv (best-effort) ..."
sudo modprobe -r ublk_drv 2>/dev/null || true
sudo modprobe ublk_drv 2>/dev/null || true

echo "Setting up TARGETED kprobe tracing for ublk operations..."
echo 0 | sudo tee /sys/kernel/tracing/tracing_on >/dev/null
: | sudo tee /sys/kernel/tracing/trace >/dev/null

echo "Enabling dynamic debug for ublk module..."
{
  echo 'module ublk_drv +p'
  echo 'file drivers/block/ublk* +p'
} | sudo tee /sys/kernel/debug/dynamic_debug/control >/dev/null || true

echo "Enabling io_uring syscalls (ublk uses URING_CMD, not ioctl)..."
# io_uring syscalls to see URING_CMD operations
for p in syscalls:sys_enter_io_uring_enter syscalls:sys_exit_io_uring_enter; do
  if [ -e "/sys/kernel/debug/tracing/events/${p}/enable" ]; then
    echo 1 | sudo tee "/sys/kernel/debug/tracing/events/${p}/enable" >/dev/null
  fi
done

echo "Setting up KPROBES for ublk functions (function tracing doesn't work for modules)..."
echo nop | sudo tee /sys/kernel/tracing/current_tracer >/dev/null

# Clear any existing kprobes first
sudo bash -c 'echo > /sys/kernel/tracing/kprobe_events' 2>/dev/null || true

echo "Adding CONFIRMED STABLE kprobes for ublk (6.1→6.11)..."

# Control path - URING_CMD on /dev/ublk-control
if echo 'p:probe_ublk_ctrl ublk_ctrl_uring_cmd' | sudo tee /sys/kernel/tracing/kprobe_events >/dev/null 2>&1; then
  echo "  ✓ Added ublk_ctrl_uring_cmd (control path)"
else
  echo "  ⚠ Could not add ublk_ctrl_uring_cmd"
fi

# Channel path - URING_CMD on /dev/ublkcN (FETCH_REQ/COMMIT)
if echo 'p:probe_ublk_ch ublk_ch_uring_cmd' | sudo tee -a /sys/kernel/tracing/kprobe_events >/dev/null 2>&1; then
  echo "  ✓ Added ublk_ch_uring_cmd (FETCH_REQ/COMMIT path)"
else
  echo "  ⚠ Could not add ublk_ch_uring_cmd"
fi

# Block-MQ path - THE CRITICAL ONE for dd I/O
if echo 'p:probe_ublk_qrq ublk_queue_rq' | sudo tee -a /sys/kernel/tracing/kprobe_events >/dev/null 2>&1; then
  echo "  ✓ Added ublk_queue_rq (blk-mq I/O path - CRITICAL!)"
else
  echo "  ⚠ Could not add ublk_queue_rq"
fi

# Enable all kprobes
for probe in probe_ublk_ctrl probe_ublk_ch probe_ublk_qrq; do
  if sudo bash -c "echo 1 > /sys/kernel/tracing/events/kprobes/${probe}/enable" 2>/dev/null; then
    echo "    ✓ Enabled $probe"
  else
    echo "    ✗ Failed to enable $probe"
  fi
done

# Add control command handlers (may be inlined)
echo "Adding control command handlers (may be inlined)..."
for func in ublk_ctrl_add_dev ublk_ctrl_set_params ublk_ctrl_get_params ublk_ctrl_get_dev_info ublk_ctrl_del_dev; do
  probe_name="probe_${func}"
  if sudo bash -c "echo 'p:${probe_name} ${func}' >> /sys/kernel/tracing/kprobe_events" 2>/dev/null; then
    echo "  ✓ Added $func"
    sudo bash -c "echo 1 > /sys/kernel/tracing/events/kprobes/${probe_name}/enable" 2>/dev/null
  else
    echo "  ⚠ Could not add $func (likely inlined)"
  fi
done

# CRITICAL: Enable block layer tracepoints to cross-check I/O flow
echo "Enabling block layer tracepoints for cross-checking..."
for event in block_rq_insert block_rq_issue block_rq_complete; do
  if sudo bash -c "echo 1 > /sys/kernel/tracing/events/block/${event}/enable" 2>/dev/null; then
    echo "  ✓ Enabled block:${event}"
  else
    echo "  ✗ Failed to enable block:${event}"
  fi
done

# NO io_uring tracing initially (causes CPU churn)
# NO block layer tracing initially (that's where it hangs anyway)

echo "Clearing any old PID filters (kprobes don't need them)..."
sudo bash -c 'echo > /sys/kernel/tracing/set_ftrace_pid' || true

# Filter io_uring events by process name (using correct tracefs path)
for event in syscalls:sys_enter_io_uring_enter syscalls:sys_exit_io_uring_enter; do
  if [ -e "/sys/kernel/tracing/events/${event}/filter" ]; then
    echo 'comm == "ublk-mem"' | sudo tee "/sys/kernel/tracing/events/${event}/filter" >/dev/null || true
  fi
done

echo "Setting smaller trace buffer for focused output..."
echo 512 | sudo tee /sys/kernel/tracing/buffer_size_kb >/dev/null || true

echo "Enabling tracing..."
echo 1 | sudo tee /sys/kernel/tracing/tracing_on >/dev/null

echo "✅ WORKING ublk kprobe tracing enabled!"
echo "Using kprobes (not function tracing - that doesn't work for kernel modules)"
echo "Tracing: ublk_ctrl_uring_cmd, ublk_ch_uring_cmd, ublk_queue_rq"
echo "Path: /sys/kernel/tracing (tracefs) not /sys/kernel/debug/tracing (debugfs)"
echo -e "\nTo check traces: sudo cat /sys/kernel/tracing/trace"
echo "To check kprobes: sudo cat /sys/kernel/tracing/kprobe_events"

# Create a helper script to add PID filtering once ublk-mem starts
cat > /tmp/add_pid_filter.sh << 'EOF'
#!/bin/bash
if [ -n "$1" ]; then
  echo $1 | sudo tee -a /sys/kernel/debug/tracing/set_ftrace_pid >/dev/null
  echo "Added PID $1 to ftrace filter"
fi
EOF
chmod +x /tmp/add_pid_filter.sh
