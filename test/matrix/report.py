#!/usr/bin/env python3
"""Kernel-matrix results.

  report.py parse OUTDIR --id ID --rc N --wall S --accel A --timeout T
                   --kinfo F --fetch F --commit-file F
      Turn one guest's console.log + results.log into OUTDIR/run.json.

  report.py reparse RUNDIR [RUNDIR ...]
      Re-derive every run.json under RUNDIR from its saved logs (after a
      classifier change, e.g. a new entry in KNOWN_KERNEL_BUGS).

  report.py aggregate RUNDIR [RUNDIR ...] --out DIR [--manifest kernels.json]
      Merge run.json files (a later RUNDIR wins per id), add fetch-failed
      kernels from the manifest, and write DIR/matrix.json (the docs-site
      schema, nothing more), DIR/summary.md and DIR/runs-detail.json.
"""
import argparse
import datetime
import glob
import json
import os
import re
import sys

OOPS_RE = re.compile(r"(Oops[:\s]|BUG: |kernel BUG at|general protection fault|Unable to handle kernel|"
                     r"KASAN:|UBSAN:|Kernel panic|soft lockup|blocked for more than \d+ seconds|"
                     r"refcount_t: |list_add corruption|list_del corruption)")
WARN_RE = re.compile(r"WARNING: CPU: \d+ PID: \d+ at (\S+)")
# Kernel reports already root-caused as kernel bugs that are not go-ublk's
# fault: (name, regex for the report line, regex that must also appear in the
# kernel log, note). Matching lines are recorded, not counted as an oops.
KNOWN_KERNEL_BUGS = [
    ("io_uring-counted_by-ubsan",
     re.compile(r"UBSAN: array-index-out-of-bounds in \S*io_uring/rsrc\.c"),
     re.compile(r"io_buffer_register_bvec"),
     "known kernel bug, not a product failure: UBSAN __counted_by false positive in io_uring "
     "io_buffer_register_bvec under ublk zero copy (site/content/guide/kernel-bugs.md"
     "#found-by-go-ublks-kernel-matrix)"),
]
MARK = "@@GOUBLK@@ "
SUMMARY_KEYS = ("pass", "fail", "skip", "error")


def now():
    return datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat()


def load(path, default=None):
    try:
        with open(path) as f:
            return json.load(f)
    except (OSError, ValueError):
        return default


def read_text(path):
    try:
        with open(path, "rb") as f:
            return f.read().decode("utf-8", "replace").replace("\r", "").replace("\0", "")
    except OSError:
        return ""


def records(results_text, console_text):
    """JSON lines from the results channel, falling back to the console copy."""
    lines = results_text.splitlines()
    if not any(l.strip() for l in lines):
        lines = [l.split(MARK, 1)[1] for l in console_text.splitlines() if MARK in l]
    out = []
    for l in lines:
        l = l.strip()
        if not l.startswith("{"):
            continue
        try:
            out.append(json.loads(l))
        except ValueError:
            pass
    return out


def summarize(results):
    s = {k: 0 for k in SUMMARY_KEYS}
    for r in results:
        st = r.get("status")
        s[st if st in s else "error"] += 1
    return s


