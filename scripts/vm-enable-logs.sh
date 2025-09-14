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

echo "Enabling dynamic debug for io_uring and ublk..."
{
  echo 'file fs/io_uring* +p'
  echo 'file kernel/io_uring* +p'
  echo 'file io_uring* +p'
  echo 'file drivers/block/ublk* +p'
} | sudo tee /sys/kernel/debug/dynamic_debug/control >/dev/null || true

echo "Enabling tracepoints (io_uring, syscalls io_uring_enter, block)..."
for p in syscalls:sys_enter_io_uring_enter syscalls:sys_exit_io_uring_enter; do
  if [ -e "/sys/kernel/debug/tracing/events/${p}/enable" ]; then echo 1 | sudo tee "/sys/kernel/debug/tracing/events/${p}/enable" >/dev/null; fi
done
for f in /sys/kernel/debug/tracing/events/io_uring/*/enable; do [ -e "$f" ] && echo 1 | sudo tee "$f" >/dev/null; done
for f in /sys/kernel/debug/tracing/events/block/*/enable; do [ -e "$f" ] && echo 1 | sudo tee "$f" >/dev/null; done

echo "Configuring ftrace function graph for io_uring*/ublk*..."
echo 0 | sudo tee /sys/kernel/debug/tracing/tracing_on >/dev/null
: | sudo tee /sys/kernel/debug/tracing/trace >/dev/null
echo function_graph | sudo tee /sys/kernel/debug/tracing/current_tracer >/dev/null
# Configure function filters for function_graph tracer
if [ -w /sys/kernel/debug/tracing/set_graph_function ]; then
  echo io_uring* | sudo tee /sys/kernel/debug/tracing/set_graph_function >/dev/null || true
  echo ublk* | sudo tee -a /sys/kernel/debug/tracing/set_graph_function >/dev/null || true
else
  echo io_uring* | sudo tee /sys/kernel/debug/tracing/set_ftrace_filter >/dev/null || true
  echo ublk* | sudo tee -a /sys/kernel/debug/tracing/set_ftrace_filter >/dev/null || true
fi
echo 1 | sudo tee /sys/kernel/debug/tracing/tracing_on >/dev/null

echo "✓ Kernel logging/tracing enabled."
