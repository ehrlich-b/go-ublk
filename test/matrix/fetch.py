#!/usr/bin/env python3
"""Fetch kernel packages for the matrix into $MATRIX_HOME/cache/downloads/<id>/.

  fetch.py list                          ids this script knows how to fetch
  fetch.py mainline [--versions v6.18.5,...] [--from 6.0]
                                         Ubuntu mainline builds: latest point
                                         release of every series from --from
                                         up, plus the newest -rc
  fetch.py distro [ID|GLOB ...]          rows of distros.tsv, each fetched with
                                         the distro's own package manager
                                         inside its own docker image
  fetch.py manifest                      merge every fetch.json + kinfo.json
                                         into cache/kernels.json

Each kernel dir gets a fetch.json: id, family, distro, version, upstream,
source, packages [{file, url, sha256}], status (ok|failed) and error. A dir
whose fetch.json says ok is not fetched again; delete it to refetch.
"""
import argparse
import fcntl
import fnmatch
import hashlib
import json
import os
import re
import subprocess
import sys
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
HOME = os.environ.get("MATRIX_HOME", os.path.expanduser("~/goublk-matrix"))
CACHE = os.path.join(HOME, "cache")
DOWNLOADS = os.path.join(CACHE, "downloads")
KERNELS = os.path.join(CACHE, "kernels")
DOCKER = os.environ.get("DOCKER", "docker")
MAINLINE = "https://kernel.ubuntu.com/mainline/"
LP_API = "https://api.launchpad.net/devel/ubuntu/+archive/primary"
LP_FILES = "https://launchpad.net/ubuntu/+archive/primary/+files/"
UBUNTU_POOL = "http://archive.ubuntu.com/ubuntu/pool/main/l/"


def log(msg):
    print(f"[fetch] {msg}", file=sys.stderr, flush=True)


def get(url):
    req = urllib.request.Request(url, headers={"User-Agent": "go-ublk-matrix"})
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read().decode()


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def download(url, dest):
    tmp = dest + ".part"
    subprocess.run(["curl", "-fsSL", "--retry", "3", "-o", tmp, url], check=True)
    os.replace(tmp, dest)


def upstream_of(version):
    m = re.match(r"v?(\d+)\.(\d+)", version)
    return f"{m.group(1)}.{m.group(2)}" if m else ""


def done(kid):
    """Already fetched, or already extracted (CI caches only the extracted dir)."""
    if os.path.exists(os.path.join(KERNELS, kid, "kinfo.json")):
        return True
    p = os.path.join(DOWNLOADS, kid, "fetch.json")
    if os.path.exists(p):
        with open(p) as f:
            return json.load(f).get("status") == "ok"
    return False


def lock(kid):
    """Hold an exclusive per-kernel lock so parallel fetches never share a dir."""
    os.makedirs(DOWNLOADS, exist_ok=True)
    f = open(os.path.join(DOWNLOADS, f".{kid}.lock"), "w")
    try:
        fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        f.close()
        log(f"{kid}: being fetched by another process, skipping")
        return None
    return f


def reset_dir(kid):
    d = os.path.join(DOWNLOADS, kid)
    os.makedirs(d, exist_ok=True)
    for n in os.listdir(d):
        os.unlink(os.path.join(d, n))
    return d


def write_fetch(kid, rec):
    with open(os.path.join(DOWNLOADS, kid, "fetch.json"), "w") as f:
        json.dump(rec, f, indent=1)
    log(f"{kid}: {rec['status']} {rec.get('version', '')} {rec.get('error', '')}")


# --- Ubuntu mainline ---------------------------------------------------------

def vkey(v):
    """Sort key for mainline dir names like v6.18.5 and v7.3-rc2."""
    m = re.match(r"v(\d+)\.(\d+)(?:\.(\d+))?(?:-rc(\d+))?$", v)
    if not m:
        return None
    a, b, c, rc = m.groups()
    return (int(a), int(b), int(c or 0), int(rc) if rc else 1000)


