"""Fail closed on incomplete, mixed, unmatched, noisy or regressing evidence."""
import argparse
from collections import defaultdict
import json
import math
from pathlib import Path
import re
import statistics
import sys

from closeout_common import (BINARIES, CONTROLS, KERNELS, LINUX_GATES, METRICS, POLICY,
                             VARIANTS, WORKLOADS, contained, diagnostics, digest, kernel_slot,
                             positive, read_json, schedule, validate_manifest, variant_backend,
                             source_files, verify_files)

ROOT = Path(__file__).resolve().parent.parent


def binding(row, manifest, manifest_sha):
    if (row.get("manifest_sha256") != manifest_sha or row.get("candidate") != manifest["candidate"]["commit"] or
            row.get("binaries") != manifest["binaries"]):
        raise ValueError("mixed candidate/manifest/binary hashes")


def allocation_gate(text):
    rows = defaultdict(list)
    expression = re.compile(r"^BenchmarkDispatch(Inline|Pool|Auto|Adaptive)(?:-\d+)?\s+"
                            r"\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op$", re.M)
    for mode, ns, bytes_per_op, allocs in expression.findall(text):
        rows[mode].append((positive(float(ns), "benchmark ns/op"), int(bytes_per_op), int(allocs)))
    for mode in POLICY["zero_alloc_modes"]:
        if len(rows[mode]) != 3 or any(b != 0 or a != 0 for _, b, a in rows[mode]):
            raise ValueError(f"hot-path allocations absent or nonzero: {mode}")
    if not re.search(r"^PASS$", text, re.M):
        raise ValueError("dispatch benchmark invocation did not pass")


def suite_gate(directory):
    expected = (directory / "suite-list.log").read_text().splitlines()
    if len(expected) < 44 or len(expected) != len(set(expected)):
        raise ValueError("incomplete or duplicated real-kernel suite inventory")
    rows = [json.loads(line) for line in (directory / "suite.log").read_text().splitlines()
            if line.startswith("{")]
    if len(rows) != len(expected) or [row["test"] for row in rows] != expected:
        raise ValueError("unfiltered suite did not emit every registered result")
    counts = defaultdict(int)
    for row in rows:
        status, name = row["status"], row["test"]
        if status not in ("pass", "skip"):
            raise ValueError(f"suite failed: {name}: {status}")
        if status == "skip":
            detail = row.get("detail", "")
            allowed = (detail.startswith("kernel lacks ") or
                       detail.startswith("kernel refused integrity parameters ") or
                       detail.startswith("kernel has no ") and detail.endswith(" support") or
                       name == "lifecycle/list-high-id" and detail.startswith("kernel refused device ID 100:"))
            if not allowed:
                raise ValueError(f"suite skipped an unmet host prerequisite: {name}: {detail}")
        counts[status] += 1
    if counts["pass"] < 44:
        raise ValueError("real-kernel suite has fewer passes than the verified 6.x floor")
    return dict(counts)


