#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Packing benchmark: two maxpack builds on the same generated input, compared.

  bench_pack.py gen <folder> [--gb 3]
      Writes a synthetic game of about that many GB (deterministic): a few loose files, many small
      files, some medium ones and a few large ones, each made of pieces of incompressible noise,
      text, structured binary data and zeros.

  bench_pack.py gen-exfat <image> [--gb 8] [--cluster-kib 32]
      The same kind of game as an exFAT image: most files contiguous (NoFatChain), some contiguous
      but chained through the FAT, a group of medium files and two large ones fragmented into
      interleaved runs of 1-64 clusters, two with ValidDataLength < size and an empty one. Next to
      it, <image>.sha256.tsv lists each file's size and SHA-256 (as the reader must return it).

  bench_pack.py evict <input>
      Drops an input made by gen or gen-exfat (it refuses any other) from the system's file cache,
      so that the next read comes from the disk: on Windows it rewrites its bytes in place past the
      cache (a write that bypasses the cache also discards what the cache had of it) and then opens
      each file once, for the antivirus to scan the rewritten files before the measured run;
      elsewhere it asks the system to drop their clean pages.

  bench_pack.py run <input> --old <maxpack> --new <maxpack> --out <folder>
                    [--levels 19,3] [--format p5z|folder] [--threads N] [--cold] [--old-name 1.0.2]
      Packs the input (a folder or an .exfat image) with each build at each level and prints, per
      run: seconds and MB/s (of the input), average CPU (of all the machine's threads), the same for
      the packs phase alone (timed with --progress json; the loose files of a .p5z are always zstd 19
      frames of up to 32 MiB), peak memory and whether both builds wrote the same bytes. Outputs are
      deleted once hashed. The input is read once beforehand so that every run finds it in the
      system's cache, or, with --cold, evicted before every run so that every run reads the disk.

Never point it at real games: it only measures generated data (and evict rewrites its input).
"""
import argparse
import array
import hashlib
import json
import os
import random
import shutil
import struct
import subprocess
import sys
import threading
import time

MIB = 1 << 20

# ---- generated input ---------------------------------------------------------------------------


class Pools:
    """Material for the files: slices of these pools have realistic compressibility."""

    def __init__(self, rng):
        letters = "abcdefghijklmnopqrstuvwxyz"
        vocab = ["".join(rng.choice(letters) for _ in range(rng.randint(2, 11))) for _ in range(5000)]
        weights = [1.0 / (i + 1) for i in range(len(vocab))]   # Zipf-like, like real text
        words = rng.choices(vocab, weights, k=3_000_000)
        seps = rng.choices([" ", " ", " ", " ", ", ", ".\n", "\n", " = ", "; "], k=len(words))
        self.text = "".join(w + s for w, s in zip(words, seps)).encode()[: 16 * MIB]
        # Vertex-like records: positions that wander, normals, texture coordinates and an index.
        a = array.array("f")
        x = y = z = 0.0
        for i in range(16 * MIB // 32):
            x += rng.uniform(-0.01, 0.01)
            y += rng.uniform(-0.01, 0.01)
            z += rng.uniform(-0.01, 0.01)
            a.extend((x, y, z, rng.uniform(-1, 1), rng.uniform(-1, 1), (i % 997) / 997.0,
                      rng.random(), float(i & 0xFFFF)))
        self.struct = a.tobytes()


def pieces(rng, pools, size):
    """The content of a file of that size, as a sequence of pieces."""
    left = size
    while left > 0:
        n = min(left, rng.randint(16 << 10, 2 * MIB))
        kind = rng.choices(("noise", "text", "struct", "zeros"), (40, 25, 25, 10))[0]
        if kind == "noise":
            yield rng.randbytes(n)
        elif kind == "zeros":
            yield bytes(n)
        else:
            pool = pools.text if kind == "text" else pools.struct
            out = bytearray()
            while len(out) < n:
                off = rng.randrange(0, len(pool) - 4096)
                out += pool[off: off + min(n - len(out), len(pool) - off)]
            yield bytes(out)
        left -= n


def gen(folder, gb):
    if os.path.exists(folder) and os.listdir(folder):
        sys.exit(f"{folder} already exists and is not empty")
    rng = random.Random(20261124)
    pools = Pools(rng)
    scale = gb / 3.0
    files = [("sce_sys/param.json", None), ("sce_sys/icon0.png", 180 << 10), ("eboot.bin", 40 * MIB)]
    files += [(f"sce_module/libbench{i}.prx", 6 * MIB) for i in range(4)]
    files += [(f"data/small/d{i % 40:02d}/f{i:05d}.bin", rng.randint(2 << 10, 200 << 10))
              for i in range(int(2500 * scale))]
    files += [(f"data/medium/m{i:03d}.bin", rng.randint(1 * MIB, 12 * MIB)) for i in range(int(150 * scale))]
    used = sum(s for _, s in files if s)
    large = max(0, int(gb * 1e9) - used)
    for i in range(5):
        files.append((f"data/large/L{i:02d}.bin", large // 5))
    total = 0
    t0 = time.time()
    for k, (rel, size) in enumerate(files):
        path = os.path.join(folder, *rel.split("/"))
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "wb") as f:
            if size is None:
                data = b'{"titleId":"TEST00042","contentVersion":"01.000.000","titleName":"Benchmark"}'
                f.write(data)
                total += len(data)
            else:
                for p in pieces(rng, pools, size):
                    f.write(p)
                total += size
        os.utime(path, (1700000000 + k, 1700000000 + k))
    print(f"{len(files)} files, {total / 1e9:.2f} GB in {time.time() - t0:.0f} s: {folder}")


# ---- generated exFAT image ---------------------------------------------------------------------

SECTOR_SHIFT = 9
EOC = 0xFFFFFFFF
GEN_SERIAL = 0x4D415851   # the volume serial number of the images this script makes


def _rot16(data, skip=()):
    c = 0
    for i, b in enumerate(data):
        if i not in skip:
            c = ((((c & 1) << 15) | (c >> 1)) + b) & 0xFFFF
    return c


def _rot32(data, skip=()):
    c = 0
    for i, b in enumerate(data):
        if i not in skip:
            c = ((((c & 1) << 31) | (c >> 1)) + b) & 0xFFFFFFFF
    return c


class Content:
    """One file's bytes, produced on demand and in order from its own random sequence."""

    def __init__(self, pools, seed, size):
        self.gen = pieces(random.Random(seed), pools, size)
        self.buf = b""

    def take(self, n):
        out, have = [self.buf], len(self.buf)
        while have < n:
            p = next(self.gen)
            out.append(p)
            have += len(p)
        b = b"".join(out)
        self.buf = b[n:]
        return b[:n]


def _dos_time(unix):
    t = time.gmtime(unix)
    dos = ((t.tm_year - 1980) << 25) | (t.tm_mon << 21) | (t.tm_mday << 16) | (t.tm_hour << 11) | \
        (t.tm_min << 5) | (t.tm_sec // 2)
    return dos, (t.tm_sec % 2) * 100


def _entry_set(name, is_dir, nofat, first, size, vdl, mtime):
    name16 = name.encode("utf-16-le")
    nchars = len(name16) // 2
    nname = (nchars + 14) // 15
    f = bytearray(32)
    f[0] = 0x85
    f[1] = 1 + nname
    struct.pack_into("<H", f, 4, 0x10 if is_dir else 0x20)
    dos, ms10 = _dos_time(mtime)
    struct.pack_into("<III", f, 8, dos, dos, dos)
    f[20] = f[21] = ms10
    f[22] = f[23] = f[24] = 0x80   # UTC offset +0, valid
    s = bytearray(32)
    s[0] = 0xC0
    s[1] = 0x01 | (0x02 if nofat else 0)
    s[3] = nchars
    upper = "".join(ch.upper() if "a" <= ch <= "z" else ch for ch in name).encode("utf-16-le")
    struct.pack_into("<H", s, 4, _rot16(upper))
    struct.pack_into("<Q", s, 8, vdl)
    struct.pack_into("<IQ", s, 20, first, size)
    names = bytearray()
    for k in range(nname):
        e = bytearray(32)
        e[0] = 0xC1
        chunk = name16[k * 30:(k + 1) * 30]
        e[2:2 + len(chunk)] = chunk
        names += e
    es = f + s + names
    struct.pack_into("<H", es, 2, _rot16(es, skip=(2, 3)))
    return bytes(es)


def gen_exfat(image, gb, cluster_kib):
    if os.path.exists(image):
        sys.exit(f"{image} already exists")
    cs = cluster_kib << 10
    if cs < 4096 or cs > (32 << 20) or cs & (cs - 1):
        sys.exit("--cluster-kib must be a power of two between 4 and 32768")
    spc_shift = (cs >> SECTOR_SHIFT).bit_length() - 1
    rng = random.Random(20261125)
    pools = Pools(rng)
    scale = gb / 3.0
    files = [("sce_sys/param.json", None), ("sce_sys/icon0.png", 180 << 10), ("eboot.bin", 40 * MIB)]
    files += [(f"sce_module/libbench{i}.prx", 6 * MIB) for i in range(4)]
    files += [(f"data/small/d{i % 40:02d}/f{i:05d}.bin", rng.randint(2 << 10, 200 << 10))
              for i in range(int(2500 * scale))]
    files += [(f"data/medium/m{i:03d}.bin", rng.randint(1 * MIB, 12 * MIB)) for i in range(int(150 * scale))]
    files.append(("data/empty.bin", 0))
    used = sum(s for _, s in files if s)
    large = max(0, int(gb * 1e9) - used)
    files += [(f"data/large/L{i:02d}.bin", large // 5) for i in range(5)]
    pj = b'{"titleId":"TEST00043","contentVersion":"01.000.000","titleName":"Benchmark image"}'
    recs = []   # path, size, vdl, nofat, fragmented
    for rel, size in files:
        size = len(pj) if size is None else size
        frag = (rel.startswith("data/medium/") and int(rel[-7:-4]) % 5 == 4) or rel in (
            "data/large/L01.bin", "data/large/L03.bin")
        chained = rel.startswith("data/small/") and int(rel[-9:-4]) % 7 == 3
        vdl = size // 2 + 1234 if rel in ("data/medium/m001.bin", "data/medium/m002.bin") else size
        recs.append([rel, size, vdl, not (frag or chained), frag])

    # The tree: folders in the order their first file appears.
    children = {"": []}
    for k, r in enumerate(recs):
        parts = r[0].split("/")
        for d in range(1, len(parts)):
            parent, me = "/".join(parts[:d - 1]), "/".join(parts[:d])
            if me not in children:
                children[me] = []
                children[parent].append(("d", me))
        children["/".join(parts[:-1])].append(("f", k))
    set_bytes = lambda name: 32 * (2 + (len(name.encode("utf-16-le")) // 2 + 14) // 15)
    base = lambda p: p.rsplit("/", 1)[-1]
    dir_size = {}
    for d, kids in children.items():
        n = sum(set_bytes(base(x if t == "d" else recs[x][0])) for t, x in kids) + (64 if d == "" else 0)
        dir_size[d] = max(cs, (n + cs - 1) // cs * cs)
    ncl = lambda n: (n + cs - 1) // cs

    # Clusters: bitmap, up-case table, folders, contiguous files, then the fragmented group.
    upcase = b"".join(struct.pack("<H", ord(chr(c).upper()) if 97 <= c <= 122 else c) for c in range(128))
    need = 1 + sum(ncl(s) for s in dir_size.values()) + sum(ncl(r[1]) for r in recs)
    bitmap_c = 1
    while True:
        count = need + bitmap_c + 16
        b = ncl((count + 7) // 8)
        if b == bitmap_c:
            break
        bitmap_c = b
    nxt = [2]

    def take(k):
        first = nxt[0]
        nxt[0] += k
        return [(first, k)]

    runs = {}   # key -> list of (first cluster, number of clusters)
    runs["bitmap"] = take(bitmap_c)
    runs["upcase"] = take(1)
    for d in children:
        runs["dir:" + d] = take(ncl(dir_size[d]))
    for k, r in enumerate(recs):
        if not r[4] and r[1]:
            runs[k] = take(ncl(r[1]))
    left = {k: ncl(r[1]) for k, r in enumerate(recs) if r[4] and r[1]}
    for k in left:
        runs[k] = []
    active = sorted(left)
    while active:
        k = rng.choice(active)
        n = min(left[k], rng.randint(1, 64))
        runs[k] += take(n)
        left[k] -= n
        if not left[k]:
            active.remove(k)
    assert nxt[0] - 2 <= count

    fat = array.array("I", bytes(4 * (count + 2)))
    fat[0], fat[1] = 0xFFFFFFF8, EOC

    def chain(rs):
        cl = [c for first, n in rs for c in range(first, first + n)]
        for a, b in zip(cl, cl[1:]):
            fat[a] = b
        if cl:
            fat[cl[-1]] = EOC

    chain(runs["bitmap"])
    chain(runs["upcase"])
    chain(runs["dir:"])   # the root goes through the FAT, and so does one folder
    chained_dir = "data/small/d07"
    if "dir:" + chained_dir in runs:
        chain(runs["dir:" + chained_dir])
    for k, r in enumerate(recs):
        if not r[3] and r[1]:
            chain(runs[k])

    mtime0 = 1714979290
    meta = {}
    bitmap = bytearray((count + 7) // 8)
    for c in range(2, nxt[0]):
        bitmap[(c - 2) // 8] |= 1 << ((c - 2) % 8)
    meta["bitmap"] = bytes(bitmap)
    meta["upcase"] = upcase
    for d, kids in children.items():
        b = bytearray()
        if d == "":
            e = bytearray(32)
            e[0] = 0x81
            struct.pack_into("<IQ", e, 20, runs["bitmap"][0][0], len(bitmap))
            b += e
            e = bytearray(32)
            e[0] = 0x82
            struct.pack_into("<I", e, 4, _rot32(upcase))
            struct.pack_into("<IQ", e, 20, runs["upcase"][0][0], len(upcase))
            b += e
        for t, x in kids:
            if t == "d":
                b += _entry_set(base(x), True, x != chained_dir, runs["dir:" + x][0][0], dir_size[x],
                                dir_size[x], mtime0)
            else:
                path, size, vdl, nofat, _ = recs[x]
                first = runs[x][0][0] if size else 0
                b += _entry_set(base(path), False, nofat, first, size, vdl, mtime0 + 2 * x)
        meta["dir:" + d] = bytes(b)

    fat_off = 128
    fat_len = (4 * (count + 2) + (1 << SECTOR_SHIFT) - 1) >> SECTOR_SHIFT
    spc = 1 << spc_shift
    heap_off = (fat_off + fat_len + spc - 1) // spc * spc
    vol_len = heap_off + count * spc
    boot = bytearray(1 << SECTOR_SHIFT)
    boot[0:3] = b"\xEB\x76\x90"
    boot[3:11] = b"EXFAT   "
    struct.pack_into("<QQIIIIIIHHBBBBB", boot, 64, 0, vol_len, fat_off, fat_len, heap_off, count,
                     runs["dir:"][0][0], GEN_SERIAL, 0x0100, 0, SECTOR_SHIFT, spc_shift, 1, 0x80, 0)
    boot[120:510] = b"\xF4" * 390
    boot[510:512] = b"\x55\xAA"
    region = bytearray(boot)
    for _ in range(8):
        ext = bytearray(1 << SECTOR_SHIFT)
        ext[-4:] = b"\x00\x00\x55\xAA"
        region += ext
    region += bytes(2 << SECTOR_SHIFT)
    region += struct.pack("<I", _rot32(region, skip=(106, 107, 112))) * ((1 << SECTOR_SHIFT) // 4)

    # Writes it in disk order: every run of clusters with its owner's next bytes.
    order = sorted(((first, n, key) for key, rs in runs.items() for first, n in rs), key=lambda x: x[0])
    content = {k: Content(pools, 1000 + k, r[2] if k != 0 else 0) for k, r in enumerate(recs) if r[1]}
    hashes = {k: hashlib.sha256() for k in range(len(recs))}
    done = {k: 0 for k in range(len(recs))}   # bytes of the file's allocation written
    t0 = time.time()
    with open(image, "wb") as f:
        f.write(region + region)
        f.write(bytes((fat_off << SECTOR_SHIFT) - f.tell()))
        f.write(fat.tobytes())
        f.write(bytes((heap_off << SECTOR_SHIFT) - f.tell()))
        for first, n, key in order:
            assert f.tell() == (heap_off << SECTOR_SHIFT) + (first - 2) * cs
            room = n * cs
            if not isinstance(key, int):
                piece = meta[key][done.setdefault(key, 0):][:room]
                done[key] += room
                f.write(piece + bytes(room - len(piece)))
                continue
            _, size, vdl, _, _ = recs[key]
            while room:
                at, k = done[key], min(room, 16 * MIB)
                real = max(0, min(k, size - at))
                valid = max(0, min(real, vdl - at))
                data = pj[at:at + valid] if key == 0 else content[key].take(valid)
                hashes[key].update(data)
                hashes[key].update(bytes(real - valid))   # beyond ValidDataLength the reader gives zeros
                f.write(data + b"\xAA" * (real - valid) + bytes(k - real))
                done[key] += k
                room -= k
        f.write(bytes((count + 2 - nxt[0]) * cs))
        assert f.tell() == vol_len << SECTOR_SHIFT
    with open(image + ".sha256.tsv", "w", encoding="utf-8", newline="\n") as m:
        for k in sorted(range(len(recs)), key=lambda k: recs[k][0].encode()):
            m.write(f"{recs[k][1]}\t{hashes[k].hexdigest()}\t{recs[k][0]}\n")
    nfrag = sum(1 for r in recs if r[4])
    extents = sum(len(runs[k]) for k, r in enumerate(recs) if r[4] and r[1])
    print(f"{len(recs)} files ({nfrag} fragmented into {extents} runs), {len(children)} folders, "
          f"{(vol_len << SECTOR_SHIFT) / 1e9:.2f} GB image, {cs >> 10} KiB clusters, "
          f"in {time.time() - t0:.0f} s: {image}")


# ---- the system's cache --------------------------------------------------------------------------


def _inputs(path):
    if os.path.isfile(path):
        return [path]
    return sorted(os.path.join(r, n) for r, _, ns in os.walk(path) for n in ns)


def _generated(path):
    """Whether path is an input this script made: evict rewrites what it is given."""
    try:
        if os.path.isfile(path):
            with open(path, "rb") as f:
                b = f.read(512)
            return len(b) == 512 and b[3:11] == b"EXFAT   " and struct.unpack_from("<I", b, 100)[0] == GEN_SERIAL
        with open(os.path.join(path, "sce_sys", "param.json"), "rb") as f:
            return b'"titleId":"TEST00042"' in f.read()
    except OSError:
        return False


def evict(path):
    if not _generated(path):
        sys.exit(f"{path} was not made by this script: evict only rewrites generated inputs")
    _evict(path)


if os.name == "nt":
    import ctypes
    from ctypes import wintypes

    _kk = ctypes.WinDLL("kernel32", use_last_error=True)
    _kk.CreateFileW.restype = wintypes.HANDLE
    _kk.CreateFileW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, wintypes.DWORD, ctypes.c_void_p,
                                wintypes.DWORD, wintypes.DWORD, wintypes.HANDLE]
    _kk.VirtualAlloc.restype = ctypes.c_void_p
    _kk.VirtualAlloc.argtypes = [ctypes.c_void_p, ctypes.c_size_t, wintypes.DWORD, wintypes.DWORD]
    _kk.VirtualFree.argtypes = [ctypes.c_void_p, ctypes.c_size_t, wintypes.DWORD]
    _kk.ReadFile.argtypes = [wintypes.HANDLE, ctypes.c_void_p, wintypes.DWORD,
                             ctypes.POINTER(wintypes.DWORD), ctypes.c_void_p]
    _kk.WriteFile.argtypes = [wintypes.HANDLE, ctypes.c_void_p, wintypes.DWORD,
                              ctypes.POINTER(wintypes.DWORD), ctypes.c_void_p]
    _kk.SetFilePointerEx.argtypes = [wintypes.HANDLE, ctypes.c_longlong, ctypes.c_void_p, wintypes.DWORD]
    _kk.CloseHandle.argtypes = [wintypes.HANDLE]

    def _evict_file(path, buf, piece):
        st = os.stat(path)
        size = st.st_size // 4096 * 4096   # the unaligned tail (< 4 KiB) stays cached
        if not size:
            return
        h = _kk.CreateFileW(path, 0xC0000000, 7, None, 3, 0x20000000 | 0x80000000, None)
        if h in (None, wintypes.HANDLE(-1).value):
            raise OSError(ctypes.get_last_error(), "cannot open", path)
        try:
            n = wintypes.DWORD()
            for off in range(0, size, piece):
                k = min(piece, size - off)
                for op in (_kk.ReadFile, _kk.WriteFile):
                    _kk.SetFilePointerEx(h, off, None, 0)
                    if not op(h, buf, k, ctypes.byref(n), None) or n.value != k:
                        raise OSError(ctypes.get_last_error(), "uncached I/O", path)
        finally:
            _kk.CloseHandle(h)
        # the dates go in the output: the rewrite must not change them
        os.utime(path, ns=(st.st_atime_ns, st.st_mtime_ns))

    def _evict(path):
        piece = 8 * MIB
        buf = _kk.VirtualAlloc(None, piece, 0x3000, 4)
        try:
            for name in _inputs(path):
                _evict_file(name, buf, piece)
        finally:
            _kk.VirtualFree(buf, 0, 0x8000)
        # A rewritten file is scanned again by the antivirus the first time it is opened (about 10 ms
        # per file here, far more than reading it): open each one now so the measured run does not
        # pay for that, as it would not with a game that was copied once and left alone.
        for name in _inputs(path):
            with open(name, "rb", buffering=0):
                pass
else:
    def _evict(path):
        for name in _inputs(path):
            fd = os.open(name, os.O_RDONLY)
            try:
                if hasattr(os, "posix_fadvise"):
                    os.posix_fadvise(fd, 0, 0, os.POSIX_FADV_DONTNEED)
            finally:
                os.close(fd)


# ---- measuring a run ---------------------------------------------------------------------------


def input_bytes(path):
    return sum(os.path.getsize(n) for n in _inputs(path))


def warm(path):
    """Reads the whole input once so that every run finds it in the system's cache."""
    for name in _inputs(path):
        with open(name, "rb") as f:
            while f.read(16 * MIB):
                pass


if os.name == "nt":
    import ctypes
    from ctypes import wintypes

    class _PMC(ctypes.Structure):
        _fields_ = [("cb", wintypes.DWORD), ("PageFaultCount", wintypes.DWORD),
                    ("PeakWorkingSetSize", ctypes.c_size_t), ("WorkingSetSize", ctypes.c_size_t),
                    ("QuotaPeakPagedPoolUsage", ctypes.c_size_t), ("QuotaPagedPoolUsage", ctypes.c_size_t),
                    ("QuotaPeakNonPagedPoolUsage", ctypes.c_size_t), ("QuotaNonPagedPoolUsage", ctypes.c_size_t),
                    ("PagefileUsage", ctypes.c_size_t), ("PeakPagefileUsage", ctypes.c_size_t)]

    _k32 = ctypes.WinDLL("kernel32", use_last_error=True)
    _psapi = ctypes.WinDLL("psapi", use_last_error=True)

    def _cpu_seconds(proc):
        c, x, k, u = (wintypes.FILETIME() for _ in range(4))
        if not _k32.GetProcessTimes(wintypes.HANDLE(proc._handle), ctypes.byref(c), ctypes.byref(x),
                                    ctypes.byref(k), ctypes.byref(u)):
            return None
        ft = lambda t: ((t.dwHighDateTime << 32) | t.dwLowDateTime) / 1e7
        return ft(k) + ft(u)

    def _memory(proc):
        """(working set, private bytes, peak working set, peak private bytes)."""
        pmc = _PMC()
        pmc.cb = ctypes.sizeof(pmc)
        if not _psapi.GetProcessMemoryInfo(wintypes.HANDLE(proc._handle), ctypes.byref(pmc), pmc.cb):
            return None, None, None, None
        return pmc.WorkingSetSize, pmc.PagefileUsage, pmc.PeakWorkingSetSize, pmc.PeakPagefileUsage
else:
    def _cpu_seconds(proc):
        try:
            with open(f"/proc/{proc.pid}/stat") as f:
                fields = f.read().rsplit(")", 1)[1].split()
            return (int(fields[11]) + int(fields[12])) / os.sysconf("SC_CLK_TCK")
        except (OSError, IndexError, ValueError):
            return None

    def _memory(proc):
        try:
            with open(f"/proc/{proc.pid}/status") as f:
                st = dict(line.split(":", 1) for line in f if ":" in line)
            kb = lambda k: int(st[k].split()[0]) * 1024 if k in st else None
            return kb("VmRSS"), None, kb("VmHWM"), None
        except OSError:
            return None, None, None, None


def run_one(cmd):
    """Runs cmd (with --progress json), samples its CPU and memory and times its phases."""
    t0 = time.perf_counter()
    p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    events, result, err = [], {}, []

    def read_out():
        for line in p.stdout:
            try:
                ev = json.loads(line)
            except ValueError:
                continue
            if ev.get("event") == "progress":
                events.append((time.perf_counter() - t0, ev["phase"], ev["done"], ev["total"]))
            elif ev.get("event") == "result":
                result.update(ev)

    readers = [threading.Thread(target=read_out), threading.Thread(target=lambda: err.append(p.stderr.read()))]
    for t in readers:
        t.start()
    samples = []   # (seconds, CPU threads busy, working set, private bytes)
    last_t, last_c, peaks = 0.0, 0.0, (None, None)
    while p.poll() is None:
        time.sleep(0.2)
        c = _cpu_seconds(p)
        ws, private, pws, ppriv = _memory(p)
        now = time.perf_counter() - t0
        if c is not None and now > last_t:
            samples.append((now, (c - last_c) / (now - last_t), ws, private))
            last_t, last_c = now, c
        if pws is not None:
            peaks = (pws, ppriv)
    wall = time.perf_counter() - t0
    cpu = None
    if os.name == "nt":
        cpu = _cpu_seconds(p)
        peaks = _memory(p)[2:]
    for t in readers:
        t.join()
    if cpu is None:   # POSIX: the children's times, now that it was reaped
        import resource
        ru = resource.getrusage(resource.RUSAGE_CHILDREN)
        cpu = ru.ru_utime + ru.ru_stime - getattr(run_one, "spent", 0.0)
        run_one.spent = ru.ru_utime + ru.ru_stime
    if p.returncode != 0:
        sys.stderr.write((err[0] if err else b"").decode(errors="replace"))
    return dict(rc=p.returncode, wall=wall, cpu=cpu, samples=samples, events=events, result=result,
                peak_ws=peaks[0], peak_private=peaks[1])


def phases(r):
    """Splits a run at its packing events: the loose files before them, the packs, the rest after."""
    pack = [e for e in r["events"] if e[1] == "packing"]
    if not pack:
        return {"loose": (0.0, r["wall"])}
    t_end = next((e[0] for e in pack if e[2] >= e[3]), pack[-1][0])
    return {"loose": (0.0, pack[0][0]), "packs": (pack[0][0], t_end), "rest": (t_end, r["wall"])}


def phase_stats(r, a, b, ncpu):
    """(seconds, average CPU %, peak working set, peak private bytes) of the samples in [a, b]."""
    inside = [s for s in r["samples"] if a < s[0] <= b + 0.2]
    cpu = 100.0 * sum(s[1] for s in inside) / len(inside) / ncpu if inside else float("nan")
    ws = max((s[2] for s in inside if s[2] is not None), default=None)
    private = max((s[3] for s in inside if s[3] is not None), default=None)
    return b - a, cpu, ws, private


def hash_output(path):
    h = hashlib.sha256()
    if os.path.isfile(path):
        names = [path]
    else:
        names = sorted(os.path.join(r, n) for r, _, ns in os.walk(path) for n in ns)
    for name in names:
        if name != path:
            h.update(os.path.relpath(name, path).replace("\\", "/").encode() + b"\0")
        with open(name, "rb") as f:
            while True:
                b = f.read(16 * MIB)
                if not b:
                    break
                h.update(b)
    return h.hexdigest()


def output_size(path):
    return os.path.getsize(path) if os.path.isfile(path) else input_bytes(path)


def remove(path):
    if os.path.isdir(path):
        shutil.rmtree(path)
    elif os.path.exists(path):
        os.remove(path)


def bench(args):
    ncpu = os.cpu_count() or 1
    size = input_bytes(args.input)
    print(f"input: {size / 1e9:.2f} GB, {ncpu} CPU threads, output {args.format}, "
          f"{'cold (evicted before every run)' if args.cold else 'in the cache'}")
    if not args.cold:
        print("warming the system's cache...")
        warm(args.input)
    os.makedirs(args.out, exist_ok=True)
    rows, same = [], []
    mib = lambda v: "n/a" if v is None else f"{v / MIB:.0f}"
    nan = float("nan")
    for level in [int(x) for x in args.levels.split(",")]:
        hashes = {}
        for name, exe in ((args.old_name, args.old), ("new", args.new)):
            out = os.path.join(args.out, f"bench-{name}-l{level}" + (".p5z" if args.format == "p5z" else ""))
            remove(out)
            cmd = [exe, "pack", args.input, "--" + args.format, "-o", out, "--level", str(level),
                   "--progress", "json"]
            if args.threads:
                cmd += ["--threads", str(args.threads)]
            if args.cold:
                evict(args.input)
            r = run_one(cmd)
            if r["rc"] != 0:
                sys.exit(f"{name} failed with {r['rc']}")
            hashes[name] = hash_output(out)
            osize = output_size(out)
            remove(out)
            st = {k: phase_stats(r, a, b, ncpu) for k, (a, b) in phases(r).items()}
            loose = st["loose"]
            pk = st.get("packs", (nan, nan, None, None))
            packed = r["result"].get("packed_bytes", 0)
            pack_mbs = packed / pk[0] / 1e6 if pk[0] == pk[0] and pk[0] > 0 else nan
            avg = 100.0 * r["cpu"] / (r["wall"] * ncpu)
            rows.append((level, name, r["wall"], size / r["wall"] / 1e6, avg, loose[0], pk[0], pack_mbs, pk[1],
                         mib(loose[2]), mib(pk[2]), mib(r["peak_ws"]), mib(r["peak_private"]), osize))
            print(f"  level {level} {name:5}: {r['wall']:6.1f} s, {size / r['wall'] / 1e6:6.1f} MB/s, CPU {avg:5.1f} %"
                  f" | loose files {loose[0]:5.1f} s | packs {pk[0]:5.1f} s, {pack_mbs:6.1f} MB/s, CPU {pk[1]:5.1f} %")
        same.append((level, hashes[args.old_name] == hashes["new"], hashes["new"]))
        print(f"  level {level}: {'identical output' if same[-1][1] else 'OUTPUTS DIFFER'}")
    print()
    print("| level | engine | total s | MB/s | avg CPU | loose files s | packs s | packs MB/s | packs avg CPU "
          "| peak WS loose / packs MiB | peak WS MiB | peak private MiB | output GB |")
    print("|---|---|---|---|---|---|---|---|---|---|---|---|---|")
    for (level, name, wall, mbs, avg, lt, pt, pmbs, pcpu, lws, pws, ws, priv, osize) in rows:
        print(f"| {level} | {name} | {wall:.1f} | {mbs:.1f} | {avg:.1f} % | {lt:.1f} | {pt:.1f} | {pmbs:.1f} "
              f"| {pcpu:.1f} % | {lws} / {pws} | {ws} | {priv} | {osize / 1e9:.3f} |")
    for level, ok, h in same:
        print(f"level {level}: {'identical bytes' if ok else 'DIFFERENT BYTES'}, SHA-256 {h}")
    if not all(ok for _, ok, _ in same):
        sys.exit(1)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    g = sub.add_parser("gen")
    g.add_argument("folder")
    g.add_argument("--gb", type=float, default=3.0)
    x = sub.add_parser("gen-exfat")
    x.add_argument("image")
    x.add_argument("--gb", type=float, default=8.0)
    x.add_argument("--cluster-kib", type=int, default=32)
    v = sub.add_parser("evict")
    v.add_argument("input")
    r = sub.add_parser("run")
    r.add_argument("input")
    r.add_argument("--old", required=True)
    r.add_argument("--new", required=True)
    r.add_argument("--out", required=True)
    r.add_argument("--levels", default="19,3")
    r.add_argument("--format", choices=("p5z", "folder"), default="p5z")
    r.add_argument("--threads", type=int, default=0)
    r.add_argument("--cold", action="store_true")
    r.add_argument("--old-name", default="old")
    a = ap.parse_args()
    if a.cmd == "gen":
        gen(a.folder, a.gb)
    elif a.cmd == "gen-exfat":
        gen_exfat(a.image, a.gb, a.cluster_kib)
    elif a.cmd == "evict":
        evict(a.input)
    else:
        bench(a)


if __name__ == "__main__":
    main()
