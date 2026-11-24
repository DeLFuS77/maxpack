#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Prueba tools/version.sh: de una etiqueta vX.Y.Z sale X.Y.Z, también para los números de Windows.

  test_version.py <bash> <tools/version.sh> <CMakeLists.txt>
"""
import re
import subprocess
import sys

bash, script, cmake = sys.argv[1:4]


def run(kind, name, sha="0123456789abcdef"):
    out = subprocess.run([bash, script, kind, name, sha], check=True, capture_output=True, text=True).stdout
    return dict(line.split("=", 1) for line in out.splitlines())


CASES = [
    ("tag", "v1.2.3", "1.2.3", "1.2.3"),
    ("tag", "v1.0.0", "1.0.0", "1.0.0"),
    ("tag", "1.2.3", "1.2.3", "1.2.3"),
    ("tag", "v1.0.0-rc1", "1.0.0-rc1", "1.0.0"),
    ("tag", "v10.20.30", "10.20.30", "10.20.30"),
    ("branch", "publicacion", "dev-0123456", "0.0.0"),
    ("tag", "nightly", "nightly", "0.0.0"),
]
bad = 0
for kind, name, version, numbers in CASES:
    got = run(kind, name)
    if got != {"VERSION": version, "NUMBERS": numbers}:
        print(f"FALLA {kind} {name}: {got} (esperado {version} / {numbers})")
        bad += 1

# la versión de CMake y la del motor (mp_version.h) son la misma, y la que lleva una etiqueta de versión
with open(cmake, encoding="utf-8") as f:
    cm = re.search(r"project\(maxpack VERSION (\S+)", f.read()).group(1)
with open(cmake.replace("CMakeLists.txt", "src/core/mp_version.h"), encoding="utf-8") as f:
    hv = re.search(r'#define MP_VERSION "([^"]+)"', f.read()).group(1)
if cm != hv:
    print(f"FALLA: CMake dice {cm} y mp_version.h {hv}")
    bad += 1

print("ok" if not bad else f"{bad} fallos")
sys.exit(1 if bad else 0)
