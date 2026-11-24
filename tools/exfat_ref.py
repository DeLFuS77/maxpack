#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Reference exFAT reader, independent of the C code, to compare results.

  exfat_ref.py ls  <image>                      -> "size<TAB>path" per file, sorted by UTF-8 bytes
  exfat_ref.py cat <image> <path> [off] [len]   -> the file's bytes to stdout (zeros beyond VDL)

Read-only. Meant for game images (hundreds of GB): it only loads the FAT and the directories.
"""
import struct
import sys

EOC_MIN = 0xFFFFFFF7


class Image:
    def __init__(self, path):
        self.f = open(path, "rb")
        b = self.f.read(512)
        if len(b) < 512 or b[3:11] != b"EXFAT   " or b[510:512] != b"\x55\xAA":
            raise SystemExit("not an exFAT image: %s" % path)
        (_, _, fat_off, fat_len, heap_off, self.count, root, _, _, _, bps, spc,
         _) = struct.unpack_from("<QQIIIIIIHHBBB", b, 64)
        self.ss = 1 << bps
        self.cs = 1 << (bps + spc)
        self.heap = heap_off * self.ss
        self.f.seek(fat_off * self.ss)
        self.fat = self.f.read(fat_len * self.ss)
        self.files = {}   # path -> (first cluster, nofat, DataLength, ValidDataLength)
        self._dir(self._read(root, False, None), "")

    def _next(self, c):
        return struct.unpack_from("<I", self.fat, c * 4)[0]

    def chain(self, first, nofat, length):
        if length is not None:
            n = (length + self.cs - 1) // self.cs
            if nofat:
                return [first + i for i in range(n)]
        out = [first]
        while length is None or len(out) < n:
            c = self._next(out[-1])
            if c >= EOC_MIN:
                break
            out.append(c)
        return out

    def _read(self, first, nofat, length):
        data = bytearray()
        for c in self.chain(first, nofat, length):
            self.f.seek(self.heap + (c - 2) * self.cs)
            data += self.f.read(self.cs)
        return bytes(data if length is None else data[:length])

    def _dir(self, d, prefix):
        i = 0
        while i + 32 <= len(d):
            t = d[i]
            if t == 0:
                break
            if t == 0x85:
                sec = d[i + 1]
                attr = struct.unpack_from("<H", d, i + 4)[0]
                st = d[i + 32:i + 64]
                nofat = bool(st[1] & 2)
                nlen = st[3]
                vdl = struct.unpack_from("<Q", st, 8)[0]
                first, dl = struct.unpack_from("<IQ", st, 20)
                name = b"".join(d[i + 32 * k + 2:i + 32 * k + 32] for k in range(2, sec + 1))
                # NameLength counts UTF-16 units (an emoji takes two).
                name = name[:2 * nlen].decode("utf-16-le", "replace")
                path = prefix + name
                if attr & 0x10:
                    if dl:
                        self._dir(self._read(first, nofat, dl), path + "/")
                else:
                    self.files[path] = (first, nofat, dl, vdl)
                i += 32 * (sec + 1)
                continue
            i += 32

    def cat(self, path, off, length, out):
        first, nofat, dl, vdl = self.files[path]
        end = dl if length is None else min(dl, off + length)
        cl = None if nofat else self.chain(first, False, dl)
        pos = off
        while pos < end:
            if pos >= vdl:
                n = min(end - pos, 1 << 20)
                out.write(bytes(n))
                pos += n
                continue
            k = pos // self.cs
            c = first + k if nofat else cl[k]
            n = min(end, vdl, (k + 1) * self.cs) - pos
            if nofat:  # contiguous: reads in one go
                n = min(end, vdl) - pos
                n = min(n, 1 << 24)
            self.f.seek(self.heap + (c - 2) * self.cs + pos % self.cs)
            out.write(self.f.read(n))
            pos += n


def main(argv):
    if len(argv) >= 3 and argv[1] == "ls":
        img = Image(argv[2])
        out = sys.stdout.buffer
        for p in sorted(img.files, key=lambda x: x.encode("utf-8")):
            out.write(b"%d\t%s\n" % (img.files[p][2], p.encode("utf-8")))
        return 0
    if len(argv) >= 4 and argv[1] == "cat":
        img = Image(argv[2])
        off = int(argv[4]) if len(argv) > 4 else 0
        length = int(argv[5]) if len(argv) > 5 else None
        img.cat(argv[3], off, length, sys.stdout.buffer)
        return 0
    sys.stderr.write(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