def cmd_parse(a):
    console = read_text(os.path.join(a.outdir, "console.log"))
    recs = records(read_text(os.path.join(a.outdir, "results.log")), console)
    kinfo = load(a.kinfo, {})
    fetch = load(a.fetch, {})
    commit = ""
    if getattr(a, "commit", ""):
        commit = a.commit
    elif os.path.exists(a.commit_file):
        commit = open(a.commit_file).read().strip()

    meta, results = {}, []
    for r in recs:
        if "meta" in r and isinstance(r["meta"], dict):
            m = dict(r["meta"])
            phase = m.pop("phase", None)
            if phase:
                meta.setdefault("phases", []).append(phase)
            meta.update(m)
        elif "test" in r:
            # Normalize: payloads may omit an empty detail (ublk-suite does).
            results.append({"test": str(r["test"]), "status": str(r.get("status", "error")),
                            "duration_s": round(float(r.get("duration_s") or 0), 2),
                            "detail": str(r.get("detail") or "")})

    booted = "boot" in meta.get("phases", [])
    finished = "done" in meta.get("phases", [])
    timed_out = a.rc in (124, 137) and not finished
    if a.hung_poweroff:
        results.append({"test": "poweroff", "status": "fail", "duration_s": 45.0,
                        "detail": "payload finished but the guest did not power off within 45s "
                                  "(kernel shutdown blocked, typically by a wedged ublk device); killed by the host"})

    # The test in flight when the guest died: its RUN line has no result.
    if booted and not finished:
        ran = re.findall(r"^=== RUN (\S+)", console, re.M)
        seen = {r["test"] for r in results}
        inflight = next((t for t in reversed(ran) if t not in seen), None)
        if inflight == "suite":
            # ublk-suite reports per test; name the first one that never did.
            inflight = next((t for t in meta.get("suite_tests", "").split() if t not in seen), inflight)
        why = f"guest killed after {a.wall}s wall clock" if timed_out else "guest died (qemu rc=%d)" % a.rc
        results.append({"test": inflight or "payload", "status": "timeout" if timed_out else "error",
                        "duration_s": 0.0, "detail": why})

    klog = console
    if a.oops_in_dmesg_only:
        m = re.search(r"@@GOUBLK-DMESG-BEGIN@@\n(.*?)@@GOUBLK-DMESG-END@@", console, re.S)
        klog = m.group(1) if m else ""
    all_oops = [l.strip() for l in klog.splitlines() if OOPS_RE.search(l) and MARK not in l]
    known, oops_lines = {}, []
    for l in all_oops:
        bug = next((b for b in KNOWN_KERNEL_BUGS if b[1].search(l) and b[2].search(klog)), None)
        if bug:
            known.setdefault(bug[0], []).append(l)
        else:
            oops_lines.append(l)
    warns = sorted({m.group(1) for m in WARN_RE.finditer(klog)})
    oops = bool(oops_lines)
    if oops:
        idx = klog.find(oops_lines[0])
        excerpt = klog[idx:idx + 1200]
        results.append({"test": "kernel-log", "status": "fail", "duration_s": 0.0,
                        "detail": f"{len(oops_lines)} oops/hang line(s); first: {excerpt}"})
    for name, lines in known.items():
        note = next(b[3] for b in KNOWN_KERNEL_BUGS if b[0] == name)
        results.append({"test": f"kernel-log/{name}", "status": "skip", "duration_s": 0.0,
                        "detail": f"{note}; {len(lines)} line(s), first: {lines[0]}"})

    # A guest OOM kill is the harness's fault (MEM too small for the payload),
    # and whatever test it hit failed for that reason: say so explicitly.
    ooms = re.findall(r"Out of memory: Killed process \d+ \((\S+)\)", klog)
    if ooms:
        results.append({"test": "guest-oom", "status": "error", "duration_s": 0.0,
                        "detail": f"the guest OOM-killed {', '.join(sorted(set(ooms)))}; a failure in the test "
                                  f"that was running is a harness memory limit, not a product result"})

    probe = meta.get("probe") or {}
    kinfo_ublk = kinfo.get("ublk_drv")
    if not kinfo:
        # Full-distro runs have no extracted kernel; trust what the guest saw.
        kinfo_ublk = meta.get("ublk_drv") if meta.get("ublk_drv") in ("module", "builtin") else False
    has_ublk = bool(kinfo_ublk) or meta.get("ublk_control") is True
    if booted and kinfo_ublk and meta.get("ublk_control") is False:
        results.insert(0, {"test": "modprobe", "status": "fail", "duration_s": 0.0,
                           "detail": f"ublk_drv is shipped ({kinfo_ublk}) but /dev/ublk-control did not appear: "
                                     f"rc={meta.get('modprobe_rc')} {meta.get('modprobe_out', '')}"})

    summary = summarize(results)
    if not booted:
        status = "boot-failed"
        tail = "\n".join(console.strip().splitlines()[-15:])
        results = [{"test": "boot", "status": "timeout" if timed_out else "error", "duration_s": float(a.wall),
                    "detail": ("timed out before /init reported; " if timed_out else "") + tail[-1500:]}]
        summary = summarize(results)
    elif timed_out:
        status = "timeout"
    elif not has_ublk:
        status = "no-ublk"
    elif summary["fail"] or summary["error"] or oops or not finished:
        status = "fail"
    else:
        status = "pass"

    run = {
        "id": a.id,
        "family": fetch.get("family", ""),
        "distro": fetch.get("distro", "") + (f" [{a.append}]" if a.append else ""),
        "kernel": meta.get("uname") or kinfo.get("kver", ""),
        "upstream": fetch.get("upstream") or ".".join(re.findall(r"\d+", meta.get("uname", ""))[:2]),
        "arch": meta.get("machine", "x86_64"),
        "boot": a.boot,
        "accel": a.accel,
        "date": getattr(a, "date", "") or now(),
        "ublk_drv": has_ublk,
        # Only a hex bitmask: the docs site decodes this field numerically.
        "features": probe.get("features", "") if str(probe.get("features", "")).startswith("0x") else "",
        "oops": oops,
        "status": status,
        "results": results,
        "summary": summary,
        # Everything below is detail for humans; aggregate drops it from matrix.json.
        "detail": {
            "go_ublk_commit": commit,
            "extra_append": a.append,
            "cmdline": getattr(a, "cmdline", ""),
            "wall_s": a.wall,
            "timeout_s": a.timeout,
            "qemu_rc": a.rc,
            "boot_s": meta.get("boot_s"),
            "guest_uptime_s": meta.get("uptime_s"),
            "ublk_drv_kind": kinfo_ublk,
            "srcversion": meta.get("srcversion") or probe.get("srcversion", ""),
            "feature_names": probe.get("names", []),
            "probe_opcode": probe.get("opcode", ""),
            "config": kinfo.get("config", {}),
            "warnings": warns,
            "oops_lines": oops_lines[:20],
            "known_kernel_bugs": {k: v[:4] for k, v in known.items()},
            "version": fetch.get("version", ""),
        },
    }
    with open(os.path.join(a.outdir, "run.json"), "w") as f:
        json.dump(run, f, indent=1)


