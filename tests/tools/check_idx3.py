#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Compara el ampr_emu.index que escribe test_idx3 (C) con el de tools/ampr_ref.py (Python).

Uso: check_idx3.py <carpeta con basic.index y basic.tsv>
"""
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REF = os.path.join(HERE, "..", "..", "tools", "ampr_ref.py")


def main():
    d = sys.argv[1]
    out = os.path.join(d, "basic.ref.index")
    subprocess.run([sys.executable, "-I", REF, "idx3-build", os.path.join(d, "basic.tsv"), out], check=True)
    a = open(os.path.join(d, "basic.index"), "rb").read()
    b = open(out, "rb").read()
    if a != b:
        first = next((i for i in range(min(len(a), len(b))) if a[i] != b[i]), min(len(a), len(b)))
        print("check_idx3: DISTINTOS (C %d bytes, Python %d bytes, primera diferencia en %d)" % (len(a), len(b), first))
        return 1
    print("check_idx3: idénticos (%d bytes)" % len(a))
    return 0


if __name__ == "__main__":
    sys.exit(main())
