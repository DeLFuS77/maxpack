# Changelog

All notable changes to MaxPack are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

### Added

- **Look for new versions** (Settings › Internet, off by default; until it is on, MaxPack does not go on the
  internet): once each time the window starts, it asks GitHub's API for the latest release of
  DeLFuS77/maxpack and, only when it is newer, downloads its `SHA256SUMS` and `SHA256SUMS.minisig` and
  checks the minisign signature (Ed25519, both the signature and its trusted comment; prehashed and legacy)
  with the public key built into the window, with Go's standard library; the trusted comment must be
  `MaxPack` and the tag's version, so an old signed list put under a newer tag is refused. Only then does it
  offer it: on Windows the installer is downloaded, checked against the signed list (and again before it
  runs; kept for the next start while it is whole, and deleted once up to date), and **Update** runs it and
  closes MaxPack; on macOS, Linux and the portable version, **Open its page**. Only
  GitHub's hosts over HTTPS. The key is not in this build yet: Settings says the option is not available.
  `docs/releasing.md` says how each release is signed (`minisign -Sm SHA256SUMS -t "MaxPack X.Y.Z"`, then
  `SHA256SUMS.minisig` uploaded to the draft before it is published).
- **Keyboard shortcuts** in the window: Ctrl+O adds files, Ctrl+Shift+O a folder, Ctrl+Enter starts, Esc
  cancels the queue once asked, Ctrl+P pauses or resumes, Ctrl+1 to Ctrl+5 open the sections (⌘ on macOS).
  Settings › Keyboard lists them.
- **Accessibility:** the bars are progress bars with their value (and whether they are paused), the ✕ of a
  row says which game it takes out, the status of the queue and the notices are read out, and every text
  reads at 4.5:1 at least in both themes (links in a darker blue; coral and amber darker in the light
  theme).
- **Sound and notice at the end** (Settings › While working, on by default): a queue that ends while the
  window is not in front is told by a notice of the system with its sound; where there is no notice (the
  portable version) or it makes none (Linux), by a short sound of MaxPack. In front, only the end card.
- **Pause and resume:** `--control stdin` takes the lines `pause` and `resume` (pack, unpack, verify,
  add, update and compact; not analyze --levels and measure, which time what they do, nor send). A pause stops the work at its next batch: the
  threads end the block they have and take no other, and nothing more is read or written, so the CPU and
  the disk are free; memory stays. Resuming goes on from there with the same result. With `--progress
  json` they say `{"event":"paused"}` and `{"event":"resumed"}`; a cancel while paused cancels. In C,
  `mp_pause_set` (`src/analyze/mp_parallel.h`). In the window, **Pause** and **Resume** next to Cancel in
  Compress, Decompress and Verify: nothing else starts while paused (`maxpack-par2`, which cannot pause, is
  suspended), the taskbar shows it paused, the watched folder waits, and the PC may sleep. Closing the
  window with a paused queue says it cannot be resumed after MaxPack closes.
- **Send to the console:** `maxpack send <game.p5z> <ps5-ip> --code <6 digits> [--drive <id>]` sends a
  `.p5z` to MaxPack for the console over the network, which verifies it when it has it whole and only
  then gives it its name. It shows the MB/s and the time left, goes on from what the console has after
  a broken connection, a pause of the console or a stop (Ctrl+C, a cancel, the window closed: the console
  keeps what arrived; `--delete-on-cancel` and `send --discard` delete it), and ends with what the console's verify
  says, with an exit code and a message for each refusal (receiving off, wrong code, already there,
  busy, no space, FAT32, file name, no such drive, arrived damaged). `send --drives` lists the console's
  drives with their free space; `--json` gives progress lines for a front end. In the window: Settings
  > MaxPack for the console (its IP address and pairing code; the code goes only to `maxpack-core`, in
  its environment, never into a log or a report), and **Send to the console** on the card of a `.p5z`
  in Details and on the end card of Compress, with the drive and its free space, the bar, Cancel and
  the end card; several `.p5z` go one after another. Needs MaxPack for the console with Settings >
  Receive games from the PC.
