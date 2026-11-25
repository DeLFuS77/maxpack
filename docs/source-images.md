<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# Source images: `.exfat`, `.ffpkg`, `.ffpfs` and `.ffpfsc`

`maxpack pack`, `analyze` (also `--levels`), `info` and `icon` take a game as a dump folder or as an
image in any of the formats [ShadowMountPlus](https://github.com/drakmor/ShadowMountPlus) mounts.
The engine reads them itself, read-only, without mounting anything, without temporary files and
without other tools; a game packed from an image is byte for byte the `.p5z` (or the folder for the
console) of the same game packed from a folder. PKGs are still refused (they are encrypted).

| extension | what it is | made by |
|---|---|---|
| `.exfat` | exFAT image, the game at its root | `mkexfat.sh`, OSFMount, `mkpfs pack exfat` |
| `.ffpkg` | UFS2 image (FreeBSD's file system), the game at its root | `mkufs2.sh` (`newfs -O 2 -b 65536 -f 65536 -m 0 -S 4096`), UFS2Tool |
| `.ffpfs` | PFS image (the PlayStation File System, unsigned), the game at its root; its files may each be PFSC-compressed | `mkpfs pack folder --raw` |
| `.ffpfsc` | a PFS image whose only file is another image (`.exfat`, `.ffpkg` or a PFS image), usually PFSC-compressed | `mkpfs pack file`, `mkpfs pack folder` |

**The extension is only a hint.** What a file is comes from its contents: an exFAT boot sector, a
UFS2 superblock (at 64 KiB, or the other places FreeBSD looks), a PFS header; a PFS image whose
only file is one of those is a container, whatever that file is called. A renamed image opens, and
when a file is none of them the message says what its extension promised (`not a UFS2 image ...`).
Containers can nest (MkPFS's two passes put a PFS image inside), at most three images deep.

What `info --json` says (the fields of before do not change): `kind` is `exfat`, `ufs2`, `pfs` or
`pfsc` (a container) besides `folder` and `p5z`; `inner` is, for `pfsc`, the kind of the image that
holds the game (`exfat`, `ufs2` or `pfs`, the innermost when they nest), `""` otherwise; and
`compressed` is `true` when reading the game decompresses PFSC blocks. `analyze --json` says the same
in `source.kind` and `source.inner` (a folder is still `dir` there), and `--capabilities` has
`"images": true`.

## The formats

**UFS2** (layout from FreeBSD's `sys/ufs/ffs/fs.h`, `sys/ufs/ufs/dinode.h`, `sys/ufs/ufs/dir.h`):
the superblock (`fs_magic` 0x19540119 at byte 1372 of it, `fs_sblockloc` equal to where it was
found) gives the block and fragment sizes, the cylinder groups (`fs_ncg` of `fs_fpg` fragments, each
with `fs_ipg` 256-byte inodes from fragment `fs_iblkno`) and the size. The root is inode 2. An inode
has 12 direct block addresses and a single, a double and a triple indirect one, all 64-bit and in
fragments; a small file's last block may be a few fragments; an address 0 is a hole (zeros).
Folders are 512-byte chunks of entries (inode, record length, type, name). Little-endian only, as
the PS5's; UFS1 is refused.

**PFS** (as MkPFS writes it, which is what ShadowMountPlus reads; checked against MkPFS 1.1.0's own
reader and its `verify`): block 0 is the header (version 1 or 2, magic 20130315, mode, block size of
4 KiB to 1 MiB, inode count, data block count, inode block count, the super-root's inode). The inode
table starts at block 1: 0xA8-byte inodes (mode, flags, size, second size, four times, block count,
12 direct and 5 indirect 32-bit block numbers), as many per block as fit. The super-root folder lists
`flat_path_table` (a hash index, not needed to list the files) and `uroot`, the root of the files.
Folder entries are inode, type (2 file, 3 folder, 4 `.`, 5 `..`), name length and entry size, the
name, padded to 8 bytes. An unsigned file is contiguous from its first block. A file whose inode has
flag 1 is a PFSC stream: its first size is what it takes stored and its second its real length
(MkPFS's convention). Signed or encrypted PFS images (bit 0 or 2 of the mode, as inside a PKG) are
refused.

**PFSC**: a 0x30-byte header (`PFSC`, 0, 6, block size twice, where the block table starts (0x400),
where the data starts (0x10000 or more), the logical length in whole blocks), a table of one 64-bit
offset per block plus one, and the blocks: one exactly a block long (64 KiB) is stored as is, a
shorter one is a zlib stream that must inflate to exactly one block.

The game's files and folders come out sorted by name, with their sizes and dates (UFS2: the inode's
modification time; PFS: the second of its four times; MkPFS writes the time it built the image in
all of them, and 2024-01-01 in its exFAT images, so a `.p5z` made from such an image has those
dates, the only thing in it that can differ from the `.p5z` of the original folder).

## How it reads

`mp_image` (`src/core/mp_image.h`) is what a reader parses: a file, a range of another image, or a
PFSC-compressed file seen decompressed. The exFAT, UFS2 and PFS readers take one, so the image inside
a container is read through the container without being written anywhere. On open, a reader loads
and checks every folder and the block map of every file; reads afterwards go by contiguous runs.

PFSC blocks are decompressed by the threads that read the game: a source that decompresses asks the
read-ahead for 16 threads instead of 4 (`mp_source_readers`, never more than `--threads`), each one
inflating the 64 KiB blocks of the 4 MiB pieces it reads (a few at a time, one read from the image
for all of them). zlib inflates about 510 MB/s per thread on this kind of data, so one thread reads
an `.ffpfsc` at about 510 MB/s and 16 at about 3 GB/s, as fast as the same game as `.exfat` (where
the limit is the SHA-256 of each file). Reads that take part of a block (an exFAT image with clusters
smaller than 64 KiB) go through a cache of the last 64 blocks, so a block is not inflated once per
small file.

## What stops it

Only reading, never writing into an image. A damaged or hostile image stops with a message, never a
crash or a hang: a truncated image; a folder reached twice (a loop) or a file reached twice (a hard
link: a game has none); an inode or block number out of range; blocks used by two files, or more
blocks than the image holds (a pointer that points back at itself); sizes larger than the volume or
that overflow; more than 4,194,304 files and folders or 64 folder levels; PFSC offsets that go
backwards or past the end, a block empty or longer than a block, or one that inflates to the wrong
size; names that are empty, `.` or `..` as a file or folder, longer than 255 bytes, not UTF-8, or that
have `/`, `\` or a NUL byte; two equal names in a folder; symbolic links, devices, pipes and sockets;
unknown entry types; signed or encrypted PFS; images more than three deep.

The tests (`test_source_images`, `test_image_pack`, `test_cli`) use images from `make_ufs2.py` and
`make_pfs.py` (the same game in ten forms and 45 damaged images) and five made by MkPFS 1.1.0
(`tests/data`), and flip bytes in the metadata of each and cut it short at many places: about 9,000
mutations, each ending in a clean error or a read.
