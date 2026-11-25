#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Generates the UFS2, PFS and PFSC images used by MaxPack's tests (after make_exfat.py).

Usage: make_images.py --out <folder> [--data <tests/data>]

The basic game of make_exfat.py in every format, with the same files, contents and dates, so the
same basic.manifest.tsv describes them all: basic.ffpkg (UFS2 of 4 KiB blocks and 1 KiB fragments,
two cylinder groups, holes, a fragmented file, indirect blocks), profile.ffpkg (ShadowMountPlus's
64 KiB profile), basic.ffpfs (PFS), basic_c.ffpfs (PFS with each file PFSC-compressed, MkPFS's
--raw), and the containers basic.exfat.ffpfsc, basic.ffpkg.ffpfsc, basic.ffpfs.ffpfsc (PFS inside,
MkPFS's two passes), basic_raw.exfat.ffpfsc (not compressed) and nest3.ffpfsc (three deep). Then
indirect.ffpkg (double and triple indirect blocks), damaged and hostile images (*.bad.* and the
like, each with the message expected in bad.tsv), the metadata regions of each basic image for the
mutation tests (<image>.meta: offset and length per line), and the real-tool images of tests/data
decompressed (with their .meta, and the game they were made from in tool_game/). Everything is
deterministic.
"""
import argparse
import gzip
import importlib.util
import os
import random
import struct
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))


def load(name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, name + ".py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


exfat = load("make_exfat")
ufs2 = load("make_ufs2")
pfs = load("make_pfs")
MTIME = exfat.MTIME_UNIX


def text(n):
    """Lines of text (make_exfat's big_text, in linear time)."""
    out, total, i = [], 0, 0
    while total < n:
        line = b"line %07d: the quick brown fox jumps over the lazy dog %d\n" % (i, (i * 7919) % 100003)
        out.append(line)
        total += len(line)
        i += 1
    return b"".join(out)[:n]


def basic():
    # make_exfat's (path, data, vdl): beyond vdl the content is zeros, so it is the data itself here.
    return [(p, d) for p, d, _ in exfat.basic_files()]


def mini():
    return [("eboot.bin", random.Random(5).randbytes(3000)),
            ("sce_sys/param.json", b'{"titleId": "PPSA00000", "contentVersion": "01.000.000"}'),
            ("data/a.bin", text(20000)),
            ("data/b.bin", bytes(9000)),
            ("data/sub/c.bin", random.Random(6).randbytes(5000)),
            ("empty.txt", b"")]


def tool_game():
    """The small game the images of tests/data were made from (see tests/data/README.md): ASCII
    names (PFS takes no others), deep folders, an empty file, text, noise, zeros, and files of
    more than 768 KiB (UFS2's direct blocks at 64 KiB)."""
    files = [("eboot.bin", random.Random(21).randbytes(20000)),
             ("sce_sys/param.json", b'{"titleId": "BREW00000", "contentId": "UP0000-BREW00000_00-MAXPACKTOOLTEST0", '
                                    b'"contentVersion": "01.000.000", "titleName": "Tool Test"}'),
             ("sce_sys/icon0.png", random.Random(22).randbytes(3000)),
             ("data/text.txt", text(800000)),
             ("data/zeros.bin", bytes(900000)),
             ("data/noise.bin", random.Random(23).randbytes(30000)),
             ("data/empty.dat", b""),
             ("a/b/c/d/e/deep.txt", b"deep\n" * 1000)]
    files += [("many/item%02d.json" % i, b'{"item": %d, "name": "thing %d"}\n' % (i, i) * (i + 1)) for i in range(30)]
    return files


def write_folder(folder, files, mtime=MTIME):
    for path, data in files:
        full = os.path.join(folder, *path.split("/"))
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "wb") as f:
            f.write(data)
        os.utime(full, (mtime, mtime))


def ufs2_basic(files, **kw):
    holes = {"zeros.bin": set(range(127)), "data/vdl.bin": {2, 3}}
    items = [(p, d, {"holes": holes.get(p, ())}) for p, d in files]
    return ufs2.build(items, interleave=[("data/frag.bin", "data/vdl.bin")], **kw)


def meta_ufs2(img, info):
    """Superblock, every inode, the folders' blocks."""
    reg = [(info["sb"], 1376)]
    reg += [(off, 256) for off in info["inode_off"].values()]
    reg += [(off, 512) for off in info["dir_off"].values()]
    reg += [(off, info["bsize"]) for off in info["ind_off"]]
    return reg


def meta_pfs(info):
    reg = [(0, 0x400), (info["sroot_off"], 64)]
    reg += [(off, 0xA8) for off in info["inode_off"].values()]
    reg += [(off, 256) for off in info["dir_off"].values()]
    return reg


def meta_of_pfs(img):
    """The metadata of any unsigned PFS image, read from it: header, inode table, folders' first
    bytes, and the PFSC header and block table of compressed files."""
    bs = struct.unpack_from("<I", img, 0x20)[0]
    ninodes, _, iblocks = struct.unpack_from("<qqq", img, 0x30)
    reg = [(0, 0x400), (bs, iblocks * bs)]
    ipb = bs // 0xA8
    for ino in range(ninodes):
        off = bs + (ino // ipb) * bs + (ino % ipb) * 0xA8
        mode, _, flags, size = struct.unpack_from("<HHIq", img, off)
        db0 = struct.unpack_from("<i", img, off + 0x64)[0]
        if db0 <= 0 or db0 * bs >= len(img):
            continue
        if mode & 0x4000:
            reg.append((db0 * bs, min(512, size)))
        elif flags & 1 and size >= 0x408:
            n = struct.unpack_from("<Q", img, db0 * bs + 0x28)[0] // 0x10000
            reg += [(db0 * bs, 0x30), (db0 * bs + 0x400, 8 * (n + 1))]
    return reg


def write_meta(path, regions):
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        for off, n in sorted(regions):
            f.write("%d\t%d\n" % (off, n))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--data", default=os.path.join(HERE, "..", "data"))
    ap.add_argument("--tool-game", action="store_true",
                    help="only write the game of tests/data's images in <out>/game and its UFS2 image <out>/BREW00000.ffpkg")
    args = ap.parse_args()
    out = args.out
    os.makedirs(out, exist_ok=True)
    if args.tool_game:
        write_folder(os.path.join(out, "game"), tool_game())
        with open(os.path.join(out, "BREW00000.ffpkg"), "wb") as f:
            f.write(ufs2.build([(p, d) for p, d in tool_game()], bsize=65536, fsize=65536)[0])
        return
    # The same game as a folder, for the tests that pack the real tools' images and compare.
    write_folder(os.path.join(out, "tool_game"), tool_game())
    bad = []   # (file, expected words in the message)

    def w(name, data):
        with open(os.path.join(out, name), "wb") as f:
            f.write(data)

    files = basic()
    exfat_img, _, _ = exfat.build(exfat.basic_files())
    img, info = ufs2_basic(files)
    w("basic.ffpkg", img)
    write_meta(os.path.join(out, "basic.ffpkg.meta"), meta_ufs2(img, info))
    ufs_img = img
    w("profile.ffpkg", ufs2.build(files, bsize=65536, fsize=65536)[0])

    pimg, pinfo = pfs.build(files)
    w("basic.ffpfs", pimg)
    write_meta(os.path.join(out, "basic.ffpfs.meta"), meta_pfs(pinfo))
    cimg, cinfo = pfs.build(files, compress=True)
    w("basic_c.ffpfs", cimg)
    write_meta(os.path.join(out, "basic_c.ffpfs.meta"), meta_pfs(cinfo) +
               [(off, 0x30) for p, off in cinfo["data_off"].items()])

    def container(name, payload, compress=True):
        im, inf = pfs.build_single(name, payload, compress=compress)
        off = inf["data_off"][name]
        n = (len(payload) + 0xFFFF) // 0x10000
        return im, meta_pfs(inf) + [(off, 0x30), (off + 0x400, 8 * (n + 1))]

    for name, payload, inner in (("basic.exfat.ffpfsc", exfat_img, "PPSA00000.exfat"),
                                 ("basic.ffpkg.ffpfsc", ufs_img, "PPSA00000.ffpkg"),
                                 ("basic.ffpfs.ffpfsc", pimg, "PPSA00000.dat")):
        im, reg = container(inner, payload)
        w(name, im)
        write_meta(os.path.join(out, name + ".meta"), reg)
    w("basic_raw.exfat.ffpfsc", container("PPSA00000.exfat", exfat_img, compress=False)[0])
    two = container("inner.ffpfsc", container("PPSA00000.exfat", exfat_img)[0], compress=False)[0]
    w("nest3.ffpfsc", two)
    w("nest4.ffpfsc", container("inner.ffpfsc", two)[0])
    bad.append(("nest4.ffpfsc", "more than 3 deep"))

    # Double and triple indirect blocks (4 KiB blocks: 12 direct, 512 per indirect block). The
    # triple one is sparse: a volume that declares 2 GiB of room in an image of a few MiB.
    nb = 12 + 512 + 512 * 512 + 3
    sparse = {0: b"first", 12: b"single", 600: b"double", nb - 3: b"triple", nb - 1: b"last"}
    big = text(3 << 20)
    items = [("double.bin", big), ("small.bin", b"tail")]
    # The sparse file: only the blocks in `sparse` are written; the rest are holes.
    payload = ufs2.Sparse(nb * 4096 - 100, {lbn * 4096: txt for lbn, txt in sparse.items()})
    holes = set(range(nb)) - set(sparse)
    items.append(("triple.bin", payload, {"holes": holes}))
    img, _ = ufs2.build(items, bsize=4096, fsize=4096, ncg=1, extra_frags=(1 << 31) // 4096, trim=True)
    w("indirect.ffpkg", img)
    with open(os.path.join(out, "indirect.tsv"), "w", encoding="utf-8", newline="\n") as f:
        f.write("double.bin\t%d\t%08x\n" % (len(big), zlib.crc32(big)))
        f.write("triple.bin\t%d\t-\n" % len(payload))
        for lbn, txt in sparse.items():
            f.write("at\t%d\t%s\n" % (lbn * 4096, txt.decode()))

    # ---- damaged and hostile images (on the small game) ----------------------------------
    m = mini()

    def u_hook(fn):
        return ufs2.build(m, hook=fn)[0]

    def entry(path, ino, typ, name):
        def fn(root, nodes):
            (nodes[path] if path else root).extra.append((ino(root, nodes), typ, name))
        return fn

    def patch(img, off, fmt, *vals):
        b = bytearray(img)
        struct.pack_into(fmt, b, off, *vals)
        return bytes(b)

    w("loop.bad.ffpkg", u_hook(entry("data/sub", lambda r, n: n["data"].ino, ufs2.DT_DIR, b"again")))
    bad.append(("loop.bad.ffpkg", "reached twice"))
    w("hardlink.bad.ffpkg", u_hook(entry("data", lambda r, n: n["eboot.bin"].ino, ufs2.DT_REG, b"twin.bin")))
    bad.append(("hardlink.bad.ffpkg", "hard link"))
    w("badino.bad.ffpkg", u_hook(entry("", lambda r, n: 999999, ufs2.DT_REG, b"lost.bin")))
    bad.append(("badino.bad.ffpkg", "out of range"))
    def dup_u(root, nodes):
        nodes["data/b.bin"].name = "a.bin"
    w("dup.bad.ffpkg", u_hook(dup_u))
    bad.append(("dup.bad.ffpkg", "is twice"))
    for tag, name, why in (("slash", b"a/b", "invalid name"), ("bslash", b"a\\b", "invalid name"),
                           ("nul", b"a\0b", "invalid name"), ("utf8", b"\xff\xfe.bin", "invalid name")):
        def mk(nm):
            def fn(root, nodes):
                nodes["data/a.bin"].name = nm
            return fn
        w("name_%s.bad.ffpkg" % tag, u_hook(mk(name)))
        bad.append(("name_%s.bad.ffpkg" % tag, why))
    for kind, why in (("link", "symbolic link"), ("chr", "device"), ("fifo", "device")):
        w("%s.bad.ffpkg" % kind, ufs2.build(m + [("special", b"target" if kind == "link" else b"",
                                                  {"kind": kind})])[0])
        bad.append(("%s.bad.ffpkg" % kind, why))
    img, inf = ufs2.build(m)
    w("hugesize.bad.ffpkg", patch(img, inf["inode_off"]["data/a.bin"] + 16, "<Q", 1 << 63))
    bad.append(("hugesize.bad.ffpkg", "larger than the volume"))
    w("badblock.bad.ffpkg", patch(img, inf["inode_off"]["data/a.bin"] + 112, "<Q", 1 << 40))
    bad.append(("badblock.bad.ffpkg", "out of range"))
    w("crosslink.bad.ffpkg", patch(img, inf["inode_off"]["data/sub/c.bin"] + 112, "<Q",
                                   struct.unpack_from("<Q", img, inf["inode_off"]["eboot.bin"] + 112)[0]))
    bad.append(("crosslink.bad.ffpkg", "same blocks"))
    # An indirect block whose pointers all point back at itself, in a volume that declares 64 MiB
    # in an image of a few hundred KiB, under a 32 MiB file: the walk would read it over and over.
    sim, sinf = ufs2.build(m, ncg=1, extra_frags=(64 << 20) // 1024, trim=True)
    x = sinf["nodes"]["data/b.bin"].blocks[0][0]
    b = bytearray(patch(sim, sinf["inode_off"]["data/a.bin"] + 16, "<Q", 32 << 20))
    struct.pack_into("<3Q", b, sinf["inode_off"]["data/a.bin"] + 208, x, x, x)
    struct.pack_into("<%dQ" % (sinf["bsize"] // 8), b, x * sinf["fsize"], *([x] * (sinf["bsize"] // 8)))
    w("selfref.bad.ffpkg", bytes(b))
    bad.append(("selfref.bad.ffpkg", "more blocks than the image has"))
    # The same pointers in a full image: blocks used twice.
    b = bytearray(patch(img, inf["inode_off"]["data/a.bin"] + 16, "<Q", 2 * inf["fpg"] * inf["fsize"]))
    x = inf["nodes"]["data/b.bin"].blocks[0][0]
    struct.pack_into("<3Q", b, inf["inode_off"]["data/a.bin"] + 208, x, 0, 0)
    struct.pack_into("<%dQ" % (inf["bsize"] // 8), b, x * inf["fsize"], *([x] * (inf["bsize"] // 8)))
    w("selfref2.bad.ffpkg", bytes(b))
    bad.append(("selfref2.bad.ffpkg", "same blocks"))
    w("ufs1.bad.ffpkg", patch(img, 65536 + 1372, "<I", 0x011954))
    bad.append(("ufs1.bad.ffpkg", "UFS1"))
    w("truncated.bad.ffpkg", img[:70000])
    bad.append(("truncated.bad.ffpkg", "truncated"))
    deep = [("/".join(["d"] * 70) + "/x.bin", b"x")]
    w("deep.bad.ffpkg", ufs2.build(deep)[0])
    bad.append(("deep.bad.ffpkg", "too many folder levels"))
    w("noext.bad", b"\x01" * 100000)
    bad.append(("noext.bad", "unrecognised format"))
    w("notufs.bad.ffpkg", b"\x01" * 100000)
    bad.append(("notufs.bad.ffpkg", "not a UFS2 image"))

    def p_hook(fn):
        return pfs.build(m, hook=fn)[0]

    def pentry(path, ino, typ, name):
        def fn(dirs, files):
            dirs[path].extra.append((ino(dirs, files), typ, name))
        return fn

    fmap = lambda files: {f.path: f for f in files}
    w("loop.bad.ffpfs", p_hook(pentry("data/sub", lambda d, f: d["data"].ino, pfs.DT_DIR, b"again")))
    bad.append(("loop.bad.ffpfs", "reached twice"))
    w("hardlink.bad.ffpfs", p_hook(pentry("data", lambda d, f: fmap(f)["eboot.bin"].ino, pfs.DT_FILE, b"twin")))
    bad.append(("hardlink.bad.ffpfs", "hard link"))
    w("badino.bad.ffpfs", p_hook(pentry("", lambda d, f: 99999, pfs.DT_FILE, b"lost")))
    bad.append(("badino.bad.ffpfs", "out of range"))
    w("type.bad.ffpfs", p_hook(pentry("", lambda d, f: fmap(f)["eboot.bin"].ino, 7, b"odd")))
    bad.append(("type.bad.ffpfs", "neither a file nor a folder"))
    w("dotfile.bad.ffpfs", p_hook(pentry("", lambda d, f: fmap(f)["eboot.bin"].ino, pfs.DT_FILE, b"..")))
    bad.append(("dotfile.bad.ffpfs", "invalid name"))
    for tag, name in (("slash", b"a/b"), ("bslash", b"a\\b"), ("nul", b"a\0b"), ("utf8", b"\xc3")):
        def mk(nm):
            def fn(dirs, files):
                fmap(files)["data/a.bin"].name = nm
            return fn
        w("name_%s.bad.ffpfs" % tag, p_hook(mk(name)))
        bad.append(("name_%s.bad.ffpfs" % tag, "invalid name"))

    def dup(dirs, files):
        fmap(files)["data/b.bin"].name = b"a.bin"
    w("dup.bad.ffpfs", p_hook(dup))
    bad.append(("dup.bad.ffpfs", "is twice"))
    img, inf = pfs.build(m)
    w("signed.bad.ffpfs", patch(img, 0x1C, "<H", 0x9))
    bad.append(("signed.bad.ffpfs", "signed or encrypted"))
    w("encrypted.bad.ffpfs", patch(img, 0x1C, "<H", 0xC))
    bad.append(("encrypted.bad.ffpfs", "signed or encrypted"))
    w("hugesize.bad.ffpfs", patch(img, inf["inode_off"]["data/a.bin"] + 8, "<q", 1 << 62))
    bad.append(("hugesize.bad.ffpfs", "invalid size"))
    w("badblock.bad.ffpfs", patch(img, inf["inode_off"]["data/a.bin"] + 0x64, "<i", 1 << 30))
    bad.append(("badblock.bad.ffpfs", "out of range"))
    w("overlap.bad.ffpfs", patch(img, inf["inode_off"]["data/sub/c.bin"] + 0x64, "<i",
                                 inf["data_off"]["eboot.bin"] // inf["bsize"]))
    bad.append(("overlap.bad.ffpfs", "same blocks"))
    w("truncated.bad.ffpfs", img[:inf["data_off"]["data/sub/c.bin"] + 100])
    bad.append(("truncated.bad.ffpfs", "truncated"))
    w("version.bad.ffpfs", patch(img, 0, "<q", 9))
    bad.append(("version.bad.ffpfs", "version"))
    w("notpfs.bad.ffpfsc", b"\x02" * 70000)
    bad.append(("notpfs.bad.ffpfsc", "not a PFS image"))

    # PFSC damaged: a container of the small game's UFS2 image.
    mini_exfat = ufs2.build(m)[0]
    im, inf = pfs.build_single("PPSA00000.exfat", mini_exfat)
    off = inf["data_off"]["PPSA00000.exfat"]
    w("pfsc_magic.bad.ffpfsc", patch(im, off, "<I", 0x12345678))
    bad.append(("pfsc_magic.bad.ffpfsc", "invalid PFSC"))
    first, second = struct.unpack_from("<QQ", im, off + 0x400)
    w("pfsc_backwards.bad.ffpfsc", patch(im, off + 0x408, "<Q", first - 1))
    bad.append(("pfsc_backwards.bad.ffpfsc", "backwards"))
    nblk = (len(mini_exfat) + 0xFFFF) // 0x10000
    w("pfsc_pastend.bad.ffpfsc", patch(im, off + 0x400 + 8 * nblk, "<Q", 1 << 40))
    bad.append(("pfsc_pastend.bad.ffpfsc", "invalid PFSC"))
    w("pfsc_blocksize.bad.ffpfsc", patch(im, off + 0x0C, "<I", 12345))
    bad.append(("pfsc_blocksize.bad.ffpfsc", "block size"))
    # A block that inflates to half a block: block 0 becomes the deflate of 32 KiB.
    stream = im[off:off + struct.unpack_from("<q", im, inf["inode_off"]["PPSA00000.exfat"] + 8)[0]]
    offs = list(struct.unpack_from("<%dQ" % (nblk + 1), stream, 0x400))
    blocks = [stream[offs[i]:offs[i + 1]] for i in range(nblk)]
    blocks[0] = zlib.compress(bytes(0x8000))
    head = bytearray(stream[:offs[0]])
    pos = [offs[0]]
    for blk in blocks:
        pos.append(pos[-1] + len(blk))
    struct.pack_into("<%dQ" % (nblk + 1), head, 0x400, *pos)
    new = bytes(head) + b"".join(blocks)
    b = bytearray(im)
    b[off:off + len(new)] = new
    struct.pack_into("<q", b, inf["inode_off"]["PPSA00000.exfat"] + 8, len(new))
    w("pfsc_inflate.bad.ffpfsc", bytes(b))
    bad.append(("pfsc_inflate.bad.ffpfsc", "does not inflate"))

    with open(os.path.join(out, "bad.tsv"), "w", encoding="utf-8", newline="\n") as f:
        for name, why in bad:
            f.write("%s\t%s\n" % (name, why))

    # ---- images made by the real tools (tests/data, compressed) ---------------------------
    n_real = 0
    if os.path.isdir(args.data):
        for name in sorted(os.listdir(args.data)):
            if name.endswith(".gz"):
                with gzip.open(os.path.join(args.data, name), "rb") as src:
                    data = src.read()
                w(name[:-3], data)
                if name.endswith((".ffpfs.gz", ".ffpfsc.gz")):
                    write_meta(os.path.join(out, name[:-3] + ".meta"), meta_of_pfs(data))
                n_real += 1
    print("images: basic in 9 forms, %d damaged, %d made by real tools, in %s" % (len(bad), n_real, out))


if __name__ == "__main__":
    main()
