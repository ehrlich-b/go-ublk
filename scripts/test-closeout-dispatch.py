#!/usr/bin/env python3
"""Functional fixtures only: these are not Linux/kernel/performance receipts."""
import copy
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.dont_write_bytecode = True
from closeout_common import (BASE, BENCH, BINARIES, KERNELS, LINUX_GATES, METRICS, POLICY,
                             REF_PINS, WORKLOADS, digest, diagnostics, kernel_slot, object_hash,
                             read_json, schedule, source_files, validate_manifest, variant_backend, write_json)
import closeout_dispatch as runner
from check_closeout import check, performance_decision

ROOT = Path(__file__).resolve().parent.parent


def benchmark():
    text = ""
    for mode in POLICY["zero_alloc_modes"]:
        text += (f"BenchmarkDispatch{mode}-2 1000 100 ns/op 0 B/op 0 allocs/op\n" * 3)
    return text + "PASS\n"


def fixture_metrics(variant, control=False):
    backend = variant_backend(variant)
    old = variant.endswith("goroutine")
    if backend == "loop":
        values = dict(iops=457000, p50_ns=28000, p99_ns=180000, p999_ns=300000, cpu_ns_per_io=5500)
    else:
        values = dict(iops=600000 if old else 751000,
                      p50_ns=35000 if old else 13900, p99_ns=200000 if old else 100000,
                      p999_ns=350000 if old else 180000, cpu_ns_per_io=5700 if old else 4028)
        if variant == "ublksrv-null":
            values["p50_ns"] = 13200
    if control:
        values = {key: value * 1.01 for key, value in values.items()}
    return values


def fixture_samples():
    return [dict(entry=entry, workload=w["name"],
                 metrics=fixture_metrics(entry["implementation"], entry["repeat"] == "control"))
            for entry in schedule() for w in WORKLOADS]


def raw_observation(workload, values):
    jobs = workload["numjobs"]
    per_job_iops = values["iops"] / jobs
    ios = int(per_job_iops * 15)
    total_cpu = values["cpu_ns_per_io"] * ios * jobs / 1e9
    server_usr = int(total_cpu / 3 * 100) / 100
    system = total_cpu / 3
    fio_usr = total_cpu - server_usr - system
    def stats(count):
        return dict(total_ios=count, iops=count / 15, runtime=15000,
                    bw_bytes=count / 15 * {"4k": 4096, "128k": 131072}[workload["bs"]],
                    clat_ns=dict(bins={str(values["p50_ns"]): int(count * .98),
                                      str(values["p99_ns"]): int(count * .015),
                                      str(values["p999_ns"]): count - int(count * .98) - int(count * .015)}))
    if workload["rw"] in ("read", "randread"):
        directions = dict(read=stats(ios))
    elif workload["rw"] in ("write", "randwrite"):
        directions = dict(write=stats(ios))
    else:
        reads = int(ios * .7)
        directions = dict(read=stats(reads), write=stats(ios - reads))
    options = {key: str(value) for key, value in workload.items() if key != "name"}
    options.update(size=str(512 << 20), direct="1", ioengine="io_uring")
    data = dict(jobs=[dict(error=0, job_runtime=15000, usr_cpu=fio_usr / jobs / 15 * 100,
                           sys_cpu=0, **directions, **{"job options": options}) for _ in range(jobs)])
    before = dict(monotonic=10, server=dict(pid=17, start_ticks=3, usr_ticks=100, sys_ticks=100),
                  system=dict(total_ticks=10000, idle_ticks=8000, steal_ticks=0),
                  cgroups={"server": dict(system_usec=100), "fio": dict(system_usec=100)})
    after = copy.deepcopy(before)
    after["monotonic"] = 25
    after["server"]["usr_ticks"] += round(server_usr * 100)
    after["system"].update(total_ticks=16000, idle_ticks=9500)
    after["cgroups"]["server"]["system_usec"] += round(system * 1e6)
    sys.path.insert(0, str(ROOT / ".scratch/bench/lib"))
    from cpu import interval
    cpu = interval(before, after)
    return data, cpu


