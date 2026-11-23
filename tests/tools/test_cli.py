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
    t = rep.get("transport")
    check("json_transporte", isinstance(t, dict) and t["est_packed"] > 0 and t["p5z_estimate"] > t["est_packed"], str(t))
    check("texto_transporte", "Transporte" in out, out[-600:])
except Exception as ex:  # noqa: BLE001
    check("json_legible", False, repr(ex))

js2 = os.path.join(TMP, "sin_transporte.json")
rc, _, err = run("analyze", BASIC, "--no-transport", "--no-dedup", "--json", js2)
try:
    with open(js2, encoding="utf-8") as f:
        check("no_transport", rc == 0 and json.load(f)["transport"] is None, err[:200])
except Exception as ex:  # noqa: BLE001
    check("no_transport", False, repr(ex))

rc, out, _ = run("analyze", BASIC, "--list")
lines = out.splitlines()
check("list_lineas", rc == 0 and len(lines) == manifest_count(), str(len(lines)))
eboot = [l for l in lines if l.endswith("\teboot.bin")]
check("list_eboot_suelto", len(eboot) == 1 and eboot[0].split("\t")[1] == "suelto", str(eboot))

uni = os.path.join(TMP, "juego ñ 🎮.exfat")
shutil.copyfile(BASIC, uni)
rc, out, err = run("analyze", uni, "--list")
check("ruta_no_ascii", rc == 0 and len(out.splitlines()) == manifest_count(), err)

# --json nunca debe pisar el origen ni escribir dentro de la carpeta que se analiza.
import hashlib
victim = os.path.join(TMP, "victima.exfat")
shutil.copyfile(BASIC, victim)
before = hashlib.sha256(open(victim, "rb").read()).hexdigest()
rc, _, err = run("analyze", victim, "--json", victim)
after = hashlib.sha256(open(victim, "rb").read()).hexdigest()
check("json_no_pisa_la_imagen", rc == 2 and before == after, "rc=%d %s" % (rc, err[:200]))
dsrc = os.path.join(TMP, "carpeta_origen")
os.makedirs(os.path.join(dsrc, "sce_sys"), exist_ok=True)
with open(os.path.join(dsrc, "datos.bin"), "wb") as f:
    f.write(b"x" * 1000)
inside = os.path.join(dsrc, "sce_sys", "informe.json")
if os.path.exists(inside):
    os.remove(inside)
rc, _, err = run("analyze", dsrc, "--json", inside)
check("json_no_escribe_en_el_origen", rc == 2 and not os.path.exists(inside), "rc=%d %s" % (rc, err[:200]))
rc, _, err = run("analyze", BASIC, "--json", os.path.join(TMP, "no", "existe", "x.json"))
check("json_carpeta_inexistente_sale_2", rc == 2, "rc=%d %s" % (rc, err[:200]))

# pack a carpeta.
def rmtree(p):
    if os.path.isdir(p):
        shutil.rmtree(p)


out = os.path.join(TMP, "pack_out")
rmtree(out)
rc, so, err = run("pack", BASIC, "-o", out, "--threads", "2")
check("pack_sale_0", rc == 0 and os.path.isfile(os.path.join(out, "ampr_zstd.index")), err[-300:])
check("pack_avisa_runtime", "runtime" in (so + err), (so + err)[-300:])
rc, _, err = run("pack", BASIC, "-o", out)
check("pack_salida_no_vacia_sale_2", rc == 2, "rc=%d %s" % (rc, err[:200]))
rc, _, err = run("pack", dsrc, "-o", os.path.join(dsrc, "dentro"))
check("pack_dentro_del_origen_sale_2", rc == 2 and not os.path.exists(os.path.join(dsrc, "dentro")),
      "rc=%d %s" % (rc, err[:200]))
rc, _, err = run("pack", BASIC, "-o", os.path.join(TMP, "nivel23"), "--level", "23")
check("pack_nivel_23_sale_2", rc == 2, "rc=%d %s" % (rc, err[:200]))
rc, _, err = run("pack", BASIC)
check("pack_sin_salida_sale_2", rc == 2, "rc=%d" % rc)
p5z = os.path.join(TMP, "basic.p5z")
if os.path.exists(p5z):
    os.remove(p5z)