- **Quick verify:** `maxpack verify --quick <game.p5z>` (text and `--json`) checks a `.p5z`'s header,
  footer and index, each volume's SHA-256 and header, and each stored file's SHA-256, without
  decompressing the packs: at the speed of the disk. In C, `mp_p5z_verify_quick`
  (`src/pack/mp_verify_quick.h`), with progress and cancel, for MaxPack for the console to check what it
  receives. It works on `.p5z` files as packed and grown, and reads one whose append stopped as it was.
- **Watched folder** (Settings > Watched folder, off by default): while the window is open, each dump
  folder, image or `.pkg` that arrives at the top of one folder is compressed with the settings of
  Compress once nothing new in it has changed for a minute (and, on Windows, no program holds a file of it
  open for writing; downloads at work are left alone), its DLC with it; what is in the destination already
  is skipped, a game that lacks files of its list stays in the list with its warning, and what ended is not
  handed over again until it changes. What is there when it is turned on is offered, not compressed. Its
  originals are always kept, whatever Settings says: it only compresses (and sends), and a game that changed
  while it was compressed fails and comes again once whole. **Send them to the console too** sends each `.p5z`
  made. Windows, macOS and Linux (it looks every 5 seconds).
- The HTTP client of the engine takes headers of its own and a response as soon as its `Content-Length`
  has arrived.

### Fixed

- **Growing a `.p5z` in the window:** a cancel during the verification that follows an update (or DLC
  added) said "Cancelled", although the `.p5z` had grown already, and the same update tried again then
  failed with `same_version`. The row now says that it grew and was not verified, ends as done, and
  Details reads it again. An update no longer warns that it "may not fit" for the size of the whole new
  dump: what it adds is known only once the engine compared it, and the engine stops before writing
  anything when that does not fit.
- **FAT32:** a game under 4 GiB whose DLC take it over 4 GiB gets the FAT32 warning before starting (and
  `--max-file-size` when you go on), instead of failing as it is written.
- **Compress with DLC:** the foot of the list counts the DLC apart from the games ("2 games, 2 DLC"),
  and **Copy report** of a game made counts the DLC that went inside it, not the DLC rows the list has
  now.
- **A `dlc_emu.ini` written by hand** is listed (Details, `info --json`) as dlc_emu reads it: comments
  after `#`, spaces inside the brackets, keys the same without `_`, `-` and case, a `[PSAC]` without
  `mount_point` in `addcont0`, `addcont1`... as dlc_emu puts it, and the entries dlc_emu skips (a content
  ID or a label it does not take, a label already taken, a mount point of 16 characters or more) left
  out.
- `unpack --folder` of a `.p5z` that grew: its progress ended below 100 %, as it counted the room
  between the chunks, which is not written.
- A malformed `.p5z` (its CRCs right, as a crafted one has them) is refused where it was not: `compact`
  of one with volumes or packed files but no `ampr_zstd.index` (it wrote a copy without them), `add` and
  `update` of one whose header names another `archive_id` or level than its packs (what grew would not
  open), and `info --play` (`mp_play_open`, what MaxPack for the console plays through) of one whose
  volume records are not the volumes of its packs. `info --play` also refuses a `.p5z` of more than 256
  volumes, the most `maxpack.image` takes; `mp_play_open` leaves that to the console, which says "too
  many volumes" rather than "damaged".
- **Covers:** the reader of the thumbnail provider reads an icon's frames only as large as zstd makes
  them, and at most 64, so a hostile `.p5z` cannot make the Explorer read far more than an icon.
- **Uninstall:** with the Explorer still holding the thumbnail provider, the uninstaller left
  `maxpack-thumbs.dll` and the install folder behind. It now moves the DLL to the temporary folder (sets
  it aside in the folder when that is on another drive), and never leaves a delete at a restart on the
  DLL's own path, which would take the DLL of a later install.
- The switch **Open .p5z files with MaxPack**, in the installer and in Settings, says that it also shows
  each `.p5z` with its game's cover in the Explorer, in the eleven languages.

## [1.0.7] - 2026-11-26

### Added

