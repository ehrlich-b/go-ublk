#!/usr/bin/env bash
set -euo pipefail

STAMP=$(date +%Y%m%d-%H%M%S)
OUTDIR=/tmp/ublk-debug-$STAMP
mkdir -p "$OUTDIR"
exec > >(tee -a "$OUTDIR/session.log") 2>&1

echo "=== VM DEBUG RUN ($STAMP) ==="
echo "Kernel: $(uname -a)"
echo "User: $(id)"
echo "Go: $(command -v go >/dev/null && go version || echo 'go not found')"

echo "-- Enabling modules and debug --"
sudo modprobe ublk_drv || true
sudo sh -c 'echo 8 > /proc/sys/kernel/printk' || true
sudo mount -t debugfs none /sys/kernel/debug 2>/dev/null || true
sudo mount -t tracefs nodev /sys/kernel/debug/tracing 2>/dev/null || true

echo "-- Dynamic debug for io_uring + ublk --"
{
  echo 'file fs/io_uring* +p'
  echo 'file kernel/io_uring* +p'
  echo 'file drivers/block/ublk* +p'
} | sudo tee /sys/kernel/debug/dynamic_debug/control >/dev/null || true

echo "-- Tracepoints --"
for p in syscalls:sys_enter_io_uring_enter syscalls:sys_exit_io_uring_enter; do
  if [ -e "/sys/kernel/debug/tracing/events/${p}/enable" ]; then echo 1 | sudo tee "/sys/kernel/debug/tracing/events/${p}/enable" >/dev/null; fi
done
for f in /sys/kernel/debug/tracing/events/io_uring/*/enable; do [ -e "$f" ] && echo 1 | sudo tee "$f" >/dev/null; done
for f in /sys/kernel/debug/tracing/events/block/*/enable; do [ -e "$f" ] && echo 1 | sudo tee "$f" >/dev/null; done

echo "-- ftrace function_graph for io_uring*/ublk* --"
echo 0 | sudo tee /sys/kernel/debug/tracing/tracing_on >/dev/null
: | sudo tee /sys/kernel/debug/tracing/trace >/dev/null
echo function_graph | sudo tee /sys/kernel/debug/tracing/current_tracer >/dev/null
if [ -w /sys/kernel/debug/tracing/set_graph_function ]; then
  echo io_uring* | sudo tee /sys/kernel/debug/tracing/set_graph_function >/dev/null || true
  echo ublk* | sudo tee -a /sys/kernel/debug/tracing/set_graph_function >/dev/null || true
else
  echo io_uring* | sudo tee /sys/kernel/debug/tracing/set_ftrace_filter >/dev/null || true
  echo ublk* | sudo tee -a /sys/kernel/debug/tracing/set_ftrace_filter >/dev/null || true
fi
echo 1 | sudo tee /sys/kernel/debug/tracing/tracing_on >/dev/null

echo "-- Header sanity: IORING_OP_URING_CMD from headers (if available) --"
grep -Rns --include='io_uring*.h' 'IORING_OP_URING_CMD' /usr/include /usr/src 2>/dev/null | head -5 || true

echo "-- Running ublk-mem (15s) with strace --"
STRACE_OUT="$OUTDIR/strace.log"
APP_OUT="$OUTDIR/ublk-mem.log"
set +e
sudo timeout 15 strace -ff -s 256 -yy -o "$STRACE_OUT" ./ublk-mem --size=16M -v | tee "$APP_OUT"
STATUS=$?
set -e
echo "ublk-mem exit status: $STATUS" | tee -a "$OUTDIR/status.txt"

echo "-- Capturing dmesg and trace tails --"
sudo dmesg | tail -n 2000 > "$OUTDIR/dmesg.tail.txt" || true
sudo tail -n 2000 /sys/kernel/debug/tracing/trace > "$OUTDIR/trace.tail.txt" || true

echo "-- Device nodes --"
ls -la /dev/ublk* || true

ARCHIVE=~/ublk-debug-$STAMP.tgz
tar -C "$OUTDIR" -czf "$ARCHIVE" .
echo "=== Collected logs: $ARCHIVE ==="

