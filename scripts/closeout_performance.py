"""One offline quiet window using the pinned harness's device/CPU lifecycle."""
import argparse
import json
import os
from pathlib import Path
import shutil
import signal
import sys

from closeout_common import (POLICY, VARIANTS, WORKLOADS, digest, read_json,
                             schedule, variant_backend, write_json)
from closeout_dispatch import ROOT, guest_guard, scratch_path


def harness_modules(bundle):
    sys.path.insert(0, str(bundle / "harness/lib"))
    import harness
    import build
    return harness, build


def versions(bundle, manifest, build):
    refs = manifest["references"]
    if not refs:
        raise ValueError("pinned offline C/Rust source and executable inputs are absent")
    rust_source = bundle / "references/libublk-rs/source/examples"
    rust_ram = build.rust_example(rust_source / "ramdisk.rs", "ramdisk")
    if (rust_ram["fixed_queues"], rust_ram["fixed_depth"]) != tuple(POLICY["geometry"]["ram"]):
        raise ValueError("pinned Rust RAM geometry does not match the declared plan")
    c_source = bundle / "references/ublksrv/source"
    null_sources = [path for path in (c_source / "ublk.null.cpp", c_source / "targets/ublk.null.cpp")
                    if path.is_file()]
    if len(null_sources) != 1:
        raise ValueError("pinned C null CLI source is missing or ambiguous")
    return {"libublk-rs": rust_ram,
            "libublk-rs-null": build.rust_example(rust_source / "null.rs", "null"),
            "ublksrv-null": build.ublksrv_null_options(null_sources[0])}


