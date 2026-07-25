#!/bin/bash
# Data-integrity sweep for go-ublk using the standalone shadow-oracle ./verify
# (no fio dependency). Sweeps queue-count x depth x {buffered,O_DIRECT} and, for
# each combo, brings up ublk-mem, runs verify with one worker per queue, then
# tears down gracefully and checks for leaked devices.
#
# Parses the actual auto-assigned /dev/ublkbN from the daemon log (does NOT
# assume ublkb0), and signals the daemon's OWN pid parsed from its
# "kill -USR1 <pid>" banner — NOT $!, which is sudo's pid (signaling sudo
# forwards SIGINT but a SIGKILL to sudo orphans the ublk-mem child).
set -u

SIZE=${1:-256M}
DURATION=${2:-15}
UBLK_MEM=./ublk-mem
VERIFY=./verify
QUEUES=(1 2 4 8)
DEPTHS=(1 64 128)

pass=0
fail=0
failed_combos=""

reap() { sudo "$UBLK_MEM" --del=all >/dev/null 2>&1 || true; }
# Last field only: `grep -oE '[0-9]+' | head -1` matches the "1" in "USR1" and
# yields pid 1 = systemd (SIGINT to pid 1 reboots the box).
daemon_pid() { grep -oE 'kill -USR1 [0-9]+' "$1" 2>/dev/null | tail -1 | awk '{print $NF}'; }

echo "=============================================="
echo "  go-ublk shadow-oracle integrity sweep"
echo "  kernel: $(uname -r)  arch: $(uname -m)"
echo "  size=$SIZE duration=${DURATION}s/combo"
echo "=============================================="

sudo pkill -9 -x ublk-mem 2>/dev/null || true
reap
sudo modprobe ublk_drv || { echo "cannot load ublk_drv"; exit 1; }

for q in "${QUEUES[@]}"; do
  for d in "${DEPTHS[@]}"; do
    for direct in 0 1; do
      dflag=""; [ "$direct" = 1 ] && dflag="-direct"
      echo ""
      echo "----- queues=$q depth=$d direct=$direct -----"
      sudo "$UBLK_MEM" --size="$SIZE" --queues="$q" --depth="$d" >/tmp/ublk-verify.log 2>&1 &
      spid=$!

      dev=""
      for _ in $(seq 1 40); do
        dev=$(grep -oE '/dev/ublkb[0-9]+' /tmp/ublk-verify.log 2>/dev/null | head -1)
        [ -n "$dev" ] && [ -b "$dev" ] && break
        [ -e "/proc/$spid" ] || break
        sleep 0.5
      done

      if [ -z "$dev" ] || [ ! -b "$dev" ]; then
        echo "FAIL: device did not appear (q=$q d=$d)"
        sed 's/^/  log| /' /tmp/ublk-verify.log | tail -20
        fail=$((fail+1)); failed_combos="$failed_combos q$q/d$d/dir$direct(no-dev)"
        sudo pkill -9 -x ublk-mem 2>/dev/null || true; reap; continue
      fi
      dpid=$(daemon_pid /tmp/ublk-verify.log); dpid=${dpid:-$spid}
      # Never signal init or a garbled pid: fall back to the direct child.
      case "$dpid" in ''|*[!0-9]*|0|1) dpid=$spid ;; esac

      if sudo "$VERIFY" -device="$dev" -workers="$q" -duration="${DURATION}s" $dflag; then
        pass=$((pass+1))
      else
        echo "FAIL combo q=$q d=$d direct=$direct dev=$dev"
        fail=$((fail+1)); failed_combos="$failed_combos q$q/d$d/dir$direct"
      fi

      # graceful teardown: SIGINT the ACTUAL daemon, wait, escalate only if wedged
      sudo kill -SIGINT "$dpid" 2>/dev/null || true
      for _ in $(seq 1 40); do [ -e "/proc/$dpid" ] || break; sleep 0.5; done
      if [ -e "/proc/$dpid" ]; then
        echo "WARN: ublk-mem did not exit gracefully (q=$q d=$d); force-killing"
        sudo kill -9 "$dpid" 2>/dev/null || true
      fi

      leftover=$(ls /dev/ublkb* 2>/dev/null || true)
      if [ -n "$leftover" ]; then
        echo "WARN: leaked device(s) after teardown: $leftover — reaping"
        reap
      fi
    done
  done
done

echo ""
echo "=============================================="
echo "  integrity sweep: PASS=$pass FAIL=$fail"
[ -n "$failed_combos" ] && echo "  failed:$failed_combos"
echo "=============================================="
[ "$fail" -eq 0 ]
