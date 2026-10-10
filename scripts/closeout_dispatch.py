"""Prepare an offline candidate or run its bounded disposable-guest plan."""
import argparse
import fcntl
import os
from pathlib import Path
import platform
import shutil
import signal
import socket
import subprocess
import sys
import time

from closeout_common import (BASE, BENCH, BINARIES, KERNELS, LINUX_GATES, LOCAL_GATES,
                             POLICY, REF_PINS, contained, diagnostics, digest, kernel_slot,
                             object_hash, read_json, source_files, validate_manifest, write_json)

ROOT = Path(__file__).resolve().parent.parent


def environment(root):
    scratch = root / ".scratch"
    paths = {"TMPDIR": scratch / "tmp", "GOCACHE": scratch / "go-build",
             "GOMODCACHE": scratch / "gomod", "npm_config_cache": scratch / "npm"}
    for path in paths.values():
        path.mkdir(parents=True, exist_ok=True)
    os.environ.update({key: str(value) for key, value in paths.items()})
    os.environ.update(GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local", GOFLAGS="-p=2",
                      GOMAXPROCS="2", npm_config_offline="true", PYTHONDONTWRITEBYTECODE="1")


def scratch_path(path):
    path = Path(path).resolve()
    if not path.is_relative_to(ROOT / ".scratch") or path == ROOT / ".scratch":
        raise ValueError("output and staged inputs must be under this clone's .scratch/")
    return path


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


def clean_export(root, destination, expected=None):
    if git(root, "status", "--porcelain", "--untracked-files=all"):
        raise ValueError(f"commit coherent source before preparing: {root}")
    commit = git(root, "rev-parse", "HEAD")
    if expected and commit != expected:
        raise ValueError(f"wrong staged source pin: {root}: {commit}")
    destination.mkdir(parents=True)
    files = git(root, "ls-files", "-z").split("\0")
    for name in filter(None, files):
        source = contained(root, name)
        target = contained(destination, name)
        if not source.is_file():
            raise ValueError(f"source is not a regular file: {name}")
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)
    return commit, source_files(destination)


def bounded(command, cwd, log, seconds, env=None):
    """One process group, one deadline, one receipt; never restart on timeout."""
    command = list(map(str, command))
    if platform.system() == "Darwin":
        command = ["taskpolicy", "-b", "nice", "-n", "15"] + command
    started = time.monotonic()
    with Path(log).open("wb") as stream:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=stream,
                                   stderr=subprocess.STDOUT, start_new_session=True)
        try:
            code = process.wait(timeout=seconds)
        except BaseException:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
            raise
    return dict(command=command, returncode=code, duration_s=time.monotonic() - started,
                log=Path(log).name, sha256=digest(log))


def stage_references(out):
    input_file = ROOT / ".scratch/closeout-reference-inputs.json"
    if not input_file.exists():
        return {}
    supplied = read_json(input_file)
    if set(supplied) != set(REF_PINS):
        raise ValueError("stage both reference families together")
    refs = {}
    for family, pin in REF_PINS.items():
        row = supplied[family]
        checkout = scratch_path(row["checkout"])
        commit, files = clean_export(checkout, out / "references" / family / "source", pin)
        names = {"ublk", "ublk.null"} if family == "ublksrv" else {"ramdisk", "null"}
        if set(row["binaries"]) != names:
            raise ValueError(f"missing reference executables: {family}: {names}")
        binaries = {}
        for name, artifact in row["binaries"].items():
            original = scratch_path(artifact["path"])
            if digest(original) != artifact["sha256"] or artifact["source_commit"] != pin:
                raise ValueError(f"reference provenance/hash mismatch: {family}/{name}")
            target = out / "references" / family / "bin" / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(original, target)
            binaries[name] = str(target.relative_to(out))
        libraries = {}
        for name, artifact in row.get("libraries", {}).items():
            if Path(name).name != name:
                raise ValueError("unsafe reference runtime library name")
            original = scratch_path(artifact["path"])
            if digest(original) != artifact["sha256"] or artifact["source_commit"] != pin:
                raise ValueError(f"reference library provenance/hash mismatch: {family}/{name}")
            target = out / "references" / family / "lib" / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(original, target)
            libraries[name] = str(target.relative_to(out))
        refs[family] = dict(commit=commit, files=files, binaries=binaries, libraries=libraries,
                            producer_assertion="source commit and artifact hashes supplied by coordinator")
    return refs