def collect(manifest_path, out):
    manifest_path = manifest_path.resolve()
    bundle = manifest_path.parent
    manifest = read_json(manifest_path)
    slot, _, attestation = guest_guard(manifest, bundle)
    if slot != "6.12" or not attestation["quiet_window_confirmed"] or not attestation["quiet_window_note"].strip():
        raise ValueError("the sole 6.12 performance window needs an explicit physical-core quiet attestation")
    if not attestation["performance_window_id"].strip():
        raise ValueError("coordinator measurement-window ID required")
    out = scratch_path(out)
    out.mkdir()  # Never overwrite, retry, or silently create a second window.
    h, build = harness_modules(bundle)
    version_info = versions(bundle, manifest, build)
    marker = ROOT / ".scratch/closeout-performance-window.json"
    with marker.open("x") as stream:
        json.dump(dict(manifest_sha256=digest(manifest_path),
                       window_id=attestation["performance_window_id"], output=str(out)), stream)
    binaries = {name: str(bundle / "bin/ublk-mem") for name in VARIANTS if name.startswith("go-")}
    for name in VARIANTS:
        if name.startswith("go-loop-"):
            binaries[name] = str(bundle / "bin/ublk-loop")
    refs = manifest["references"]
    binaries.update(ublk=str(bundle / refs["ublksrv"]["binaries"]["ublk"]),
                    **{"libublk-rs": str(bundle / refs["libublk-rs"]["binaries"]["ramdisk"]),
                       "libublk-rs-null": str(bundle / refs["libublk-rs"]["binaries"]["null"]),
                       "ublksrv-null": str(bundle / refs["ublksrv"]["binaries"]["ublk.null"])})
    loop_backing = {}
    original_server_command = h.server_command

    def dispatch_command(impl, bins, versions, queues, depth, device_id=None, backing=None):
        if not impl.startswith("go-"):
            return original_server_command(impl, bins, versions, queues, depth, device_id, backing)
        backend, mode = impl.split("-")[1:]
        command = [bins[impl], "-size", "512M", "-queues", str(queues), "-depth", str(depth)]
        command += ["-file", str(loop_backing[impl])] if backend == "loop" else ["-backend", backend]
        command += ["-inline"] if mode == "inline" else ["-dispatch", mode]
        return command

    # Extend only the staged harness's in-memory target table; its source stays pinned.
    for name in VARIANTS:
        if name.startswith("go-"):
            backend = variant_backend(name)
            h.TARGETS[name] = dict(family="go-ublk", rung="L2" if backend == "null" else "L3",
                                   semantics="null" if backend == "null" else "retaining-ram")
    h.server_command = dispatch_command

    class DispatchDevice(h.Device):
        def start(self):
            if self.impl.startswith("go-loop-"):
                mount = self.private_mount("tmpfs", "loop-backing", "size=600m,mode=0700,noswap")
                backing = mount / "disk.img"
                with backing.open("xb") as stream:
                    stream.truncate(512 << 20)
                loop_backing[self.impl] = backing
            super().start()
            logical = int(self.command(["blockdev", "--getss", str(self.device)], timeout=10).strip())
            if logical != 512:
                raise ValueError("logical block geometry differs from the plan")
            if (self.receipt["effective_queues"], self.receipt["effective_depth"]) != (
                    self.entry["requested_queues"], self.entry["requested_depth"]):
                raise ValueError("accepted geometry differs from the declared comparison")
            self.receipt["logical_block_bytes"] = logical
            self.save()

    for tool in ("dd", "fio", "ps", "blockdev", "mount", "umount", "mountpoint", "modprobe"):
        if shutil.which(tool) is None:
            raise ValueError(f"missing offline measurement dependency: {tool}")
    mask = lambda cpus: ",".join(map(str, sorted(cpus)))
    server, fio = mask(attestation["server_cpus"]), mask(attestation["fio_cpus"])
    config = dict(cpus=mask(attestation["assigned_cpus"]), server_cpus=server, fio_cpus=fio,
                  cpu_isolation="disjoint", seed=POLICY["seed"])
    commands = h.Commands(out)
    commands.env["GOMAXPROCS"] = "2"
    commands.env["LD_LIBRARY_PATH"] = str(bundle / "references/ublksrv/lib")
    commands.env.pop("LD_PRELOAD", None)
    budget = h.CpuBudget(config["cpus"], "ublk-closeout-" + str(os.getpid()), server, fio)
    commands.budget = budget
    performance = dict(manifest_sha256=digest(manifest_path), candidate=manifest["candidate"]["commit"],
                       tree_sha256=manifest["candidate"]["tree_sha256"], binaries=manifest["binaries"],
                       schedule=schedule(), policy=POLICY, attestation=attestation, status="RUNNING")
    write_json(out / "performance.json", performance)
    signal.signal(signal.SIGINT, h.signal_stop)
    signal.signal(signal.SIGTERM, h.signal_stop)
    try:
        budget.enter()
        for entry in schedule():
            directory = out / "runs" / f"{entry['sequence']:03d}-{entry['implementation']}"
            directory.mkdir(parents=True)
            device = DispatchDevice(dict(config, depth=entry["requested_depth"]), entry,
                                    binaries, version_info, commands, budget, directory)
            try:
                device.start()
                device.measure()
            finally:
                h.close_device(device)
            for workload in WORKLOADS:
                path = directory / (workload["name"] + ".sample.json")
                meta = read_json(path)
                meta.update(manifest_sha256=digest(manifest_path), candidate=performance["candidate"],
                            binaries=manifest["binaries"], logical_block_bytes=512)
                write_json(path, meta)
        performance["status"] = "COMPLETE"
    except BaseException as error:
        performance.update(status="FAIL", error=str(error))
        raise
    finally:
        try:
            budget.close()
            performance["cpu_cleanup_verified"] = True
        except BaseException as error:
            performance.update(status="FAIL", cleanup_error=str(error), cpu_cleanup_verified=False)
        write_json(out / "performance.json", performance)
    if performance["status"] != "COMPLETE":
        raise ValueError("performance lifecycle failed; retain evidence and stop")
    print(f"COLLECTED {len(schedule()) * len(WORKLOADS)} hash-bound observations; checker decides acceptance")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    try:
        collect(args.manifest, args.out)
    except (OSError, ValueError, KeyError, TypeError, RuntimeError) as error:
        print(f"closeout-performance: {error}", file=sys.stderr)
        sys.exit(1)
