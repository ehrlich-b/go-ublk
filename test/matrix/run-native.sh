#!/bin/bash
# Run the payload against the host's own kernel, no VM, and record it like a
# matrix run (boot "full-vm", accel "native"). For a CI runner or any
# disposable machine: it creates, kills and deletes ublk devices. Needs root.
#
#   run-native.sh [OUTDIR]       default: $RUNS/native-<date>/<id>
#
# Same knobs as a guest: TESTS, PROFILE, SCALE (default 1). The payload must
# already be built (make matrix-payload).
set -uo pipefail
. "$(dirname "$0")/lib.sh"

[ "$(id -u)" = 0 ] || die "run-native.sh needs root"
P=$BUILD/payload
[ -x "$P/run" ] || die "no payload at $P (make matrix-payload)"
. /etc/os-release
kver=$(uname -r)
id="native-${ID:-linux}-${VERSION_ID:-0}-$kver"
out=${1:-$RUNS/native-$(date +%Y%m%d-%H%M%S)/$id}
mkdir -p "$out"
: >"$out/results.log"
emit() { printf '%s\n' "$1" >>"$out/results.log"; printf '@@GOUBLK@@ %s\n' "$1"; }

ublk=false
modinfo ublk_drv >/dev/null 2>&1 && ublk=module
grep -qw ublk_drv "/lib/modules/$kver/modules.builtin" 2>/dev/null && ublk=builtin
cat >"$out/fetch.json" <<EOF
{"id": "$id", "family": "${ID:-linux}", "distro": "${PRETTY_NAME:-$ID} (native)", "version": "$kver",
 "upstream": "$(echo "$kver" | grep -oE '^[0-9]+\.[0-9]+')", "source": "host kernel", "status": "ok"}
EOF
printf '{"id": "%s", "kver": "%s", "ublk_drv": %s}\n' "$id" "$kver" \
	"$([ "$ublk" = false ] && echo false || echo "\"$ublk\"")" >"$out/kinfo.json"

{
	t0=$(date +%s)
	emit "{\"meta\":{\"phase\":\"boot\",\"uname\":\"$kver\",\"machine\":\"$(uname -m)\",\"cpus\":$(nproc)}}"
	mp_out=$(modprobe ublk_drv 2>&1)
	mp_rc=$?
	[ -e /dev/ublk-control ] && ctl=true || ctl=false
	emit "{\"meta\":{\"phase\":\"modprobe\",\"ublk_drv\":\"$ublk\",\"modprobe_rc\":$mp_rc,\"modprobe_out\":\"$(echo "$mp_out" | tr '"\n' "' ")\",\"ublk_control\":$ctl,\"srcversion\":\"$(cat /sys/module/ublk_drv/srcversion 2>/dev/null)\"}}"
	dmesg_mark=$(dmesg | wc -l)
	MATRIX_SCALE=${SCALE:-1} MATRIX_PROFILE=${PROFILE:-full} MATRIX_TESTS=${TESTS:-} \
		"$P/run" | while IFS= read -r line; do emit "$line"; done
	emit "{\"meta\":{\"phase\":\"done\"}}"
	# Only this run's kernel log, so an older oops on the box is not charged to us.
	echo "@@GOUBLK-DMESG-BEGIN@@"
	dmesg | tail -n +"$((dmesg_mark + 1))"
	echo "@@GOUBLK-DMESG-END@@"
	echo "$(($(date +%s) - t0))" >"$out/wall"
} 2>&1 | tee "$out/console.log"

python3 "$MATRIX_DIR/report.py" parse "$out" --id "$id" --rc 0 --wall "$(cat "$out/wall")" \
	--accel native --boot full-vm --kinfo "$out/kinfo.json" --fetch "$out/fetch.json" \
	--commit-file "$P/COMMIT" --oops-in-dmesg-only
log "$id: $(json_get "$out/run.json" status)"