def mainline_plan(start):
    idx = get(MAINLINE)
    names = sorted({n for n in re.findall(r'href="(v[0-9][^"/]*)/"', idx) if vkey(n)}, key=vkey)
    series = {}
    for n in names:
        k = vkey(n)
        if k[:2] < start:
            continue
        series.setdefault(k[:2], []).append(n)
    plan = []
    for s in sorted(series):
        rels = [n for n in series[s] if vkey(n)[3] == 1000]
        if rels:
            plan.append(("release", rels[::-1]))   # newest first, fallbacks after
        elif s == max(series):
            plan.append(("rc", series[s][::-1]))
    return plan


def mainline_one(version, fallbacks=()):
    kid = f"mainline-{version.lstrip('v')}"
    if done(kid):
        return kid
    notes = []
    for v in [version, *fallbacks][:4]:
        page = get(MAINLINE + v + "/")
        m = re.search(r"Test amd64/build (succeeded|failed)", page)
        debs = re.findall(r'href="(amd64/linux-(?:image-unsigned|modules)-[^"]*-generic_[^"]*_amd64\.deb)"', page)
        if not m or m.group(1) != "succeeded" or len(debs) < 2:
            notes.append(f"{v}: amd64 build {'failed' if m else 'missing'}")
            continue
        kid = f"mainline-{v.lstrip('v')}"
        if done(kid):
            return kid
        lk = lock(kid)
        if not lk:
            return kid
        d = reset_dir(kid)
        sums = {}
        try:
            for line in get(MAINLINE + v + "/CHECKSUMS").splitlines():
                parts = line.split()
                if len(parts) == 2 and len(parts[0]) == 64:
                    sums[parts[1]] = parts[0]
        except Exception as e:  # CHECKSUMS is advisory
            notes.append(f"no CHECKSUMS: {e}")
        rec = {"id": kid, "family": "mainline", "distro": "Ubuntu mainline build",
               "version": v.lstrip("v"), "upstream": upstream_of(v), "source": MAINLINE + v + "/",
               "packages": [], "status": "ok", "notes": notes}
        try:
            for rel in debs:
                dest = os.path.join(d, os.path.basename(rel))
                download(MAINLINE + v + "/" + rel, dest)
                got = sha256(dest)
                if rel in sums and sums[rel] != got:
                    raise RuntimeError(f"sha256 mismatch for {rel}")
                rec["packages"].append({"file": os.path.basename(rel), "url": MAINLINE + v + "/" + rel,
                                        "sha256": got, "verified": rel in sums})
        except Exception as e:
            rec.update(status="failed", error=str(e))
        write_fetch(kid, rec)
        return kid
    # Every candidate failed to build: record it under the requested id.
    kid = f"mainline-{version.lstrip('v')}"
    reset_dir(kid)
    write_fetch(kid, {"id": kid, "family": "mainline", "distro": "Ubuntu mainline build",
                      "version": version.lstrip("v"), "upstream": upstream_of(version),
                      "source": MAINLINE + version + "/", "packages": [], "status": "failed",
                      "error": "; ".join(notes)})
    return kid


def cmd_mainline(a):
    if a.versions:
        for v in a.versions.split(","):
            mainline_one(v if v.startswith("v") else "v" + v)
        return
    start = tuple(int(x) for x in a.from_.split("."))
    plan = mainline_plan(start)
    if a.latest:
        plan = plan[-a.latest:]
    for kind, cands in plan:
        mainline_one(cands[0], cands[1:])


# --- distro kernels ------------------------------------------------------------

