#!/bin/bash
# Systemd shutdown-storm driver for go-ublk (TODO Phase 3).
#
# The one teardown path never exercised: a host reboot while a ublk device is
# serving I/O. systemd SIGTERMs every unit, waits out its timeout, SIGKILLs the
# stragglers, and unmounts filesystems — all while the daemon backing a mounted
# block device is being killed underneath the writeback it still owes. This is
# where the single real x86 oops (io_req_uring_cleanup NULL-deref, 6.17.0-14)
# actually came from, and "the host rebooted mid-backup" is an ordinary BCDR
# event rather than an exotic one.
#
#   arm [SIZE]   Build the storm and leave it running: ublk-loop serving a
#                multi-queue device, an ext4 filesystem mounted on it, buffered
#                and O_DIRECT fio jobs writing into that filesystem, then print
#                ARMED and exit. The caller then reboots the machine normally
#                (systemctl reboot) — NOT sysrq, the whole point is that systemd
#                runs its full shutdown sequence.
#   check        After that reboot: scan the previous boot for a kernel oops,
#                measure how long the shutdown actually took, and assert the
#                host came back clean (no leaked device, no persistent D-state,
#                boot_id changed).
#   selftest     Prove `check` can fail. Installs a late-shutdown unit that
#                fires sysrq-l (show-backtrace-all-active-cpus) so a real kernel
#                backtrace is emitted at the same point in shutdown a real oops
#                would be, then asserts `check` REPORTS it. Without this the
#                whole test is indistinguishable from a scanner that matches
#                nothing.
#
# Detection has two independent channels because neither alone is sufficient:
#   - journalctl -b -1 -k. Persistent (needs /var/log/journal, asserted below),
#     but journald is itself a unit: anything the kernel prints after journald
#     stops, or after / goes read-only, never reaches the disk.
#   - The host's serial log. The kernel console is on hvc0, which the Lima host
#     captures to ~/.lima/<vm>/serialv.log; that file is written by the VMM, so
#     it survives a guest that is too dead to write anything. The host side of
#     this test (the Makefile target) scans it. This script asserts the console
#     is actually wired to hvc0, because a guest without it makes the host scan
#     silently vacuous.

set -uo pipefail

MODE=${1:-arm}
SIZE=${2:-1G}
UBLK_LOOP=./ublk-loop
IMG=/var/tmp/storm.img
MNT=/mnt/ublk-storm
DLOG=/var/tmp/storm-daemon.log
STAMP=/var/tmp/storm.armed
SYSRQ_UNIT=/etc/systemd/system/ublk-storm-sysrq.service

# Kernel-taint patterns worth failing on. Deliberately does NOT include the
# "Buffer I/O error ... lost async page write" that a killed daemon always
# produces — that is the block layer correctly reporting writes it cannot
# complete, i.e. the graceful-degradation behavior, not a kernel defect.
OOPS_RE='Oops|BUG:|WARNING:|Call trace:|Unable to handle|Internal error|general protection|kernel NULL pointer|KASAN|refcount_t|list_add corruption|soft lockup|hung_task'

boot_id() { cat /proc/sys/kernel/random/boot_id; }
reap() { sudo "$UBLK_LOOP" -del=all >/dev/null 2>&1 || true; }

# dstate_persistent — a task in D in *every* sample over 5s. Same reasoning as
# vm-crash.sh: kernel workqueue threads dip into D as normal operation, so a
# single sample reports unrelated kworkers as a go-ublk wedge.
dstate_persistent() {
  local acc="" cur i
  for i in $(seq 1 10); do
    cur=$(ps -eo state=,pid= | awk '$1 ~ /^D/ {print $2}' | sort)
    if [ "$i" = 1 ]; then acc="$cur"; else acc=$(comm -12 <(echo "$acc") <(echo "$cur")); fi
    [ -z "$acc" ] && return 0
    sleep 0.5
  done
  [ -z "$acc" ] && return 0
  echo "$acc"
  return 1
}

