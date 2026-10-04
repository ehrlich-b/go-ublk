#!/usr/bin/env python3
"""Write an uncompressed newc cpio (the initramfs format) from a directory.

Every entry is owned by root, hard links are stored as independent files, and
device nodes can be added without creating them on disk, so it works under
rootless docker or as an unprivileged user.

  mkcpio.py OUT ROOT [--prefix DIR] [--node PATH:c|b:MAJOR:MINOR:MODE]...
"""
import argparse
import os
import stat
import sys

ino = 1


def header(name, mode, size, mtime=0, nlink=1, rmaj=0, rmin=0):
    global ino
    ino += 1
    nb = name.encode() + b"\0"
    fields = [ino, mode, 0, 0, nlink, mtime, size, 0, 0, rmaj, rmin, len(nb), 0]
    h = b"070701" + b"".join(b"%08X" % f for f in fields) + nb
    return h + b"\0" * (-len(h) % 4)


def pad(out, n):
    out.write(b"\0" * (-n % 4))


def add_file(out, name, path, st):
    out.write(header(name, st.st_mode, st.st_size, int(st.st_mtime)))
    with open(path, "rb") as f:
        while True:
            chunk = f.read(1 << 20)
            if not chunk:
                break
            out.write(chunk)
    pad(out, st.st_size)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("root")
    ap.add_argument("--prefix", default="")
    ap.add_argument("--node", action="append", default=[])
    a = ap.parse_args()

    prefix = a.prefix.strip("/")
    with open(a.out, "wb") as out:
        # Parent directories of the prefix, so archives can be concatenated.
        parts = prefix.split("/") if prefix else []
        for i in range(1, len(parts) + 1):
            out.write(header("/".join(parts[:i]), stat.S_IFDIR | 0o755, 0))
        for dirpath, dirnames, filenames in os.walk(a.root):
            dirnames.sort()
            rel = os.path.relpath(dirpath, a.root)
            for n in sorted(dirnames) + sorted(filenames):
                path = os.path.join(dirpath, n)
                name = os.path.normpath(os.path.join(prefix, rel, n))
                st = os.lstat(path)
                if stat.S_ISLNK(st.st_mode):
                    target = os.readlink(path).encode()
                    out.write(header(name, st.st_mode, len(target), int(st.st_mtime)))
                    out.write(target)
                    pad(out, len(target))
                elif stat.S_ISDIR(st.st_mode):
                    out.write(header(name, st.st_mode, 0, int(st.st_mtime), nlink=2))
                elif stat.S_ISREG(st.st_mode):
                    add_file(out, name, path, st)
        for spec in a.node:
            path, kind, maj, mnr, mode = spec.split(":")
            fmt = stat.S_IFCHR if kind == "c" else stat.S_IFBLK
            out.write(header(path.strip("/"), fmt | int(mode, 8), 0, rmaj=int(maj), rmin=int(mnr)))
        out.write(header("TRAILER!!!", 0, 0))
        out.write(b"\0" * (-out.tell() % 512))


if __name__ == "__main__":
    sys.exit(main())