APT = r'''
set -e
export DEBIAN_FRONTEND=noninteractive
cd /out
if [ -n "$SUITE" ]; then
  echo "deb http://deb.debian.org/debian $SUITE main" > /etc/apt/sources.list.d/extra.list
  T="-t $SUITE"
fi
if ! apt-get update -qq >/dev/null 2>&1; then
  # End-of-life Ubuntu releases move to old-releases.
  sed -i -E 's#https?://(archive|security|ports)\.ubuntu\.com#http://old-releases.ubuntu.com#' \
    /etc/apt/sources.list /etc/apt/sources.list.d/* 2>/dev/null || true
  apt-get update -qq
fi
if [ -n "$PKG" ]; then
  p=$PKG
  for i in 1 2 3 4 5; do
    case $p in linux-image-[0-9]*|linux-image-unsigned-[0-9]*) break ;; esac
    p=$(apt-cache $T depends "$p" | awk '$1 ~ /Depends:/ && $2 ~ /^linux-image-/ {print $2; exit}')
    [ -n "$p" ] || { echo "cannot resolve $PKG to a linux-image package" >&2; exit 3; }
  done
else
  p=$(apt-cache pkgnames linux-image- | grep -E "$RE" | sort -V | tail -1)
  [ -n "$p" ] || { echo "no package matches $RE" >&2; exit 3; }
fi
k=${p#linux-image-}; k=${k#unsigned-}
echo "KVER $k"
got_image=
for n in linux-image-$k linux-image-unsigned-$k linux-modules-$k linux-modules-extra-$k; do
  case $n in linux-image-*) [ -n "$got_image" ] && continue ;; esac
  apt-cache show "$n" >/dev/null 2>&1 || continue
  spec=$n; [ -n "$SUITE" ] && spec="$n/$SUITE"
  # A name can exist in the base suite but not in $SUITE: skip it, unless it
  # is the image.
  uris=$(apt-get download --print-uris "$spec" 2>/dev/null) || continue
  echo "$uris" | awk '{gsub("'"'"'","",$1); print "URL", $2, $1}'
  apt-get download -q "$spec" >/dev/null 2>&1 || { echo "MISSING $spec"; continue; }
  case $n in linux-image-*) got_image=1 ;; esac
done
[ -n "$got_image" ] || { echo "no image package for $k" >&2; exit 3; }
# Newer Debian splits the image into a meta package plus linux-binary-$k and a
# virtual linux-modules-$k: pull every dependency named after this kernel.
apt-cache $T depends --recurse --no-recommends --no-suggests --no-conflicts --no-breaks \
    --no-replaces --no-enhances "linux-image-$k" 2>/dev/null |
  sed 's/^ *|\{0,1\}\(Depends: \)\{0,1\}//; s/[<>]//g' | grep -F -- "$k" | grep -v '^linux-headers' |
  sort -u | while read -r n; do
    ls /out/"${n}"_*.deb >/dev/null 2>&1 && continue
    spec=$n; [ -n "$SUITE" ] && spec="$n/$SUITE"
    uris=$(apt-get download --print-uris "$spec" 2>/dev/null) || continue
    echo "$uris" | awk '{gsub("'"'"'","",$1); print "URL", $2, $1}'
    apt-get download -q "$spec" >/dev/null 2>&1 || true
  done
'''

DNF = r'''
set -e
cd /out
E=""; [ -n "$ENABLE" ] && E="--enablerepo=$ENABLE"
# dnf4 images (Rocky, Amazon) ship without the download/repoquery plugins.
dnf download --help >/dev/null 2>&1 || dnf -q -y install dnf-plugins-core >/dev/null 2>&1
first=${PKGS%%,*}
ver=$(dnf -q $E repoquery --latest-limit=1 --arch x86_64 --qf '%{version}-%{release}\n' "$first" 2>/dev/null | grep -v '^$' | tail -1)
[ -n "$ver" ] || { echo "no version for $first" >&2; exit 3; }
echo "KVER $ver"
for p in $(echo "$PKGS" | tr ',' ' '); do
  dnf -q $E download --arch x86_64 --url "$p-$ver" 2>/dev/null | grep -E '^(https?|ftp)://' | sed 's/^/URL - /' || true
  dnf -q $E download --arch x86_64 --destdir /out "$p-$ver" >/dev/null 2>&1 || echo "MISSING $p-$ver"
done
'''