- **A `.p5z` grows without packing it again.** `maxpack add <game.p5z> --dlc <DLC>` puts DLC that come
  out later into a `.p5z` already made, as `pack --dlc` would have (the next `addcontN/`, `dlc_emu.ini`
  written again with the DLC it had and the new ones). `maxpack update <game.p5z> <new dump>` makes it a
  new version of its game: it compares the new dump with the `.p5z`'s list by path, size and SHA-256,
  compresses only what is new or changed, drops what is gone and keeps the DLC; it must be the same game
  in another version (a DLC given as the new dump is refused: `not_a_game`), and when more than half of
  the game changed it stops unless given `--force`. Both
  only **append**: new regions, new volumes after the old ones (same `archive_id` and level), a new
  index and footer; nothing before the old end is written again. The new index is of **version 2**
  (`prev_size` and `grown` after version 1's fields), which readers of before refuse; a `.p5z` that never
  grew stays of version 1, byte for byte. Its packs follow ampr-emu-ng's p5z rules (chunks in any order,
  room between them, headers that count fewer volumes), which need ampr-emu-ng `v0.5.1-maxpack.2`; at
  most 256 volumes. Tested on the PC end to end; not yet on a PS5.
- **Cut short, nothing lost.** Before appending, MaxPack writes `<name>.p5z.maxpack-append` (the size and
  footer the `.p5z` had). A failure or a cancel cuts the `.p5z` back exactly; after a power cut, every
  command that opens the `.p5z` (and so the window) does, and says so; a reader meanwhile sees the
  `.p5z` as it was. Another MaxPack never undoes an append at work. The journal's name is flushed to
  disk before the first byte is appended; one that cannot be read is left alone with its `.p5z` (`add`,
  `update` and `compact` stop saying so); an append that had ended before its journal could be deleted
  is kept, and `add`/`update` say so (`journal_left`).
- `maxpack compact <in.p5z> -o <out.p5z>`: a copy without the room `add` and `update` leave, copying
  what is stored and laying the chunks out again, nothing compressed again; it is of version 1, the
  `.p5z` packing that dump gives. `info` says how many times a `.p5z` grew and the room it does not use;
  `info --json` has `grown`, `unused_bytes` and `recovered`; `info --play` ends with
  `"index_version":2,"grown":N` for one that grew. `unpack --folder` of one that grew lays its volumes
  out again, so the folder works anywhere; the console installer refuses one (`grown_p5z`).
  `--capabilities` has `grow`. The repair data of a `.p5z` that grew no longer match it: MaxPack says so.
- **The window:** in Details, the card of a `.p5z` has **Add DLC…** and **Update with a new dump…**, each
  with its bar, **Cancel** and end card; the `.p5z` is verified after it grows, and gets its repair data
  again when it had some. An update of most of the game says so and offers **Update anyway**. Details
  says how many times a `.p5z` grew and the space it does not use. In its 11 languages.
- `maxpack info --sums <game.p5z>`: what the index records of its packs, as `level N`, `archive_id <hex>`
  and one `ampr_zstd-NNN.pak <size> <sha256>` line per volume (the SHA-256 of its bytes after its 64-byte
  header), the lines MaxPack for the console prints after packing on the console, so that `diff` compares
  the two packs. `info --json` of a `.p5z` has the same as `archive_id`, `level` and `volume_sums`; in C,
  `mp_p5z_sums_get`, `mp_p5z_sums_lines` and `mp_p5z_sums_json`. For `.p5z` files that grew too.
- **DLC inside the `.p5z`.** `pack --dlc <DLC>` (as many as needed) puts a game's DLC inside what is
  made of it: a PS5 package of additional content (`.pkg`, with the same passcode) or a dumped DLC
  folder (`sce_sys/param.json` and no `eboot.bin`), of the game's title ID. Each DLC with files of its
  own goes, all its files, to `addcont0/`, `addcont1/`... in the order given (at most 100); a license
  only adds no folder. MaxPack writes `dlc_emu.ini` at the root: `[PSAC]` with the `content_id` and
  `mount_point=/app0/addcontN` of each DLC with files, `[PSAL]` with `download_status=NO_EXTRA_DATA`
  for each license. On the console, MaxPack for the console finds that file loose in the `.p5z` and
  adds dlc_emu, which tells the game its DLC are installed where the file says, inside the `.p5z`: one
  file with the game and its DLC, nothing installed. The `.p5z` is still of format version 1, so MaxPack
  for the console 1.0.1 plays it; it verifies and unpacks as any other, and its original is the dump
  with the `addcontN/` folders and the `dlc_emu.ini`. A DLC's `sce_sys/` stays loose, as the game's
  does, so its `addcontN/` folder is a real one in the console's cache too. It stops before writing anything, with the reason
  and an error code for `--progress json`, for a dump that has its own `dlc_emu.ini` (`dlc_has_ini`: two
  lists are not mixed), more than 100 DLC with files or an ini over the 32 KiB dlc_emu reads
  (`dlc_too_many`), and (`dlc_refused`) a DLC of another game, the same one twice, a content ID that is
  not 36 characters, a "DLC" with the game's own content ID, a game given as a DLC, a DLC folder
  without `param.json` or already packed, or a game with no title ID. What dump tools leave behind and
  the OS's files are left out of a DLC, and make no license a DLC with files. The result line has `dlc_data`, `dlc_license` and `dlc_bytes`. A game whose
  **Fakelib** setting on the console is **Off** gets no dlc_emu and sees none of its DLC. That a game
  reads a DLC with files of its own from inside a `.p5z` has not been tried on a real PS5 yet.
