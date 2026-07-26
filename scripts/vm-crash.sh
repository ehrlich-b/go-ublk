#!/bin/bash
# Crash / power-fail consistency driver for go-ublk (TODO Phase 2).
#
# Uses the self-describing ./crash oracle against ublk-loop, the only example
# whose data outlives its daemon. Three subcommands:
#
#   kill [CYCLES] [SIZE]  SIGKILL the daemon mid-write, reap, restart on the
#                         same image, and verify. Alternates the buffered
#                         (volatile cache -> FLUSH -> fsync) and -sync
#                         (O_DSYNC write-through) durability flavors. Also
#                         asserts the host recovers: no D-state, no leaked
#                         device, no oops, and the same boot_id throughout.
#   arm [SIZE]            Set up for a host-driven hard reset: start daemon and
#                         writer detached, wait for a flushed generation, print
#                         ARMED and exit. The caller then resets the machine.
#   verify                After that reset, restart on the surviving image and
#                         verify. This is the only mode that tests real
#                         power-fail durability, because it is the only one that
#                         loses the host page cache.
#   selftest              Prove the oracle can fail. Runs the writer against a
#                         plain file (no ublk at all), then injects a lost, an
#                         aliased and a torn block and asserts each is caught.
#                         `kill` runs this first, so a PASS there is never the
#                         silence of a check that cannot detect anything.
#
# What each mode can and cannot prove is spelled out where it is asserted: a
# SIGKILL does not discard the page cache, so it tests the ublk path and
# recovery, not whether fsync reached the platter.

# pipefail matters here: every oracle invocation is piped through sed for
# indentation, and without it the exit status checked would be sed's.
set -uo pipefail

MODE=${1:-kill}
UBLK_LOOP=./ublk-loop
CRASH=./crash
IMG=/var/tmp/crash.img
WITNESS=$PWD/crash.witness
DLOG=/tmp/crash-daemon.log
WLOG=/tmp/crash-writer.log

boot_id() { cat /proc/sys/kernel/random/boot_id; }
# Exact-name match only. An unanchored pattern here once matched the harness's
# own command line and SIGKILLed the caller.
daemon_pid() { pgrep -x ublk-loop | tail -1; }
reap() { sudo "$UBLK_LOOP" -del=all >/dev/null 2>&1 || true; }
# dstate_persistent — prints any task that is in D-state in *every* sample over
# 5s, and returns non-zero if there is one. The intersection is the point: a task
# wedged on a dead device stays in D indefinitely (the "needs a host reboot"
# failure), while kernel workqueue threads dip into D constantly as normal
# operation. A single sample flagged an unrelated `kworker/u16:2+events_unbound`
# — idle again seconds later — as a go-ublk failure.
dstate_persistent() {
  local acc="" cur i
  for i in $(seq 1 10); do
    cur=$(ps -eo state=,pid= | awk '$1 ~ /^D/ {print $2}' | sort)
    if [ "$i" = 1 ]; then
      acc="$cur"
    else
      acc=$(comm -12 <(echo "$acc") <(echo "$cur"))
    fi
    [ -z "$acc" ] && return 0
    sleep 0.5
  done
  local p
  for p in $acc; do
    echo "  D $p $(cat "/proc/$p/comm" 2>/dev/null) $(sudo cat "/proc/$p/wchan" 2>/dev/null)"
  done
  return 1
}

# start_daemon FLAVOR — brings up ublk-loop on $IMG detached from this shell and
# echoes the device path it was given (which is NOT assumed to be ublkb0).
start_daemon() {
  local flavor=$1 extra=""
  [ "$flavor" = "sync" ] && extra="-sync"
  sudo setsid "$UBLK_LOOP" -file="$IMG" -size="$SIZE" -queues=4 -depth=64 $extra \
    >"$DLOG" 2>&1 < /dev/null &
  # Drop it from the job table so bash does not print a "Killed" notice for it
  # in the middle of the report when the test kills it on purpose.
  disown 2>/dev/null || true
  local dev=""
  for _ in $(seq 1 60); do
    dev=$(grep -oE '/dev/ublkb[0-9]+' "$DLOG" 2>/dev/null | head -1)
    [ -n "$dev" ] && [ -b "$dev" ] && break
    sleep 0.5
  done
  [ -n "$dev" ] && [ -b "$dev" ] || return 1
  echo "$dev"
}

# MIN_FLUSHED is how many full generations of region A must be written AND
# flushed before the crash lands. It has to exceed 1: the check requires every A
# block to hold generation (flushed-1) or newer, so at a witness of 1 the
# requirement is "gen >= 0", which no data can violate. Waiting for 3 means any
# block left at gen 0 or 1 is a provable lost write.
MIN_FLUSHED=3