ZYPPER = r'''
set -e
zypper -q --non-interactive --gpg-auto-import-keys refresh >/dev/null
for p in $(echo "$PKGS" | tr ',' ' '); do
  zypper -q --non-interactive download "$p" >/dev/null 2>&1 || echo "MISSING $p"
done
# Newer openSUSE images have no find(1); zypper keeps packages at <repo>/<arch>/.
for f in /var/cache/zypp/packages/*/*/*.rpm; do [ -e "$f" ] && cp "$f" /out/; done
ls /out/*.rpm >/dev/null
'''

PACMAN = r'''
set -e
pacman-key --init >/dev/null 2>&1
pacman-key --populate archlinux >/dev/null 2>&1
pacman -Sy --noconfirm >/dev/null
for p in $(echo "$PKGS" | tr ',' ' '); do
  pacman -Sddp --noconfirm "$p" | sed 's/^/URL - /'
  pacman -Sddw --noconfirm --cachedir /out "$p" >/dev/null
done
'''

SCRIPTS = {"apt": APT, "dnf": DNF, "zypper": ZYPPER, "pacman": PACMAN}


def distro_rows():
    rows = []
    with open(os.path.join(HERE, "distros.tsv")) as f:
        for line in f:
            if not line.strip() or line.startswith("#"):
                continue
            kid, family, distro, image, method, spec = line.rstrip("\n").split("\t")
            opts = dict(kv.split("=", 1) for kv in spec.split(";") if kv)
            rows.append(dict(id=kid, family=family, distro=distro, image=image, method=method, opts=opts))
    return rows


def launchpad(row, d):
    kver = row["opts"]["kver"]
    # The same ABI is often built for several series (native and HWE); pin it.
    arch = f"/{row['opts']['series']}/amd64"
    files = []
    for name in (f"linux-image-unsigned-{kver}", f"linux-modules-{kver}", f"linux-modules-extra-{kver}"):
        q = (f"{LP_API}?ws.op=getPublishedBinaries&binary_name={name}&exact_match=true"
             f"&ordered=true&ws.size=75")
        entries = [e for e in json.loads(get(q))["entries"]
                   if e["distro_arch_series_link"].endswith(arch)]
        if not entries:
            if "extra" in name:
                continue
            raise RuntimeError(f"{name} not published on Launchpad")
        ver = entries[0]["binary_package_version"]
        src = entries[0]["source_package_name"]
        fn = f"{name}_{ver}_amd64.deb"
        # The archive pool is much faster than the librarian while the build is
        # still published; the librarian keeps it after it is superseded.
        for url in (f"{UBUNTU_POOL}{src[0]}/{src}/{fn}", LP_FILES + fn):
            try:
                download(url, os.path.join(d, fn))
                files.append((fn, url))
                break
            except subprocess.CalledProcessError:
                continue
        else:
            raise RuntimeError(f"cannot download {fn}")
    return kver, files


