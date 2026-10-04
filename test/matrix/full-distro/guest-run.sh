#!/bin/bash
# Runs inside a full-distro guest, started by cloud-init from the payload ISO.
# Same channels as the initramfs /init: JSON lines on ttyS1, human output on
# the console, then a dmesg dump and power-off.
exec </dev/null >/dev/console 2>&1
R=/dev/ttyS1
stty -F $R raw -echo 115200 2>/dev/null || R=/dev/null
emit() {
	printf '%s\n' "$1" >$R
	printf '@@GOUBLK@@ %s\n' "$1"
}
src=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$src/config.env"
P=/var/tmp/payload
mkdir -p $P
cp -a "$src/payload/." $P/

up() { cut -d' ' -f1 /proc/uptime; }
. /etc/os-release
emit "{\"meta\":{\"phase\":\"boot\",\"uname\":\"$(uname -r)\",\"machine\":\"$(uname -m)\",\"boot_s\":$(up),\"cpus\":$(nproc),\"os\":\"$PRETTY_NAME\",\"systemd\":\"$(systemctl --version 2>/dev/null | head -1)\"}}"
echo 30 >/proc/sys/kernel/hung_task_timeout_secs 2>/dev/null

ublk=none
modinfo ublk_drv >/dev/null 2>&1 && ublk=module
grep -qw ublk_drv "/lib/modules/$(uname -r)/modules.builtin" 2>/dev/null && ublk=builtin
mp_out=$(modprobe ublk_drv 2>&1)
mp_rc=$?
udevadm settle 2>/dev/null
[ -e /dev/ublk-control ] && ctl=true || ctl=false
emit "{\"meta\":{\"phase\":\"modprobe\",\"ublk_drv\":\"$ublk\",\"modprobe_rc\":$mp_rc,\"modprobe_out\":\"$(echo "$mp_out" | tr '"\n\\' "'  ")\",\"ublk_control\":$ctl,\"srcversion\":\"$(cat /sys/module/ublk_drv/srcversion 2>/dev/null)\"}}"

echo "=== payload start ==="
MATRIX_SCALE=$MATRIX_SCALE MATRIX_PROFILE=$MATRIX_PROFILE MATRIX_TESTS=$MATRIX_TESTS \
	$P/run | while IFS= read -r line; do emit "$line"; done
echo "=== payload end ==="
emit "{\"meta\":{\"phase\":\"done\",\"uptime_s\":$(up)}}"
echo "@@GOUBLK-DMESG-BEGIN@@"
dmesg
echo "@@GOUBLK-DMESG-END@@"
sync
systemctl poweroff --no-block || poweroff -f