wait_witness() {
  for _ in $(seq 1 240); do
    [ -f "$WITNESS" ] && [ "$(cat "$WITNESS" 2>/dev/null || echo 0)" -ge "$MIN_FLUSHED" ] && return 0
    sleep 0.5
  done
  return 1
}

# run_selftest — the oracle checking itself. Everything here happens on a plain
# file, so a failure is a bug in ./crash and never in go-ublk.
run_selftest() {
  local img=/var/tmp/crash-selftest.img
  local wit=$PWD/crash-selftest.witness
  local rc=0

  echo "----- oracle self-test (plain file, no ublk) -----"
  rm -f "$img" "$wit" "$wit.tmp"
  # Preallocated and buffered on purpose: this stage tests the oracle, not any
  # I/O path, and O_DIRECT into a sparse file spends all its time in ext4 extent
  # allocation instead of producing data to check.
  fallocate -l 32M "$img" 2>/dev/null || truncate -s 32M "$img"
  "$CRASH" write -device="$img" -witness="$wit" -duration=5s -direct=false >/dev/null || {
    echo "FAIL: self-test writer errored"; return 1
  }

  expect() { # expect PASS|CLASS FILE
    local want=$1 f=$2 out
    out=$("$CRASH" check -target="$f" -witness="$wit" -min-flushed=2 2>&1) || true
    if [ "$want" = PASS ]; then
      case "$out" in
        *PASS*) echo "  ok   clean image passes" ;;
        *) echo "  FAIL clean image did not pass:"; echo "$out" | sed 's/^/       /'; rc=1 ;;
      esac
    else
      case "$out" in
        *"$want"*) echo "  ok   injected $want detected" ;;
        *) echo "  FAIL injected $want NOT detected:"; echo "$out" | sed 's/^/       /'; rc=1 ;;
      esac
    fi
  }

  expect PASS "$img"

  # A zeroed region-A block: an acknowledged, flushed write that vanished.
  cp "$img" "$img.lost"
  dd if=/dev/zero of="$img.lost" bs=4096 seek=5 count=1 conv=notrunc status=none
  expect LOST "$img.lost"

  # Block 7's contents sitting at block 9: the offset-mapping class of bug that
  # the original descriptor-mmap stride produced.
  cp "$img" "$img.alias"
  dd if="$img" of=/tmp/blk.bin bs=4096 skip=7 count=1 status=none
  dd if=/tmp/blk.bin of="$img.alias" bs=4096 seek=9 count=1 conv=notrunc status=none
  expect ALIASED "$img.alias"

  # One body byte changed while the header still claims its generation: a block
  # assembled from two different writes.
  cp "$img" "$img.torn"
  local off=$((11 * 4096 + 100)) b
  b=$(od -An -tu1 -j $off -N1 "$img.torn" | tr -d ' ')
  printf "$(printf '\\x%02x' $((b ^ 0xff)))" |
    dd of="$img.torn" bs=1 seek=$off count=1 conv=notrunc status=none
  expect TORN "$img.torn"

  rm -f "$img" "$img.lost" "$img.alias" "$img.torn" "$wit" "$wit.tmp" /tmp/blk.bin
  return $rc
}

case "$MODE" in

selftest)
  run_selftest && { echo "PASS: oracle detects lost, aliased and torn blocks"; exit 0; }
  echo "FAIL: oracle self-test failed — crash-consistency results would be meaningless"
  exit 1
  ;;

