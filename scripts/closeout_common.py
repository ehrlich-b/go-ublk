"""Fixed closeout contract and hash checks; no network or guest operations."""
import hashlib
import json
import math
import os
from pathlib import Path
import re

BASE = "77b8be0b0b01dfd9be08189dc6e3c5ce6a0d9df0"
BENCH = "3dcf845fcb09e859cdf9642287923a6b6e8af944"
KERNELS = ("6.8", "6.12", "7.0", "7.2.6", "7.2.9", "7.3-rc3")
BINARIES = ("queue.test", "ublk.test", "ublk-mem.test", "ublk-loop.test",
            "ublk-mem", "ublk-loop", "ublk-suite")
LOCAL_GATES = ("check-dispatch-linux", "test-dispatch-portable", "test-perf-cleanup",
               "dispatch-vm-binaries")
LINUX_GATES = ("test-unit", "test-race", "benchmark-dispatch")
WORKLOADS = (
    dict(name="4k-randread", rw="randread", bs="4k", iodepth=32, numjobs=2),
    dict(name="4k-randwrite", rw="randwrite", bs="4k", iodepth=32, numjobs=2),
    dict(name="4k-randrw-70r30w", rw="randrw", bs="4k", iodepth=32, numjobs=2, rwmixread=70),
    dict(name="4k-randread-qd1", rw="randread", bs="4k", iodepth=1, numjobs=1),
    dict(name="128k-read", rw="read", bs="128k", iodepth=8, numjobs=1),
    dict(name="128k-write", rw="write", bs="128k", iodepth=8, numjobs=1),
)
VARIANTS = ("go-ram-goroutine", "go-ram-inline", "go-ram-auto",
            "go-null-goroutine", "go-null-inline", "go-null-auto",
            "go-loop-goroutine", "go-loop-auto", "libublk-rs", "libublk-rs-null", "ublksrv-null")
CONTROLS = ("go-ram-goroutine", "go-null-goroutine", "go-loop-goroutine")
METRICS = ("iops", "p50_ns", "p99_ns", "p999_ns", "cpu_ns_per_io")
POLICY = dict(kernels=list(KERNELS), performance_kernel="6.12", rounds=3,
              geometry={"ram": [1, 128], "null": [2, 64], "loop": [2, 64]},
              size_bytes=512 << 20, block_bytes=512,
              runtime_seconds=15, warmup_seconds=3, seed=20261010,
              workloads=list(WORKLOADS), variants=list(VARIANTS), controls=list(CONTROLS),
              max_noise_percent=10, win_noise_multiple=2,
              qd1_max_ratio=1.10, depth_min_rust_ratio=1.0, p99_max_rust_ratio=2.0,
              zero_alloc_modes=["Inline", "Pool", "Auto", "Adaptive"],
              guest_ram_mib=4096, guest_balloon=False, max_measurement_windows=1)
REF_PINS = {"ublksrv": "abbfea2b59184b26e7212ce3d4c47702450510df",
            "libublk-rs": "479f3097e128d595877185781987d218fe78c047"}


