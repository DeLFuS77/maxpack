#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Generates the PS5 debug packages (FPKG, .pkg) used by MaxPack's tests (after make_exfat.py).

Usage: make_fpkg.py --out <folder> [--xts <mp_fpkgtool>]

The packages have the layout of pearlxcore/ProsperoPkgTool's builder (MIT), as fthdgn-gh/Alembic
ports it (GPL-3.0-or-later; see docs/source-images.md): an FIH header, the outer PFS (data first,
AES-128-XTS on 64 KiB blocks with keys derived from the content ID and the passcode, S32 inodes and
their indirect blocks) holding pfs_image.dat and naps_pkg_layout.dat, the CNT with its entries
(some AES-CBC encrypted) and the SI. pfs_image.dat is the inner image: the files' payloads stored or
Kraken-compressed (fpkg_kraken.py) in 128 KiB pieces, the NAPS block map, and the metadata (a PFS
of 64 KiB blocks: super-root, inode table, folders, flat path tables) in a PFSv3 container.
Everything is deterministic (fixed seeds and times). No RSA key or other key material is used: the
entries a real builder signs with them are left zero.

Written to <out>:
  basic.pkg, basic_k.pkg  make_exfat's basic game, stored and Kraken: the same basic.manifest.tsv
                          describes them (sce_sys/param.json comes from the CNT)
  basic_x.pkg             Kraken with the parts of the format the reference encoder leaves out
  pass.pkg                a small game made with another passcode (PASSCODE below)
  game/, game.pkg         a game folder and the package a builder makes of it, which adds and leaves
                          out files; game.differ.tsv lists them (what packing each gives differently)
  playgo_leftovers.pkg    make_playgo.py's playgo/leftovers/ (with the table of the original package's
                          files, and what dump tools leave behind), stored
  dumpindex_broken.pkg    its dumpindex/broken/ (no table; a copy that lacks files of its dump tool's
                          list), Kraken
  retail.pkg, patch.pkg, dlc.pkg, ps4.pkg, cnt.pkg, other.pkg   what MaxPack refuses
  *.bad.pkg and bad_fpkg.tsv   damaged and hostile packages, each with the words of its message
  basic.pkg.meta          the regions of basic.pkg the mutation tests flip bytes in
  kraken.vec              Kraken blocks and what they decode to, for test_kraken
