#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Pruebas de la línea de comandos de maxpack.

Uso: test_cli.py <maxpack> <carpeta de fixtures> <carpeta temporal>
"""
import json
import math
import os
import re
import shutil
import subprocess
import sys

EXE, FX, TMP = sys.argv[1], sys.argv[2], sys.argv[3]
BASIC = os.path.join(FX, "basic.exfat")
failures = []


def run(*args):
    p = subprocess.run([EXE, *args], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return p.returncode, p.stdout.decode("utf-8", "replace"), p.stderr.decode("utf-8", "replace")


def check(name, cond, detail=""):
    print(("ok    " if cond else "FALLO ") + name + ("" if cond else "  " + detail))
    if not cond:
        failures.append(name)


def manifest_count():
    with open(os.path.join(FX, "basic.manifest.tsv"), encoding="utf-8") as f:
        return sum(1 for _ in f)


def all_finite(v):
    if isinstance(v, dict):
        return all(all_finite(x) for x in v.values())
    if isinstance(v, list):
        return all(all_finite(x) for x in v)
    if isinstance(v, float):
        return math.isfinite(v)
    return True


os.makedirs(TMP, exist_ok=True)

rc, out, _ = run("--version")
check("version", rc == 0 and re.fullmatch(r"maxpack 0\.0\.1 \(zstd 1\.5\.6, zlib 1\.3\.1\)\s*", out) is not None, out)
rc, _, _ = run()
check("sin_argumentos_sale_2", rc == 2, str(rc))
rc, _, _ = run("analyze")
check("analyze_sin_origen_sale_2", rc == 2, str(rc))
rc, _, err = run("analyze", BASIC, "--bogus")
check("opcion_desconocida_sale_2", rc == 2, str(rc))
rc, _, _ = run("analyze", BASIC, "--sample", "0")
check("sample_0_sale_2", rc == 2, str(rc))
rc, _, _ = run("analyze", BASIC, "--threads")
check("threads_sin_valor_sale_2", rc == 2, str(rc))
rc, _, err = run("analyze", os.path.join(FX, "no_existe.exfat"))
check("no_existe_sale_1", rc == 1 and "error:" in err, err)
rc, _, err = run("analyze", os.path.join(FX, "truncated_head.exfat"))
check("truncada_sale_1", rc == 1 and "error:" in err, err)
rc, _, err = run("analyze", os.path.join(FX, "fatloop.exfat"))
check("fatloop_sale_1", rc == 1 and "FAT" in err, err)
rc, _, err = run("analyze", os.path.join(FX, "huge_len.exfat"))
check("tamano_imposible_sale_1", rc == 1 and "error:" in err, err)
rc, _, err = run("analyze", os.path.join(FX, "fake.pkg"))
check("pkg_sale_1", rc == 1 and "PKG" in err, err)

js = os.path.join(TMP, "basic.json")
if os.path.exists(js):
    os.remove(js)
rc, out, err = run("analyze", BASIC, "--full", "--threads", "2", "--json", js)
check("analyze_full_sale_0", rc == 0, err)
try:
    with open(js, encoding="utf-8") as f:
        rep = json.load(f)
    keys = {"v1-l5", "v1-l9", "v1-l19", "v1-l22", "1m-l19", "dict-l19", "zlib-64k"}
    check("json_7_configs", set(rep["configs"]) == keys, str(list(rep["configs"])))
    check("json_files_total", rep["files"]["total"] == manifest_count(), str(rep["files"]["total"]))
    check("json_small_files", rep["small_files"]["files"] == 204, str(rep["small_files"]))
    check("json_finitos", all_finite(rep))
    check("json_nombre_sin_ruta", rep["source"]["name"] == "basic.exfat", rep["source"]["name"])
    check("texto_tiene_tabla", "(Forge)" in out and "basic.exfat" in out, out[:300])
except Exception as ex:  # noqa: BLE001
    check("json_legible", False, repr(ex))

rc, out, _ = run("analyze", BASIC, "--list")
lines = out.splitlines()
check("list_lineas", rc == 0 and len(lines) == manifest_count(), str(len(lines)))
eboot = [l for l in lines if l.endswith("\teboot.bin")]
check("list_eboot_suelto", len(eboot) == 1 and eboot[0].split("\t")[1] == "suelto", str(eboot))

uni = os.path.join(TMP, "juego ñ 🎮.exfat")
shutil.copyfile(BASIC, uni)
rc, out, err = run("analyze", uni, "--list")
check("ruta_no_ascii", rc == 0 and len(out.splitlines()) == manifest_count(), err)

print("test_cli: %d fallos" % len(failures))
sys.exit(1 if failures else 0)
