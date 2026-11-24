# `.p5z` format (version 1)

A `.p5z` is a PS5 game already packed into a single file to keep on the PC. It contains:
- the folder that will go to the console: the zstd packs as they are, plus the loose files and the
  compressed indexes;
- the data to rebuild the original dump bit for bit: the manifest with the SHA-256 of every file and
  the originals that the folder replaces.

It is written by `maxpack pack --p5z` and read by `unpack`, `verify` and `info`. All integers are
little-endian and the CRC is CRC-32 IEEE (zlib's).

## Layout

```
[header 64 B][data ...][index: one zstd frame][footer 32 B]
```

### Header (64 B)

| off | type | field |
|---|---|---|
| 0 | [8] | `MAXPKP5Z` |
| 8 | u32 | version = 1 |
| 12 | u32 | header size = 64 |
| 16 | u32 | flags = 0 |
| 20 | u8 | zstd level of the packs |
| 21 | [3] | 0 |
| 24 | [16] | archive_id (the one in `ampr_zstd.index`; zeros if there are no packs) |
| 40 | [20] | 0 |
| 60 | u32 | CRC of the header with bytes 60..64 set to zero |

### Data

Regions one after another from byte 64, in the order the packing produces them:
1. loose files, in path order;
2. `ampr_zstd-NNN.pak` volumes;
3. `ampr_emu.index`;
4. `fakelib/libSceAmpr.sprx` from `--runtime`;
5. replaced originals;
6. `ampr_zstd.index`.

How each region is stored:
- **Volumes:** raw, identical to the file in the folder; installing is copying.
- **Everything else:** independent zstd frames of up to 32 MiB, at level 19, window 2^25,
  long-distance matching and checksum.

### Footer (32 B)

| off | type | field |
|---|---|---|
| 0 | [8] | `P5ZFOOT1` |
| 8 | u64 | index offset |
| 16 | u64 | compressed index length |
| 24 | u32 | CRC of the compressed index |
| 28 | u32 | CRC of footer bytes 0..28 |

The index ends right where the footer starts. Since the footer is written last, a truncated `.p5z` is
recognised.

### Index (uncompressed)

Index header (56 B):

| off | type | field |
|---|---|---|
| 0 | [8] | `P5ZIDX01` |
| 8 | u32 | version = 1 |
| 12 | u32 | n_files (stored files) |
| 16 | u32 | n_frames |
| 20 | u32 | n_manifest (files of the original dump) |
| 24 | u64 | length of the strings |
| 32 | u32, u32 | offset and length of the title ID in the strings |
| 40 | u32, u32 | same, content version |
| 48 | u32, u32 | same, title name |

It is followed, with no gaps, by `files`, `frames`, `manifest` and the strings.

**files** (88 B per record):

| off | type | field |
|---|---|---|
| 0 | u32, u32 | path: offset and length in the strings |
| 8 | u8 | role: 0 loose, 1 volume, 2 `ampr_zstd.index`, 3 `ampr_emu.index`, 4 runtime, 5 replaced original |
| 9 | u8 | storage: 0 raw, 1 frames |
| 10 | u16 | 0 |
| 12 | u64 | size |
| 20 | i64 | mtime (Unix seconds) |
| 28 | u64 | region start |
| 36 | u64 | region length |
| 44 | u32 | first frame (in `frames`) |
| 48 | u32 | number of frames |
| 52 | [32] | SHA-256 of the content; for a volume, of its bytes [64, end), because the header has its own CRC |
| 84 | u32 | 0 |

**frames** (16 B): u64 offset · u32 compressed size · u32 uncompressed size.

**manifest** (64 B, one per file of the original dump, in path order):

| off | type | field |
|---|---|---|
| 0 | u32, u32 | path |
| 8 | u32 | flags: bit0 packed (it is in the packs), bit1 replaced (in `files` with role 5) |
| 12 | u32 | index in `files` (0xFFFFFFFF if packed) |
| 16 | u64 | size |
| 24 | i64 | mtime |
| 32 | [32] | SHA-256 |

## Rebuilding

- **`unpack --folder`:** writes the files of `files` except those with role 5. It is the folder for the
  console, the same as `pack --folder`.
- **`unpack --original`:** rebuilds every manifest record, checking its SHA-256:
  - packed: it is decoded from the packs;
  - loose: it is the file of `files` that the record points to;
  - replaced: it is its original with role 5.

## Playing without installing (console)

The console can play a `.p5z` without installing it, and without leaving on the internal disk more than
the essentials.

- **What is extracted:** only the loose files, that is, the files with role 0 (the game's loose files), 2
  (`ampr_zstd.index`) and 3 (`ampr_emu.index`). Each one is written whole, with its date, and its SHA-256
  is checked. The volumes (role 1), the runtime (role 4) and the originals (role 5) are never extracted.
- **What is not extracted:** the `ampr_zstd-NNN.pak` volumes are read in place, inside the `.p5z`, where
  they are stored uncompressed. Each volume starts at `offset` (the `data_off` of its record) and is
  `length` bytes long, 64 B header included.
- **Opening:** before playing, the header, the footer, the index, `ampr_zstd.index` and the header of
  every volume are checked, and so is that the `archive_id` in the `.p5z` header is the one in
  `ampr_zstd.index`.
- **What the console reads:** `maxpack info --play <game.p5z>` writes as JSON the title, the version, the
  `archive_id`, the size of the `.p5z`, the loose files (`loose`, with `loose_bytes` as the sum) and the
  volumes (`volumes`, with `id`, `offset` and `length`). In C, the same is in `mp_play_open`,
  `mp_play_json` and `mp_play_extract` (`src/pack/mp_play.h`).
- **`maxpack.image`:** with that data the payload writes the `maxpack.image` file that `ampr-emu-ng` reads
  to locate the volumes inside the `.p5z`. Its contract is in `docs/p5z-mode.md` of ampr-emu-ng.