def cmd_reparse(a):
    """Re-derive run.json from the saved logs, e.g. after a classifier change."""
    kernels = os.path.join(os.environ.get("MATRIX_HOME", os.path.expanduser("~/goublk-matrix")), "cache", "kernels")
    for d in a.rundirs:
        for p in sorted(glob.glob(os.path.join(d, "*", "run.json"))):
            old = load(p)
            if not old:
                continue
            det = old.get("detail", {})
            outdir = os.path.dirname(p)
            base = re.split(r"[~+]", old["id"])[0]
            kdir = os.path.join(kernels, base)
            fetch = os.path.join(kdir, "fetch.json")
            if not os.path.exists(fetch):
                fetch = os.path.join(outdir, "fetch.json")
            ns = argparse.Namespace(
                outdir=outdir, id=old["id"], rc=det.get("qemu_rc", 0), wall=det.get("wall_s", 0),
                accel=old.get("accel", "tcg"), timeout=det.get("timeout_s") or 0,
                kinfo=os.path.join(kdir, "kinfo.json"), fetch=fetch, commit_file="",
                commit=det.get("go_ublk_commit", ""), date=old.get("date", ""), boot=old.get("boot", "initramfs"),
                append=det.get("extra_append", ""), cmdline=det.get("cmdline", ""),
                oops_in_dmesg_only=old.get("accel") == "native",
                hung_poweroff=any(x["test"] == "poweroff" for x in old.get("results", [])))
            cmd_parse(ns)
            print(f"reparsed {old['id']}")


SCHEMA_KEYS = ("id", "family", "distro", "kernel", "upstream", "arch", "boot", "accel", "date",
               "ublk_drv", "features", "oops", "status", "results", "summary")


def cmd_aggregate(a):
    runs = {}
    commit = ""
    for d in a.rundirs:
        for p in sorted(glob.glob(os.path.join(d, "*", "run.json"))):
            r = load(p)
            if r:
                runs[r["id"]] = r
                commit = r.get("detail", {}).get("go_ublk_commit") or commit
    manifest = load(a.manifest, []) if a.manifest else []
    for m in manifest:
        if m.get("status") != "ok" and m["id"] not in runs:
            runs[m["id"]] = {
                "id": m["id"], "family": m.get("family", ""), "distro": m.get("distro", ""),
                "kernel": m.get("version", ""), "upstream": m.get("upstream", ""), "arch": "x86_64",
                "boot": "initramfs", "accel": "", "date": now(), "ublk_drv": False, "features": "",
                "oops": False, "status": "fetch-failed",
                "results": [{"test": "fetch", "status": "error", "duration_s": 0.0,
                             "detail": (m.get("error") or "")[:1500]}],
                "summary": {"pass": 0, "fail": 0, "skip": 0, "error": 1},
                "detail": {},
            }

    order = {"mainline": 0}
    ordered = sorted(runs.values(), key=lambda r: (order.get(r["family"], 1), r["family"],
                                                     verkey(r.get("upstream", "")), r["id"]))
    os.makedirs(a.out, exist_ok=True)
    matrix = {"generated": now(), "go_ublk_commit": a.commit or commit,
              "runs": [{k: r.get(k) for k in SCHEMA_KEYS} for r in ordered]}
    with open(os.path.join(a.out, "matrix.json"), "w") as f:
        json.dump(matrix, f, indent=1)
    with open(os.path.join(a.out, "runs-detail.json"), "w") as f:
        json.dump([{"id": r["id"], **r.get("detail", {})} for r in ordered], f, indent=1)
    with open(os.path.join(a.out, "summary.md"), "w") as f:
        f.write(markdown(matrix, ordered))
    if manifest:
        keep = ("id", "family", "distro", "version", "upstream", "source", "status", "error", "notes")
        slim = []
        for m in manifest:
            k = m.get("kinfo", {})
            slim.append({**{x: m[x] for x in keep if x in m},
                         "packages": [{x: p.get(x, "") for x in ("file", "url", "sha256")} for p in m.get("packages", [])],
                         "kver": k.get("kver", ""), "ublk_drv": k.get("ublk_drv"), "config": k.get("config", {})})
        with open(os.path.join(a.out, "kernels.json"), "w") as f:
            json.dump(slim, f, indent=1)
    print(f"wrote {a.out}/matrix.json ({len(ordered)} runs)")


