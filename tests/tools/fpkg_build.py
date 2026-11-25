#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""A PS5 debug package (FPKG) writer for MaxPack's tests: package(files, content_id, ...).

The layout of pearlxcore/ProsperoPkgTool's builder (MIT; Containers/ProsperoDebugPackageBuilder.cs,
ProsperoInnerImageAssembler.cs, ProsperoPfsv3Writer.cs, ProsperoNapsPlanGenerator.cs,
ProsperoNapsLayoutBuilder.cs, ProsperoOuterPfsBuilder.cs, ProsperoCntBuilder.cs,
ProsperoFihBuilder.cs, Crypto/ProsperoKeys.cs, Crypto/ProsperoEntryCipher.cs), as
fthdgn-gh/Alembic's src/core/fpkg_*_build.cpp port it (GPL-3.0-or-later):

  FIH header (64 KiB) | outer PFS | CNT | SI (a ZIP)

The outer PFS is data first: pfs_image.dat's blocks, naps_pkg_layout.dat's, the plaintext
superblock, the S32 inode table, the super-root, a flat path table, indirect blocks and uroot; every
block but the superblock is AES-128-XTS (one unit per 64 KiB block; pfs_image.dat's data blocks with
the block index as the sector, the rest with 0x800000000000 | index) with keys from the content ID
and the passcode. pfs_image.dat is the inner image: the files' payloads in 128 KiB pieces, stored or
Kraken (fpkg_kraken.py), a block-info table, and the metadata (a PFS of 64 KiB blocks) in a PFSv3
container; naps_pkg_layout.dat maps the mount (the uncompressed PFS) onto it. The CNT keeps
param.json, the images and the other sce_sys files a dump has, some AES-CBC encrypted.

