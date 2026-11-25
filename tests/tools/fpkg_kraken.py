#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""The Kraken ("newLZ") encoder of the PS5 package writers, for make_fpkg.py's test packages.

A port of pearlxcore/ProsperoPkgTool's Compression/KrakenLzEncoder.cs and
Compression/KrakenHuffmanArrayEncoder.cs (MIT; see THIRD_PARTY_NOTICES), as fthdgn-gh/Alembic's
src/core/kraken_encode.cpp has it (GPL-3.0-or-later): an LZ77 parse with a hash-chain match finder
and lazy matching, arrays raw or Huffman-coded (three streams), offsets with scaling 1, and the
"excess" framing for long lengths. It makes what those tools write into a package, so MaxPack's
decoder is tested on it. encode_block(data, extras=True) also uses, where they fit, the parts of the
format those encoders leave out but a package from another writer may have: repeated offsets
(the decoder's recent-offset slots), offsets without scaling (the 0xF0 distance codes) and the
two-split Huffman array (type 4). Slow (pure Python): for test data of a few MB.
"""

ALPHABET = 256
MAX_BITS = 11
SEED = 8
MIN_MATCH = 4
NO_MATCH_ZONE = 16
LITERAL_TAIL = 8
MAX_ESCAPES = 512
HASH_BITS = 18
MAX_CHAIN = 128
CHUNK = 0x20000
MAX_BLOCK = 0x40000

CHUNK0_SUB = 0x01
CHUNK0_NEWLZ = 0x02
CHUNK1_SUB = 0x10
CHUNK1_NEWLZ = 0x20


class MsbBits:
    def __init__(self):
        self.out = bytearray()
        self.cur = 0
        self.n = 0

    def bit(self, b):
        self.cur = (self.cur << 1) | (b & 1)
        self.n += 1
        if self.n == 8:
            self.out.append(self.cur)
            self.cur = 0
            self.n = 0

    def bits(self, value, count):
        for i in range(count - 1, -1, -1):
            self.bit((value >> i) & 1)

    def finish(self):
        if self.n:
            self.out.append((self.cur << (8 - self.n)) & 0xFF)
        self.cur = 0
        self.n = 0
        return bytes(self.out)


class LsbBits:
    def __init__(self):
        self.out = bytearray()
        self.acc = 0
        self.n = 0

    def bits(self, value, count):
        if count == 0:
            return
        self.acc |= (value & ((1 << count) - 1)) << self.n
        self.n += count
        while self.n >= 8:
            self.out.append(self.acc & 0xFF)
            self.acc >>= 8
            self.n -= 8

    def finish(self):
        if self.n:
            self.out.append(self.acc & 0xFF)
        self.acc = 0
        self.n = 0
        return bytes(self.out)


def limited_lengths(freq):
    """Package-merge code lengths of at most MAX_BITS; None if the code would not be complete."""
    present = [s for s in range(ALPHABET) if freq[s] > 0]
    n = len(present)
    if n == 0:
        return None
    lengths = [0] * ALPHABET
    if n == 1:
        lengths[present[0]] = 1
        return lengths
    present.sort(key=lambda s: (freq[s], s))
    weight, left, right, leaf_of, leaves = [], [], [], [], []
    for s in present:
        weight.append(freq[s])
        left.append(-1)
        right.append(-1)
        leaf_of.append(s)
        leaves.append(len(weight) - 1)
    current = list(leaves)
    for _ in range(1, MAX_BITS):
        packages = []
        for i in range(0, len(current) - 1, 2):
            x, y = current[i], current[i + 1]
            weight.append(weight[x] + weight[y])
            left.append(x)
            right.append(y)
            leaf_of.append(-1)
            packages.append(len(weight) - 1)
        merged, i, j = [], 0, 0
        while i < len(leaves) and j < len(packages):
            if weight[leaves[i]] <= weight[packages[j]]:
                merged.append(leaves[i])
                i += 1
            else:
                merged.append(packages[j])
                j += 1
        merged += leaves[i:]
        merged += packages[j:]
        current = merged
    counts = [0] * ALPHABET
    for c0 in current[:2 * n - 2]:
        stack = [c0]
        while stack:
            c = stack.pop()
            if leaf_of[c] >= 0:
                counts[leaf_of[c]] += 1
            else:
                stack.append(left[c])
                stack.append(right[c])
    kraft = 0
    for s in range(ALPHABET):
        if freq[s] > 0 and not 1 <= counts[s] <= MAX_BITS:
            return None
        lengths[s] = counts[s]
        if freq[s] > 0:
            kraft += 1 << (MAX_BITS - counts[s])
    return lengths if kraft == 1 << MAX_BITS else None


def huffman_table(data):
    """(lengths, reversed codes, the code-length list) for data, or None."""
    freq = [0] * ALPHABET
    for c in data:
        freq[c] += 1
    distinct = sum(1 for f in freq if f)
    if distinct == 1:
        # the decoder takes no one-symbol table: a phantom second symbol
        real = next(s for s in range(ALPHABET) if freq[s])
        freq[1 if real == 0 else 0] = 1
        distinct = 2
    lengths = limited_lengths(freq)
    if lengths is None or not 2 <= distinct < ALPHABET:
        return None
    rev = [0] * ALPHABET
    slot = 0
    for length in range(1, MAX_BITS + 1):
        for s in range(ALPHABET):
            if lengths[s] != length:
                continue
            code = slot >> (MAX_BITS - length)
            r = 0
            for _ in range(length):
                r = (r << 1) | (code & 1)
                code >>= 1
            rev[s] = r
            slot += 1 << (MAX_BITS - length)
    width = (max(lengths) - 1).bit_length()
    if width > 4:
        return None
    w = MsbBits()
    w.bits(0, 1)
    w.bits(0, 1)
    w.bits(distinct, 8)
    w.bits(width, 3)
    for s in range(ALPHABET):
        if lengths[s]:
            w.bits(s, 8)
            if width:
                w.bits(lengths[s] - 1, width)
    return lengths, rev, w.finish()


def three_streams(data, lengths, rev):
    """The decoder's three-stream layout: the first stream's size (2 bytes), it, the third, the
    second backwards."""
    st = [LsbBits(), LsbBits(), LsbBits()]
    for i, c in enumerate(data):
        st[i % 3].bits(rev[c], lengths[c])
    a, b, c = (s.finish() for s in st)
    if len(a) > 0xFFFF:
        return None
    return bytes([len(a) & 0xFF, len(a) >> 8]) + a + c + b[::-1]


def huffman_array(data, split=False):
    """A DecodeBytes record of type 2 (or 4, two halves of three streams each, with split), or None
    if it cannot be made or is not smaller."""
    d = len(data)
    if d < 2 or d > 0x3FFFF:
        return None
    t = huffman_table(data)
    if t is None:
        return None
    lengths, rev, table = t
    if not split:
        body = three_streams(data, lengths, rev)
        if body is None:
            return None
        body = table + body
        kind = 2
    else:
        half = (d + 1) >> 1
        left = three_streams(data[:half], lengths, rev)
        right = three_streams(data[half:], lengths, rev)
        if left is None or right is None or d < 4:
            return None
        mid = len(left)
        body = table + bytes([mid & 0xFF, (mid >> 8) & 0xFF, mid >> 16]) + left + right
        kind = 4
    if len(body) >= d:
        return None
    dm1 = d - 1
    sizes = len(body) | ((dm1 & 0x3FFF) << 18)
    return bytes([(kind << 4) | ((dm1 >> 14) & 0xF)]) + sizes.to_bytes(4, "big") + body


def append_array(out, data, split=False):
    raw = 3 + len(data)
    h = huffman_array(data, split) if len(data) >= 2 else None
    if h is not None and len(h) < raw:
        out += h
        return
    out += len(data).to_bytes(3, "big")
    out += data


class Finder:
    def __init__(self, data, low=0):
        self.data = data
        self.n = len(data)
        self.low = low   # no match from before this position
        self.head = {}
        self.prev = [-1] * self.n
        self.next_insert = 0

    def hash(self, p):
        d = self.data
        v = d[p] | d[p + 1] << 8 | d[p + 2] << 16 | d[p + 3] << 24
        return ((v * 2654435761) & 0xFFFFFFFF) >> (32 - HASH_BITS)

    def insert_upto(self, upto):
        while self.next_insert < upto and self.next_insert + 3 < self.n:
            h = self.hash(self.next_insert)
            self.prev[self.next_insert] = self.head.get(h, -1)
            self.head[h] = self.next_insert
            self.next_insert += 1
        if self.next_insert < upto:
            self.next_insert = upto

    def length_at(self, cand, pos, limit):
        d = self.data
        n = 0
        while n < limit and d[cand + n] == d[pos + n]:
            n += 1
        return n

    def find(self, pos, end_limit):
        d = self.data
        max_len = end_limit - pos
        best, dist = 0, 0
        if max_len < MIN_MATCH:
            return 0, 0
        cand = self.head.get(self.hash(pos), -1)
        key = d[pos:pos + 4]
        chain = 0
        while cand >= 0 and chain < MAX_CHAIN:
            distance = pos - cand
            if distance >= SEED and cand >= self.low and d[cand:cand + 4] == key:
                n = MIN_MATCH
                # compare in slices first (fast), then byte by byte
                while n + 64 <= max_len and d[cand + n:cand + n + 64] == d[pos + n:pos + n + 64]:
                    n += 64
                while n < max_len and d[cand + n] == d[pos + n]:
                    n += 1
                if n > best:
                    best, dist = n, distance
                    if n >= max_len:
                        break
            cand = self.prev[cand]
            chain += 1
        return best, dist


def write_length(w, value):
    code = value + 64
    zeros = code.bit_length() - 1 - 6
    if not 0 <= zeros <= 12:
        return False
    for _ in range(zeros):
        w.bit(0)
    w.bit(1)
    width = zeros + 6
    w.bits(code - (1 << width), width)
    return True


def region(f, b):
    return f + b[::-1]


def distance_code(w, d):
    """An offset with no scaling (the decoder's read_distance): the code byte, writing its bits."""
    # v < 0xF0: n = (v >> 4) + 4 bits w; offset = ((w | 1 << n) << 4) + (v & 0xF) - 248 ... the
    # decoder computes rv = ((w & m) << 4) + (v & 0xF) - 248 with w's top bit forced to 1.
    x = d + 248
    for n in range(4, 19):
        hi = x >> 4
        if (1 << n) <= hi < (2 << n):
            w.bits(hi - (1 << n), n)
            return ((n - 4) << 4) | (x & 0xF)
    return None


def encode_chunk(finder, chunk_start, chunk_end, with_seed, out, mode):
    """One sub-chunk into out; False if it cannot be written (too many escapes)."""
    data = finder.data
    rep_on, nonscaled, sub = mode & REPEATS, mode & NO_SCALING, mode & SUB_LITERALS
    parse_start = SEED if with_seed else chunk_start
    start_limit = chunk_end - NO_MATCH_ZONE
    end_limit = chunk_end - LITERAL_TAIL
    literal = bytearray()
    command = bytearray()
    lengths = bytearray()
    escapes = []
    pending = []   # the new offsets' distances, in order
    recent = [-8, -8, -8]   # the decoder's recent offsets (negative), newest first
    last = -8   # the offset the next literals are deltas against (sub literals)
    lit_start = parse_start
    pos = parse_start

    def literals(a, b):
        if sub:
            literal.extend((data[i] - data[i + last]) & 0xFF for i in range(a, b))
        else:
            literal.extend(data[a:b])

    while pos < start_limit:
        finder.insert_upto(pos)
        mlen, mdist = finder.find(pos, end_limit)
        rep = -1
        if rep_on:
            # a repeated offset that matches at least as long wins (no offset to write)
            for k in range(3):
                d = -recent[k]
                if pos - d >= finder.low:
                    n = finder.length_at(pos - d, pos, end_limit - pos)
                    if n >= MIN_MATCH and n >= mlen:
                        mlen, mdist, rep = n, d, k
                        break
        if mlen < MIN_MATCH:
            pos += 1
            continue
        if rep < 0 and pos + 1 < start_limit:
            finder.insert_upto(pos + 1)
            lazy, _ = finder.find(pos + 1, end_limit)
            if lazy > mlen + 1:
                pos += 1
                continue
        lit_len = pos - lit_start
        literals(lit_start, pos)
        lit_field = lit_len if lit_len <= 2 else 3
        match_field = mlen - 2 if mlen <= 16 else 15
        command.append(((rep if rep >= 0 else 3) << 6) | (match_field << 2) | lit_field)
        if rep >= 0:
            recent.insert(0, recent.pop(rep))
        else:
            recent.insert(0, -mdist)
            recent.pop()
            pending.append(mdist)
        last = recent[0]
        if lit_len >= 3:
            if lit_len - 3 < 255:
                lengths.append(lit_len - 3)
            else:
                lengths.append(255)
                escapes.append(lit_len - 258)
        if mlen > 16:
            if mlen - 17 < 255:
                lengths.append(mlen - 17)
            else:
                lengths.append(255)
                escapes.append(mlen - 272)
        if len(escapes) > MAX_ESCAPES:
            return False
        pos += mlen
        lit_start = pos
    finder.insert_upto(chunk_end)
    literals(lit_start, chunk_end)

    forward, backward = MsbBits(), MsbBits()
    offs = bytearray()
    for i, d in enumerate(pending):
        w = forward if i % 2 == 0 else backward
        if nonscaled:
            code = distance_code(w, d)
            if code is None:
                return False
            offs.append(code)
        else:
            off = d + SEED
            e = 0
            while (off >> e) >= 16:
                e += 1
            base = (off >> e) - 8
            offs.append((e << 3) | base)
            if e:
                w.bits(off - ((8 + base) << e), e)

    ef, eb = MsbBits(), MsbBits()
    for i, v in enumerate(escapes):
        if not write_length(ef if i % 2 == 0 else eb, v):
            return False
    excess = region(ef.finish(), eb.finish())
    bits = region(forward.finish(), backward.finish())
    if with_seed:
        out += data[:SEED]
    count = len(excess)
    if count <= 31:
        out.append(0x80 | count)
    else:
        high = (count - 32) // 32
        out.append(0x80 | (count - high * 32))
        out.append(high)
    append_array(out, literal, split=bool(rep_on))
    append_array(out, command)
    if not nonscaled:
        out.append(0x80)   # offsets with scaling 1
    append_array(out, offs)
    append_array(out, lengths)
    out += bits
    out += excess
    return True


# encode_block's modes (0: exactly the reference encoders)
REPEATS = 1        # repeated offsets, and literals as a two-split Huffman array (type 4)
NO_SCALING = 2     # offsets without scaling (the decoder's distance codes)
SUB_LITERALS = 4   # literals as deltas against the last offset (flag CHUNK0_SUB / CHUNK1_SUB)


def encode_block(data, mode=0, seed_free=False):
    """(payload, flags, first_chunk_size) for 8 bytes to 256 KiB, or None if it is not smaller.
    seed_free: no match takes the first 8 bytes (the raw seed the payload starts with), so they can
    be changed in the data and the payload alike (the benchmark's many-but-cheap blocks)."""
    data = bytes(data)
    n = len(data)
    if n < SEED or n > MAX_BLOCK:
        return None
    finder = Finder(data, SEED if seed_free else 0)
    out = bytearray()
    first_end = min(n, CHUNK)
    if not encode_chunk(finder, 0, first_end, True, out, mode):
        return None
    sub = mode & SUB_LITERALS
    if n <= CHUNK:
        if len(out) >= n:
            return None
        return bytes(out), CHUNK0_NEWLZ | (CHUNK0_SUB if sub else 0), 0
    first = len(out)
    if not encode_chunk(finder, CHUNK, n, False, out, mode):
        return None
    if len(out) >= n:
        return None
    flags = CHUNK0_NEWLZ | CHUNK1_NEWLZ | ((CHUNK0_SUB | CHUNK1_SUB) if sub else 0)
    return bytes(out), flags, first


def bare_array(data):
    """A block with no LZ (a "bare entropy array" sub-chunk: the flags without NEWLZ), or None."""
    return huffman_array(bytes(data))