def check_kernel(directory, slot, manifest, manifest_sha):
    receipt = read_json(directory / "receipt.json")
    binding(receipt, manifest, manifest_sha)
    if (receipt.get("schema") != 1 or receipt.get("status") != "COMPLETE" or
            receipt.get("tree_sha256") != manifest["candidate"]["tree_sha256"] or
            receipt.get("kernel_slot") != slot or kernel_slot(receipt["kernel_release"]) != slot):
        raise ValueError("incomplete/wrong-kernel correctness receipt")
    attestation = receipt["attestation"]
    if (attestation.get("disposable") is not True or attestation.get("fixed_ram_mib") != 4096 or
            attestation.get("balloon") is not False or attestation.get("kernel_release") != receipt["kernel_release"]):
        raise ValueError("guest attestation does not match fixed 4 GiB/no-balloon plan")
    server, fio = set(attestation["server_cpus"]), set(attestation["fio_cpus"])
    if (len(server) < 2 or len(fio) < 2 or server & fio or
            server | fio != set(attestation["assigned_cpus"])):
        raise ValueError("guest CPU assignment does not match disjoint plan")
    raw = receipt["raw_sha256"]
    actual = {str(path.relative_to(directory)) for path in directory.rglob("*")
              if path.is_file() and path.name != "receipt.json"}
    if set(raw) != actual:
        raise ValueError("raw result inventory is incomplete or contains extra files")
    verify_files(directory, raw)
    required = {target + ".log" for target in LINUX_GATES} | {name + ".log" for name in BINARIES[:4]}
    required |= {"suite.log", "suite-list.log", "cleanup.json", "dmesg-before.txt",
                 "dmesg-after.txt", "dmesg-delta.txt"}
    if not required <= set(raw):
        raise ValueError("required gate logs, cleanup or dmesg evidence absent")
    cpu_prefix = ["taskset", "-c", ",".join(map(str, sorted(attestation["assigned_cpus"])))]
    expected_commands = {target + ".log": cpu_prefix + ["make", "-j1", target] for target in LINUX_GATES}
    expected_commands.update({name + ".log": cpu_prefix + [name, "-test.v", "-test.timeout=5m", "-test.parallel=2"]
                              for name in BINARIES[:4]})
    expected_commands.update({"suite-list.log": cpu_prefix + ["ublk-suite", "-list"],
                              "suite.log": cpu_prefix + ["ublk-suite", "-scale", "1"]})
    commands = {row["log"]: row for row in receipt["commands"]}
    if len(commands) != len(receipt["commands"]) or not set(expected_commands) <= commands.keys():
        raise ValueError("duplicate or missing exact command receipts")
    for log, expected in expected_commands.items():
        row = commands[log]
        actual_command = list(row["command"])
        if len(actual_command) < 4:
            raise ValueError("truncated command receipt")
        actual_command[0] = Path(actual_command[0]).name
        actual_command[3] = Path(actual_command[3]).name
        if actual_command != expected or row["returncode"] != 0 or row["sha256"] != raw[log]:
            raise ValueError(f"invalid command/status/log hash: {log}")
    allocation_gate((directory / "benchmark-dispatch.log").read_text())
    if "--- PASS: TestDispatchHotPathDoesNotAllocate" not in (directory / "test-unit.log").read_text():
        raise ValueError("allocation regression test absent from full Linux unit suite")
    counts = suite_gate(directory)
    before, after = [(directory / f"dmesg-{which}.txt").read_text() for which in ("before", "after")]
    delta = (directory / "dmesg-delta.txt").read_text()
    if not after.startswith(before) or after[len(before):] != delta:
        raise ValueError("dmesg delta is absent, wrapped or inconsistent")
    classified = diagnostics(delta, slot)
    if classified != receipt["diagnostics"] or classified["unexpected"]:
        raise ValueError("unexpected kernel diagnostic or changed classification")
    cleanup = read_json(directory / "cleanup.json")
    if (receipt["cleanup_verified"] is not True or cleanup.get("verified") is not True or
            cleanup.get("registrations_after") != [] or cleanup.get("blocks_after") != []):
        raise ValueError("cleanup did not prove absence of device registrations")
    return dict(kernel_release=receipt["kernel_release"], suite=counts, diagnostics=classified)