rc, so, err = run("pack", BASIC, "-o", p5z, "--p5z", "--threads", "2")
ok = rc == 0 and os.path.isfile(p5z) and open(p5z, "rb").read(8) == b"MAXPKP5Z"
check("pack_p5z_sale_0", ok and ".p5z" in so, (so + err)[-300:])
rc, _, err = run("pack", BASIC, "-o", p5z, "--p5z")
check("pack_p5z_existente_sale_2", rc == 2, "rc=%d %s" % (rc, err[:200]))

# info y unpack del .p5z.
rc, so, err = run("info", p5z)
check("info_sale_0", rc == 0 and "TEST00001" in so, (so + err)[-300:])
rc, _, err = run("unpack", p5z, "-o", os.path.join(TMP, "unp_x"))
check("unpack_sin_modo_sale_2", rc == 2, "rc=%d" % rc)
orig = os.path.join(TMP, "unp_original")
rmtree(orig)
rc, so, err = run("unpack", p5z, "--original", "-o", orig)
n_orig = sum(len(f) for _, _, f in os.walk(orig)) if os.path.isdir(orig) else 0
check("unpack_original_sale_0", rc == 0 and n_orig == manifest_count(), "rc=%d n=%d %s" % (rc, n_orig, err[-200:]))
rc, _, err = run("unpack", p5z, "--original", "-o", orig)
check("unpack_salida_no_vacia_sale_2", rc == 2, "rc=%d" % rc)