def verkey(v):
    try:
        return tuple(int(x) for x in v.split("."))
    except ValueError:
        return (0,)


def markdown(matrix, runs):
    counts = {}
    for r in runs:
        counts[r["status"]] = counts.get(r["status"], 0) + 1
    commits = {}
    for r in runs:
        c = (r.get("detail", {}).get("go_ublk_commit") or "?")[:12]
        commits[c] = commits.get(c, 0) + 1
    out = ["# go-ublk kernel matrix", "",
           f"Generated {matrix['generated']} against go-ublk `{matrix['go_ublk_commit'][:12]}`. "
           f"{len(runs)} kernels: " + ", ".join(f"{v} {k}" for k, v in sorted(counts.items())) + ".", ""]
    if len(commits) > 1:
        out += ["Rows by the commit they ran: " + ", ".join(f"`{c}` {n}" for c, n in sorted(commits.items())) + ".", ""]
    out += [
           "| Kernel id | Distro | uname -r | Base | ublk_drv | Features | Status | P/F/S/E | Oops | Wall | Failing tests |",
           "|---|---|---|---|---|---|---|---|---|---|---|"]
    for r in runs:
        s = r["summary"]
        d = r.get("detail", {})
        bad = [x["test"] for x in r["results"] if x["status"] in ("fail", "error", "timeout")]
        wall = f"{d['wall_s']}s" if d.get("wall_s") is not None else ""
        out.append("| {} | {} | {} | {} | {} | {} | **{}** | {}/{}/{}/{} | {} | {} | {} |".format(
            r["id"], r["distro"], r["kernel"], r["upstream"], "yes" if r["ublk_drv"] else "no",
            r["features"] or "-", r["status"], s["pass"], s["fail"], s["skip"], s["error"],
            "YES" if r["oops"] else "", wall, ", ".join(bad[:6]) + (" ..." if len(bad) > 6 else "")))
    out.append("")
    notable = [r for r in runs if r["status"] not in ("pass",)]
    if notable:
        out += ["## Details for runs that did not pass", ""]
        for r in notable:
            out.append(f"### {r['id']} ({r['kernel']}): {r['status']}")
            for x in r["results"]:
                if x["status"] not in ("fail", "error", "timeout"):
                    continue
                d = x["detail"]
                if "\n" in d:  # kernel log excerpts keep their lines
                    out += [f"- `{x['test']}` {x['status']}:", "  ```"]
                    out += ["  " + l for l in d[:2000].splitlines()]
                    out.append("  ```")
                else:
                    out.append(f"- `{x['test']}` {x['status']}: {d[:600]}")
            out.append("")
    return "\n".join(out) + "\n"


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("parse")
    p.add_argument("outdir")
    p.add_argument("--id", required=True)
    p.add_argument("--rc", type=int, required=True)
    p.add_argument("--wall", type=int, required=True)
    p.add_argument("--accel", default="tcg")
    p.add_argument("--boot", default="initramfs")
    p.add_argument("--append", default="", help="extra kernel command line this run used")
    p.add_argument("--cmdline", default="", help="the whole kernel command line, for the record")
    p.add_argument("--hung-poweroff", action="store_true", help="the host killed a finished guest stuck in power-off")
    p.add_argument("--oops-in-dmesg-only", action="store_true",
                   help="scan only the dmesg block (native runs, where the console is not the kernel log)")
    p.add_argument("--timeout", type=int, default=0)
    p.add_argument("--kinfo", default="")
    p.add_argument("--fetch", default="")
    p.add_argument("--commit-file", default="")
    rp = sub.add_parser("reparse")
    rp.add_argument("rundirs", nargs="+")
    g = sub.add_parser("aggregate")
    g.add_argument("rundirs", nargs="+")
    g.add_argument("--out", required=True)
    g.add_argument("--manifest", default="")
    g.add_argument("--commit", default="", help="go_ublk_commit to record (default: from the runs)")
    a = ap.parse_args()
    {"parse": cmd_parse, "aggregate": cmd_aggregate, "reparse": cmd_reparse}[a.cmd](a)


if __name__ == "__main__":
    sys.exit(main())
