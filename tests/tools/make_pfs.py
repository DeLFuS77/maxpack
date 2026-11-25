#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Builds unsigned PFS images (.ffpfs) and PFS containers (.ffpfsc) for MaxPack's tests.

The layout is the one MkPFS (PSBrew/MkPFS) writes, described in docs/source-images.md and checked
against MkPFS's own reader: block 0 the header (version, magic 20130315, mode, block size, inode
count, data block count, inode block count), the table of 0xA8-byte inodes from block 1, the
super-root folder (flat_path_table and uroot), the flat path table, a spare block, then uroot,
the other folders and the files, each contiguous from its first block. A compressed file is a PFSC
stream: a 0x30-byte header, a table of block offsets at 0x400, the blocks from 0x10000 (more if
the table needs it), each 64 KiB block stored as is or deflated with zlib. Its inode's size is the
stored size and its second size the real one.

As a library: build(files, ...) and build_single(name, payload, ...) -> (image, info), and
pfsc(raw) -> stream. As a tool:
    make_pfs.py folder <folder> <image.ffpfs> [--compress] [--bsize 65536]
    make_pfs.py file <image> <container.ffpfsc> [--no-compress]
"""
import argparse
import os
import struct
import zlib

MAGIC = 20130315
INODE = 0xA8
PFSC_MAGIC = 0x43534650
PFSC_BLOCK = 0x10000
PFSC_TABLE = 0x400
PFSC_DATA = 0x10000
IF_DIR, IF_FILE = 0x4000, 0x8000
RX = 0o555
FLAG_COMPRESSED, FLAG_READONLY, FLAG_INTERNAL = 0x1, 0x10, 0x20000
DT_FILE, DT_DIR, DT_DOT, DT_DOTDOT = 2, 3, 4, 5
MTIME = 1714979290


def ceil_div(a, b):
    return (a + b - 1) // b


def pfsc(raw, level=6, block=PFSC_BLOCK, raw_blocks=()):
    """The PFSC stream of raw. Blocks that do not shrink, and those in raw_blocks, are stored as
    is (padded to a whole block); the last block is padded with zeros before compressing."""
    n = ceil_div(len(raw), block)
    table = (n + 1) * 8
    extra = max(0, table - (PFSC_DATA - PFSC_TABLE))
    data_start = PFSC_DATA + ceil_div(extra, block) * block
    blocks = []
    for i in range(n):
        b = raw[i * block:(i + 1) * block].ljust(block, b"\0")
        c = zlib.compress(b, level)
        blocks.append(b if i in raw_blocks or len(c) >= block else c)
    offs = [data_start]
    for b in blocks:
        offs.append(offs[-1] + len(b))
    head = bytearray(data_start)
    struct.pack_into("<IIIIQQQQ", head, 0, PFSC_MAGIC, 0, 6, block, block, PFSC_TABLE, data_start, n * block)
    struct.pack_into("<%dQ" % (n + 1), head, PFSC_TABLE, *offs)
    return bytes(head) + b"".join(blocks)


def fpt_hash(path, case_insensitive=True):
    h = 0
    for c in path:
        h = (ord(c.upper() if case_insensitive else c) + 31 * h) & 0xFFFFFFFF
    return h


def dirent(ino, typ, name):
    size = (len(name) + 17 + 7) & ~7
    return struct.pack("<Iiii", ino, typ, len(name), size) + name + bytes(size - 16 - len(name))


class Node:
    def __init__(self, path, name, is_dir, data=b""):
        self.path, self.name, self.is_dir, self.data = path, name, is_dir, data
        self.children = []
        self.ino = 0
        self.extra = []   # (inode, type, name bytes) entries added by hostile fixtures


def inode_bytes(mode, nlink, flags, size, size2, blocks, db0, mtime):
    b = bytearray(INODE)
    struct.pack_into("<HHIqq", b, 0, mode, nlink, flags, size, size2)
    struct.pack_into("<qqqq", b, 0x18, mtime, mtime, mtime, mtime)
    struct.pack_into("<I", b, 0x60, blocks)
    struct.pack_into("<12i", b, 0x64, db0, *([-1] * 11))
    struct.pack_into("<5i", b, 0x94, 0, 0, 0, 0, 0)
    return bytes(b)


def build(files, bsize=4096, compress=(), version=2, mode=0x8, mtime=MTIME, hook=None, level=6):
    """files: (path, data) pairs. compress: paths stored PFSC-compressed, or True for all that
    shrink. hook(nodes, inode_of) may change nodes before the layout (hostile cases).
    Returns (image, info) with info["inode_off"][path], info["data_off"][path], info["dir_off"][path]."""
    root = Node("", b"", True)
    dirs = {"": root}
    for item in files:
        path, data = item[0], item[1]
        parts = path.split("/")
        cur = root
        for i, p in enumerate(parts[:-1]):
            sub = "/".join(parts[:i + 1])
            if sub not in dirs:
                dirs[sub] = Node(sub, p.encode("utf-8"), True)
                cur.children.append(dirs[sub])
            cur = dirs[sub]
        n = Node(path, parts[-1].encode("utf-8"), False, data)
        cur.children.append(n)
    file_nodes = []

    def collect(d):
        for c in d.children:
            if c.is_dir:
                collect(c)
            else:
                file_nodes.append(c)
    collect(root)
    dir_nodes = sorted((d for d in dirs.values() if d.path), key=lambda d: d.path.lower())
    file_nodes.sort(key=lambda f: f.path.lower())
    # Inodes: 0 super-root, 1 flat_path_table, 2 uroot, then folders and files.
    root.ino = 2
    for i, n in enumerate(dir_nodes + file_nodes):
        n.ino = 3 + i
    if hook:
        hook(dirs, file_nodes)
    ninodes = 3 + len(dir_nodes) + len(file_nodes)
    ipb = bsize // INODE
    iblocks = ceil_div(ninodes, ipb)

    def parent(n):
        return dirs[n.path.rsplit("/", 1)[0]] if "/" in n.path else root

    def dir_blob(d):
        b = dirent(d.ino, DT_DOT, b".") + dirent(parent(d).ino if d.path else d.ino, DT_DOTDOT, b"..")
        for c in sorted((c for c in d.children if c.is_dir), key=lambda c: c.path.lower()):
            b += dirent(c.ino, DT_DIR, c.name)
        for c in sorted((c for c in d.children if not c.is_dir), key=lambda c: c.path.lower()):
            b += dirent(c.ino, DT_FILE, c.name)
        for ino, typ, name in d.extra:
            b += dirent(ino, typ, name)
        return b

    entries = [(("/" + d.path), d.ino | 0x20000000) for d in dir_nodes] + [("/" + f.path, f.ino) for f in file_nodes]
    fpt = b"".join(struct.pack("<II", h, v) for h, v in sorted((fpt_hash(p), v) for p, v in entries))
    for f in file_nodes:
        want = compress is True or f.path in compress
        f.stored, f.compressed = f.data, False
        if want and f.data:
            c = pfsc(f.data, level)
            if compress is not True or len(c) < len(f.data):
                f.stored, f.compressed = c, True
    nd = 1 + iblocks
    sroot_db = nd
    nd += 1
    fpt_db = nd
    nd += max(1, ceil_div(len(fpt), bsize))
    nd += 1   # the spare block MkPFS leaves when there is no collision_resolver
    layout = []
    for d in [root] + dir_nodes:
        blob = dir_blob(d)
        blocks = max(1, ceil_div(len(blob), bsize))
        layout.append((d, blob, blocks, nd))
        nd += blocks
    for f in file_nodes:
        blocks = max(1, ceil_div(len(f.stored), bsize))
        layout.append((f, f.stored, blocks, nd))
        nd += blocks
    img = bytearray(nd * bsize)
    hdr = bytearray(0x400)
    struct.pack_into("<qqq", hdr, 0, version, MAGIC, 0)
    struct.pack_into("<BBBBHHII", hdr, 0x18, 0, 0, 1, 0, mode, 0, bsize, 0)
    struct.pack_into("<qqqqq", hdr, 0x28, 1, ninodes, nd, iblocks, 0)
    struct.pack_into("<HHIqq", hdr, 0x50, 0, 1, FLAG_READONLY, iblocks * bsize, iblocks * bsize)
    struct.pack_into("<I", hdr, 0x50 + 0x60, iblocks)
    struct.pack_into("<q", hdr, 0x50 + 0x68 + 32, 1)
    struct.pack_into("<I", hdr, 0x368, 1)
    img[0:0x400] = hdr
    info = {"inode_off": {}, "data_off": {}, "dir_off": {}, "bsize": bsize, "iblocks": iblocks,
            "sroot_off": sroot_db * bsize}

    def inode_of(ino):
        return bsize + (ino // ipb) * bsize + (ino % ipb) * INODE

    def put_inode(ino, b):
        img[inode_of(ino):inode_of(ino) + INODE] = b

    put_inode(0, inode_bytes(IF_DIR | RX, 1, FLAG_INTERNAL | FLAG_READONLY, bsize, bsize, 1, sroot_db, mtime))
    put_inode(1, inode_bytes(IF_FILE | RX, 1, FLAG_INTERNAL | FLAG_READONLY, len(fpt), len(fpt),
                             max(1, ceil_div(len(fpt), bsize)), fpt_db, mtime))
    sroot = dirent(1, DT_FILE, b"flat_path_table") + dirent(2, DT_DIR, b"uroot")
    img[sroot_db * bsize:sroot_db * bsize + len(sroot)] = sroot
    img[fpt_db * bsize:fpt_db * bsize + len(fpt)] = fpt
    for n, blob, blocks, db0 in layout:
        img[db0 * bsize:db0 * bsize + len(blob)] = blob
        if n.is_dir:
            nlink = 2 + sum(1 for c in n.children if c.is_dir)
            put_inode(n.ino, inode_bytes(IF_DIR | RX, nlink, FLAG_READONLY, blocks * bsize, blocks * bsize,
                                         blocks, db0, mtime))
            info["dir_off"][n.path] = db0 * bsize
        else:
            flags = FLAG_READONLY | (FLAG_COMPRESSED if n.compressed else 0)
            put_inode(n.ino, inode_bytes(IF_FILE | RX, 1, flags, len(n.stored),
                                         len(n.data) if n.compressed else len(n.stored), blocks, db0, mtime))
            info["data_off"][n.path] = db0 * bsize
        info["inode_off"][n.path] = inode_of(n.ino)
    return bytes(img), info


def build_single(name, payload, bsize=65536, compress=True, version=2, mode=0x8, mtime=MTIME, level=6):
    """A container: a PFS image whose uroot holds only `name`, compressed with PFSC if compress (and
    it shrinks), as MkPFS's `pack file` makes it."""
    return build([(name, payload)], bsize=bsize, compress=True if compress else (), version=version,
                 mode=mode, mtime=mtime, level=level)


def from_folder(folder):
    files = []
    for dp, dns, fns in os.walk(folder):
        dns.sort()
        for f in sorted(fns):
            full = os.path.join(dp, f)
            with open(full, "rb") as h:
                files.append((os.path.relpath(full, folder).replace(os.sep, "/"), h.read()))
    return files


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    a = sub.add_parser("folder")
    a.add_argument("folder")
    a.add_argument("image")
    a.add_argument("--compress", action="store_true")
    a.add_argument("--bsize", type=int, default=65536)
    b = sub.add_parser("file")
    b.add_argument("source")
    b.add_argument("image")
    b.add_argument("--no-compress", action="store_true")
    b.add_argument("--bsize", type=int, default=65536)
    args = ap.parse_args()
    if args.cmd == "folder":
        img, _ = build(from_folder(args.folder), bsize=args.bsize, compress=True if args.compress else ())
    else:
        with open(args.source, "rb") as f:
            img, _ = build_single(os.path.basename(args.source), f.read(), bsize=args.bsize,
                                  compress=not args.no_compress)
    with open(args.image, "wb") as f:
        f.write(img)


if __name__ == "__main__":
    main()
