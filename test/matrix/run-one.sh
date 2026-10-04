#!/bin/bash
# Boot one extracted kernel with the userland + payload and record the run.
#
#   run-one.sh ID RUNDIR
#
# Writes RUNDIR/ID/{console.log,results.log,run.json}. A guest that outlives
# TIMEOUT is killed and recorded as a timeout: a hung kernel is a result, not
# a harness failure.
#
# Knobs (environment):
#   ACCEL=tcg|kvm   QEMU=qemu-system-x86_64   MEM=2048 (MiB)   SMP=2
#   TIMEOUT=seconds (default 3000 under tcg, 1200 under kvm)
#   SCALE=ublk-suite -scale (default 0.25 under tcg, 1 under kvm)
#   PROFILE=full|quick   TESTS="probe unit largeio verify loop suite" (default: all)
#   SUITE_RUN / SUITE_SKIP = ublk-suite -run / -skip regexps (no spaces)
#   EXTRA_APPEND="..."   extra kernel command line; ID_SUFFIX records it as its own row
set -uo pipefail
. "$(dirname "$0")/lib.sh"

id=$1
k=$KERNELS/$id
# A variant (say, the same kernel with a sysctl flipped on the command line)
# is recorded as its own row: ID_SUFFIX is appended to the id.
rid=$id${ID_SUFFIX:-}
out=$2/$rid
ACCEL=${ACCEL:-tcg}
QEMU=${QEMU:-qemu-system-x86_64}
MEM=${MEM:-2048}
SMP=${SMP:-2}
PROFILE=${PROFILE:-full}
TESTS=${TESTS:-}
if [ "$ACCEL" = kvm ]; then
	TIMEOUT=${TIMEOUT:-1200}
	SCALE=${SCALE:-1}
	accel=(-accel kvm -cpu host)
	append_accel=""
else
	TIMEOUT=${TIMEOUT:-3000}
	SCALE=${SCALE:-0.25}
	accel=(-accel tcg,thread=multi,tb-size=256 -cpu max)
	# Speculation mitigations only cost time under emulation.
	append_accel="mitigations=off"
fi

[ -f "$k/kinfo.json" ] || die "$id: not extracted ($k/kinfo.json missing)"
for f in "$BUILD/userland.cpio" "$BUILD/payload.cpio"; do
	[ -f "$f" ] || die "missing $f (make matrix-userland matrix-payload)"
done
mkdir -p "$out"
rm -f "$out/console.log" "$out/results.log" "$out/run.json"
initrd=$out/initrd.cpio
cat "$BUILD/userland.cpio" "$BUILD/payload.cpio" "$k/modules.cpio" >"$initrd"

append="console=ttyS0,115200 panic=-1 random.trust_cpu=on rdinit=/init $append_accel"
append="$append goublk.profile=$PROFILE goublk.scale=$SCALE"
[ -n "$TESTS" ] && append="$append goublk.tests=$(echo $TESTS | tr ' ' ',')"
# ublk-suite -run/-skip regexps; no spaces (they ride on the kernel command line).
[ -n "${SUITE_RUN:-}" ] && append="$append goublk.suite_run=$SUITE_RUN"
[ -n "${SUITE_SKIP:-}" ] && append="$append goublk.suite_skip=$SUITE_SKIP"
[ -n "${EXTRA_APPEND:-}" ] && append="$append $EXTRA_APPEND"

log "$rid: booting ($ACCEL, ${SMP} cpu, ${MEM}M, timeout ${TIMEOUT}s)"
t0=$(date +%s)
: >"$out/results.log"
# shellcheck disable=SC2086
timeout -k 15 "$TIMEOUT" $QEMU "${accel[@]}" -m "$MEM" -smp "$SMP" \
	-nodefaults -no-user-config -display none -no-reboot \
	-kernel "$k/vmlinuz" -initrd "$initrd" -append "$append" \
	-serial "file:$out/console.log" -serial "file:$out/results.log" \
	>"$out/qemu.log" 2>&1 &
qpid=$!
# A guest whose payload finished but that cannot power off (a wedged ublk
# device blocks the kernel's shutdown path) is killed after a grace period
# instead of sitting out the whole timeout; that is recorded, not hidden.
hung_poweroff=
while kill -0 "$qpid" 2>/dev/null; do
	if grep -q '"phase":"done"' "$out/results.log" 2>/dev/null; then
		for _ in $(seq 1 45); do kill -0 "$qpid" 2>/dev/null || break; sleep 1; done
		if kill -0 "$qpid" 2>/dev/null; then
			hung_poweroff=1
			kill -TERM "$qpid" 2>/dev/null
		fi
		break
	fi
	sleep 2
done
wait "$qpid"
rc=$?
wall=$(($(date +%s) - t0))
rm -f "$initrd"

python3 "$MATRIX_DIR/report.py" parse "$out" --id "$rid" --rc "$rc" --wall "$wall" \
	--accel "$ACCEL" --timeout "$TIMEOUT" --kinfo "$k/kinfo.json" \
	--fetch "$k/fetch.json" --commit-file "$BUILD/payload/COMMIT" --append "${EXTRA_APPEND:-}" \
	${hung_poweroff:+--hung-poweroff}
log "$rid: $(json_get "$out/run.json" status) after ${wall}s (qemu rc=$rc)"