require_console_hvc0() {
  if ! grep -qw hvc0 /proc/consoles; then
    echo "FAIL: kernel console is not on hvc0 (/proc/consoles: $(tr -s ' \n' ' ' < /proc/consoles))"
    echo "      The host serial-log scan would be vacuous. Add console=hvc0 to the"
    echo "      kernel cmdline (see /etc/default/grub.d/99-ublk-console.cfg) and reboot."
    return 1
  fi
  return 0
}

require_persistent_journal() {
  if [ ! -d /var/log/journal ]; then
    echo "FAIL: /var/log/journal missing, journald is volatile — the previous boot's"
    echo "      kernel log will not survive the reboot this test depends on."
    return 1
  fi
  return 0
}

#-----------------------------------------------------------------------------
# arm — build the storm and leave it running
#-----------------------------------------------------------------------------
arm_storm() {
  require_console_hvc0 || return 1
  require_persistent_journal || return 1

  command -v fio >/dev/null || { echo "FAIL: fio not installed"; return 1; }
  command -v mkfs.ext4 >/dev/null || { echo "FAIL: mkfs.ext4 not installed"; return 1; }

  # This test reboots between every cycle and ublk_drv is not autoloaded, so
  # arming without this fails at /dev/ublk-control on every cycle after the first.
  sudo modprobe ublk_drv 2>/dev/null
  [ -c /dev/ublk-control ] || { echo "FAIL: /dev/ublk-control absent after modprobe ublk_drv"; return 1; }

  sudo pkill -9 -x ublk-loop 2>/dev/null
  sudo umount "$MNT" 2>/dev/null
  reap

  # A plain file sitting at /dev/ublkb0 stops udev creating the real block node,
  # so the daemon reports "Device created: /dev/ublkb0" while the node the test
  # waits for never appears — which reads exactly like a device-creation hang.
  # Any `dd of=/dev/ublkb0` that runs when the device is absent leaves one behind.
  if [ -e /dev/ublkb0 ] && [ ! -b /dev/ublkb0 ]; then
    echo "storm: removing a non-block file squatting at /dev/ublkb0"
    sudo rm -f /dev/ublkb0
  fi

  sudo rm -f "$IMG"
  sudo mkdir -p "$MNT"

  echo "storm: creating $SIZE image at $IMG"
  sudo truncate -s "$SIZE" "$IMG"

  # Baseline the I/O-error count BEFORE the daemon starts. dmesg is per-boot, not
  # per-cycle, so an absolute count charges this cycle for every error the
  # previous cycle's teardown produced — and teardown always produces some.
  local errbase
  errbase=$(sudo dmesg | grep -ac 'I/O error, dev ublkb0')

  # Buffered + volatile cache on purpose: that leaves dirty pages the kernel
  # still owes writeback for when systemd starts killing things, which is the
  # pressure the real event applies. -sync would quietly defuse the test.
  # The daemon writes DIRECTLY to the console, which the Lima host captures to
  # ~/.lima/<vm>/serialv.log. Two earlier attempts at this failed for reasons
  # worth keeping:
  #   - a plain file ($DLOG) is rolled back by ext4 replay when the storm wedges
  #     the machine, so the one cycle whose log matters is exactly the one lost;
  #   - a `tail -F $DLOG > /dev/console` mirror is a process in the same session
  #     scope, so systemd kills it at the START of shutdown — precisely before
  #     the window we are trying to observe.
  # Writing straight to the console has no intermediary that can be killed, and
  # the host file cannot be rolled back. A Go panic or fatal error during
  # teardown lands there.
  sudo sh -c "exec '$UBLK_LOOP' -file='$IMG' -queues=4 -depth=64 > /dev/console 2>&1" < /dev/null &
  for i in $(seq 1 240); do
    [ -b /dev/ublkb0 ] && break
    sleep 0.5
  done
  if [ ! -b /dev/ublkb0 ]; then
    echo "FAIL: /dev/ublkb0 never appeared"
    sudo dmesg | tail -10
    return 1
  fi
  local dpid
  dpid=$(pgrep -x ublk-loop | tail -1)
  echo "storm: device up, daemon pid=$dpid"

  sudo mkfs.ext4 -q -F /dev/ublkb0 >/dev/null 2>&1 || { echo "FAIL: mkfs.ext4"; return 1; }
  sudo mount /dev/ublkb0 "$MNT" || { echo "FAIL: mount"; return 1; }
  echo "storm: ext4 mounted at $MNT"

  # A filesystem mounted on the ublk device is the realism that matters: systemd
  # must unmount it during shutdown, which forces writeback through the very
  # daemon it is concurrently terminating.
  sudo fio --name=storm-buffered --directory="$MNT" --rw=randwrite --bs=64k \
    --size=64M --numjobs=4 --iodepth=32 --ioengine=libaio --direct=0 \
    --time_based --runtime=900 --group_reporting > /var/tmp/storm-fio-buf.log 2>&1 &
  sudo fio --name=storm-direct --filename=/dev/ublkb0 --rw=randwrite --bs=4k \
    --offset=512M --size=128M --numjobs=2 --iodepth=32 --ioengine=libaio --direct=1 \
    --time_based --runtime=900 --group_reporting > /var/tmp/storm-fio-dir.log 2>&1 &

  sleep 8
  if ! pgrep -x fio >/dev/null; then
    echo "FAIL: no fio running after 8s — no load means no storm"
    tail -20 /var/tmp/storm-fio-buf.log
    return 1
  fi

  # Arming on an already-broken device proves nothing about shutdown: whatever
  # the reboot then finds was true before the reboot. So assert the device is
  # healthy at the moment we hand it to the storm, and treat a device that died
  # during arm as its own (more interesting) failure rather than as setup noise.
  if ! pgrep -x ublk-loop >/dev/null; then
    echo "FAIL: the daemon died during arm, before any shutdown was involved"
    echo "--- console has the daemon output; see the host serial log ---"
    return 1
  fi
  local armerr
  armerr=$(( $(sudo dmesg | grep -ac 'I/O error, dev ublkb0') - errbase ))
  if [ "$armerr" -gt 0 ]; then
    echo "FAIL: $armerr new block-layer I/O errors on ublkb0 during arm, before any shutdown."
    echo "      The device broke under plain load; a shutdown result on top of this"
    echo "      would be measuring the wrong thing."
    echo "--- kernel ---"; sudo dmesg | grep -a 'ublkb0' | tail -15
    echo "--- console has the daemon output; see the host serial log ---"
    return 1
  fi

  # Record what we armed so `check` can tell a real storm from a no-op run.
  # armed_at is journalctl's --since format on purpose: the scan is scoped to
  # the storm window, because a boot contains plenty of kernel chatter that has
  # nothing to do with this test. Anchoring on arm time is what stops earlier
  # activity in the same boot from being reported as a shutdown-storm failure —
  # which is exactly what `systemctl disable --now` on the selftest injector
  # did, firing a backtrace into the boot the first real cycle then inspected.
  sudo tee "$STAMP" >/dev/null <<EOF
boot_id=$(boot_id)
daemon_pid=$dpid
armed_at=$(date '+%Y-%m-%d %H:%M:%S')
fio_procs=$(pgrep -xc fio)
EOF

  echo "storm: fio running ($(pgrep -xc fio) procs), dirty=$(grep -E '^Dirty:' /proc/meminfo | awk '{print $2" "$3}')"
  echo "ARMED"
  return 0
}

