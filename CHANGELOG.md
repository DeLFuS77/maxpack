# Changelog

All notable changes to MaxPack are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/).

## [1.0.4] - 2026-11-25

### Added

- **Repair data for a `.p5z`** (PC window): a standard PAR2 set next to the `.p5z`
  (`<name>.p5z.par2`, a small index, and one recovery file `<name>.p5z.volA+B.par2`), and MaxPack
  repairs a damaged `.p5z` with it back to the exact bytes it was made with. Any PAR2 tool can read
  and use the set too.
  - **Compress** has **Add repair data** with **1, 5 or 10 %**, off until you tick it (5 % the first
    time) and remembered in Settings, which has it too. Only for a `.p5z`: it is disabled, with the
    reason, for a folder to play from the USB and in Decompress. Once the `.p5z` is made, verified by
    the engine and in its place, the game gets a step of its own that makes the set, with its
    progress and Cancel. If making it fails or is cancelled the game is still done (its `.p5z` is
    good), its row says why, and nothing of the set is left: par2 works in
    `<name>.p5z.maxpack-par2` and its files take their names next to the `.p5z` only once all are
    made. The space of the queue counts the set, the card at the end counts the repair data made, and
    **Copy report** says the setting.
  - **Verify** says under each `.p5z` what its repair data can repair ("Repair data: 5 % — can repair
    up to 200 blocks of 1.3 MB, 268.8 MB in all"). A set is the `.p5z`'s own only if it protects one
    file of its size and of the MD5 of its first 16 KiB (or, when those 16 KiB are damaged, of the MD5
    of some of its other blocks); any other is "repair data of another file" and is never used, nor
    replaced.
  - **Repair** (on a `.p5z` the engine found damaged, or **Repair the damaged ones** for the list)
    checks first that its drive has room for a whole copy, and says it with the numbers. The `.p5z` is
    moved, on its own drive, into `<name>.p5z.maxpack-repair`, where par2 repairs it (the damaged one
    is kept there as `<name>.1`, never purged); the engine verifies the repaired one before it takes
    the `.p5z`'s place, and only then does the damaged one go. If par2 cannot repair it (too much
    damage: it says how many more blocks were needed), what it repaired does not verify, or it is
    cancelled, the `.p5z` goes back exactly as it was. A crash or a kill in the middle leaves the
    folder with a file that says how far the repair went: the leftovers offered at the next start put
    back the one that verified, else the damaged one as it was, and keep a folder that may hold a copy
    of a `.p5z` that is in its place; nothing is deleted unasked.
  - **Add repair data** (Verify) gives a set to `.p5z` files that are there, several at once: each is
    verified by the engine first, and only a good one gets a set; one that has its own of the same
    share keeps it, one of another share is replaced.
  - **Details** (and the card of a double click on a `.p5z`) says whether it has repair data, and how
    much.
  - **Rename** renames the set with its `.p5z` (index and recovery file), all or nothing. The set keeps
    the name the `.p5z` had inside; MaxPack gives par2 the `.p5z` itself, so a renamed pair still
    verifies and repairs.
  - A new `.p5z` written over an old one of the same name takes the old one's set away with it.
  - **maxpack-par2** is par2cmdline-turbo 1.5.0, built from its source (the submodule
    `third_party/par2cmdline-turbo`, GPL-2.0-or-later) in the CI of every system and shipped next to
    `maxpack-core` in the installer, the portable zip, `MaxPack.app` and the Linux package. A build of
    the window without it offers no repair data and says why. The `maxpack` command line does not
    change: the guide gives the `par2` commands. MaxPack for the console ignores the `.par2` files.
