#!/usr/bin/env bash
# Run on the coordinator's disposable Linux VM as root.
# Usage: scripts/perf-profile.sh /path/to/ublk-mem '-backend ram' '-backend null -inline'
# ROUNDS=3 OUT=.scratch/profile CPU_PROFILE=1 scripts/perf-profile.sh ...
set -euo pipefail

fail() { echo "perf-profile: $*" >&2; exit 1; }

if [[ ${1:-} == -h || ${1:-} == --help ]]; then
    echo "Usage: ROUNDS=3 CPU_PROFILE=0 OUT=<new-directory> $0 <ublk-mem> '<flags>' ['<flags>' ...]"
    exit 0
fi
[[ $# -ge 2 ]] || fail "provide a ublk-mem binary and at least one quoted flag set"
[[ $(uname -s) == Linux ]] || fail "requires Linux"
[[ $(id -u) == 0 ]] || fail "run as root on a disposable benchmark VM"
for command in fio perf python3 modprobe timeout tail awk sha256sum realpath tee; do
    command -v "$command" >/dev/null || fail "missing command: $command"
done

binary=$(realpath "$1")
shift
[[ -x $binary ]] || fail "binary is not executable: $binary"
flag_sets=("$@")
rounds=${ROUNDS:-3}
cpu_profile=${CPU_PROFILE:-0}
[[ $rounds =~ ^[1-9][0-9]*$ ]] || fail "ROUNDS must be a positive integer"
[[ $cpu_profile == 0 || $cpu_profile == 1 ]] || fail "CPU_PROFILE must be 0 or 1"
out=${OUT:-.scratch/perf-profile-$(date -u +%Y%m%dT%H%M%SZ)}
[[ ! -e $out ]] || fail "output directory already exists: $out"
mkdir -p "$out"
out=$(realpath "$out")
reporter=$(dirname "$(realpath "$0")")/perf-profile-report.py
[[ -f $reporter ]] || fail "missing report helper: $reporter"

# Flag sets are whitespace-separated argument lists, never shell programs.
# The harness owns device geometry, profiling paths and daemon lifetime.
for flag_set in "${flag_sets[@]}"; do
    [[ $flag_set != *$'\n'* && $flag_set != *$'\t'* ]] || fail "flag sets cannot contain tabs or newlines"
    read -r -a flags <<< "$flag_set"
    for ((argument_index=0; argument_index<${#flags[@]}; argument_index++)); do
        argument=${flags[$argument_index]}
        case "$argument" in
            -backend|--backend)
                argument_index=$((argument_index + 1))
                value=${flags[$argument_index]:-}
                [[ $value == ram || $value == null ]] || fail "backend must be ram or null" ;;
            -dispatch|--dispatch)
                argument_index=$((argument_index + 1))
                value=${flags[$argument_index]:-}
                [[ $value == goroutine || $value == pool ]] || fail "dispatch must be goroutine or pool" ;;
            -backend=ram|--backend=ram|-backend=null|--backend=null|\
            -dispatch=goroutine|--dispatch=goroutine|-dispatch=pool|--dispatch=pool|\
            -inline|--inline|-inline=true|--inline=true|-inline=false|--inline=false|\
            -zip|--zip|-zip=true|--zip=true|-zip=false|--zip=false|\
            -v|--v|-v=true|--v=true|-v=false|--v=false) ;;
            *) fail "unsupported or harness-owned flag: $argument" ;;
        esac
    done
done

server_pid=""
stop_server() {
    local daemon_pid=$server_pid
    server_pid=""
    if kill -0 "$daemon_pid" 2>/dev/null; then
        kill -INT "$daemon_pid"
        # Bound cleanup without SIGKILL or deleting unrelated devices. The
        # example's own shutdown backstop is 15 seconds; allow twice that.
        if ! timeout 30s tail --pid="$daemon_pid" -f /dev/null; then
            echo "perf-profile: daemon $daemon_pid did not stop; inspect its log and device" >&2
            return 1
        fi
    fi
    wait "$daemon_pid"
}

cleanup() {
    local status=$?
    trap - EXIT
    if [[ -n $server_pid ]]; then
        stop_server || status=1
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

events=context-switches,cpu-migrations,raw_syscalls:sys_enter
modprobe ublk_drv
perf stat --no-big-num -x ';' -o "$out/event-check.csv" -e "$events" -- true
{
    date -u
    uname -a
    sha256sum "$binary"
    printf 'binary=%s\nrounds=%s\nsize=512M queues=2 depth=64 runtime=10s\n' "$binary" "$rounds"
    for index in "${!flag_sets[@]}"; do
        printf 'set=%s flags=%s\n' "$index" "${flag_sets[$index]}"
    done
} > "$out/metadata.txt"
printf 'round\tset\tflags\tworkload\tiops\tp50_us\tp99_us\tios\tsyscalls\tcontext_switches\tmigrations\tsyscalls_per_io\tcontext_switches_per_io\tmigrations_per_io\n' > "$out/results.tsv"

for ((round=1; round<=rounds; round++)); do
    # Rotate the first variant each round to spread warmup and time drift.
    # Duplicate baseline flag sets can be supplied to measure an A/A floor.
    for ((offset=0; offset<${#flag_sets[@]}; offset++)); do
        index=$(((offset + round - 1) % ${#flag_sets[@]}))
        run_dir=$out/round-$round-set-$index
        mkdir -p "$run_dir"
        read -r -a flags <<< "${flag_sets[$index]}"
        profile_flags=()
        if [[ $cpu_profile == 1 ]]; then
            profile_flags=(-cpuprofile "$run_dir/cpu.pprof")
        fi
        "$binary" "${flags[@]}" -size 512M -queues 2 -depth 64 \
            "${profile_flags[@]}" > "$run_dir/server.log" 2>&1 &
        server_pid=$!
        device=""
        # Device startup normally takes under a second; allow 10 seconds for
        # module/udev startup and fail if this particular server exits.
        for ((attempt=0; attempt<100; attempt++)); do
            kill -0 "$server_pid" 2>/dev/null || fail "server exited; see $run_dir/server.log"
            device=$(awk '$1 == "Device" && $2 == "created:" && $3 ~ /^\/dev\/ublkb[0-9]+$/ {print $3; exit}' "$run_dir/server.log")
            if [[ -n $device && -b $device ]]; then
                break
            fi
            sleep 0.1
        done
        [[ -n $device && -b $device ]] || fail "device did not appear; see $run_dir/server.log"
        printf 'pid=%s device=%s\n' "$server_pid" "$device" > "$run_dir/device.txt"
        for workload in qd1 qd32x2; do
            depth=1
            jobs=1
            if [[ $workload == qd32x2 ]]; then
                depth=32
                jobs=2
            fi
            echo "round=$round set=$index flags='${flag_sets[$index]}' workload=$workload"
            # -p selects the server's threads; fio controls the measurement
            # lifetime and does not contribute its own counters to this table.
            perf stat --no-big-num -x ';' -o "$run_dir/$workload.perf.csv" \
                -e "$events" -p "$server_pid" -- \
                fio --name=randread --filename="$device" --size=512M --rw=randread \
                --bs=4k --ioengine=io_uring --direct=1 --readonly --iodepth="$depth" \
                --numjobs="$jobs" --runtime=10 --time_based=1 --group_reporting=1 \
                --clat_percentiles=1 --percentile_list=50:99 --output-format=json \
                --output="$run_dir/$workload.fio.json" > "$run_dir/$workload.fio.log" 2>&1
            python3 "$reporter" "$round" "$index" "${flag_sets[$index]}" "$workload" \
                "$run_dir/$workload.fio.json" "$run_dir/$workload.perf.csv" | tee -a "$out/results.tsv"
        done
        stop_server
    done
done
python3 "$reporter" --summary "$out/results.tsv"
echo "Artifacts: $out"