- `info --json` says `content` (`game` or `dlc`), `dlc_data` (a DLC with files of its own, not a license
  only) and `dlc`: the DLC a game carries, from its own `dlc_emu.ini`, each with its content ID, whether
  it has data, its folder, and the size and files in it. A DLC's `title_id` is its content ID's. `info`
  and `icon` open a DLC package; `analyze` and `pack` still take only a game, and a DLC package given as
  one says to use `--dlc`. `--capabilities` has `dlc`.
- **The window:** in Compress, a game's DLC are dropped with it, added with **Add files** or **Add
  folder**, or with **Add DLC** under the game; each goes inside every game of the list with its title
  ID. The game's row says how many ("with 2 DLC"), the DLC's row which game it goes inside, or why it
  goes in none (no game of its ID in the list, the same DLC twice, or it came after its game started).
  One the engine would refuse (no content ID dlc_emu takes, or the content ID of a game of the list) is
  an error on its own row and never stops a game. Compress waits until every row is read, so no DLC is
  left out for being read late. A
  DLC is never compressed alone and its original is never deleted. Details says what a DLC is and lists
  the DLC of a `.p5z`; **Copy report** says how many, never which. In its 11 languages.

- **`.p5z` covers in the file manager.** With **Open .p5z files with MaxPack** on (Settings › Windows
  Explorer, or the installer), the Windows Explorer shows each `.p5z` with its game's cover, its
  `sce_sys/icon0.png`, as its thumbnail, in the views with medium icons or larger, whichever program
  opens `.p5z` files. It is `maxpack-thumbs.dll`, next to `MaxPack.exe`: a thumbnail provider of the
  Explorer, registered for you only (under `HKEY_CURRENT_USER`, no administrator) with that place and
  taken away with it, or when you uninstall MaxPack. It reads only the header, the footer and the index
  of the `.p5z` and the cover itself, never the game, within caps (an index of 64 MiB compressed or
  128 MiB unpacked, a cover of 8 MiB and 4096 pixels a side): a `.p5z` still being copied, damaged,
  over a cap or not a `.p5z` at all shows the normal icon. The cover is decoded with WIC, which comes
  with Windows, and the DLL carries its own C runtime. A place that MaxPack 1.0.6 or before turned on
  gets the covers the first time the window opens. An update goes in while the Explorer has the DLL
  loaded: the installer sets the loaded one aside and deletes it when it can. The portable version has
  no DLL, and still writes nothing in Windows.
- **Linux:** `install.sh` also installs, in `~/.local/share`, the type of `.p5z` files
  (`application/x-maxpack-p5z`, by the name and by the first bytes) and a freedesktop thumbnailer
  that runs `maxpack-core icon`, for the covers in file managers that run `.thumbnailer` files outside
  a sandbox, such as Thunar (through Tumbler); others may show them too. GNOME Files runs thumbnailers
  in a sandbox that cannot see `~/.local`, where this install puts the engine, and KDE Dolphin does not
  use `.thumbnailer` files: neither shows the covers. `install.sh --uninstall` removes them. The menu entry says MaxPack opens `.p5z` files.

### Changed

- The check of whether a dump is complete never counts `dlc_emu.ini` or the `addcont0/` to `addcont99/`
  folders at the root as files of more, against either list: what `pack --dlc` adds, or a user adds by
  hand for dlc_emu. So a dump that carries its own `dlc_emu.ini` no longer counts it as one.