- How a set is made, measured on a Ryzen 9 7950X3D (32 threads) with a Samsung 990 PRO and a
  generated `.p5z` of 5.63 GB that the cache did not hold: about 4000 blocks, each a whole number of
  4 KiB (a bad sector is in one block only) and 32 MiB at most (a bad byte of a 100 GB game costs one
  block of about 25 MB; past 128 GB the blocks stay at 32 MiB and there are more of them), and par2
  limited to 960 MiB of memory.

  | Share | Set | Time | Speed | CPU (of 32 threads) | Peak memory |
  |---|---|---|---|---|---|
  | 1 % | 54.3 MiB (40 blocks of 1.34 MiB) | 7.6 s | 738 MB/s | 8 % | 94 MiB |
  | 5 % | 269.5 MiB (200 blocks) | 9.1 s | 618 MB/s | 22 % | 309 MiB |
  | 10 % | 538.3 MiB (400 blocks) | 11.9 s | 473 MB/s | 36 % | 578 MiB |

  With 8 threads the times are the same (7.4, 9.2 and 11.2 s). Making the set does not get down to
  "one more read" of the `.p5z` on this NVMe: the drive reads it in 0.9 s (6.2 GB/s) and the engine
  verifies it in 3.3 s, but par2 must hash the whole file with MD5, one stream, and goes at that
  speed whatever its blocks (2000, 4000 or 8000 blocks took 9.4, 10.3 and 11.9 s at 5 % before the
  blocks were aligned to 4 KiB). The set of a 100 GB game at 5 % needs more than 960 MiB: par2 then
  hashes in a pass of its own and computes in several, which on this file, given as little memory for
  its size, took 10.4 s instead of 9.3 and 60 MiB. Repairing 3 damaged spots with the 5 % set took
  22.8 s (par2 reads the file, writes it again and reads what it wrote), 45 MiB, and the engine's
  verification of the result 3.0 s. These numbers come from an MSVC build of par2cmdline-turbo; the
  Windows packages are built with clang-cl, as par2cmdline-turbo's own releases, which also has its
  assembly MD5 and should be as fast or faster.

## [1.0.3] - 2026-11-25

### Added

- **MaxPack in the Windows Explorer** (PC window, Settings › Windows Explorer, and the installer; each
  off until you turn it on, for this user only, no administrator): a right-click menu with **Compress
  with MaxPack** on folders and `.exfat` images and **Verify with MaxPack** and **Details** on `.p5z`
  files (on Windows 11, under Show more options); **Send to › MaxPack**; and **Open .p5z files with
  MaxPack**, a double click that shows the game's card in Details, with MaxPack's icon, in Open with and
  in Default apps, never taking `.p5z` from a program the user chose. The menu speaks the language of
  the window. The uninstaller takes all of it away; the portable version writes nothing in Windows and
  says so in Settings.
- **Files from the Explorer go to their place** (PC window): a second MaxPack started from the menu,
  Send to or a double click hands its files to the window open, which comes to the front; folders and
  images go to Compress, a `.p5z` to Details, or to the section the menu asked for. With no window open,
  it opens with them. The command line works as before.
- **The card of a `.p5z` in Details** (PC window): what it saved against the original, its volumes, and
  **Verify** and **Decompress**, which take it to those sections.

### Changed