class CloseoutFixtures(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="closeout-fixtures-", dir=ROOT / ".scratch/tmp")
        cls.template = Path(cls.temporary.name) / "template"
        cls.template.mkdir()
        bundle = cls.template / "bundle"
        bundle.mkdir()
        (bundle / "source").mkdir()
        (bundle / "source/candidate.txt").write_text("fixture source; no kernel execution\n")
        harness = ROOT / ".scratch/bench"
        shutil.copytree(harness, bundle / "harness", ignore=shutil.ignore_patterns(".git", ".scratch", "__pycache__"))
        (bundle / "bin").mkdir()
        for name in BINARIES:
            (bundle / "bin" / name).write_text(f"fixture binary {name}\n")
        references = {}
        for family, pin in REF_PINS.items():
            source = bundle / "references" / family / "source"
            source.mkdir(parents=True)
            (source / "fixture.txt").write_text("functional provenance fixture\n")
            names = ("ublk", "ublk.null") if family == "ublksrv" else ("ramdisk", "null")
            binaries = {}
            for name in names:
                path = bundle / "references" / family / "bin" / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("functional executable fixture\n")
                binaries[name] = str(path.relative_to(bundle))
            libraries = {}
            if family == "ublksrv":
                path = bundle / "references" / family / "lib/libublksrv.so.0"
                path.parent.mkdir()
                path.write_text("functional runtime-library fixture\n")
                libraries[path.name] = str(path.relative_to(bundle))
            references[family] = dict(commit=pin, files=source_files(source), binaries=binaries, libraries=libraries)
        files = source_files(bundle / "source")
        harness_files = source_files(bundle / "harness")
        artifacts = {str(path.relative_to(bundle)): digest(path)
                     for folder in ("harness", "bin", "references") for path in (bundle / folder).rglob("*") if path.is_file()}
        manifest = dict(schema=1, candidate=dict(base=BASE, commit=BASE, files=files, tree_sha256=object_hash(files)),
                        policy=POLICY, binaries={name: digest(bundle / "bin" / name) for name in BINARIES},
                        harness=dict(commit=BENCH, files=harness_files), artifacts=artifacts,
                        references=references)
        write_json(bundle / "manifest.json", manifest)
        manifest_sha = digest(bundle / "manifest.json")
        (bundle / "manifest.sha256").write_text(manifest_sha + "  manifest.json\n")
        attestation = dict(hostname="fixture-guest", disposable=True, fixed_ram_mib=4096, balloon=False,
                           assigned_cpus=[0, 1, 2, 3], server_cpus=[2, 3], fio_cpus=[0, 1],
                           quiet_window_confirmed=True, quiet_window_note="fixture reserved physical cores",
                           performance_window_id="fixture-single-window", kernel_release="6.12.1-fixture")
        for slot in KERNELS:
            directory = cls.template / "results" / slot
            directory.mkdir(parents=True)
            release = slot + (".1-fixture" if slot in ("6.8", "6.12", "7.0") else "-fixture")
            receipt = dict(schema=1, manifest_sha256=manifest_sha, candidate=BASE, tree_sha256=object_hash(files),
                           binaries=manifest["binaries"], kernel_slot=slot, kernel_release=release, status="COMPLETE",
                           attestation=dict(attestation, kernel_release=release), commands=[], cleanup_verified=True,
                           diagnostics=diagnostics("", slot))
            commands = [(target + ".log", ["taskset", "-c", "0,1,2,3", "make", "-j1", target]) for target in LINUX_GATES]
            commands += [(name + ".log", ["taskset", "-c", "0,1,2,3", "/fixture/bin/" + name,
                                          "-test.v", "-test.timeout=5m", "-test.parallel=2"])
                         for name in BINARIES[:4]]
            commands += [("suite-list.log", ["taskset", "-c", "0,1,2,3", "/fixture/bin/ublk-suite", "-list"]),
                         ("suite.log", ["taskset", "-c", "0,1,2,3", "/fixture/bin/ublk-suite", "-scale", "1"])]
            names = [f"fixture/test-{i}" for i in range(62)]
            for log, command in commands:
                text = benchmark() if log == "benchmark-dispatch.log" else "PASS\n"
                if log == "test-unit.log":
                    text = "--- PASS: TestDispatchHotPathDoesNotAllocate (0.00s)\nPASS\n"
                if log == "suite-list.log":
                    text = "\n".join(names) + "\n"
                if log == "suite.log":
                    text = "\n".join(json.dumps(dict(test=name, status="pass", duration_s=.1)) for name in names) + "\n"
                (directory / log).write_text(text)
                receipt["commands"].append(dict(command=command, log=log, returncode=0, sha256=digest(directory / log)))
            for which in ("before", "after", "delta"):
                (directory / f"dmesg-{which}.txt").write_text("")
            write_json(directory / "cleanup.json", dict(verified=True, registrations_after=[], blocks_after=[]))
            write_json(directory / "receipt.json", receipt)
        perf = cls.template / "results/6.12/performance"
        perf.mkdir()
        write_json(perf / "performance.json", dict(manifest_sha256=manifest_sha, candidate=BASE,
                   tree_sha256=object_hash(files), binaries=manifest["binaries"], policy=POLICY,
                   status="COMPLETE", schedule=schedule(), attestation=attestation, cpu_cleanup_verified=True))
        sys.path.insert(0, str(ROOT / ".scratch/bench/lib"))
        from common import fio_command
        journal = []
        for entry in schedule():
            run = perf / "runs" / f"{entry['sequence']:03d}-{entry['implementation']}"
            run.mkdir(parents=True)
            backend = variant_backend(entry["implementation"])
            q, d = POLICY["geometry"][backend]
            lifecycle = dict(entry, status="stopped", cleanup_verified=True, cleanup_forced=False,
                             size_bytes=512 << 20, semantics="null" if backend == "null" else "retaining-ram",
                             effective_queues=q, effective_depth=d, logical_block_bytes=512,
                             device_bytes=(250 << 30) if entry["implementation"] in ("ublksrv-null", "libublk-rs-null")
                             else (512 << 20), precondition_verified=backend != "null")
            variant = entry["implementation"]
            if variant.startswith("go-"):
                mode = variant.split("-")[-1]
                launch = ["/fixture/bin/" + ("ublk-loop" if backend == "loop" else "ublk-mem"),
                          "-size", "512M", "-queues", str(q), "-depth", str(d)]
                launch += ["-file", str(run / "loop-backing/disk.img")] if backend == "loop" else ["-backend", backend]
                launch += ["-inline"] if mode == "inline" else ["-dispatch", mode]
            elif variant == "ublksrv-null":
                launch = ["/fixture/bin/ublk", "add", "-t", "null", "-n", "0", "-q", "2", "-d", "64"]
            elif variant == "libublk-rs-null":
                launch = ["/fixture/bin/null", "add", "-n", "0", "-q", "2", "-d", "64", "--foreground"]
            else:
                launch = ["/fixture/bin/ramdisk", "add", "0", "512"]
            lifecycle["commands"] = [launch]
            write_json(run / "run.json", lifecycle)
            for workload in WORKLOADS:
                name = workload["name"]
                fio_file, cpu_file = run / (name + ".fio.json"), run / (name + ".cpu.json")
                values = fixture_metrics(entry["implementation"], entry["repeat"] == "control")
                data, cpu = raw_observation(workload, values)
                write_json(fio_file, data)
                write_json(cpu_file, cpu)
                meta = dict(entry, manifest_sha256=manifest_sha, candidate=BASE, binaries=manifest["binaries"],
                            workload=name, runtime_seconds=15, numjobs=workload["numjobs"],
                            effective_queues=q, effective_depth=d, size_bytes=512 << 20, logical_block_bytes=512,
                            semantics=lifecycle["semantics"], cpu_isolation="disjoint", server_cpus="2,3", fio_cpus="0,1",
                            cpu_budget="0,1,2,3", command=fio_command("/dev/FIXTURE", workload, fio_file),
                            fio_file=fio_file.name, cpu_file=cpu_file.name, fio_sha256=digest(fio_file), cpu_sha256=digest(cpu_file))
                write_json(run / (name + ".sample.json"), meta)
                journal.append(dict(command=meta["command"], utc="2026-10-10T20:00:00+00:00"))
        (perf / "commands.jsonl").write_text("".join(json.dumps(row) + "\n" for row in journal))
        cls.rehash(cls.template / "results")

    @staticmethod
    def rehash(results):
        for receipt_path in results.glob("*/receipt.json"):
            receipt = read_json(receipt_path)
            directory = receipt_path.parent
            receipt["raw_sha256"] = {str(path.relative_to(directory)): digest(path)
                                     for path in directory.rglob("*") if path.is_file() and path != receipt_path}
            for row in receipt["commands"]:
                if (directory / row["log"]).is_file():
                    row["sha256"] = digest(directory / row["log"])
            write_json(receipt_path, receipt)

    @classmethod
    def tearDownClass(cls):
        cls.temporary.cleanup()

    def setUp(self):
        self.case = Path(self.temporary.name) / self._testMethodName
        shutil.copytree(self.template, self.case)
        self.bundle = self.case / "bundle"
        self.results = self.case / "results"
        self.manifest = self.bundle / "manifest.json"

    def decision(self):
        return check(self.manifest, self.results, source_root=self.bundle / "source")

    def mutate_json(self, path, mutation):
        value = read_json(path)
        mutation(value)
        write_json(path, value)
        self.rehash(self.results)

    def sample(self):
        return next((self.results / "6.12/performance/runs").glob("*/4k-randread.sample.json"))

    def assertHeld(self):
        result = self.decision()
        self.assertNotEqual(result["status"], "PASS", result)
        self.assertTrue(result["held"])

    def test_complete_functional_fixture_passes(self):
        result = self.decision()
        self.assertEqual(result["status"], "PASS", result.get("held"))
        self.assertEqual(result["performance"]["observations"], 288)

    def test_changed_binary_sha_is_rejected(self):
        (self.bundle / "bin/ublk-mem").write_text("changed fixture\n")
        with self.assertRaisesRegex(ValueError, "SHA256"):
            self.decision()

    def test_changed_candidate_source_is_rejected(self):
        (self.bundle / "source/candidate.txt").write_text("changed source\n")
        with self.assertRaisesRegex(ValueError, "source export changed"):
            self.decision()

    def test_weakened_manifest_policy_is_rejected(self):
        value = read_json(self.manifest)
        value["policy"]["qd1_max_ratio"] = 2
        write_json(self.manifest, value)
        with self.assertRaisesRegex(ValueError, "fixed closeout policy"):
            self.decision()

    def test_missing_kernel_cannot_pass(self):
        shutil.rmtree(self.results / "7.3-rc3")
        self.assertHeld()

    def test_mixed_hashes_cannot_pass(self):
        self.mutate_json(self.results / "7.0/receipt.json", lambda row: row.update(candidate="a" * 40))
        self.assertHeld()

    def test_unmatched_geometry_cannot_pass(self):
        self.mutate_json(self.sample(), lambda row: row.update(effective_depth=32))
        self.assertHeld()

    def test_unmatched_raw_fio_geometry_cannot_pass(self):
        meta = read_json(self.sample())
        raw = self.sample().parent / meta["fio_file"]
        data = read_json(raw)
        data["jobs"][0]["job options"]["bs"] = "8k"
        write_json(raw, data)
        self.mutate_json(self.sample(), lambda row: row.update(fio_sha256=digest(raw)))
        self.assertHeld()

    def test_incomplete_raw_tail_histogram_cannot_pass(self):
        meta = read_json(self.sample())
        raw = self.sample().parent / meta["fio_file"]
        data = read_json(raw)
        data["jobs"][0]["read"]["clat_ns"]["bins"] = {"1000": 1}
        write_json(raw, data)
        self.mutate_json(self.sample(), lambda row: row.update(fio_sha256=digest(raw)))
        self.assertHeld()

    def test_incomplete_ab_cannot_pass(self):
        self.sample().unlink()
        self.rehash(self.results)
        self.assertHeld()

    def test_command_journal_reordering_cannot_pass(self):
        path = self.results / "6.12/performance/commands.jsonl"
        lines = path.read_text().splitlines()
        path.write_text("\n".join(reversed(lines)) + "\n")
        self.rehash(self.results)
        self.assertHeld()

    def test_mislabeled_dispatch_launch_cannot_pass(self):
        path = next((self.results / "6.12/performance/runs").glob("*-go-ram-auto/run.json"))
        self.mutate_json(path, lambda row: row["commands"][0].__setitem__(-1, "goroutine"))
        self.assertHeld()

    def test_absent_cleanup_cannot_pass(self):
        (self.results / "7.2.9/cleanup.json").unlink()
        self.rehash(self.results)
        self.assertHeld()

    def test_false_device_cleanup_cannot_pass(self):
        self.mutate_json(self.results / "7.2.9/cleanup.json", lambda row: row.update(registrations_after=["ublkc0"]))
        self.assertHeld()

    def test_absent_cpu_cleanup_cannot_pass(self):
        self.mutate_json(self.results / "6.12/performance/performance.json",
                         lambda row: row.update(cpu_cleanup_verified=False))
        self.assertHeld()

    def test_nonzero_allocations_cannot_pass(self):
        path = self.results / "6.8/benchmark-dispatch.log"
        path.write_text(benchmark().replace("0 allocs/op", "1 allocs/op"))
        self.rehash(self.results)
        self.assertHeld()

    def test_suite_truncation_cannot_pass(self):
        path = self.results / "6.8/suite.log"
        path.write_text("\n".join(path.read_text().splitlines()[:-1]) + "\n")
        self.rehash(self.results)
        self.assertHeld()

    def test_unknown_warning_is_not_suppressed(self):
        delta = "WARNING: CPU: 0 PID: 2 at unrelated_function\n"
        for which in ("after", "delta"):
            (self.results / "7.0" / f"dmesg-{which}.txt").write_text(delta)
        self.mutate_json(self.results / "7.0/receipt.json", lambda row: row.update(diagnostics=diagnostics(delta, "7.0")))
        self.assertHeld()

    def test_known_diagnostic_is_narrow_and_preserved(self):
        line = "UBSAN: array-index-out-of-bounds in io_uring/rsrc.c:1070:12\nio_buffer_register_bvec+0x01\n"
        self.assertEqual(len(diagnostics(line, "7.0")["known_zero_copy_diagnostic"]), 1)
        self.assertTrue(diagnostics(line, "7.3-rc3")["unexpected"])
        self.assertTrue(diagnostics(line + "BUG: unrelated\n", "7.0")["unexpected"])

    def test_kernel_slots_are_exact(self):
        self.assertEqual(kernel_slot("7.2.9-200.fc44.x86_64"), "7.2.9")
        self.assertEqual(kernel_slot("6.8.0-31-generic"), "6.8")
        self.assertEqual(kernel_slot("7.3.0-rc3"), "7.3-rc3")
        for release in ("7.2.8", "7.2.90", "7.3-rc30", "6.9.0"):
            with self.assertRaises(ValueError):
                kernel_slot(release)

    def test_guest_requires_linux_and_disposable_opt_in(self):
        with mock.patch.object(runner.platform, "system", return_value="Darwin"):
            with self.assertRaisesRegex(ValueError, "requires Linux"):
                runner.guest_guard({}, self.bundle)
        with mock.patch.object(runner.platform, "system", return_value="Linux"), mock.patch.dict(os.environ, {}, clear=True):
            with self.assertRaisesRegex(ValueError, "DISPOSABLE"):
                runner.guest_guard({}, self.bundle)

    def test_guest_changed_sha_refuses_before_spawning_commands(self):
        manifest = read_json(self.manifest)
        (self.bundle / "bin/ublk-mem").write_text("changed fixture binary\n")
        attestation = dict(hostname="fixture", kernel_release="6.12.1-fixture", disposable=True,
                           fixed_ram_mib=4096, balloon=False, assigned_cpus=[0, 1, 2, 3],
                           server_cpus=[2, 3], fio_cpus=[0, 1])
        write_json(self.bundle / "guest-attestation.json", attestation)
        real_read = Path.read_text

        def read(path, *args, **kwargs):
            return "MemTotal: 4194304 kB\n" if path == Path("/proc/meminfo") else real_read(path, *args, **kwargs)

        with mock.patch.object(runner.platform, "system", return_value="Linux"), \
                mock.patch.object(runner.platform, "machine", return_value="x86_64"), \
                mock.patch.object(runner.platform, "release", return_value="6.12.1-fixture"), \
                mock.patch.object(runner.os, "geteuid", return_value=0), \
                mock.patch.object(runner.os, "sched_getaffinity", create=True, return_value={0, 1, 2, 3}), \
                mock.patch.object(runner.socket, "gethostname", return_value="fixture"), \
                mock.patch.dict(os.environ, {"GO_UBLK_DISPOSABLE_TEST": "1"}), \
                mock.patch.object(Path, "read_text", read), \
                mock.patch.object(runner.subprocess, "run", side_effect=AssertionError("spawned before hash check")):
            with self.assertRaisesRegex(ValueError, "SHA256"):
                runner.guest_guard(manifest, self.bundle)
    def test_dry_run_does_not_create_output(self):
        destination = self.case / "dry-output"
        command = ["bash", "scripts/closeout-dispatch.sh", "--dry-run", "--out", str(destination)]
        result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(destination.exists())
        self.assertIn("288 timed observations", result.stdout)