#-----------------------------------------------------------------------------
# arm-unit — the same storm, deployed the way you would actually ship it
#
# `arm` runs the daemon as a bare background process inside the ssh login
# session's scope, which systemd tears down early in shutdown — so the daemon
# dies while the filesystem above it still owes writeback. That is a real
# scenario, but it is a DEPLOYMENT shape, not an inherent property of ublk, and
# a result from it cannot distinguish "go-ublk wedges reboots" from "this is the
# wrong way to run it". This mode is the control that separates them: the daemon
# becomes a systemd service, and the mount declares After=/Requires= on it, so
# systemd's stop order (the reverse of start order) unmounts the filesystem
# BEFORE it stops the daemon that backs it. If the storm is clean here and dirty
# under `arm`, the answer for a production deployment is a correct unit file.
#-----------------------------------------------------------------------------
SERVICE_UNIT=/etc/systemd/system/ublk-storm.service
MOUNT_UNIT_NAME=$(systemd-escape -p --suffix=mount /mnt/ublk-storm 2>/dev/null)

arm_storm_unit() {
  require_console_hvc0 || return 1
  require_persistent_journal || return 1
  command -v fio >/dev/null || { echo "FAIL: fio not installed"; return 1; }

  sudo modprobe ublk_drv 2>/dev/null
  [ -c /dev/ublk-control ] || { echo "FAIL: /dev/ublk-control absent"; return 1; }

  sudo systemctl stop "$MOUNT_UNIT_NAME" 2>/dev/null
  sudo systemctl stop ublk-storm.service 2>/dev/null
  sudo pkill -9 -x ublk-loop 2>/dev/null
  sudo umount "$MNT" 2>/dev/null
  reap
  if [ -e /dev/ublkb0 ] && [ ! -b /dev/ublkb0 ]; then sudo rm -f /dev/ublkb0; fi
  sudo rm -f "$IMG"
  sudo mkdir -p "$MNT"

  echo "storm(unit): creating $SIZE image at $IMG"
  sudo truncate -s "$SIZE" "$IMG"

  # Conflicts/Before umount.target with DefaultDependencies=no keeps the daemon
  # alive until unmounting actually begins, instead of being swept up with the
  # ordinary services at the start of the shutdown transaction.
  sudo tee "$SERVICE_UNIT" >/dev/null <<EOF
[Unit]
Description=go-ublk shutdown-storm daemon (correctly ordered)
DefaultDependencies=no
After=local-fs.target
Before=umount.target
Conflicts=umount.target

[Service]
Type=simple
ExecStart=$PWD/ublk-loop -file=$IMG -queues=4 -depth=64
KillMode=mixed
TimeoutStopSec=90
Restart=no

[Install]
WantedBy=multi-user.target
EOF

  # The load-bearing line is the mount's After=/Requires= on the service: stop
  # order is the reverse of start order, so the filesystem unmounts first.
  sudo tee "/etc/systemd/system/$MOUNT_UNIT_NAME" >/dev/null <<EOF
[Unit]
Description=go-ublk shutdown-storm filesystem
Requires=ublk-storm.service
After=ublk-storm.service
Before=umount.target
Conflicts=umount.target
DefaultDependencies=no

[Mount]
What=/dev/ublkb0
Where=$MNT
Type=ext4
Options=defaults
EOF

  sudo systemctl daemon-reload
  sudo systemctl start ublk-storm.service || { echo "FAIL: could not start ublk-storm.service"; return 1; }
  sudo setsid sh -c "journalctl -u ublk-storm.service -f -n0 > /dev/console 2>/dev/null" < /dev/null > /dev/null 2>&1 &

  local i
  for i in $(seq 1 240); do
    [ -b /dev/ublkb0 ] && break
    sleep 0.5
  done
  [ -b /dev/ublkb0 ] || { echo "FAIL: /dev/ublkb0 never appeared"; sudo journalctl -u ublk-storm.service -n 20 --no-pager; return 1; }
  echo "storm(unit): device up under systemd"

  sudo mkfs.ext4 -q -F /dev/ublkb0 >/dev/null 2>&1 || { echo "FAIL: mkfs.ext4"; return 1; }
  sudo systemctl start "$MOUNT_UNIT_NAME" || { echo "FAIL: could not start $MOUNT_UNIT_NAME"; return 1; }
  echo "storm(unit): ext4 mounted via $MOUNT_UNIT_NAME"

  sudo fio --name=storm-buffered --directory="$MNT" --rw=randwrite --bs=64k \
    --size=64M --numjobs=4 --iodepth=32 --ioengine=libaio --direct=0 \
    --time_based --runtime=900 --group_reporting > /var/tmp/storm-fio-buf.log 2>&1 &
  sudo fio --name=storm-direct --filename=/dev/ublkb0 --rw=randwrite --bs=4k \
    --offset=512M --size=128M --numjobs=2 --iodepth=32 --ioengine=libaio --direct=1 \
    --time_based --runtime=900 --group_reporting > /var/tmp/storm-fio-dir.log 2>&1 &

  sleep 8
  pgrep -x fio >/dev/null || { echo "FAIL: no fio running after 8s"; return 1; }
  pgrep -x ublk-loop >/dev/null || { echo "FAIL: daemon died during arm"; sudo journalctl -u ublk-storm.service -n 30 --no-pager; return 1; }

  sudo tee "$STAMP" >/dev/null <<EOF
boot_id=$(boot_id)
daemon_pid=$(pgrep -x ublk-loop | tail -1)
armed_at=$(date '+%Y-%m-%d %H:%M:%S')
fio_procs=$(pgrep -xc fio)
mode=unit
EOF
  echo "storm(unit): fio running ($(pgrep -xc fio) procs), dirty=$(awk '/^Dirty:/{print $2}' /proc/meminfo)kB"
  echo "ARMED"
  return 0
}