- **Reading the game is about four times faster**, above all from an `.exfat` image or from a disk
  whose data is not already in the system's cache. 1.0.2 read every 256 KiB block with a request of
  its own on one thread, and computed its SHA-256 before asking for the next one (for a folder it
  also opened and closed the file for every block), so the disk waited for the hash and the hash for
  the disk, and the disk only ever had one small request to work on. Now a read-ahead of up to 4
  threads reads 4 MiB at a time (a whole run of an image's clusters in one request), keeps up to
  256 MiB read ahead of the compression, and computes each file's SHA-256 on the way, several files
  at once. On a 32-thread PC with an NVMe, reading a generated 8 GB `.exfat` image (contiguous and
  fragmented files, thousands of small ones) that the cache did not hold went from 768 to 2820 MB/s,
  and a 3 GB folder from 737 to 3526 MB/s; from the cache, from 1784 to 3237 and from 1520 to
  4338 MB/s (SHA-256s included; median of three runs).
- What that gives when packing, on the same PC with all 32 threads: at level 3 the 8 GB image packs
  to a `.p5z` in 9.2 s instead of 15.4 (the packs alone at 1766 MB/s instead of 735) and the 3 GB
  folder in 5.8 s instead of 8.2 (1751 MB/s instead of 710), both read from the disk; to a folder,
  with the game in the cache, in 3.4 s instead of 5.2 and 1.2 s instead of 2.4. At level 19 an NVMe
  was already fast enough: the compression is what holds it back, and the times stay the same (34
  s, 15 s) with the CPU 96-97 % busy. The gain at level 19 is on slower disks, where 1.0.2's
  compression threads waited for one 256 KiB request at a time: a hard disk now gets several requests
  of 4 MiB at once.
- `analyze --levels` reads its sample through the same read-ahead, so its times follow the new
  reading.
- **The output is the same, byte for byte.** Same chunks in the same order, volumes, index and
  SHA-256s as 1.0.2: the identity tests (hashes recorded with 1.0.1) still pass at 1, 2 and all the
  CPU's threads, and both engines wrote identical bytes from the same input in every benchmark run
  (levels 3 and 19, `.p5z` and folder, image and folder, 8 and 32 threads). New tests compare the
  read-ahead with plain reading (bytes, order and SHA-256s, at 1 to 16 threads, several piece sizes
  and memory limits down to a single piece) and check that a file that vanishes, a truncated image
  and a cancel stop it with the same message as before. The exFAT reader itself did not change.
- Memory: up to 256 MiB more while the packs are written (what is read ahead), and less at the start
  of the packs of a `.p5z`: the frames of its loose files are finished and their compression
  contexts freed before the packs begin, not when the first block of the packs is written, so the two
  are no longer in memory together. Measured at 32 threads: a `.p5z` peaks at 1022 MiB instead of
  1126-1260, a folder at 371-506 MiB instead of 112-247.
- `tools/bench_pack.py` also generates an `.exfat` image (`gen-exfat`), takes an image as input, and
  can drop the input from the system's cache before every run (`--cold`, `evict`); `mp_readbench`
  (built with the tests) measures the reading alone, the 1.0.2 way or through the read-ahead.


## [1.0.2] - 2026-11-24

### Added

- **Delete the originals once checked** (PC window, off by default): in Settings, and as a box next to
  Compress for each run. Each original goes to the recycle bin (Windows, macOS and Linux), or for good
  with **Delete them for good, not to the recycle bin**, only once its new `.p5z` or folder is made,
  verified and in its place; never when decompressing or verifying, nor when anything failed. Before
  the queue starts, the originals that go are listed with their size when one is a folder or cannot go
  to the recycle bin (a network or USB drive, a game bigger than the bin holds), which are kept unless
  you say to delete those for good. Each game, the card at the end and `maxpack.log` say what was
  removed.
- **Low priority** (PC window, Settings, off by default): `maxpack-core` runs below normal priority on
  Windows and through `nice` on macOS and Linux, so the PC stays usable while it works.
- **The space of the whole queue** (PC window) is checked before anything starts, drive by drive, and
  told with the numbers when it may not fit.
- **The card at the end** (PC window) shows the time the queue took, the games done and the average
  speed next to the space saved and what saved the most, and what became of the originals.
- **Copy report** (PC window: Details, the card at the end, Settings > About): a plain text for a GitHub
  issue with MaxPack's and the engine's versions, the system, CPU threads, memory, the settings that
  matter and, for the games it is about, their kind, sizes, volumes and result, with the row for the
  compatibility list; never a game's name, title ID or path. **Report a bug** and **Report a game** open
  the forms of maxpack-ps5.
- **What goes on the internet** (README): the window makes no connection of its own and the command
  line never goes online; Settings > Internet says so. There is no switch, as there is nothing to turn
  off.
- **File names by a template** (PC window, Settings > File names): `{title}`, `{id}`, `{version}` and
  `{region}` (from the content ID: USA, EUR, JPN, ASIA, KOR), a bracket left empty dropped, three
  ready-made templates and one of your own, with a live preview. The default, `{title} [{id}]`, gives the
  names of 1.0.1; a folder for the console keeps its `TITLE_ID`.
- **Rename** (PC window, in Check): names `.p5z` files, dump folders and `.exfat` images by what each
  holds, with the template of Settings, showing the new names before anything changes. A name taken, a
  file in use or a game that cannot be read is skipped and said; nothing is replaced.
- **Add folder takes several folders at once** (PC window): the folder dialog of Windows and of macOS
  takes several, and so does Linux's with `zenity` (one at a time without it). Several folders dropped
  at once were already taken; each keeps the rule of one folder.
- **Games added while a queue runs join it** (PC window): in the same section they show as queued, are
  done with the same options and count in the progress, the time left, the space check and the card at
  the end; one taken out before its turn leaves the queue.
- **Compare the levels on this PC** (PC window, under the level and under each game in Compress;
  `maxpack analyze <dump> --levels [L,L,...]`): a sample of the game (the one of `analyze`, 2 % of what
  is packed and at least 1 GiB) is packed at levels 3, 9, 15 and 19 with the same code and threads as
  `pack`, and the loose files are compressed as a `.p5z` compresses them, to tell for each level the
  size of the `.p5z` and of the folder, what it saves, and the time to pack the whole game on this PC.
  A card fills its table as each level is measured, and a level is chosen from it. Nothing is
  written; it can be cancelled. Text, `--json FILE`, and `level` lines with `--progress json`.
- **Measure this PC** (PC window, Settings > While working; `maxpack measure [<folder>]`): with no
  game, generated data is packed with the code of `pack` at 25, 50, 75 and 100 % of the CPU's threads,
  and the fewest threads less than 5 % slower than the fastest are recommended; the disk of the
  destination is written and read with a temporary file that is deleted, also on a cancel. The window
  keeps those threads (**Threads to compress**) and gives them to the engine when compressing; **Use
  them all** goes back to the default. About half a minute.
- **The content ID in `info --json`** (`content_id`, from `param.json`) for folders, `.exfat` images
  and `.p5z` files, made before 1.0.2 too: the `{region}` of the names now works for every game.

### Changed

- **Packing is more than twice as fast.** The engine used to read a batch of blocks and compute
  their SHA-256 on one thread, then start threads to compress the batch and wait for the slowest
  one, and then write it on one thread, so most cores sat idle most of the time. Now the three
  overlap: one thread reads ahead and hashes, a pool of threads that lives for the whole pack
  compresses, and the blocks are written in order as they are ready. The loose files of a `.p5z`
  (zstd frames of up to 32 MiB) go the same way, and one file's frames no longer wait for the
  previous file's.
- **SHA-256 uses the CPU's SHA instructions** where there are any (64-bit x86 PCs: AMD since Zen,
  Intel since Ice Lake and the Atom cores since Goldmont), about five times faster than before; the
  other CPUs, and the console, keep the portable code. With the pipeline the hashing had become what
  held the packing back. It also speeds up `verify`, `unpack` and every other check.