def distro_one(row):
    kid = row["id"]
    if done(kid):
        return
    lk = lock(kid)
    if not lk:
        return
    d = reset_dir(kid)
    rec = {"id": kid, "family": row["family"], "distro": row["distro"], "image": row["image"],
           "method": row["method"], "packages": [], "status": "ok"}
    try:
        if row["method"] == "launchpad":
            ver, files = launchpad(row, d)
            urls = {fn: u for fn, u in files}
            rec["source"] = "launchpad.net (librarian)"
        else:
            env = []
            for k, v in (("PKG", "pkg"), ("RE", "re"), ("SUITE", "suite"), ("PKGS", "pkgs"), ("ENABLE", "enable")):
                env += ["-e", f"{k}={row['opts'].get(v, '')}"]
            p = subprocess.run(["timeout", "1800", DOCKER, "run", "--rm", "-v", f"{d}:/out", *env,
                                # POSIX sh: some images (openSUSE) have no bash.
                                row["image"], "sh", "-c", SCRIPTS[row["method"]]],
                               capture_output=True, text=True)
            out = p.stdout
            if p.returncode != 0:
                raise RuntimeError(f"rc={p.returncode}: {(p.stderr or out).strip()[-600:]}")
            ver = next((l.split()[1] for l in out.splitlines() if l.startswith("KVER ")), "")
            urls = {}
            for l in out.splitlines():
                if l.startswith("URL "):
                    parts = l.split()
                    url = parts[-1]
                    urls[parts[1] if parts[1] != "-" else os.path.basename(url)] = url
            missing = [l.split()[1] for l in out.splitlines() if l.startswith("MISSING ")]
            if missing:
                rec["notes"] = ["not in repo: " + " ".join(missing)]
            rec["source"] = f"{row['method']} in {row['image']}"
        pkgs = sorted(n for n in os.listdir(d) if n.endswith((".deb", ".rpm", ".pkg.tar.zst", ".pkg.tar.xz")))
        if not pkgs:
            raise RuntimeError("no packages downloaded")
        rec["version"] = ver
        rec["upstream"] = upstream_of(ver)
        rec["packages"] = [{"file": n, "url": urls.get(n, ""), "sha256": sha256(os.path.join(d, n))} for n in pkgs]
    except Exception as e:
        rec.update(status="failed", error=str(e))
    write_fetch(kid, rec)


def cmd_distro(a):
    rows = distro_rows()
    pats = [p for arg in a.ids for p in arg.split()] or ["*"]
    for row in rows:
        if any(fnmatch.fnmatch(row["id"], p) for p in pats):
            distro_one(row)


def cmd_list(a):
    for kind, cands in mainline_plan(tuple(int(x) for x in a.from_.split("."))):
        print(f"mainline-{cands[0].lstrip('v')}")
    for row in distro_rows():
        print(row["id"])


def cmd_manifest(a):
    out = []
    ids = set()
    for d in (DOWNLOADS, KERNELS):
        if os.path.isdir(d):
            ids.update(os.listdir(d))
    for kid in sorted(ids):
        # extract.sh keeps a copy next to the kernel, which outlives the download.
        p = os.path.join(KERNELS, kid, "fetch.json")
        if not os.path.exists(p):
            p = os.path.join(DOWNLOADS, kid, "fetch.json")
        if not os.path.exists(p):
            continue
        with open(p) as f:
            rec = json.load(f)
        k = os.path.join(KERNELS, kid, "kinfo.json")
        if os.path.exists(k):
            with open(k) as f:
                rec["kinfo"] = json.load(f)
            # zypper/pacman fetches do not report a version; the module dir does.
            if not rec.get("version"):
                rec["version"] = rec["kinfo"].get("kver", "")
                rec["upstream"] = upstream_of(rec["version"])
        x = os.path.join(KERNELS, kid, "extract-error.json")
        if rec.get("status") == "ok" and not os.path.exists(k) and os.path.exists(x):
            with open(x) as f:
                rec.update(status="failed", error=json.load(f).get("error", "extract failed"))
        out.append(rec)
    dest = os.path.join(CACHE, "kernels.json")
    with open(dest, "w") as f:
        json.dump(out, f, indent=1)
    log(f"wrote {dest}: {len(out)} kernels")


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("list")
    s.add_argument("--from", dest="from_", default="6.0")
    s = sub.add_parser("mainline")
    s.add_argument("--versions")
    s.add_argument("--latest", type=int, default=0, help="only the newest N series")
    s.add_argument("--from", dest="from_", default="6.0")
    s = sub.add_parser("distro")
    s.add_argument("ids", nargs="*")
    sub.add_parser("manifest")
    a = ap.parse_args()
    os.makedirs(DOWNLOADS, exist_ok=True)
    {"list": cmd_list, "mainline": cmd_mainline, "distro": cmd_distro, "manifest": cmd_manifest}[a.cmd](a)


if __name__ == "__main__":
    main()
