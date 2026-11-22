#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Genera las imágenes exFAT sintéticas que usan las pruebas de MaxPack.

Uso: make_exfat.py --out <carpeta>

Escribe basic.exfat (+ basic.manifest.tsv y basic.dirs.tsv), truncated_head.exfat,
truncated_tail.exfat, fatloop.exfat y fake.pkg. Todo es determinista.
"""
import argparse
import os
import random
import struct
import zlib

SECTOR_SHIFT = 9
SPC_SHIFT = 3
SECTOR = 1 << SECTOR_SHIFT
CLUSTER = SECTOR << SPC_SHIFT          # 4 KiB
FAT_OFFSET = 128                       # en sectores
EOC = 0xFFFFFFFF
# 2024-05-06 07:08:10 UTC (formato DOS, segundos de 2 en 2) y desplazamiento UTC válido +0.
MTIME_DOS = ((2024 - 1980) << 25) | (5 << 21) | (6 << 16) | (7 << 11) | (8 << 5) | (10 // 2)
MTIME_UNIX = 1714979290
UTC_OFS = 0x80


def rot16(data, skip=()):
    c = 0
    for i, b in enumerate(data):
        if i in skip:
            continue
        c = ((((c & 1) << 15) | (c >> 1)) + b) & 0xFFFF
    return c


def rot32(data, skip=()):
    c = 0
    for i, b in enumerate(data):
        if i in skip:
            continue
        c = ((((c & 1) << 31) | (c >> 1)) + b) & 0xFFFFFFFF
    return c


def ceil_div(a, b):
    return (a + b - 1) // b


class Node:
    def __init__(self, name, is_dir, data=b"", vdl=None):
        self.name = name
        self.is_dir = is_dir
        self.data = data            # archivos: contenido lógico (más allá de VDL son ceros)
        self.vdl = vdl              # None = igual a DataLength
        self.children = []
        self.clusters = []
        self.nofat = True
        self.dl = 0                 # DataLength en bytes
        self.raw = None             # bytes que se escriben en los clústeres (puede diferir más allá de VDL)


def many_json(i):
    rng = random.Random(100 + i)
    target = 300 + (i * 137) % 2701
    tipos = ["arma", "armadura", "pocion", "llave", "mapa"]
    out = []
    n = 0
    while sum(len(x) for x in out) < target:
        vals = ", ".join(str(rng.randint(0, 9999)) for _ in range(rng.randint(2, 6)))
        out.append('{"id": %d, "nombre": "objeto_%03d_%d", "tipo": "%s", "valores": [%s], '
                   '"activo": %s}\n' % (i * 100 + n, i, n, rng.choice(tipos), vals,
                                        "true" if rng.random() < 0.5 else "false"))
        n += 1
    return "".join(out).encode()[:target]


def big_text(n):
    out = []
    i = 0
    while sum(len(x) for x in out) < n:
        out.append(("linea %06d: el rapido zorro marron salta sobre el perro perezoso %d\n"
                    % (i, (i * 7919) % 100003)).encode())
        i += 1
    return b"".join(out)[:n]


LONG_NAME = "Nombre muy largo con acentos ñ y emoji 🎮 para probar varias entradas.dat"


def basic_files():
    """(ruta, datos, vdl) en el orden en que se crean dentro de cada carpeta."""
    files = [
        ("sce_sys/param.json", (b'{"titleId": "TEST00001", "contentVersion": "01.000.000"}'
                                + b" " * 200)[:200], None),
        ("eboot.bin", random.Random(10).randbytes(10000), None),
        ("empty.txt", b"", None),
        ("data/big.bin", big_text(300000), None),
        ("data/frag.bin", random.Random(11).randbytes(50000), None),
        ("data/vdl.bin", random.Random(12).randbytes(5000) + bytes(15000), 5000),
        (LONG_NAME, b"x", None),
        ("deep/a/b/c/d.bin", random.Random(13).randbytes(4096), None),
        ("zeros.bin", bytes(524288), None),
        ("dup1.bin", random.Random(2).randbytes(262144), None),
        ("dup2.bin", random.Random(2).randbytes(262144), None),
        ("dupblocks.bin", random.Random(3).randbytes(262144) * 3, None),
    ]
    files += [("many/f%03d.json" % i, many_json(i), None) for i in range(200)]
    files.append(("random.bin", random.Random(1).randbytes(600000), None))
    return files


def build_tree(files):
    root = Node("", True)
    for path, data, vdl in files:
        parts = path.split("/")
        cur = root
        for p in parts[:-1]:
            nxt = next((c for c in cur.children if c.is_dir and c.name == p), None)
            if nxt is None:
                nxt = Node(p, True)
                cur.children.append(nxt)
            cur = nxt
        cur.children.append(Node(parts[-1], False, data, vdl))
    return root


def walk(node, prefix=""):
    for c in node.children:
        p = prefix + c.name
        yield p, c
        if c.is_dir:
            yield from walk(c, p + "/")


def set_size(name):
    return 32 * (2 + ceil_div(len(name.encode("utf-16-le")) // 2, 15))


def dir_bytes(node, is_root):
    n = sum(set_size(c.name) for c in node.children) + (64 if is_root else 0)
    return max(CLUSTER, ceil_div(n, CLUSTER) * CLUSTER)


def file_set(node):
    name16 = node.name.encode("utf-16-le")
    nchars = len(name16) // 2
    nname = ceil_div(nchars, 15)
    f = bytearray(32)
    f[0] = 0x85
    f[1] = 1 + nname
    struct.pack_into("<H", f, 4, 0x10 if node.is_dir else 0x20)
    struct.pack_into("<III", f, 8, MTIME_DOS, MTIME_DOS, MTIME_DOS)
    f[22] = f[23] = f[24] = UTC_OFS
    s = bytearray(32)
    s[0] = 0xC0
    s[1] = 0x01 | (0x02 if (node.nofat or getattr(node, "dl_override", None) is not None) else 0)
    s[3] = nchars
    upper = "".join(ch.upper() if "a" <= ch <= "z" else ch for ch in node.name).encode("utf-16-le")
    struct.pack_into("<H", s, 4, rot16(upper))
    vdl = node.dl if node.vdl is None else node.vdl
    dl = node.dl
    if getattr(node, "dl_override", None) is not None:   # imágenes trampa: tamaño falso
        dl = vdl = node.dl_override
    struct.pack_into("<Q", s, 8, vdl)
    first = node.clusters[0] if node.clusters else 0
    if getattr(node, "first_override", None) is not None:
        first = node.first_override
    struct.pack_into("<IQ", s, 20, first, dl)
    names = bytearray()
    for k in range(nname):
        e = bytearray(32)
        e[0] = 0xC1
        chunk = name16[k * 30:(k + 1) * 30]
        e[2:2 + len(chunk)] = chunk
        names += e
    entry_set = f + s + names
    struct.pack_into("<H", entry_set, 2, rot16(entry_set, skip=(2, 3)))
    return bytes(entry_set)


def build(files, overrides=None, extra_clusters=0):
    """overrides: {ruta: DataLength falso}; extra_clusters: clústeres que el volumen declara de
    más sin escribirlos (la imagen queda más corta que el volumen)."""
    root = build_tree(files)
    nodes = list(walk(root))
    by_path = {p: n for p, n in nodes}
    for path, dl in (overrides or {}).items():
        by_path[path].dl_override = dl
        if extra_clusters:   # que apunte a los clústeres extra, sin chocar con otros archivos
            by_path[path].first_override = "extra"

    # Tamaños.
    root.dl = dir_bytes(root, True)
    for _, n in nodes:
        n.dl = dir_bytes(n, False) if n.is_dir else len(n.data)
    upcase = bytearray()
    for ch in range(128):
        upcase += struct.pack("<H", ord(chr(ch).upper()) if 97 <= ch <= 122 else ch)
    data_clusters = 1 + ceil_div(root.dl, CLUSTER) + sum(ceil_div(n.dl, CLUSTER) for _, n in nodes)
    bitmap_c = 1
    while True:
        count = data_clusters + bitmap_c + 16 + extra_clusters
        need = ceil_div(ceil_div(count, 8), CLUSTER)
        if need == bitmap_c:
            break
        bitmap_c = need

    # Asignación: mapa de bits, upcase, directorios, datos; frag/vdl intercalados; random.bin al final.
    nxt = [2]

    def take(k):
        out = list(range(nxt[0], nxt[0] + k))
        nxt[0] += k
        return out

    bitmap_clusters = take(bitmap_c)
    upcase_clusters = take(1)
    root.clusters = take(ceil_div(root.dl, CLUSTER))
    root.nofat = False
    for p, n in nodes:
        if n.is_dir:
            n.clusters = take(ceil_div(n.dl, CLUSTER))
            n.nofat = p != "many"          # many/ va encadenado por FAT
    frag, vdl, rnd = by_path["data/frag.bin"], by_path["data/vdl.bin"], by_path["random.bin"]
    for p, n in nodes:
        if not n.is_dir and n not in (frag, vdl, rnd) and n.dl:
            n.clusters = take(ceil_div(n.dl, CLUSTER))
            n.nofat = p != "dupblocks.bin"   # encadenado por FAT aunque sea contiguo
    nf, nv = ceil_div(frag.dl, CLUSTER), ceil_div(vdl.dl, CLUSTER)
    inter = take(2 * nv)
    vdl.clusters = inter[1::2]
    frag.clusters = inter[0::2] + take(nf - nv)
    frag.nofat = vdl.nofat = False
    rnd.clusters = take(ceil_div(rnd.dl, CLUSTER))
    rnd.nofat = True

    used = nxt[0] - 2
    assert used <= count
    for _, n in nodes:
        if getattr(n, "first_override", None) == "extra":
            n.first_override = count - extra_clusters + 2

    # FAT.
    fat = [0] * (count + 2)
    fat[0], fat[1] = 0xFFFFFFF8, EOC

    def chain(cl):
        for a, b in zip(cl, cl[1:]):
            fat[a] = b
        if cl:
            fat[cl[-1]] = EOC

    chain(bitmap_clusters)
    chain(upcase_clusters)
    chain(root.clusters)
    for _, n in nodes:
        if not n.nofat:
            chain(n.clusters)

    # Heap.
    heap = bytearray((count - extra_clusters) * CLUSTER)   # los clústeres extra no se escriben

    def put(cl, data):
        for i, c in enumerate(cl):
            piece = data[i * CLUSTER:(i + 1) * CLUSTER]
            off = (c - 2) * CLUSTER
            heap[off:off + len(piece)] = piece

    bitmap = bytearray(ceil_div(count, 8))
    for c in range(2, nxt[0]):
        bitmap[(c - 2) // 8] |= 1 << ((c - 2) % 8)
    put(bitmap_clusters, bitmap)
    put(upcase_clusters, upcase)
    for _, n in nodes:
        if n.is_dir:
            continue
        raw = bytearray(n.data)
        if n.vdl is not None:
            # Más allá de VDL el disco tiene basura: el lector debe devolver ceros.
            raw = raw[:n.vdl] + b"\xAA" * (len(n.clusters) * CLUSTER - n.vdl)
        put(n.clusters, raw)

    def dir_content(node, is_root):
        b = bytearray()
        if is_root:
            e = bytearray(32)
            e[0] = 0x81
            struct.pack_into("<IQ", e, 20, bitmap_clusters[0], len(bitmap))
            b += e
            e = bytearray(32)
            e[0] = 0x82
            struct.pack_into("<I", e, 4, rot32(upcase))
            struct.pack_into("<IQ", e, 20, upcase_clusters[0], len(upcase))
            b += e
        for c in node.children:
            b += file_set(c)
        return bytes(b)

    put(root.clusters, dir_content(root, True))
    for _, n in nodes:
        if n.is_dir:
            put(n.clusters, dir_content(n, False))

    # Región de arranque, FAT y heap.
    fat_len = ceil_div((count + 2) * 4, SECTOR)
    spc = 1 << SPC_SHIFT
    heap_off = ceil_div(FAT_OFFSET + fat_len, spc) * spc
    vol_len = heap_off + count * spc
    boot = bytearray(SECTOR)
    boot[0:3] = b"\xEB\x76\x90"
    boot[3:11] = b"EXFAT   "
    struct.pack_into("<QQIIIIIIHHBBBBB", boot, 64, 0, vol_len, FAT_OFFSET, fat_len, heap_off,
                     count, root.clusters[0], 0x4D415850, 0x0100, 0, SECTOR_SHIFT, SPC_SHIFT,
                     1, 0x80, 0)
    boot[120:510] = b"\xF4" * 390
    boot[510:512] = b"\x55\xAA"
    region = bytearray(boot)
    for _ in range(8):
        ext = bytearray(SECTOR)
        ext[-4:] = b"\x00\x00\x55\xAA"
        region += ext
    region += bytes(2 * SECTOR)
    csum = rot32(region, skip=(106, 107, 112))
    region += struct.pack("<I", csum) * (SECTOR // 4)
    img = bytearray(region + region)                       # principal + copia
    img += bytes(FAT_OFFSET * SECTOR - len(img))
    img += struct.pack("<%dI" % len(fat), *fat)
    img += bytes(heap_off * SECTOR - len(img))
    img += heap
    assert len(img) == (vol_len - extra_clusters * spc) * SECTOR

    def coff(c):
        return heap_off * SECTOR + (c - 2) * CLUSTER

    info = {
        "random_off": coff(rnd.clusters[0]),
        "frag_fat_entry": FAT_OFFSET * SECTOR + frag.clusters[2] * 4,
        "frag_first": frag.clusters[0],
    }
    return bytes(img), nodes, info


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)
    img, nodes, info = build(basic_files())

    def w(name, data):
        with open(os.path.join(args.out, name), "wb") as f:
            f.write(data)

    w("basic.exfat", img)
    files = sorted(((p, n) for p, n in nodes if not n.is_dir), key=lambda x: x[0].encode())
    with open(os.path.join(args.out, "basic.manifest.tsv"), "w", encoding="utf-8", newline="\n") as f:
        for p, n in files:
            f.write("%d\t%08x\t%d\t%s\n" % (len(n.data), zlib.crc32(n.data), MTIME_UNIX, p))
    dirs = sorted((p for p, n in nodes if n.is_dir), key=lambda x: x.encode())
    with open(os.path.join(args.out, "basic.dirs.tsv"), "w", encoding="utf-8", newline="\n") as f:
        for p in dirs:
            f.write(p + "\n")
    w("truncated_head.exfat", img[:300])
    w("truncated_tail.exfat", img[:info["random_off"] + 300000])
    loop = bytearray(img)
    struct.pack_into("<I", loop, info["frag_fat_entry"], info["frag_first"])
    w("fatloop.exfat", bytes(loop))
    w("fake.pkg", b"\x7FFIH" + bytes(60))
    # Imágenes trampa: un archivo con DataLength = 2^64 - 1 y una carpeta que dice ocupar 300 MiB
    # en un volumen que declara clústeres de sobra (la imagen real es mucho más corta).
    w("huge_len.exfat", build(basic_files(), overrides={"zeros.bin": 2**64 - 1})[0])
    w("bigdir.exfat", build(basic_files(), overrides={"deep": 300 << 20}, extra_clusters=80000)[0])
    print("fixtures: %d archivos, %d carpetas, imagen de %d bytes en %s"
          % (len(files), len(dirs), len(img), args.out))


if __name__ == "__main__":
    main()