- Together, on a 32-thread PC, a generated 3 GB game packs to a `.p5z` in 15.9 s instead of 35.5 s
  at level 19 (the packs alone at 258 MB/s instead of 106, with the CPU 90 % busy instead of 39 %)
  and in 7.4 s instead of 25.7 s at level 3. The loose files of a `.p5z` are always compressed at
  level 19 in frames of up to 32 MiB, and no frame can be cut into parts without changing the
  bytes, so a game with large loose files gains less.
- **The output is the same, byte for byte.** Same chunks in the same order, same volumes, same
  index and same SHA-256s as 1.0.1, with any number of threads: new tests pack the test images and a
  synthetic game (many small files, a few large ones, frames of exactly 32 MiB and several volumes)
  at 1, 2 and all the CPU's threads and compare them with hashes recorded with 1.0.1. Cancelling,
  progress (the same events, always increasing) and errors behave as before.
- Memory: while the packs are written the engine now holds less (a ring of 4 blocks per thread
  instead of batches of 8, and the `.p5z` frames' compression contexts are freed before the packs
  start). While the loose files of a `.p5z` are compressed it can hold more at once than before,
  because frames of several files are compressed together, up to the same limit as 1.0.1 (8 frames).
- `tools/bench_pack.py` generates a synthetic game and compares two builds of the engine: time,
  MB/s, CPU, memory and identical output.