kill)
  CYCLES=${2:-6}
  SIZE=${3:-256M}
  BOOT0=$(boot_id)
  fails=0
  echo "=============================================="
  echo "  go-ublk crash consistency: SIGKILL mid-write"
  echo "  kernel: $(uname -r)  arch: $(uname -m)"
  echo "  cycles=$CYCLES size=$SIZE image=$IMG"
  echo "=============================================="

  sudo pkill -9 -x ublk-loop 2>/dev/null || true
  sudo pkill -9 -x crash 2>/dev/null || true
  reap
  sudo modprobe ublk_drv || { echo "cannot load ublk_drv"; exit 1; }

  # Establish that the oracle can fail before trusting it to say the device is
  # fine. Everything after this is only as meaningful as this passing.
  run_selftest || { echo "FAIL: oracle self-test failed, aborting"; exit 1; }

  for i in $(seq 1 "$CYCLES"); do
    flavor=buffered
    [ $((i % 2)) -eq 0 ] && flavor=sync
    echo ""
    echo "----- cycle $i/$CYCLES  flavor=$flavor -----"

    sudo rm -f "$IMG" "$WITNESS" "$WITNESS.tmp"
    dev=$(start_daemon "$flavor") || {
      echo "FAIL: device did not appear"; sed 's/^/  log| /' "$DLOG" | tail -20
      fails=$((fails+1)); sudo pkill -9 -x ublk-loop || true; reap; continue
    }
    echo "device: $dev"

    sudo setsid "$CRASH" write -device="$dev" -witness="$WITNESS" -direct \
      >"$WLOG" 2>&1 < /dev/null &
    disown 2>/dev/null || true
    if ! wait_witness; then
      echo "FAIL: fewer than $MIN_FLUSHED generations flushed within 120s"
      sed 's/^/  writer| /' "$WLOG" | tail -20
      fails=$((fails+1)); sudo pkill -9 -x crash || true
      sudo pkill -9 -x ublk-loop || true; reap; continue
    fi
    # Land the kill at a varying point inside a generation rather than always
    # just after a flush, so region A is caught mid-pass.
    sleep "0.$(( RANDOM % 9 + 1 ))"

    dpid=$(daemon_pid)
    wpid=$(pgrep -x crash | tail -1)
    gen_at_kill=$(cat "$WITNESS")
    echo "killing daemon pid=$dpid at flushed-generations=$gen_at_kill (writer pid=$wpid)"
    sudo kill -9 "$dpid" 2>/dev/null || true

    # Recovery half: the host must come back on its own. The writer is still
    # mid-pwrite on a device whose daemon just vanished; with no USER_RECOVERY
    # the kernel must abort its in-flight I/O and let it die. A writer stuck in
    # D forever is the "permanent D-state on a customer host" failure, so wait
    # for it rather than papering over it with a blind pkill.
    sudo kill -9 "$wpid" 2>/dev/null || true
    stuck=1
    for _ in $(seq 1 40); do
      [ -e "/proc/$wpid" ] || { stuck=0; break; }
      sleep 0.5
    done
    if [ "$stuck" = 1 ]; then
      echo "FAIL: writer pid=$wpid unkillable 20s after the daemon died (state=$(cut -d' ' -f3 "/proc/$wpid/stat" 2>/dev/null))"
      sudo cat "/proc/$wpid/stack" 2>/dev/null | sed 's/^/  stack| /' || true
      fails=$((fails+1))
    fi
    reap
    if ! stuckd=$(dstate_persistent); then
      echo "FAIL: task still in D-state across 5s of samples after daemon kill:"
      echo "$stuckd"
      fails=$((fails+1))
    fi
    if ls /dev/ublkb* >/dev/null 2>&1; then
      echo "FAIL: leaked device after reap: $(ls /dev/ublkb* 2>/dev/null | tr '\n' ' ')"
      fails=$((fails+1))
    fi

    # Ground truth first: the image file is what a real recovery would find on
    # disk, and reading it directly cannot be masked by a read-path bug.
    if sudo "$CRASH" check -target="$IMG" -witness="$WITNESS" -min-flushed=$MIN_FLUSHED | sed 's/^/  file| /'; then
      :
    else
      echo "FAIL: image-file check failed"; fails=$((fails+1))
    fi

    # Then the same bytes through a freshly restarted device, which also proves
    # the device comes back after an ungraceful death.
    dev2=$(start_daemon "$flavor") || {
      echo "FAIL: device did not come back on the same image"
      sed 's/^/  log| /' "$DLOG" | tail -20; fails=$((fails+1)); reap; continue
    }
    if sudo "$CRASH" check -target="$dev2" -witness="$WITNESS" -min-flushed=$MIN_FLUSHED | sed 's/^/  dev | /'; then
      :
    else
      echo "FAIL: device check failed after restart"; fails=$((fails+1))
    fi

    dpid=$(daemon_pid)
    sudo kill -INT "$dpid" 2>/dev/null || true
    for _ in $(seq 1 40); do [ -e "/proc/$dpid" ] || break; sleep 0.5; done
    if [ -e "/proc/$dpid" ]; then
      echo "FAIL: graceful stop hung after crash recovery"; fails=$((fails+1))
      sudo kill -9 "$dpid" 2>/dev/null || true
    fi
    reap

    if sudo dmesg | tail -80 | grep -qiE 'BUG:|Oops|kernel NULL|general protection'; then
      echo "FAIL: kernel complaint in dmesg:"
      sudo dmesg | tail -80 | grep -iE 'BUG:|Oops|kernel NULL|general protection' | sed 's/^/  /'
      fails=$((fails+1))
    fi
  done

  sudo rm -f "$IMG" "$WITNESS" "$WITNESS.tmp"
  BOOT1=$(boot_id)
  echo ""
  echo "=============================================="
  if [ "$BOOT0" != "$BOOT1" ]; then
    echo "  REBOOTED MID-RUN (boot_id $BOOT0 -> $BOOT1) — results void"
    exit 1
  fi
  if [ "$fails" -eq 0 ]; then
    echo "  PASS: $CYCLES cycles, no lost/torn/aliased data, clean recovery"
    echo "  (SIGKILL keeps the host page cache, so this proves the ublk path"
    echo "   and recovery, not that fsync reached the disk — see 'arm'/'verify'.)"
    exit 0
  fi
  echo "  FAIL: $fails failure(s) across $CYCLES cycles"
  exit 1
  ;;

