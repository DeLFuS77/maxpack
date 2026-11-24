# The installer on the PS5 (`maxpack.elf`)

> **The `maxpack.elf` of this repository is no longer published.** The console installer is now the
> payload of [DeLFuS77/maxpack-ps5](https://github.com/DeLFuS77/maxpack-ps5): you download it from its
> **Releases**, it uses this same engine (the libraries in `src/install/` and `src/web/`) and has the
> same name and the same port (8095), so they cannot be loaded at the same time. This page describes
> what the installer does (how it installs, resumes and recovers); to load it, follow the README of
> maxpack-ps5.

The installer leaves your games ready to play on the console. It installs the `.p5z` files that
`maxpack pack` makes, which stay compressed on the console. It also installs, as they are, games in the
usual formats: folders (dumps, or folders with Dump Forge packs) and `.exfat`, `.ffpkg`, `.ffpfs` and
`.ffpfsc` images. On the console nothing is ever compressed.

You need a jailbroken PS5, an ELF loader (elfldr, etaHEN or Payload Manager) and, to play,
ShadowMountPlus or Kraken Mount.

> First of all, check on your console that a game packed with `maxpack pack` plays well:
> [test-on-ps5.md](test-on-ps5.md). The installer only places that same folder.

## 1. Load the installer

1. Download `maxpack.elf` from the Releases of
   [DeLFuS77/maxpack-ps5](https://github.com/DeLFuS77/maxpack-ps5).
2. Send it to the ELF loader, on port 9021. For example, from Linux or macOS:
   `nc -q0 <PS5 IP> 9021 < maxpack.elf`.
3. The console shows the notification "MaxPack started".
4. Open `http://<PS5 IP>:8095` in the browser of the PS5, a phone or a PC, inside your network.

If MaxPack was already loaded, open the page directly.

The page only accepts requests that arrive through the console's IP and from the page itself: another
website open on a phone or a PC on your network cannot install or delete anything.

## 2. Install

1. Plug in a USB drive with your games. If a `.p5z` weighs more than 4 GiB, the USB drive has to be
   exFAT. The installer looks in the root of every USB drive, in the root of extended storage and one
   level down, and also in `/data/maxpack/`. If something does not show up, type its path by hand.
2. Pick the game, pick where to install it and press **Install**.
   - A `.p5z` is installed in `<drive>/homebrew/<TITLE_ID>`. The packs are copied as they are and only
     the small files that the system needs loose are decompressed.
   - A folder goes to `<drive>/homebrew/<TITLE_ID>` and an image to `<drive>/homebrew/<name>`,
     identical to the original. The original is not deleted.
   - A `.ffpkg`, `.ffpfs` or `.ffpfsc` image must have the game's ID in its name, for example
     `StarfallOdyssey-PPSA01234.ffpkg`.
3. If the game is already on the console (even as your uncompressed dump, or on another drive), the
   installer tells you what will be deleted and how much it takes, and asks for confirmation. The old
   one is deleted only when the new one is installed and verified.
4. **Pause** stops the install without losing what was done. If it is interrupted (USB drive removed,
   rest mode, power cut), it shows under **Unfinished installs**:
   - **Resume** goes on where it stopped and rereads what was already copied;
   - **Discard** deletes what was copied so far.

   What the half copy already takes counts as available space when you resume. If an interruption
   catches the installer right when it is replacing a game, when you load it again it leaves the old
   game in its place.
5. "Delete the .p5z once it is installed and verified" rereads the whole installed game before deleting
   the `.p5z`, because afterwards it will be the only copy.

Progress and notices also show on the console. While it installs you do not need to keep the page open.

## 3. ShadowMountPlus and Kraken Mount

- **ShadowMountPlus:** if it is running, the installer registers the game (`manual/add` and `scan`).
  For games with zstd packs it also sets the fakelib mode, so that ShadowMountPlus does not replace
  ampr-emu-ng with its old `ampr_emu`. If ShadowMountPlus is not running or puts the scan off (for
  example, because a game is open), the game shows as **Pending in ShadowMountPlus** and the installer
  retries by itself every minute. To do it by hand: add the game's path in ShadowMountPlus and, in its
  profile, use the fakelib mode "Full" (`full`). The default, "Default (Full)", is also fine (see P1).
- **Kraken Mount:** it scans the same paths (`/data/homebrew`, `homebrew/` of every drive) and comes with
  its own ampr-emu-ng. There is nothing more to do.

## 4. Verify and uninstall

- **Verify** rereads the installed game:
  - for one from a `.p5z`, it checks the packs and their blocks and, if the `.p5z` is still where it
    was, compares every file with it;
  - for one installed as it is, it compares every file with the SHA-256 saved when it was copied.
- **Uninstall** only works with games installed with MaxPack. It asks for confirmation, deletes the
  folder or the image and removes the path from ShadowMountPlus.
- ShadowMountPlus 1.7 does not say which game is open, so **close the game before uninstalling or
  replacing it**.

## 5. Settings

`/data/maxpack/config.json`:

```json
{ "smp_fakelib_mode": "full" }
```

The possible values are `full`, `emulators`, `disabled` and `default`. The default value is `full`, and
it is checked on a console (see P1): you do not need to change it.

What the installer stores, always outside the game folders:
- `/data/maxpack/installed/`: the record of what is installed;
- `/data/maxpack/state/`: the unfinished installs;
- `/data/maxpack/tmp/` and `<drive>/maxpack-tmp/`: the temporary folders.

## 6. Console checklist

- [ ] Load `maxpack.elf` and open the page from the PS5 and from a phone.
- [ ] Install Starfall Odyssey (`.p5z`) from the USB drive to the internal storage. Pull the USB drive out
      halfway, plug it back in and resume with Install.
- [ ] Have ShadowMountPlus register it and play it: start, videos, load a save game, save and 10
      minutes of play. The same with Kraken Mount (without ShadowMountPlus running).
- [ ] Install a folder dump and a `.ffpkg` image as they are, and play them.
- [ ] Replace a game's uncompressed dump with its `.p5z` (with confirmation).
- [ ] Verify and uninstall.
- [ ] Install Iron Harbor (79 GB) and note how long it takes.

## 7. Still to check on a console

- **P1. ShadowMountPlus fakelib mode. Answered: `full`.**
  - Checked on 24 Nov 2026 on a PS5 with firmware 13.60 and ShadowMountPlus 1.7beta12, with Starfall Odyssey
    packed by MaxPack.
  - With `full` ("Full"; the per-game mode that comes by default is "Default (Full)"), ShadowMountPlus
    uses the game's `fakelib/libSceAmpr.sprx` and only adds its backport (notice "Backport applied:
    <TITLE_ID>"). It shows "ampr-emu-ng: v... (zstd) running", and the game loads and saves.
  - Since the default mode is already that one, the first scan, before the installer sets the mode,
    does not change the runtime either.
  - There was no need to try `emulators` or `disabled`.
- **P2. Uninstall.** Check that, after uninstalling, the game disappears from ShadowMountPlus. The
  installer calls `manual/remove` and `scan`.
- **P3. Rest mode.** Check whether the installer is still loaded after rest mode. If not, load it
  again: the install goes on where it was.
- **P5. USB drive not connected.** With no USB drive at all, check whether `/mnt/usb0` still exists as
  an empty folder. If so, the page will offer it as a destination even if nothing is plugged in. Report
  it so that the installer can tell the difference.
- **P4. The runtime.** It has to be decided which copy of ampr-emu-ng the installer carries inside: the
  one from Dump Forge or the one from Kraken Mount, and which version. Meanwhile it is built without a
  runtime inside, and only `.p5z` files packed with `--runtime` and folders that already carry
  ampr-emu-ng are installed.

## Building

`maxpack.elf` is built in DeLFuS77/maxpack-ps5, which carries a copy of this engine. Here only the
libraries it uses are built and tested (with `ctest`, on the PC).

To try without a console, `maxpack-installer-pc` runs the same engine and the same page on folders of
the PC:

```
maxpack-installer-pc --internal <folder> --drive <usb-folder> --runtime <libSceAmpr.sprx>
```

Then open `http://127.0.0.1:8095`.