- **Every output of the PC window is written apart**, not only one that replaces another: the new
  `.p5z` or folder is written as `<name>.maxpack-tmp` (hidden on Windows), verified there by the engine,
  and only then renamed to its name, never over something that came there meanwhile. A cancel, an error
  or a kill takes away only what was written apart. What a crash or a power cut leaves is offered for
  deletion the next time MaxPack opens. A folder for the console is now verified every time, not only
  when it replaces another.


## [1.0.1] - 2026-11-24

MaxPack for the PC now brings its own ampr-emu-ng, and stops asking for it where it is not needed.

### Fixed

- A game made for the **MaxPack installer** (`.p5z`) no longer shows "carries no ampr-emu-ng that reads
  zstd: it will not start until given one", and **Compress** no longer opens the "ampr-emu-ng is
  missing" dialog for it. Neither was true: MaxPack for the console puts its own ampr-emu-ng in when it
  plays a `.p5z`. The warning, the dialog and the note when it ends ("done, but its libSceAmpr.sprx does
  not read zstd") are now only for folders made to **Play from the USB**.
- The hint of **MaxPack installer** says what is true now, in the eleven languages: the `.p5z` plays as
  it is on the PS5 with MaxPack for the console, or installs with its installer.

### Changed

- **ampr-emu-ng comes with the PC window.** The `libSceAmpr.sprx` of the release
  [`v0.5.1-maxpack.1`](https://github.com/DeLFuS77/ampr-emu-ng/releases/tag/v0.5.1-maxpack.1) is inside
  MaxPack (`gui/bundled/ampr-emu-ng`, checked against the release's SHA-256 by the tests). The window
  writes it to its settings folder and puts it in the `fakelib/` of every game that does not carry one
  that reads zstd, in both modes: there is nothing to download. Settings shows "Built in: ampr-emu-ng
  v0.5.1-maxpack.1"; **Choose another…** takes a `libSceAmpr.sprx` of your own, which still wins, and
  **Use the built-in one** goes back to it. The `maxpack` command line is unchanged: it takes
  `--runtime` as before.
- `THIRD_PARTY_NOTICES` names the bundled ampr-emu-ng, its licence (GPL-3.0-or-later) and its
  Corresponding Source. The [guide](docs/guide.md), the [PC window](docs/pc-window.md) and the README
  no longer tell you to download `libSceAmpr.sprx` for the window.

## [1.0.0] - 2026-11-24

First public release of MaxPack for the PC.

MaxPack lets you play compressed PS5 game dumps. It packs a game you own into zstd packs that the
console reads while you play, so the game is never decompressed on the PS5. You keep one `.p5z` file
per game and can play it directly on the console with MaxPack for the console.

### Compression results

Measured on the PC at the default level (zstd 19):

| Game | Original | Packed |
|---|---|---|
| Starfall Odyssey | 8.39 GB | 2.33 GB as a `.p5z` (2.25 GB as a folder, -72.4 %) |
| Iron Harbor | 133.59 GB | 79.32 GB as a `.p5z` (-40.6 % in the packs) |

Both were packed, verified and (Starfall Odyssey) rebuilt bit for bit with zero failures. Starfall Odyssey also started,
saved and loaded a save game on a real PS5 (firmware 13.60, ShadowMountPlus 1.7beta12). See
[docs/reports/phase1.md](docs/reports/phase1.md).

### What is included

- **PC app** for Windows, macOS (Apple silicon and Intel) and Linux. Drop your games, choose what they
  are for and press **Compress**:
  - **Compress**, **Decompress**, **Verify** and **Details** sections;
  - made for the **MaxPack installer** (one `.p5z` per game) or to **Play from the USB** (a folder in
    `homebrew/<TITLE_ID>`);
  - compression levels 1 to 22 (19 by default) with the shortcuts Light, Medium and Maximum;
  - every game is verified when it finishes; **Cancel** leaves nothing half made;
  - warnings for FAT32 (4 GiB limit), lack of space, games that are already at the destination
    (**Replace** writes the new copy apart and deletes the old one only after verifying) and runtimes
    that do not read zstd;
  - eleven languages (English, Russian, Chinese, German, Spanish, French, Japanese, Portuguese, Italian,
    Korean and Arabic), starting in the language of the system, light and dark themes, progress on the taskbar or Dock, system notification
    when finished, and a single window.
- **`maxpack` command line**: `analyze`, `pack`, `unpack`, `verify`, `info`, `icon`, `--version` and
  `--capabilities`, with `--progress json` and `--control stdin` for other programs. The same input and
  options give the same bytes with any number of threads.
- **The `.p5z` format**: one file with the zstd packs as they are, the loose files and indexes
  compressed, and a SHA-256 for every file of the original dump. `unpack --original` gives back the
  original dump bit for bit; a truncated or damaged `.p5z` is detected when opened. Documented in
  [docs/p5z-format.md](docs/p5z-format.md).
- **Packs compatible with ampr-emu-ng** (`AMPRZPK1`, 256 KiB blocks) and a generated `ampr_emu.index`,
  so the games work with ShadowMountPlus and Kraken Mount, and with MaxPack for the console.
- **Packages**: Windows installer and portable zip, macOS `.dmg` for Apple silicon and for Intel, and a
  Linux `.tar.gz` with an `install.sh`, with a `SHA256SUMS` file. They are not signed.
- **Documentation**: a step-by-step [guide](docs/guide.md) (download, PC window, command line, the
  console, ShadowMountPlus and Kraken Mount, troubleshooting), the [PC window](docs/pc-window.md) with
  screenshots, and the design documents.

### Safe by design

- MaxPack never writes to the source game, and does not touch keys, encryption or DRM.
- It refuses an output that is not empty, games that are already packed and outputs inside the source.

### Known limits

- PKG, `.ffpkg`, `.ffpfs` and `.ffpfsc` cannot be compressed yet.
- Playing a `.p5z` as it is and the library have been tested on one PS5 (firmware 13.60) with three games
  of 1, 9 and 19 volumes; the installer has not been tested on a real PS5 yet. See the section "What is tested and what is missing" of the
  [guide](docs/guide.md#8-what-is-tested-and-what-is-missing).
- The Windows installer has been tested automatically but not by hand with a double click and the
  wizard; if it does not work, use the portable zip.

### Related repositories

- [maxpack-ps5](https://github.com/DeLFuS77/maxpack-ps5): MaxPack for the console (`maxpack.elf`).
- [ampr-emu-ng](https://github.com/DeLFuS77/ampr-emu-ng): the runtime (`libSceAmpr.sprx`).

### Legal

MaxPack is for backups of games you own. It includes no games, firmware, keys or other copyrighted
content, does not help to obtain them and does not bypass encryption, keys or DRM; piracy is not
supported. It is provided "as is", without warranty, under the GPL-3.0-or-later: use it at your own
risk. The authors are not responsible for data loss, lost saves, damage to the console or storage,
bans or any other problem. MaxPack is not affiliated with or endorsed by Sony Interactive
Entertainment. Full text: [Legal and disclaimer](README.md#legal-and-disclaimer).

[1.0.4]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.4
[1.0.3]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.3
[1.0.2]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.2
[1.0.1]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.1
[1.0.0]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.0
