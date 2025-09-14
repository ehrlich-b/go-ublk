#!/usr/bin/env bash
set -euo pipefail

echo "--- dmesg (tail) ---"
sudo dmesg | tail -n 1000 || true

echo
echo "--- trace buffer (tail) ---"
sudo tail -n 1000 /sys/kernel/debug/tracing/trace || true

echo
echo "--- io_uring rings ---"
if [ -d /sys/kernel/debug/io_uring ]; then ls -l /sys/kernel/debug/io_uring; fi