remove_storm_units() {
  sudo systemctl stop "$MOUNT_UNIT_NAME" 2>/dev/null
  sudo systemctl disable --now ublk-storm.service 2>/dev/null
  sudo rm -f "$SERVICE_UNIT" "/etc/systemd/system/$MOUNT_UNIT_NAME"
  sudo systemctl daemon-reload
  echo "storm units removed"
}

#-----------------------------------------------------------------------------
# check — post-reboot forensics
#-----------------------------------------------------------------------------
check_storm() {
  local fails=0 expect_detect=${EXPECT_DETECT:-0}

  if [ ! -f "$STAMP" ]; then
    echo "FAIL: $STAMP missing — nothing was armed, so this proves nothing"
    return 1
  fi
  local armed_boot armed_at armed_mode
  armed_boot=$(grep '^boot_id=' "$STAMP" | cut -d= -f2)
  armed_at=$(grep '^armed_at=' "$STAMP" | cut -d= -f2-)
  armed_mode=$(grep '^mode=' "$STAMP" | cut -d= -f2)
  # Which deployment shape produced this result decides what it means, so say so
  # rather than leaving two very different runs looking identical in a log.
  echo "  (deployment: ${armed_mode:-bare background process in the ssh session scope})"

  echo "=== 1. the reboot actually happened ==="
  if [ "$armed_boot" = "$(boot_id)" ]; then
    echo "  FAIL: boot_id unchanged ($armed_boot) — the machine never rebooted"
    fails=$((fails + 1))
  else
    echo "  PASS: boot_id changed ($armed_boot -> $(boot_id))"
  fi

  echo "=== 2. previous boot's kernel log ==="
  if ! journalctl --list-boots 2>/dev/null | grep -q '^ *-1'; then
    echo "  FAIL: no previous boot in the journal — cannot inspect the shutdown"
    fails=$((fails + 1))
  else
    local hits
    echo "  (scanning boot -1 from arm time: $armed_at)"
    hits=$(journalctl -b -1 -k --since="$armed_at" --no-pager 2>/dev/null | grep -aEi "$OOPS_RE")
    if [ -n "$hits" ]; then
      echo "  DETECTED kernel taint during the previous boot:"
      echo "$hits" | tail -40 | sed 's/^/    /'
      if [ "$expect_detect" = 1 ]; then
        echo "  PASS (selftest): the scanner reported the injected backtrace"
      else
        echo "  FAIL: kernel taint across the shutdown storm"
        fails=$((fails + 1))
      fi
    else
      if [ "$expect_detect" = 1 ]; then
        echo "  FAIL (selftest): injected a kernel backtrace and the scanner saw NOTHING."
        echo "        The detector is blind; a clean result from the real run would be meaningless."
        fails=$((fails + 1))
      else
        echo "  PASS: no oops/BUG/WARNING/call-trace in the previous boot"
      fi
    fi
  fi

  echo "=== 3. how long the shutdown took ==="
  # A ublk teardown that wedges shows up as systemd waiting out its 90s unit
  # stop timeout, even when nothing oopses. Measured from the last journal entry
  # of the previous boot back to the start of the shutdown transaction.
  local shutdown_start shutdown_end
  shutdown_start=$(journalctl -b -1 --no-pager -o short-unix 2>/dev/null | \
    grep -aE 'Stopped target|Reached target Shutdown|systemd-shutdown' | head -1 | cut -d. -f1)
  shutdown_end=$(journalctl -b -1 --no-pager -o short-unix 2>/dev/null | tail -1 | cut -d. -f1)
  if [ -n "$shutdown_start" ] && [ -n "$shutdown_end" ]; then
    local dur=$((shutdown_end - shutdown_start))
    echo "  shutdown sequence spanned ${dur}s"
    if [ "$dur" -ge 85 ]; then
      echo "  FAIL: >=85s means systemd waited out a unit-stop timeout — something did not stop"
      fails=$((fails + 1))
    else
      echo "  PASS: no unit-stop timeout"
    fi
  else
    echo "  (could not bracket the shutdown in the journal; skipping)"
  fi
  echo "  --- forced-unmount / timeout evidence, if any ---"
  journalctl -b -1 --no-pager 2>/dev/null | \
    grep -aEi 'timed out|killing|still running|Failed unmounting|forcibly' | tail -12 | sed 's/^/    /' || true

  echo "=== 4. writeback the shutdown could not complete ==="
  # Reported, deliberately NOT failed on. A daemon started as a bare background
  # process is not a systemd unit, so nothing orders it after the filesystem
  # mounted on top of it: systemd kills ublk-loop, THEN unmounts, and the
  # unmount's writeback has nowhere to go. That is a packaging problem in how
  # the daemon is run, not a defect in the ublk path, and it is precisely what a
  # production deployment must fix with a unit ordered against umount.target.
  # The count is printed because "how much did a clean reboot lose" is the
  # number a BCDR product actually cares about.
  local ioerr
  ioerr=$(journalctl -b -1 -k --since="$armed_at" --no-pager 2>/dev/null | \
    grep -ac 'I/O error, dev ublkb0')
  echo "  $ioerr block-layer I/O errors on ublkb0 during shutdown"
  if [ "$ioerr" -gt 0 ]; then
    echo "  NOTE: the daemon died before the filesystem above it was unmounted."
    echo "        Expected for a bare background process; a production deployment"
    echo "        needs a systemd unit ordered After= the mount / Before=umount.target."
  fi

  echo "=== 5. host came back clean ==="
  local leaked
  leaked=$(ls /dev/ublkb* 2>/dev/null | wc -l | tr -d ' ')
  if [ "$leaked" != "0" ]; then
    echo "  FAIL: $leaked leaked block device(s) after reboot: $(ls /dev/ublkb* 2>/dev/null | tr '\n' ' ')"
    fails=$((fails + 1))
  else
    echo "  PASS: no leaked block devices"
  fi
  local dpids
  if dpids=$(dstate_persistent); then
    echo "  PASS: no persistent D-state tasks"
  else
    echo "  FAIL: tasks stuck in D across 5s: $dpids"
    ps -o pid=,stat=,comm= -p $(echo "$dpids" | tr '\n' ',' | sed 's/,$//') 2>/dev/null | sed 's/^/    /'
    fails=$((fails + 1))
  fi
  if sudo modprobe ublk_drv 2>/dev/null && [ -c /dev/ublk-control ]; then
    echo "  PASS: ublk_drv still loadable, /dev/ublk-control present"
  else
    echo "  FAIL: ublk_drv unusable after the storm"
    fails=$((fails + 1))
  fi

  echo "=== 6. a device still works after the storm ==="
  sudo rm -f /var/tmp/storm-post.img
  sudo truncate -s 256M /var/tmp/storm-post.img
  sudo "$UBLK_LOOP" -file=/var/tmp/storm-post.img -queues=4 -depth=64 > /var/tmp/storm-post.log 2>&1 &
  local ok=0 i
  for i in $(seq 1 240); do
    [ -b /dev/ublkb0 ] && { ok=1; break; }
    sleep 0.5
  done
  if [ "$ok" = 1 ] && sudo dd if=/dev/urandom of=/dev/ublkb0 bs=1M count=8 oflag=direct status=none 2>/dev/null; then
    echo "  PASS: new device created and written after the storm"
  else
    echo "  FAIL: could not create/write a device after the storm"
    tail -10 /var/tmp/storm-post.log | sed 's/^/    /'
    fails=$((fails + 1))
  fi
  sudo pkill -x ublk-loop 2>/dev/null
  sleep 2
  reap
  sudo rm -f /var/tmp/storm-post.img "$STAMP"

  echo
  if [ "$fails" = 0 ]; then
    echo "=============================================="
    echo "  SHUTDOWN STORM: PASS"
    echo "=============================================="
    return 0
  fi
  echo "=============================================="
  echo "  SHUTDOWN STORM: FAIL ($fails check(s))"
  echo "=============================================="
  return 1
}