- The ampr-emu-ng that comes with MaxPack is now
  [v0.5.1-maxpack.2](https://github.com/DeLFuS77/ampr-emu-ng/releases/tag/v0.5.1-maxpack.2), the release
  that also plays a `.p5z` that grew. Settings says "Built in: ampr-emu-ng v0.5.1-maxpack.2".

## [1.0.6] - 2026-11-25

### Added

- **PS5 debug packages (FPKG, `.pkg`) as the source** (engine: `pack`, `analyze` and `--levels`,
  `info`, `icon`, `verify --against`): MaxPack reads the debug packages ("fake PKGs") that
  ProsperoPkgTool and the tools that port it make of a dump, itself and read-only. It decrypts
  (AES-128-XTS; AES-128-CBC for the `sce_sys` files the package keeps outside its PFS) and decodes
  (Kraken) each block as it reads it, with nothing installed, mounted or written and no other tool.
  The keys come from the package's content ID and its passcode: the default of 32 zeros, or the one
  given with `--passcode <32 characters>`; MaxPack carries no keys. A package made with another
  passcode says so (`this package was made with another passcode; give it with --passcode`, and the
  error code `passcode` with `--json`). Retail packages (`retail packages need keys this tool does not
  have`), PS4 packages, patches and additional content are refused with the reason: only a full PS5
  game is packed. What comes out is the package's files: its PFS's `uroot`, with the `sce_sys` files
  of its CNT put back where a dump has them. So the `.p5z` is that of the folder the package was made
  from, less what its builder changed: ProsperoPkgTool fake-signs the executables, adds
  `pfs-version.dat`, `keystone` and `right.sprx` when they are missing, makes its own PlayGo files and
  `.dds` pictures, and dates every file when it built the package. A damaged or hostile package stops
  with the reason. Formats and checks: [docs/source-images.md](docs/source-images.md#ps5-packages-fpkg-pkg).
- `info --json` says `kind` `fpkg` for a package, `compressed` when its blocks are Kraken-compressed,
  and `content_id` from its `param.json` (or the package's own); `analyze --json` says it in
  `source.kind`; `--capabilities` has `fpkg` and `passcode`.
- Speed, on a 32-thread PC, for a generated 3.00 GB game (6,584 files) as a Kraken `.pkg` (2.25 GB),
  a stored `.pkg` (3.11 GB) and an `.ffpkg` (3.96 GB): reading it with every file's SHA-256, from the
  cache, 5401 MB/s from the Kraken package, 7348 from the stored one and 4786 from the `.ffpkg`. The
  blocks are decrypted and decoded by the reading threads, 16 for a package (Kraken decodes about
  600 MB/s per thread), with the CPU's AES instructions on 64-bit x86 PCs that have them (a portable
  AES elsewhere). Packing to a `.p5z` with all 32 threads: 8.4 s at level 3 from either package and
  8.2 s from the `.ffpkg`; 18.8 to 18.9 s at level 19 from all three. Peak memory 552 MiB, against
  504 MiB from the `.ffpkg`. The three `.p5z` are identical.
- **The PC window takes them too**, everywhere it takes an image: dropping them, **Add files** (the
  dialogs show `*.pkg`), **Add folder**, the command line, Send to, Compress, **Compare levels**, Details
  and Rename; and **Compress with MaxPack** on `.pkg` files in the right-click menu (Windows, for you
  only; only that entry: the program that opens them and their icon stay as they were; a menu of 1.0.5
  gets it the first time the window opens). Details says "PS5 package (FPKG)" and, under **Package
  file**, what it takes ("Kraken compression"); the room is the game's and the saving is against the
  package's file, as for an image; **Copy report** says the kind, and whether a passcode was given,
  never which.
  - **Settings › FPKG passcode**: for packages made with a passcode other than the default. A package
    that needs one says so in its row, in the 11 languages, and is read again once it is given.
  - The window needs an engine that says `fpkg` and `passcode` in `--capabilities`.
- **Whether a dump is complete** (engine and window). A PS5 package lists the files of its image in
  `sce_sys/playgo-hash-table.dat` (one hash per path, no names), and a dump that still carries it is
  checked against it: how many files the original package has, how many the dump has, how many are
  missing, what the dump has that the package does not (OS junk and the package's own entries in
  `sce_sys/` aside) and, of that, what dump tools leave behind (a `decrypted/` folder at the root,
  `*.auth_info` files, `entitlements.txt` and `entitlement_key.dat`). `sce_sys/about/right.sprx` is
  not counted: package builders add their own. A dump without the table, or with one of another
  format, is not checked, and says why.
  - `analyze` (text and JSON), `info --json` and `verify` (text and its JSON result) gain
    `completeness`: `checkable`, `reason`, `listed`, `present`, `missing`, `extra_count`,
    `extra_bytes`, `leftover_count`, `leftover_bytes`, and the first extra paths (`extra_paths`, in
    `analyze --json` and `info --json`). A `.p5z` is checked against the table it carries, a compressed
    folder with the files in its packs, and a PS5 package (`.pkg`) as a dump, with its files (a builder
    that makes its own table, as ProsperoPkgTool does, lists what it put in the package). `pack` warns
    when files are missing and packs all the same; its JSON result has the counts too. No change to the
    `.p5z` format. `--capabilities` has `completeness`, which the window needs.
  - `pack --leave-out-leftovers` leaves out what dump tools leave behind, only when the table proves it
    is not the game's: without one, everything stays and `pack` says so. `verify --against` does not
    count what was left out as missing.
  - **The window**: a Compress row warns "3 files of the original game are missing" before anything is
    packed; Details (and the card of a `.p5z`) says whether the game is complete, what is not in the
    original package and what dump tools left, with its size; Verify says it under each `.p5z` and
    compressed folder it can check. Settings › While working has **Leave out what dump tools leave
    behind** (on): what Compress does with them when the table proves they are not the game's. **Copy
    report** has the counts, never a name or a path. In the 11 languages.
  - **Without that table, the dump tool's list.** Most dumps do not carry the table; most carry the
    list their dump tool wrote, `ampr_emu.index` (every file it wrote, with its size). It is checked
    instead, and said so (`source`: `playgo` or `dump_index`): it tells whether the copy has every
    file the dump tool wrote with the size it wrote (`missing`, and the new `size_mismatch`), which
    finds copies cut short or damaged; never that the dump is complete. What a fine dump differs from
    its list in is not counted: info and checksum files that are not the game's (`*.nfo`, `*.sfv`, `*.diz`), the emulators the user
    changes (`fakelib/`, `fakelib2/`, `dlc_emu.ini`), what the game writes while it runs (`Saved/`,
    `*.log`) and the runtime's own files; on 15 real dumps it raised no false alarm
    (12 checked, 3 without a list). A list that cannot be trusted is not used, and the reason says why
    (`dump_index_damaged`, `dump_index_mismatch` for another game or version, `dump_index_packed`).
    The table wins when there is one, and only the table proves leftovers are not the game's. A
    `.p5z` keeps the dump tool's list, and says the same as its dump; a compressed folder has
    MaxPack's own list, which is not used. The window says "All 95 files the dump tool wrote are
    here" or "Damaged copy: 2 files the dump tool wrote are missing", never "complete", in Compress,
    Details, Verify and Copy report.
  - The hash and the table's layout are adapted from
    [ProsperoPkgTool](https://github.com/pearlxcore/ProsperoPkgTool) by pearlxcore (MIT); its notice is
    in `src/core/mp_playgo.c` and in `THIRD_PARTY_NOTICES`.

## [1.0.5] - 2026-11-25

### Added

- **ShadowMountPlus images as the source** (engine: `pack`, `analyze` and `--levels`, `info`, `icon`):
  besides dump folders and `.exfat`, MaxPack now packs `.ffpkg` (UFS2), `.ffpfs` (PFS, also with
  MkPFS's per-file compression) and `.ffpfsc` (a PFS container, usually PFSC-compressed, with an
  exFAT, UFS2 or PFS image inside, up to three deep). It reads them itself, read-only: nothing to
  mount, no other tool and no temporary space. What a file is comes from its contents, so a renamed
  image opens; the extension only words the message when it is none of them. The `.p5z` (or the
  folder for the console) is byte for byte that of the same game packed from its folder; the only
  thing that can differ is the files' dates, which are the ones the image gives (MkPFS writes the time
  it built the image, and 2024-01-01 in its exFAT images). A damaged or hostile image stops with the
  reason (truncated, loops, blocks out of range or used twice, sizes that overflow, PFSC blocks that
  do not inflate, odd names, links, devices, signed or encrypted PFS...). PKGs are still refused.
  Formats and checks: [docs/source-images.md](docs/source-images.md).
- **The PC window takes them too**, everywhere it took an `.exfat`: dropping them, **Add files** (the
  dialogs of Windows, macOS and Linux show `*.exfat;*.ffpkg;*.ffpfs;*.ffpfsc`), **Add folder** and the
  folders it looks into, the command line, Send to and the right-click menu (they go to Compress, or to
  Details), Compress, **Compare levels**, Details and Rename. A game moves from its image to a `.p5z` (or
  a folder for the console) in one step, and with **Delete the originals once checked** the image is the
  original that goes. The extension only puts a file in a list: what it is comes from the engine.
  - **Details** says the kind in words in the 11 languages (exFAT, UFS2 or PFS image, or PFS container
    and the image inside it) and what the image file takes, with "PFSC compression" when it is
    compressed. **Copy report** says the kind ("PFS container (.ffpfsc) with an exFAT image inside,
    compressed"), never "dump folder", and no names or paths.
  - **The room is the game's; the saving is against the image's file.** The list and the space of the
    queue count the size of the game as the engine reads it (an `.ffpfsc` is usually smaller than the
    game in it, and may be smaller than what is made of it). What a game saved (under it, the card at
    the end, the notice, **Copy report** and its row for the compatibility list) is against the image's
    file on disk, what the disk gets back, never against the game; a dump folder is against the game.
    **Compare levels** also says, for a compressed image, what each level gives against its file ("vs
    your .ffpfsc: −8 %"), and says the savings are against the game in it. `analyze` and `analyze
    --levels` say the same in their text for a compressed image.
  - **Rename** keeps an image's extension, or gives a renamed one the extension of what it is, so its
    name says it (MaxPack for the console finds images by their extension). In **Verify**, anything that
    is not compressed (an image named as a `.p5z` too) says there is nothing to verify, as a dump folder
    does; Verify does not take images, as it never took an `.exfat`.
  - **Windows Explorer:** **Compress with MaxPack** on `.ffpkg`, `.ffpfs` and `.ffpfsc` too, for you
    only, off until you turn it on, and removed with it and on uninstall; it is only that entry, the
    program that opens them and their icon stay. A menu that MaxPack 1.0.4 or before wrote gets the new
    entries the first time the window opens. On macOS the app lists the three extensions among what it
    can open (as an alternate, never the default).
  - The window needs an engine that says `images` in `--capabilities`.
- `info --json` adds `inner` (for a container, the image inside: `exfat`, `ufs2` or `pfs`) and
  `compressed`, and `kind` can be `ufs2`, `pfs` or `pfsc`; `analyze --json` says the same in
  `source.kind` and `source.inner`; `--capabilities` has `images`.
- Speed, on a 32-thread PC with an NVMe, for a generated 6 GB game (5,305 files) as an `.exfat` and a
  `.ffpfsc` made from it by MkPFS 1.1.0 (4.3 GB) and as an `.ffpkg`: reading it with every file's
  SHA-256, from the cache, 3192 MB/s from the `.exfat`, 3278 from the `.ffpkg` and 3058 from the
  `.ffpfsc`; from the disk (the cache emptied first), 2984, 2835 and 3033 MB/s. PFSC is inflated by
  the reading threads: an image that needs it is read by 16 instead of 4 (zlib inflates about
  510 MB/s per thread; with 4 the `.ffpfsc` read at 1972 MB/s). Packing to a `.p5z` with all
  32 threads takes the same from the three: 8.6 s at level 3 (median of three) and 28 to 30.5 s at
  level 19, where the compression is the limit; peak memory 1.03 GB (private), as from the `.exfat`.

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

[Unreleased]: https://github.com/DeLFuS77/maxpack/compare/v1.0.7...HEAD
[1.0.7]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.7
[1.0.6]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.6
[1.0.5]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.5
[1.0.4]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.4
[1.0.3]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.3
[1.0.2]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.2
[1.0.1]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.1
[1.0.0]: https://github.com/DeLFuS77/maxpack/releases/tag/v1.0.0
