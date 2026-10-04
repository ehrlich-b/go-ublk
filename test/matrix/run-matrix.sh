#!/bin/bash
# Boot every selected extracted kernel, JOBS guests at a time, then aggregate.
#
#   run-matrix.sh [ID|GLOB ...]      default: every extracted kernel
#
# JOBS (default 4) is the concurrency knob; each guest takes MEM MiB (default
# 2048) plus QEMU overhead. RUN_ID names the run dir under $RUNS. Per-guest
# knobs (ACCEL, QEMU, MEM, SMP, TIMEOUT, PROFILE, TESTS) pass through to
# run-one.sh.
set -uo pipefail
. "$(dirname "$0")/lib.sh"

JOBS=${JOBS:-4}
RUN_ID=${RUN_ID:-$(date +%Y%m%d-%H%M%S)}
rundir=$RUNS/$RUN_ID
# Patterns may arrive as one quoted, space-separated word (from make); split
# without letting the shell glob them against the cwd.
read -ra pats <<<"$*"
[ ${#pats[@]} -eq 0 ] && pats=("*")

ids=()
for f in "$KERNELS"/*/kinfo.json; do
	[ -e "$f" ] || continue
	id=$(basename "$(dirname "$f")")
	for p in "${pats[@]}"; do
		if [[ $id == $p ]]; then ids+=("$id"); break; fi
	done
done
[ ${#ids[@]} -gt 0 ] || die "no extracted kernels match: ${pats[*]}"
mkdir -p "$rundir"
log "run $RUN_ID: ${#ids[@]} kernels, $JOBS at a time -> $rundir"
export ACCEL QEMU MEM SMP TIMEOUT SCALE PROFILE TESTS SUITE_RUN SUITE_SKIP EXTRA_APPEND ID_SUFFIX MATRIX_HOME 2>/dev/null
printf '%s\n' "${ids[@]}" | xargs -P "$JOBS" -I{} bash "$MATRIX_DIR/run-one.sh" {} "$rundir"

python3 "$MATRIX_DIR/fetch.py" manifest
python3 "$MATRIX_DIR/report.py" aggregate "$rundir" --out "$rundir" --manifest "$CACHE/kernels.json"
