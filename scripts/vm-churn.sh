#!/bin/bash
# Lifecycle/leak soak for go-ublk: rapid create -> load -> teardown-under-load
# cycles across random queue/depth, plus a periodic ungraceful SIGKILL+reaper
# path. Counts leaked kernel devices, teardown hangs, and device-create failures.
# Exercises the Critical-Bug-#8 stop-under-load ordering and the reaper.
#
# Signals the daemon's OWN pid (from its "kill -USR1 <pid>" banner), NOT $!
# (= sudo's pid). SIGKILLing sudo only orphans the ublk-mem child, which
# previously produced a FALSE "leak/wedge" (orphaned healthy daemons pinning
# the module).
set -u

CYCLES=${1:-40}
UBLK_MEM=./ublk-mem
VERIFY=./verify
depths=(1 16 64 128)
leaks=0; hangs=0; nodev=0

reap() { sudo "$UBLK_MEM" --del=all >/dev/null 2>&1 || true; }
# Take the LAST field of the banner. A plain `grep -oE '[0-9]+' | head -1` picks
# the "1" out of "USR1" and returns pid 1 = systemd: SIGINT to pid 1 is
# ctrl-alt-del, so every graceful cycle silently REBOOTED the VM and the churn
# then blamed a kernel panic (boot_id changed, no oops trace).
daemon_pid() { grep -oE 'kill -USR1 [0-9]+' "$1" 2>/dev/null | tail -1 | awk '{print $NF}'; }

echo "=============================================="
echo "  go-ublk lifecycle/leak churn: $CYCLES cycles"
echo "  kernel: $(uname -r)  arch: $(uname -m)"
echo "=============================================="
sudo pkill -9 -x ublk-mem 2>/dev/null || true
sudo pkill -9 -x verify 2>/dev/null || true
reap
sudo modprobe ublk_drv || { echo "cannot load ublk_drv"; exit 1; }

for i in $(seq 1 "$CYCLES"); do
  q=$(( RANDOM % 8 + 1 ))
  d=${depths[$((RANDOM % 4))]}
  sudo GODEBUG="${GODEBUG:-}" "$UBLK_MEM" --size=64M --queues="$q" --depth="$d" >/tmp/churn.log 2>&1 &
  spid=$!

  dev=""
  for _ in $(seq 1 30); do
    dev=$(grep -oE "/dev/ublkb[0-9]+" /tmp/churn.log 2>/dev/null | head -1)
    [ -n "$dev" ] && [ -b "$dev" ] && break
    [ -e "/proc/$spid" ] || break
    sleep 0.3
  done
  if [ -z "$dev" ] || [ ! -b "$dev" ]; then
    echo "cycle $i: NO DEVICE (q=$q d=$d)"; nodev=$((nodev+1))
    sudo pkill -9 -x ublk-mem 2>/dev/null || true; reap; continue
  fi
  dpid=$(daemon_pid /tmp/churn.log); dpid=${dpid:-$spid}
  # Never signal init or a garbled pid: fall back to the direct child.
  case "$dpid" in ''|*[!0-9]*|0|1) dpid=$spid ;; esac

  if (( i % 5 == 0 )); then
    # ungraceful path: SIGKILL the ACTUAL daemon (leaves a registered device),
    # then reap it and confirm nothing is left.
    sudo kill -9 "$dpid" 2>/dev/null || true; sleep 0.5
    reap
    left=$(ls /dev/ublkb* 2>/dev/null | wc -l)
    if [ "$left" -ne 0 ]; then echo "cycle $i: reaper left $left device(s)"; leaks=$((leaks+1)); fi
  else
    # graceful teardown WHILE I/O is in flight (bug #8 path)
    sudo "$VERIFY" -device="$dev" -workers="$q" -duration=4s >/dev/null 2>&1 &
    vpid=$!
    sleep 1
    sudo kill -SIGINT "$dpid" 2>/dev/null || true
    ok=0
    for _ in $(seq 1 40); do [ -e "/proc/$dpid" ] || { ok=1; break; }; sleep 0.5; done
    if [ "$ok" = 0 ]; then echo "cycle $i: TEARDOWN HANG (q=$q d=$d)"; hangs=$((hangs+1)); sudo kill -9 "$dpid" 2>/dev/null || true; fi
    kill -9 "$vpid" 2>/dev/null || true
    sudo pkill -9 -x verify 2>/dev/null || true
    left=$(ls /dev/ublkb* 2>/dev/null || true)
    if [ -n "$left" ]; then echo "cycle $i: LEAK $left"; leaks=$((leaks+1)); reap; fi
  fi
done

echo ""
echo "=============================================="
echo "  churn done: cycles=$CYCLES nodev=$nodev hangs=$hangs leaks=$leaks"
echo "=============================================="
[ $((nodev + hangs + leaks)) -eq 0 ]
