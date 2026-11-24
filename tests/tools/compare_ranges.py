#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Compares the C reader (mp_cat) with the reference reader on a real image.

Usage: compare_ranges.py <mp_cat> <image.exfat>

For each file it compares the SHA-256 of the first MiB, the last MiB and, if it is over 4 GiB,
the 2 MiB centered on 2^32. Read-only.
"""
import hashlib
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REF = os.path.join(HERE, "..", "..", "tools", "exfat_ref.py")
MiB = 1 << 20


def sha(cmd):
    p = subprocess.run(cmd, stdout=subprocess.PIPE, check=True)
    return hashlib.sha256(p.stdout).hexdigest(), len(p.stdout)


def main():
    cat, img = sys.argv[1], sys.argv[2]
    ls = subprocess.run([sys.executable, "-I", REF, "ls", img], stdout=subprocess.PIPE, check=True)
    files = [l.split("\t", 1) for l in ls.stdout.decode("utf-8").splitlines()]
    checked = bad = 0
    for size, path in files:
        size = int(size)
        ranges = {(0, min(size, MiB)), (max(0, size - MiB), min(size, MiB))}
        if size > (4 << 30):
            ranges.add(((1 << 32) - MiB, 2 * MiB))
        for off, n in sorted(ranges):
            a = sha([cat, img, path, str(off), str(n)])
            b = sha([sys.executable, "-I", REF, "cat", img, path, str(off), str(n)])
            checked += 1
            if a != b:
                bad += 1
                print("DISTINTO: %s [%d, +%d)" % (path, off, n))
    print("%s: %d files, %d ranges, %d different"
          % (os.path.basename(img), len(files), checked, bad))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