def digest(path):
    h = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def object_hash(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def read_json(path):
    return json.loads(Path(path).read_text())


def write_json(path, value):
    path = Path(path)
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(json.dumps(value, sort_keys=True, indent=2) + "\n")
    temporary.replace(path)


def contained(root, name):
    root = Path(root).resolve()
    relative = Path(name)
    if relative.is_absolute() or ".." in relative.parts:
        raise ValueError(f"unsafe relative path: {name}")
    path = root / relative
    if not path.resolve().is_relative_to(root) or path.is_symlink():
        raise ValueError(f"path escapes bundle or is a symlink: {name}")
    return path


def verify_files(root, files):
    for name, expected in files.items():
        if not re.fullmatch(r"[0-9a-f]{64}", expected):
            raise ValueError(f"invalid SHA256: {name}")
        path = contained(root, name)
        if not path.is_file() or digest(path) != expected:
            raise ValueError(f"missing file or changed SHA256: {name}")


def source_files(root):
    """Exports contain only source. Caches/results belong under .scratch."""
    root = Path(root)
    result = {}
    for directory, children, files in os.walk(root):
        if Path(directory) == root:
            children[:] = [name for name in children if name not in (".git", ".scratch")]
        for name in children + files:
            path = Path(directory) / name
            if path.is_symlink():
                raise ValueError(f"source symlink is unsupported: {path.relative_to(root)}")
        for name in sorted(files):
            path = Path(directory) / name
            result[str(path.relative_to(root))] = digest(path)
    return result


def kernel_slot(release):
    # Minor families permit distro patch/build suffixes, never a different minor.
    for slot in KERNELS:
        pattern = (r"7\.3(?:\.0)?-rc3(?:[-+].*)?" if slot == "7.3-rc3" else
                   re.escape(slot) + (r"(?:\.\d+)?(?:[-+].*)?" if slot in ("6.8", "6.12", "7.0")
                                     else r"(?:[-+].*)?"))
        if re.fullmatch(pattern, release):
            return slot
    raise ValueError(f"kernel is outside the six-kernel plan: {release}")


def schedule():
    entries = []
    for control in CONTROLS:
        for repeat in ("primary", "control"):
            entries.append(dict(phase="calibration", round=0, implementation=control, repeat=repeat))
    for round_number in range(1, 4):
        offset = round_number - 1
        for variant in VARIANTS[offset:] + VARIANTS[:offset]:
            entries.append(dict(phase="measured", round=round_number,
                                implementation=variant, repeat="primary"))
        for control in CONTROLS:
            entries.append(dict(phase="measured", round=round_number,
                                implementation=control, repeat="control"))
    for number, entry in enumerate(entries, 1):
        backend = variant_backend(entry["implementation"])
        queues, depth = POLICY["geometry"][backend]
        entry.update(sequence=number, requested_queues=queues, requested_depth=depth)
    return entries


def variant_backend(variant):
    if variant in ("libublk-rs-null", "ublksrv-null") or variant.startswith("go-null-"):
        return "null"
    return "loop" if variant.startswith("go-loop-") else "ram"


def validate_manifest(manifest, bundle, *, check_source=True):
    if manifest.get("schema") != 1 or manifest.get("policy") != POLICY:
        raise ValueError("manifest does not match the fixed closeout policy")
    candidate = manifest["candidate"]
    if candidate["base"] != BASE or not re.fullmatch(r"[0-9a-f]{40}", candidate["commit"]):
        raise ValueError("wrong candidate base or invalid commit")
    if candidate["tree_sha256"] != object_hash(candidate["files"]):
        raise ValueError("candidate source fingerprint changed")
    if manifest["harness"]["commit"] != BENCH:
        raise ValueError("wrong benchmark harness pin")
    if set(manifest["binaries"]) != set(BINARIES):
        raise ValueError("incomplete binary set")
    for name, value in manifest["binaries"].items():
        if manifest["artifacts"].get("bin/" + name) != value:
            raise ValueError("binary hashes disagree with package hashes")
    verify_files(bundle, manifest["artifacts"])
    if check_source and source_files(Path(bundle) / "source") != candidate["files"]:
        raise ValueError("candidate source export changed")
    for name, value in manifest["harness"]["files"].items():
        if manifest["artifacts"].get("harness/" + name) != value:
            raise ValueError("harness hashes disagree with package hashes")
    if manifest.get("references") and set(manifest["references"]) != set(REF_PINS):
        raise ValueError("both pinned reference families are required")
    for family, row in manifest.get("references", {}).items():
        if row["commit"] != REF_PINS[family]:
            raise ValueError("reference source pin changed")
        names = {"ublk", "ublk.null"} if family == "ublksrv" else {"ramdisk", "null"}
        if set(row["binaries"]) != names:
            raise ValueError("reference executable set is incomplete")
        for name, relative in row["binaries"].items():
            if relative != f"references/{family}/bin/{name}" or relative not in manifest["artifacts"]:
                raise ValueError("reference binary is outside the frozen artifact set")
        for name, sha in row["files"].items():
            if manifest["artifacts"].get(f"references/{family}/source/{name}") != sha:
                raise ValueError("reference source hashes disagree with frozen artifacts")
        if source_files(Path(bundle) / "references" / family / "source") != row["files"]:
            raise ValueError("reference source export changed")
        libraries = row.get("libraries", {})
        if family == "ublksrv" and not any(re.fullmatch(r"libublksrv\.so(?:\.\d+)*", name) for name in libraries):
            raise ValueError("C reference libublksrv runtime library is absent")
        for name, relative in libraries.items():
            if (Path(name).name != name or relative != f"references/{family}/lib/{name}" or
                    relative not in manifest["artifacts"]):
                raise ValueError("reference runtime library is outside the frozen artifact set")
    return candidate


def positive(value, label):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
        raise ValueError(f"missing or invalid {label}")
    return value


def diagnostics(delta, slot):
    """Classify one known report signature; every other warning stays fatal."""
    warnings = re.compile(r"WARNING:|BUG:|Oops:|UBSAN:|KASAN:|Kernel panic|soft lockup|"
                          r"blocked for more than \d+ seconds|general protection fault|"
                          r"refcount_t:|list_(?:add|del) corruption", re.I)
    known, failures = [], []
    lines = delta.splitlines()
    for i, line in enumerate(lines):
        if not warnings.search(line):
            continue
        if (slot in ("7.0", "7.2.6", "7.2.9") and
                re.search(r"UBSAN: array-index-out-of-bounds in \S*io_uring/rsrc\.c:\d+:\d+", line) and
                any("io_buffer_register_bvec" in nearby for nearby in lines[i:i + 35])):
            known.append(line)
        else:
            failures.append(line)
    return dict(known_zero_copy_diagnostic=known, unexpected=failures)
