#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Builds UFS2 images (.ffpkg) for MaxPack's tests, from a list of files.

The layout follows FreeBSD's sys/ufs/ffs/fs.h, sys/ufs/ufs/dinode.h and sys/ufs/ufs/dir.h: the
superblock at 64 KiB, cylinder groups of fpg fragments each with its inodes at iblkno, 256-byte
inodes with 12 direct and 3 indirect (single, double, triple) 64-bit block pointers in fragments,
the last block of a small file in fragments, folders in 512-byte chunks of entries. Root is inode 2.

As a library: build(files, ...) -> (image bytes, info). As a tool:
    make_ufs2.py <folder> <image.ffpkg> [--bsize 65536 --fsize 65536]
builds the image of a folder (what newfs -O 2 -b 65536 -f 65536 -m 0 -S 4096 plus a copy gives).
"""
import argparse
import bisect
import os
import struct

SBLOCK = 65536
SBLOCKSIZE = 8192
MAGIC = 0x19540119
NDADDR = 12
NIADDR = 3
DINODE = 256
DIRBLKSIZ = 512
ROOTINO = 2
IFDIR, IFREG, IFLNK, IFCHR, IFIFO = 0o040000, 0o100000, 0o120000, 0o020000, 0o010000
DT_DIR, DT_REG, DT_LNK, DT_CHR, DT_FIFO = 4, 8, 10, 2, 1
MTIME = 1714979290


def ceil_div(a, b):
    return (a + b - 1) // b


class Sparse:
    """The content of a sparse file: `size` bytes, zeros but for the pieces {offset: bytes}."""

    def __init__(self, size, pieces):
        self.size, self.pieces = size, pieces

    def __len__(self):
        return self.size

    def __getitem__(self, sl):
        start, stop, _ = sl.indices(self.size)
        out = bytearray(max(0, stop - start))
        for off, b in self.pieces.items():
            lo, hi = max(off, start), min(off + len(b), stop)
            if lo < hi:
                out[lo - start:hi - start] = b[lo - off:hi - off]
        return bytes(out)


class Node:
    def __init__(self, name, kind, data=b"", mtime=MTIME):
        self.name = name            # str, or bytes for names that are not UTF-8
        self.kind = kind            # "dir", "file", "link", "chr", "fifo"
        self.data = data
        self.mtime = mtime
        self.children = []
        self.ino = 0
        self.holes = set()          # file blocks (lbn) left as holes
        self.db = [0] * NDADDR
        self.ib = [0] * NIADDR
        self.frags_held = 0
        self.extra = []             # (inode, type, name) entries added by hostile fixtures


def build_tree(files):
    """files: (path, data) or (path, data, opts) with opts a dict: holes (set of lbn), kind."""
    root = Node("", "dir")
    for item in files:
        path, data = item[0], item[1]
        opts = item[2] if len(item) > 2 and isinstance(item[2], dict) else {}
        parts = path.split("/")
        cur = root
        for p in parts[:-1]:
            nxt = next((c for c in cur.children if c.kind == "dir" and c.name == p), None)
            if nxt is None:
                nxt = Node(p, "dir")
                cur.children.append(nxt)
            cur = nxt
        n = Node(parts[-1], opts.get("kind", "file"), data, opts.get("mtime", MTIME))
        n.holes = set(opts.get("holes", ()))
        cur.children.append(n)
    return root


def walk(node, prefix=""):
    for c in node.children:
        p = prefix + (c.name if isinstance(c.name, str) else c.name.decode("utf-8", "replace"))
        yield p, c
        if c.kind == "dir":
            yield from walk(c, p + "/")


def name_bytes(n):
    return n.name if isinstance(n.name, bytes) else n.name.encode("utf-8")


def dirent(ino, typ, name):
    size = (8 + len(name) + 1 + 3) & ~3
    return bytearray(struct.pack("<IHBB", ino, size, typ, len(name)) + name + bytes(size - 8 - len(name)))


def dir_data(node, parent_ino):
    """Entries of a folder in 512-byte chunks; the last of each chunk takes the rest of it."""
    ents = [dirent(node.ino, DT_DIR, b"."), dirent(parent_ino, DT_DIR, b"..")]
    types = {"dir": DT_DIR, "file": DT_REG, "link": DT_LNK, "chr": DT_CHR, "fifo": DT_FIFO}
    for c in node.children:
        ents.append(dirent(c.ino, types[c.kind], name_bytes(c)))
    for ino, typ, name in node.extra:
        ents.append(dirent(ino, typ, name))
    out = bytearray()
    chunk = bytearray()
    for e in ents:
        if len(chunk) + len(e) > DIRBLKSIZ:
            struct.pack_into("<H", chunk, last + 4, DIRBLKSIZ - last)
            out += chunk + bytes(DIRBLKSIZ - len(chunk))
            chunk = bytearray()
        last = len(chunk)
        chunk += e
    struct.pack_into("<H", chunk, last + 4, DIRBLKSIZ - last)
    out += chunk + bytes(DIRBLKSIZ - len(chunk))
    return bytes(out)


class Layout:
    """Fragment allocator over the data areas of the cylinder groups."""

    def __init__(self, bsize, fsize, ipg, fpg, ncg):
        self.bsize, self.fsize, self.frag = bsize, fsize, bsize // fsize
        self.ipg, self.fpg, self.ncg = ipg, fpg, ncg
        frag = self.frag
        self.sblkno = ceil_div(ceil_div(SBLOCK + SBLOCKSIZE, fsize), frag) * frag
        self.cblkno = self.sblkno + ceil_div(ceil_div(SBLOCKSIZE, fsize), frag) * frag
        self.iblkno = self.cblkno + frag
        self.dblkno = self.iblkno + ipg * DINODE // fsize
        assert self.dblkno % frag == 0 and self.dblkno < fpg
        self.cg = 0
        self.cur = self.dblkno + frag          # a block for the summary area first
        self.csaddr = self.dblkno
        self.partial = None                    # (block start, frags used) for tails

    def _room(self):
        if self.cur >= (self.cg + 1) * self.fpg:
            self.cg += 1
            if self.cg >= self.ncg:
                raise RuntimeError("image too small")
            self.cur = self.cg * self.fpg + self.dblkno
        return (self.cg + 1) * self.fpg

    def block(self):
        while True:
            end = self._room()
            self.cur = ceil_div(self.cur, self.frag) * self.frag
            if self.cur + self.frag <= end:
                a = self.cur
                self.cur += self.frag
                return a
            self.cur = end

    def frags(self, n):
        if self.partial and self.partial[1] + n <= self.frag:
            a = self.partial[0] + self.partial[1]
            self.partial = (self.partial[0], self.partial[1] + n)
            return a
        a = self.block()
        self.partial = (a, n) if n < self.frag else None
        return a


def build(files, bsize=4096, fsize=1024, ipg=None, ncg=2, interleave=(), extra_frags=0, hook=None, trim=False,
          out=None):
    """Returns (image, info). interleave: pairs of paths whose blocks alternate (fragmented files).
    hook(root, nodes): called after inode numbers are assigned and before writing (hostile cases).
    trim: cut the image after the last byte used (the volume then declares more room than it has,
    so a sparse file can be larger than the image). out: a file opened "w+b" to write the image to
    instead of returning it (the image is then None).
    info: {"inode_off": {path: byte offset of its inode}, "dir_off": {path: offset of its
    first block}, "sb": superblock offset, "nodes": {path: Node}}."""
    root = build_tree(files)
    nodes = list(walk(root))
    by_path = dict(nodes)
    inopb = bsize // DINODE
    if ipg is None:
        ipg = max(inopb, ceil_div(ceil_div(len(nodes) + 3, ncg), inopb) * inopb)
    # Inode numbers: root 2, then the rest in the walk's order across the groups.
    root.ino = ROOTINO
    for i, (_, n) in enumerate(nodes):
        n.ino = ROOTINO + 1 + i
    assert ROOTINO + len(nodes) < ncg * ipg, "not enough inodes"
    if hook:
        hook(root, by_path)

    nindir = bsize // 8
    frag = bsize // fsize
    # Room: data, folders, indirect blocks (a rough upper bound), spread over ncg groups.
    need = 0
    for _, n in [("", root)] + nodes:
        size = len(n.data) if n.kind != "dir" else 2 * DIRBLKSIZ + 40 * len(n.children)
        nb = ceil_div(size, bsize)
        need += (nb + (2 + (nb - NDADDR) // nindir if nb > NDADDR else 0)) * frag
    need += extra_frags
    sblkno = ceil_div(ceil_div(SBLOCK + SBLOCKSIZE, fsize), frag) * frag
    dblkno = sblkno + ceil_div(ceil_div(SBLOCKSIZE, fsize), frag) * frag + frag + ipg * DINODE // fsize
    fpg = ceil_div(dblkno + ceil_div(need, ncg) + need // 10 + 4 * frag, frag) * frag
    L = Layout(bsize, fsize, ipg, fpg, ncg)

    def place(n, size):
        """Data blocks of n (the file's bytes are written later)."""
        nb = ceil_div(size, bsize)
        n.blocks = {}
        for lbn in range(nb):
            if lbn in n.holes:
                continue
            last = lbn == nb - 1
            if last and nb <= NDADDR and size % bsize:
                k = ceil_div(size - lbn * bsize, fsize)
                n.blocks[lbn] = (L.frags(k), k)
            else:
                n.blocks[lbn] = (L.block(), frag)

    root.dirbytes = None
    # Folders first (the root and then each in walk order), then files.
    for n in [root] + [n for _, n in nodes if n.kind == "dir"]:
        n.payload = b""   # set once inode numbers of children are known (they are)
    def parent_of(path):
        return by_path[path.rsplit("/", 1)[0]] if "/" in path else root
    root.payload = dir_data(root, ROOTINO)
    for p, n in nodes:
        if n.kind == "dir":
            n.payload = dir_data(n, parent_of(p).ino)
    place(root, len(root.payload))
    for _, n in nodes:
        if n.kind == "dir":
            place(n, len(n.payload))
    inter = {a for pair in interleave for a in pair}
    for p, n in nodes:
        if n.kind in ("file", "link") and p not in inter:
            n.payload = n.data
            place(n, len(n.data))
    for a, b in interleave:
        na, nb_ = by_path[a], by_path[b]
        na.payload, nb_.payload = na.data, nb_.data
        na.blocks, nb_.blocks = {}, {}
        ka, kb = ceil_div(len(na.data), bsize), ceil_div(len(nb_.data), bsize)
        for lbn in range(max(ka, kb)):
            for n, k in ((na, ka), (nb_, kb)):
                if lbn < k and lbn not in n.holes:
                    n.blocks[lbn] = (L.block(), frag)
    for _, n in nodes:
        if n.kind in ("chr", "fifo"):
            n.payload = b""
            n.blocks = {}

    img = bytearray()
    end = [0]

    def write(off, data):
        end[0] = max(end[0], off + len(data))
        if out is not None:   # straight to the file (large images)
            out.seek(off)
            out.write(data)
            return
        if len(img) < off + len(data):
            img.extend(bytes(off + len(data) - len(img)))
        img[off:off + len(data)] = data

    def put(addr, data):
        write(addr * fsize, data)

    def indirect(n, level, start):
        """Pointer to the indirect block of `level` mapping blocks from lbn start (0 if all holes)."""
        span = nindir ** (level - 1)
        lo = bisect.bisect_left(n.lbns, start)
        if lo == len(n.lbns) or n.lbns[lo] >= start + span * nindir:
            return 0
        ptrs = [0] * nindir
        for i in range(nindir):
            lbn = start + i * span
            k = bisect.bisect_left(n.lbns, lbn)
            if k == len(n.lbns) or n.lbns[k] >= lbn + span:
                continue
            ptrs[i] = n.blocks[lbn][0] if level == 1 else indirect(n, level - 1, lbn)
        a = L.block()
        n.frags_held += frag
        put(a, struct.pack("<%dQ" % nindir, *ptrs))
        info["ind_off"].append(a * fsize)
        return a

    info = {"inode_off": {}, "dir_off": {}, "ind_off": [], "nodes": by_path, "sb": SBLOCK, "root": root}

    def inode_off(ino):
        cg, idx = divmod(ino, ipg)
        return (cg * fpg + L.iblkno) * fsize + idx * DINODE

    for p, n in [("", root)] + nodes:
        n.nb = ceil_div(len(n.payload), bsize)
        n.lbns = sorted(n.blocks)
        for lbn, (a, k) in n.blocks.items():
            put(a, n.payload[lbn * bsize:lbn * bsize + k * fsize])
            n.frags_held += k
        n.db = [n.blocks.get(lbn, (0, 0))[0] for lbn in range(NDADDR)]
        start = NDADDR
        n.ib = [0, 0, 0]
        for level in (1, 2, 3):
            if start < n.nb:
                n.ib[level - 1] = indirect(n, level, start)
            start += nindir ** level
        if n.kind == "dir" and n.blocks:
            info["dir_off"][p] = n.blocks[0][0] * fsize
        mode = {"dir": IFDIR | 0o755, "file": IFREG | 0o644, "link": IFLNK | 0o777,
                "chr": IFCHR | 0o600, "fifo": IFIFO | 0o600}[n.kind]
        nlink = 2 + sum(1 for c in n.children if c.kind == "dir") if n.kind == "dir" else 1
        di = bytearray(DINODE)
        struct.pack_into("<HhIII", di, 0, mode, nlink, 0, 0, bsize)
        struct.pack_into("<QQ", di, 16, len(n.payload), n.frags_held * fsize // 512)
        struct.pack_into("<qqqq", di, 32, n.mtime, n.mtime, n.mtime, n.mtime)
        struct.pack_into("<I", di, 80, 0x1234 + n.ino)
        struct.pack_into("<12Q", di, 112, *n.db)
        struct.pack_into("<3Q", di, 208, *n.ib)
        off = inode_off(n.ino)
        write(off, di)
        info["inode_off"][p] = off

    # Superblock (and its copy at the start of each group) and the group headers.
    fs_size = ncg * fpg
    sb = bytearray(1376)
    struct.pack_into("<iiiiii", sb, 0, 0, 0, L.sblkno, L.cblkno, L.iblkno, L.dblkno)
    struct.pack_into("<IiiiI", sb, 44, ncg, bsize, fsize, frag, 0)
    struct.pack_into("<iiii", sb, 72, ~(bsize - 1), ~(fsize - 1), bsize.bit_length() - 1, fsize.bit_length() - 1)
    struct.pack_into("<ii", sb, 88, 16, 2048)
    struct.pack_into("<iii", sb, 96, frag.bit_length() - 1, (fsize // 512).bit_length() - 1, 4096)
    struct.pack_into("<iI", sb, 116, nindir, inopb)
    struct.pack_into("<i", sb, 156, fsize)                     # fs_cssize
    struct.pack_into("<i", sb, 160, bsize)                     # fs_cgsize
    struct.pack_into("<Ii", sb, 184, ipg, fpg)
    sb[209] = 1                                                # fs_clean
    struct.pack_into("<i", sb, 860, bsize)                     # fs_maxbsize
    struct.pack_into("<qq", sb, 992, SBLOCK, SBLOCK)           # fs_sblockactualloc, fs_sblockloc
    struct.pack_into("<qqqq", sb, 1072, MTIME, fs_size, fs_size - ncg * L.dblkno, L.csaddr)
    struct.pack_into("<II", sb, 1196, 16384, 64)               # avgfilesize, avgfpdir
    struct.pack_into("<i", sb, 1320, 120)                      # fs_maxsymlinklen
    struct.pack_into("<i", sb, 1324, 2)                        # fs_old_inodefmt
    struct.pack_into("<Qqq", sb, 1328, (1 << 63) - 1, bsize - 1, fsize - 1)
    struct.pack_into("<i", sb, 1372, MAGIC)
    write(SBLOCK, sb)
    for cg in range(ncg):
        base = cg * fpg * fsize
        if cg:
            write(base + L.sblkno * fsize, sb)
        cgh = bytearray(bsize)
        struct.pack_into("<iiIII", cgh, 0, 0, 0x090255, 0, cg, MTIME & 0xFFFFFFFF)
        struct.pack_into("<I", cgh, 16, fpg)                  # cg_ndblk
        struct.pack_into("<I", cgh, 64, ipg)                  # cg_niblk
        struct.pack_into("<I", cgh, 136, ipg)                 # cg_initediblk
        write(base + L.cblkno * fsize, cgh)
    info["fpg"], info["ipg"], info["iblkno"], info["fsize"], info["bsize"] = fpg, ipg, L.iblkno, fsize, bsize
    info["inode_of"] = inode_off
    if not trim:
        write(ncg * fpg * fsize - 1, b"\0")
    if out is not None:
        out.truncate(end[0])
        return None, info
    return bytes(img), info


class FileData:
    """A file on disk as the content of a node, read when it is written into the image."""

    def __init__(self, path):
        self.path, self.size, self.f = path, os.path.getsize(path), None

    def __len__(self):
        return self.size

    def __getitem__(self, sl):
        start, stop, _ = sl.indices(self.size)
        if self.f is None:
            self.f = open(self.path, "rb")
        self.f.seek(start)
        data = self.f.read(max(0, stop - start))
        if stop >= self.size:
            self.f.close()
            self.f = None
        return data


def from_folder(folder):
    files = []
    for dp, dns, fns in os.walk(folder):
        dns.sort()
        for f in sorted(fns):
            full = os.path.join(dp, f)
            rel = os.path.relpath(full, folder).replace(os.sep, "/")
            files.append((rel, FileData(full), {"mtime": int(os.stat(full).st_mtime)}))
    return files


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("folder")
    ap.add_argument("image")
    ap.add_argument("--bsize", type=int, default=65536)
    ap.add_argument("--fsize", type=int, default=65536)
    a = ap.parse_args()
    with open(a.image, "w+b") as f:
        build(from_folder(a.folder), bsize=a.bsize, fsize=a.fsize, out=f)


if __name__ == "__main__":
    main()
