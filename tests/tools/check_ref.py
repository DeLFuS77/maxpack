#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Checks that tools/exfat_ref.py reads basic.exfat as its manifest says.

Usage: check_ref.py <fixtures folder>
"""
import os
import subprocess
import sys
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
REF = os.path.join(HERE, "..", "..", "tools", "exfat_ref.py")


def run(*args):
    return subprocess.run([sys.executable, "-I", REF, *args], check=True,
                          stdout=subprocess.PIPE).stdout


def main():
    fx = sys.argv[1]
    img = os.path.join(fx, "basic.exfat")
    manifest = []
    with open(os.path.join(fx, "basic.manifest.tsv"), encoding="utf-8") as f:
        for line in f:
            size, crc, _mtime, path = line.rstrip("\n").split("\t", 3)
            manifest.append((int(size), crc, path))
    listed = [l.split("\t", 1) for l in run("ls", img).decode("utf-8").splitlines()]
    expected = [(str(s), p) for s, _, p in manifest]
    errors = 0
    if [tuple(x) for x in listed] != expected:
        print("ls does not match the manifest")
        errors += 1
    for size, crc, path in manifest:
        data = run("cat", img, path)
        if len(data) != size or "%08x" % zlib.crc32(data) != crc:
            print("different content: %s" % path)
            errors += 1
    tail = run("cat", img, "data/vdl.bin", "5000", "15000")
    if tail != bytes(15000):
        print("data/vdl.bin does not return zeros beyond VDL")
        errors += 1
    print("check_ref: %d files, %d errors" % (len(manifest), errors))
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
