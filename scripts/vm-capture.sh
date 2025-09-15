#!/usr/bin/env bash
set -euo pipefail

cd ~/ublk-test

# Start ublk-mem under sudo and capture its PID and logs in /tmp
sudo bash -lc '
  set -euo pipefail
  : > /tmp/ublk_mem.log
  nohup ./ublk-mem --size=16M -v > /tmp/ublk_mem.log 2>&1 &
  echo $! > /tmp/ublk_mem.pid
'

sleep 3

echo "DEVNODES:"
ls -la /dev/ublk* || true

echo "--- START of /tmp/ublk_mem.log ---"
sudo sed -n '1,260p' /tmp/ublk_mem.log || true
echo "--- END log ---"

sudo kill -INT $(sudo cat /tmp/ublk_mem.pid) 2>/dev/null || true