def load_performance(directory, manifest, manifest_sha, bundle):
    # These imports only parse raw data; the harness build/network entry point is never called.
    sys.path.insert(0, str(bundle / "harness/lib"))
    from report import parse_fio
    from cpu import cpu_cost, interval
    performance = read_json(directory / "performance.json")
    binding(performance, manifest, manifest_sha)
    if (performance.get("status") != "COMPLETE" or performance.get("policy") != POLICY or
            performance.get("schedule") != schedule() or performance.get("cpu_cleanup_verified") is not True or
            performance.get("tree_sha256") != manifest["candidate"]["tree_sha256"]):
        raise ValueError("performance window incomplete or changed plan/CPU cleanup")
    attestation = performance["attestation"]
    if not attestation.get("quiet_window_confirmed") or not attestation.get("quiet_window_note", "").strip():
        raise ValueError("quiet physical-core window is unconfirmed")
    if kernel_slot(attestation["kernel_release"]) != "6.12" or not attestation.get("performance_window_id"):
        raise ValueError("performance kernel/window identity absent")
    server, fio = set(attestation["server_cpus"]), set(attestation["fio_cpus"])
    if len(server) < 2 or len(fio) < 2 or server & fio or server | fio != set(attestation["assigned_cpus"]):
        raise ValueError("performance CPU budget is unmatched or shared")
    masks = {key: ",".join(map(str, sorted(attestation[key])))
             for key in ("server_cpus", "fio_cpus", "assigned_cpus")}
    samples = []
    expected_paths = set()
    timed_commands = []
    for entry in schedule():
        run = directory / "runs" / f"{entry['sequence']:03d}-{entry['implementation']}"
        lifecycle = read_json(run / "run.json")
        backend = variant_backend(entry["implementation"])
        semantics = "null" if backend == "null" else "retaining-ram"
        queues, depth = POLICY["geometry"][backend]
        if (lifecycle.get("status") != "stopped" or lifecycle.get("cleanup_verified") is not True or
                lifecycle.get("cleanup_forced") is not False or lifecycle.get("logical_block_bytes") != 512 or
                lifecycle.get("effective_queues") != queues or lifecycle.get("effective_depth") != depth or
                lifecycle.get("size_bytes") != 512 << 20 or lifecycle.get("semantics") != semantics):
            raise ValueError(f"unmatched geometry or incomplete cleanup: {run.name}")
        for key, value in entry.items():
            if lifecycle.get(key) != value:
                raise ValueError("lifecycle does not match interleaved schedule")
        capacity = lifecycle["device_bytes"]
        if capacity != 512 << 20 and not (entry["implementation"] in ("ublksrv-null", "libublk-rs-null")
                                         and capacity == 250 << 30):
            raise ValueError("unexpected backing capacity; fixed upstream null capacity must be labelled")
        variant = entry["implementation"]
        if variant.startswith("go-"):
            mode = variant.split("-")[-1]
            executable = "ublk-loop" if backend == "loop" else "ublk-mem"
            launches = [command for command in lifecycle["commands"] if Path(command[0]).name == executable]
            if len(launches) != 1:
                raise ValueError("exact dispatch server launch receipt is absent")
            command = launches[0]
            flags = ["-size", "512M", "-queues", str(queues), "-depth", str(depth)]
            if backend == "loop":
                backing = command[8] if len(command) > 8 else ""
                if not backing.endswith(f"/{run.name}/loop-backing/disk.img"):
                    raise ValueError("loop backing file is outside its owned run")
                flags += ["-file", backing]
            else:
                flags += ["-backend", backend]
            flags += ["-inline"] if mode == "inline" else ["-dispatch", mode]
            if command[1:] != flags:
                raise ValueError("server launch flags do not match the measured dispatch mode")
        else:
            executable = "ublk" if variant == "ublksrv-null" else "null" if variant.endswith("-null") else "ramdisk"
            expected = (["add", "-t", "null", "-n", "0", "-q", "2", "-d", "64"] if executable == "ublk" else
                        ["add", "-n", "0", "-q", "2", "-d", "64", "--foreground"] if executable == "null" else
                        ["add", "0", "512"])
            if not any(Path(command[0]).name == executable and command[1:] == expected
                       for command in lifecycle["commands"]):
                raise ValueError("pinned reference launch command is absent or changed")
        if semantics == "retaining-ram" and lifecycle.get("precondition_verified") is not True:
            raise ValueError("full retaining-device CRC precondition absent")
        for workload in WORKLOADS:
            sample_path = run / (workload["name"] + ".sample.json")
            expected_paths.add(sample_path)
            meta = read_json(sample_path)
            timed_commands.append(meta["command"])
            binding(meta, manifest, manifest_sha)
            if any(meta.get(key) != value for key, value in entry.items()):
                raise ValueError("incomplete or reordered A/B schedule")
            expected_meta = dict(workload=workload["name"], runtime_seconds=15, numjobs=workload["numjobs"],
                                 effective_queues=queues, effective_depth=depth, size_bytes=512 << 20,
                                 logical_block_bytes=512, semantics=semantics, cpu_isolation="disjoint",
                                 server_cpus=masks["server_cpus"], fio_cpus=masks["fio_cpus"],
                                 cpu_budget=masks["assigned_cpus"])
            if any(meta.get(key) != value for key, value in expected_meta.items()):
                raise ValueError("unmatched workload, accepted geometry, semantics or CPU budget")
            expected_flags = dict(ioengine="io_uring", direct="1", thread="1", group_reporting="0",
                                  size=str(512 << 20), time_based="1", runtime="15", ramp_time="0",
                                  randrepeat="1", randseed="20261010", invalidate="1", allow_file_create="0",
                                  exitall_on_error="1", clat_percentiles="1", lat_percentiles="0",
                                  percentile_list="50:99:99.9", **{k: str(v) for k, v in workload.items() if k != "name"})
            flags = {}
            for argument in meta["command"][1:]:
                if not argument.startswith("--") or "=" not in argument:
                    raise ValueError("unexpected fio command shape")
                key, value = argument[2:].split("=", 1)
                if key in flags:
                    raise ValueError("duplicate fio option")
                flags[key] = value
            if any(flags.get(key) != value for key, value in expected_flags.items()):
                raise ValueError("fio geometry/flags differ from exact workload")
            paths = {name: contained(run, meta[name + "_file"]) for name in ("fio", "cpu")}
            for name, path in paths.items():
                if digest(path) != meta[name + "_sha256"]:
                    raise ValueError("raw fio/CPU SHA256 changed")
            data = read_json(paths["fio"])
            metrics = parse_fio(data)
            if metrics["jobs"] != workload["numjobs"] or metrics["min_runtime_ms"] < 14500:
                raise ValueError("fio worker count or timed interval incomplete")
            for job in data["jobs"]:
                # fio retains exact submitted job options; don't trust only a wrapper's metadata.
                options = dict(data.get("global options", {}), **job.get("job options", {}))
                for key in ("rw", "bs", "iodepth", "size", "direct", "ioengine", "rwmixread"):
                    if key in expected_flags and str(options.get(key)) != expected_flags[key]:
                        raise ValueError(f"raw fio job has unmatched {key}")
                for direction in ("read", "write"):
                    stats = job.get(direction, {})
                    if stats.get("total_ios", 0):
                        positive(stats.get("runtime"), "fio direction runtime")
                        if not math.isclose(stats["iops"], stats["total_ios"] / (stats["runtime"] / 1000), rel_tol=.005):
                            raise ValueError("raw completed I/O count does not match reported rate/runtime")
                        bins = stats.get("clat_ns", {}).get("bins", {})
                        if not bins or sum(bins.values()) != stats["total_ios"]:
                            raise ValueError("complete raw nanosecond tail histograms are required")
                read_ios, write_ios = [job.get(direction, {}).get("total_ios", 0) for direction in ("read", "write")]
                if ((workload["rw"] in ("randread", "read") and (read_ios <= 0 or write_ios != 0)) or
                        (workload["rw"] in ("randwrite", "write") and (write_ios <= 0 or read_ios != 0)) or
                        (workload["rw"] == "randrw" and (read_ios <= 0 or write_ios <= 0))):
                    raise ValueError("raw I/O directions do not match workload semantics")
            cpu = read_json(paths["cpu"])
            recomputed = interval(cpu["before"], cpu["after"])
            for key in ("elapsed_seconds", "server_usr_seconds", "server_sys_seconds", "cgroup_system_seconds"):
                if not math.isclose(cpu[key], recomputed[key], rel_tol=1e-9, abs_tol=1e-9):
                    raise ValueError("derived CPU accounting disagrees with raw snapshots")
            if cpu.get("valid") is not True or recomputed["valid"] is not True:
                raise ValueError("invalid CPU interval or PID identity")
            metrics.update(cpu_cost(metrics, recomputed))
            for metric in METRICS:
                positive(metrics.get(metric), metric)
            samples.append(dict(entry=entry, workload=workload["name"], metrics=metrics))
    actual_paths = set((directory / "runs").glob("*/*.sample.json"))
    if actual_paths != expected_paths:
        raise ValueError("duplicate/unexpected performance observation")
    journal = [read_json_line for read_json_line in
               (json.loads(line) for line in (directory / "commands.jsonl").read_text().splitlines())]
    actual_timed = [row["command"] for row in journal if row["command"][0] == "fio" and
                    any(arg.startswith("--output=") and arg.endswith(".fio.json") for arg in row["command"])]
    if actual_timed != timed_commands:
        raise ValueError("actual command journal does not prove the interleaved A/B order")
    return samples


