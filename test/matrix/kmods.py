#!/usr/bin/env python3
"""Turn an extracted kernel package tree into what a matrix guest boots.

  kmods.py STAGE OUT ID

STAGE holds the unpacked packages (any of /boot, /lib/modules, /usr/lib/modules).
OUT receives:
  vmlinuz       the kernel
  modules.cpio  /lib/modules/<kver> with only ublk_drv, the filesystems the
                payload mounts, and their dependency closure (incl. softdeps),
                decompressed and re-depmod'ed so any guest kmod can load them
  kinfo.json    kver, whether ublk_drv exists (module|builtin|false), the
                relevant CONFIG_ values, and which modules went in

Needs depmod (kmod) that can read the distro's compression, plus zstd/xz/gzip.
"""
import glob
import gzip
import json
import lzma
import os
import re
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
WANT = ["ublk_drv", "ext4", "xfs", "jbd2", "mbcache", "crc16", "crc32c_generic", "crc32c_intel",
        "crc32_generic", "crc32_pclmul", "libcrc32c", "crc64", "crc64_rocksoft", "crc64_rocksoft_generic",
        "crct10dif_generic", "crct10dif_pclmul", "crc_t10dif", "xxhash_generic"]
CONFIGS = ["CONFIG_BLK_DEV_UBLK", "CONFIG_BLKDEV_UBLK_LEGACY_OPCODES", "CONFIG_IO_URING",
           "CONFIG_EXT4_FS", "CONFIG_XFS_FS", "CONFIG_MODULE_SIG_FORCE", "CONFIG_HW_RANDOM_VIRTIO"]


def norm(name):
    return re.sub(r"\.ko(\.(zst|xz|gz))?$", "", os.path.basename(name)).replace("-", "_")


def find_tree(stage):
    """Return (kver, moddir) for the module tree that has a kernel next to it."""
    cands = []
    for base in ("lib/modules", "usr/lib/modules"):
        for d in glob.glob(os.path.join(stage, base, "*")):
            if os.path.isdir(os.path.join(d, "kernel")):
                cands.append((os.path.basename(d), d))
    if not cands:
        raise SystemExit("no lib/modules/<kver>/kernel in packages")
    for kver, d in cands:
        if find_vmlinuz(stage, kver, d):
            return kver, d
    return cands[0]


def find_vmlinuz(stage, kver, moddir):
    for p in (os.path.join(moddir, "vmlinuz"), os.path.join(stage, "boot", f"vmlinuz-{kver}"),
              os.path.join(stage, "usr/lib/modules", kver, "vmlinuz"), os.path.join(stage, "lib/modules", kver, "vmlinuz")):
        if os.path.isfile(p):
            return p
    hits = glob.glob(os.path.join(stage, "boot", "vmlinu*"))
    return hits[0] if len(hits) == 1 else None


def read_config(stage, kver, moddir):
    for p in (os.path.join(stage, "boot", f"config-{kver}"), os.path.join(moddir, "config")):
        if os.path.isfile(p):
            vals = {}
            with open(p) as f:
                for line in f:
                    m = re.match(r"(CONFIG_\w+)=(.*)", line)
                    if m and m.group(1) in CONFIGS:
                        vals[m.group(1)] = m.group(2)
                    m = re.match(r"# (CONFIG_\w+) is not set", line)
                    if m and m.group(1) in CONFIGS:
                        vals[m.group(1)] = "n"
            return vals
    return {}


def depmod(base, kver):
    subprocess.run(["depmod", "-b", base, kver], check=True)


def main():
    stage, out, kid = sys.argv[1:4]
    kver, moddir = find_tree(stage)
    vml = find_vmlinuz(stage, kver, moddir)
    if not vml:
        raise SystemExit(f"no vmlinuz for {kver}")
    os.makedirs(out, exist_ok=True)
    shutil.copyfile(vml, os.path.join(out, "vmlinuz"))

    # depmod wants <base>/lib/modules/<kver>.
    base = os.path.dirname(os.path.dirname(moddir))
    if base.endswith("/lib"):
        base = os.path.dirname(base)
    depmod(base, kver)

    deps = {}   # name -> (relpath, [dep relpaths])
    with open(os.path.join(moddir, "modules.dep")) as f:
        for line in f:
            if ":" not in line:
                continue
            mod, rest = line.split(":", 1)
            deps[norm(mod)] = (mod, rest.split())
    by_path = {v[0]: k for k, v in deps.items()}
    soft = {}
    sp = os.path.join(moddir, "modules.softdep")
    if os.path.exists(sp):
        with open(sp) as f:
            for line in f:
                w = line.split()
                if len(w) > 2 and w[0] == "softdep":
                    soft[norm(w[1])] = [norm(x) for x in w[2:] if not x.endswith(":")]
    builtin = set()
    bp = os.path.join(moddir, "modules.builtin")
    if os.path.exists(bp):
        with open(bp) as f:
            builtin = {norm(l.strip()) for l in f if l.strip()}

    keep, todo = set(), [w for w in WANT if w in deps]
    while todo:
        n = todo.pop()
        if n in keep or n not in deps:
            continue
        keep.add(n)
        todo += [by_path[p] for p in deps[n][1] if p in by_path]
        todo += soft.get(n, [])

    tree = os.path.join(out, "mods")
    if os.path.exists(tree):
        shutil.rmtree(tree)
    dst_root = os.path.join(tree, "lib", "modules", kver)
    os.makedirs(dst_root)
    for n in sorted(keep):
        rel = deps[n][0]
        src = os.path.join(moddir, rel)
        dst = os.path.join(dst_root, re.sub(r"\.(zst|xz|gz)$", "", rel))
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        if rel.endswith(".zst"):
            subprocess.run(["zstd", "-q", "-d", "-f", src, "-o", dst], check=True)
        elif rel.endswith(".xz"):
            with lzma.open(src) as i, open(dst, "wb") as o:
                shutil.copyfileobj(i, o)
        elif rel.endswith(".gz"):
            with gzip.open(src) as i, open(dst, "wb") as o:
                shutil.copyfileobj(i, o)
        else:
            shutil.copyfile(src, dst)
    for n in ("modules.builtin", "modules.builtin.modinfo", "modules.order"):
        if os.path.exists(os.path.join(moddir, n)):
            shutil.copyfile(os.path.join(moddir, n), os.path.join(dst_root, n))
    depmod(tree, kver)
    subprocess.run([sys.executable, os.path.join(HERE, "mkcpio.py"), os.path.join(out, "modules.cpio"), tree],
                   check=True)
    shutil.rmtree(tree)

    if "ublk_drv" in deps:
        ublk = "module"
    elif "ublk_drv" in builtin:
        ublk = "builtin"
    else:
        ublk = False
    ext = next((m.group(0) for m in [re.search(r"\.ko(\.\w+)?$", v[0]) for v in deps.values()] if m), "")
    info = {"id": kid, "kver": kver, "ublk_drv": ublk, "modules": sorted(keep),
            "module_ext": ext, "config": read_config(stage, kver, moddir),
            "ext4": "builtin" if "ext4" in builtin else ("module" if "ext4" in deps else False),
            "xfs": "builtin" if "xfs" in builtin else ("module" if "xfs" in deps else False)}
    with open(os.path.join(out, "kinfo.json"), "w") as f:
        json.dump(info, f, indent=1)
    print(f"{kid}: kver={kver} ublk_drv={ublk} modules={len(keep)}")


if __name__ == "__main__":
    main()
