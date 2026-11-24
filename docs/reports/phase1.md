# Phase 1: verification with real games (22 Nov 2026)

Windows 11, Ryzen 9 7950X3D (32 threads), NVMe. `maxpack` 0.0.1 with zstd 1.5.6, level 19 (the
default). Times are wall-clock; some coincided with parallel builds.

## Starfall Odyssey (PPSA01234, 2,165 files, 8.39 GB)

| Step | Result |
|---|---|
| `pack --folder` | 95 s · 2,119 packed: 8.17 GB → 2.25 GB in 1 volume (−72.4 %) |
| `pack --p5z` | 128 s · `.p5z` of 2.33 GB |
| Generated `ampr_emu.index` | **byte-for-byte identical** to the one in the dump (made with `build_ampr_index.py`) |
| `verify` folder `--against` image | 2,163 OK, 0 failed (3 s) |
| `verify` `.p5z` | 2,168 OK, 0 failed (7 s) |
| `tools/ampr_ref.py zpk-check` (independent reader in Python) | 2,119 files, 0 errors |
| `unpack --folder` | identical to `pack --folder` |
| `unpack --original` (29 s) + independent SHA-256 in Python | 2,165 files identical to the image |
| Analyzer (1 GiB sample) | estimated 2.31 GB of packs: +2.7 % over the real result |

## Iron Harbor (PPSA05678, 49 files, 133.59 GB)

It has files of up to 72 GB, which cross several 4 GiB volumes.

| Step | Result |
|---|---|
| `pack --p5z` | 1,456 s · 21 packed: 133.23 GB → 79.18 GB in 19 volumes (−40.6 %) · `.p5z` of 79.32 GB |
| `verify` `.p5z` | 70 OK, 0 failed (291 s): every stored file and every packed file against its original SHA-256 |
| Analyzer (2 % sample) | estimated 79.87 GB of packs: +0.9 % over the real result |

## Pending

- The console test with the `libSceAmpr.sprx` of ampr-emu-ng (see
  [test-on-ps5.md](../test-on-ps5.md)). The runtimes that Starfall Odyssey and another dump carry are the old
  ampr_emu and do not read zstd packs.
- Cross-check with a third-party reader (`ps5-dump-forge inspect`): optional, needs downloading it.
- Build and test on Linux and macOS.