def gap(a, b):
    return abs(a - b) / ((a + b) / 2) * 100


def performance_decision(samples):
    groups = defaultdict(list)
    for sample in samples:
        entry = sample["entry"]
        groups[(entry["implementation"], sample["workload"], entry["phase"], entry["repeat"])].append(sample)
    medians = {}
    for variant in VARIANTS:
        for workload in WORKLOADS:
            key = (variant, workload["name"])
            rows = groups[(*key, "measured", "primary")]
            if len(rows) != 3 or {row["entry"]["round"] for row in rows} != {1, 2, 3}:
                raise ValueError("each A/B peer requires three unique measured rounds")
            medians[key] = {metric: statistics.median(row["metrics"][metric] for row in rows) for metric in METRICS}
    floors = {}
    for control in CONTROLS:
        for workload in WORKLOADS:
            name = workload["name"]
            for metric in METRICS:
                pairs = []
                for phase, rounds in (("calibration", (0,)), ("measured", (1, 2, 3))):
                    for round_number in rounds:
                        values = []
                        for repeat in ("primary", "control"):
                            matched = [s for s in groups[(control, name, phase, repeat)]
                                       if s["entry"]["round"] == round_number]
                            if len(matched) != 1:
                                raise ValueError("pilot and interleaved A/A pairs are required")
                            values.append(matched[0]["metrics"][metric])
                        pairs.append(gap(*values))
                floor = max(pairs[0], math.sqrt(statistics.mean(value * value for value in pairs[1:])))
                if floor > POLICY["max_noise_percent"]:
                    raise ValueError(f"noisy A/A: {control}/{name}/{metric}: {floor:.2f}%")
                floors[(control, name, metric)] = floor
    outcomes = []
    for backend in ("ram", "null", "loop"):
        control = f"go-{backend}-goroutine"
        modes = ("auto",) if backend == "loop" else ("inline", "auto")
        for mode in modes:
            candidate = f"go-{backend}-{mode}"
            for workload in WORKLOADS:
                name = workload["name"]
                for metric in METRICS:
                    a, b = medians[(control, name)][metric], medians[(candidate, name)][metric]
                    worse = b < a if metric == "iops" else b > a
                    if worse and gap(a, b) > floors[(control, name, metric)]:
                        raise ValueError(f"workload regression: {candidate}/{name}/{metric}")
            name = "4k-randread-qd1"
            a, b = medians[(control, name)]["p50_ns"], medians[(candidate, name)]["p50_ns"]
            win = b < a and gap(a, b) > POLICY["win_noise_multiple"] * floors[(control, name, "p50_ns")]
            if backend != "loop" and not win:
                raise ValueError(f"equal within noise is not an optimization win: {candidate}")
            outcomes.append(dict(candidate=candidate, qd1="WIN_BEYOND_NOISE" if win else "EQUAL_WITHIN_NOISE"))
            if backend == "loop":
                continue
            rust = "libublk-rs-null" if backend == "null" else "libublk-rs"
            if backend == "null":
                p50 = medians[(candidate, name)]["p50_ns"]
                for reference in (rust, "ublksrv-null"):
                    if p50 > POLICY["qd1_max_ratio"] * medians[(reference, name)]["p50_ns"]:
                        raise ValueError("qd1 userspace p50 gap exceeds 10%")
            depth = "4k-randread"
            if medians[(candidate, depth)]["iops"] < medians[(rust, depth)]["iops"]:
                raise ValueError("matched-depth throughput is below Rust")
            for workload in WORKLOADS:
                name = workload["name"]
                if medians[(candidate, name)]["p99_ns"] > 2 * medians[(rust, name)]["p99_ns"]:
                    raise ValueError("p99 exceeds 2x matched Rust")
    return dict(observations=len(samples), outcomes=outcomes,
                medians={"/".join(key): value for key, value in medians.items()},
                aa_noise_percent={"/".join(key): value for key, value in floors.items()},
                latency_scope="userspace clat histograms only; kernel inline clat is never a full round trip",
                throughput_scope="meeting Rust (including equality) clears the bound, not a superiority claim")