def prepare(out):
    out = scratch_path(out)
    if out.exists():
        raise ValueError("prepare output already exists; preserve it and use a new directory")
    if git(ROOT, "status", "--porcelain", "--untracked-files=all"):
        raise ValueError("commit candidate before --prepare")
    if git(ROOT, "rev-parse", "refs/closeout/base-77b8be0") != BASE:
        raise ValueError("immutable base ref is absent or changed")
    subprocess.run(["git", "merge-base", "--is-ancestor", BASE, "HEAD"], cwd=ROOT, check=True)
    out.mkdir(parents=True)
    environment(ROOT)
    start_files = source_files(ROOT)
    receipts = []
    # The local checks are mandatory and are recorded rather than inferred.
    for target in LOCAL_GATES:
        result = bounded(["make", "-j1", target], ROOT, out / (target + ".log"), 1800)
        receipts.append(result)
        write_json(out / "prepare-checks.json", receipts)
        if result["returncode"]:
            raise ValueError(f"local gate failed: {target}; see {out / result['log']}")
    checks = (("syntax", ["bash", "-n", "scripts/closeout-dispatch.sh"]),
              ("dry-run", ["bash", "scripts/closeout-dispatch.sh", "--dry-run"]),
              ("fixtures", [sys.executable, "scripts/test-closeout-dispatch.py"]),
              ("diff-check", ["git", "diff", "--check"]))
    for name, command in checks:
        result = bounded(command, ROOT, out / (name + ".log"), 600)
        receipts.append(result)
        write_json(out / "prepare-checks.json", receipts)
        if result["returncode"]:
            raise ValueError(f"closeout local check failed: {name}; see {out / result['log']}")
    result = bounded(["make", "-j1", "suite", "GOARCH=amd64",
                      "SUITE_BINARY=.scratch/vm-bin/ublk-suite"], ROOT, out / "suite-build.log", 600)
    receipts.append(result)
    write_json(out / "prepare-checks.json", receipts)
    if result["returncode"]:
        raise ValueError("Linux suite cross-build failed; see suite-build.log")
    if source_files(ROOT) != start_files:
        raise ValueError("candidate changed during preparation")
    commit, files = clean_export(ROOT, out / "source")
    if files != start_files:
        raise ValueError("untracked or ignored source/configuration influenced the candidate")
    harness_commit, harness_files = clean_export(ROOT / ".scratch/bench", out / "harness", BENCH)
    (out / "bin").mkdir()
    binaries = {}
    for name in BINARIES:
        source = ROOT / ".scratch/vm-bin" / name
        with source.open("rb") as stream:
            header = stream.read(20)
        if header[:5] != b"\x7fELF\x02" or header[18:20] != b"\x3e\x00":
            raise ValueError(f"binary is not Linux amd64 ELF: {name}")
        shutil.copy2(source, out / "bin" / name)
        binaries[name] = digest(out / "bin" / name)
    refs = stage_references(out)
    if refs:
        from closeout_performance import harness_modules, versions
        _, build = harness_modules(out)
        versions(out, dict(references=refs), build)
    # Package the isolated module cache; a guest still needs an installed local Go toolchain.
    shutil.copytree(ROOT / ".scratch/gomod", out / "offline/gomod",
                    ignore=shutil.ignore_patterns("*.lock"))
    artifacts = {}
    for folder in ("bin", "harness", "references", "offline"):
        for path in sorted((out / folder).rglob("*")):
            if path.is_file():
                artifacts[str(path.relative_to(out))] = digest(path)
    manifest = dict(schema=1, policy=POLICY,
                    candidate=dict(base=BASE, commit=commit, files=files, tree_sha256=object_hash(files)),
                    binaries=binaries, harness=dict(commit=harness_commit, files=harness_files),
                    artifacts=artifacts, references=refs, local_checks=receipts,
                    performance_status="READY" if refs else "REQUIRED-UNRUN: reference inputs absent")
    validate_manifest(manifest, out)
    write_json(out / "manifest.json", manifest)
    (out / "manifest.sha256").write_text(digest(out / "manifest.json") + "  manifest.json\n")
    write_json(out / "guest-attestation.example.json", dict(
        hostname="COORDINATOR_DISPOSABLE_GUEST", disposable=True,
        fixed_ram_mib=4096, balloon=False, assigned_cpus=[0, 1, 2, 3],
        server_cpus=[2, 3], fio_cpus=[0, 1], quiet_window_confirmed=False,
        quiet_window_note="Record physical-core/SMT reservation, host quietness and hopper/NGN precedence",
        performance_window_id="ONE_COORDINATOR_WINDOW", kernel_release="EXACT_UNAME_R"))
    (out / "guest-plan.txt").write_text(plan())
    print(f"PREPARED {out / 'manifest.json'}\nCandidate {commit}\nManifest SHA256 {digest(out / 'manifest.json')}")
    print(manifest["performance_status"])


