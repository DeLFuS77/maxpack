#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Generates the dumps that carry a list of the original package's files, for the completeness tests.

Usage: make_playgo.py --out <folder>
       make_playgo.py --hash <path>...      prints the flat path table hash of each path (hex)

A PS5 package lists the files of its image in sce_sys/playgo-hash-table.dat: a 0x38-byte header
("\\x7fFLT" at 0x18, the count at 0x24) and one 64-bit hash per path, sorted. The hash is the one of
the PS5 flat path table: a three-lane sponge over the path's bytes, ASCII letters in upper case,
without the leading '/'. The hash is a port of FltPathHash, and the header of the table follows
ProsperoPlayGoHashTableBuilder, both in ProsperoPkgTool by pearlxcore (MIT License, Copyright (c)
2026 pearlxcore; https://github.com/pearlxcore/ProsperoPkgTool). The two hashes of real packages that
ps5upload's flt.rs tests (GPL-3.0-or-later) check it.

Writes in <out>/playgo/: complete/ (every file its table lists), missing/ (three of them gone, one of
them sce_sys/about/right.sprx, which is never counted), leftovers/ (complete, plus what dump tools
leave behind, OS junk and a file of the dump's own), notable/ (no table, with a decrypted/ folder),
badtable/ (a truncated table), alien/ (a playgo-hash-table.dat of another format), and missing.ffpkg
(missing/ as a UFS2 image).

And in <out>/dumpindex/, dumps without that table but with the list the dump tool wrote,
ampr_emu.index (AMPRIDX3, written with tools/ampr_ref.py): ok/ (every file it lists, with the
differences a fine dump shows: an .nfo it lists that is not there and one it does not list,
fakelib/ changed, Unreal's Saved/Config written at run time), broken/ (two files gone and one cut
short), foreign/ (the list of another game), damaged/ (a cut list), packed/ (its files in AMPRPAK4
packs), both/ (a complete table and a list that disagrees: the table wins) and fallback/ (a table of
another format, and the list). Everything is deterministic.
"""
import argparse
import importlib.util
import os
import random
import struct
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
M64 = (1 << 64) - 1
SEED0 = 0x92CA8AAB26A24F51
SEED1 = 0x09BBB761A41BC44D
ROUND = 0x8000000080008081
HEADER = 0x38
TABLE = "sce_sys/playgo-hash-table.dat"


def rotl(x, n):
    n &= 63
    return ((x << n) | (x >> (64 - n))) & M64 if n else x


def rotr(x, n):
    return rotl(x, 64 - n)


def flt_hash(path):
    """The flat path table hash of a path (str as UTF-8, or bytes)."""
    b = path.encode("utf-8") if isinstance(path, str) else bytes(path)
    b = b.lstrip(b"/")
    b = bytes(c - 32 if 0x61 <= c <= 0x7A else c for c in b)
    a, bb, c = SEED0, rotl(SEED0, 11), rotl(SEED0, 23)
    tail = 0
    if b:
        pos = 0
        for _ in range((len(b) - 1) >> 3):
            a ^= struct.unpack_from("<Q", b, pos)[0]
            pos += 8
            t18 = rotr(rotl(c ^ bb, 5) ^ a, 11)
            t12 = rotl(rotl(c ^ a, 17) ^ bb, 11)
            c2 = rotr(rotl(bb ^ a, 1) ^ c, 5)
            a = (~t12 & M64 & c2) ^ t18 ^ ROUND
            bb = (~c2 & M64 & t18) ^ t12
            c = (~t18 & M64 & t12) ^ c2
        for j, ch in enumerate(b[pos:]):
            tail |= ch << (8 * j)
    x16, x11, x6 = bb, c, tail ^ a ^ SEED1
    u17 = rotl(x11 ^ x16, 5) ^ x6
    u18 = rotl(x11 ^ x6, 17) ^ x16
    x11b = rotl(x16 ^ x6, 1) ^ x11
    return (~rotl(u18, 11) & M64 & rotr(x11b, 5)) ^ rotr(u17, 11) ^ ROUND


def table(paths):
    """playgo-hash-table.dat listing these paths."""
    hashes = sorted({flt_hash(p) for p in paths})
    n = len(hashes)
    d = bytearray(HEADER + 8 * n)
    struct.pack_into("<IIII", d, 0, 1, 0x08000000, HEADER, 8 * n)
    d[0x18:0x1C] = b"\x7fFLT"
    struct.pack_into("<IQQ", d, 0x24, n, SEED0, SEED1)
    for i, h in enumerate(hashes):
        struct.pack_into("<Q", d, HEADER + 8 * i, h)
    return bytes(d)


def load(name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, name + ".py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def text(n, seed):
    rng = random.Random(seed)
    out = b""
    while len(out) < n:
        out += b"line %06d of the test game %d\n" % (len(out), rng.randint(0, 999999))
    return out[:n]


# The game: what its image holds (and so its table lists), with paths of UTF-8, deep folders, an
# image file in sce_sys/ and right.sprx; and the package's own entries in sce_sys/, which a dump
# carries but the image's list does not.
IMAGE = [
    ("eboot.bin", random.Random(31).randbytes(6000)),
    ("sce_module/libc.prx", random.Random(32).randbytes(3000)),
    ("sce_sys/about/right.sprx", random.Random(33).randbytes(500)),
    ("sce_sys/icon0.png", random.Random(34).randbytes(700)),
    ("data/a.bin", text(30000, 35)),
    ("data/sub/b.bin", bytes(9000)),
    ("data/Café ñandú.txt", text(2000, 36)),
    ("Media/Deep/folders/one/two/three/four/five/six/seven/eight.bin", text(5000, 37)),
    ("data/empty.dat", b""),
]
ENTRIES = [
    ("sce_sys/param.json", b'{"titleId": "PPSA00000", "contentId": "UP0000-PPSA00000_00-MAXPACKPLAYGO000", '
                           b'"contentVersion": "01.000.000", "titleName": "PlayGo Test"}'),
    ("sce_sys/keystone", random.Random(38).randbytes(96)),
    ("sce_sys/playgo-chunk.dat", random.Random(39).randbytes(400)),
]
GONE = ["data/sub/b.bin", "sce_module/libc.prx", "sce_sys/about/right.sprx"]
LEFTOVERS = [
    ("decrypted/eboot.bin", random.Random(40).randbytes(7000)),
    ("decrypted/sce_module/libc.prx", random.Random(41).randbytes(4000)),
    ("eboot.bin.auth_info", random.Random(42).randbytes(64)),
]
JUNK = [("Thumbs.db", b"x" * 50), ("data/.DS_Store", b"y" * 60), ("._eboot.bin", b"z" * 70)]
OWN = [("dlc_emu.ini", b"[dlc]\n" * 20)]


# The dump tool's list: every file it wrote, with its size (and a date, never compared: copies change
# it). ok/ is a fine dump as real ones are.
SAVED = "Media/Game/Saved/Config/PS5/Engine.ini"
FINE = IMAGE + ENTRIES + [("decrypted/eboot.bin", random.Random(43).randbytes(7000)),
                          ("fakelib/libScePlayGo.sprx", random.Random(44).randbytes(900)),
                          (SAVED, b"[Core]\n" * 9)]
LISTED_ONLY = [("Notes/info.nfo", 4398)]
CHANGED = {"fakelib/libScePlayGo.sprx": 1200, SAVED: 20}
UNLISTED = [("Notes/other.nfo", b"nfo" * 40), ("Media/Game/Saved/Config/PS5/GameUserSettings.ini", b"x")]


def dump_index(files, listed_only=(), changed=None):
    """ampr_emu.index of these files as the dump tool wrote them (sizes in changed instead)."""
    ref = load_tool("ampr_ref")
    changed = changed or {}
    rows = [(changed.get(path, len(data)), 1714979290, "/app0/" + path) for path, data in files]
    rows += [(size, 1714979290, "/app0/" + path) for path, size in listed_only]
    return ref.idx3_build(rows)


def load_tool(name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, "..", "..", "tools", name + ".py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def write_folder(folder, files):
    for path, data in files:
        full = os.path.join(folder, *path.split("/"))
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "wb") as f:
            f.write(data)
        os.utime(full, (1714979290, 1714979290))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out")
    ap.add_argument("--hash", nargs="+")
    args = ap.parse_args()
    if args.hash:
        for p in args.hash:
            print("%016x %s" % (flt_hash(p), p))
        return
    if not args.out:
        ap.error("--out or --hash")
    out = os.path.join(args.out, "playgo")
    os.makedirs(out, exist_ok=True)
    listed = table(p for p, _ in IMAGE)
    game = IMAGE + ENTRIES + [(TABLE, listed)]
    missing = [(p, d) for p, d in game if p not in GONE]
    write_folder(os.path.join(out, "complete"), game)
    write_folder(os.path.join(out, "missing"), missing)
    write_folder(os.path.join(out, "leftovers"), game + LEFTOVERS + JUNK + OWN)
    write_folder(os.path.join(out, "notable"), IMAGE + ENTRIES + LEFTOVERS[:1])
    write_folder(os.path.join(out, "badtable"), IMAGE + ENTRIES + [(TABLE, listed[:-4])])
    write_folder(os.path.join(out, "alien"), IMAGE + ENTRIES + [(TABLE, struct.pack("<IIII", 1, 0, 16, 4) + b"\x00\x01\x00\x02")])
    ufs2 = load("make_ufs2")
    with open(os.path.join(out, "missing.ffpkg"), "wb") as f:
        f.write(ufs2.build(missing)[0])

    di = os.path.join(args.out, "dumpindex")
    os.makedirs(di, exist_ok=True)
    index = dump_index(FINE, LISTED_ONLY, CHANGED)
    write_folder(os.path.join(di, "ok"), FINE + UNLISTED + [("ampr_emu.index", index)])
    gone = ("data/sub/b.bin", "sce_module/libc.prx")
    broken = [(p, d[:len(d) // 2] if p == "data/a.bin" else d) for p, d in FINE if p not in gone]
    write_folder(os.path.join(di, "broken"), broken + [("ampr_emu.index", index)])
    other = [("other/%02d.bin" % i, bytes(10 + i)) for i in range(12)]
    write_folder(os.path.join(di, "foreign"), FINE + [("ampr_emu.index", dump_index(other))])
    write_folder(os.path.join(di, "damaged"), FINE + [("ampr_emu.index", index[:100])])
    write_folder(os.path.join(di, "packed"), FINE + [("ampr_emu.index", index), ("ampr_assets.index", b"AMPRPAK4" + bytes(56))])
    lacking = dump_index(FINE + [("data/never.bin", b"z" * 10)])
    write_folder(os.path.join(di, "both"), FINE + [(TABLE, listed), ("ampr_emu.index", lacking)])
    write_folder(os.path.join(di, "fallback"), broken + [(TABLE, b"{}" * 40), ("ampr_emu.index", index)])
    print("playgo: %d files listed, 6 dumps and a UFS2 image in %s" % (len(IMAGE), out), file=sys.stderr)


if __name__ == "__main__":
    main()
