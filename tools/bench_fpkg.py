#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""PS5 package benchmark: one synthetic game as a UFS2 image (.ffpkg) and as PS5 debug packages
(.pkg, Kraken and stored), read and packed by maxpack.

  bench_fpkg.py gen <folder> --tool <mp_fpkgtool> [--gb 3]
      Writes <folder>/game/ (a synthetic game of about that many GB, deterministic), game.ffpkg (its
      UFS2 image with ShadowMountPlus's 64 KiB profile), game.pkg (a debug package, Kraken as the
      package writers make it: 128 KiB pieces, each the file's last one stored) and game_stored.pkg
      (the same, stored). The files are made of 128 KiB pieces of noise (stored in the package: it
      does not compress), text and structured binary data (slices of bench_pack.py's pools, each
      piece with 8 bytes of its own at the start) and zeros; the text and binary pieces are encoded
      once each (in parallel, with tests/tools/fpkg_kraken.py), so a package of GB is quick to write.

  bench_fpkg.py run <folder> --maxpack <maxpack> --readbench <mp_readbench> [--levels 3,19]
                    [--threads N] [--repeat 3]
      Reads each form the way the packer does (mp_readbench, best of --repeat runs, after a warm-up)
      and packs each to a .p5z at each level, printing seconds, MB/s (of the game), average CPU and
      peak memory, and whether every .p5z has the same bytes (it must: the same files, dates included).

Never point it at real games: it only measures generated data.
"""
import argparse
import concurrent.futures
import hashlib
import importlib.util
import os
import random
import re
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
TOOLS = os.path.join(HERE, "..", "tests", "tools")
MIB = 1 << 20
PIECE = 0x20000
CID = "UP0000-PPSA00000_00-MAXPACKBENCH0000"
MTIME = 1714979290


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


bench_pack = load(os.path.join(HERE, "bench_pack.py"), "bench_pack")
kraken = load(os.path.join(TOOLS, "fpkg_kraken.py"), "fpkg_kraken")


def _encode(piece):
    return kraken.encode_block(piece, 0, seed_free=True)


def bases(rng):
    """The text and binary pieces the game is made of (each starting with 8 bytes of 0x80)."""
    pools = bench_pack.Pools(rng)
    out = []
    for pool in (pools.text, pools.struct):
        for i in range(len(pool) // PIECE):
            out.append(b"\x80" * 8 + pool[i * PIECE + 8:(i + 1) * PIECE])
    return out


def gen(folder, gb, tool):
    os.makedirs(folder, exist_ok=True)
    rng = random.Random(106)
    base = bases(rng)
    t0 = time.perf_counter()
    with concurrent.futures.ProcessPoolExecutor() as ex:
        enc = list(ex.map(_encode, base, chunksize=4))
    print(f"{len(base)} pieces encoded in {time.perf_counter() - t0:.0f} s")
    cache = {b[8:]: e for b, e in zip(base, enc) if e}
    zero = bytes(PIECE)
    cache[zero[8:]] = kraken.encode_block(zero)
    counter = [0]

    def piece(kind):
        if kind == "noise":
            return rng.randbytes(PIECE)
        if kind == "zeros":
            return zero
        b = base[rng.randrange(len(base) // 2) + (0 if kind == "text" else len(base) // 2)]
        counter[0] += 1
        c = counter[0]
        return bytes(0x80 | ((c >> (7 * k)) & 0x7F) for k in range(8)) + b[8:]

    # sizes: a big executable, large files, medium ones and many small ones, about gb in all
    total = int(gb * 1e9)
    sizes = [("eboot.bin", 40 * MIB)]
    left = total - 40 * MIB
    for i in range(40):
        sizes.append(("data/big%02d.bin" % i, int(left * 0.65 / 40)))
    med = int(left * 0.3)
    i = 0
    while med > 0:
        n = rng.randint(64 << 10, 1 << 20)
        sizes.append(("data/medium/%03d/m%05d.dat" % (i // 200, i), n))
        med -= n
        i += 1
    small = int(left * 0.05)
    i = 0
    while small > 0:
        n = rng.randint(200, 60000)
        sizes.append(("data/small/%02d/s%05d.json" % (i // 500, i), n))
        small -= n
        i += 1
    game = os.path.join(folder, "game")
    files = [("sce_sys/param.json", b'{"contentId": "' + CID.encode() + b'", "titleId": "PPSA00000", '
                                    b'"contentVersion": "01.000.000"}'),
             ("sce_sys/icon0.png", b"\x89PNG\r\n\x1a\n" + rng.randbytes(50000)),
             ("sce_sys/keystone", b"keystone\x03\x00" + bytes(22) + rng.randbytes(64))]
    for path, data in files:
        write(game, path, data)
    for path, n in sizes:
        full = os.path.join(game, *path.split("/"))
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "wb") as f:
            if path == "eboot.bin":
                f.write(b"\x7fELF")
                n -= 4
            while n > 0:
                kind = rng.choices(("noise", "text", "struct", "zeros"), (40, 25, 25, 10))[0]
                p = piece(kind)[:n]
                f.write(p)
                n -= len(p)
        os.utime(full, (MTIME, MTIME))
    for path, _ in files:
        os.utime(os.path.join(game, *path.split("/")), (MTIME, MTIME))
    paths = [p for p, _ in files] + [p for p, _ in sizes]
    print(f"game: {len(paths)} files, {sum(os.path.getsize(os.path.join(game, *p.split('/'))) for p in paths) / 1e9:.2f} GB")

    build = load(os.path.join(TOOLS, "fpkg_build.py"), "fpkg_build")
    build.use_helper(tool, folder)

    def encode(p, index):
        e = cache.get(p[8:]) if len(p) == PIECE else None
        return (p[:8] + e[0][8:], e[1], e[2]) if e else None

    disk = [(p, build.DiskFile(os.path.join(game, *p.split("/")))) for p in paths]
    for name, k in (("game.pkg", True), ("game_stored.pkg", False)):
        t0 = time.perf_counter()
        build.package(disk, CID, kraken_on=k, t_sec=MTIME, out=os.path.join(folder, name), encode=encode)
        print(f"{name}: {os.path.getsize(os.path.join(folder, name)) / 1e9:.2f} GB in {time.perf_counter() - t0:.0f} s")
    ufs2 = load(os.path.join(TOOLS, "make_ufs2.py"), "make_ufs2")
    t0 = time.perf_counter()
    data = [(p, open(os.path.join(game, *p.split("/")), "rb").read()) for p in paths]
    with open(os.path.join(folder, "game.ffpkg"), "w+b") as f:
        ufs2.build(data, bsize=65536, fsize=65536, out=f)
    print(f"game.ffpkg: {os.path.getsize(os.path.join(folder, 'game.ffpkg')) / 1e9:.2f} GB in "
          f"{time.perf_counter() - t0:.0f} s")


def write(root, path, data):
    full = os.path.join(root, *path.split("/"))
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "wb") as f:
        f.write(data)


def readbench(tool, src, threads, repeat):
    best = None
    for k in range(repeat + 1):   # the first warms the cache
        cmd = [tool, src] + (["--threads", str(threads)] if threads else [])
        p = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        out = p.stdout.decode(errors="replace")
        m = re.search(r"([0-9.]+) MB/s", out)
        if p.returncode != 0 or not m:
            print(out)
            return None
        if k and (best is None or float(m.group(1)) > best):
            best = float(m.group(1))
    return best


def run(args):
    forms = ["game.ffpkg", "game_stored.pkg", "game.pkg"]
    game_bytes = bench_pack.input_bytes(os.path.join(args.folder, "game"))
    ncpu = os.cpu_count() or 1
    print(f"game: {game_bytes / 1e9:.2f} GB; {ncpu} CPU threads")
    for f in forms:
        mbs = readbench(args.readbench, os.path.join(args.folder, f), args.threads, args.repeat)
        size = os.path.getsize(os.path.join(args.folder, f))
        print(f"read {f:16s} ({size / 1e9:.2f} GB file): {mbs} MB/s of the game")
    hashes = {}
    for level in [int(x) for x in args.levels.split(",")]:
        for f in forms:
            dst = os.path.join(args.folder, "out.p5z")
            bench_pack.remove(dst)
            cmd = [args.maxpack, "pack", os.path.join(args.folder, f), "-o", dst, "--p5z", "--level", str(level),
                   "--progress", "json"] + (["--threads", str(args.threads)] if args.threads else [])
            r = bench_pack.run_one(cmd)
            h = hashlib.sha256(open(dst, "rb").read()).hexdigest() if r["rc"] == 0 else None
            hashes.setdefault(level, set()).add(h)
            cpu = 100.0 * r["cpu"] / r["wall"] / ncpu if r["cpu"] else float("nan")
            peak = (r["peak_private"] or r["peak_ws"] or 0) / MIB
            print(f"pack level {level:2d} {f:16s}: {r['wall']:6.1f} s, {game_bytes / 1e6 / r['wall']:7.1f} MB/s, "
                  f"CPU {cpu:4.0f} %, peak {peak:6.0f} MiB, .p5z {os.path.getsize(dst) / 1e9 if h else 0:.3f} GB")
            bench_pack.remove(dst)
        print(f"  level {level}: the .p5z are {'the same' if len(hashes[level]) == 1 else 'DIFFERENT'}")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    g = sub.add_parser("gen")
    g.add_argument("folder")
    g.add_argument("--gb", type=float, default=3.0)
    g.add_argument("--tool", required=True)
    r = sub.add_parser("run")
    r.add_argument("folder")
    r.add_argument("--maxpack", required=True)
    r.add_argument("--readbench", required=True)
    r.add_argument("--levels", default="3,19")
    r.add_argument("--threads", type=int, default=0)
    r.add_argument("--repeat", type=int, default=3)
    args = ap.parse_args()
    if args.cmd == "gen":
        gen(args.folder, args.gb, args.tool)
    else:
        run(args)


if __name__ == "__main__":
    sys.exit(main())
