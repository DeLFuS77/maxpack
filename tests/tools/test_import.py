#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Command-line tests of packing a game packed already with zstd packs.

Usage: test_import.py <maxpack> <fixtures folder> <temporary folder> [<mp_fpkgtool>]

A packed folder (made with pack --folder) is packed again to a .p5z: the one of the dump at that level,
from the folder and from an exFAT image of it alike; with --keep-packs its chunks are kept, at their
level. info --json tells what it is, and what is damaged, missing or not zstd is refused with its code. The
same from a PS5 package of the folder, with DLC, and with names that differ only in case beside the real ones.
"""
import importlib.util
import json
import os
import shutil
import subprocess
import sys

EXE, FX, TMP = sys.argv[1], sys.argv[2], sys.argv[3]
FPKGTOOL = sys.argv[4] if len(sys.argv) > 4 else None
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
        for kind, out_name in (("--p5z", "refused.p5z"), ("--folder", "refused_folder")):
            rc, out, err = pack(src, at(out_name), kind, *keep)
            res = result(out)
            tag = name + ("_keep" if keep else "") + ("" if kind == "--p5z" else "_folder")
            check(tag, rc == 1 and res.get("ok") is False and res.get("error") == code, "%d %s %s" % (rc, out, err))
            check(tag + "_says", words in err, err)
            check(tag + "_nothing_left", not os.path.exists(at(out_name)))
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



def image_of(folder, extra=(), last=None):
    """The files of folder (and extra (path, bytes)) as an exFAT image; last goes at its end."""
    got = []
    for root, _, names in os.walk(folder):
        for name in names:
            full = os.path.join(root, name)
            rel = os.path.relpath(full, folder).replace(os.sep, "/")
            with open(full, "rb") as f:
                got.append((rel, f.read(), None))
    got += [(p, d, None) for p, d in extra]
    got.sort(key=lambda x: x[0] == last)
    return make_exfat.build(got)[0]


def compressed_chunk(index):
    """(volume, offset, stored) of a compressed chunk of an ampr_zstd.index's bytes."""
    nchunks, coff = int.from_bytes(index[48:56], "little"), int.from_bytes(index[64:72], "little")
    for k in range(nchunks):
        c = int.from_bytes(index[coff + 8 * k:coff + 8 * k + 8], "little")
        if c >> 62 == 1:
            return (c >> 32) & 0xFFF, c & 0xFFFFFFFF, ((c >> 44) & 0x3FFFF) + 1
    return None


# names that differ only in case beside the real ones (a case-sensitive image can hold both): what the console
# opens is the exact name, as here; the others are left out
with open(os.path.join(FOLDER, "ampr_zstd.index"), "rb") as f:
    index = f.read()
vol, off, stored = compressed_chunk(index)
with open(os.path.join(FOLDER, "ampr_zstd-%03d.pak" % vol), "rb") as f:
    decoy = bytearray(f.read())
