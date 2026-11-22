#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Contrasta el lector C (mp_cat) con el lector de referencia en una imagen real.

Uso: compare_ranges.py <mp_cat> <imagen.exfat>

Para cada archivo compara el SHA-256 del primer MiB, del último MiB y, si pasa de 4 GiB,
de los 2 MiB centrados en 2^32. Solo lee.
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
    print("%s: %d archivos, %d rangos, %d distintos"
          % (os.path.basename(img), len(files), checked, bad))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
