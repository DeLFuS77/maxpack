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
check("version", rc == 0 and re.fullmatch(r"maxpack 1[.]0[.]7 \(zstd 1\.5\.6, zlib 1\.3\.1\)\s*", out) is not None, out)
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
check("pkg_sale_1", rc == 1 and "PS5 package" in err, err)   # 64 bytes: too short to be one

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

# ---- analyze --levels: the levels compared on this PC ---------------------------------------------
LEVEL_ROW = re.compile(r"^ +(\d+) +\d+\.\d\d [KMGT]?B +-?\d+\.\d % ", re.M)
rc, out, err = run("analyze", BASIC, "--levels", "--threads", "2")
check("levels_text", rc == 0 and LEVEL_ROW.findall(out) == ["3", "9", "15", "19"] and "Levels compared" in out
      and '"event"' not in out, "rc=%d %s %s" % (rc, out[-600:], err[-200:]))
lj = os.path.join(TMP, "levels.json")
if os.path.exists(lj):
    os.remove(lj)
rc, out, err = run("analyze", BASIC, "--levels", "19,1,12,6", "--threads", "2", "--json", lj)
try:
    with open(lj, encoding="utf-8") as f:
        lv = json.load(f)
    rows = lv["levels"]
    keys = {"level", "packed", "p5z_size", "folder_size", "rate", "p5z_seconds", "folder_seconds"}
    check("levels_json", rc == 0 and [x["level"] for x in rows] == [1, 6, 12, 19] and all(keys <= set(x) for x in rows)
          and all(0 < x["p5z_size"] < lv["original_size"] and x["rate"] > 0 and x["p5z_seconds"] >= 1 for x in rows)
          and lv["original_size"] == lv["loose_bytes"] + lv["packable_bytes"] and lv["threads"] == 2
          and lv["sample_rate"] == 1 and all_finite(lv) and LEVEL_ROW.findall(out) == ["1", "6", "12", "19"],
          json.dumps(lv)[:600])
except Exception as ex:  # noqa: BLE001
    check("levels_json", False, repr(ex) + out[-300:] + err[-300:])
rc, so, err = run("analyze", BASIC, "--levels", "3,19", "--threads", "2", "--progress", "json")
try:
    evs = json_lines(so)
    res = last_result(evs)
    check("levels_progress_json", rc == 0 and res.get("ok") is True and [x["level"] for x in res["levels"]] == [3, 19]
          and [e["level"] for e in evs if e["event"] == "level"] == [3, 19] and progress_monotonic(evs)
          and has_final_progress(evs) and all(e["event"] in ("progress", "level", "result") for e in evs)
          and "Levels compared" in err, "rc=%d %s %s" % (rc, so[-400:], err[-200:]))
except Exception as ex:  # noqa: BLE001
    check("levels_progress_json", False, repr(ex) + so[-300:])
for bad in ("0", "23", "3,,9", "3,9,"):
    rc, _, _ = run("analyze", BASIC, "--levels", bad)
    check("levels_bad_list_%s_exits_2" % bad, rc == 2, str(rc))
rc, _, _ = run("analyze", BASIC, "--levels", "x")
check("levels_extra_argument_exits_2", rc == 2, str(rc))
rc, _, _ = run("analyze", BASIC, "--levels", "--list")
check("levels_with_list_exits_2", rc == 2, str(rc))
rc, so, _ = run("analyze", os.path.join(TMP, "never_there"), "--levels", "--progress", "json")
check("levels_missing_source", rc == 1 and last_result(json_lines(so)).get("error") == "bad_input", "rc=%d %s" % (rc, so))
p = subprocess.Popen([EXE, "analyze", BIG, "--levels", "19,20,21,22", "--threads", "1", "--progress", "json",
                      "--control", "stdin"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
first = p.stdout.readline()
p.stdin.write(b"cancel\n")
p.stdin.flush()
rest = p.stdout.read()
p.stderr.read()
p.wait()
try:
    res = last_result(json_lines((first + rest).decode("utf-8", "replace")))
    check("levels_cancel", p.returncode == 130 and res.get("error") == "cancelled", "rc=%s %s" % (p.returncode, rest[-300:]))
except Exception as ex:  # noqa: BLE001
    check("levels_cancel", False, repr(ex))

# ---- measure: this PC, with no game; the disk of a folder, where nothing is left ---------------------
mdir = os.path.join(TMP, "measure_dest")
rmtree(mdir)
os.makedirs(mdir)
mj = os.path.join(TMP, "measure.json")
if os.path.exists(mj):
    os.remove(mj)
rc, so, err = run("measure", mdir, "--seconds", "0.2", "--progress", "json", "--json", mj)
try:
    evs = json_lines(so)
    res = last_result(evs)
    counts = [t["threads"] for t in res["trials"]]
    disk = res["disk"]
    with open(mj, encoding="utf-8") as f:
        saved = json.load(f)
    check("measure_progress_json", rc == 0 and res.get("ok") is True and res["cpu_threads"] >= 1 and res["level"] == 19
          and counts == sorted(set(counts)) and counts[-1] == res["cpu_threads"] and res["recommended_threads"] in counts
          and res["fastest_threads"] in counts and all(t["rate"] > 0 and t["bytes"] > 0 for t in res["trials"])
          and [e["threads"] for e in evs if e["event"] == "trial"] == counts and progress_monotonic(evs)
          and has_final_progress(evs) and disk["write_rate"] > 0 and disk["read_rate"] > 0 and 0 < disk["read"] <= disk["written"]
          and os.listdir(mdir) == [] and saved["trials"] == res["trials"] and "Recommended" in err,
          "rc=%d %s %s" % (rc, so[-500:], err[-300:]))
except Exception as ex:  # noqa: BLE001
    check("measure_progress_json", False, repr(ex) + so[-300:] + err[-300:])
rc, so, err = run("measure", "--seconds", "0.1", "--level", "3")
check("measure_text_no_folder", rc == 0 and "Measured on this PC" in so and "zstd level 3" in so and "Recommended" in so
      and "disk of" not in so, "rc=%d %s %s" % (rc, so[-400:], err[-200:]))
rc, so, _ = run("measure", "--seconds", "0.1", "--progress", "json")
try:
    check("measure_no_folder_disk_null", rc == 0 and last_result(json_lines(so))["disk"] is None, so[-300:])
except Exception as ex:  # noqa: BLE001
    check("measure_no_folder_disk_null", False, repr(ex))
for name, args in (("missing_folder", [os.path.join(TMP, "never_there")]), ("file", [BASIC]), ("level_23", ["--level", "23"]),
                   ("seconds_0", ["--seconds", "0"]), ("two_folders", [mdir, mdir]), ("unknown", ["--bogus"])):
    rc, _, _ = run("measure", *args)
    check("measure_%s_exits_2" % name, rc == 2, str(rc))
p = subprocess.Popen([EXE, "measure", mdir, "--seconds", "4", "--progress", "json", "--control", "stdin"],
                     stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
first = p.stdout.readline()
p.stdin.write(b"cancel\n")
p.stdin.flush()
rest = p.stdout.read()
p.stderr.read()
p.wait()
try:
    res = last_result(json_lines((first + rest).decode("utf-8", "replace")))
    check("measure_cancel", p.returncode == 130 and res.get("error") == "cancelled" and os.listdir(mdir) == [],
          "rc=%s %s %s" % (p.returncode, rest[-300:], os.listdir(mdir)))
except Exception as ex:  # noqa: BLE001
    check("measure_cancel", False, repr(ex))

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
                                                                        "info --json", "icon", "--capabilities", "measure",
                                                                        "--levels")), so[:300])
