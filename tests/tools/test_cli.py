#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Command-line tests for maxpack.

Usage: test_cli.py <maxpack> <fixtures folder> <temporary folder>
"""
import json
import math
import os
import re
import shutil
import subprocess
import sys
import time

EXE, FX, TMP = sys.argv[1], sys.argv[2], sys.argv[3]
BASIC = os.path.join(FX, "basic.exfat")
failures = []


def run(*args):
    p = subprocess.run([EXE, *args], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return p.returncode, p.stdout.decode("utf-8", "replace"), p.stderr.decode("utf-8", "replace")


def check(name, cond, detail=""):
    print(("ok    " if cond else "FAIL  ") + name + ("" if cond else "  " + detail))
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
check("version", rc == 0 and re.fullmatch(r"maxpack 1[.]0[.]1 \(zstd 1\.5\.6, zlib 1\.3\.1\)\s*", out) is not None, out)
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
    check("texto_transporte", "Transport" in out, out[-600:])
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
check("list_eboot_suelto", len(eboot) == 1 and eboot[0].split("\t")[1] == "loose", str(eboot))

uni = os.path.join(TMP, "juego ñ 🎮.exfat")
shutil.copyfile(BASIC, uni)
rc, out, err = run("analyze", uni, "--list")
check("ruta_no_ascii", rc == 0 and len(out.splitlines()) == manifest_count(), err)

# --json must never overwrite the source or write inside the folder being analysed.
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

# pack to a folder.
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

# info and unpack of the .p5z.
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
check("verify_danado_sale_1", rc == 1 and ("FAIL" in err or "error" in err), (so + err)[-300:])

# ---- --progress json, --control stdin, --max-file-size and cleanup on failure ----------------------
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


def progress_monotonic(evs):
    """Within each phase, total stays the same and done never goes back nor over total."""
    last = {}
    for e in evs:
        if e.get("event") != "progress":
            continue
        done, total = last.get(e["phase"], (0, e["total"]))
        if e["total"] != total or e["done"] < done or e["done"] > e["total"]:
            return False
        last[e["phase"]] = (e["done"], e["total"])
    return bool(last)


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

# Input of several MiB so that cancellation lands in the middle of the work.
BIG = os.path.join(TMP, "big_src")
rmtree(BIG)
os.makedirs(os.path.join(BIG, "data"))
os.makedirs(os.path.join(BIG, "sce_sys"))
for i in range(6):
    with open(os.path.join(BIG, "data", "f%d.bin" % i), "wb") as f:
        f.write(os.urandom(2 << 20))
with open(os.path.join(BIG, "sce_sys", "param.json"), "w") as f:
    f.write('{"titleId":"TEST00002"}')

# The JSON progress of a pack with several threads: in order and up to the end of each phase.
for fmt in ("--p5z", "--folder"):
    mono = os.path.join(TMP, "mono" + (".p5z" if fmt == "--p5z" else ""))
    if os.path.isfile(mono):
        os.remove(mono)
    rmtree(mono)
    rc, so, err = run("pack", BIG, "-o", mono, fmt, "--threads", "4", "--level", "3", "--volume-limit", "1000000",
                      "--progress", "json")
    try:
        evs = json_lines(so)
        phases = [e["phase"] for e in evs if e["event"] == "progress"]
        check("progress_json_monotonic" + fmt.replace("--", "_"),
              rc == 0 and last_result(evs).get("ok") is True and progress_monotonic(evs)
              and has_final_progress(evs) and "packing" in phases
              and phases.index("packing") > max([i for i, x in enumerate(phases) if x == "copying"] or [-1]),
              "rc=%d %s %s" % (rc, so[-300:], err[-200:]))
    except Exception as ex:  # noqa: BLE001
        check("progress_json_monotonic" + fmt.replace("--", "_"), False, repr(ex) + so[-200:])

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

# If what was written cannot be deleted on cancel, it does not say nothing is left: it says what is left. An
# open file (Windows) or a folder without write permission (the others) prevents deleting it.
hold = os.path.join(TMP, "hold_out")
held_dir = os.path.join(hold, "sce_sys")
if os.path.isdir(held_dir):
    os.chmod(held_dir, 0o755)
rmtree(hold)
if os.name == "nt" or os.geteuid() != 0:
    p = subprocess.Popen([EXE, "pack", BIG, "-o", hold, "--threads", "1", "--progress", "json", "--control", "stdin"],
                         stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    first = p.stdout.readline()
    holder = None
    for _ in range(500):
        try:
            if os.name == "nt":
                holder = open(os.path.join(held_dir, "param.json"), "rb")   # without sharing deletion
            else:
                os.stat(os.path.join(held_dir, "param.json"))
                os.chmod(held_dir, 0o555)
                holder = True
            break
        except OSError:
            time.sleep(0.01)
    p.stdin.write(b"cancel\n")
    p.stdin.flush()
    rest = p.stdout.read()
    errtxt = p.stderr.read().decode("utf-8", "replace")
    p.wait()
    if os.name == "nt" and holder:
        holder.close()
    if os.path.isdir(held_dir):
        os.chmod(held_dir, 0o755)
    left = os.path.exists(os.path.join(held_dir, "param.json"))
    check("cancel_sin_poder_borrar_dice_que_queda",
          holder is not None and p.returncode == 130 and left and "nothing is left written" not in errtxt
          and "half-done" in errtxt and hold in errtxt,
          "rc=%s holder=%s left=%s %s" % (p.returncode, holder is not None, left, errtxt[-400:]))
    rmtree(hold)

# If whoever launched the engine goes away (closes its reading end and stdin), the engine cancels and cleans up: it does not die
# when writing to a closed pipe.
gone = os.path.join(TMP, "gone_out")
rmtree(gone)
p = subprocess.Popen([EXE, "pack", BIG, "-o", gone, "--threads", "2", "--progress", "json", "--control", "stdin"],
                     stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
p.stdout.readline()
p.stdout.close()
p.stderr.close()
p.stdin.close()
p.wait()
check("ventana_ida_motor_limpia", p.returncode == 130 and not os.path.exists(gone), "rc=%s" % p.returncode)

# Closing stdin also cancels.
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
# A cap of exactly the .p5z's size fits; one byte less does not: too_big, and it never goes over the cap.
full = os.path.getsize(pj)
for cap, want in ((full, 0), (full - 1, 1), (64, 1)):
    if os.path.exists(mx):
        os.remove(mx)
    rc, so, err = run("pack", BASIC, "-o", mx, "--p5z", "--threads", "2", "--max-file-size", str(cap), "--progress", "json")
    try:
        res = last_result(json_lines(so))
        if want == 0:
            good = rc == 0 and res.get("ok") is True and os.path.getsize(mx) == full
        else:
            good = rc == 1 and res.get("error") == "too_big" and not os.path.exists(mx)
        check("max_file_size_borde_%d" % cap, good, "rc=%d %s %s" % (rc, so[-200:], err[-200:]))
    except Exception as ex:  # noqa: BLE001
        check("max_file_size_borde_%d" % cap, False, repr(ex))

# --volume-limit (for testing): with 512 KiB volumes, the small fixture gives several, with ids 0..n-1.
vl = os.path.join(TMP, "vl.p5z")
if os.path.exists(vl):
    os.remove(vl)
rc, so, err = run("pack", BASIC, "-o", vl, "--p5z", "--volume-limit", str(512 << 10))
rc2, pjs, err2 = run("info", "--play", vl) if rc == 0 else (rc, "", "")
try:
    vols = json.loads(pjs)["volumes"]
    check("volume_limit_varios_volumenes", rc == 0 and rc2 == 0 and len(vols) >= 3
          and [v["id"] for v in vols] == list(range(len(vols)))
          and all(0 < v["length"] <= 512 << 10 for v in vols)
          and all(vols[k]["offset"] + vols[k]["length"] <= vols[k + 1]["offset"] for k in range(len(vols) - 1)),
          "rc=%d/%d %s %s" % (rc, rc2, pjs[-300:], (err + err2)[-200:]))
except Exception as ex:  # noqa: BLE001
    check("volume_limit_varios_volumenes", False, repr(ex) + (err + err2)[-200:])
_, human, _ = run("info", vl)
m = re.search(r"in (\d+) volumes", human)
check("volume_limit_info", m is not None and int(m.group(1)) >= 3, human[-300:])
# The smallest it accepts: the volume header plus one whole block.
vmin = os.path.join(TMP, "vl_min")
rmtree(vmin)
rc, _, err = run("pack", BASIC, "-o", vmin, "--volume-limit", str(64 + (256 << 10)))
check("volume_limit_minimo", rc == 0 and os.path.isfile(os.path.join(vmin, "ampr_zstd-002.pak")), "rc=%d %s" % (rc, err[-200:]))
for bad in ("0", str(64 + (256 << 10) - 1), "4294901761", "abc", "-1", "512k", ""):
    rc, _, _ = run("pack", BASIC, "-o", os.path.join(TMP, "vl_bad"), "--volume-limit", bad)
    check("volume_limit_%s_sale_2" % (bad or "vacio"), rc == 2 and not os.path.exists(os.path.join(TMP, "vl_bad")), str(rc))

# A param.json without a titleId (or that cannot be read) gives a .p5z without a TITLE_ID: pack warns about it.
noid = os.path.join(TMP, "noid_game")
rmtree(noid)
os.makedirs(os.path.join(noid, "sce_sys"))
with open(os.path.join(noid, "sce_sys", "param.json"), "w") as f:
    f.write('{"titleName":"Sin ID"}')
noid_p5z = os.path.join(TMP, "noid.p5z")
if os.path.exists(noid_p5z):
    os.remove(noid_p5z)
rc, so, err = run("pack", noid, "-o", noid_p5z, "--p5z")
check("pack_sin_title_id_avisa", rc == 0 and "warning:" in err and "TITLE_ID" in err, "rc=%d %s" % (rc, err[-300:]))
bomd = os.path.join(TMP, "bom_game")
rmtree(bomd)
os.makedirs(os.path.join(bomd, "sce_sys"))
with open(os.path.join(bomd, "sce_sys", "param.json"), "wb") as f:
    f.write(b'\xef\xbb\xbf{"titleId":"BOMS00001","titleName":"Con BOM"}\r\n\x00\x00')
rc, so, err = run("info", "--json", bomd)
try:
    check("info_json_param_bom", rc == 0 and json.loads(so)["title_id"] == "BOMS00001", so[-200:])
except Exception as ex:  # noqa: BLE001
    check("info_json_param_bom", False, repr(ex))
bom_p5z = os.path.join(TMP, "bom.p5z")
if os.path.exists(bom_p5z):
    os.remove(bom_p5z)
rc, so, err = run("pack", bomd, "-o", bom_p5z, "--p5z")
_, human, _ = run("info", bom_p5z)
check("pack_param_bom_guarda_title_id", rc == 0 and "TITLE_ID" not in err and "BOMS00001" in human, err[-200:] + human[-200:])

# A byte changed in the data: unpack fails midway and the output folder is not left behind.
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

# Without --progress the output is as always: stdout without JSON and the text on stderr.
rc, so, err = run("verify", pj)
check("human_output_unchanged", rc == 0 and so.startswith("verify:") and '"event"' not in so + err, so + err)

rc, so, _ = run("--help")
check("help_tiene_todas_las_ordenes", rc == 0 and all(w in so for w in ("analyze", "pack", "unpack", "verify", "info",
                                                                        "info --json", "icon", "--capabilities")), so[:300])
check("help_volume_limit_para_pruebas", re.search(r"--volume-limit N .*testing", so) is not None, so[:2000])

# --capabilities: what this version can do, so that the window knows whether it is its own.
rc, so, err = run("--capabilities")
try:
    caps = json.loads(so)
    check("capabilities", rc == 0 and all(caps.get(k) is True for k in
                                          ("progress_json", "control_stdin", "max_file_size", "info_json", "icon", "packs")),
          so + err)
except Exception as ex:  # noqa: BLE001
    check("capabilities", False, repr(ex) + so[-200:] + err[-200:])

# info --json e icon.
rc, so, err = run("info", "--json", BASIC)
try:
    d = json.loads(so)
    check("info_json", rc == 0 and d["kind"] == "exfat" and d["title_id"] == "TEST00001"
          and d["version"] == "01.000.000" and d["files"] > 0 and d["packed"] is False
          and d["has_runtime"] is False and d["has_zstd_runtime"] is False
          and d["install_size"] == d["original_size"] and d["p5z_size"] == 0
          and d["packed_files"] == 0 and d["volumes"] == 0, so[-300:] + err[-200:])
except Exception as ex:
    check("info_json", False, repr(ex) + so[-200:])
rc, so, err = run("info", "--json", pj)
try:
    d = json.loads(so)
    check("info_json_p5z", rc == 0 and d["kind"] == "p5z" and d["packed"] is True
          and d["p5z_size"] == os.path.getsize(pj), so[-300:] + err[-200:])
    # the packs, as info counts them for people: packed files and volumes
    _, human, _ = run("info", pj)
    packed = re.search(r"\((\d+) loose, (\d+) packed\)", human)
    vols = re.search(r"in (\d+) volumes", human)
    check("info_json_packs", packed is not None and vols is not None
          and d["packed_files"] == int(packed.group(2)) and d["volumes"] == int(vols.group(1))
          and 0 < d["packed_files"] < d["files"] and d["volumes"] >= 1, so[-300:] + human)
except Exception as ex:
    check("info_json_p5z", False, repr(ex) + so[-200:])
# Name with quotes, backslashes, accents and control characters: valid JSON and intact bytes.
nm = os.path.join(TMP, "nm_game")
rmtree(nm)
os.makedirs(os.path.join(nm, "sce_sys"))
title = 'A "B" \ café ™'
with open(os.path.join(nm, "sce_sys", "param.json"), "w", encoding="utf-8") as f:
    json.dump({"titleId": "NAME00001", "contentVersion": "01.000.000", "titleName": title}, f, ensure_ascii=False)
rc, so, err = run("info", "--json", nm)
try:
    d = json.loads(so)
    check("info_json_name", rc == 0 and d["kind"] == "folder" and d["name"] == title, repr(so))
except Exception as ex:
    check("info_json_name", False, repr(ex) + so[-200:])
rc, so, err = run("info", "--json", os.path.join(TMP, "no_existe_nunca"))
check("info_json_no_existe", rc == 1 and "error:" in err, "rc=%d %s" % (rc, err))
rc, _, _ = run("info", "--json")
check("info_json_sin_ruta_sale_2", rc == 2, str(rc))

png = bytes(range(64))
open(os.path.join(nm, "sce_sys", "icon0.png"), "wb").write(png)
outpng = os.path.join(TMP, "icon_out.png")
if os.path.exists(outpng):
    os.remove(outpng)
rc, so, err = run("icon", nm, "-o", outpng)
check("icon_cli", rc == 0 and os.path.exists(outpng) and open(outpng, "rb").read() == png, "rc=%d %s" % (rc, err))
outpng2 = os.path.join(TMP, "icon_none.png")
if os.path.exists(outpng2):
    os.remove(outpng2)
rc, _, err = run("icon", BASIC, "-o", outpng2)
check("icon_cli_sin_icono_sale_1", rc == 1 and not os.path.exists(outpng2), "rc=%d %s" % (rc, err))
rc, _, _ = run("icon", nm)
check("icon_sin_salida_sale_2", rc == 2, str(rc))

print("test_cli: %d failures" % len(failures))
sys.exit(1 if failures else 0)
