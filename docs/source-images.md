<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# Source images and packages: `.exfat`, `.ffpkg`, `.ffpfs`, `.ffpfsc` and `.pkg`

`maxpack pack`, `analyze` (also `--levels`), `info` and `icon` take a game as a dump folder, as an
image in any of the formats [ShadowMountPlus](https://github.com/drakmor/ShadowMountPlus) mounts, or
as a PS5 debug package (FPKG, `.pkg`). The engine reads them itself, read-only, without mounting or
installing anything, without temporary files and without other tools; a game packed from an image is
byte for byte the `.p5z` (or the folder for the console) of the same game packed from a folder, and
one packed from a package has the files its builder put in it
([PS5 packages](#ps5-packages-fpkg-pkg)). The PC window takes the four images and the packages
wherever it takes a dump folder ([pc-window.md](pc-window.md#images)).

| extension | what it is | made by |
|---|---|---|
| `.exfat` | exFAT image, the game at its root | `mkexfat.sh`, OSFMount, `mkpfs pack exfat` |
| `.ffpkg` | UFS2 image (FreeBSD's file system), the game at its root | `mkufs2.sh` (`newfs -O 2 -b 65536 -f 65536 -m 0 -S 4096`), UFS2Tool |
| `.ffpfs` | PFS image (the PlayStation File System, unsigned), the game at its root; its files may each be PFSC-compressed | `mkpfs pack folder --raw` |
| `.ffpfsc` | a PFS image whose only file is another image (`.exfat`, `.ffpkg` or a PFS image), usually PFSC-compressed | `mkpfs pack file`, `mkpfs pack folder` |
| `.pkg` | PS5 debug package (FPKG): the game in an encrypted PFS image, its files stored or Kraken-compressed | [ProsperoPkgTool](https://github.com/pearlxcore/ProsperoPkgTool) and the tools that port its builder |

**The extension is only a hint.** What a file is comes from its contents: an exFAT boot sector, a
UFS2 superblock (at 64 KiB, or the other places FreeBSD looks), a PFS header, a package's FIH
header (`\x7FFIH`); a PFS image whose
only file is one of those is a container, whatever that file is called. A renamed image opens, and
when a file is none of them the message says what its extension promised (`not a UFS2 image ...`).
Containers can nest (MkPFS's two passes put a PFS image inside), at most three images deep. A
package is read only as a file of its own, never from inside a container.

What `info --json` says (the fields of before do not change): `kind` is `exfat`, `ufs2`, `pfs`,
`pfsc` (a container) or `fpkg` (a package) besides `folder` and `p5z`; `inner` is, for `pfsc`, the kind of the image that
holds the game (`exfat`, `ufs2` or `pfs`, the innermost when they nest), `""` otherwise; and
`compressed` is `true` when reading the game decompresses PFSC blocks (or, in a package, Kraken
blocks). `analyze --json` says the same in `source.kind` and `source.inner` (a folder is still `dir`
there), and `--capabilities` has `"images": true`, and `"fpkg": true` and `"passcode": true` for
packages.

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

## PS5 packages (FPKG, `.pkg`)

A debug package (FPKG, a "fake PKG") is a dump made into something the console installs: the game's
files in a PFS image, stored or Kraken-compressed, encrypted with keys that come from the package's
content ID and a 32-character passcode chosen by whoever built it (32 zeros unless they chose
another). MaxPack reads one as it reads an image, read-only: it decrypts and decompresses each block
as it reads it, in memory, with nothing installed, mounted or written. It carries no keys and fetches
none: the keys come from the content ID and the passcode, the way the package's builder makes them.

**The passcode.** `pack`, `analyze`, `info`, `icon` and `verify --against` take `--passcode <32
characters>` (letters, digits or signs, no spaces); without it, the default. The PC window gives the
one of Settings › **FPKG passcode** ([pc-window.md](pc-window.md#4-settings)). The passcode decrypts
the outer PFS's inode table, and the root folder it gives has to be one. When it is not, and the
superblock's own checksum (a SHA3-256 that does not depend on the passcode) is right, the package was
made with another passcode: `this package was made with another passcode; give it with --passcode`
(with `--json`, the error code is `passcode`; with a wrong one given, `... than the one given`). When
the checksum is wrong too, the package is damaged.

**Only a full PS5 game.** The rest stops with the reason:

| the package | why it is refused |
|---|---|
| retail (byte 5 of the FIH header is 0x80) | `retail packages need keys this tool does not have` |
| a PS4 package (`\x7FCNT`) | MaxPack is for PS5 games |
| a patch (`\x7FLIH`) | it only has what the update changes: install it on the console, or pack the game with it applied |
| additional content (CNT content type 0x21) | it is not a game: install it on the console |
| another content type | not a game's package |
| one whose PFS does not decrypt to a valid root folder | another passcode, or damaged (above) |

**The format** (as [ProsperoPkgTool](https://github.com/pearlxcore/ProsperoPkgTool) writes and reads
it; [Alembic](https://github.com/fthdgn-gh/Alembic), which ports it, was the second reading). A 64 KiB
FIH header (`\x7FFIH`, little-endian: byte 5 0x00 for a debug package, where the outer PFS and the
CNT are, and where the inner PFS's superblock is), the outer PFS, the CNT (big-endian: the content ID,
the content type and a table of entries, some AES-128-CBC-encrypted) and a ZIP. The outer PFS has
64 KiB blocks, data first; its superblock is plain, and every other block is one AES-128-XTS unit (the
whole 64 KiB block, its index as the sector; `0x800000000000 | index` for the metadata), keyed by
HMAC-SHA256 from the superblock's seed and a key that is the SHA3-256 of the content ID and the
passcode. It holds two files: `pfs_image.dat`, the inner image, and `naps_pkg_layout.dat`, its map.
The inner image is the game's PFS (version 2, 64 KiB blocks, 0xA8-byte inodes with 64-bit data
offsets) cut into 128 KiB pieces that are stored, Kraken-compressed (Oodle's "newLZ", with
Huffman-coded or raw arrays, as the package builders write it) or zero, and the map says where each
piece of the mount (the PFS as it is when installed) is. The builders lay the map out in a few ways;
MaxPack reads it each way and takes the one whose mount holds a readable PFS.

**What comes out.** The game is the inner PFS's `uroot` folder: its files and folders with their sizes
and dates (the modification time of each inode). The package also keeps part of `sce_sys/` outside
that PFS, as entries of its CNT (`param.json`, `icon0.png`, `pic0.png`, `nptitle.dat`, `changeinfo/`,
the trophies and others); MaxPack puts each one back at `sce_sys/<its name>`, where a dump has it,
decrypted, with the date of `uroot`. When a file is in both, the one in the PFS wins; the CNT's own
entries (the digests, the signatures and the PlayGo tables of the package itself) are not the game's
and are left out. `content_id` is that of `param.json`, or the package's own when it gives none.

So a `.p5z` made from a package is the one of the folder its builder packed, less what the builder
changed, and a builder does not keep a dump as it is. ProsperoPkgTool fake-signs the executables that
are raw ELF (`eboot.bin`, `.elf`, `.prx`, `.sprx`), adds `sce_sys/pfs-version.dat`,
`sce_sys/keystone` and `sce_sys/about/right.sprx` when the dump has none, makes its own
`playgo-chunk.dat` with the `playgo-hash-table.dat` and `playgo-ficm.dat` that go with it, and puts a
`.dds` next to each `.png` icon and picture; and every date is the time it built the package. Those
files come out as the package has them; the rest is byte for byte the dump's. `test_source_fpkg`
checks it with `make_fpkg.py`'s game folder and the package a builder of that kind makes of it
(`game.differ.tsv`): packing each gives the same files but these, and the package's dates.

| | files |
|---|---|
| changed | `eboot.bin` (fake-signed), `sce_sys/playgo-chunk.dat` |
| added | `sce_sys/pfs-version.dat`, `sce_sys/about/right.sprx`, `sce_sys/playgo-hash-table.dat`, `sce_sys/playgo-ficm.dat`, `sce_sys/icon0.dds`, `sce_sys/pic0.dds` |

A package that holds the same files and dates as a folder (MaxPack's benchmark makes them) packs to
exactly the `.p5z` of that folder.

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

A package is read the same way, by 16 threads, each decrypting (AES-128-XTS, with the CPU's AES
instructions on a 64-bit x86 PC that has them, checked as it runs; a portable AES elsewhere) and
Kraken-decoding the 128 KiB pieces of the 4 MiB it reads, with a decoder of its own; a piece that two
reads share goes through a cache of the last 64 decoded 256 KiB pages (16 MiB), so a small file in
the middle of one is not decoded once per file. On a 32-thread PC, with a generated 3.00 GB game
(6,584 files) read from the cache with every file's SHA-256: 5.4 GB/s from its Kraken `.pkg`
(2.25 GB; about 600 MB/s for each thread), 7.3 GB/s from a stored one and 4.8 GB/s from its `.ffpkg`.

## What stops it

Only reading, never writing into an image. A damaged or hostile image stops with a message, never a
crash or a hang: a truncated image; a folder reached twice (a loop) or a file reached twice (a hard
link: a game has none); an inode or block number out of range; blocks used by two files, or more
blocks than the image holds (a pointer that points back at itself); sizes larger than the volume or
that overflow; more than 4,194,304 files and folders or 64 folder levels; PFSC offsets that go
backwards or past the end, a block empty or longer than a block, or one that inflates to the wrong
size; names that are empty, `.` or `..` as a file or folder, longer than 255 bytes, not UTF-8, or that
have `/`, `\` or a NUL byte; two equal names in a folder; symbolic links, devices, pipes and sockets;
unknown entry types; signed or encrypted PFS; images more than three deep. In a package, also: a
FIH or CNT header out of place, entries outside the package or with names that are not a file's, a
content ID that is not one, an outer PFS without its superblock or with its inode table, a file's
blocks or its indirect blocks out of range, a block map whose records point outside
`pfs_image.dat`, overlap, leave part of the mount unmapped or can be read two ways that both hold a
PFS, a mount over 2 TiB, a Kraken piece that does not decode to its size (or uses a coding MaxPack
does not read: tANS, RLE and the recursive arrays, which no package builder is known to write), and
a file of the CNT that is a folder of the PFS.

The tests (`test_source_images`, `test_image_pack`, `test_cli`) use images from `make_ufs2.py` and
`make_pfs.py` (the same game in ten forms and 45 damaged images) and five made by MkPFS 1.1.0
(`tests/data`), and flip bytes in the metadata of each and cut it short at many places: about 9,000
mutations, each ending in a clean error or a read. `test_source_fpkg`, `test_kraken` and
`test_crypto` use the packages of `tests/tools/make_fpkg.py`, a builder that follows ProsperoPkgTool's
(the basic game stored, Kraken-compressed and with the parts of Kraken the reference encoder leaves
out; one made with another passcode; the game folder and its package above; the six kinds refused; 21
damaged ones, each with the words of its message) and the test vectors of FIPS-197, SP 800-38A,
IEEE 1619, FIPS-202 and RFC 4231, and flip bytes in 2,500 places of two packages, each ending in a
clean error or a read.