def plan():
    return "\n".join([
        "No transport, VM state, pinning, kernel change, package install, or release operations.",
        "Transfer the exact clone plus .scratch/closeout bundle; preserve manifest.sha256 separately.",
        "Use only separately authorized disposable amd64 Linux guests: fixed 4 GiB, no balloon;",
        "CPU IDs must be coordinator-assigned after hopper/NGN reservations. Run as root;",
        "Go with offline module cache, C compiler, fio, util-linux, mkfs.ext4 and mkfs.xfs must already exist.",
        "ublk_drv must already be loaded. Copy guest-attestation.example.json to guest-attestation.json",
        "beside the manifest and fill exact hostname/kernel/CPU/RAM attestations for EACH guest.",
        "Only 6.12 runs the one new quiet performance window; it requires both pinned reference inputs",
        "staged before preparation and a confirmed physical-core/SMT quiet-window note.",
        "RAM peers use 1 queue/depth 128 (pinned Rust ramdisk); null/loop use 2/depth 64.",
        "All peers touch 512 MiB, 512-byte logical blocks, identical six fio workloads and CPU budgets.",
        "Three rotated A/B rounds plus pilot and interleaved A/A: 48 devices, 288 timed observations.",
        "Suite runs unfiltered at scale=1. Capture dmesg before/after and verify no registration remains.",
        "Known 7.0/7.2.6/7.2.9 io_buffer_register_bvec UBSAN is reported separately; other warnings fail.",
        "A timeout/failure retains outputs and does not authorize a rerun, reboot, or second window.",
        *[f"REQUIRED-UNRUN {kernel}: GO_UBLK_DISPOSABLE_TEST=1 bash scripts/closeout-dispatch.sh "
          "--guest --manifest .scratch/closeout/manifest.json --out .scratch/closeout/results"
          for kernel in KERNELS],
        "Return each results/<kernel-slot>/ directory unchanged, including raw logs and receipt.json.",
        "python3 scripts/check-closeout.py --manifest .scratch/closeout/manifest.json "
        "--results .scratch/closeout/results", ""])


def registrations():
    directory = Path("/sys/class/ublk-char")
    if not directory.is_dir():
        raise ValueError("cannot verify device cleanup: /sys/class/ublk-char absent")
    return sorted(path.name for path in directory.glob("ublkc*"))