# verify.
rc, so, err = run("verify", p5z)
check("verify_p5z_sale_0", rc == 0, (so + err)[-300:])
rc, so, err = run("verify", out, "--against", BASIC)
check("verify_carpeta_against_sale_0", rc == 0, (so + err)[-300:])
bad = os.path.join(TMP, "danado.p5z")
data = bytearray(open(p5z, "rb").read())
data[len(data) // 2] ^= 0x55
open(bad, "wb").write(bytes(data))
rc, so, err = run("verify", bad)
check("verify_danado_sale_1", rc == 1 and ("FALLO" in err or "error" in err), (so + err)[-300:])

# ---- --progress json, --control stdin, --max-file-size y limpieza al fallar ----------------------
def json_lines(text):
    evs = []
    for l in text.splitlines():
        if l.strip():
            evs.append(json.loads(l))
    return evs


def last_result(evs):
    return evs[-1] if evs and evs[-1].get("event") == "result" else {}


def has_final_progress(evs):
    return any(e.get("event") == "progress" and e["total"] > 0 and e["done"] == e["total"] for e in evs)


pj = os.path.join(TMP, "pj.p5z")
if os.path.exists(pj):
    os.remove(pj)
rc, so, err = run("pack", BASIC, "-o", pj, "--p5z", "--threads", "2", "--progress", "json")
try:
    evs = json_lines(so)
    res = last_result(evs)
    check("progress_json_pack",
          rc == 0 and res.get("ok") is True and res.get("output_size") == os.path.getsize(pj)
          and has_final_progress(evs) and all(e["event"] in ("progress", "result") for e in evs)
          and sum(1 for e in evs if e["event"] == "result") == 1
          and {"original_size", "loose_bytes", "packed_bytes", "stored_bytes", "volumes",
               "runtime_without_zstd"} <= set(res),
          "rc=%d %s %s" % (rc, so[-300:], err[-200:]))
except Exception as ex:  # noqa: BLE001
    check("progress_json_pack", False, repr(ex) + so[-200:])

pjo = os.path.join(TMP, "pj_orig")
rmtree(pjo)
rc, so, err = run("unpack", pj, "--original", "-o", pjo, "--progress", "json")
try:
    evs = json_lines(so)
    res = last_result(evs)
    check("progress_json_unpack",
          rc == 0 and res.get("ok") is True and res.get("bytes", 0) > 0 and has_final_progress(evs),
          "rc=%d %s" % (rc, so[-300:]))
except Exception as ex:  # noqa: BLE001
    check("progress_json_unpack", False, repr(ex) + so[-200:])
rc, so, err = run("verify", pj, "--progress", "json")
try:
    evs = json_lines(so)
    res = last_result(evs)
    check("progress_json_verify",
          rc == 0 and res.get("ok") is True and res.get("files_bad") == 0 and res.get("files_ok", 0) > 0
          and has_final_progress(evs), "rc=%d %s" % (rc, so[-300:]))
except Exception as ex:  # noqa: BLE001
    check("progress_json_verify", False, repr(ex) + so[-200:])

# Entrada de varios MiB para que la cancelación caiga a mitad del trabajo.
BIG = os.path.join(TMP, "big_src")
rmtree(BIG)
os.makedirs(os.path.join(BIG, "data"))
os.makedirs(os.path.join(BIG, "sce_sys"))
for i in range(6):
    with open(os.path.join(BIG, "data", "f%d.bin" % i), "wb") as f:
        f.write(os.urandom(2 << 20))
with open(os.path.join(BIG, "sce_sys", "param.json"), "w") as f:
    f.write('{"titleId":"TEST00002"}')

cdst = os.path.join(TMP, "cancel_out")
rmtree(cdst)
p = subprocess.Popen([EXE, "pack", BIG, "-o", cdst, "--threads", "2", "--progress", "json", "--control", "stdin"],
                     stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
first = p.stdout.readline()
p.stdin.write(b"cancel\n")
p.stdin.flush()
rest = p.stdout.read()
errtxt = p.stderr.read().decode("utf-8", "replace")
p.wait()
try:
    evs = json_lines((first + rest).decode("utf-8", "replace"))
    res = last_result(evs)
    check("control_stdin_cancel",
          p.returncode == 130 and res.get("ok") is False and res.get("error") == "cancelled"
          and json.loads(first)["event"] == "progress" and not os.path.exists(cdst),
          "rc=%s %s %s" % (p.returncode, (first + rest)[-300:], errtxt[-200:]))
except Exception as ex:  # noqa: BLE001
    check("control_stdin_cancel", False, repr(ex))

# Cerrar stdin también cancela.
cdst2 = os.path.join(TMP, "cancel_out2")
rmtree(cdst2)
p = subprocess.Popen([EXE, "pack", BIG, "-o", cdst2, "--threads", "2", "--progress", "json", "--control", "stdin"],
                     stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
first = p.stdout.readline()
p.stdin.close()
p.stdout.read()
p.stderr.read()
p.wait()
check("control_stdin_eof_cancela", p.returncode == 130 and not os.path.exists(cdst2), str(p.returncode))

mx = os.path.join(TMP, "max.p5z")
if os.path.exists(mx):
    os.remove(mx)
rc, so, err = run("pack", BASIC, "-o", mx, "--p5z", "--max-file-size", "100000", "--progress", "json")
try:
    res = last_result(json_lines(so))
    check("max_file_size", rc == 1 and res.get("ok") is False and res.get("error") == "too_big"
          and not os.path.exists(mx), "rc=%d %s %s" % (rc, so[-200:], err[-200:]))
except Exception as ex:  # noqa: BLE001
    check("max_file_size", False, repr(ex))
rc, _, _ = run("pack", BASIC, "-o", os.path.join(TMP, "mx_folder"), "--max-file-size", "100000")
check("max_file_size_solo_p5z_sale_2", rc == 2, str(rc))

# Un byte cambiado en los datos: unpack falla a mitad y la carpeta de salida no queda.
dam = os.path.join(TMP, "dam.p5z")
shutil.copyfile(pj, dam)
data = bytearray(open(dam, "rb").read())
data[len(data) // 2] ^= 0x55
open(dam, "wb").write(bytes(data))
fc = os.path.join(TMP, "fail_clean")
rmtree(fc)
rc, so, err = run("unpack", dam, "--original", "-o", fc)
check("failure_cleans_folder", rc == 1 and not os.path.exists(fc), "rc=%d %s" % (rc, err[-200:]))
fc2 = os.path.join(TMP, "fail_clean_empty")
rmtree(fc2)
os.makedirs(fc2)
rc, so, err = run("unpack", dam, "--original", "-o", fc2, "--progress", "json")
res = last_result(json_lines(so)) if so.strip() else {}
check("failure_keeps_empty_folder", rc == 1 and os.path.isdir(fc2) and not os.listdir(fc2)
      and res.get("ok") is False, "rc=%d %s" % (rc, so[-200:]))

# Sin --progress la salida es la de siempre: stdout sin JSON y el texto en stderr.
rc, so, err = run("verify", pj)
check("human_output_unchanged", rc == 0 and so.startswith("verify:") and '"event"' not in so + err, so + err)

rc, so, _ = run("--help")
check("help_tiene_todas_las_ordenes", rc == 0 and all(w in so for w in ("analyze", "pack", "unpack", "verify", "info")), so[:300])

print("test_cli: %d fallos" % len(failures))
sys.exit(1 if failures else 0)
