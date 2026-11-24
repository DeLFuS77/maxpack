#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Reference implementation, independent of the C code, of the AMPR formats.

  ampr_ref.py idx3-build <list.tsv> <output>   list: "size<TAB>mtime<TAB>path" (relative)
  ampr_ref.py idx3-dump <ampr_emu.index>        "size<TAB>mtime<TAB>/app0/..." per row
  ampr_ref.py zpk-check <folder> <source>      validates ampr_zstd.index + volumes and compares
                                                each packed file with the source (folder
                                                or .exfat image); needs compression.zstd

AMPRIDX3 follows write_index of tools/build_ampr_index.py (drakmor/ampr_emu); AMPRZPK1 follows
ampr-emu-ng's docs/zstd-format.md and ps5-dump-forge's docs/zstd.md.
"""
import os
import struct
import sys
import zlib

IDX3_BASIS = 1469598103934665603
STD_BASIS = 14695981039346656037
PRIME = 1099511628211
M64 = 0xFFFFFFFFFFFFFFFF


def key_for(path):
    raw = path.replace("\\", "/").encode("utf-8")
    return bytes((b + 0x20) if 0x41 <= b <= 0x5A else b for b in raw)


def fnv(data, basis):
    h = basis
    for b in data:
        h = ((h ^ b) * PRIME) & M64
    return h


def idx3_hash(path):
    return fnv(key_for(path), IDX3_BASIS) or 1


def idx3_build(rows):
    rows = sorted(rows, key=lambda r: key_for(r[2]))
    blob = bytearray()
    records = bytearray()
    for size, mtime, path in rows:
        enc = path.encode("utf-8") + b"\0"
        records += struct.pack("<IIQq", len(blob), len(enc) - 1, size, mtime)
        blob += enc
    slots_n = 2
    while slots_n < 2 * len(rows):
        slots_n <<= 1
    slots = [(0, 0, 0)] * slots_n
    mask = slots_n - 1
    for i, (_, _, path) in enumerate(rows):
        h = idx3_hash(path)
        pos = h & mask
        dup = False
        while slots[pos][1] != 0:
            if slots[pos][0] == h:
                oh, oi, of = slots[pos]
                slots[pos] = (oh, oi, of | 1)
                dup = True
            pos = (pos + 1) & mask
        slots[pos] = (h, i + 1, 1 if dup else 0)
    path_end = 48 + len(records) + len(blob)
    hash_off = (path_end + 15) & ~15
    out = bytearray(struct.pack("<8sIIQQQII", b"AMPRIDX3", 3, 24, len(rows), len(blob), hash_off, 16, slots_n))
    out += records + blob + b"\0" * (hash_off - path_end)
    for h, i, f in slots:
        out += struct.pack("<QII", h, i, f)
    return bytes(out)


def cmd_idx3_build(tsv, dst):
    rows = []
    with open(tsv, encoding="utf-8") as f:
        for line in f:
            size, mtime, path = line.rstrip("\n").split("\t", 2)
            rows.append((int(size), int(mtime), "/app0/" + path))
    with open(dst, "wb") as f:
        f.write(idx3_build(rows))
    return 0


def cmd_idx3_dump(path):
    d = open(path, "rb").read()
    magic, ver, rsz, n, blob, hoff, ssz, slots = struct.unpack_from("<8sIIQQQII", d, 0)
    if magic != b"AMPRIDX3" or ver != 3:
        raise SystemExit("not AMPRIDX3")
    base = 48 + rsz * n
    out = sys.stdout.buffer
    for i in range(n):
        off, ln, size, mtime = struct.unpack_from("<IIQq", d, 48 + rsz * i)
        out.write(b"%d\t%d\t%s\n" % (size, mtime, d[base + off: base + off + ln]))
    return 0


# ---- AMPRZPK1 ------------------------------------------------------------------------------

def zpk_load(folder):
    d = open(os.path.join(folder, "ampr_zstd.index"), "rb").read()
    (magic, ver, hsz, shift, level, _r, vcount, aid, fcount, ccount, foff, coff, voff, soff, slen,
     total, _r2, crc_body, crc_head, _r3) = struct.unpack_from("<8sIIBBHI16sQQQQQQQQQIIQ", d, 0)
    errs = []
    if magic != b"AMPRZPK1" or ver != 1 or hsz != 128 or shift != 18 or not 1 <= level <= 22:
        errs.append("header")
    if zlib.crc32(d[128:]) != crc_body:
        errs.append("body CRC")
    head = bytearray(d[:128])
    head[112:120] = b"\0" * 8
    if zlib.crc32(bytes(head)) != crc_head:
        errs.append("header CRC")
    if foff != 128 or coff != foff + 32 * fcount or voff != coff + 8 * ccount or soff != voff + 8 * vcount \
            or soff + slen != len(d) or (ccount == 0) != (vcount == 0):
        errs.append("tables")
    files = []
    for i in range(fcount):
        h, size, first, poff, plen, flags, _ = struct.unpack_from("<QQIIHHI", d, foff + 32 * i)
        path = d[soff + poff: soff + poff + plen].decode("utf-8")
        files.append((h, size, first, path, flags))
    chunks = [struct.unpack_from("<Q", d, coff + 8 * i)[0] for i in range(ccount)]
    vols = [struct.unpack_from("<Q", d, voff + 8 * i)[0] for i in range(vcount)]
    return files, chunks, vols, aid, errs


def zpk_check(folder, src):
    from compression import zstd   # Python >= 3.14
    files, chunks, vols, aid, errs = zpk_load(folder)
    # Order, first_chunk, hash.
    prev = None
    expect_first = 0
    for h, size, first, path, flags in files:
        if fnv(path.encode("utf-8"), STD_BASIS) != h:
            errs.append("hash of %s" % path)
        if prev is not None and (h, path.encode()) <= prev:
            errs.append("order in %s" % path)
        prev = (h, path.encode())
        if first != expect_first:
            errs.append("first_chunk of %s" % path)
        expect_first += (size + 262143) // 262144
    if expect_first != len(chunks):
        errs.append("number of blocks")
    # Volumes: headers and blocks consecutive from 64.
    vfiles = []
    for v in range(len(vols)):
        name = os.path.join(folder, "ampr_zstd-%03d.pak" % v)
        data = open(name, "rb").read()
        vfiles.append(data)
        if len(data) != vols[v]:
            errs.append("size of volume %d" % v)
        m, ver, hsz, vid, vcount, vaid, payload = struct.unpack_from("<8sIIII16sQ", data, 0)
        crc = struct.unpack_from("<I", data, 60)[0]
        hh = bytearray(data[:64])
        hh[60:64] = b"\0" * 4
        if m != b"AMPRZDT1" or ver != 1 or hsz != 64 or vid != v or vcount != len(vols) or vaid != aid \
                or payload != len(data) - 64 or zlib.crc32(bytes(hh)) != crc:
            errs.append("header of volume %d" % v)
    pos_v, pos_off = 0, 64
    for c in chunks:
        off, vid, stored, codec = c & 0xFFFFFFFF, (c >> 32) & 0xFFF, ((c >> 44) & 0x3FFFF) + 1, c >> 62
        if vid != pos_v:
            if vid != pos_v + 1 or pos_off != vols[pos_v]:
                errs.append("blocks not consecutive across volumes")
            pos_v, pos_off = vid, 64
        if off != pos_off or codec > 1:
            errs.append("block out of place or codec")
        pos_off = off + stored
    # Content against the source.
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    read_src = source_reader(src)
    bad = 0
    for h, size, first, path, flags in files:
        out = bytearray()
        for k in range((size + 262143) // 262144):
            c = chunks[first + k]
            off, vid, stored, codec = c & 0xFFFFFFFF, (c >> 32) & 0xFFF, ((c >> 44) & 0x3FFFF) + 1, c >> 62
            raw = vfiles[vid][off: off + stored]
            want = min(262144, size - k * 262144)
            if codec == 0:
                if stored != want:
                    errs.append("raw block with a different size in %s" % path)
                out += raw
            else:
                if stored >= want:
                    errs.append("zstd block that is not smaller in %s" % path)
                if flags & 1:
                    errs.append("store-only with a zstd block in %s" % path)
                out += zstd.decompress(raw)
        if bytes(out) != read_src(path, size):
            bad += 1
            errs.append("different content: %s" % path)
    for e in errs[:20]:
        print("ERROR:", e)
    print("zpk-check: %d files, %d errors" % (len(files), len(errs)))
    return 1 if errs else 0


def source_reader(src):
    if os.path.isdir(src):
        def read(path, size):
            with open(os.path.join(src, path), "rb") as f:
                return f.read()
        return read
    import io
    from exfat_ref import Image
    img = Image(src)

    def read(path, size):
        b = io.BytesIO()
        img.cat(path, 0, size, b)
        return b.getvalue()
    return read


def main(argv):
    if len(argv) == 4 and argv[1] == "idx3-build":
        return cmd_idx3_build(argv[2], argv[3])
    if len(argv) == 3 and argv[1] == "idx3-dump":
        return cmd_idx3_dump(argv[2])
    if len(argv) == 4 and argv[1] == "zpk-check":
        return zpk_check(argv[2], argv[3])
    sys.stderr.write(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