arm)
  SIZE=${2:-256M}
  echo "arming power-fail test: image=$IMG size=$SIZE"
  sudo pkill -9 -x ublk-loop 2>/dev/null || true
  sudo pkill -9 -x crash 2>/dev/null || true
  reap
  sudo modprobe ublk_drv || { echo "cannot load ublk_drv"; exit 1; }
  sudo rm -f "$IMG" "$WITNESS" "$WITNESS.tmp"

  dev=$(start_daemon buffered) || {
    echo "FAIL: device did not appear"; sed 's/^/  log| /' "$DLOG" | tail -20; exit 1
  }
  sudo setsid "$CRASH" write -device="$dev" -witness="$WITNESS" -direct \
    >"$WLOG" 2>&1 < /dev/null &
  disown 2>/dev/null || true
  wait_witness || { echo "FAIL: fewer than $MIN_FLUSHED generations flushed within 120s"; exit 1; }
  echo "boot_id=$(boot_id)"
  echo "ARMED device=$dev flushed-generations=$(cat "$WITNESS") — reset the machine now"
  ;;

verify)
  SIZE=${2:-256M}
  echo "=============================================="
  echo "  go-ublk crash consistency: after hard reset"
  echo "  kernel: $(uname -r)  boot_id=$(boot_id)"
  echo "=============================================="
  [ -f "$IMG" ] || { echo "FAIL: $IMG did not survive the reset"; exit 1; }
  [ -f "$WITNESS" ] || { echo "FAIL: witness $WITNESS did not survive the reset"; exit 1; }
  echo "surviving witness: flushed-generations=$(cat "$WITNESS")"

  reap
  sudo modprobe ublk_drv || { echo "cannot load ublk_drv"; exit 1; }
  fails=0

  # The page cache is genuinely gone here, so this is the real durability test:
  # every generation the writer flushed must have reached the disk.
  sudo "$CRASH" check -target="$IMG" -witness="$WITNESS" -min-flushed=$MIN_FLUSHED | sed 's/^/  file| /' || fails=$((fails+1))

  # Sensitivity control on the surviving image itself: claim two more
  # generations were flushed than really were, which makes every region-A block
  # too old. That check MUST fail. If it passes, the one above was not actually
  # constraining anything and its PASS meant nothing.
  echo $(( $(cat "$WITNESS") + 2 )) > /tmp/control.witness
  if sudo "$CRASH" check -target="$IMG" -witness=/tmp/control.witness >/tmp/control.log 2>&1; then
    echo "  FAIL control: an over-claiming witness still passed — the durability check is not live"
    fails=$((fails+1))
  elif grep -q LOST /tmp/control.log; then
    echo "  ok   control: over-claiming witness correctly reports LOST"
  else
    echo "  FAIL control: over-claiming witness failed for the wrong reason:"
    sed 's/^/       /' /tmp/control.log
    fails=$((fails+1))
  fi
  rm -f /tmp/control.witness /tmp/control.log

  dev=$(start_daemon buffered) || { echo "FAIL: device did not come up on the surviving image"; exit 1; }
  sudo "$CRASH" check -target="$dev" -witness="$WITNESS" -min-flushed=$MIN_FLUSHED | sed 's/^/  dev | /' || fails=$((fails+1))
  dpid=$(daemon_pid); sudo kill -INT "$dpid" 2>/dev/null || true
  for _ in $(seq 1 40); do [ -e "/proc/$dpid" ] || break; sleep 0.5; done
  reap
  sudo rm -f "$IMG" "$WITNESS" "$WITNESS.tmp"

  echo ""
  [ "$fails" -eq 0 ] && { echo "PASS: every flushed generation survived a hard reset"; exit 0; }
  echo "FAIL: $fails check(s) failed after hard reset"
  exit 1
  ;;

*)
  echo "usage: $0 {kill [CYCLES] [SIZE] | arm [SIZE] | verify [SIZE]}"
  exit 2
  ;;
esac
