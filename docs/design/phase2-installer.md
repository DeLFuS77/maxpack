# MaxPack phase 2: installer on the PS5 (design)

*23 November 2026. It develops section 6.5 of [maxpack.md](maxpack.md), which remains the general
reference.*

## 1. Intent

Make installing a game compressed with MaxPack **one step on the console**: you pick the `.p5z`, press
Install and the game shows up in ShadowMountPlus (or in Kraken Mount) ready to play, still compressed.

Why (your words): if extra steps are needed to play, "it is a hassle and people will not accept it".
Today no loader understands the `.p5z`; you have to extract the folder on the PC, copy it, add the
runtime and touch the fakelib by hand. The installer does all of that by itself.

It also installs, as they are, games in the usual formats (folders and images). On the console games are
only left ready to play: **nothing is ever compressed there** (decided by you); compressing is a PC job.

## 2. Goals, non-goals and anti-goals

**Goals**
- I1. Install a `.p5z` that is on a USB drive, on extended storage or on internal storage, from a web
  page served by the console itself. The result is the same folder that `maxpack unpack --folder`
  gives, with the runtime in place.
- I2. Compatible with what people use: the game ends up in a path that ShadowMountPlus and Kraken Mount
  scan; if ShadowMountPlus is running, it is registered and its fakelib is set.
- I3. Safe: it checks space before starting, verifies the SHA-256 of everything it writes and goes on
  where it stopped if it is cut off.
- I4. A minimal manager of what was installed with MaxPack: list, Verify and Uninstall.
- I5. Page in Spanish and English, depending on the browser's language.
- I6. Install as they are (decided by you) game folders, whether dumps or folders with Dump Forge packs,
  and `.exfat`, `.ffpkg`, `.ffpfs` and `.ffpfsc` images. They are copied verified, with the same space
  check, resuming, replacing and registration as a `.p5z` (5.4 bis).