def guest_guard(manifest, bundle):
    if platform.system() != "Linux" or os.environ.get("GO_UBLK_DISPOSABLE_TEST") != "1":
        raise ValueError("guest requires Linux and GO_UBLK_DISPOSABLE_TEST=1")
    if platform.machine() != "x86_64" or os.geteuid() != 0:
        raise ValueError("guest requires root on Linux amd64; runner never elevates")
    release = platform.release()
    slot = kernel_slot(release)
    attestation = read_json(bundle / "guest-attestation.json")
    if (attestation["hostname"] != socket.gethostname() or attestation["kernel_release"] != release or
            attestation["disposable"] is not True or attestation["fixed_ram_mib"] != 4096 or
            attestation["balloon"] is not False):
        raise ValueError("exact disposable-guest, kernel, fixed-RAM/no-balloon attestation required")
    assigned = set(attestation["assigned_cpus"])
    server, fio = set(attestation["server_cpus"]), set(attestation["fio_cpus"])
    if (not assigned or any(type(cpu) is not int or cpu < 0 for cpu in assigned) or
            len(server) < 2 or len(fio) < 2 or server & fio or server | fio != assigned or
            not assigned <= os.sched_getaffinity(0)):
        raise ValueError("invalid coordinator CPU assignment or disjoint masks")
    total = int(Path("/proc/meminfo").read_text().split("MemTotal:")[1].split()[0])
    if not 3500 * 1024 <= total <= 4200 * 1024:
        raise ValueError("guest RAM does not match fixed 4 GiB")
    if list(Path("/sys/bus/virtio/drivers/virtio_balloon").glob("virtio*")):
        raise ValueError("active balloon device is forbidden")
    # Check every byte before spawning commands, even suite -list or VM detection.
    manifest_path = bundle / "manifest.json"
    if (read_json(manifest_path) != manifest or
            digest(manifest_path) != (bundle / "manifest.sha256").read_text().split()[0]):
        raise ValueError("manifest SHA256 changed from prepared bundle")
    validate_manifest(manifest, bundle)
    if source_files(ROOT) != manifest["candidate"]["files"]:
        raise ValueError("running clone changed from the prepared candidate")
    if (ROOT / ".git").exists() and git(ROOT, "rev-parse", "HEAD") != manifest["candidate"]["commit"]:
        raise ValueError("running clone HEAD changed from the prepared candidate")
    detect = subprocess.run(["systemd-detect-virt", "--vm"], capture_output=True, text=True, check=False)
    if detect.returncode:
        raise ValueError("a detected disposable VM is required")
    if not Path("/dev/ublk-control").is_char_device() or registrations():
        raise ValueError("ublk driver must be loaded and have no pre-existing devices")
    if list(Path("/sys/class/block").glob("ublkb*")):
        raise ValueError("pre-existing ublk block device")
    for tool in ("make", "go", "gcc", "dmesg", "fio", "taskset", "blockdev", "mount", "umount",
                 "mountpoint", "ps", "mkfs.ext4", "mkfs.xfs"):
        if shutil.which(tool) is None:
            raise ValueError(f"missing offline guest dependency: {tool}")
    return slot, release, attestation


