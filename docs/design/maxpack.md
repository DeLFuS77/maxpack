# MaxPack: design

*Working name. 22 November 2026.*

## 1. Intent

Make a PS5 game (your own dump or homebrew) take **as little space as possible and still be playable**,
both stored on the PC and inside the console, without ever decompressing it to full size.

Why: games are big and storage is expensive. Today the best option (PS5 Dump Forge zstd packs +
ampr-emu-ng) already brings an RE Engine game from 90.6 GB down to 38.4 GB, but it falls short in
several points that can be improved (section 4).

## 2. Goals, non-goals and anti-goals

**Goals**
- G1. Maximum **playable** compression on PS5 (internal SSD, M.2 or USB), loading with ShadowMountPlus.
- G2. The file you keep on the PC is the same compressed game; when you put it on the console it stays
  compressed (only the small part that the system requires loose is decompressed).
- G3. Bit-for-bit recovery: from the file you can rebuild the original dump, verified with a SHA-256
  per file (preservation).
- G4. A PC tool for Windows, Linux and macOS, and an installer/manager on the PS5.
- G5. Decide with data: an analyzer measures, per game, how much each technique gains before it is
  applied.

**Non-goals**
- Touching keys, encryption or DRM; creating or signing PKGs; consoles without a jailbreak; PS4 games
  (for now).
- A graphical interface in the first phases (first a PC CLI and a web page on the PS5).

**Anti-goals (a failure even if it "works")**
- A compressed game that stutters or freezes: compression must not break playability.
- A format that only our tool reads and that cannot be turned back into a normal folder.
- A fork of the runtime that diverges and dies: changes are proposed to its authors.

## 3. Constraints (from the research)

- Executables and modules (`eboot.bin`, `*.elf`, `*.self`, `*.prx`, `*.sprx`), `sce_sys/`,
  `sce_module/`, `fakelib*/` and the `.ini` files must stay **loose** on the console: the system loads
  them.
- A file that the game opens with `mmap` cannot be packed (the runtime does not serve it).
- Stock runtime (ampr-emu-ng, `AMPRZPK1` format): fixed 256 KiB block, window <= 256 KiB, no
  dictionary, blocks must be consecutive (no deduplication), level 1-22, volumes <= 4,294,901,760
  bytes, and it needs `ampr_emu.index` (`AMPRIDX3`) listing every file.
- Compatibility trap: `AMPRZPK1` uses FNV-1a 64 with the standard basis (14695981039346656037) over the
  relative path, case-sensitive; `AMPRIDX3` uses the basis **1469598103934665603** (the one in
  `build_ampr_index.py`) over `/app0/...` in lowercase, and `h or 1`. Mixing them already broke games
  in exFAT2AMPR.
- Mounting an `.exfat` image reads at about 350 MB/s; a folder is faster.
- The runtime shares CPU and memory with the game (a pool of about 32 MiB plus the index).
- In this environment I can only build C (clang 18, which is what the PS5 SDK asks for) and I cannot
  test on a console: the PS5 tests are done by you.

## 4. Improvements over what exists

