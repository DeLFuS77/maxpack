<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# How to read the `maxpack analyze` report

`maxpack analyze` measures how much each compression technique would save on a given game, without
writing anything. Its options are in the [guide](guide.md#analyze-measure-before-compressing).

```
maxpack analyze <folder|image.exfat> [options]

  --sample N%        sample percentage (default 2 %, minimum 1 GiB)
  --full             measure the whole game, with no sample
  --json FILE        save the report as JSON
  --threads N        threads (default: all the CPU's)
  --keep-loose GLOB  also leave loose whatever matches (can be repeated); '*' and '?' inside one
                     folder, '**' any number of folders. E.g.: --keep-loose "**/*.bnk"
  --no-dedup         skip the deduplication pass (which reads the whole game)
  --no-transport     do not measure the transport .p5z (zstd 19 with a long window)
  --list             only list the files and whether they would go loose or packed
```

`--json` never overwrites the game being analysed nor writes inside its folder: if it points there, or
to a folder that does not exist, `maxpack` refuses before starting. Example:

```
maxpack analyze "D:\games\MyGame-PPSA00000.exfat" --json report.json
```

Progress goes to stderr and the report to stdout. If the source is a PKG, for now it is rejected with a
warning: PKG support is planned for phase 4.

Reports of the games analysed are in [reports/](reports/).

## What the report says

- **Loose** is what the console needs as a normal file: executables and modules (`eboot.bin`, `*.elf`,
  `*.self`, `*.prx`, `*.sprx`), `*.ini`, `sce_sys/`, `sce_module/`, `fakelib*/` and the AMPR files.
  **Packable** is everything else.
- **Sample**: a deterministic sample of 1 MiB windows is compressed (by default 2 %, minimum 1 GiB) and
  extrapolated by file type (extension). With `--full` everything is measured and the result is exact.
  The same input and the same options give the same figures with any number of threads.
- **Configurations**:
  - *v1 level 5 / 9 / 19 / 22*: packs compatible with ampr-emu-ng (`AMPRZPK1`), 256 KiB blocks. Level 5
    is the one Dump Forge uses by default. Forge's rule is applied: a block that does not improve is
    stored raw (like Forge, the whole file is no longer stored raw when it saves little).
  - *v2 1 MiB block* and *v2 dictionary*: improvements that would need a new runtime (M7 and M6 of the
    [design](design/maxpack.md)). The dictionary is trained on one half of the data and measured on
    the other. With any dictionary, zstd gives up the double pass of the first block that it does at
    level 19 (it costs 0.75-0.9 %); that is a real cost of using a dictionary and so it is included.
  - *zlib 64 KiB*: a rough reference of `.ffpfsc` (it does not count its block table).
- **Console total** = loose + packed (+ `AMPRZPK1`/`AMPRIDX3` indexes in v1). **Saved** = 1 - console
  total / original size.
- **MB/s/thread** and **Est. time** are the measured compression speed and the estimated time to pack
  the whole game with the threads used.
- **Deduplication** (M4): identical 256 KiB blocks and files, by XXH64, over the whole game.
- **Files < 64 KiB** (M5): compressed one by one (as in v1), grouped into solid 256 KiB blocks, or one
  by one with a dictionary.
- **.p5z estimate**: what the single PC file would take with v1 level 19 and the loose files compressed
  with zstd 19 and a long window. It is installed by copying the packs as they are.
- **Transport**: what a `.p5z` meant only for keeping on the PC would take, with everything packable
  compressed like the loose files (zstd 19, long window, 32 MiB frames). It takes less, but on install
  the PS5 would have to decompress it and compress it again as v1, which makes the install much longer.
  It is measured on a sample of 32 MiB stretches.