"""
import argparse
import importlib.util
import io
import os
import random
import struct
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))


def load(name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, name + ".py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


build = load("fpkg_build")
kraken = build.kraken

PASSCODE = "MaxPackTestPasscode-0123456789ab"   # 32 characters, for pass.pkg


# ---- Kraken vectors ---------------------------------------------------------------------------


def text(n, seed=0):
    out, total, i = [], 0, 0
    while total < n:
        line = b"line %07d: the quick brown fox jumps over the lazy dog %d\n" % (i + seed, (i * 7919) % 100003)
        out.append(line)
        total += len(line)
        i += 1
    return b"".join(out)[:n]


def records(n, seed):
    """Binary records whose fields change slowly: the kind of data delta literals are for."""
    r = random.Random(seed)
    out = bytearray()
    a, b = r.randrange(1 << 16), r.randrange(1 << 30)
    while len(out) < n:
        a = (a + r.randrange(3)) & 0xFFFF
        b = (b + r.randrange(1000)) & 0xFFFFFFFF
        out += struct.pack("<HIHf", a, b, len(out) & 0xFFFF, a / 7.0)
    return bytes(out[:n])


def vectors():
    """(raw, payload, flags, first_chunk) blocks for test_kraken."""
    rng = random.Random(7)
    mixed = bytearray(text(0x14000, 3))
    mixed += b"\xAB" * 0x9000
    mixed += rng.randbytes(0x8000)
    mixed += mixed[:0x13000]
    mixed = bytes(mixed[:0x40000]).ljust(0x40000, b"\x11")
    allbytes = bytes((v if r % 3 == 0 else (v * 7) & 0x3F) for r in range(600) for v in range(256))[:0x25000]
    # every mode on a few inputs, the reference encoder's on the rest
    every = [text(1000), text(0x20001, 3), mixed, records(0x20000, 1), allbytes[:0x8000]]
    plain = [text(9), text(40), text(0x1FFFF, 1), text(0x20000, 2), text(0x40000, 5), bytes(0x40000), bytes(40),
             allbytes, records(0x40000, 2), text(5000, 6) + bytes(70000) + text(5000, 7)]
    modes = [0, kraken.REPEATS, kraken.NO_SCALING, kraken.SUB_LITERALS,
             kraken.REPEATS | kraken.NO_SCALING | kraken.SUB_LITERALS]
    out = []
    for data, ms in [(d, modes) for d in every] + [(d, [0]) for d in plain]:
        for mode in ms:
            e = kraken.encode_block(data, mode)
            if e is not None:
                out.append((data, e[0], e[1], e[2]))
    # bare entropy arrays (no LZ), and a two-sub-chunk block whose second sub-chunk is stored
    for data in (text(5000, 8), allbytes[:0x20000], text(0x20000, 9)):
        h = kraken.bare_array(data)
        if h is not None:
            out.append((data, h, 0, 0))
    first = kraken.encode_block(text(0x20000, 10))
    noise = random.Random(11).randbytes(0x10000)
    out.append((text(0x20000, 10) + noise, first[0] + noise, kraken.CHUNK0_NEWLZ | kraken.CHUNK1_NEWLZ, len(first[0])))
    return out


def write_vectors(path):
    v = vectors()
    with open(path, "wb") as f:
        f.write(b"KVEC" + struct.pack("<I", len(v)))
        for raw, enc, flags, first in v:
            f.write(struct.pack("<4I", len(raw), len(enc), flags, first))
            f.write(raw)
            f.write(enc)


exfat = load("make_exfat")
MTIME = exfat.MTIME_UNIX
BUILD_TIME = 1730000000   # when the builder made game.pkg (its files' dates)
BASIC_CID = "UP0000-TEST00001_00-MAXPACKTEST00000"
GAME_CID = "UP0000-PPSA00000_00-MAXPACKTEST00000"
PLAYGO_CID = "UP0000-PPSA00000_00-MAXPACKPLAYGO000"   # the content ID of make_playgo.py's game
MODES = [0, kraken.REPEATS, kraken.NO_SCALING, kraken.SUB_LITERALS,
         kraken.REPEATS | kraken.NO_SCALING | kraken.SUB_LITERALS]


def basic():
    return [(p, d) for p, d, _ in exfat.basic_files()]


def mini():
    return [("eboot.bin", b"\x7fELF" + random.Random(5).randbytes(3000)),
            ("sce_sys/param.json", b'{"titleId": "PPSA00000", "contentId": "' + GAME_CID.encode() +
             b'", "contentVersion": "01.000.000"}'),
            ("data/a.bin", text(200000)),
            ("data/b.bin", bytes(9000)),
            ("data/sub/c.bin", random.Random(6).randbytes(5000)),
            ("empty.txt", b"")]


def game():
    """A game folder as a dump has it: sce_sys files of the CNT (some protected), of the inner PFS
    (keystone), an ELF and a SELF module, big and small files."""
    r = random.Random(30)
    param = (b'{"contentId": "' + GAME_CID.encode() + b'", "titleId": "PPSA00000", "contentVersion": "01.000.000", '
             b'"applicationDrmType": "standard", "localizedParameters": {"defaultLanguage": "en-US", '
             b'"en-US": {"titleName": "MaxPack Test Game"}}}')
    files = [("eboot.bin", b"\x7fELF" + r.randbytes(200000)),
             ("modules/libgame.prx", b"\x4f\x15\x3d\x1d" + r.randbytes(70000)),
             ("sce_sys/param.json", param),
             ("sce_sys/icon0.png", b"\x89PNG\r\n\x1a\n" + r.randbytes(20000)),
             ("sce_sys/pic0.png", b"\x89PNG\r\n\x1a\n" + r.randbytes(30000)),
             ("sce_sys/keystone", b"keystone\x03\x00" + bytes(22) + r.randbytes(64)),
             ("sce_sys/nptitle.dat", r.randbytes(0x50)),
             ("sce_sys/license.dat", r.randbytes(0x400)),
             ("sce_sys/trophy2/trophy00.ucp", r.randbytes(5000)),
             ("sce_sys/changeinfo/changeinfo.xml", b"<changeinfo><changes app_ver=\"01.000\"/></changeinfo>\n"),
             ("sce_sys/playgo-chunk.dat", b"PLGO" + r.randbytes(500)),
             ("sce_sys/about/legal.txt", text(3000, 8)),
             ("data/big.bin", text(1500000, 9)),
             ("data/noise.bin", r.randbytes(400000)),
             ("data/zeros.bin", bytes(600000)),
             ("data/empty.bin", b"")]
    files += [("small/s%02d.txt" % i, b"small file %d\n" % i * (i + 1)) for i in range(40)]
    return files


def fake_signed(path, data):
    """Whether a builder fake-signs this file (FakeSignInnerModules): eboot.bin and the .elf, .prx and
    .sprx files that are raw ELF; one that is a SELF already is kept as it is."""
    name = path.rsplit("/", 1)[-1].lower()
    return (name == "eboot.bin" or name.endswith((".elf", ".prx", ".sprx"))) and data[:4] == b"\x7fELF"


def as_built(files):
    """What a package builder makes of a dump (ProsperoDebugPackageBuilder.cs): it fake-signs the
    executables that are raw ELF, adds sce_sys/pfs-version.dat, sce_sys/keystone and
    sce_sys/about/right.sprx when they are missing, makes its own playgo-chunk.dat and the
    playgo-hash-table.dat and playgo-ficm.dat that go with it, and a DDS beside each PNG icon or
    picture. The contents here stand in for those (a fake-signed SELF wraps the ELF in a header and
    its segments; the real right.sprx is a fake-signed module of 12,752 bytes; the DDS are BC7
    images). (files, the differences)."""
    have = {p for p, _ in files}
    out, differ = [], []
    for p, d in files:
        if p == "sce_sys/playgo-chunk.dat":
            continue
        if fake_signed(p, d):
            d = b"\x4f\x15\x3d\x1d" + bytes(0x1C) + d   # a SELF header stands in for the real one
            differ.append(("changed", p))
        out.append((p, d))

    def add(path, data, how="added"):
        out.append((path, data))
        differ.append((how, path))

    if "sce_sys/pfs-version.dat" not in have:
        add("sce_sys/pfs-version.dat", b"01.000.000")
    if "sce_sys/keystone" not in have:
        add("sce_sys/keystone", b"keystone\x02\x00" + bytes(0x56))
    if "sce_sys/about/right.sprx" not in have:
        add("sce_sys/about/right.sprx", b"\x54\x14\xf5\xee" + text(12748, 11))
    add("sce_sys/playgo-chunk.dat", b"PLGO" + text(400, 12), "changed" if "sce_sys/playgo-chunk.dat" in have else "added")
    add("sce_sys/playgo-hash-table.dat", text(64, 13))
    add("sce_sys/playgo-ficm.dat", text(96, 14))
    for png in ("icon0", "pic0", "pic1", "pic2"):
        if "sce_sys/%s.png" % png in have and "sce_sys/%s.dds" % png not in have:
            add("sce_sys/%s.dds" % png, b"DDS " + text(5000, 15))
    return out, sorted(differ, key=lambda d: d[1])


def write_folder(folder, files, mtime):
    for path, data in files:
        full = os.path.join(folder, *path.split("/"))
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "wb") as f:
            f.write(data)
        os.utime(full, (mtime, mtime))


def meta_regions(pkg, info):
    """Where to flip bytes: the FIH, the CNT's header, table and names, the outer superblock, inode
    table, indirect blocks, uroot and map, and the inner image's metadata and first payload."""
    o = info["outer"]
    po = 0x10000
    cnt = info["cnt_offset"]
    table = struct.unpack_from(">I", pkg, cnt + 0x18)[0]
    count = struct.unpack_from(">I", pkg, cnt + 0x10)[0]
    reg = [(0, 0x100), (cnt, 0x80), (cnt + table, 32 * count)]
    names_off, names_len = info["cnt"][0x200]
    reg.append((cnt + names_off, names_len))
    reg.append((po + info["sb"] * build.BS, 0x400))
    reg.append((po + o["itab"] * build.BS, 5 * build.S32))
    reg.append((po + o["uroot"] * build.BS, 256))
    reg += [(po + b * build.BS, 36 * 16) for b in o["indirect"]]
    reg.append((po + o["first"][1] * build.BS, len(info["naps"])))
    inner = info["inner"]
    data = po + inner["meta_on_disk"] + inner["data_section"]
    if not inner["blocks"] or not inner["blocks"][0]["flags"]:
        reg.append((data, 0x100))                       # the inner superblock
        reg.append((data + build.BS, 0xA8 * 64))        # the inode table
        reg.append((data + (1 + inner["inode_blocks"]) * build.BS, 4096))   # the super-root and the tables
    else:
        reg.append((data, 4096))
    reg.append((po, 4096))
    return reg