| # | Improvement | Needs a new runtime | Expected gain |
|---|---|---|---|
| M1 | Level 19 instead of 5 (zstd decompresses just as fast) | No | a few % (to measure) |
| M2 | Pack everything packable (Forge's "everything except system" rules) | No | depends on the game |
| M3 | Single PC file with the loose part compressed (zstd 19, long window) | No | whatever the loose part weighs |
| M4 | Deduplication of identical blocks and files | Yes | 0-30 % depending on the game |
| M5 | Group small files into solid blocks | Yes | high in games with >100k files |
| M6 | Dictionary trained per game | Yes | mostly on small files |
| M7 | Adaptive block per file (64 KiB-1 MiB depending on read pattern) | Yes | a few %, with latency risk |

The runtime improvements (M4-M7) are **only implemented if the analyzer shows they pay off**.

## 5. Approaches considered

- **A. Compatible only:** `AMPRZPK1` packs at maximum + PC file + installer. It works today with
  ampr-emu-ng and Kraken Mount, but stays at M1-M3.
- **B. A + format v2 and improved runtime, in phases.** <- **Chosen** by you. It gives the maximum
  playable compression; it costs more work and console testing.
- **C. Contribute to Dump Forge / ampr-emu-ng** without an app of our own. Discarded as the main path
  (you control neither the result nor the schedule), but the runtime improvements will be proposed
  upstream anyway.

## 6. Design

### 6.1 Pieces

1. **Core `libmaxpack` (C11)**: formats, packing, hashes (SHA-256, CRC-32, FNV-1a, XXH64), source readers
   (folder and exFAT image; later UFS2/`.ffpkg`, PFS/`.ffpfsc` and PKG). zstd is included as a submodule
   (facebook/zstd). The same code for PC and PS5.
2. **CLI `maxpack` (PC):** `analyze`, `pack`, `unpack`, `verify`, `info`.
3. **PS5 payload `maxpack.elf`** (C, ps5-payload-sdk + zstd from pacbrew-repo): web interface, installs
   `.p5z` and registers the game in ShadowMountPlus. Today it is published by DeLFuS77/maxpack-ps5,
   which uses the libraries of this repository (`src/install/`, `src/web/`).
4. **Runtime v2:** a modified version of ampr-emu-ng (Rust, GPL-3) that also reads format v2.

### 6.2 Analyzer (phase 0)

`maxpack analyze <folder|image.exfat> [--sample 2%] [--full] [--json report.json]`

- Walks the game and separates what must go loose from what is packable (same rules as 6.3).
- Compresses a deterministic sample of blocks (by default 2 %, minimum 1 GiB; `--full` does the whole
  game) and extrapolates. It measures: total/loose/packable size, number of files and size
  distribution; v1 packs at level 5 (Forge's), 9, 19 and 22; 256 KiB blocks against 1 MiB; exact
  duplicates (256 KiB blocks and whole files); grouping of files < 64 KiB; a trained dictionary; and,
  as a reference, zlib in 64 KiB blocks (about `.ffpfsc`).
- It reports the measured compression speed and estimates the total time of each option.

### 6.3 Compatible v1 packing (phase 1)

`maxpack pack <source> -o <output> [--level 19] [--runtime libSceAmpr.sprx] [--p5z | --folder]`

- **Always loose:** empty files, `eboot.bin` (in any folder), `*.elf`, `*.self`, `*.prx`, `*.sprx`,
  `*.ini`, all of `sce_sys/`, `sce_module/`, `fakelib*/` in the root, and the AMPR files
  (`ampr_emu.index`, `ampr_zstd.index`, `ampr_zstd-*.pak`, `ampr_assets*`, logs). Extra loose/packed
  rules with `--keep-loose <glob>` (in case a game uses `mmap`).
- **Packed:** the rest, in 256 KiB blocks, each one an independent zstd frame (content size in the
  header, checksum on, window <= 2^18, no dictionary). If the frame is not smaller, the block goes
  raw; if the file saves < 1 %, the whole file goes raw.
- Output identical in format to Dump Forge's: `ampr_zstd.index` (`AMPRZPK1`), `ampr_zstd-NNN.pak`
  (`AMPRZDT1`) and `ampr_emu.index` (`AMPRIDX3`) generated from the **original** list of files (loose
  and packed, with their original size and date).
- The result does not depend on the number of threads: same input, same bytes.
- If `--runtime` is passed, it is copied to `fakelib/libSceAmpr.sprx`.

### 6.4 PC archive `.p5z` (phase 1)

A single file with the game already packed:

- `.pak` volumes **stored as they are** (they are already compressed), so installing them is copying.
- Loose files and the indexes compressed with zstd 19 and a long window, in frames of up to 32 MiB
  (parallelizable and randomly accessible).
- Metadata: `param.json`, `icon0.png`, title ID, version, and a manifest with path, size, date and
  SHA-256 of **every file of the original dump**.
- Index at the end and a footer with its position and CRC (written in one pass). Header with version.
- `maxpack unpack game.p5z --original` rebuilds the original dump and verifies every SHA-256;
  `--folder` leaves the packed folder ready for the console.

### 6.5 Installer on the PS5 (phase 2)

- ELF payload with a web interface (for example on port 8095).
- Source: `.p5z` on a USB drive or on internal storage; later SMB (libsmb2) and direct sending from the
  PC (`maxpack send --to <ip>`).
- Installs to `/data/homebrew/<TITLE_ID>` or to a USB drive: copies the `.pak` files, decompresses the
  loose ones, puts `libSceAmpr.sprx` in `fakelib/`, verifies and registers it with ShadowMountPlus's
  local API (`/api/v1/manual/add` and `/api/v1/scan`), leaving the fakelib update turned off for that
  game.
- Checks free space before starting; resumes if interrupted.

### 6.6 Format v2 and runtime (phase 3, conditional on the data)

- Own format `MXPKIDX1` / `MXPKDAT1` (`maxpack.index`, `maxpack-NNN.pak`) so as not to clash with
  ampr-emu-ng's namespace.
- Block records that can point to the same physical stretch (deduplication, M4).
- Solid groups of small files: the record gives group, offset and length (M5).
- Optional dictionary per pack, loaded once as a `ZSTD_DDict` (M6).
- Block size per file, from 2^16 to 2^20 (M7).
- Runtime: a fork of ampr-emu-ng that reads v1, v2, LZ4 and PKG; batch reading is split when the blocks
  are not contiguous. It is proposed upstream once tested on a console.
- Only the improvements that the analyzer has justified are enabled.

### 6.7 Phases

| Phase | Deliverable | Who tests |
|---|---|---|
| 0 | `maxpack analyze` + Linux binary + instructions to build on Windows/macOS | You, on your PC with an `.exfat` |
| 1 | Compatible v1 `pack`, `.p5z`, bit-for-bit `unpack`, `verify` | Me (round trip) + you on PS5 with SMP + ampr-emu-ng |
| 2 | PS5 installer | You on a console |
| 3 | Format v2 + runtime | You on a console, game by game |
| 4 | GUI, reading `.ffpkg`/`.ffpfsc`/PKG, CI with binaries for the 3 systems, upstream proposals | - |

### 6.8 Verification

- Unit tests of every format with known vectors (for example, FNV-1a hashes with both bases).
- Round-trip tests on synthetic dumps: random and compressible data, duplicates, hundreds of thousands
  of small files, files over 4 GiB and volume changes (with a small limit in tests). Check: identical
  SHA-256.
- Cross-checking with third-party readers: v1 output must open with `ps5-dump-forge inspect` and with
  ampr-emu-ng on a console.
- Console checklist: start, video, load, save and 10 minutes of play, with no errors.

## 7. Decisions I made (you can change them)

- C11 + CMake language for PC and PS5 (verifiable here and shared); Rust only for the runtime.
- GPL-3.0-or-later licence (that of ampr-emu-ng, ShadowMountPlus and Dump Forge).
- The name "MaxPack" and the `.p5z` extension.
- Default level 19; 22 stays available, because with a 256 KiB window it hardly gains anything.
- Default output as a folder (faster on the console); `.exfat` image as an option later.
- `.exfat` input in phase 0; PKG later (it needs decoding Kraken).

## 8. What is left to the builder's judgement

CLI option names, thread pool, exact format of the JSON report, details of the web interface and the
internal structure of the modules.
