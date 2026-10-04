#!/bin/bash
# Soak test: hours of verified filesystem I/O through the shipped systemd
# units, with server crashes and upgrade handoffs on a schedule.
#
#   ./vm-soak.sh [HOURS] [SIZE] [EVENT_EVERY_S]      (as a user with sudo)
#
# HOURS may be fractional; phase 2's leak check needs at least ~25 minutes.
#
# Installs ./ublk-loop and the units from ./systemd/ (ublk-loop@.service,
# srv-ublk0.mount), creates ext4 on a SIZE image, and runs fio with crc32c
# verification on it for HOURS:
#
#   phase 1 (first half)   every EVENT_EVERY_S seconds, alternately hand the
#                          device to a new server (SIGUSR2, the upgrade path)
#                          and SIGKILL the server (the crash path); systemd
#                          restarts it and the new process recovers the device.
#   phase 2 (second half)  no events; samples the long-lived server's RSS and
#                          open fds every minute to catch leaks.
#
# PASS requires: fio exits 0 with no errors (every block it wrote verified),
# every event recovered within 60s, no I/O errors, oopses or hung tasks in the
# kernel log, RSS within 2x and fds unchanged across phase 2, a clean e2fsck,
# and a clean stop. Destroys /var/lib/ublk/0.img and /dev/ublkb0's contents.

set -uo pipefail

HOURS=${1:-4}
SIZE=${2:-2G}
EVERY=${3:-300}
UNIT=ublk-loop@0
MNT=/srv/ublk0
FIOLOG=/tmp/soak-fio.log
SAMPLES=/tmp/soak-samples.txt

fail=0
check() { if [ "$1" = 0 ]; then echo "  ok   $2"; else echo "  FAIL $2"; fail=1; fi; }
mainpid() { systemctl show -p MainPID --value $UNIT; }
# newpid OLD — waits up to 60s for systemd to report a different, running main
# process, and prints it (or TIMEOUT).
newpid() {
  local p _
  for _ in $(seq 1 600); do
    p=$(mainpid)
    if [ "$p" != "$1" ] && [ "$p" != 0 ] && [ "$(systemctl is-active $UNIT)" = active ]; then
      echo "$p"; return
    fi
    sleep 0.1
  done
  echo TIMEOUT
}

echo "== soak: ${HOURS}h, $SIZE image, an event every ${EVERY}s in phase 1"
command -v fio >/dev/null || { echo "FAIL: fio not installed"; exit 1; }

# Clean slate: our units stopped, no ublk devices left from earlier runs.
sudo systemctl stop srv-ublk0.mount $UNIT 2>/dev/null
sudo ./ublk-loop -del=all >/dev/null 2>&1
sudo install -m 0755 ./ublk-loop /usr/local/bin/ublk-loop
sudo install -m 0644 systemd/ublk-loop@.service systemd/srv-ublk0.mount /etc/systemd/system/
sudo systemctl daemon-reload
sudo mkdir -p /var/lib/ublk "$MNT"
sudo rm -f /var/lib/ublk/0.img && sudo truncate -s "$SIZE" /var/lib/ublk/0.img
sudo systemctl start $UNIT || { echo "FAIL: $UNIT did not start"; exit 1; }
sudo mkfs.ext4 -q -F /dev/ublkb0 && sudo systemctl start srv-ublk0.mount || { echo "FAIL: mount"; exit 1; }
sudo dmesg -C

# Teeth: prove fio's verification catches corruption on this filesystem before
# trusting its silence for hours. Write a file, verify it untouched (must pass),
# corrupt one block underneath fio and verify again (must fail).
st="--name=selftest --filename=$MNT/selftest --rw=write --bs=16k --size=16M --ioengine=libaio --direct=1 --verify=crc32c"
sudo fio $st --do_verify=0 >/dev/null 2>&1 && sudo fio $st --verify_only >/dev/null 2>&1
check $? "selftest: fio verifies an untouched file"
sudo dd if=/dev/urandom of=$MNT/selftest bs=4k count=1 seek=100 conv=notrunc oflag=direct status=none
sudo fio $st --verify_only >/dev/null 2>&1
check $(( $? == 0 )) "selftest: fio catches one corrupted block"
sudo rm -f $MNT/selftest
[ $fail = 0 ] || { echo "FAIL: the verifier cannot be trusted; not soaking"; exit 1; }