def write_meta(path, regions):
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        for off, n in sorted(regions):
            f.write("%d\t%d\n" % (off, n))


def set_name(meta, where, folder, old, new):
    """Renames old to new (the same length) among folder's entries."""
    assert len(old) == len(new)
    off = where["content"][folder]
    i = meta.index(old.encode(), off)
    meta[i:i + len(old)] = new.encode()


def entry_at(meta, where, folder, name):
    """The offset of name's entry (its 16-byte header) among folder's entries."""
    off = where["content"][folder]
    return meta.index(name.encode(), off) - 16


def damaged(out, w):
    """Damaged and hostile packages, each with words of the message it must give."""
    bad = []
    m = mini()

    def made(name, words, **kw):
        pkg, _ = build.package(m, GAME_CID, **kw)
        w(name, pkg)
        bad.append((name, words))
        return pkg

    pkg, info = build.package(m, GAME_CID)
    w("trunc.bad.pkg", pkg[:len(pkg) * 6 // 10])
    bad.append(("trunc.bad.pkg", "damaged package"))
    f = bytearray(pkg)
    struct.pack_into("<Q", f, 0x18, len(pkg))   # the PFS's size: past the end
    w("fih.bad.pkg", f)
    bad.append(("fih.bad.pkg", "its PFS is out of range"))
    f = bytearray(pkg)
    sb = 0x10000 + info["sb"] * build.BS
    f[sb + 8:sb + 16] = bytes(8)
    w("nosb.bad.pkg", f)
    bad.append(("nosb.bad.pkg", "no superblock"))
    f = bytearray(pkg)
    f[sb + 0x370] ^= 1   # another seed: other keys, and the checksum no longer matches
    w("icv.bad.pkg", f)
    bad.append(("icv.bad.pkg", "checksum does not match"))
    f = bytearray(pkg)
    cnt = info["cnt_offset"]
    struct.pack_into(">I", f, cnt + 0x10, 0x20000)
    w("entries.bad.pkg", f)
    bad.append(("entries.bad.pkg", "entry table"))
    f = bytearray(pkg)
    f[cnt:cnt + 4] = b"XCNT"
    w("cnt.bad.pkg", f)
    bad.append(("cnt.bad.pkg", "no CNT header"))
    f = bytearray(pkg)
    f[cnt + 0x40] = 0x01
    w("cid.bad.pkg", f)
    bad.append(("cid.bad.pkg", "content ID"))

    def big_counts(blob):
        b = bytearray(blob)
        b[8:11] = b"\xff\xff\xff"   # outer blocks
        return bytes(b)

    made("naps.bad.pkg", "shorter than its header", hooks={"naps": big_counts})

    def garble(blob):
        b = bytearray(blob)
        cbi = len(b) - 64
        b[cbi:] = bytes(64)
        return bytes(b)

    made("napsrec.bad.pkg", "block map", hooks={"naps": garble})

    def no_super(meta, where):
        meta[8:16] = bytes(8)

    made("isb.bad.pkg", "no PFS superblock", hooks={"meta": no_super})

    def slash(meta, where):
        set_name(meta, where, "", "eboot.bin", "eboot/bin")

    made("slash.bad.pkg", "invalid name", hooks={"meta": slash})

    def twice(meta, where):
        set_name(meta, where, "data", "a.bin", "b.bin")

    made("twice.bad.pkg", "twice", hooks={"meta": twice})

    def loop(meta, where):
        e = entry_at(meta, where, "data", "sub")
        struct.pack_into("<I", meta, e, where["dirs"][""])

    made("loop.bad.pkg", "a loop", hooks={"meta": loop})

    def outside(meta, where):
        struct.pack_into("<Q", meta, where["inode"](where["files"]["data/a.bin"]) + 0x60, 1 << 60)

    made("outside.bad.pkg", "outside its mount", hooks={"meta": outside})

    def hardlink(meta, where):
        e = entry_at(meta, where, "data", "b.bin")
        struct.pack_into("<I", meta, e, where["files"]["data/a.bin"])

    made("hardlink.bad.pkg", "hard link", hooks={"meta": hardlink})

    def kind(meta, where):
        e = entry_at(meta, where, "data", "a.bin")
        struct.pack_into("<I", meta, e + 4, 7)

    made("type.bad.pkg", "not supported", hooks={"meta": kind})

    def mode(meta, where):
        struct.pack_into("<H", meta, where["inode"](where["files"]["data/a.bin"]), 0x416D)

    made("mode.bad.pkg", "does not match its inode", hooks={"meta": mode})

    def ino(meta, where):
        e = entry_at(meta, where, "data", "a.bin")
        struct.pack_into("<I", meta, e, 0x7FFFFFFF)

    made("ino.bad.pkg", "out of range", hooks={"meta": ino})

    def names(blob):
        return blob.replace(b"param.json", b"../aram.js")

    made("cntname.bad.pkg", "invalid name", hooks={"names": names})

    def far(entries):
        return [(i, n, d, f1 | (0x80000000 if i == 0x2000 else 0), f2 | ((3 << 12) if i == 0x2000 else 0))
                for i, n, d, f1, f2 in entries]

    pkg2, info2 = build.package(m, GAME_CID, hooks={"entries": far})
    f = bytearray(pkg2)
    c2 = info2["cnt_offset"]
    t2 = struct.unpack_from(">I", f, c2 + 0x18)[0]
    for i in range(struct.unpack_from(">I", f, c2 + 0x10)[0]):
        if struct.unpack_from(">I", f, c2 + t2 + 32 * i)[0] == 0x2000:
            struct.pack_into(">I", f, c2 + t2 + 32 * i + 16, 0x7FFFFFF0)
    w("cntout.bad.pkg", f)
    bad.append(("cntout.bad.pkg", "outside it"))
    return bad


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--xts", help="mp_fpkgtool, to encrypt with the engine's AES (faster)")
    args = ap.parse_args()
    out = args.out
    os.makedirs(out, exist_ok=True)
    build.self_test()
    if args.xts:
        build.use_helper(args.xts, out)

    def w(name, data):
        with open(os.path.join(out, name), "wb") as f:
            f.write(data)

    write_vectors(os.path.join(out, "kraken.vec"))

    b = basic()
    pkg, info = build.package(b, BASIC_CID, t_sec=MTIME)
    w("basic.pkg", pkg)
    write_meta(os.path.join(out, "basic.pkg.meta"), meta_regions(pkg, info))
    pkg, info = build.package(b, BASIC_CID, kraken_on=True, t_sec=MTIME)
    w("basic_k.pkg", pkg)
    write_meta(os.path.join(out, "basic_k.pkg.meta"), meta_regions(pkg, info))
    pkg, _ = build.package(b, BASIC_CID, kraken_on=True, t_sec=MTIME, mode_of=lambda i: MODES[i % len(MODES)])
    w("basic_x.pkg", pkg)

    m = mini()
    w("pass.pkg", build.package(m, GAME_CID, passcode=PASSCODE)[0])
    w("plain.pkg", build.package(m, GAME_CID, plain=True)[0])
    w("mini.pkg", build.package(m, GAME_CID)[0])
    write_folder(os.path.join(out, "mini"), m, MTIME)
    retail = bytearray(build.package(m, GAME_CID)[0])
    retail[5] = 0x80
    w("retail.pkg", retail)
    w("dlc.pkg", build.package(m, GAME_CID, content_type=0x21)[0])
    w("other.pkg", build.package(m, GAME_CID, content_type=0x1A)[0])
    w("patch.pkg", b"\x7fLIH" + bytes(0x10000 - 4))
    w("ps4.pkg", b"\x7fCNT" + bytes(0x1000 - 4))
    w("notpkg.pkg", random.Random(9).randbytes(0x20000))
    mk = [(p, d) for p, d in m]
    w("mini_k.pkg", build.package(mk, GAME_CID, kraken_on=True)[0])

    # A Kraken block damaged: the package opens, reading the file it holds fails.
    def flip(image, inner):
        blk = next(x for x in inner["blocks"] if x["flags"])
        b2 = bytearray(image)
        b2[blk["phys"] + blk["stored"] // 2] ^= 0x55
        return bytes(b2)

    w("kraken.bad.pkg", build.package(mk, GAME_CID, kraken_on=True, hooks={"image": flip})[0])

    g = game()
    write_folder(os.path.join(out, "game"), g, MTIME)
    built, differ = as_built(g)
    w("game.pkg", build.package(built, GAME_CID, kraken_on=True, t_sec=BUILD_TIME)[0])
    with open(os.path.join(out, "game.differ.tsv"), "w", encoding="utf-8", newline="\n") as f:
        f.write("# how\tpath (what packing game.pkg gives that packing game/ does not; every date is %d)\n" %
                BUILD_TIME)
        for how, path in differ:
            f.write("%s\t%s\n" % (how, path))

    # Two dumps of make_playgo.py as packages with their files as they are: a package is a source like
    # any other, checked against the table it carries or, without one, against its dump tool's list.
    playgo = load("make_playgo")
    w("playgo_leftovers.pkg", build.package(playgo.leftovers_files(), PLAYGO_CID)[0])
    w("dumpindex_broken.pkg", build.package(playgo.broken_files() + [("ampr_emu.index", playgo.fine_index())],
                                            PLAYGO_CID, kraken_on=True)[0])

    bad = damaged(out, w)
    with open(os.path.join(out, "bad_fpkg.tsv"), "w", encoding="utf-8", newline="\n") as f:
        for name, words in bad:
            f.write("%s\t%s\n" % (name, words))


if __name__ == "__main__":
    main()