#-----------------------------------------------------------------------------
# selftest — install / remove the late-shutdown backtrace injector
#-----------------------------------------------------------------------------
install_sysrq_unit() {
  # Ordered against the same late-shutdown targets a ublk teardown oops would
  # land in, so a PASS here says the channel carries a kernel trace emitted at
  # that point — not merely that grep works on a file we wrote ourselves.
  sudo tee "$SYSRQ_UNIT" >/dev/null <<'EOF'
[Unit]
Description=go-ublk shutdown-storm selftest: emit a kernel backtrace late in shutdown
DefaultDependencies=no
Before=shutdown.target umount.target final.target
Conflicts=reboot.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/true
ExecStop=/bin/sh -c "echo 1 > /proc/sys/kernel/sysrq; echo l > /proc/sysrq-trigger"

[Install]
WantedBy=multi-user.target
EOF
  sudo systemctl daemon-reload
  sudo systemctl enable --now ublk-storm-sysrq.service >/dev/null 2>&1
  echo "selftest: late-shutdown backtrace injector installed"
}

remove_sysrq_unit() {
  # Neutralize ExecStop BEFORE stopping the unit. `systemctl disable --now` runs
  # ExecStop, so removing the injector the obvious way fires one last backtrace
  # into the current boot — the boot the next cycle inspects as its boot -1.
  # That cost a false "kernel taint across the shutdown storm" the first time.
  sudo tee "$SYSRQ_UNIT" >/dev/null <<'EOF'
[Unit]
Description=go-ublk shutdown-storm selftest injector (neutralized for removal)
DefaultDependencies=no

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/true
ExecStop=/bin/true
EOF
  sudo systemctl daemon-reload
  sudo systemctl disable --now ublk-storm-sysrq.service >/dev/null 2>&1
  sudo rm -f "$SYSRQ_UNIT"
  sudo systemctl daemon-reload
  echo "selftest: injector removed (ExecStop neutralized first)"
}

case "$MODE" in
arm)
  arm_storm
  ;;
arm-unit)
  arm_storm_unit
  ;;
units-remove)
  remove_storm_units
  ;;
check)
  check_storm
  ;;
check-expect-detect)
  EXPECT_DETECT=1 check_storm
  ;;
selftest-install)
  install_sysrq_unit
  ;;
selftest-remove)
  remove_sysrq_unit
  ;;
*)
  echo "usage: $0 {arm [SIZE]|check|check-expect-detect|selftest-install|selftest-remove}"
  exit 2
  ;;
esac