runtime=$(echo "$HOURS * 3600 / 1" | bc)
half=$((runtime / 2))
# Two writers verifying their own blocks as they go (verify_backlog), plus a
# final verify pass. time_based keeps them writing for the whole soak.
sudo fio --name=soak --directory=$MNT --rw=randwrite --bs=16k --size=256M \
  --numjobs=2 --iodepth=16 --ioengine=libaio --direct=1 \
  --verify=crc32c --verify_backlog=1024 --verify_fatal=1 --do_verify=1 \
  --time_based --runtime=$runtime > $FIOLOG 2>&1 &
FIO=$!

# $FIO is sudo's pid, a root process: kill -0 on it fails with EPERM for us.
running() { ps -p "$FIO" >/dev/null; }

start=$(date +%s)
events=0
event_fail=0
n=0
echo "-- phase 1: events"
while [ $(( $(date +%s) - start )) -lt $half ] && running; do
  sleep "$EVERY"
  [ $(( $(date +%s) - start )) -lt $half ] || break
  n=$((n + 1))
  old=$(mainpid)
  t0=$(date +%s.%N)
  if [ $((n % 2)) = 1 ]; then sudo systemctl kill -s SIGUSR2 $UNIT; what=handoff; else sudo kill -9 "$old"; what=crash; fi
  new=$(newpid "$old")
  events=$((events + 1))
  [ "$new" = TIMEOUT ] && event_fail=$((event_fail + 1))
  printf '   %s %-7s %s -> %s in %.1fs\n' "$(date +%T)" "$what" "$old" "$new" "$(echo "$(date +%s.%N) - $t0" | bc)"
done

echo "-- phase 2: steady state"
: > $SAMPLES
while running; do
  p=$(mainpid)
  echo "$(date +%s) $p $(ps -o rss= -p "$p" | tr -d ' ') $(sudo ls /proc/"$p"/fd 2>/dev/null | wc -l)" >> $SAMPLES
  sleep 60
done
wait $FIO
fio_rc=$?

echo "-- results"
check $fio_rc "fio exited 0 (every verified block matched)"
grep -q 'err= 0' $FIOLOG && ! grep -qE 'err= *[1-9]|verify: bad' $FIOLOG
check $? "fio reported no errors"
check $(( events == 0 )) "phase 1 ran at least one event"
check $(( event_fail > 0 )) "all $events events recovered within 60s"
# Phase 2 leak check: compare the first sample 10 minutes in (warm) to the last,
# for the same process.
read -r _ p0 rss0 fd0 < <(sed -n 11p $SAMPLES)
read -r _ p1 rss1 fd1 < <(tail -1 $SAMPLES)
if [ -n "${p0:-}" ] && [ "$p0" = "$p1" ]; then
  echo "       server $p0: RSS ${rss0}K -> ${rss1}K, fds $fd0 -> $fd1 over $(wc -l < $SAMPLES) samples"
  check $(( rss1 > 2 * rss0 )) "RSS stayed within 2x in phase 2"
  check $(( fd1 != fd0 )) "open fds unchanged in phase 2"
else
  check 1 "one server process lived through phase 2 (first ${p0:-none}, last ${p1:-none})"
fi
# Oopses and hung tasks anywhere count; I/O errors only on our device, since
# other tests on the same machine may fail I/O on theirs on purpose.
kerr=$(sudo dmesg | grep -E 'BUG:|WARNING:|Oops|blocked for more than|UBSAN|(I/O|EXT4-fs) error.*ublkb0\b' | head -5)
[ -z "$kerr" ]
check $? "kernel log clean${kerr:+: $kerr}"
sudo systemctl stop srv-ublk0.mount
sudo e2fsck -fn /dev/ublkb0 > /tmp/soak-fsck.log 2>&1
check $? "e2fsck -fn clean"
sudo systemctl stop $UNIT
[ "$(systemctl is-active $UNIT)" != active ] && ! ls /dev/ublkb0 >/dev/null 2>&1
check $? "stopped cleanly, device deleted"

if [ $fail = 0 ]; then echo "PASS: soak ${HOURS}h, $events events"; else echo "FAIL: soak"; fi
exit $fail