**Non-goals (in this phase)**
- Installing over SMB or with `maxpack send` from the PC (phase 2 bis, after this one).
- Compressing on the console (decided by you); installing PKG (ShadowMountPlus and etaHEN do that with
  Sony's installer); PS4 games.
- Managing games that MaxPack did not install, except replacing them with their compressed version when
  installing it (5.4, step 2).
- Format v2 and an own runtime (phase 3).

**Anti-goals (a failure even if it "works")**
- ShadowMountPlus or Kraken Mount seeing a game half installed or unverified.
- Deleting the `.p5z` before the installed game is verified.
- Overwriting or deleting a game without asking, or touching files of other games.
- Changing ShadowMountPlus's global settings (they would affect every game).
- The page allowing arbitrary paths of the console to be read or deleted.

## 3. Constraints

- **SDK:** ps5-payload-dev/sdk (GPL-3): clang-18 and lld-18, built with a Makefile, only on Linux or
  macOS. This PC has no WSL. pacbrew-repo has zstd 1.5.6 and libmicrohttpd 1.0.1.
- **Loading:** the payload is sent to the ELF loader on port 9021 (elfldr, etaHEN, Payload Manager).
  Ports already taken by the scene: 8080 (websrv), 2121 (ftpsrv), 9120/9180/19113 (ps5upload), 10101
  (ShadowMountPlus). The installer uses **8095**.
- **ShadowMountPlus 1.7:** HTTP/JSON API on `0.0.0.0:10101`, with no authentication, everything `POST`.
  `/api/v1/version`, `/api/v1/manual/add {"path"}` (idempotent), `/api/v1/scan` (can be deferred:
  `scan_deferred_reason` = `game_active`, `runtime_prepared`, `rest_mode`), `/api/v1/games`,
  `/api/v1/games/fakelib {"title_id","mode"}` with modes `full`, `emulators`, `disabled` and `default`.
  Its AMPR updater downloads drakmor's old `ampr_emu`, which does not read zstd packs.
- **Kraken Mount:** it carries ampr-emu-ng inside, cannot work at the same time as ShadowMountPlus and
  only scans one level below each root.
- **Paths both of them scan:** `/data/homebrew`, `/data/etaHEN/games`, `/mnt/ext0..1` and
  `/mnt/usb0..7` (and their `homebrew/` and `etaHEN/games/`).
- **Runtime:** ampr-emu-ng (GPL-3). Its page shows no releases; there are copies in Dump Forge
  (`vendor/ampr-emu-ng/libSceAmpr.sprx`) and inside Kraken Mount.
- **Sizes:** the `.pak` volumes are at most 4,294,901,760 bytes, so they fit in FAT32; a big `.p5z` (79 GB
  in Iron Harbor) needs an exFAT USB drive.
- **Tests:** on a console only the user can do them. The phase 1 console test has not been done yet. The
  work on the PC (5.8, steps 1 and 2) can start now; that test has to pass before step 4, and what
  comes out of it (section 7) may change the runtime and the ShadowMountPlus part.

## 4. Approaches considered

1. **Mountable image** (`.ffpkg`/`.exfat` with the packs inside) that ShadowMountPlus mounts without a
   program of ours. Simpler to adopt, but it takes somewhat more than the `.p5z`, it reads slower than
   a folder and it is not known whether ampr-emu-ng works inside a mounted image.
2. **Own installer on the console.** <- **Chosen by you.** It leaves the folder, which is the fastest
   to read, and keeps the `.p5z` as the smallest file. It costs loading one more payload and console
   testing.
3. **Ask ShadowMountPlus, Kraken Mount or ps5upload to accept `.p5z`.** It does not depend on us.

A PC-only `maxpack send` (FTP + ShadowMountPlus API) was also discarded: it forces the PC to be on and
is of no use for a `.p5z` that is already on a USB drive. It stays for phase 2 bis as a way of sending.

## 5. Design

### 5.1 Pieces

1. **`libmaxpack`** (what already exists), also built for the PS5: `.p5z` reader, packs,
   `ampr_emu.index`, hashes and zstd 1.5.6. The POSIX code of `mp_platform.c` is the starting point.
2. **Install engine** (new, portable C11): looks for `.p5z`, plans, copies, verifies, resumes, keeps the
   record of what is installed, verifies and uninstalls. It works the same on the PC and on the PS5.
3. **Web server** (new, portable): minimal HTTP with the JSON API. The page that uses it, with its own
   translations, is the console payload's (`maxpack-ps5`); this repository no longer carries a page.
4. **PS5 layer** (new, console only): payload start-up, on-screen notices, free space, mounted drives
   and ShadowMountPlus API client.
5. **PC version of the installer** (for testing): the engine and the web server built on Windows or
   Linux, with a fake ShadowMountPlus. It is not distributed; it is used to test almost everything
   without a console.
6. **Building the `.elf` in GitHub Actions** (Ubuntu, with the SDK and pacbrew) on every push, with the
   `.elf` as an artifact. That way WSL is not needed on the PC.

### 5.2 The page

It opens at `http://<PS5 IP>:8095` from the PS5 browser, a phone or a PC. The PS5 browser is an old
WebKit: simple JS, no frameworks, and progress is requested every second.

```
MaxPack · installer                           192.168.1.50:8095   ES | EN
Games found                                                     [Search again]
  [icon] Starfall Odyssey    PPSA01234 v01.000.010   2.33 GB   USB0
  [icon] Iron Harbor     PPSA05678 v01.310.000  79.32 GB   USB0
  Manual path: [____________________]
Destination: (o) Internal /data/homebrew - 512 GB free  ( ) USB0 /mnt/usb0/homebrew - 1.2 TB free
[ ] Delete the .p5z once it is installed and verified
[ Install ]
Installing Iron Harbor · 37 % · 412 MB/s · 3 min left        [Pause]
Installed with MaxPack
  Starfall Odyssey  PPSA01234  /data/homebrew/PPSA01234  ok   [Verify] [Uninstall]
```

- The title, the version and the icon come from the `.p5z` (manifest and `sce_sys/icon0.png`).
- Texts that come from the game (names) are always escaped: they are untrusted data.
- Uninstall and Replace ask for confirmation.

### 5.3 Where it looks

In the root and one level below `/mnt/usb0..7` and `/mnt/ext0..1`, and in `/data/maxpack/`, it looks for:
- `.p5z` files;
- game folders (with `sce_sys/param.json`);
- `.exfat`, `.ffpkg`, `.ffpfs` and `.ffpfsc` images.

The page says what type each one is. A path can also be typed by hand; the API only accepts paths of
those types inside those drives or `/data`.

### 5.4 Installing

1. **Open and validate** the `.p5z` (header, footer and index, like `mp_p5z_open`). A damaged one is
   rejected before anything is written.
2. **Destination:** `/data/homebrew/<TITLE_ID>`, `/mnt/extN/homebrew/<TITLE_ID>` or
   `/mnt/usbN/homebrew/<TITLE_ID>`. It looks for whether the game is already on the console: a folder
   at the destination, or the same `TITLE_ID` in another of the paths the loaders scan (reading its
   `sce_sys/param.json`, and in ShadowMountPlus's list if it is running). **Decided by you:** it can be
   replaced even if MaxPack did not install it (for example, the uncompressed dump), so that only one
   copy of each game is left.
   - It asks **Replace** or Cancel, saying which folder is deleted and how much it takes.
   - The old one is only deleted when the new one is installed and verified (step 8).
   - Nothing is ever overwritten or deleted without that confirmation.
3. **Space:** the total size of the final folder is needed (the index says it) plus a margin; if it does
   not fit, it says how much is missing and does not start. Even if "delete the `.p5z`" is ticked, the
   space is counted in full, because the `.p5z` is deleted at the end. If a game on the same disk is
   replaced, both have to fit at the same time.
4. **Temporary folder outside the paths the loaders scan** and on the same file system as the
   destination, so it can be renamed at the end: `/data/maxpack/tmp/<TITLE_ID>` for internal storage and
   `/mnt/<drive>/maxpack-tmp/<TITLE_ID>` for the others. That way no loader sees a half-installed game.
5. **Writing:** the `.pak` volumes are copied as they are and the SHA-256 that the `.p5z` stores is
   checked; the loose files are decompressed and their SHA-256 from the index is checked; then come the
   indexes. The result has to be identical byte for byte to `maxpack unpack --folder`.
   **What "verified" means:** the SHA-256 is computed over the data as it is written, and each file is
   forced to disk (`fsync`) before it is noted as finished. If "delete the `.p5z`" is ticked, before
   deleting it the whole installed folder is reread and checked again, because afterwards it will be
   the only copy. That doubles the reading (79 GB more in Iron Harbor), but only in that case.
6. **Runtime:** if the `.p5z` carries `fakelib/libSceAmpr.sprx` and it reads zstd packs (marker
   `AMPRZPK1`), that one is used. If it does not carry one, or carries the old `ampr_emu`, the copy of
   ampr-emu-ng that the installer carries is put in and a notice is shown. In that case
   **`ampr_emu.index` must be regenerated** with the row of the new runtime (size and date), just like
   `pack --runtime` does: the complete list of files is in the `.p5z` index. **It is the normal case,
   not an exception:** the Starfall Odyssey and another dump carry the old `ampr_emu`.
7. **Resuming:** a state file in `/data/maxpack/state/<TITLE_ID>.json` notes every finished and verified
   file. If it is cut off (power cut, rest mode, USB drive out, payload closed), when you press Install
   again with the same `.p5z` (same `archive_id`) the finished files are checked again by reading them
   from the destination and the ones that match are kept; the rest is redone. The size alone is not
   enough: after a power cut, a USB drive with a cache can have the right size and unwritten content.
   With another `.p5z` for the same title it starts from scratch.
8. **Finishing:** the temporary folder is renamed to the destination (when replacing: if the old one is
   in the same place, it is set aside, the new one is put in and then the old one is deleted; if it is
   on another drive, it is deleted afterwards; in both cases ShadowMountPlus is asked to scan again) and
   the record `/data/maxpack/installed/<TITLE_ID>.json` is saved (title, version, path, `archive_id`,
   runtime, date). **No MaxPack file is added inside the game folder.**
9. **ShadowMountPlus:** if `127.0.0.1:10101/api/v1/version` answers, first the game's fakelib is set
   (`games/fakelib`, with the mode that lets ampr-emu-ng work, see P1) and then `manual/add` is called
   with the final path and `scan`. The game counts as **installed** as soon as the folder is verified in
   place; the ShadowMountPlus part is a separate step:
   - if any call fails or the scan is deferred, it is noted as pending in the game's record and retried
     when the payload starts and every minute while it is loaded;
   - the page shows "pending in ShadowMountPlus", with the reason and how to do it by hand.

   If ShadowMountPlus is not running, it warns that it will see it on its next scan, or Kraken Mount will.
10. **Delete the `.p5z`**, only if it was ticked and everything is verified.
11. **Console notice:** "Starfall Odyssey installed" (or the error).

There is only one install at a time; another press is queued.

### 5.4 bis Install as they are (folders and images)

Same steps as 5.4 (destination, existing copies and Replace, space, temporary folder, resuming,
record and ShadowMountPlus), with these differences:
- **The game:**
  - in a folder or an `.exfat` image, the `TITLE_ID`, the name and the version come from
    `sce_sys/param.json` (the exFAT reader already exists);
  - in `.ffpkg`, `.ffpfs` and `.ffpfsc`, the `TITLE_ID` comes from the file name (for example
    `StarfallOdyssey-PPSA01234.ffpkg`). If it does not carry one, it is not installed and it says to add it
    to the name.
- **Destination:** a folder goes to `<root>/homebrew/<TITLE_ID>`; an image, to `<root>/homebrew/` with its
  file name. An image with the same `TITLE_ID` in its name counts as an existing copy.
- **Copy:** byte by byte, with the SHA-256 of each file computed as it is read, `fsync` and the original
  date. The SHA-256 values are saved next to the record, so that Verify and resuming have something to
  compare with.
- **Ready to play:** if a folder carries zstd packs (`ampr_zstd.index`) and lacks ampr-emu-ng (or carries
  the old `ampr_emu`), the installer's runtime is put in and its row in `ampr_emu.index` is changed, as
  in 5.4 step 6. An image is not touched.
- **ShadowMountPlus:** the fakelib is only set on folders with packs; for the rest `manual/add` and
  `scan` are enough.
- **The original** (on the USB drive) is not deleted: there is no option for that.

### 5.5 Verify and uninstall

- **Verify** checks the installed folder like `maxpack verify <folder>`: indexes, zstd frames with their
  checksum and the loose files against the `.p5z` if it is still available. For what was installed as it
  is, it rereads every file and compares it with the saved SHA-256 values.
- **Uninstall** only works with games in MaxPack's record, asks for confirmation, deletes the folder and
  the record and asks ShadowMountPlus to scan again.
- Neither Replace nor Uninstall is done while that game is running (if ShadowMountPlus says so).

### 5.6 Errors

Every error is shown on the page and in a console notice, with which file and what happened, and the
install is left in a state from which it can be resumed or discarded:
- disk full halfway, USB drive removed, damaged `.p5z` (SHA-256 that does not match): it stops, and it
  is never renamed to the destination;
- port 8095 busy (another MaxPack loaded): notice and exit;
- requested path outside the allowed drives: rejected.

### 5.7 Tests

**On the PC (CTest, without a console)**
- Install a test `.p5z` and compare with `unpack --folder` byte for byte.
- Cut the install at random points (error injected after N bytes) and resume: identical result.
- Simulated full disk, `.p5z` with one byte changed, existing destination without confirmation: no
  final folder appears and nothing outside the temporary one is touched.
- Replaced runtime: `ampr_emu.index` identical to what `pack --runtime` gives with that runtime.
- Replace an uncompressed dump: without confirmation nothing is touched; with confirmation, the old one
  is only deleted after the new one is verified; if both do not fit, it does not start; the same
  `TITLE_ID` on another drive (with another folder name) is detected.
- Fake ShadowMountPlus: `manual/add`, `scan` and `games/fakelib` are received with the right data;
  without ShadowMountPlus, the install ends all the same.
- Web API: paths outside what is allowed rejected; names with `<script>` escaped.
- As they are:
  - a dump folder ends up identical to the original (bytes and dates);
  - a folder with packs and without ampr-emu-ng gets the runtime and its `ampr_emu.index` ends up like
    the one from `pack --runtime`;
  - an `.exfat` image and a `.ffpkg` with the ID in the name are copied identical; one without an ID is
    rejected;
  - cutting and resuming work the same, and Verify detects a changed byte.

**In GitHub Actions:** the `.elf` builds and links with the SDK.

**On a console (you do it), after the phase 1 test passes**
- Load the payload and open the page from the PS5 and from a phone.
- Install Starfall Odyssey from the USB drive to internal storage, pull the USB drive out halfway, plug it back
  in and resume.
- Have ShadowMountPlus register it and have it played (the phase 1 list); the same with Kraken Mount.
- Uninstall. Install Iron Harbor (79 GB) and note the time.

### 5.8 Order of work

1. Install engine and tests on the PC.
2. Web server and page, on the PC.
3. PS5 layer and build in GitHub Actions.
4. Console test.

Then phase 2 bis: install from SMB and `maxpack send` from the PC.

## 6. Decisions I made (you can change them)

- The installer carries its own copy of ampr-emu-ng, with the GPL-3 licence and credit to its authors.
- Temporary folder outside the loaders' paths and record in `/data/maxpack/`, without adding files
  inside the game.
- Option "delete the `.p5z` when finished", unticked by default.
- Page in Spanish and English.
- Own minimal HTTP server (sockets), so as not to depend on libmicrohttpd on the PC; it can be changed
  to libmicrohttpd if the console needs it.
- Progress by polling every second, with the PS5 browser in mind.
- Building the `.elf` in GitHub Actions instead of installing WSL.
- "Verified" = SHA-256 when writing and `fsync`; everything is reread only before deleting the `.p5z`,
  when Verify is pressed and, when resuming, for the files already finished.
- "Installed" = folder verified in place; the ShadowMountPlus part is retried separately.
- The work on the PC starts without waiting for the phase 1 console test.
- As they are: the `TITLE_ID` of `.ffpkg`, `.ffpfs` and `.ffpfsc` is taken from the file name, because
  there is no UFS2 or PFS reader yet; no option to delete the original; a folder with packs gets
  ampr-emu-ng if it does not carry it; the SHA-256 values of what was copied are saved so it can be
  verified.

## 7. Still to check on a console (with the phase 1 test)

- **P1.** Which ShadowMountPlus fakelib mode (`full`, `emulators`, `disabled`) lets the game load its
  `fakelib/libSceAmpr.sprx` without ShadowMountPlus replacing it with its own; whether that mode can be
  set before ShadowMountPlus knows the game, and whether a scan done before setting it already puts
  its runtime in.
- **P2.** What ShadowMountPlus does with a game whose folder disappears (uninstall), and whether it has
  a call to remove it.
- **P3.** Whether the payload survives rest mode (if not, resuming is enough).
- **P4.** Which copy of ampr-emu-ng to start from (Dump Forge's or Kraken Mount's) and its version.

## 8. What is left to the builder's judgement

Names of the modules and of the API paths, exact format of the state and record files, how many threads
to use on the console, the space margin, how the build is split between CMake and the SDK's Makefile, the
visual design of the page and the texts.
