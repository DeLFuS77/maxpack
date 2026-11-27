#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Command-line tests of packing a game packed already with zstd packs.

Usage: test_import.py <maxpack> <fixtures folder> <temporary folder>

A packed folder (made with pack --folder) is packed again to a .p5z: the one of the dump at that level,
from the folder and from an exFAT image of it alike; with --keep-packs its chunks are kept, at their
level. info --json tells what it is, and what is damaged, missing or not zstd is refused with its code.
"""
import json
import os
import shutil
import subprocess
import sys

EXE, FX, TMP = sys.argv[1], sys.argv[2], sys.argv[3]
BASIC = os.path.join(FX, "basic.exfat")
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import make_exfat  # noqa: E402

failures = []


def run(*args):
    p = subprocess.run([EXE, *args], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return p.returncode, p.stdout.decode("utf-8", "replace"), p.stderr.decode("utf-8", "replace")


def check(name, cond, detail=""):
    print(("ok    " if cond else "FAIL  ") + name + ("" if cond else "  " + detail))
    if not cond:
        failures.append(name)


def at(name):
    return os.path.join(TMP, name)


def fresh(path):
    if os.path.isdir(path):
        shutil.rmtree(path)
    elif os.path.exists(path):
        os.remove(path)
    return path


def result(out):
    for line in reversed(out.splitlines()):
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        if ev.get("event") == "result":
            return ev
    return {}


def same(a, b):
    with open(a, "rb") as x, open(b, "rb") as y:
        return x.read() == y.read()


def pack(src, out, *opts):
    return run("pack", src, "-o", fresh(out), "--threads", "3", "--progress", "json", *opts)


def copy_tree(src, dst):
    fresh(dst)
    shutil.copytree(src, dst)   # copy2: dates kept
    return dst


os.makedirs(TMP, exist_ok=True)
with open(os.path.join(FX, "basic.manifest.tsv"), encoding="utf-8") as f:
    DUMP_SIZE = sum(int(line.split("\t")[0]) for line in f)

# the dump packed: at level 19 and 9 to .p5z, and to a folder at 9 in several volumes
FOLDER = at("folder")
rc, _, err = run("pack", BASIC, "-o", fresh(at("a19.p5z")), "--p5z", "--threads", "3")
check("pack_dump", rc == 0, err)
rc, _, err = run("pack", BASIC, "-o", fresh(at("a9.p5z")), "--p5z", "--level", "9", "--threads", "3")
check("pack_dump_9", rc == 0, err)
rc, _, err = run("pack", BASIC, "-o", fresh(FOLDER), "--folder", "--level", "9", "--volume-limit", "1000000")
check("pack_dump_folder", rc == 0, err)

# info --json of the folder: packed with zstd at 9, and what its game takes unpacked
rc, out, err = run("info", "--json", FOLDER)
try:
    info = json.loads(out)
    check("info_packs", info["packed"] is True and info["packs"] == "zstd" and info["packed_level"] == 9, out)
    check("info_unpacked_size", info["unpacked_size"] == DUMP_SIZE, "%s != %d" % (info.get("unpacked_size"), DUMP_SIZE))
except (ValueError, KeyError) as ex:
    check("info_json", False, repr(ex) + " " + out + err)
rc, out, _ = run("info", "--json", BASIC)
try:
    info = json.loads(out)
    check("info_dump_not_packed", info["packs"] == "" and info["packed_level"] == 0 and info["unpacked_size"] == 0, out)
except (ValueError, KeyError) as ex:
    check("info_dump_json", False, repr(ex) + " " + out)
rc, out, _ = run("--capabilities")
check("capability_import", json.loads(out).get("import") is True, out)

# compressed again: the dump's .p5z, and the result says what it was made from
rc, out, err = pack(FOLDER, at("b19.p5z"), "--p5z")
res = result(out)
check("recompress_ok", rc == 0 and res.get("ok") is True, err)
check("recompress_result", res.get("from_packed") is True and res.get("packed_level") == 9 and res.get("kept_packs") is False, out)
check("recompress_says_it", "made from a packed game" in err and "compressed again" in err, err)
check("recompress_identical", rc == 0 and same(at("a19.p5z"), at("b19.p5z")))
rc, out, err = run("verify", at("b19.p5z"))
check("recompress_verifies", rc == 0, out + err)

# its packs kept: the dump's .p5z at their level, whatever --level says
rc, out, err = pack(FOLDER, at("k9.p5z"), "--p5z", "--keep-packs", "--level", "3")
res = result(out)
check("keep_ok", rc == 0 and res.get("ok") is True, err)
check("keep_result", res.get("from_packed") is True and res.get("packed_level") == 9 and res.get("kept_packs") is True, out)
check("keep_says_it", "made from a packed game" in err and "kept" in err, err)
check("keep_identical", rc == 0 and same(at("a9.p5z"), at("k9.p5z")))
# and to a folder for the console
rc, out, err = pack(FOLDER, at("kfolder"), "--folder", "--keep-packs")
check("keep_folder_ok", rc == 0 and result(out).get("kept_packs") is True, err)
rc, out, err = run("verify", at("kfolder"))
check("keep_folder_verifies", rc == 0, out + err)
# a dump has no packs to keep: --keep-packs changes nothing
rc, out, err = pack(BASIC, at("kdump.p5z"), "--p5z", "--keep-packs")
check("keep_dump", rc == 0 and result(out).get("from_packed") is False and same(at("a19.p5z"), at("kdump.p5z")), err)

# the same folder in an exFAT image (named as ModGames44's .zstd: the engine reads its signature)
files = []
for root, _, names in os.walk(FOLDER):
    for name in names:
        full = os.path.join(root, name)
        rel = os.path.relpath(full, FOLDER).replace(os.sep, "/")
        with open(full, "rb") as f:
            files.append((rel, f.read(), None))
img, _, _ = make_exfat.build(files)
with open(fresh(at("game.zstd")), "wb") as f:
    f.write(img)
rc, out, err = run("info", "--json", at("game.zstd"))
try:
    info = json.loads(out)
    check("image_info", info["kind"] == "exfat" and info["packs"] == "zstd" and info["packed_level"] == 9, out)
except (ValueError, KeyError) as ex:
    check("image_info_json", False, repr(ex) + " " + out + err)
rc, out, err = pack(at("game.zstd"), at("i19.p5z"), "--p5z")
check("image_ok", rc == 0 and result(out).get("from_packed") is True, err)
check("image_identical", rc == 0 and same(at("b19.p5z"), at("i19.p5z")))
rc, out, err = pack(at("game.zstd"), at("i9.p5z"), "--p5z", "--keep-packs")
check("image_keep_identical", rc == 0 and same(at("k9.p5z"), at("i9.p5z")), err)


# what is refused: its code, exit 1, nothing left of the .p5z, the source as it was
def tree_bytes(path):
    got = {}
    for root, _, names in os.walk(path):
        for name in names:
            full = os.path.join(root, name)
            with open(full, "rb") as f:
                got[os.path.relpath(full, path)] = (f.read(), os.stat(full).st_mtime_ns)
    return got


def refused(name, src, code, words):
    before = tree_bytes(src)
    for keep in ([], ["--keep-packs"]):
        rc, out, err = pack(src, at("refused.p5z"), "--p5z", *keep)
        res = result(out)
        tag = name + ("_keep" if keep else "")
        check(tag, rc == 1 and res.get("ok") is False and res.get("error") == code, "%d %s %s" % (rc, out, err))
        check(tag + "_says", words in err, err)
        check(tag + "_nothing_left", not os.path.exists(at("refused.p5z")))
    check(name + "_source_kept", tree_bytes(src) == before)


bad = copy_tree(FOLDER, at("bad_index"))
with open(os.path.join(bad, "ampr_zstd.index"), "r+b") as f:
    f.seek(200)
    b = f.read(1)
    f.seek(200)
    f.write(bytes([b[0] ^ 0x5A]))
refused("damaged_index", bad, "packs_damaged", "damaged")
bad = copy_tree(FOLDER, at("bad_missing"))
os.remove(os.path.join(bad, "eboot.bin"))
refused("missing_file", bad, "packed_file_missing", "eboot.bin")
lz4 = fresh(at("lz4"))
os.makedirs(os.path.join(lz4, "sce_sys"))
with open(os.path.join(lz4, "eboot.bin"), "wb") as f:
    f.write(b"EBOOT")
with open(os.path.join(lz4, "sce_sys", "param.json"), "wb") as f:
    f.write(b'{"titleId":"PPSA01234"}')
with open(os.path.join(lz4, "ampr_assets.index"), "wb") as f:
    f.write(b"AMPRPAK4" + bytes(56))
refused("lz4", lz4, "packs_lz4", "LZ4")
rc, out, _ = run("info", "--json", lz4)
try:
    check("info_lz4", json.loads(out)["packs"] == "lz4", out)
except (ValueError, KeyError) as ex:
    check("info_lz4_json", False, repr(ex) + " " + out)

print("%d failures" % len(failures))
sys.exit(1 if failures else 0)