def guest(manifest_path, out):
    manifest_path = Path(manifest_path).resolve()
    bundle = scratch_path(manifest_path.parent)
    manifest = read_json(manifest_path)
    if digest(manifest_path) != (bundle / "manifest.sha256").read_text().split()[0]:
        raise ValueError("manifest SHA256 changed from prepared bundle")
    slot, release, attestation = guest_guard(manifest, bundle)
    # Hold an existing-job lock. A repeated command cannot duplicate a window.
    out = scratch_path(out)
    out.mkdir(parents=True, exist_ok=True)
    lock = (ROOT / ".scratch/closeout-guest.lock").open("a+")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    destination = out / slot
    destination.mkdir()  # Refuse overwrite, including failed/incomplete previous attempts.
    environment(ROOT)
    os.environ["GOMODCACHE"] = str(bundle / "offline/gomod")
    receipt = dict(schema=1, manifest_sha256=digest(manifest_path), candidate=manifest["candidate"]["commit"],
                   tree_sha256=manifest["candidate"]["tree_sha256"], binaries=manifest["binaries"],
                   kernel_slot=slot, kernel_release=release, attestation=attestation,
                   status="RUNNING", commands=[], cleanup_verified=False, raw_sha256={})
    write_json(destination / "receipt.json", receipt)
    before = subprocess.check_output(["dmesg", "--raw"], text=True)
    (destination / "dmesg-before.txt").write_text(before)
    cpu_prefix = ["taskset", "-c", ",".join(map(str, sorted(attestation["assigned_cpus"])))]
    try:
        for target in LINUX_GATES:
            row = bounded(cpu_prefix + ["make", "-j1", target], ROOT, destination / (target + ".log"), 1800)
            receipt["commands"].append(row)
            write_json(destination / "receipt.json", receipt)
            if row["returncode"]:
                raise ValueError(f"Linux gate failed: {target}")
        for name in BINARIES[:4]:
            row = bounded(cpu_prefix + [bundle / "bin" / name, "-test.v", "-test.timeout=5m", "-test.parallel=2"],
                          ROOT, destination / (name + ".log"), 360)
            receipt["commands"].append(row)
            if row["returncode"]:
                raise ValueError(f"packaged Linux binary failed: {name}")
        for name, arguments in (("suite-list", ["-list"]), ("suite", ["-scale", "1"])):
            row = bounded(cpu_prefix + [bundle / "bin/ublk-suite", *arguments], ROOT,
                          destination / (name + ".log"), 3600)
            receipt["commands"].append(row)
            if row["returncode"]:
                raise ValueError(f"real-kernel suite failed: {name}")
        receipt["performance_status"] = "NOT_REQUIRED_ON_THIS_KERNEL"
        if slot == POLICY["performance_kernel"]:
            if not manifest["references"]:
                receipt["performance_status"] = "REQUIRED-UNRUN: reference inputs absent"
            else:
                row = bounded([sys.executable, ROOT / "scripts/closeout_performance.py",
                               "--manifest", manifest_path, "--out", destination / "performance"],
                              ROOT, destination / "performance.log", 10800)
                receipt["commands"].append(row)
                receipt["performance_status"] = "COMPLETE" if row["returncode"] == 0 else "FAIL"
                if row["returncode"]:
                    raise ValueError("matched performance collection failed")
        receipt["status"] = "COMPLETE"
    except BaseException as error:
        receipt.update(status="FAIL", error=str(error))
        raise
    finally:
        try:
            after = subprocess.check_output(["dmesg", "--raw"], text=True)
            (destination / "dmesg-after.txt").write_text(after)
            if not after.startswith(before):
                raise ValueError("dmesg wrapped or changed: delta cannot be proved")
            delta = after[len(before):]
            (destination / "dmesg-delta.txt").write_text(delta)
            receipt["diagnostics"] = diagnostics(delta, slot)
            remaining = registrations()
            blocks = sorted(path.name for path in Path("/sys/class/block").glob("ublkb*"))
            cleanup = dict(registrations_after=remaining, blocks_after=blocks,
                           verified=not remaining and not blocks)
            write_json(destination / "cleanup.json", cleanup)
            receipt["cleanup_verified"] = cleanup["verified"]
            validate_manifest(manifest, bundle)
            if source_files(ROOT) != manifest["candidate"]["files"]:
                raise ValueError("candidate changed while guest gates ran")
            if not cleanup["verified"] or receipt["diagnostics"]["unexpected"]:
                raise ValueError("cleanup or unexpected kernel diagnostic failed")
        except BaseException as error:
            receipt.update(status="FAIL", finalization_error=str(error))
        receipt["raw_sha256"] = {str(path.relative_to(destination)): digest(path)
                                 for path in sorted(destination.rglob("*"))
                                 if path.is_file() and path.name != "receipt.json"}
        write_json(destination / "receipt.json", receipt)
    if receipt["status"] != "COMPLETE" or receipt["performance_status"].startswith("REQUIRED-UNRUN"):
        raise ValueError("guest acceptance held; inspect receipt.json")
    print(f"COLLECTED {slot}: {destination / 'receipt.json'} (six-kernel decision still required)")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_mutually_exclusive_group(required=True)
    modes.add_argument("--dry-run", action="store_true")
    modes.add_argument("--prepare", action="store_true")
    modes.add_argument("--guest", action="store_true")
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    if args.dry_run:
        print(plan(), end="")
    elif args.prepare and args.out and not args.manifest:
        prepare(args.out)
    elif args.guest and args.out and args.manifest:
        guest(args.manifest, args.out)
    else:
        parser.error("--prepare requires --out; --guest requires --manifest and --out")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f"closeout: {error}", file=sys.stderr)
        sys.exit(1)