decoy[off + stored // 2] ^= 0x5A
with open(fresh(at("decoys.exfat")), "wb") as f:
    f.write(image_of(FOLDER, [("AMPR_ZSTD-%03d.PAK" % vol, bytes(decoy)), ("AMPR_ZSTD.INDEX", b"not an index")]))
rc, out, err = pack(at("decoys.exfat"), at("decoys.p5z"), "--p5z")
check("exact_names", rc == 0 and same(at("b19.p5z"), at("decoys.p5z")), err)

# a read of the source that fails (an image cut short in its last volume) is no damage of the packs
img = image_of(FOLDER, last="ampr_zstd-002.pak")
with open(fresh(at("cut.exfat")), "wb") as f:
    f.write(img[:len(img) - 16 * 4096 - 30000])   # the 16 clusters the builder leaves free at its end, and more
for keep in ([], ["--keep-packs"]):
    rc, out, err = pack(at("cut.exfat"), at("cut.p5z"), "--p5z", *keep)
    res = result(out)
    check("read_error" + ("_keep" if keep else ""), rc == 1 and res.get("error") == "io" and "packs are damaged" not in err,
          "%d %s %s" % (rc, out, err))
    check("read_error_nothing_left" + ("_keep" if keep else ""), not os.path.exists(at("cut.p5z")))


# a PS5 package with the packed folder in it: the same .p5z as the folder
def load(name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(os.path.dirname(os.path.abspath(__file__)), name + ".py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


build = load("fpkg_build")
if FPKGTOOL:
    build.use_helper(FPKGTOOL, TMP)
pkg_files = sorted(((rel, data) for rel, data, _ in files), key=lambda x: x[0])
pkg, _ = build.package(pkg_files, "UP0000-TEST00001_00-MAXPACKTEST00000", t_sec=make_exfat.MTIME_UNIX)
with open(fresh(at("packed.pkg")), "wb") as f:
    f.write(pkg)
rc, out, err = run("info", "--json", at("packed.pkg"))
try:
    info = json.loads(out)
    check("pkg_info", info["kind"] == "fpkg" and info["packs"] == "zstd" and info["packed_level"] == 9, out)
except (ValueError, KeyError) as ex:
    check("pkg_info_json", False, repr(ex) + " " + out + err)
rc, out, err = pack(at("packed.pkg"), at("p19.p5z"), "--p5z")
check("pkg_identical", rc == 0 and result(out).get("from_packed") is True and same(at("a19.p5z"), at("p19.p5z")), err)
rc, out, err = pack(at("packed.pkg"), at("p9.p5z"), "--p5z", "--keep-packs")
check("pkg_keep_identical", rc == 0 and same(at("a9.p5z"), at("p9.p5z")), err)

# with a DLC put inside: what the dump gives with it, compressed again or with the packs kept
MINI, DLC = os.path.join(FX, "mini"), os.path.join(FX, "dlc_data")
rc, _, err = run("pack", MINI, "-o", fresh(at("mini_folder")), "--folder", "--threads", "3")
check("pack_mini_folder", rc == 0, err)
rc, _, err = run("pack", MINI, "-o", fresh(at("mini_dlc.p5z")), "--p5z", "--dlc", DLC, "--threads", "3")
check("pack_mini_dlc", rc == 0, err)
for keep in ([], ["--keep-packs"]):
    rc, out, err = pack(at("mini_folder"), at("mini_dlc_again.p5z"), "--p5z", "--dlc", DLC, *keep)
    res = result(out)
    check("dlc_identical" + ("_keep" if keep else ""), rc == 0 and res.get("dlc_data") == 1 and res.get("from_packed") is True
          and same(at("mini_dlc.p5z"), at("mini_dlc_again.p5z")), err)

# a DLC packed already is a DLC, not a game: info --json says so, and pack refuses it (pack --dlc refuses it too)
rc, _, err = run("pack", DLC, "-o", fresh(at("dlc_packed")), "--folder")
check("pack_dlc_folder", rc == 0, err)
rc, out, err = run("info", "--json", at("dlc_packed"))
try:
    info = json.loads(out)
    check("dlc_packed_info", info["content"] == "dlc" and info["packs"] == "zstd" and info["title_id"] == "PPSA00000"
          and info["dlc_data"] is True, out)
except (ValueError, KeyError) as ex:
    check("dlc_packed_info_json", False, repr(ex) + " " + out + err)
rc, out, err = pack(at("dlc_packed"), at("dlc_packed.p5z"), "--p5z")
check("dlc_packed_refused", rc == 1 and result(out).get("error") == "dlc_packed" and "DLC" in err
      and not os.path.exists(at("dlc_packed.p5z")), "%d %s %s" % (rc, out, err))

# a packed game that carries the list of the original package's files: as complete in an image as in a folder,
# as its dump is (its packed files are counted, the packs are not)
COMPLETE = os.path.join(FX, "playgo", "complete")
rc, _, err = run("pack", COMPLETE, "-o", fresh(at("complete_packed")), "--folder", "--volume-limit", "1000000")
check("pack_complete_folder", rc == 0, err)
with open(fresh(at("complete_packed.exfat")), "wb") as f:
    f.write(image_of(at("complete_packed")))
KEYS = ("checkable", "reason", "source", "listed", "present", "missing", "extra_count", "leftover_count")
rc, out, _ = run("info", "--json", COMPLETE)
dump = {k: json.loads(out)["completeness"][k] for k in KEYS}
check("complete_dump", dump["checkable"] is True and dump["missing"] == 0, str(dump))
for name in ("complete_packed", "complete_packed.exfat"):
    rc, out, err = run("info", "--json", at(name))
    try:
        got = {k: json.loads(out)["completeness"][k] for k in KEYS}
        check("completeness_" + name, got == dump, "%s != %s" % (got, dump))
    except (ValueError, KeyError) as ex:
        check("completeness_" + name + "_json", False, repr(ex) + " " + out + err)

# an ampr_zstd.index of more than 512 MiB (here one an image says it is: its data is not there) is refused before it
# is read, as the console's runtime refuses it
files_of = [(rel, data, None) for rel, data, _ in files]
with open(fresh(at("big_index.exfat")), "wb") as f:
    f.write(make_exfat.build(files_of, overrides={"ampr_zstd.index": 513 << 20}, extra_clusters=140000)[0])
rc, out, err = pack(at("big_index.exfat"), at("big_index.p5z"), "--p5z")
check("index_too_large", rc == 1 and result(out).get("error") == "packs_damaged" and "too large" in err, "%d %s %s" % (rc, out, err))

# a level outside 1..22 in a broken index's header is no level
bad = copy_tree(FOLDER, at("bad_level"))
with open(os.path.join(bad, "ampr_zstd.index"), "r+b") as f:
    f.seek(17)
    f.write(bytes([200]))
rc, out, _ = run("info", "--json", bad)
try:
    check("no_level", json.loads(out)["packed_level"] == 0, out)
except (ValueError, KeyError) as ex:
    check("no_level_json", False, repr(ex) + " " + out)

print("%d failures" % len(failures))
sys.exit(1 if failures else 0)
