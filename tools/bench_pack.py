#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Packing benchmark: two maxpack builds on the same generated input, compared.

  bench_pack.py gen <folder> [--gb 3]
      Writes a synthetic game of about that many GB (deterministic): a few loose files, many small
      files, some medium ones and a few large ones, each made of pieces of incompressible noise,
      text, structured binary data and zeros.

  bench_pack.py run <input> --old <maxpack> --new <maxpack> --out <folder>
                    [--levels 19,3] [--format p5z|folder] [--threads N]
      Packs the input with each build at each level and prints, per run: seconds and MB/s (of the
      input), average CPU (of all the machine's threads), the same for the packs phase alone (timed
      with --progress json; the loose files of a .p5z are always zstd 19 frames of up to 32 MiB),
      peak memory and whether both builds wrote the same bytes. Outputs are deleted once hashed.

Never point it at real games: it only measures generated data.
"""
import argparse
import array
import hashlib
import json
import os
import random
import shutil
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


# ---- measuring a run ---------------------------------------------------------------------------


def input_bytes(folder):
    total = 0
    for root, _, names in os.walk(folder):
        for n in names:
            total += os.path.getsize(os.path.join(root, n))
    return total


def warm(folder):
    """Reads the whole input once so that every run finds it in the system's cache."""
    for root, _, names in os.walk(folder):
        for n in names:
            with open(os.path.join(root, n), "rb") as f:
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
    print(f"input: {size / 1e9:.2f} GB, {ncpu} CPU threads, output {args.format}")
    print("warming the system's cache...")
    warm(args.input)
    os.makedirs(args.out, exist_ok=True)
    rows, same = [], []
    mib = lambda v: "n/a" if v is None else f"{v / MIB:.0f}"
    nan = float("nan")
    for level in [int(x) for x in args.levels.split(",")]:
        hashes = {}
        for name, exe in (("1.0.1", args.old), ("new", args.new)):
            out = os.path.join(args.out, f"bench-{name}-l{level}" + (".p5z" if args.format == "p5z" else ""))
            remove(out)
            cmd = [exe, "pack", args.input, "--" + args.format, "-o", out, "--level", str(level),
                   "--progress", "json"]
            if args.threads:
                cmd += ["--threads", str(args.threads)]
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
        same.append((level, hashes["1.0.1"] == hashes["new"], hashes["new"]))
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
    r = sub.add_parser("run")
    r.add_argument("input")
    r.add_argument("--old", required=True)
    r.add_argument("--new", required=True)
    r.add_argument("--out", required=True)
    r.add_argument("--levels", default="19,3")
    r.add_argument("--format", choices=("p5z", "folder"), default="p5z")
    r.add_argument("--threads", type=int, default=0)
    a = ap.parse_args()
    if a.cmd == "gen":
        gen(a.folder, a.gb)
    else:
        bench(a)


if __name__ == "__main__":
    main()