class DecisionFixtures(unittest.TestCase):
    def test_pinned_reference_clis_and_geometry_are_inspected(self):
        from closeout_performance import versions
        sys.path.insert(0, str(ROOT / ".scratch/bench/lib"))
        import build
        with tempfile.TemporaryDirectory(prefix="closeout-cli-", dir=ROOT / ".scratch/tmp") as temp:
            bundle = Path(temp)
            rust = bundle / "references/libublk-rs/source/examples"
            rust.mkdir(parents=True)
            (rust / "ramdisk.rs").write_text('.nr_queues(1).depth(128); "add" => {}')
            (rust / "null.rs").write_text('Arg::new("number") Arg::new("queues") Arg::new("depth") Arg::new("foreground")')
            c = bundle / "references/ublksrv/source"
            c.mkdir(parents=True)
            (c / "ublk.null.cpp").write_text('dev_size = 250ULL * 1024 * 1024 * 1024;')
            result = versions(bundle, dict(references={"fixture": True}), build)
            self.assertEqual(result["libublk-rs"]["fixed_queues"], 1)
            self.assertEqual(result["libublk-rs"]["fixed_depth"], 128)
            self.assertTrue(result["ublksrv-null"]["fixed_capacity"])
            (rust / "ramdisk.rs").write_text('.nr_queues(2).depth(64); "add" => {}')
            with self.assertRaisesRegex(ValueError, "geometry"):
                versions(bundle, dict(references={"fixture": True}), build)

    def test_noisy_aa_cannot_pass(self):
        samples = fixture_samples()
        for sample in samples:
            if sample["entry"]["implementation"] == "go-ram-goroutine" and sample["entry"]["repeat"] == "control":
                sample["metrics"]["iops"] *= 2
        with self.assertRaisesRegex(ValueError, "noisy A/A"):
            performance_decision(samples)

    def test_equal_within_noise_is_not_a_win(self):
        samples = fixture_samples()
        for sample in samples:
            if sample["entry"]["implementation"] == "go-ram-auto":
                sample["metrics"] = fixture_metrics("go-ram-goroutine")
        with self.assertRaisesRegex(ValueError, "not an optimization win"):
            performance_decision(samples)

    def test_named_workload_regression_cannot_pass(self):
        samples = fixture_samples()
        for sample in samples:
            if sample["entry"]["implementation"] == "go-loop-auto" and sample["workload"] == "128k-write":
                sample["metrics"]["cpu_ns_per_io"] *= 2
        with self.assertRaisesRegex(ValueError, "workload regression"):
            performance_decision(samples)

    def test_qd1_gap_cannot_pass(self):
        samples = fixture_samples()
        for sample in samples:
            if sample["entry"]["implementation"] == "go-null-auto":
                sample["metrics"]["p50_ns"] *= 1.5
        with self.assertRaisesRegex(ValueError, "gap exceeds 10%"):
            performance_decision(samples)

    def test_depth_below_rust_cannot_pass(self):
        samples = fixture_samples()
        for sample in samples:
            if sample["entry"]["implementation"] == "go-ram-auto":
                sample["metrics"]["iops"] *= .95
        with self.assertRaisesRegex(ValueError, "below Rust"):
            performance_decision(samples)

    def test_p99_bound_cannot_pass(self):
        samples = fixture_samples()
        for sample in samples:
            if sample["entry"]["implementation"] == "libublk-rs":
                sample["metrics"]["p99_ns"] = 1000
        with self.assertRaisesRegex(ValueError, "p99 exceeds"):
            performance_decision(samples)

    def test_absent_aa_pairs_cannot_pass(self):
        samples = [sample for sample in fixture_samples() if sample["entry"]["repeat"] != "control"]
        with self.assertRaisesRegex(ValueError, "A/A pairs"):
            performance_decision(samples)


if __name__ == "__main__":
    (ROOT / ".scratch/tmp").mkdir(parents=True, exist_ok=True)
    unittest.main()
