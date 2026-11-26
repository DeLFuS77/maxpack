# `.p5z` format (index versions 1 and 2)

A `.p5z` is a PS5 game already packed into a single file to keep on the PC. It contains:
- the folder that will go to the console: the zstd packs as they are, plus the loose files and the
  compressed indexes;
- the data to rebuild the original dump bit for bit: the manifest with the SHA-256 of every file and
  the originals that the folder replaces.

It is written by `maxpack pack --p5z`, grown by `maxpack add` and `maxpack update` (see
[A `.p5z` that grew](#a-p5z-that-grew-index-version-2)) and read by `unpack`, `verify` and `info`. All
integers are little-endian and the CRC is CRC-32 IEEE (zlib's).

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

## A game with its DLC

`pack --dlc` changes nothing in the format: the `.p5z` is still of version 1 (`maxpack add` puts DLC
into a `.p5z` already made, which then grows: see the next section). The DLC are files of the
dump it was made of, as if they had been put in it by hand: each DLC with files of its own in a folder
`addcont0/`, `addcont1/`... at the root, and `dlc_emu.ini` at the root, which MaxPack writes (a `[PSAC]`
section with `content_id` and `mount_point=/app0/addcontN` for each DLC with files, a `[PSAL]` one with
`content_id` and `download_status=NO_EXTRA_DATA` for each license). They are in the manifest with their
SHA-256, loose or packed by the same rules as any file (`dlc_emu.ini` is always loose, as every `.ini`
is, and so is each `addcontN/sce_sys/`, as the root `sce_sys/` is, so the folder exists among the loose
files), in `ampr_emu.index`, and `unpack --original` gives them back. A reader that knows nothing of DLC
reads such a `.p5z` as it reads any other; the console finds `dlc_emu.ini` among the loose files.

## A `.p5z` that grew (index version 2)

`maxpack add` (DLC that come out later) and `maxpack update` (only the files a new version of the game
changes) never pack the game again: they **append** to the `.p5z`.

```
[everything it had][new regions][new index: one zstd frame][new footer 32 B]
```

- Nothing before the old end is written again: not the header (still version 1, with the same
  `archive_id` and level), not the volumes, not the old index nor the old footer, which become room
  that nothing uses.
- The new regions are the new and changed files, stored as `pack` stores them: loose files in frames,
  the rest in **new volumes**, numbered after the old ones (`ampr_zstd-NNN.pak` with the ids that
  follow), with the header's `archive_id` and level. The headers of the new volumes count every
  volume; the old ones keep the count they had when they were written.
- `ampr_zstd.index` and `ampr_emu.index` are made again for the whole new dump (and kept as they are
  when they come out the same): a file that did not change keeps its chunks where they were, in the
  old volumes; a changed or new one points to the new volumes; a removed one is in neither. So the
  chunks of a volume may no longer be in the index's order, room that no chunk uses is left in the old
  volumes, and an old volume can end up with no chunk at all. That is what ampr-emu-ng's **p5z mode**
  takes (its `docs/p5z-mode.md`, "Growing a `.p5z`"), from `v0.5.1-maxpack.2`.
- The manifest is the new dump's: its files, sizes, dates and SHA-256. A file that did not change
  keeps its record (only its date changes when the dump's did).

### Index version 2

The index is the one of version 1 with a longer header and the same tables. Its header (72 B):

| off | type | field |
|---|---|---|
| 0 | [8] | `P5ZIDX01` |
| 8 | u32 | version = 2 |
| 12 .. 56 | | as in version 1: `n_files`, `n_frames`, `n_manifest`, the length of the strings, title ID, content version, title name |
| 56 | u64 | `prev_size`: the size of the `.p5z` before the append that wrote this index; the footer it had then is its last 32 bytes |
| 64 | u32 | `grown`: how many times the `.p5z` grew (1 after the first append) |
| 68 | u32 | 0 |

It is followed, with no gaps, by `files`, `frames`, `manifest` and the strings, as in version 1. A reader
of version 1 refuses it at its version field (byte 8), the same place as always, instead of reading it
wrong. What else a reader checks for version 2:

- `prev_size` is at least 96 and at most where the new index starts; `grown` is at least 1; the
  reserved field is 0.
- The volume records are `ampr_zstd-000.pak`, `ampr_zstd-001.pak`... with the ids 0 to n-1, each once
  (the console's `maxpack.image` lists them by id), and there is at most one `ampr_zstd.index` and one
  `ampr_emu.index`.
- MaxPack writes the records in the order a fresh pack writes them (loose files by path, volumes by id,
  `ampr_emu.index`, the runtime, the originals by path, `ampr_zstd.index`), but no reader checks that
  order, and a reader must not rely on it.
- Its packs are checked by ampr-emu-ng's p5z rules: each chunk lies in `[64, size)` of its volume and
  none overlaps another, in any order and with room between them; a volume header has its id, the
  `archive_id` and its size, and counts at most the index's volumes. Codecs, sizes, the store-only flag,
  the sums and the CRCs are checked as for any pack. A `.p5z` of version 1 keeps the rules of every pack:
  chunks back to back in the index's order and headers that count exactly the index's volumes.
- At most **256 volumes**, the most `maxpack.image` takes: an append that would pass them stops (and the
  `.p5z` stays as it was).

Readers: `unpack --original`, `verify`, `info` and `info --play` read it as above. `unpack --folder`
lays its volumes out again (no chunk is compressed again) as `pack --folder` lays them out, with the
`ampr_zstd.index` that goes with them: the folder is the one packing the new dump gives, which
ampr-emu-ng mounts outside p5z mode too. The installer of the console refuses it (`grown_p5z`), since it
leaves the packs as a folder: play it from the `.p5z`, or make a compact copy on the PC.
`info --play` ends its JSON with `"index_version":2,"grown":N` for a `.p5z` that grew (and says nothing
new for one that did not), so the console can tell it needs an ampr-emu-ng with the p5z rules.

### The room it does not use, and `compact`

The bytes nothing uses are the old indexes and footers, the regions of the files an update changed or
removed, and the room in the volumes no chunk uses: the size of the `.p5z` less its header, the regions
of its records (of a volume, its 64-byte header and its chunks' stored bytes), its index and its footer.
`maxpack info` says them, and `info --json` gives `grown` and `unused_bytes`.

`maxpack compact <in.p5z> -o <out.p5z>` writes a new `.p5z` with what the old one uses and nothing
else: the stored records are copied as they are, in a fresh pack's order, and the chunks go to new
volumes back to back in the index's order, with a new `ampr_zstd.index` (its `archive_id` that of a
fresh pack). Nothing is compressed again. What comes out is always of version 1; in the tests, an
updated `.p5z` compacted is byte for byte the `.p5z` that packing the new dump gives (with the same
volume limit and options), and one that never grew comes out the same.

### The journal of an append

An append that stops (a cancel, a full disk, a crash, a power cut) must not leave half a `.p5z`. Before
it writes anything at the end, MaxPack writes next to it `<name>.p5z.maxpack-append` (written as
`<name>.p5z.maxpack-append.tmp`, flushed and renamed, the rename flushed too: `MOVEFILE_WRITE_THROUGH`
on Windows, the folder flushed elsewhere, `F_FULLFSYNC` on macOS), 64 bytes:

| off | type | field |
|---|---|---|
| 0 | [8] | `P5ZJRNL1` |
| 8 | u32 | version = 1 |
| 12 | u32 | size = 64 |
| 16 | u64 | the size the `.p5z` had |
| 24 | [32] | its last 32 bytes then (its footer) |
| 56 | u32 | 0 |
| 60 | u32 | CRC of bytes 0..60 |

- The append holds the `.p5z` as its only writer (on Windows a write handle that others may only read;
  elsewhere `flock`). What it adds is flushed to disk before its index and footer are written; then the
  `.p5z` is flushed again and only then the journal deleted.
- Any failure cuts the `.p5z` back to the size the journal says, flushes it and deletes the journal: it
  is exactly as it was.
- After a power cut or a crash, the journal is still there: every command of MaxPack that opens a
  `.p5z` (and so the window, which reads them with the engine) first looks at it. The journal is
  `<name>.p5z.maxpack-append` or, when that is not there whole, a whole `.tmp` (its rename did not reach
  the disk); a `.tmp` cut short alone means no append began, and it is deleted.
  - When the `.p5z` is longer than the journal says and its last 32 bytes there are the footer the
    journal says, the append either had ended or not. It had ended when the `.p5z` ends with a whole
    footer whose index lies past that size and is of version 2 with `prev_size` equal to it: the `.p5z`
    is kept as it grew and only the journal deleted (a delete that failed after the append, or a power
    cut right then). Else the `.p5z` is cut back to that size and the journal deleted.
  - A journal that is damaged, or does not match (another file in its place), is deleted and the
    `.p5z` left alone.
  - A journal that is there and cannot be read (an I/O error, a drive waking up) is never taken for
    damaged: it and the `.p5z` are left as they are; `add`, `update` and `compact` stop saying so, and
    the other commands warn and read the `.p5z` as it is.
  - While another MaxPack holds the `.p5z` (an append at work), nothing is touched.
- Meanwhile, a reader that finds a journal that matches reads the `.p5z` as it was before the append,
  or as it grew when that append had ended.
- The journal is found by the `.p5z`'s name: a `.p5z` renamed after an append stopped is read as it is
  (and fails to open, its end half written) until it gets its name back, or its journal the new name.
- The repair data (PAR2) of a `.p5z` that grew no longer match it: MaxPack says so, and the window makes
  them again when the `.p5z` had them.

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
  every volume are checked, and so are that the `archive_id` in the `.p5z` header is the one in
  `ampr_zstd.index`, and that the volume records are the volumes of `ampr_zstd.index`, as many.
  `maxpack.image` takes at most 256 volumes: the console checks that itself and says "too many
  volumes", and `info --play` refuses a `.p5z` with more; `mp_play_open` does not (the console would
  take its refusal for a damaged file).
- **What the console reads:** `maxpack info --play <game.p5z>` writes as JSON the title, the version, the
  `archive_id`, the size of the `.p5z`, the loose files (`loose`, with `loose_bytes` as the sum) and the
  volumes (`volumes`, with `id`, `offset` and `length`). In C, the same is in `mp_play_open`,
  `mp_play_json` and `mp_play_extract` (`src/pack/mp_play.h`).
- **The sums, to compare two packs:** `maxpack info --sums <game.p5z>` prints the header's level and
  `archive_id` and, for each volume record in its order, its name, its size (the whole volume) and its
  SHA-256 (of bytes [64, end) of the volume): `level N`, `archive_id <32 hex>`, `ampr_zstd-NNN.pak <size>
  <64 hex>`, lowercase, each line ending in `\n`. They are read from the index, nothing is hashed again.
  In C: `mp_p5z_sums_get`, `mp_p5z_sums_lines` and `mp_p5z_sums_json` (`src/pack/mp_p5z.h`); the JSON is
  `{"archive_id","level","volumes":[{"name","size","sha256"}...]}`, and `info --json` has the same as
  `archive_id`, `level` and `volume_sums`.
- **`maxpack.image`:** with that data the payload writes the `maxpack.image` file that `ampr-emu-ng` reads
  to locate the volumes inside the `.p5z`. Its contract is in `docs/p5z-mode.md` of ampr-emu-ng.