check("help_volume_limit_para_pruebas", re.search(r"--volume-limit N .*testing", so) is not None, so[:2000])

# --capabilities: what this version can do, so that the window knows whether it is its own.
rc, so, err = run("--capabilities")
try:
    caps = json.loads(so)
    check("capabilities", rc == 0 and all(caps.get(k) is True for k in
                                          ("progress_json", "control_stdin", "max_file_size", "info_json", "icon", "packs",
                                           "content_id", "levels", "measure")),
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
# The content ID (its first letters are the region) of an image, the .p5z made of it and a folder;
# "" when param.json gives none.
CID = os.path.join(FX, "cid.exfat")
cid_p5z = os.path.join(TMP, "cid.p5z")
if os.path.exists(cid_p5z):
    os.remove(cid_p5z)
run("pack", CID, "-o", cid_p5z, "--p5z", "--threads", "2")
for name, path, want in (("image", CID, "EP9000-TEST00003_00-MAXPACKCONTENTID"),
                         ("p5z", cid_p5z, "EP9000-TEST00003_00-MAXPACKCONTENTID"), ("none", BASIC, ""), ("folder_none", nm, "")):
    rc, so, err = run("info", "--json", path)
    try:
        check("info_json_content_id_" + name, rc == 0 and json.loads(so)["content_id"] == want, so[-300:] + err[-200:])
    except Exception as ex:  # noqa: BLE001
        check("info_json_content_id_" + name, False, repr(ex) + so[-200:])
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

# ---- ShadowMountPlus images: .ffpkg (UFS2), .ffpfs (PFS), .ffpfsc (a PFS container) ----------------
check("capabilities_images", caps.get("images") is True if isinstance(caps, dict) else False, so)
rc, so, _ = run("info", "--json", BASIC)
old_keys = set(json.loads(so)) if rc == 0 else set()
check("info_json_keeps_fields", {"kind", "title_id", "version", "name", "content_id", "original_size", "install_size",
                                 "p5z_size", "files", "packed_files", "volumes", "has_runtime", "has_zstd_runtime",
                                 "packed", "inner", "compressed"} <= old_keys, str(old_keys))
IMAGES = {"basic.ffpkg": ("ufs2", "", False), "profile.ffpkg": ("ufs2", "", False), "basic.ffpfs": ("pfs", "", False),
          "basic_c.ffpfs": ("pfs", "", True), "basic.exfat.ffpfsc": ("pfsc", "exfat", True),
          "basic.ffpkg.ffpfsc": ("pfsc", "ufs2", True), "basic.ffpfs.ffpfsc": ("pfsc", "pfs", True),
          "basic_raw.exfat.ffpfsc": ("pfsc", "exfat", False), "mkpfs_exfat.ffpfsc": ("pfsc", "exfat", True),
          "mkpfs_raw.ffpfs": ("pfs", "", True)}
for name, (kind, inner, comp) in IMAGES.items():
    rc, so, err = run("info", "--json", os.path.join(FX, name))
    try:
        d = json.loads(so)
        want_files = 38 if name.startswith("mkpfs") else manifest_count()
        check("info_json_" + name, rc == 0 and d["kind"] == kind and d["inner"] == inner and d["compressed"] is comp
              and d["files"] == want_files and set(d) == old_keys and d["title_id"] in ("TEST00001", "BREW00000"),
              so[-300:] + err[-200:])
    except Exception as ex:  # noqa: BLE001
        check("info_json_" + name, False, repr(ex) + so[-200:] + err[-200:])
# The kind is the contents', whatever the name says.
misnamed = os.path.join(TMP, "really_ufs2.exfat")
shutil.copyfile(os.path.join(FX, "basic.ffpkg"), misnamed)
rc, so, err = run("info", "--json", misnamed)
check("info_json_misnamed", rc == 0 and json.loads(so or "{}").get("kind") == "ufs2", so + err)
# Packing an image: the same .p5z as the .exfat of the same game, and its folder for the console.
ref_p5z = os.path.join(TMP, "img_ref.p5z")
for f in (ref_p5z,):
    if os.path.exists(f):
        os.remove(f)
rc, _, err = run("pack", BASIC, "-o", ref_p5z, "--p5z", "--level", "3", "--threads", "4")
for name in ("basic.ffpkg", "basic_c.ffpfs", "basic.exfat.ffpfsc"):
    dst = os.path.join(TMP, "img_" + name + ".p5z")
    if os.path.exists(dst):
        os.remove(dst)
    rc, _, err = run("pack", os.path.join(FX, name), "-o", dst, "--p5z", "--level", "3", "--threads", "4")
    same = rc == 0 and os.path.exists(dst) and open(dst, "rb").read() == open(ref_p5z, "rb").read()
    check("pack_p5z_" + name, same, "rc=%d %s" % (rc, err[-300:]))
img_dir = os.path.join(TMP, "img_folder")
rmtree(img_dir)
rc, _, err = run("pack", os.path.join(FX, "basic.ffpkg.ffpfsc"), "-o", img_dir, "--level", "3", "--threads", "4")
check("pack_folder_ffpfsc", rc == 0 and os.path.exists(os.path.join(img_dir, "ampr_zstd.index")), err[-300:])
orig = os.path.join(TMP, "img_original")
rmtree(orig)
rc, _, err = run("unpack", os.path.join(TMP, "img_basic.exfat.ffpfsc.p5z"), "--original", "-o", orig)
got = sum(len(fs) for _, _, fs in os.walk(orig)) if rc == 0 else -1
check("unpack_original_of_ffpfsc", rc == 0 and got == manifest_count(), "rc=%d files=%d %s" % (rc, got, err[-300:]))
# analyze: the kind, what a container holds, and the same files as the .exfat.
js3 = os.path.join(TMP, "img.json")
rc, out, err = run("analyze", os.path.join(FX, "basic.exfat.ffpfsc"), "--no-dedup", "--no-transport", "--json", js3)
try:
    with open(js3, encoding="utf-8") as f:
        rep = json.load(f)
    check("analyze_json_ffpfsc", rc == 0 and rep["source"]["kind"] == "pfsc" and rep["source"]["inner"] == "exfat"
          and rep["files"]["total"] == manifest_count(), str(rep.get("source")) + err[-200:])
    check("analyze_text_ffpfsc", "(PFS container with an exFAT image inside, compressed)" in out, out[:300])
    # a compressed image: what is saved is against the game in it, and the .p5z is also compared with
    # the image file, which takes less
    check("analyze_text_ffpfsc_vs_image", "Saved is against the game in it" in out and "than the image file)" in out,
          out[-1200:])
except Exception as ex:  # noqa: BLE001
    check("analyze_json_ffpfsc", False, repr(ex) + err[-300:])
rc, out, err = run("analyze", os.path.join(FX, "basic.ffpkg"), "--no-dedup", "--no-transport")
check("analyze_text_ffpkg_no_image_note", rc == 0 and "image file" not in out, out[-800:])
rc, out, err = run("analyze", os.path.join(FX, "basic.ffpkg"), "--levels", "3", "--threads", "2")
check("levels_ffpkg", rc == 0 and LEVEL_ROW.findall(out) == ["3"] and "image file" not in out, out[-400:] + err[-300:])
rc, out, err = run("analyze", os.path.join(FX, "basic.exfat.ffpfsc"), "--levels", "3,19", "--threads", "2")
check("levels_ffpfsc_vs_image", rc == 0 and LEVEL_ROW.findall(out) == ["3", "19"]
      and "Against the image file: level 3 " in out and ", level 19 " in out, out[-700:] + err[-300:])
# Damaged images: exit 1 with the reason, nothing written.
for name, words in (("truncated.bad.ffpkg", "truncated"), ("loop.bad.ffpfs", "reached twice"),
                    ("pfsc_inflate.bad.ffpfsc", "does not inflate"), ("signed.bad.ffpfs", "signed or encrypted"),
                    ("link.bad.ffpkg", "symbolic link"), ("nest4.ffpfsc", "deep")):
    dst = os.path.join(TMP, "bad_" + name + ".p5z")
    if os.path.exists(dst):
        os.remove(dst)
    rc, _, err = run("pack", os.path.join(FX, name), "-o", dst, "--p5z", "--threads", "2")
    check("pack_bad_" + name, rc == 1 and words in err and not os.path.exists(dst), "rc=%d %s" % (rc, err[-300:]))
rc, _, err = run("info", "--json", os.path.join(FX, "notufs.bad.ffpkg"))
check("info_json_not_ufs2", rc == 1 and "not a UFS2 image" in err, err)

# ---- PS5 debug packages (FPKG, .pkg) -------------------------------------------------------------
PASSCODE = "MaxPackTestPasscode-0123456789ab"
check("capabilities_fpkg", isinstance(caps, dict) and caps.get("fpkg") is True and caps.get("passcode") is True, so)
for name, comp in (("basic.pkg", False), ("basic_k.pkg", True)):
    rc, so, err = run("info", "--json", os.path.join(FX, name))
    try:
        d = json.loads(so)
        check("info_json_" + name, rc == 0 and d["kind"] == "fpkg" and d["inner"] == "" and d["compressed"] is comp
              and d["files"] == manifest_count() and set(d) == old_keys and d["title_id"] == "TEST00001"
              and d["content_id"] == "UP0000-TEST00001_00-MAXPACKTEST00000", so[-300:] + err[-200:])
    except Exception as ex:  # noqa: BLE001
        check("info_json_" + name, False, repr(ex) + so[-200:] + err[-200:])
# The passcode: the default does not open pass.pkg; --passcode does, in any place on the line.
pass_pkg = os.path.join(FX, "pass.pkg")
rc, so, err = run("info", "--json", pass_pkg)
check("fpkg_needs_passcode", rc == 1 and "this package was made with another passcode; give it with --passcode" in err,
      err)
rc, so, err = run("info", "--json", pass_pkg, "--passcode", PASSCODE)
check("fpkg_passcode_info", rc == 0 and json.loads(so or "{}").get("kind") == "fpkg", so + err)
rc, so, err = run("info", "--passcode", PASSCODE, "--json", pass_pkg)
check("fpkg_passcode_first", rc == 0, err)
rc, _, err = run("info", "--json", pass_pkg, "--passcode", "0123")
check("fpkg_passcode_short_exit_2", rc == 2 and "--passcode must be 32 characters" in err, err[:300])
rc, _, err = run("info", "--json", pass_pkg, "--passcode")
check("fpkg_passcode_missing_exit_2", rc == 2 and "missing value for: --passcode" in err, err[:300])
rc, _, err = run("unpack", os.path.join(TMP, "img_ref.p5z"), "--folder", "-o", os.path.join(TMP, "x"),
                 "--passcode", PASSCODE)
check("fpkg_passcode_not_for_unpack", rc == 2 and "unknown option" in err, err[:300])
dst = os.path.join(TMP, "pass_out")
rmtree(dst)
rc, so, err = run("pack", pass_pkg, "-o", dst, "--progress", "json", "--threads", "2")
res = [json.loads(l) for l in so.splitlines() if l.startswith("{")]
check("fpkg_pack_passcode_code", rc == 1 and res and res[-1].get("error") == "passcode" and not os.path.exists(dst),
      so[-300:] + err[-300:])
rc, _, err = run("pack", pass_pkg, "-o", dst, "--passcode", PASSCODE, "--level", "3", "--threads", "2")
check("fpkg_pack_with_passcode", rc == 0 and os.path.exists(os.path.join(dst, "eboot.bin")), err[-300:])
# verify --against the package: with its passcode it compares, without it it says which is needed
rc, _, err = run("verify", dst, "--against", pass_pkg, "--passcode", PASSCODE, "--threads", "2")
check("fpkg_verify_against", rc == 0, err[-300:])
rc, _, err = run("verify", dst, "--against", pass_pkg, "--threads", "2")
check("fpkg_verify_against_needs_passcode", rc == 1 and "--passcode" in err, err[-300:])
rc, so, err = run("pack", pass_pkg, "-o", dst + "_bad", "--passcode", "x" * 32, "--progress", "json")
check("fpkg_pack_wrong_passcode", rc == 1 and "than the one given" in err, err[-300:])
# A package packs to the same .p5z as the basic game's .exfat (its files have the same dates).
for name in ("basic.pkg", "basic_k.pkg"):
    dst = os.path.join(TMP, "fpkg_" + name + ".p5z")
    if os.path.exists(dst):
        os.remove(dst)
    rc, _, err = run("pack", os.path.join(FX, name), "-o", dst, "--p5z", "--level", "3", "--threads", "4")
    same = rc == 0 and os.path.exists(dst) and open(dst, "rb").read() == open(ref_p5z, "rb").read()
    check("pack_p5z_" + name, same, "rc=%d %s" % (rc, err[-300:]))
# analyze: the kind, and Saved against the game, the package file beside it.
js4 = os.path.join(TMP, "fpkg.json")
rc, out, err = run("analyze", os.path.join(FX, "basic_k.pkg"), "--no-dedup", "--no-transport", "--json", js4)
try:
    with open(js4, encoding="utf-8") as f:
        rep = json.load(f)
    check("analyze_json_fpkg", rc == 0 and rep["source"]["kind"] == "fpkg" and rep["files"]["total"] == manifest_count(),
          str(rep.get("source")) + err[-200:])
    check("analyze_text_fpkg", "(PS5 package (FPKG), compressed)" in out and "not the package file" in out
          and "than the package file)" in out, out[-1200:])
except Exception as ex:  # noqa: BLE001
    check("analyze_json_fpkg", False, repr(ex) + err[-300:])
rc, out, err = run("analyze", os.path.join(FX, "basic.pkg"), "--levels", "3", "--threads", "2")
check("levels_fpkg_vs_package", rc == 0 and LEVEL_ROW.findall(out) == ["3"] and "Against the package file: level 3 " in out,
      out[-700:] + err[-300:])
rc, out, err = run("analyze", pass_pkg, "--list", "--passcode", PASSCODE)
check("analyze_list_passcode", rc == 0 and "data/a.bin" in out, err[-300:])
# What is refused, and damaged packages: exit 1 with the reason, nothing written.
for name, words in (("retail.pkg", "retail packages need keys this tool does not have"),
                    ("patch.pkg", "patch (update) package"), ("dlc.pkg", "additional content"),
                    ("ps4.pkg", "MaxPack is for PS5 games"), ("other.pkg", "content type 0x1A"),
                    ("notpkg.pkg", "not a PS5 package"), ("loop.bad.pkg", "a loop"),
                    ("naps.bad.pkg", "shorter than its header"), ("icv.bad.pkg", "checksum does not match")):
    dst = os.path.join(TMP, "bad_" + name + ".p5z")
    if os.path.exists(dst):
        os.remove(dst)
    rc, _, err = run("pack", os.path.join(FX, name), "-o", dst, "--p5z", "--threads", "2")
    check("pack_refused_" + name, rc == 1 and words in err and not os.path.exists(dst), "rc=%d %s" % (rc, err[-300:]))

# Completeness: the dump against the list of the original package's files it carries
# (sce_sys/playgo-hash-table.dat), in analyze, info --json, pack and verify; the fixtures of make_playgo.py.
PG = os.path.join(FX, "playgo")
FIELDS = {"checkable", "reason", "listed", "present", "missing", "extra_count", "extra_bytes", "leftover_count",
          "leftover_bytes"}


def info_complete(path):
    rc, so, err = run("info", "--json", path)
    try:
        return rc, json.loads(so).get("completeness"), err
    except Exception:  # noqa: BLE001
        return rc, None, so + err


rc, c, err = info_complete(os.path.join(PG, "complete"))
check("info_json_complete", rc == 0 and c is not None and FIELDS <= set(c) and c["checkable"] is True
      and c["reason"] == "" and c["listed"] == 8 and c["present"] == 8 and c["missing"] == 0 and c["extra_count"] == 0
      and c["extra_paths"] == [], str(c) + err)
rc, c, err = info_complete(os.path.join(PG, "missing"))
check("info_json_missing", rc == 0 and c and c["checkable"] and c["missing"] == 2 and c["present"] == 6, str(c) + err)
rc, c, err = info_complete(os.path.join(PG, "missing.ffpkg"))
check("info_json_missing_ufs2", rc == 0 and c and c["checkable"] and c["missing"] == 2, str(c) + err)
rc, c, err = info_complete(os.path.join(PG, "leftovers"))
check("info_json_leftovers", rc == 0 and c and c["missing"] == 0 and c["extra_count"] == 3 and c["leftover_count"] == 3
      and c["leftover_bytes"] == 7000 + 4000 + 64 and c["extra_paths"][0] == "decrypted/eboot.bin"
      and "Thumbs.db" not in c["extra_paths"], str(c) + err)
for name, reason in (("notable", "no_table"), ("badtable", "damaged"), ("alien", "unknown_format")):
    rc, c, err = info_complete(os.path.join(PG, name))
    check("info_json_" + name, rc == 0 and c and c["checkable"] is False and c["reason"] == reason
          and c["missing"] == 0, str(c) + err)
rc, c, err = info_complete(BASIC)
check("info_json_completeness_without_table", rc == 0 and c and c["checkable"] is False and c["reason"] == "no_table",
      str(c) + err)
# analyze: a line of text, and the object in its JSON
js4 = os.path.join(TMP, "missing.json")
rc, out, err = run("analyze", os.path.join(PG, "missing"), "--no-dedup", "--no-transport", "--json", js4)
try:
    with open(js4, encoding="utf-8") as f:
        rep = json.load(f)
    check("analyze_json_completeness", rc == 0 and rep["completeness"]["missing"] == 2
          and rep["completeness"]["listed"] == 8 and "extra_paths" in rep["completeness"], str(rep.get("completeness")))
except Exception as ex:  # noqa: BLE001
    check("analyze_json_completeness", False, repr(ex) + err[-300:])
check("analyze_text_completeness", "Completeness: incomplete: 2 of the 8 files of the original package are missing" in out,
      out[:600])
rc, out, err = run("analyze", os.path.join(PG, "complete"), "--no-dedup", "--no-transport")
check("analyze_text_complete", rc == 0 and "Completeness: complete: all 8 files of the original package" in out, out[:600])
rc, out, err = run("analyze", os.path.join(PG, "notable"), "--no-dedup", "--no-transport")
check("analyze_text_no_table", rc == 0 and "Completeness: not checked: it has no list" in out
      and "1 file (7.00 KB) looks like what dump tools leave behind" in out, out[:800])
# pack warns of what is missing and never stops for it
mp5 = os.path.join(TMP, "pg_missing.p5z")
if os.path.exists(mp5):
    os.remove(mp5)
rc, so, err = run("pack", os.path.join(PG, "missing"), "-o", mp5, "--p5z", "--level", "3", "--threads", "2",
                  "--progress", "json")
res = last_result(json_lines(so))
check("pack_warns_missing", rc == 0 and os.path.exists(mp5)
      and "warning: the dump is incomplete: 2 of the 8 files of the original package are missing" in err, err[-500:])
check("pack_result_completeness", res.get("completeness", {}).get("missing") == 2
      and "extra_paths" not in res.get("completeness", {}) and res.get("left_out_files") == 0, str(res))
# verify of a .p5z checks against the table it carries
rc, so, err = run("verify", mp5)
check("verify_p5z_incomplete", rc == 0 and "incomplete: 2 of the 8 files of the original package are missing" in so, so + err)
rc, so, err = run("verify", mp5, "--progress", "json")
res = last_result(json_lines(so))
check("verify_result_completeness", rc == 0 and res.get("ok") is True and res.get("completeness", {}).get("missing") == 2,
      str(res))
# --leave-out-leftovers: out when the table proves it, kept (and said) when there is none
lp5 = os.path.join(TMP, "pg_left.p5z")
if os.path.exists(lp5):
    os.remove(lp5)
rc, so, err = run("pack", os.path.join(PG, "leftovers"), "-o", lp5, "--p5z", "--level", "3", "--threads", "2",
                  "--leave-out-leftovers", "--progress", "json")
res = last_result(json_lines(so))
check("pack_leave_out_leftovers", rc == 0 and res.get("left_out_files") == 3 and res.get("left_out_bytes") == 11064
      and "left out: 3 files dump tools left behind" in err, str(res) + err[-300:])
rc, so, err = run("verify", lp5)
check("verify_p5z_complete", rc == 0 and "complete: all 8 files of the original package" in so
      and "not in the original package" not in so, so + err)   # its dlc_emu.ini is never a file of more
np5 = os.path.join(TMP, "pg_notable.p5z")
if os.path.exists(np5):
    os.remove(np5)
rc, so, err = run("pack", os.path.join(PG, "notable"), "-o", np5, "--p5z", "--level", "3", "--threads", "2",
                  "--leave-out-leftovers")
check("pack_keeps_unproven_leftovers", rc == 0 and "note: kept: 1 file" in err and "playgo-hash-table.dat" in err,
      err[-400:])
_, c, _ = info_complete(np5)
check("info_json_p5z_no_table", c and c["checkable"] is False and c["leftover_count"] == 1, str(c))
rc, so, _ = run("--help")
check("help_leave_out_leftovers", "--leave-out-leftovers" in so, so[-1500:])

rc, so, _ = run("--capabilities")
check("capabilities_completeness", json.loads(so).get("completeness") is True, so)

# Without a usable table, the dump tool's own list (ampr_emu.index): whether the copy has every file it
# wrote, with its size; never "complete". The fixtures of make_playgo.py in dumpindex/.
DI = os.path.join(FX, "dumpindex")
rc, c, err = info_complete(os.path.join(DI, "ok"))
check("info_json_dump_index_ok", rc == 0 and c and c["checkable"] is True and c["source"] == "dump_index"
      and c["listed"] == 13 and c["present"] == 13 and c["missing"] == 0 and c["size_mismatch"] == 0
      and c["extra_count"] == 2, str(c) + err)
rc, c, err = info_complete(os.path.join(DI, "broken"))
check("info_json_dump_index_broken", rc == 0 and c and c["source"] == "dump_index" and c["missing"] == 2
      and c["size_mismatch"] == 1 and c["present"] == 10, str(c) + err)
for name, reason in (("foreign", "dump_index_mismatch"), ("damaged", "dump_index_damaged"), ("packed", "dump_index_packed")):
    rc, c, err = info_complete(os.path.join(DI, name))
    check("info_json_dump_index_" + name, rc == 0 and c and c["checkable"] is False and c["reason"] == reason
          and c["source"] == "", str(c) + err)
rc, c, err = info_complete(os.path.join(DI, "both"))
check("info_json_table_wins", rc == 0 and c and c["source"] == "playgo" and c["missing"] == 0, str(c) + err)
rc, c, err = info_complete(os.path.join(PG, "complete"))
check("info_json_source_playgo", c and c["source"] == "playgo" and c["size_mismatch"] == 0, str(c))
rc, out, err = run("analyze", os.path.join(DI, "ok"), "--no-dedup", "--no-transport")
check("analyze_text_dump_index", rc == 0 and "Completeness: all 13 files the dump tool wrote are here (its list does not "
      "prove it got every file of the game)" in out and "Completeness: complete" not in out, out[:700])
bp5 = os.path.join(TMP, "di_broken.p5z")
if os.path.exists(bp5):
    os.remove(bp5)
rc, so, err = run("pack", os.path.join(DI, "broken"), "-o", bp5, "--p5z", "--level", "3", "--threads", "2",
                  "--leave-out-leftovers", "--progress", "json")
res = last_result(json_lines(so))
check("pack_warns_damaged_copy", rc == 0 and "warning: damaged copy: 2 of the 13 files the dump tool wrote are missing, "
      "1 has another size: what was made of it lacks them too" in err, err[-600:])
check("pack_dump_index_keeps_leftovers", res.get("left_out_files") == 0 and res.get("completeness", {}).get("source") == "dump_index"
      and "note: kept: 1 file" in err, str(res) + err[-300:])
rc, so, err = run("verify", bp5)
check("verify_p5z_dump_index", rc == 0 and "damaged copy: 2 of the 13 files the dump tool wrote are missing" in so, so + err)

# Completeness of a PS5 package (.pkg): its files are a source like any other, so it is checked against
# the table it carries, or its dump tool's list, as the folder it was made of is (make_fpkg.py packs
# playgo/leftovers and dumpindex/broken with their files as they are).
for pkg, folder in (("playgo_leftovers.pkg", os.path.join(PG, "leftovers")),
                    ("dumpindex_broken.pkg", os.path.join(DI, "broken"))):
    rc, c, err = info_complete(os.path.join(FX, pkg))
    _, want, _ = info_complete(folder)
    check("info_json_completeness_" + pkg, rc == 0 and c is not None and c["checkable"] is True and c == want,
          str(c) + " / " + str(want) + err)
rc, out, err = run("analyze", os.path.join(FX, "playgo_leftovers.pkg"), "--no-dedup", "--no-transport")
check("analyze_text_completeness_pkg", rc == 0 and "Completeness: complete: all 8 files of the original package" in out
      and "11.06 KB of them is what dump tools leave behind" in out, out[:700])
kp5 = os.path.join(TMP, "pg_left_pkg.p5z")
if os.path.exists(kp5):
    os.remove(kp5)
rc, so, err = run("pack", os.path.join(FX, "playgo_leftovers.pkg"), "-o", kp5, "--p5z", "--level", "3",
                  "--threads", "2", "--leave-out-leftovers", "--progress", "json")
res = last_result(json_lines(so))
check("pack_pkg_leave_out_leftovers", rc == 0 and res.get("left_out_files") == 3 and res.get("left_out_bytes") == 11064
      and res.get("completeness", {}).get("source") == "playgo", str(res) + err[-300:])
check("pack_pkg_leftovers_same_p5z", os.path.exists(kp5) and os.path.exists(lp5)
      and open(kp5, "rb").read() == open(lp5, "rb").read(), kp5)
di_broken = os.path.join(TMP, "di_broken_pkg.p5z")
if os.path.exists(di_broken):
    os.remove(di_broken)
rc, so, err = run("pack", os.path.join(FX, "dumpindex_broken.pkg"), "-o", di_broken,
                  "--p5z", "--level", "3", "--threads", "2", "--leave-out-leftovers")
check("pack_pkg_warns_damaged_copy", rc == 0 and "note: kept: 1 file" in err
      and "warning: damaged copy: 2 of the 13 files the dump tool wrote are missing" in err, err[-600:])
# A builder's package whose playgo-hash-table.dat is not a table (game.pkg's stands in for one) and a
# package without one: not checked, with the reason, and everything else as before.
for name, reason in (("game.pkg", "unknown_format"), ("basic_k.pkg", "no_table")):
    rc, c, err = info_complete(os.path.join(FX, name))
    check("info_json_completeness_" + name, rc == 0 and c and c["checkable"] is False and c["reason"] == reason
          and c["missing"] == 0, str(c) + err)
rc, c, err = info_complete(os.path.join(FX, "pass.pkg"))
check("info_json_completeness_passcode_needed", rc == 1 and c is None and "another passcode" in err, err[-300:])
rc, so, err = run("info", "--json", "--passcode", PASSCODE, os.path.join(FX, "pass.pkg"))
check("info_json_completeness_passcode",
      rc == 0 and json.loads(so or "{}").get("completeness", {}).get("reason") == "no_table", so + err[-300:])

# ---- A package made by another builder: LibProsperoPkg 1.2.0 (ProsperoPkgTool's test package, tests/data) ----
import hashlib  # noqa: E402
LPP = os.path.join(FX, "lpp120-mixed.pkg")
rc, so, err = run("info", "--json", LPP)
d = json.loads(so or "{}") if rc == 0 else {}
check("lpp120_info", rc == 0 and d.get("kind") == "fpkg" and d.get("title_id") == "PPSA12345"
      and d.get("content_id") == "UP0000-PPSA12345_00-TESTPACKAGE00000", so[-300:] + err[-300:])
lpp_p5z = os.path.join(TMP, "lpp120.p5z")
lpp_back = os.path.join(TMP, "lpp120_back")
if os.path.exists(lpp_p5z):
    os.remove(lpp_p5z)
rmtree(lpp_back)
rc1, _, err1 = run("pack", LPP, "-o", lpp_p5z, "--p5z", "--level", "3", "--threads", "4")
rc2, _, err2 = run("unpack", lpp_p5z, "-o", lpp_back, "--original")
payload = os.path.join(lpp_back, "payload.bin")
digest = ""
if os.path.exists(payload):
    with open(payload, "rb") as f:
        digest = hashlib.sha256(f.read()).hexdigest()
# the SHA-256 its README gives for the 8 MiB payload.bin
check("lpp120_payload", rc1 == 0 and rc2 == 0
      and digest == "084b53568666b6b3ba37c3721a94e899909660b5ea03b8d4ce0a4752f9401a65", err1[-300:] + err2[-300:])

# ---- DLC: what info --json says of one and of a game, its icon (make_fpkg.py's DLC of the mini game) ----
DATA_CID = "UP0000-PPSA00000_00-MAXPACKDLCDATA01"
LICENSE_CID = "UP0000-PPSA00000_00-MAXPACKLICENSE01"


def info_json(path):
    rc, so, err = run("info", "--json", path)
    try:
        return rc, json.loads(so), err
    except Exception:  # noqa: BLE001
        return rc, {}, so + err


rc, d, err = info_json(os.path.join(FX, "dlc_data.pkg"))
check("info_json_dlc", rc == 0 and d.get("content") == "dlc" and d.get("dlc_data") is True
      and d.get("title_id") == "PPSA00000" and d.get("content_id") == DATA_CID and d.get("dlc") == []
      and d.get("kind") == "fpkg", str(d) + err[-300:])
rc, d, err = info_json(os.path.join(FX, "dlc_license"))
check("info_json_dlc_license", rc == 0 and d.get("content") == "dlc" and d.get("dlc_data") is False
      and d.get("content_id") == LICENSE_CID, str(d) + err[-300:])
rc, d, err = info_json(os.path.join(FX, "mini.pkg"))
check("info_json_game_content", rc == 0 and d.get("content") == "game" and d.get("dlc_data") is False
      and d.get("dlc") == [], str(d) + err[-300:])
icon_out = os.path.join(TMP, "dlc_icon.png")
if os.path.exists(icon_out):
    os.remove(icon_out)
rc, _, err = run("icon", os.path.join(FX, "dlc_data.pkg"), "-o", icon_out)
check("icon_dlc", rc == 0 and open(icon_out, "rb").read(4) == b"\x89PNG", err[-300:])
rc, so, _ = run("--capabilities")
check("capabilities_dlc", json.loads(so).get("dlc") is True, so)
# a DLC is still no game: analyze refuses its package
rc, _, err = run("analyze", os.path.join(FX, "dlc_data.pkg"), "--list")
check("analyze_dlc_refused", rc == 1 and "with --dlc" in err, err[-300:])

# ---- pack --dlc: the mini game with a DLC with data and a license ----
MINI_PKG = os.path.join(FX, "mini.pkg")
DLC_DATA_SIZE = sum(os.path.getsize(os.path.join(r, f)) for r, _, fs in os.walk(os.path.join(FX, "dlc_data")) for f in fs)
INI = ("[PSAC]\ncontent_id=" + DATA_CID + "\nmount_point=/app0/addcont0\n\n[PSAL]\ncontent_id=" + LICENSE_CID +
       "\ndownload_status=NO_EXTRA_DATA\n").encode()


def fresh(path):
    if os.path.isdir(path):
        rmtree(path)
    elif os.path.exists(path):
        os.remove(path)
    return path


g5 = fresh(os.path.join(TMP, "dlc_game.p5z"))
rc, so, err = run("pack", MINI_PKG, "--dlc", os.path.join(FX, "dlc_data.pkg"), "--dlc", os.path.join(FX, "dlc_license.pkg"),
                  "-o", g5, "--p5z", "--level", "3", "--threads", "2")
check("pack_dlc_p5z", rc == 0 and "  DLC:    1 with data (addcont0/), 1 license only" in so, so[-500:] + err[-300:])
rc, d, err = info_json(g5)
check("info_json_p5z_dlc", rc == 0 and d.get("content") == "game" and d.get("dlc") == [
    {"content_id": DATA_CID, "data": True, "folder": "addcont0", "size": DLC_DATA_SIZE, "files": 6},
    {"content_id": LICENSE_CID, "data": False, "folder": "", "size": 0, "files": 0}], str(d.get("dlc")) + err[-300:])
check("info_json_p5z_dlc_complete", d.get("completeness", {}).get("extra_count") == 0, str(d.get("completeness")))
rc, so, err = run("verify", g5)
check("verify_p5z_dlc", rc == 0, so[-300:] + err[-300:])
orig = fresh(os.path.join(TMP, "dlc_orig"))
rc, _, err = run("unpack", g5, "--original", "-o", orig)
ini_path = os.path.join(orig, "dlc_emu.ini")
check("unpack_dlc_original", rc == 0 and os.path.exists(ini_path) and open(ini_path, "rb").read() == INI
      and os.path.exists(os.path.join(orig, "addcont0", "data", "sub", "dlc2.bin"))
      and not os.path.exists(os.path.join(orig, "addcont1")), err[-300:])
g5j = fresh(os.path.join(TMP, "dlc_game_json.p5z"))
rc, so, err = run("pack", MINI_PKG, "--dlc", os.path.join(FX, "dlc_data"), "--dlc", os.path.join(FX, "dlc_license"),
                  "-o", g5j, "--p5z", "--level", "3", "--threads", "2", "--progress", "json")
res = last_result(json_lines(so))
check("pack_dlc_result", rc == 0 and res.get("ok") is True and res.get("dlc_data") == 1 and res.get("dlc_license") == 1
      and res.get("dlc_bytes") == DLC_DATA_SIZE, str(res) + err[-300:])
# for the console: addcont0/ and the ini in the folder, which verifies
fo = fresh(os.path.join(TMP, "dlc_folder"))
rc, so, err = run("pack", os.path.join(FX, "mini"), "--dlc", os.path.join(FX, "dlc_data.pkg"), "-o", fo, "--level", "3",
                  "--threads", "2")
rc2, so2, err2 = run("verify", fo)
check("pack_dlc_folder", rc == 0 and rc2 == 0 and os.path.exists(os.path.join(fo, "dlc_emu.ini"))
      and os.path.exists(os.path.join(fo, "addcont0", "module", "libdlc.prx")), err[-300:] + so2[-300:] + err2[-300:])
# against the game it was made of: what the DLC added is not in it, and is no failure
rc, so, err = run("verify", fo, "--against", os.path.join(FX, "mini"))
check("verify_dlc_folder_against_game", rc == 0 and " 0 failed" in so + err, so[-400:] + err[-400:])
# what stops it: exit 1 with the reason, the error code for a front end, nothing left
for name, game, dlc, code, words in (
        ("other_game", MINI_PKG, os.path.join(FX, "dlc_other.pkg"), "dlc_refused", "a DLC of another game"),
        ("own_ini", os.path.join(PG, "leftovers"), os.path.join(FX, "dlc_data"), "dlc_has_ini", "already has a dlc_emu.ini"),
        ("a_game", MINI_PKG, MINI_PKG, "dlc_refused", "a game's package, not additional content"),
        ("not_there", MINI_PKG, os.path.join(FX, "no_such_dlc.pkg"), "dlc_refused", "does not exist")):
    out = fresh(os.path.join(TMP, "dlc_bad_" + name + ".p5z"))
    rc, so, err = run("pack", game, "--dlc", dlc, "-o", out, "--p5z", "--threads", "2", "--progress", "json")
    res = last_result(json_lines(so))
    check("pack_dlc_refused_" + name, rc == 1 and words in err and res.get("error") == code and not os.path.exists(out),
          "rc=%d %s %s" % (rc, res, err[-300:]))
rc, _, err = run("pack", MINI_PKG, "-o", os.path.join(TMP, "x.p5z"), "--p5z", "--dlc")
check("pack_dlc_missing_value", rc == 2 and "missing value for: --dlc" in err, err[-300:])
dcopy = fresh(os.path.join(TMP, "dlc_copy"))
shutil.copytree(os.path.join(FX, "dlc_license"), dcopy)
rc, _, err = run("pack", MINI_PKG, "--dlc", dcopy, "-o", os.path.join(dcopy, "out.p5z"), "--p5z")
check("pack_dlc_output_inside", rc == 2 and "the output cannot be a DLC or be inside one" in err
      and not os.path.exists(os.path.join(dcopy, "out.p5z")), err[-300:])
rc, so, _ = run("--help")
check("help_dlc", "--dlc DLC" in so, so[:2000])


# ---- growing a .p5z: add, update, compact, and the journal of an append that stopped -----------------
import struct  # noqa: E402
import zlib  # noqa: E402
GAME = os.path.join(FX, "game")


def version_of(folder, version):
    pj = os.path.join(folder, "sce_sys", "param.json")
    with open(pj, encoding="utf-8") as f:
        d = json.load(f)
    d["contentVersion"] = version
    with open(pj, "w", encoding="utf-8") as f:
        json.dump(d, f)


def game_copy(name, version=None):
    dst = fresh(os.path.join(TMP, name))
    shutil.copytree(GAME, dst)
    if version:
        version_of(dst, version)
    return dst


gp = fresh(os.path.join(TMP, "grow.p5z"))
rc, so, err = run("pack", GAME, "-o", gp, "--p5z", "--threads", "2")
check("grow_pack", rc == 0, err[-300:])
before = os.path.getsize(gp)
v2 = game_copy("grow_v2", "01.001.000")
with open(os.path.join(v2, "data", "new_cli.bin"), "wb") as f:
    f.write(b"a file that version 2 brings " * 10000)
rc, so, err = run("update", gp, v2, "--threads", "2", "--progress", "json")
evs = json_lines(so)
res = last_result(evs)
check("update_cli", rc == 0 and res.get("ok") is True and res.get("new_files") == 1 and res.get("changed_files") == 1
      and res.get("removed_files") == 0 and res.get("grown") == 1 and res.get("p5z_size") == os.path.getsize(gp)
      and res.get("added_bytes") == os.path.getsize(gp) - before and res.get("unused_bytes", 0) > 0
      and res.get("nothing") is False and res.get("par2") is False, str(res) + err[-300:])
check("update_cli_comparing", any(e.get("event") == "progress" and e.get("phase") == "comparing" for e in evs), so[-500:])
rc, so, err = run("info", "--json", gp)
d = json.loads(so or "{}") if rc == 0 else {}
check("info_json_grown", rc == 0 and d.get("grown") == 1 and d.get("unused_bytes", 0) > 0 and d.get("recovered") is False
      and d.get("version") == "01.001.000", so[-300:] + err[-300:])
rc, out, err = run("info", gp)
check("info_text_grown", rc == 0 and "Grown:      1 time" in out and "Unused:" in out, out + err)
rc, out, err = run("update", gp, v2)
check("update_text", rc == 1 and "the version the .p5z has already" in err, out + err[-300:])
rc, so, err = run("update", gp, v2, "--progress", "json")
check("update_same_version", rc == 1 and last_result(json_lines(so)).get("error") == "same_version", so + err[-300:])
other = game_copy("grow_other", "01.002.000")
with open(os.path.join(other, "sce_sys", "param.json"), encoding="utf-8") as f:
    d = json.load(f)
d["titleId"] = "PPSA99999"
with open(os.path.join(other, "sce_sys", "param.json"), "w", encoding="utf-8") as f:
    json.dump(d, f)
rc, so, err = run("update", gp, other, "--progress", "json")
check("update_other_game", rc == 1 and last_result(json_lines(so)).get("error") == "other_game", so + err[-300:])
v3 = game_copy("grow_v3", "01.003.000")
with open(os.path.join(v3, "data", "lots.bin"), "wb") as f:
    f.write(os.urandom(4 << 20))
size_v2 = os.path.getsize(gp)
rc, so, err = run("update", gp, v3, "--progress", "json")
check("update_mostly_changed", rc == 1 and last_result(json_lines(so)).get("error") == "mostly_changed"
      and "--force" in err and os.path.getsize(gp) == size_v2, so + err[-300:])
rc, so, err = run("update", gp, v3, "--force", "--progress", "json")
check("update_force", rc == 0 and last_result(json_lines(so)).get("grown") == 2, so[-300:] + err[-300:])
# add, with repair data next to it: it says they no longer match
with open(gp + ".par2", "wb") as f:
    f.write(b"PAR2\0PKT")
rc, so, err = run("add", gp, "--dlc", os.path.join(FX, "dlc_data"), "--threads", "2", "--progress", "json")
res = last_result(json_lines(so))
check("add_cli", rc == 0 and res.get("dlc_data") == 1 and res.get("dlc_license") == 0 and res.get("grown") == 3
      and res.get("par2") is True and "repair data" in err, str(res) + err[-300:])
os.remove(gp + ".par2")
rc, so, err = run("add", gp, "--dlc", os.path.join(FX, "dlc_data"), "--progress", "json")
check("add_again_refused", rc == 1 and last_result(json_lines(so)).get("error") == "dlc_refused", so + err[-300:])
rc, out, err = run("add", gp, "--dlc", os.path.join(FX, "dlc_license"))
check("add_text", rc == 0 and "1 license only" in out and "the .p5z grew by" in out, out + err[-300:])
rc, so, err = run("info", "--play", gp)
d = json.loads(so or "{}") if rc == 0 else {}
check("info_play_grown", rc == 0 and d.get("index_version") == 2 and d.get("grown") == 4
      and [v["id"] for v in d.get("volumes", [])] == list(range(len(d.get("volumes", []))))
      and len(d.get("volumes", [])) >= 3, so[-400:] + err[-300:])
rc, so, err = run("verify", gp, "--progress", "json")
check("verify_grown", rc == 0 and last_result(json_lines(so)).get("files_bad") == 0, so[-300:] + err[-300:])
back = fresh(os.path.join(TMP, "grow_back"))
rc, _, err = run("unpack", gp, "--folder", "-o", back)
check("unpack_folder_grown", rc == 0 and os.path.isfile(os.path.join(back, "ampr_zstd-000.pak"))
      and not os.path.exists(os.path.join(back, "ampr_zstd-001.pak")), err[-300:])
rc, _, err = run("verify", back, "--against", v3)
check("unpack_folder_grown_verifies", rc == 0, err[-300:])
# compact: a copy without the room the updates left, of version 1
cp = fresh(os.path.join(TMP, "grow_compact.p5z"))
rc, so, err = run("compact", gp, "-o", cp, "--threads", "2", "--progress", "json")
res = last_result(json_lines(so))
rc2, so2, _ = run("info", "--json", cp)
d = json.loads(so2 or "{}") if rc2 == 0 else {}
check("compact_cli", rc == 0 and res.get("p5z_size") == os.path.getsize(cp) and res.get("saved_bytes", 0) > 0
      and d.get("grown") == 0 and d.get("unused_bytes") == 0 and os.path.getsize(cp) < os.path.getsize(gp),
      str(res) + so2[-300:] + err[-300:])
rc, _, err = run("compact", gp, "-o", cp)
check("compact_exists", rc == 2 and "already exists" in err, err[-300:])
rc, _, err = run("compact", gp)
check("compact_needs_o", rc == 2 and "-o" in err, err[-300:])
rc, _, err = run("update", gp)
check("update_needs_dump", rc == 2, err[-300:])
rc, _, err = run("add", gp)
check("add_needs_dlc", rc == 2 and "--dlc" in err, err[-300:])
# an append that stopped (as a power cut leaves it): the next command undoes it
jp = fresh(os.path.join(TMP, "grow_journal.p5z"))
shutil.copyfile(cp, jp)
size = os.path.getsize(jp)
with open(jp, "rb") as f:
    f.seek(size - 32)
    foot = f.read(32)
head = b"P5ZJRNL1" + struct.pack("<IIQ", 1, 64, size) + foot + struct.pack("<I", 0)
with open(jp + ".maxpack-append", "wb") as f:
    f.write(head + struct.pack("<I", zlib.crc32(head) & 0xFFFFFFFF))
with open(jp, "ab") as f:
    f.write(b"half of an append" * 1000)
rc, so, err = run("info", "--json", jp)
d = json.loads(so or "{}") if rc == 0 else {}
check("recover_on_info", rc == 0 and d.get("recovered") is True and "was undone" in err and os.path.getsize(jp) == size
      and not os.path.exists(jp + ".maxpack-append") and open(jp, "rb").read() == open(cp, "rb").read(),
      so[-300:] + err[-300:])
# only a .p5z has a journal of MaxPack: a file named so next to an image is someone else's
img = fresh(os.path.join(TMP, "journal_image.exfat"))
shutil.copyfile(BASIC, img)
with open(img + ".maxpack-append", "wb") as f:
    f.write(b"not a journal of MaxPack")
rc, so, err = run("info", "--json", img)
check("recover_only_p5z", rc == 0 and os.path.exists(img + ".maxpack-append"), err[-300:])
if os.path.exists(img + ".maxpack-append"):
    os.remove(img + ".maxpack-append")
rc, so, _ = run("--capabilities")
check("capabilities_grow", json.loads(so or "{}").get("grow") is True, so)
rc, so, _ = run("--help")
check("help_grow", "maxpack add <game.p5z> --dlc" in so and "maxpack update <game.p5z>" in so
      and "maxpack compact <in.p5z> -o <out.p5z>" in so, so[:3000])

# info --sums: what the index records of the packs, in the lines the console's pack prints (after its
# "# " notes) so that a diff of the two compares them; info --json has the same as archive_id, level and
# volume_sums.
def run_raw(*args):
    p = subprocess.run([EXE, *args], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return p.returncode, p.stdout, p.stderr.decode("utf-8", "replace")


def console_lines(level, archive_id, volumes):
    # crates/maxpack-logic/src/pack.rs lines(), with no notes
    out = "level %d\narchive_id %s\n" % (level, archive_id)
    for v in volumes:
        out += "%s %d %s\n" % (v["name"], v["size"], v["sha256"])
    return out.encode("ascii")


SUMS_RE = re.compile(rb"\Alevel \d+\narchive_id [0-9a-f]{32}\n(ampr_zstd-\d{3}\.pak \d+ [0-9a-f]{64}\n)*\Z")
for name, path in (("grown", gp), ("v1", cp)):
    rc, raw, err = run_raw("info", "--sums", path)
    rcj, so, _ = run("info", "--json", path)
    d = json.loads(so or "{}") if rcj == 0 else {}
    vs = d.get("volume_sums", [])
    check("info_sums_" + name, rc == 0 and SUMS_RE.match(raw) is not None and b"\r" not in raw
          and [v["name"] for v in vs] == ["ampr_zstd-%03d.pak" % i for i in range(len(vs))] and len(vs) >= 1
          and raw == console_lines(d.get("level", -1), d.get("archive_id", ""), vs), repr(raw[:400]) + so[-300:] + err)
    rc, raw2, _ = run_raw("info", path, "--sums")
    check("info_sums_either_order_" + name, rc == 0 and raw2 == raw)
# each volume's SHA-256 is of its bytes after its 64-byte header, as unpack --folder writes it (v1)
v1_back = fresh(os.path.join(TMP, "sums_back"))
rc, _, err = run("unpack", cp, "--folder", "-o", v1_back)
_, so, _ = run("info", "--json", cp)
vs = json.loads(so or "{}").get("volume_sums", [])
same = rc == 0 and len(vs) >= 1
for v in vs:
    with open(os.path.join(v1_back, v["name"]), "rb") as f:
        b = f.read()
    same = same and len(b) == v["size"] and hashlib.sha256(b[64:]).hexdigest() == v["sha256"]
check("info_sums_are_the_volumes", same, err[-300:])
# the archive ID is the one info --play gives the console
_, so, _ = run("info", "--play", gp)
_, sj, _ = run("info", "--json", gp)
check("info_sums_archive_id", json.loads(so or "{}").get("archive_id") == json.loads(sj or "{}").get("archive_id"),
      so[:300] + sj[:300])
# only a .p5z has them
rc, raw, err = run_raw("info", "--sums", BASIC)
check("info_sums_only_p5z", rc != 0 and raw == b"" and ".p5z" in err, err)
_, so, _ = run("info", "--json", BASIC)
d = json.loads(so or "{}")
check("info_json_no_sums_elsewhere", "archive_id" not in d and "volume_sums" not in d and "level" not in d, so[:300])
rc, so, _ = run("--help")
check("help_sums", "maxpack info --sums <game.p5z>" in so, so[:3000])

print("test_cli: %d failures" % len(failures))
sys.exit(1 if failures else 0)