def check(manifest_path, results, *, source_root=ROOT):
    manifest_path, results = Path(manifest_path).resolve(), Path(results).resolve()
    bundle = manifest_path.parent
    manifest = read_json(manifest_path)
    validate_manifest(manifest, bundle)
    if source_files(source_root) != manifest["candidate"]["files"]:
        raise ValueError("running candidate changed from frozen manifest")
    expected_digest = (bundle / "manifest.sha256").read_text().split()[0]
    if digest(manifest_path) != expected_digest:
        raise ValueError("manifest SHA256 changed from prepared bundle")
    kernels, held = {}, []
    unexpected = {path.name for path in results.iterdir() if path.is_dir()} - set(KERNELS) if results.exists() else set()
    if unexpected:
        held.append(f"unexpected kernel/result directories: {sorted(unexpected)}")
    for slot in KERNELS:
        try:
            kernels[slot] = check_kernel(results / slot, slot, manifest, expected_digest)
        except (OSError, ValueError, KeyError, TypeError) as error:
            held.append(f"{slot}: {error}")
    performance = None
    try:
        if not manifest["references"]:
            raise ValueError("C/Rust reference inputs absent from frozen bundle")
        samples = load_performance(results / "6.12/performance", manifest, expected_digest, bundle)
        if (read_json(results / "6.12/performance/performance.json")["attestation"] !=
                read_json(results / "6.12/receipt.json")["attestation"]):
            raise ValueError("performance window and correctness guest attestations differ")
        performance = performance_decision(samples)
    except (OSError, ValueError, KeyError, TypeError, ImportError) as error:
        held.append(f"matched performance: {error}")
    decision = dict(status="PASS" if not held else "REQUIRED-UNRUN/FAIL", candidate=manifest["candidate"]["commit"],
                    manifest_sha256=expected_digest, kernels=kernels, performance=performance, held=held)
    return decision


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--results", type=Path, required=True)
    args = parser.parse_args()
    decision = check(args.manifest, args.results)
    print(json.dumps(decision, sort_keys=True, indent=2))
    return 0 if decision["status"] == "PASS" else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, TypeError, ImportError) as error:
        print(f"closeout-check: FAIL: {error}", file=sys.stderr)
        sys.exit(1)
