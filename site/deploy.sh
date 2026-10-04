#!/usr/bin/env bash
# Ship the docs site to ublk.ehrlich.dev as a hashed static release.
# Same pattern as mandelbrot's scripts/deploy.sh. nginx and DNS are owned by
# ~/repos/infra; this script only swaps the /var/www/ublk symlink.
#
#   make site                          build site/public
#   bash site/deploy.sh package        write site/release/ublk-ID.tar.gz and print its path
#   bash site/deploy.sh ship ARCHIVE   upload, verify every file hash, switch the symlink
#   bash site/deploy.sh                package, then ship
#
# The release ID is the SHA-256 of the build manifest (one "hash  path" line per
# file, sorted), so identical output always gets the same ID and re-shipping it
# is a no-op on the server.
set -euo pipefail
cd "$(dirname "$0")"
target=root@104.131.94.68

package() {
  test -f public/index.html || { echo 'site/public is empty; run make site first' >&2; exit 1; }
  if [ "${ALLOW_SAMPLE:-}" != 1 ] && grep -l '"sample": *true' data/*.json; then
    echo 'The files above are sample data; drop in the generated files (or set ALLOW_SAMPLE=1)' >&2
    exit 1
  fi
  if find public -type l | grep -q .; then echo 'Release cannot contain symlinks' >&2; exit 1; fi
  rm -f public/build.json
  commit=$(git rev-parse HEAD 2>/dev/null || echo unknown)
  dirty=$(git status --porcelain -- . 2>/dev/null | grep -q . && echo true || echo false)
  hugo_version=$(hugo version 2>/dev/null | awk '{print $2}' || echo unknown)
  python3 - "$commit" "$dirty" "$hugo_version" <<'PY'
import hashlib, json, sys, datetime
from pathlib import Path
root = Path("public")
files = {}
for p in sorted(root.rglob("*")):
    if p.is_file():
        files[p.relative_to(root).as_posix()] = hashlib.sha256(p.read_bytes()).hexdigest()
lines = "".join(f"{h}  {n}\n" for n, h in sorted(files.items()))
release = hashlib.sha256(lines.encode()).hexdigest()
manifest = {
    "id": release,
    "commit": sys.argv[1],
    "dirty": sys.argv[2] == "true",
    "hugo": sys.argv[3],
    "builtAt": datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds"),
    "files": files,
}
(root / "build.json").write_text(json.dumps(manifest, indent=2) + "\n")
PY
  id=$(python3 -c 'import json; print(json.load(open("public/build.json"))["id"])')
  mkdir -p release
  archive=release/ublk-$id.tar.gz
  # COPYFILE_DISABLE keeps macOS tar from adding AppleDouble ._* entries.
  COPYFILE_DISABLE=1 tar -czf "$archive" -C public .
  echo "site/$archive"
}

ship() {
  archive=${1:?usage: bash site/deploy.sh ship site/release/ublk-ID.tar.gz}
  archive=${archive#site/}
  test -f "$archive"
  id=$(tar -xOzf "$archive" ./build.json | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
  [[ "$id" =~ ^[a-f0-9]{64}$ ]] || { echo 'Invalid release ID' >&2; exit 1; }
  archive_hash=$(shasum -a 256 "$archive" | cut -d' ' -f1)
  scp "$archive" "$target:/tmp/ublk-$id.tar.gz"
  ssh "$target" bash -s -- "$id" "$archive_hash" <<'REMOTE'
set -euo pipefail
id=$1
archive=/tmp/ublk-$id.tar.gz
printf '%s  %s\n' "$2" "$archive" | sha256sum -c -
destination=/var/www/ublk
if test -e "$destination" && ! test -L "$destination"; then
  echo 'Existing /var/www/ublk is a directory; preserve it before switching to releases.' >&2
  exit 1
fi
mkdir -p /var/www/ublk-releases
stage=$(mktemp -d /var/www/ublk-releases/.stage-XXXXXXXX)
trap 'rm -rf "$stage"; rm -f "$archive"' EXIT
tar -xzf "$archive" -C "$stage"
python3 - "$stage" "$id" <<'PY'
import hashlib, json, sys
from pathlib import Path
root = Path(sys.argv[1]); manifest = json.loads((root / "build.json").read_text())
assert manifest["id"] == sys.argv[2]
assert "index.html" in manifest["files"] and "404.html" in manifest["files"]
lines = "".join(f"{h}  {n}\n" for n, h in sorted(manifest["files"].items()))
assert hashlib.sha256(lines.encode()).hexdigest() == manifest["id"], "manifest does not hash to its ID"
on_disk = {p.relative_to(root).as_posix() for p in root.rglob("*") if p.is_file()} - {"build.json"}
assert on_disk == set(manifest["files"]), "archive and manifest list different files"
for name, expected in manifest["files"].items():
    path = root / name
    assert path.resolve().is_relative_to(root.resolve()) and path.is_file() and not path.is_symlink()
    assert hashlib.sha256(path.read_bytes()).hexdigest() == expected, name
print(f"Verified {len(manifest['files'])} files")
PY
chmod -R u=rwX,go=rX "$stage"
release=/var/www/ublk-releases/$id
if test -e "$release"; then
  cmp <(python3 -c 'import json,sys; print(json.dumps(json.load(open(sys.argv[1]))["files"], sort_keys=True))' "$stage/build.json") \
      <(python3 -c 'import json,sys; print(json.dumps(json.load(open(sys.argv[1]))["files"], sort_keys=True))' "$release/build.json")
else
  mv "$stage" "$release"
fi
previous=$(readlink "$destination" || true)
next=/var/www/.ublk-$id
test ! -e "$next" && test ! -L "$next"
ln -s "$release" "$next"
mv -Tf "$next" "$destination"
printf 'Published release: %s\nPrevious release: %s\n' "$id" "${previous:-none}"
REMOTE
}

case "${1:-all}" in
  package) package ;;
  ship) ship "${2:-}" ;;
  all) archive=$(package | tail -n1); ship "$archive" ;;
  *) echo "usage: bash site/deploy.sh [package | ship ARCHIVE]" >&2; exit 2 ;;
esac