No RSA key and no other key material: the entries a real builder signs or wraps with them
(entry-keys, image-key and the header's RSA block) are zeros here, and the digests MaxPack does not
read are computed or left zero. Pure Python (with hashlib's SHA3 and hmac): for packages of a few MB.
"""
import hashlib
import hmac
import importlib.util
import io
import os
import struct
import subprocess
import tempfile
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))


def _load(name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, name + ".py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


kraken = _load("fpkg_kraken")

DEFAULT_PASSCODE = "0" * 32

# mp_fpkgtool (tests/tools/mp_fpkgtool.c), if given: it encrypts the outer PFS with the engine's AES,
# much faster than the pure-Python one below (which stays the reference: self_test checks it).
_helper = {"exe": None, "dir": None}


def use_helper(exe, workdir):
    _helper["exe"] = exe
    _helper["dir"] = workdir

# ---- AES-128 (encryption only: what a writer needs), XTS and CBC ------------------------------


def _xt(a):
    return ((a << 1) ^ 0x1B) & 0xFF if a & 0x80 else a << 1


def _mul(a, b):
    r = 0
    while b:
        if b & 1:
            r ^= a
        a = _xt(a)
        b >>= 1
    return r


def _sbox():
    inv = [0] * 256
    for a in range(1, 256):
        for b in range(1, 256):
            if _mul(a, b) == 1:
                inv[a] = b
                break
    out = []
    for x in range(256):
        b = inv[x]
        s = b
        for i in range(1, 5):
            s ^= ((b << i) | (b >> (8 - i))) & 0xFF
        out.append(s ^ 0x63)
    return out


SBOX = _sbox()
# The round table: the column (2s, s, s, 3s) as a big-endian word, the other three by rotation.
T0 = [(_mul(s, 2) << 24) | (s << 16) | (s << 8) | _mul(s, 3) for s in SBOX]
T1 = [((t >> 8) | (t << 24)) & 0xFFFFFFFF for t in T0]
T2 = [((t >> 16) | (t << 16)) & 0xFFFFFFFF for t in T0]
T3 = [((t >> 24) | (t << 8)) & 0xFFFFFFFF for t in T0]


class Aes:
    def __init__(self, key):
        w = list(struct.unpack(">4I", key))
        rcon = 1
        for i in range(4, 44):
            t = w[i - 1]
            if i % 4 == 0:
                t = ((t << 8) | (t >> 24)) & 0xFFFFFFFF
                t = (SBOX[t >> 24] << 24) | (SBOX[(t >> 16) & 0xFF] << 16) | (SBOX[(t >> 8) & 0xFF] << 8) | \
                    SBOX[t & 0xFF]
                t ^= rcon << 24
                rcon = _xt(rcon)
            w.append(w[i - 4] ^ t)
        self.rk = w

    def encrypt(self, block):
        rk = self.rk
        s0, s1, s2, s3 = struct.unpack(">4I", block)
        s0 ^= rk[0]
        s1 ^= rk[1]
        s2 ^= rk[2]
        s3 ^= rk[3]
        for k in range(4, 40, 4):
            t0 = T0[s0 >> 24] ^ T1[(s1 >> 16) & 0xFF] ^ T2[(s2 >> 8) & 0xFF] ^ T3[s3 & 0xFF] ^ rk[k]
            t1 = T0[s1 >> 24] ^ T1[(s2 >> 16) & 0xFF] ^ T2[(s3 >> 8) & 0xFF] ^ T3[s0 & 0xFF] ^ rk[k + 1]
            t2 = T0[s2 >> 24] ^ T1[(s3 >> 16) & 0xFF] ^ T2[(s0 >> 8) & 0xFF] ^ T3[s1 & 0xFF] ^ rk[k + 2]
            t3 = T0[s3 >> 24] ^ T1[(s0 >> 16) & 0xFF] ^ T2[(s1 >> 8) & 0xFF] ^ T3[s2 & 0xFF] ^ rk[k + 3]
            s0, s1, s2, s3 = t0, t1, t2, t3
        S = SBOX
        o0 = (S[s0 >> 24] << 24 | S[(s1 >> 16) & 0xFF] << 16 | S[(s2 >> 8) & 0xFF] << 8 | S[s3 & 0xFF]) ^ rk[40]
        o1 = (S[s1 >> 24] << 24 | S[(s2 >> 16) & 0xFF] << 16 | S[(s3 >> 8) & 0xFF] << 8 | S[s0 & 0xFF]) ^ rk[41]
        o2 = (S[s2 >> 24] << 24 | S[(s3 >> 16) & 0xFF] << 16 | S[(s0 >> 8) & 0xFF] << 8 | S[s1 & 0xFF]) ^ rk[42]
        o3 = (S[s3 >> 24] << 24 | S[(s0 >> 16) & 0xFF] << 16 | S[(s1 >> 8) & 0xFF] << 8 | S[s2 & 0xFF]) ^ rk[43]
        return struct.pack(">4I", o0, o1, o2, o3)


M128 = (1 << 128) - 1


def xts_encrypt(data_key, tweak_key, sector, unit):
    """One XTS data unit (a multiple of 16 bytes); the sector as a 128-bit little-endian number."""
    t = int.from_bytes(tweak_key.encrypt(sector.to_bytes(16, "little")), "little")
    out = bytearray(len(unit))
    enc = data_key.encrypt
    for i in range(0, len(unit), 16):
        x = int.from_bytes(unit[i:i + 16], "little") ^ t
        y = int.from_bytes(enc(x.to_bytes(16, "little")), "little") ^ t
        out[i:i + 16] = y.to_bytes(16, "little")
        t <<= 1
        if t >> 128:
            t = (t & M128) ^ 0x87
    return bytes(out)


def cbc_encrypt(key, iv, data):
    out = bytearray()
    prev = int.from_bytes(iv, "big")
    for i in range(0, len(data), 16):
        c = key.encrypt((int.from_bytes(data[i:i + 16], "big") ^ prev).to_bytes(16, "big"))
        out += c
        prev = int.from_bytes(c, "big")
    return bytes(out)


def self_test():
    """The standards' vectors (as tests/test_crypto.c has them), so a broken writer shows here."""
    a = Aes(bytes(range(16)))
    assert a.encrypt(bytes.fromhex("00112233445566778899aabbccddeeff")).hex() == "69c4e0d86a7b0430d8cdb78070b4c55a"
    z = Aes(bytes(16))
    assert xts_encrypt(z, z, 0, bytes(32)).hex() == "917cf69ebd68b2ec9b9fe9a3eadda692cd43d2f59598ed858c02c2652fbf922e"
    k1, k2 = Aes(b"\x11" * 16), Aes(b"\x22" * 16)
    assert xts_encrypt(k1, k2, 0x3333333333, b"\x44" * 32).hex() == \
        "c454185e6a16936e39334038acef838bfb186fff7480adc4289382ecd6d394f0"
    c = Aes(bytes.fromhex("2b7e151628aed2a6abf7158809cf4f3c"))
    assert cbc_encrypt(c, bytes(range(16)), bytes.fromhex("6bc1bee22e409f96e93d7e117393172a")).hex() == \
        "7649abac8119b246cee98e9b12e9197d"


# ---- keys (ProsperoKeys.cs, ProsperoEntryCipher.cs) --------------------------------------------


def sha3(b):
    return hashlib.sha3_256(b).digest()


def compute_keys(content_id, passcode, index):
    """SHA3( SHA3(BE32(index)) || SHA3(content ID NUL-padded to 48) || passcode )."""
    cid = content_id.encode("ascii").ljust(48, b"\0")
    return sha3(sha3(struct.pack(">I", index)) + sha3(cid) + passcode.encode("ascii"))


def outer_key_bytes(content_id, passcode, seed):
    """(data key, tweak key) of the outer PFS: HMAC-SHA256(HMAC-SHA256(EKPFS, seed), LE32(1) || seed)
    gives the tweak key (first half) and the data key (second half)."""
    ekpfs = compute_keys(content_id, passcode, 1)
    inner = hmac.new(ekpfs, seed, "sha256").digest()
    k = hmac.new(inner, struct.pack("<I", 1) + seed, "sha256").digest()
    return k[16:], k[:16]


def outer_keys(content_id, passcode, seed):
    dk, tk = outer_key_bytes(content_id, passcode, seed)
    return Aes(dk), Aes(tk)


def entry_encrypt(content_id, passcode, row, data):
    """A protected CNT entry: AES-128-CBC of the data zero-padded to 16; key and IV from its table row."""
    key_index = (struct.unpack_from(">I", row, 12)[0] >> 12) & 0xF
    seed = sha3(row + compute_keys(content_id, passcode, key_index))
    padded = data + bytes(-len(data) % 16)
    return cbc_encrypt(Aes(seed[16:]), seed[:16], padded)


# ---- the inner image ---------------------------------------------------------------------------

BS = 0x10000            # mount block
UBLOCK = 0x40000        # NAPS unit
PIECE = 0x20000         # payload encoding unit
CONTAINER_BLOCK = 0x40000
INODE = 0xA8
IPB = BS // INODE
META_RESERVE = 60
FLT_SEED0, FLT_SEED1, FLT_ROUND = 0x92CA8AAB26A24F51, 0x09BBB761A41BC44D, 0x8000000080008081
M64 = (1 << 64) - 1
SIGNED = 0x800000000000


def up(v, g):
    return (v + g - 1) // g * g


def rotl64(x, n):
    return ((x << n) | (x >> (64 - n))) & M64


def rotr64(x, n):
    return ((x >> n) | (x << (64 - n))) & M64


def flt_hash(s):
    b = s.encode("utf-8")
    n = len(b)
    s0, s1, s2 = FLT_SEED0, rotl64(FLT_SEED0, 11), rotl64(FLT_SEED0, 23)
    tail = 0
    if n:
        off = 0
        a0, a1, a2 = s0, s1, s2
        for _ in range((n - 1) >> 3):
            w = struct.unpack_from("<Q", b, off)[0]
            off += 8
            a0 ^= w
            t18 = rotr64(rotl64(a2 ^ a1, 5) ^ a0, 11)
            t12 = rotl64(rotl64(a2 ^ a0, 17) ^ a1, 11)
            a2 = rotr64(rotl64(a1 ^ a0, 1) ^ a2, 5)
            a0 = ((~t12 & M64) & a2) ^ t18 ^ FLT_ROUND
            a1 = ((~a2 & M64) & t18) ^ t12
            a2 = ((~t18 & M64) & t12) ^ a2
        s0, s1, s2 = a0, a1, a2
        for j in range(((n - 1) & 7) + 1):
            tail |= b[off + j] << (8 * j)
    u16, u11, u6 = s1, s2, tail ^ s0 ^ FLT_SEED1
    u17 = rotl64(u11 ^ u16, 5) ^ u6
    u18 = rotl64(u11 ^ u6, 17) ^ u16
    u11 = rotl64(u16 ^ u6, 1) ^ u11
    return ((~rotl64(u18, 11) & M64) & rotr64(u11, 5)) ^ rotr64(u17, 11) ^ FLT_ROUND


def path_hash(path):
    p = path[1:] if path.startswith("/") else path
    return flt_hash("".join(chr(ord(c) - 32) if "a" <= c <= "z" else c for c in p))


def dirents(entries):
    out = bytearray()
    for ino, typ, name in entries:
        nb = name.encode("utf-8")
        size = up(len(nb) + 17, 8)
        out += struct.pack("<IIII", ino, typ, len(nb), size) + nb + bytes(size - 16 - len(nb))
    return bytes(out)


def flt(entries):
    entries = sorted(entries, key=lambda e: e[0])
    out = bytearray(0x40 + 16 * len(entries))
    struct.pack_into("<I", out, 0, 1)
    out[4] = 0x10
    struct.pack_into("<I", out, 8, 0x40)
    out[0x20:0x24] = b"\x7fFLT"
    struct.pack_into("<IQQ", out, 0x2C, len(entries), FLT_SEED0, FLT_SEED1)
    for i, (h, packed) in enumerate(entries):
        struct.pack_into("<QQ", out, 0x40 + 16 * i, h, packed)
    return bytes(out)


EXEC_MAGIC = (0x1D3D154F, 0xEEF51454, 0x464C457F)


class DiskFile:
    """A file's data read from the disk when it is needed (for big packages): data[a:b] and len()."""

    def __init__(self, path):
        self.path = path
        self.size = os.path.getsize(path)
        self.f = None

    def __len__(self):
        return self.size

    def __getitem__(self, sl):
        a, b = sl.start or 0, min(sl.stop if sl.stop is not None else self.size, self.size)
        if self.f is None:
            self.f = open(self.path, "rb")
        self.f.seek(a)
        data = self.f.read(max(0, b - a))
        if b >= self.size:
            self.f.close()
            self.f = None
        return data


class Sink:
    """Bytes written one after the other into a file (or memory), with what has been written so far."""

    def __init__(self, f):
        self.f = f
        self.size = 0

    def write(self, b):
        self.f.seek(self.size)
        self.f.write(b)
        self.size += len(b)

    def pad_to(self, pos):
        if pos > self.size:
            self.write(bytes(pos - self.size))

    def read(self, off, n):
        self.f.seek(off)
        return self.f.read(n)


class Bytes:
    """Bytes in memory as a source (size and read)."""

    def __init__(self, b):
        self.b = b
        self.size = len(b)

    def read(self, off, n):
        return self.b[off:off + n]


class Dir:
    def __init__(self, name, full, parent):
        self.name, self.full, self.parent = name, full, parent
        self.subdirs, self.files = {}, {}
        self.inode = 0
        self.dirent_offset = -1
        self.dirents = []


class File:
    def __init__(self, name, full, data, parent):
        self.name, self.full, self.data, self.parent = name, full, data, parent
        self.length = len(data)
        self.exec = len(data) >= 4 and struct.unpack_from("<I", bytes(data[0:4]))[0] in EXEC_MAGIC
        self.inode = self.afid = 0
        self.logical = self.on_disk = self.encoded = 0
        self.raw = self.whole_raw = False
        self.dirent_offset = -1
        self.chunk = 0


def bkey(name):
    return name.encode("utf-8")


def items(m):
    return [m[k] for k in sorted(m, key=bkey)]


def pfsv3_container(payload, blocks_out, kraken_on, mode_of):
    """The metadata in a PFSv3 container: a header, sections (a fixed hash, the shuffle table, the
    block boundaries, the blocks' hashes) and the data, each 256 KiB block stored or Kraken."""
    git_hash = bytes([0x23, 0x98, 0x7d, 0x16, 0xc9, 0x20, 0x9a, 0xc7, 0x28, 0x37, 0x19, 0x32, 0x7e, 0x0f, 0x50,
                      0x6b, 0xbc, 0xf4, 0x59, 0xf4])
    shuffle = bytes([4, 4, 0, 0, 0, 0, 0, 0, 2, 2, 4, 0, 0, 0, 0, 0, 1, 1, 6, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1,
                     8, 2, 2, 4, 0, 0, 0, 0, 1, 1, 6, 2, 2, 4, 0, 0, 1, 1, 6, 1, 1, 6, 0, 0, 4, 4, 4, 4, 0, 0, 0, 0])
    n = len(payload)
    nb = max(1, -(-n // CONTAINER_BLOCK))
    pieces = [payload[i * CONTAINER_BLOCK:(i + 1) * CONTAINER_BLOCK] for i in range(nb)]
    encoded = []
    for i, piece in enumerate(pieces):
        e = kraken.encode_block(piece, mode_of(i)) if kraken_on and piece else None
        if e is not None and len(e[0]) >= len(piece):
            e = None
        encoded.append(e)
        if e:
            blocks_out.append((len(e[0]), len(piece), e[1], bool(e[1] & kraken.CHUNK1_NEWLZ), e[2]))
        else:
            blocks_out.append((len(piece), len(piece), 0, False, 0))
    data_size = sum(len(e[0]) if e else len(pieces[i]) for i, e in enumerate(encoded))
    header, d, sections = 0x48, 16, 7
    off1 = header + sections * d
    off2 = up(off1 + len(git_hash), 8)
    off3 = up(off2 + len(shuffle), 8)
    off4 = up(off3 + (nb + 1) * d, 8)
    off5 = up(off4 + nb * 32, 8)
    off6 = up(off5 + nb * d, 8)
    off7 = up(off6, 0x400)
    b = bytearray(off7 + data_size)
    struct.pack_into("<IHHII", b, 0, 0x43534650, 3, sections, CONTAINER_BLOCK, 0x0802)
    struct.pack_into("<QQQ", b, 0x10, 2 | (7 << 8) | (18 << 16), n, len(b))
    for i, (sid, off, size) in enumerate([(1, off1, len(git_hash)), (2, off2, len(shuffle)), (3, off3, (nb + 1) * d),
                                          (4, off4, nb * 32), (5, off5, nb * d), (6, off6, 0), (7, off7, data_size)]):
        p = header + i * d
        struct.pack_into("<HI", b, p, sid, off)
        struct.pack_into("<I", b, p + 10, size)
    b[off1:off1 + len(git_hash)] = git_hash
    b[off2:off2 + len(shuffle)] = shuffle
    plain = stored = 0
    for i, e in enumerate(encoded):
        piece = pieces[i]
        if e:
            flags = e[1]
            hint = min((e[2] if e[1] & kraken.CHUNK1_NEWLZ else len(e[0])) - 1, 0x1FFFF)
            body = e[0]
        else:
            flags = 0x0C | (0xC0 if len(piece) > CONTAINER_BLOCK // 2 else 0)
            hint = min(max(len(piece) - 1, 0), 0x1FFFF)
            body = piece
        struct.pack_into("<QQ", b, off3 + i * d, stored | (flags << 48), plain | (hint << 44))
        b[off4 + 32 * i:off4 + 32 * i + 32] = sha3(piece)
        b[off7 + stored:off7 + stored + len(body)] = body
        plain += len(piece)
        stored += len(body)
    struct.pack_into("<QQ", b, off3 + nb * d, data_size, n)
    h32 = bytearray(32)
    struct.pack_into("<I", h32, 0, 1)
    h32[4:12] = b[8:16]
    h32[16:32] = b[0x10:0x20]
    b[0x28:0x48] = sha3(bytes(h32) + bytes(b[off2:off2 + len(shuffle)]) + bytes(b[off3:off3 + (nb + 1) * d]) +
                        bytes(b[off4:off4 + nb * 32]))
    return bytes(b), off7


def inner_image(files, kraken_on, t_sec, t_nsec, mode_of=lambda i: 0, meta_hook=None, sink=None, encode=None):
    """files: [(path, data)], the inner PFS's (no leading slash). The image and its geometry.
    meta_hook(meta, where), for damaged packages, may change the metadata (a bytearray) before it is
    stored; where has inode(i) (the offset of inode i), dirs and files ({path: inode}) and content
    ({folder path: the offset of its entries}). sink: where the image goes (in memory if None).
    encode(piece, index): the Kraken encoder of the payload (encode_block with mode_of if None)."""
    img = sink or Sink(io.BytesIO())
    if encode is None:
        def encode(piece, index):
            return kraken.encode_block(piece, mode_of(index))
    uroot = Dir("uroot", "", None)
    for path, data in sorted(files, key=lambda f: bkey(f[0])):
        parts = path.split("/")
        d = uroot
        for k, part in enumerate(parts[:-1]):
            if part not in d.subdirs:
                d.subdirs[part] = Dir(part, "/".join(parts[:k + 1]), d)
            d = d.subdirs[part]
        d.files[parts[-1]] = File(parts[-1], "/" + path, data, d)

    def pre(d, out):
        out.append(d)
        for c in items(d.subdirs):
            pre(c, out)
        return out

    def post(d, out):
        for c in items(d.subdirs):
            post(c, out)
        out.append(d)
        return out

    def files_pre(d, out):
        out += items(d.files)
        for c in items(d.subdirs):
            files_pre(c, out)
        return out

    def entry_order(d, excluded, out):
        # at each folder, its subfolders and files merged in name order
        names = sorted([(bkey(n), 0, n) for n in d.subdirs] + [(bkey(n), 1, n) for n in d.files])
        for _, is_file, n in names:
            if is_file:
                out.append(d.files[n])
            elif d.subdirs[n] is not excluded:
                entry_order(d.subdirs[n], excluded, out)
        return out

    dirs = pre(uroot, [])
    nxt = 4   # the super-root and three tables come first
    for d in dirs:
        d.inode = nxt
        nxt += 1
    file_nodes = []
    for d in post(uroot, []):
        for f in items(d.files):
            f.inode = nxt
            nxt += 1
            file_nodes.append(f)

    def rank(f):
        if f.full == "/sce_sys/keystone":
            return 0
        if f.exec:
            return 1
        return 3 if f.full == "/sce_sys/pfs-version.dat" else 2

    sce = uroot.subdirs.get("sce_sys")
    order = sorted(files_pre(sce, []), key=lambda f: (rank(f), bkey(f.full))) if sce else []
    order += entry_order(uroot, sce, [])
    empty = [f for f in order if f.length == 0]
    order = [f for f in order if f.length > 0]

    # Sparse slots: 28 files in the first 256 KiB ublock, 30 in each later one; a full ublock gets a
    # 256 KiB hole (a null slot).
    slots = []
    cursor, current, in_ub = 0, -1, 0
    for f in order:
        ub = cursor // UBLOCK
        if ub != current:
            current, in_ub = ub, 0
        if in_ub >= (28 if current == 0 else 30):
            slots.append(None)
            cursor += UBLOCK
            current, in_ub = cursor // UBLOCK, 0
        f.afid = len(slots)
        f.logical = cursor
        slots.append(f)
        cursor += f.length
        in_ub += 1
    for f in empty:
        f.afid = len(slots) + len(empty) + 1
    prev = None
    for f in order:
        transition = prev is not None and prev != f.chunk
        prev = f.chunk
        f.whole_raw = f.full == "/sce_sys/keystone" or f.exec or transition

    blocks = []   # the payload pieces, in physical order
    pos = 0
    index = 0
    for f in order:
        f.raw = not kraken_on or f.whole_raw
        if f.whole_raw or (f.raw and pos % BS and f.length > BS - pos % BS):
            pos = up(pos, BS)
        f.on_disk = pos
        img.pad_to(pos)
        phys = pos
        off = 0
        while off < f.length:
            n = min(PIECE, f.length - off)
            piece = f.data[off:off + n]
            e = None
            if kraken_on and not f.raw and off + n < f.length:   # not a file's last piece
                e = encode(piece, index)
                index += 1
            if e:
                img.write(e[0])
                blocks.append(dict(logical=f.logical + off, size=n, stored=len(e[0]), flags=e[1], first=e[2], phys=phys))
                phys += len(e[0])
            else:
                img.write(piece)
                blocks.append(dict(logical=f.logical + off, size=n, stored=n, flags=0, first=0, phys=phys))
                phys += n
            off += n
        f.encoded = phys - pos
        pos += f.encoded
        if f.whole_raw:
            pos = up(pos, BS)

    data_end = max([f.logical + f.length for f in order] or [0])
    for f in empty:
        f.logical = data_end
    logical_data_blocks = up(max(data_end, 1), UBLOCK) // BS

    for d in dirs:
        d.dirents = [(d.inode, 4, "."), (d.parent.inode if d.parent else d.inode, 5, "..")]
        d.dirents += [(c.inode, 3, c.name) for c in items(d.subdirs)]
        d.dirents += [(f.inode, 2, f.name) for f in items(d.files)]
        off = 0
        by_name = {}
        for _, _, name in d.dirents:
            by_name[name] = off
            off += up(len(name.encode("utf-8")) + 17, 8)
        for c in d.subdirs.values():
            c.dirent_offset = by_name[c.name]
        for f in d.files.values():
            f.dirent_offset = by_name[f.name]

    def in_sce(d):
        return d.full == "sce_sys" or d.full.startswith("sce_sys/")

    inode_flt, apr_flt = [], []
    for d in dirs:
        if d is not uroot:
            inode_flt.append((path_hash(d.full), (d.inode & 0xFFFFFF) | 0x40000000 | (0x80000000 if in_sce(d) else 0) |
                              (0xFFFFFF << 40)))
    for f in file_nodes:
        apr = not f.full.startswith("/sce_sys/")
        inode_flt.append((path_hash(f.full), (f.inode & 0xFFFFFF) | (0 if apr else 0x80000000) | (f.afid << 40)))
        if apr:
            apr_flt.append((path_hash(f.full), (f.length & 0xFFFFFFFFFF) | (f.afid << 40)))
    content = [dirents([(1, 2, "inode_flat_path_table"), (2, 2, "apr_flat_path_table"), (3, 2, "afid_to_ino_table"),
                        (uroot.inode, 3, "uroot")]),
               flt(inode_flt), flt(apr_flt)]
    t = bytearray(4 * (len(slots) + 3))
    struct.pack_into("<I", t, 0, len(slots) + 2)
    for i, f in enumerate(slots):
        struct.pack_into("<I", t, 4 + 4 * i, f.inode if f else 0xFFFFFFFF)
    struct.pack_into("<II", t, 4 + 4 * len(slots), 0xFFFFFFFF, 0xFFFFFFFF)
    content.append(bytes(t))
    for d in dirs:
        content.append(dirents(d.dirents))

    def alloc(c):
        return max(1, -(-len(c) // BS))

    node_count = 4 + len(dirs) + len(file_nodes)
    inode_blocks = -(-node_count // IPB)
    natural = 1 + inode_blocks + sum(alloc(c) for c in content)
    if natural & 1:
        content.append(b"")   # an even number of blocks
    meta_base_block = logical_data_blocks + META_RESERVE
    meta_base = meta_base_block * BS
    ndblock = meta_base_block + natural + (natural & 1)
    offsets = []
    c = meta_base + (1 + inode_blocks) * BS
    for x in content:
        offsets.append(c)
        c += alloc(x) * BS

    meta = bytearray((1 + inode_blocks + sum(alloc(x) for x in content)) * BS)
    times = (t_sec, t_sec, t_sec, t_sec, t_nsec, t_nsec, t_nsec, t_nsec)
    struct.pack_into("<QQ", meta, 0, 2, 20130315)
    meta[0x1A] = 1
    struct.pack_into("<H", meta, 0x1C, 0x18)
    struct.pack_into("<I", meta, 0x20, BS)
    struct.pack_into("<QQQQ", meta, 0x28, 1, node_count, ndblock, inode_blocks)
    struct.pack_into("<IIII", meta, 0x50, BS, 0x10, BS, BS)
    struct.pack_into("<4Q4I", meta, 0x68, *times)
    struct.pack_into("<Q", meta, 0xB0, 1)
    struct.pack_into("<Q", meta, 0xD8, meta_base_block + 1)
    meta[0x368] = 1

    def inode(i, mode, nlink, flags, size, logical, db1=-1, parent=-1, doff=-1):
        o = (1 + i // IPB) * BS + (i % IPB) * INODE
        struct.pack_into("<HHIQQ", meta, o, mode, nlink, flags, size, size)
        struct.pack_into("<4Q4I", meta, o + 0x18, *times)
        struct.pack_into("<Qiii", meta, o + 0x60, logical, db1, parent, doff)

    inode(0, 0x416D, 1, 0x00020010, alloc(content[0]) * BS, offsets[0])
    for i in (1, 2, 3):
        inode(i, 0x816D, 1, 0x00020010, len(content[i]), offsets[i])
    for j, d in enumerate(dirs):
        root = d is uroot
        sce_d = not root and in_sce(d)
        inode(d.inode, 0x4168 if sce_d else 0x416D, 2 + len(d.subdirs) + (1 if root else 0),
              0x00020010 if sce_d else 0x10, alloc(content[4 + j]) * BS, offsets[4 + j], -1,
              -1 if root else d.parent.inode, -1 if root else d.dirent_offset)
    for f in file_nodes:
        sce_f = f.full.startswith("/sce_sys/")
        inode(f.inode, 0x8168 if sce_f else 0x816D, 1, 0x10 | (0x40 if f.exec else 0x20) | (0x20000 if sce_f else 0),
              f.length, f.logical, f.afid, f.parent.inode, f.dirent_offset)
    c = (1 + inode_blocks) * BS
    for x in content:
        meta[c:c + len(x)] = x
        c += alloc(x) * BS
    if meta_hook:
        where = dict(inode=lambda i: (1 + i // IPB) * BS + (i % IPB) * INODE,
                     dirs={d.full: d.inode for d in dirs}, files={f.full[1:]: f.inode for f in file_nodes},
                     content={d.full: offsets[4 + j] - meta_base for j, d in enumerate(dirs)},
                     superroot=offsets[0] - meta_base, meta_base=meta_base)
        meta_hook(meta, where)

    # The block-info table, then the metadata container.
    be = (0x0027FFFC - (data_end & 0xFFFF) * 4) & 0xFFFFFF
    value = ((be & 0xFF) << 16) | (be & 0xFF00) | ((be >> 16) & 0xFF)
    info = b"".join(struct.pack("<II", 0x00FCFF27 if i < 3 else value, 0x00400003) for i in range(4))
    pos = up(pos, BS)
    img.pad_to(pos)
    block_info = pos
    img.write(info)
    pos += len(info)
    meta_blocks = []
    container, data_section = pfsv3_container(bytes(meta), meta_blocks, kraken_on, lambda i: mode_of(100000 + i))
    pos = up(pos, BS)
    img.pad_to(pos)
    meta_on_disk = pos
    img.write(container)
    return dict(image=img, blocks=blocks, meta_blocks=meta_blocks, data_end=data_end, meta_base=meta_base,
                ndblock=ndblock, block_info=block_info, meta_on_disk=meta_on_disk, data_section=data_section,
                placements=[(f.logical, f.length) for f in order], meta=bytes(meta), inode_blocks=inode_blocks,
                content_inodes=len(dirs) + len(file_nodes),
                app_files=sum(1 for f in file_nodes if not f.full.startswith("/sce_sys/")), empty=len(empty))


# ---- naps_pkg_layout.dat ------------------------------------------------------------------------


def cblock(run, coff, uoff=0, clen=0, even=0, odd=0, kde=0, shuffle=0, tweak=0, c256k=0, key=0, raw2=0, raw8=0):
    """One 9-byte CblockInfo record: an STD (a block) or a RUN (re-bases the compressed cursor)."""
    lo = coff & 0x3FFFF
    if not run:
        lo |= (uoff & 0x3FFFF) << 20 | (clen & 0x1FFFF) << 38 | (even & 7) << 55 | (odd & 7) << 58 | (kde & 7) << 61
        hi = ((kde >> 3) & 7) | (shuffle & 0xF) << 4
    else:
        lo |= 1 << 18 | (tweak & 0xFFFFFFF) << 20 | (key & 3) << 48 | (c256k & 0x3FFF) << 50
        hi = (c256k >> 14) & 0xFF
    lo |= (raw2 & 0x08) << 16
    if not run:
        hi |= raw8 & 0x08
    return struct.pack("<Q", lo & M64) + bytes([hi & 0xFF])


def naps(inner, kraken_on):
    """naps_pkg_layout.dat for an inner image; (blob, number of fidx entries)."""
    plan = []
    state = {"prev_end": -1, "first": True}

    def needs_run(on_disk):
        need = state["first"] or on_disk != state["prev_end"]
        state["first"] = False
        return need

    def emit(e):
        plan.append(e)
        if not e.get("term"):
            state["prev_end"] = e["on_disk"] + e["stream"]

    def kraken_entry(on_disk, logical, size, stream, even_len, flags, multi):
        assert 0 < stream < size
        emit(dict(run=needs_run(on_disk), on_disk=on_disk, logical=logical, even_len=even_len, stream=stream,
                  even=1 | (4 if flags & 2 else 0) | (2 if flags & 1 else 0),
                  odd=((1 if flags & 0x20 == 0 else 4) | (2 if flags & 0x10 else 0)) if multi else 0))

    def stored(on_disk, logical, size, stream):
        emit(dict(run=needs_run(on_disk), on_disk=on_disk, logical=logical, even_len=min(size, 0x20000), stream=stream,
                  even=1, odd=1 if size > 0x20000 else 0, raw=True))

    # 1. the payload pieces, in physical order
    for b in inner["blocks"]:
        if b["flags"]:
            multi = b["size"] > 0x20000
            kraken_entry(b["phys"], b["logical"], b["size"], b["stored"], b["first"] if multi else b["stored"],
                         b["flags"], multi)
        else:
            stored(b["phys"], b["logical"], b["size"], b["stored"])
    # 2. [data end, metadata base): 256 KiB zero blocks deduplicated onto the block-info table
    if inner["meta_base"] > inner["data_end"]:
        padding = inner["meta_base"] - inner["data_end"]
        holes = -(-padding // UBLOCK)
        base = cursor = inner["block_info"]
        for k in range(holes):
            last = k == holes - 1
            on_disk = cursor if last else base
            full = min(UBLOCK, padding - k * UBLOCK) > 0x20000
            stream = 16 if full else 8
            e = dict(on_disk=on_disk, logical=inner["data_end"] + k * UBLOCK, even_len=8, stream=stream, even=1,
                     odd=1 if full else 0)
            e["run"] = (not last) or needs_run(on_disk)
            emit(e)
            cursor = on_disk + stream
    # 3. the metadata: the container's blocks where its data section has them
    logical = inner["meta_base"]
    on_disk = inner["meta_on_disk"] + inner["data_section"]
    for comp, size, flags, multi, first in inner["meta_blocks"]:
        if flags and comp < size:
            kraken_entry(on_disk, logical, size, comp, first if multi else comp, flags, multi)
        else:
            stored(on_disk, logical, size, comp)
        logical += size
        on_disk += comp
    # 4. the terminator at the mount's end
    mount = inner["ndblock"] * BS
    plan.append(dict(run=True, on_disk=on_disk, logical=mount, term=True, stream=0))

    fidx = [lg for lg, n in inner["placements"] if n > 0] + [inner["data_end"], inner["meta_base"], mount]
    entries, std_logical = [], []
    cursor = 0
    for b in plan:
        if b["run"]:
            entries.append(cblock(True, cursor & 0x3FFFF, tweak=0 if b.get("term") else b["on_disk"] >> 16,
                                  c256k=b["on_disk"] >> 18))
            cursor = b["on_disk"]
        if b.get("term"):
            rec = cblock(False, cursor & 0x3FFFF, b["logical"] & 0x3FFFF, 0, 0, 0, raw2=0x08)
        else:
            rec = cblock(False, cursor & 0x3FFFF, b["logical"] & 0x3FFFF, min(b["even_len"] - 1, 0x1FFFF), b["even"],
                         b["odd"], raw8=0x08 if b.get("raw") else 0)
        std_logical.append((len(entries), b["logical"]))
        entries.append(rec)
        cursor += b["stream"]
    num_files = len(fidx)
    num_ublocks = -(-mount // UBLOCK)
    num_outer = -(-inner["image"].size // BS)
    num_cblock = len(entries)
    std_logical.sort(key=lambda x: x[1])
    first = []
    p = 0
    for u in range(num_ublocks):
        while p < len(std_logical) and std_logical[p][1] < u * UBLOCK:
            p += 1
        first.append(std_logical[p][0] if p < len(std_logical) else num_cblock - 1)
    groups = (num_ublocks + 8) // 8
    fidx_off = 16 + num_outer * 8
    u2c_off = fidx_off + num_files * 6
    cbi_off = (num_outer * 8 + 10 * groups + 6 * (num_files - 1) + 29) & ~7
    out = bytearray(up(cbi_off + num_cblock * 9, 16))
    w0 = ((num_files - 1) & 0xFFFFFF) | ((2 if kraken_on else 0) << 24) | ((num_ublocks & 0xFFFFFF) << 32)
    w1 = (num_outer & 0xFFFFFF) | (((num_cblock - 2) & 0xFFFFFF) << 24)
    struct.pack_into("<QQ", out, 0, w0, w1)
    for i, v in enumerate(fidx):
        out[fidx_off + 6 * i:fidx_off + 6 * i + 5] = v.to_bytes(5, "little")
        out[fidx_off + 6 * i + 5] = 0x40 if i == num_files - 1 else 0
    for g in range(groups):
        bu = 8 * g
        base = first[bu] if bu < num_ublocks else num_cblock - 1
        e = u2c_off + 10 * g
        out[e:e + 3] = base.to_bytes(3, "little")
        for i in range(7):
            u = bu + 1 + i
            start = first[u] if u < num_ublocks else num_cblock - 1
            assert start - base <= 255
            out[e + 3 + i] = start - base
    for i, rec in enumerate(entries):
        out[cbi_off + 9 * i:cbi_off + 9 * i + 9] = rec
    return bytes(out), num_files


# ---- the outer PFS ------------------------------------------------------------------------------

S32 = 0x2C8
SIG = 36
PER_INDIRECT = BS // SIG


def dirent_block(entries):
    b = bytearray(BS)
    raw = dirents(entries)
    b[:len(raw)] = raw
    return b


def outer_pfs(files, content_id, passcode, seed, t_sec, t_nsec, plain=False, sink=None, base=0):
    """files: [(name, source (size, read), signed, size_compressed)]. Written into sink at base (the
    data blocks streamed: only their digests are kept); (superblock index, info)."""
    first, counts, kinds = [], [], []
    digest = {}
    nb = 0
    for name, src, signed, _ in files:
        first.append(nb)
        n = max(1, -(-src.size // BS))
        counts.append(n)
        for i in range(n):
            blk = bytes(src.read(i * BS, BS)).ljust(BS, b"\0")
            digest[nb] = sha3(blk)
            sink.f.seek(base + nb * BS)
            sink.f.write(blk)
            kinds.append(signed)
            nb += 1
    data_blocks = nb
    sb, itab, sroot, fltb = data_blocks, data_blocks + 1, data_blocks + 2, data_blocks + 3
    nxt = [fltb + 1]

    def capacity(depth):
        return PER_INDIRECT ** depth

    def allocate(depth, first_data, count):
        node = dict(block=nxt[0], depth=depth, first=first_data, count=count, children=[])
        nxt[0] += 1
        if depth > 1:
            off, left = first_data, count
            while left:
                take = min(left, capacity(depth - 1))
                node["children"].append(allocate(depth - 1, off, take))
                off += take
                left -= take
        return node

    trees = []
    for i in range(len(files)):
        plan, excess, fd = [], max(0, counts[i] - 12), 12
        for level in range(5):
            if not excess:
                break
            take = min(excess, capacity(level + 1))
            plan.append((level, allocate(level + 1, fd, take)))
            fd += take
            excess -= take
        trees.append(plan)
    uroot = nxt[0]
    total = uroot + 1
    meta = {i: bytearray(BS) for i in range(sb, total)}

    def dig(i):
        return digest[i]

    def seal(i):
        digest[i] = sha3(bytes(meta[i]))

    meta[sroot] = dirent_block([(1, 2, "inode_flat_path_table"), (2, 3, "uroot")])
    seal(sroot)
    fb = meta[fltb]
    struct.pack_into("<III", fb, 0, 1, 0x10, 0x40)
    fb[0x20:0x24] = b"\x7fFLT"
    struct.pack_into("<IQQ", fb, 0x2C, len(files), FLT_SEED0, FLT_SEED1)
    for i, f in enumerate(files):
        struct.pack_into("<QQ", fb, 0x40 + 16 * i, path_hash(f[0]), (3 + i) | (i << 40))
    seal(fltb)
    meta[uroot] = dirent_block([(2, 4, "."), (2, 5, "..")] + [(3 + i, 2, f[0]) for i, f in enumerate(files)])
    seal(uroot)

    def serialize(node, file_first):
        b = meta[node["block"]]
        if node["depth"] == 1:
            for i in range(node["count"]):
                db = file_first + node["first"] + i
                b[i * SIG:i * SIG + 32] = dig(db)
                struct.pack_into("<I", b, i * SIG + 32, db)
        else:
            for j, ch in enumerate(node["children"]):
                h = serialize(ch, file_first)
                b[j * SIG:j * SIG + 32] = h
                struct.pack_into("<I", b, j * SIG + 32, ch["block"])
        seal(node["block"])
        return digest[node["block"]]

    t = meta[itab]
    times = (t_sec, t_sec, t_sec, t_sec, t_nsec, t_nsec, t_nsec, t_nsec)

    def meta_inode(num, mode, nlink, flags, size, owned):
        o = num * S32
        struct.pack_into("<HHIQQ", t, o, mode, nlink, flags, size, size)
        struct.pack_into("<4Q4I", t, o + 24, *times)
        struct.pack_into("<I", t, o + 96, 1)
        t[o + 0x64:o + 0x64 + 32] = digest[owned]
        struct.pack_into("<I", t, o + 0x64 + 32, owned)

    meta_inode(0, 0x416D, 1, 0x2000C, BS, sroot)
    meta_inode(1, 0x816D, 1, 0x2000C, 0x40 + 16 * len(files), fltb)
    meta_inode(2, 0x416D, 3, 0x000C, BS, uroot)
    for i, (name, data, signed, size_c) in enumerate(files):
        o = (3 + i) * S32
        struct.pack_into("<HHIQQ", t, o, 0x816D, 1, 0x000D, data.size, size_c)
        struct.pack_into("<4Q4I", t, o + 24, *times)
        struct.pack_into("<I", t, o + 96, counts[i])
        for j in range(min(counts[i], 12)):
            t[o + 0x64 + j * SIG:o + 0x64 + j * SIG + 32] = dig(first[i] + j)
            struct.pack_into("<I", t, o + 0x64 + j * SIG + 32, first[i] + j)
        for level, root in trees[i]:
            h = serialize(root, first[i])
            t[o + 0x214 + level * SIG:o + 0x214 + level * SIG + 32] = h
            struct.pack_into("<I", t, o + 0x214 + level * SIG + 32, root["block"])
    seal(itab)
    s = meta[sb]
    struct.pack_into("<QQ", s, 0, 2, 20130315)
    s[0x1A] = 1
    struct.pack_into("<H", s, 0x1C, 0x0D)
    struct.pack_into("<I", s, 0x20, BS)
    struct.pack_into("<QQQQ", s, 0x28, 1, 3 + len(files), total, 1)
    sig = 0x50
    struct.pack_into("<H", s, sig + 2, 1)
    struct.pack_into("<QQ", s, sig + 8, BS, BS)
    struct.pack_into("<4Q4I", s, sig + 24, *times)
    struct.pack_into("<I", s, sig + 96, 1)
    s[sig + 104:sig + 136] = digest[itab]
    struct.pack_into("<Q", s, sig + 136, itab)
    struct.pack_into("<I", s, 0x36C, 1)
    s[0x370:0x380] = b"PPRPLAIN-NOAUTH!" if plain else seed
    region = bytearray(s[:0x5A0])
    region[0x380:0x3A0] = bytes(32)
    s[0x380:0x3A0] = sha3(bytes(region))
    for i in range(sb, total):
        sink.f.seek(base + i * BS)
        sink.f.write(bytes(meta[i]))
    sink.size = max(sink.size, base + total * BS)
    signed_from = counts[0] if files and not files[0][2] else 0
    if not plain:
        dk, tk = outer_key_bytes(content_id, passcode, seed)
        uniform = all(kinds[i] == (i >= signed_from) for i in range(data_blocks))
        if _helper["exe"] and uniform:
            # the engine's AES, on a file: the package's own if it is one, else a copy of the region
            real = getattr(sink.f, "name", None)
            if isinstance(real, str):
                sink.f.close()   # (Windows: the tool cannot write a file open here)
                path, off = real, base
            else:
                fd, path = tempfile.mkstemp(suffix=".outer", dir=_helper["dir"])
                with os.fdopen(fd, "wb") as f:
                    f.write(sink.read(base, total * BS))
                off = 0
            try:
                subprocess.run([_helper["exe"], "xts", path, str(off), str(total), dk.hex(), tk.hex(), str(signed_from),
                                str(sb)], check=True)
                if path != real:
                    with open(path, "rb") as f:
                        sink.f.seek(base)
                        sink.f.write(f.read())
            finally:
                if path != real:
                    os.remove(path)
                else:
                    sink.f = open(real, "r+b")
        else:
            d, t2 = Aes(dk), Aes(tk)
            for i in range(total):
                if i == sb:
                    continue
                sector = i if (i < data_blocks and not kinds[i]) else (i | SIGNED)
                blk = sink.read(base + i * BS, BS)
                sink.f.seek(base + i * BS)
                sink.f.write(xts_encrypt(d, t2, sector, blk))
    indirect = []

    def walk(node):
        indirect.append(node["block"])
        for ch in node["children"]:
            walk(ch)

    for plan in trees:
        for _, root in plan:
            walk(root)
    return sb, dict(itab=itab, uroot=uroot, total=total, first=first, counts=counts, sb_plain=bytes(meta[sb]),
                    indirect=indirect, data_blocks=data_blocks)


# ---- the CNT and the FIH ------------------------------------------------------------------------

CNT_BODY = 0x2000


def cnt(entries, content_id, passcode, content_type=0x20, outer_size=0, names_hook=None):
    """entries: [(id, name or None, data, flags1, flags2)], in body order. (bytes, {id: (offset, size)})."""
    entries = [list(e) for e in entries]
    count = len(entries)
    named = sorted([e for e in entries if e[1]], key=lambda e: e[1].encode("utf-8"))
    names = bytearray(b"\0")
    name_off = {}
    for e in named:
        name_off[id(e)] = len(names)
        names += e[1].encode("utf-8") + b"\0"
    if names_hook:
        names = names_hook(names)
    for e in entries:
        if e[0] == 0x200:
            e[2] = bytes(names)
        if e[0] in (0x1, 0x100):
            e[2] = bytes(32 * count)

    def stored_len(e):
        return up(len(e[2]), 16) if e[3] & 0x80000000 else len(e[2])

    cursor = CNT_BODY
    offs = {}
    for e in entries:
        offs[id(e)] = cursor
        cursor = up(cursor + stored_len(e), 16)
    image_off = up(cursor, BS)
    srt = sorted(entries, key=lambda e: e[0])
    table = bytearray(32 * count)
    for i, e in enumerate(srt):
        struct.pack_into(">IIIIII", table, 32 * i, e[0], name_off.get(id(e), 0), e[3], e[4], offs[id(e)], len(e[2]))
    for e in entries:
        if e[0] == 0x100:
            e[2] = bytes(table)

    def stored(e):
        if not e[3] & 0x80000000:
            return e[2]
        i = srt.index(e)
        return entry_encrypt(content_id, passcode, bytes(table[32 * i:32 * i + 32]), e[2])

    digests = bytearray(32 * count)
    for i, e in enumerate(srt):
        if e[0] != 0x1:
            digests[32 * i:32 * i + 32] = sha3(stored(e))
    for e in entries:
        if e[0] == 0x1:
            e[2] = bytes(digests)
    body = bytearray(image_off)
    for e in entries:
        data = stored(e)
        body[offs[id(e)]:offs[id(e)] + len(data)] = data
    h = body
    h[0:4] = b"\x7fCNT"
    struct.pack_into(">HHII", h, 4, 2, 1, 0x80000000, 0x0C)
    metas = next(e for e in entries if e[0] == 0x100)
    struct.pack_into(">IHHII", h, 0x10, count, 6, count, offs[id(metas)],
                     sum(len(e[2]) for e in entries if e[0] in (0x1, 0x10, 0x20, 0x80, 0x100)))
    struct.pack_into(">QQ", h, 0x20, CNT_BODY, image_off - CNT_BODY)
    h[0x40:0x40 + len(content_id)] = content_id.encode("ascii")
    struct.pack_into(">III", h, 0x70, 0, content_type, 0x02020000)
    struct.pack_into(">I", h, 0x7C, image_off)
    h[0x200:0x200 + len(content_id)] = content_id.encode("ascii")
    struct.pack_into(">II", h, 0x400, 1, 1)
    struct.pack_into(">QQQ", h, 0x408, 0xA00000000000030C, BS, outer_size)
    h[0xFE0:0x1000] = sha3(bytes(h[:0xFE0]))
    return bytes(body), {e[0]: (offs[id(e)], len(e[2])) for e in entries}


def fih_header(outer_size, sb_index, game_digest, naps_blob, inner):
    h = bytearray(BS)
    h[0:4] = b"\x7fFIH"
    h[4] = 1
    struct.pack_into("<HI", h, 6, 3, 1)
    struct.pack_into("<QQQ", h, 0x10, BS, outer_size, BS + sb_index * BS)
    struct.pack_into("<Q", h, 0x28, BS)
    h[0x30:0x50] = game_digest
    struct.pack_into("<QQQQ", h, 0x50, inner["meta_base"] // BS, BS + outer_size, BS, SIGNED)
    h[0x70:0x90] = game_digest
    naps_blocks = max(1, -(-len(naps_blob) // BS))
    inner_blocks = sb_index - naps_blocks
    struct.pack_into("<III", h, 0x90, inner_blocks, inner["files"], inner["files"] + inner["empty"])
    struct.pack_into("<QQ", h, 0xA0, inner_blocks * BS, len(naps_blob))
    h[0xB0:0xD0] = sha3(naps_blob)
    h[0xD0:0xF0] = game_digest
    struct.pack_into("<IIII", h, 0xF0, max(1, inner["app_files"]), 0, 2, inner["empty"])
    return h


def si_zip(content_id):
    b = io.BytesIO()
    with zipfile.ZipFile(b, "w", zipfile.ZIP_STORED) as z:
        z.writestr(zipfile.ZipInfo("common/etc/content_id.txt", date_time=(2024, 5, 6, 7, 8, 10)),
                   content_id.encode("ascii"))
    return b.getvalue()


# The CNT entry IDs of the sce_sys files a package keeps there (ProsperoSceSysMedia.cs's name map; the
# ones the tests use).
SCE_IDS = {
    "license.dat": 0x400, "license.info": 0x401, "nptitle.dat": 0x402, "npbind.dat": 0x403, "selfinfo.dat": 0x404,
    "imageinfo.dat": 0x406, "target-deltainfo.dat": 0x407, "origin-deltainfo.dat": 0x408, "psreserved.dat": 0x409,
    "playgo-chunk.dat": 0x1001, "pic1.png": 0x1006, "save_data.png": 0x100D, "icon0.png": 0x1200,
    "pic0.png": 0x1220, "snd0.at9": 0x1240, "changeinfo/changeinfo.xml": 0x1260, "icon0.dds": 0x1280,
    "pic0.dds": 0x12A0, "pic1.dds": 0x12C0, "trophy2/trophy00.ucp": 0x1480, "uds/uds00.ucp": 0x14A0,
    "pic2.png": 0x2040, "pic2.dds": 0x2060, "param.json": 0x2000, "playgo-hash-table.dat": 0x2010,
    "playgo-ficm.dat": 0x2011, "trophy2/npbind.dat": 0x2021,
}
UNNAMED = {0x400, 0x401, 0x402, 0x403, 0x404, 0x406, 0x407, 0x408, 0x409}   # stored without a name


def entry_flags(eid, name):
    """ProsperoCntEntryPolicy.cs: protected entries carry 0x80000000 and their key index."""
    if eid in (0x400, 0x402, 0x403, 0x404, 0x406, 0x407, 0x408, 0x409):
        return 0x80000000, 3 << 12
    if eid == 0x401:
        return 0x80000000, 2 << 12
    if eid == 0x2000:
        return 0, 0
    if name.endswith("npbind.dat"):
        return 0x80000000, 3 << 12
    return 0x08000000, 0


def package(files, content_id, passcode=DEFAULT_PASSCODE, kraken_on=False, t_sec=1714979290, t_nsec=0, mode_of=None,
            seed=b"MaxPackOuterSeed", content_type=0x20, plain=False, hooks=None, out=None, encode=None):
    """A whole package of a game (files: [(path, data)], as a folder has it). The sce_sys files the CNT
    keeps (SCE_IDS) go there, the rest into the inner PFS. (bytes, info). hooks, for damaged packages:
    "meta" (see inner_image), "image" (pfs_image.dat's bytes), "naps" (the map), "entries" (the CNT
    entries) and "names" (the CNT's names table), each taking and giving the thing. out: a path to write
    it to as it is made (for big packages; then bytes is None), with pfs_image.dat in out + ".inner"
    meanwhile. A file's data may be a DiskFile. encode: the payload's Kraken encoder (see inner_image)."""
    hooks = hooks or {}
    inner_files, cnt_files = [], []
    for path, data in files:
        rel = path[8:] if path.startswith("sce_sys/") else None
        if rel is not None and rel in SCE_IDS:
            cnt_files.append((rel, bytes(data[0:len(data)])))
        else:
            inner_files.append((path, data))
    inner_f = open(out + ".inner", "w+b") if out else io.BytesIO()
    inner = inner_image(inner_files, kraken_on, t_sec, t_nsec, mode_of or (lambda i: 0), hooks.get("meta"),
                        Sink(inner_f), encode)
    naps_blob, nfiles = naps(inner, kraken_on)
    inner["files"] = nfiles - 1
    if "naps" in hooks:
        naps_blob = hooks["naps"](naps_blob)
    image = inner["image"]
    if "image" in hooks:
        image = Bytes(hooks["image"](image.read(0, image.size), inner))
    pkg_f = open(out, "w+b") if out else io.BytesIO()
    pkg = Sink(pkg_f)
    pkg.write(bytes(BS))   # the FIH, written once the rest is known
    sb, oinfo = outer_pfs([("pfs_image.dat", image, False, inner["ndblock"] * BS),
                           ("naps_pkg_layout.dat", Bytes(naps_blob), True, len(naps_blob))],
                          content_id, passcode, seed, t_sec, t_nsec, plain=plain, sink=pkg, base=BS)
    inner_f.close()
    if out:
        os.remove(out + ".inner")
    outer_size = oinfo["total"] * BS
    entries = [(0x1, None, b"", 0x40000000, 0), (0x10, None, bytes(0x2A0), 0x60000000, 0),
               (0x20, None, bytes(0x100), 0x60000000, 0), (0x80, None, bytes(0x1E0), 0x60000000, 0),
               (0x100, None, b"", 0x60000000, 0), (0x200, None, b"", 0x40000000, 0)]
    for rel, data in sorted(cnt_files, key=lambda f: SCE_IDS[f[0]]):
        eid = SCE_IDS[rel]
        f1, f2 = entry_flags(eid, rel)
        entries.append((eid, None if eid in UNNAMED else rel, data, f1, f2))
    entries.append((0x40A, None, bytes(32), 0x60000000, 0))
    if "entries" in hooks:
        entries = hooks["entries"](entries)
    body, where = cnt(entries, content_id, passcode, content_type, outer_size, hooks.get("names"))
    fih = fih_header(outer_size, sb, sha3(oinfo["sb_plain"]), naps_blob, inner)
    pkg.size = BS + outer_size
    pkg.write(body)
    pkg.write(si_zip(content_id))
    pkg.f.seek(0)
    pkg.f.write(bytes(fih))
    data = None
    if out:
        pkg.f.close()
    else:
        data = pkg.f.getvalue()
    return data, dict(inner=inner, naps=naps_blob, sb=sb, outer=oinfo, cnt_offset=BS + outer_size, cnt=where,
                      outer_size=outer_size)
