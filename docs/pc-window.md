<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# The MaxPack window for the PC

MaxPack has a window for Windows, macOS and Linux. It does the same as the command line
(`maxpack pack`, `unpack`, `verify` and `info`), without typing commands: you drop the games, choose
what you want them for and press **Compress**.

![The window with the light theme](img/window-light.jpg)

![The window with the dark theme](img/window-dark.jpg)

## 1. Download and open

The releases are in **Releases** at https://github.com/DeLFuS77/maxpack/releases. Download the package
for your system (the installer or the portable zip on Windows, the `.dmg` or the `.tar.gz` on macOS,
the `.tar.gz` on Linux) and, if you want to check it, compare its SHA-256 with the one in
`SHA256SUMS`, on the same page. Every package carries `LICENSE` and `THIRD_PARTY_NOTICES` (the
licences of the third-party software they include).

Then follow the steps for your system.

**Windows**
- `maxpack-windows-x64-setup.exe` is the installer. It installs MaxPack in your user folder, without
  administrator rights, with a shortcut, and you uninstall it from "Installed apps". A page of its own
  offers MaxPack's places in the Windows Explorer (the right-click menu, Send to and opening `.p5z`
  files, with their covers as thumbnails), each off until you tick it: see
  [Windows Explorer](#windows-explorer). The uninstaller takes them all away.
- `maxpack-windows-x64-portable.zip` is the portable version: unzip it anywhere and open
  `MaxPack.exe`. It has a `settings` folder next to it, and while that is there MaxPack keeps all its
  data (settings, window size and position) in it and nothing in your user profile. It is handy for a
  USB drive. It writes nothing in Windows: no notifications and no places in the Explorer (so no
  covers on `.p5z` files either).
- `MaxPack.exe` and `maxpack-core.exe` must stay together. The window uses `maxpack-core.exe` to do
  its work, and if it is missing the window tells you what is missing. If the one there is of another
  version (an old one that does not do everything the window needs), the window says "maxpack-core of
  another version" and does not let you start: put next to MaxPack the `maxpack-core` that came with
  it.

> Neither the installer nor `MaxPack.exe` is signed: Windows SmartScreen will warn you the first time
> ("Windows protected your PC"). Press **More info** and **Run anyway**.

> The Windows installer is checked automatically on every build (silent install with its places in the
> Explorer, an update while the covers' DLL is in use, compress a test image and uninstall), but it has
> not been tested by hand yet with a double click and the on-screen wizard. If something does not work,
> use the portable zip and report it. The covers are checked by a test that loads their DLL as the
> Explorer does, and on Linux by running the thumbnailer's command; not yet by hand in a file manager.

**macOS**
- Open the `.dmg` and drag `MaxPack.app` to Applications. The `.tar.gz` carries the same app.
- The app is not notarized. The first time, open it with a right click, **Open**. On macOS 15 or
  later, if that does not work, try to open it once, then in System Settings, Privacy & Security,
  press **Open Anyway**.
- The Apple silicon and Intel versions are different: choose the one for your Mac.

**Linux**
- Unpack the `.tar.gz` and run `sh install.sh`. It puts MaxPack in the applications menu, with its
  icon, and `maxpack` in `~/.local/bin`; and, in `~/.local/share`, the type of `.p5z` files and a
  freedesktop thumbnailer, which gives each `.p5z` its game's cover (its `sce_sys/icon0.png`, which
  `maxpack-core icon` takes out). `sh install.sh --uninstall` removes it all.
- Where the covers show: in file managers that run `.thumbnailer` files outside a sandbox, such as
  Thunar (through Tumbler); other file managers may show them too. **GNOME Files** runs thumbnailers in
  a sandbox that cannot see `~/.local`, where this install puts the engine, so it shows no covers, and
  **KDE Dolphin** does not use `.thumbnailer` files. A file manager that tried a `.p5z` before may
  remember that it failed (in `~/.cache/thumbnails/fail`): deleting that folder lets it try again.
- It needs GTK 3 and WebKitGTK 4.1, which almost every desktop has.

On all three systems, opening MaxPack with games or folders (dragging them to the icon, Send to, the
right-click menu or a double click on a `.p5z` on Windows, the Finder, or the command line) adds them to
the window that is already open, which comes to the front: there is only one. With none open, the window
opens with them. Folders, images (`.exfat`, `.ffpkg`, `.ffpfs`, `.ffpfsc`) and PS5 packages (`.pkg`)
go to **Compress** and a
`.p5z` to **Details**, unless the right-click menu asked for another section. With command-line options
(`MaxPack pack ...`, `MaxPack --version`) it behaves like `maxpack`, as always.

## 2. Compress

1. Drop games or folders here, or use **Add files** and **Add folder**. PS5 dump folders, images and
   packages are fine (see [Images](#images)). **Add folder** takes several folders at once (on Linux, when `zenity`
   is there; else one at a time), and each one can be a game or a folder with games right below it;
   several dropped at once are each taken the same way. Each game shows its title, ID, version and size.
   A game packed already with zstd packs (a folder made by Dump Forge, Alembic or MaxPack, or an image
   with one, such as a `.zstd`) shows "already packed (zstd, level 5)" and is compressed from the files
   it was made of (see [Games packed already](#games-packed-already)); one with LZ4 packs shows "it is
   packed with LZ4 packs" and is skipped.
   A game's DLC can come along (see [DLC](#dlc) below).
2. Choose **Made for**.
3. Choose the compression **level**, 1 to 22 (19 by default), or one of the shortcuts **Light**,
   **Medium** and **Maximum**. 20 to 22 use a lot of memory and take much longer to gain little.
   **Compare the levels on this PC** (under the level, for the first game of the list) or **Compare
   levels** (under each game) shows what each level gives that game here (see below).
4. Choose the **Destination** and press **Compress**. Each game is verified when it finishes.
5. If you want, tick **Delete the originals once checked** (see below).
6. If you want, tick **Add repair data** and choose **1 %**, **5 %** or **10 %** (see below). Only for a
   `.p5z`: for "Play from the USB" it is off, and says why.

**Compare the levels on this PC.** A card compresses a sample of the game (2 % of what is packed, at
least 1 GiB, read once) at levels 3, 9, 15 and 19, with the same code and the same threads as Compress,
and fills a table as each level is measured: the level, the size of the `.p5z` (or of the folder, when
**Made for** is "Play from the USB"), what it saves and how long compressing the whole game takes on
this PC. The level in use is highlighted, and **Use** on a row sets that level once all are measured.
Nothing is written. **Cancel**, Escape or closing the card stops it; nothing else starts meanwhile. The
sizes are estimates from the sample; the times assume the game is read about as fast as its sample was,
so a game on a hard drive may compress faster than they say. It is `maxpack analyze --levels` (see
the [guide](guide.md#analyze---levels-compare-the-levels-on-this-pc)).

**Complete dumps.** A dump that still carries the list of the original package's files
(`sce_sys/playgo-hash-table.dat`) is checked against it as it is added: one that lacks some shows "3
files of the original game are missing" on its row, before anything starts. It is compressed all the
same, and what is made lacks them too: dump it again if you can. Details says more (below). What dump
tools leave behind (a `decrypted/` folder, `.auth_info` files) is left out of what is made, when the
list proves it is not the game's; **Leave out what dump tools leave behind**, in Settings, keeps it.
Most dumps do not carry that list, but most carry the one their dump tool wrote (`ampr_emu.index`):
then the copy is checked against it, and a copy that lacks files the dump tool wrote, or has them of
another size, says so ("2 files the dump tool wrote are missing"); that list does not prove the dump has
every file of the game, so it never says complete, and nothing is left out because of it. See
[the guide](guide.md#whether-a-dump-is-complete).

Before anything starts, MaxPack adds up what the games may take on each drive (each one at most its
original size, the game's and not its image's, with its repair data when it gets some) and, if the queue
may not fit, says so with the numbers: "up to 322.4 GB needed, 118.2 GB free". You can choose another
destination or **Start anyway**: a game that fills its drive fails, and its original stays as it was.

While a queue runs, games you add to the same section join it: they show as **Queued**, are done after
the others with the same options, and count in the progress, the time left and the summary at the end.
The **✕** of a game that has not started takes it out of the queue. Games added to another section wait
there, as before.

**Every output is written apart.** The new `.p5z` or folder is written as `<name>.maxpack-tmp`, hidden
on Windows, and verified there; only then does it take its name. Something that came to that name
meanwhile is never replaced: the game says so and the new copy goes. If MaxPack is closed by a crash or
a power cut in the middle, the next time it opens it offers to delete what was left apart, with its
size; **Not now** asks again the next time.

**Delete the originals once checked** (off by default; Settings has the default, and the box next to
Compress the choice for this run): each original (the dump folder, or the image) goes to the recycle bin
(the Recycle Bin of Windows, the Trash of macOS, the trash of Linux), or for good if Settings says so,
only once its new copy is made, verified and in its place. Never when decompressing or verifying, and
never when anything failed or was cancelled.
- Before the queue starts, MaxPack lists the originals that go, with their size, when one of them is a
  folder or cannot go to the recycle bin. **Keep the originals** starts the queue without deleting any.
- Some drives have no recycle bin (network folders, USB drives), and a game can be bigger than the
  recycle bin of its drive holds (on Windows it is 5 to 10 % of the drive by default). Those are
  kept, unless you tick "Delete for good the ones that cannot go to the recycle bin".
- Each game says what became of its original, the summary at the end tells how much went to the
  recycle bin (its space comes back once it is emptied), and the log of the window (`maxpack.log`, in
  its settings folder; Settings shows where) keeps every original removed.

**Add repair data** (off by default; Settings has the default and keeps the share; 5 % the first time)
gives each `.p5z` a standard PAR2 set next to it, so a damaged `.p5z` can be repaired back to the exact
bytes it was made with (see [Repair data](#5-repair-data)). Once the `.p5z` is made, verified and in its
place, its bar gets a third step, **Repair data**, with its progress; Cancel stops it too. If making it
fails or is cancelled, the game is still done (its `.p5z` is good), its row says why, and no part of the
set is left: `maxpack-par2` makes it in `<name>.p5z.maxpack-par2`, and its files take their names next to
the `.p5z` only once they are all made. Each `.p5z` done says what its set takes ("7.8 GB → 3.3 GB ·
repair data 168.4 MB"), and the card at the end counts the repair data made and the games left without
it. A new `.p5z` that replaces an old one of the same name takes the old one's set away with it.

![MaxPack compressing, with the progress of each game](img/window-working.jpg)

While there is work, the PC does not go to sleep. Progress also shows on the taskbar (Windows) or in
the Dock (macOS). When it ends while MaxPack is not in front, the system notifies you, with its sound
(Settings › While working › **Sound and notice at the end**, on by default); in front, the end card says
it. The portable version of Windows does not notify: to send notifications it would have to register in
the Windows registry, and the portable one writes nothing there, so it plays a short sound of its own
instead (as on Linux, whose notices make none).

**Pause** (next to **Cancel**, in Compress, Decompress and Verify) frees the PC for something else
without losing anything:
- the game at work stops at its next batch: each thread ends the block it has (a few MiB), then nothing
  more is read, compressed or written, so the CPU and the disk are idle; the memory it uses stays;
- nothing else starts: not the next game, nor the verification or the repair data of this one
  (`maxpack-par2`, which cannot be told to pause, is suspended);
- the bars stand still, the status says "Paused", the taskbar shows it paused (the Dock keeps its
  percentage), the watched folder is not looked at, and the PC may go to sleep meanwhile;
- **Resume** goes on from where it stopped, and what is made is the same as without the pause;
- **Cancel** while paused cancels as usual. A paused queue cannot be resumed once MaxPack closes:
  closing the window says so before it cancels it. A queue whose last game ends while it is paused has
  nothing to resume: its end card comes at once.

Low priority (Settings) is still there apart: it keeps the work going, but behind everything else.

Cancelling, or closing the window while work is running, stops the current game without leaving
anything half made or unverified:
- what it had written is deleted, and the destination is left as it was: without the game if it was
  not there, or empty if it was an empty folder; with **Replace**, the old one stays intact;
- `maxpack-core` finishes what it has in hand first and then deletes, so with a big game or a slow USB
  drive it can take a few seconds;
- if you close the window, `maxpack-core` is not cut off: it goes on by itself until it has deleted
  everything, even if the window is already gone;
- if something cannot be deleted (for example, a file open in another program), the row says so, with
  the path, so you can delete it yourself.

When it ends, the result shows. If the queue compressed and saved space, the size before squeezes down
to the size now, with a bar that shrinks, and the GB saved count up. For a game that was an image, the
size before is the image's file, so what it says is saved is what the disk gets back. Below, you see what saved the most
and what to do next. If the system asks for less motion, the final numbers show without animation.

![The result: what saved the most and what to do next](img/window-result.jpg)

### Games packed already

When the list has a game packed already with zstd packs, the panel shows **Games packed already**:

- **Compress again (smaller)**, the default: its packs are read decompressed and compressed at the level
  above, by MaxPack's rules; the `.p5z` is the one its dump would give.
- **Keep their packs (faster)**: the chunks of its packs go in as they are, checked as they are read; what
  is made keeps their level (the level above is not used for it). What MaxPack leaves loose comes out of
  the packs.

The choice is kept for next time. Either way the other tool's runtime (`fakelib/libSceAmpr.sprx`) is left
out: for **Play from the USB** the folder gets the ampr-emu-ng in use, as a dump does. The room it needs is
what its files take unpacked. Once made, its row says "made from a packed game, compressed again" or "made
from a packed game, its packs kept (zstd level 5)". A game whose packs are damaged, or that lacks a file its
`ampr_emu.index` lists, fails and says so; nothing of it is left. Its DLC go inside it as they go inside a
dump. See [the guide](guide.md#a-game-packed-already).

### Images

A game kept as an image moves to a `.p5z` (or a folder for the console) in one step: no dumping it
again, no other tool and no temporary space. MaxPack takes the images
[ShadowMountPlus](https://github.com/drakmor/ShadowMountPlus) mounts, and PS5 debug packages,
everywhere it takes a dump folder
(dropping them, **Add files**, **Add folder**, the right-click menu, Send to, Details, Rename and
**Compare levels**):

| extension | what it is |
|---|---|
| `.exfat` | exFAT image |
| `.ffpkg` | UFS2 image |
| `.ffpfs` | PFS image (its files may be PFSC-compressed) |
| `.ffpfsc` | PFS container: a PFS image whose only file is another image, usually PFSC-compressed |
| `.pkg` | PS5 debug package (FPKG): decrypted with its passcode as it is read (see below) |

- `maxpack-core` reads them itself, read-only, without mounting anything, and tells what each one is by
  its contents: the extension only puts it in the list. A renamed image opens too; Details says what it
  is, and Rename gives it the extension of what it is. The `.p5z` is the one of the same game as a
  folder; only the files' dates can differ, as the image gives them
  ([source-images.md](source-images.md)).
- **The room it needs is the game's; what it saves is against the image's file.** The size of its
  row, and the total at the foot of the list, is what the image's (or package's) file takes on disk,
  as your file manager says; when it is compressed (an `.ffpfsc`, or a `.pkg` with compressed blocks)
  and the game in it takes more unpacked, the line under its name says so ("512.0 GB unpacked"). The
  space of the queue counts the size of the game in the image, as `maxpack-core` reads it: an `.ffpfsc`
  is compressed, so the game in it (and what is made of it) can be bigger than the file. What a game
  saved, under it, on the card at the end ("before" and "saved"), in the notice and in **Copy
  report**, is against the image's file on disk, what your disk gets back, never against the game.
  **Compare levels** says what each level saves against the game, and for a compressed image
  (`.ffpfsc`, or an `.ffpfs` with compressed files) or a package also against your file ("vs your .ffpfsc: −8 %"),
  with a line saying which is which. Details shows both sizes: the original size and, under **Image
  file**, what the file takes ("PFSC compression" when it is compressed).
- **The image is the original.** With **Delete the originals once checked**, it is the image (or the
  package) that goes,
  once its `.p5z` or folder is made, verified and in its place, as a dump folder would.
- A `.p5z` made from an image or a package decompresses to the game's folder, not to an image or a
  package.
- **Verify** does not take images or packages, as it never took an `.exfat`: there is nothing of
  MaxPack's in them to check. What is made of them is verified.
- **PS5 packages (`.pkg`).** A debug package (FPKG, a "fake PKG") is read like an image: `maxpack-core`
  decrypts and decompresses it as it reads it, with nothing installed or written. It needs the passcode
  it was made with: the default (32 zeros, what fake packages use) unless Settings › **FPKG passcode**
  has another. A package made with another one says so in its row ("this package was made with another
  passcode: give it in Settings › FPKG passcode"); give it there, and the rows that needed it are read
  again. Retail packages, PS4 packages and patches are not taken, and the row says why; a package of
  additional content is a DLC, which goes inside its game ([DLC](#dlc)). What comes out is the files the package holds, and its builder did not keep the dump as it
  was: it fake-signs the executables, adds a few `sce_sys` files (`pfs-version.dat`, `right.sprx`, the
  PlayGo tables, `.dds` pictures), makes its own `playgo-chunk.dat`, and gives every file the date it
  built the package. So the `.p5z` is the one of what the builder packed, not always byte for byte of
  your first dump ([source-images.md](source-images.md#ps5-packages-fpkg-pkg)). Details shows "PS5
  package (FPKG)" and, under **Package file**, what it takes ("Kraken compression" when its blocks are
  compressed).

### DLC

A game's DLC go inside what is made of it, its `.p5z` or its folder for the console, and play from
there with nothing installed (`maxpack pack --dlc`, see the
[guide](guide.md#dlc-inside-the-p5z)).
- **Adding them.** A DLC is its package (`.pkg`, a debug package of additional content, opened with the
  passcode of Settings as a game's is) or a dumped DLC folder (`sce_sys/param.json` and no `eboot.bin`).
  Drop them with the game, add them with **Add files** or **Add folder**, or press **Add DLC** under a
  game, which asks for `.pkg` files. MaxPack tells a DLC from a game as it reads it: its row says "DLC",
  or "DLC, license only" for one with no files of its own.
- **Each DLC goes inside the game of its title ID**, the one in its content ID, wherever the two are in
  the list and whichever was read first; with two games of that ID (two versions), inside both. The
  game's row says how many go inside it ("with 2 DLC"), and the DLC's row which game it goes inside
  ("Goes inside Starfall Odyssey", and once made "Went inside ..."). A DLC is never compressed alone, and
  its original is never deleted, whatever **Delete the originals once checked** says.
- **A DLC that goes in none says why:** there is no game of its ID in the list ("No game of PPSA10009
  in the list: add it and this DLC goes inside it"), it is the same DLC as another one of the list (only
  the first goes), or it came after its game started ("compress it again to put this DLC inside"). One
  that `maxpack-core` would refuse says it as an error on its own row and goes inside no game, so it
  never stops one: it has no content ID that dlc_emu takes (36 characters, in its `param.json`), or it
  has the content ID of a game of the list (it is that game, not a DLC of it).
- **Compress waits for every row to be read**, as a game's DLC may be among them: until then it says
  how many are still being read ("1 is still being read: Compress waits for them, so no DLC is left
  out"), and a DLC is never left out of its game for being read late.
- The room a game needs counts its DLC with files of their own, and so does what it saved: what is made
  holds them too. A license only adds no files, only its line in the game's `dlc_emu.ini`.
- **What stops a game:** a dump that already has a `dlc_emu.ini` of its own ("its dump has a
  dlc_emu.ini of its own"): MaxPack writes that file for the DLC it puts inside and does not mix two
  lists; take the DLC out of the list, or that file out of the dump. More than 100 DLC with files of
  their own for one game stops it too. Either way nothing is left written.
- **Details** says what a DLC is and lists the DLC of a `.p5z` made with them: how many, and each one
  with its content ID and what its files take, or "license only". **Copy report** says how many, never
  which.
- On the console, the `.p5z` needs dlc_emu, which MaxPack for the console adds by itself when it finds
  the `dlc_emu.ini`: a game whose **Fakelib** setting is **Off** gets none, and sees none of its DLC.

### Made for: MaxPack installer

This is the normal choice. Each game is saved in **one `.p5z` file** named `<Title> [<TITLE_ID>].p5z`.
It is the smallest and keeps well on the PC or on a USB drive. It needs no runtime from you: MaxPack for
the console puts its own ampr-emu-ng in when it plays it, and the window puts the one that comes with it
in the `.p5z` of a game that does not carry one.

Afterwards:
1. Copy the `.p5z` files to a USB drive.
2. Plug the USB drive into the PS5 and open it with MaxPack for the console, which is the payload from
   [DeLFuS77/maxpack-ps5](https://github.com/DeLFuS77/maxpack-ps5) (`maxpack.elf`;
   [how its installation works](ps5-installer.md)). It plays the `.p5z` as it is, or installs the game
   in `<drive>/homebrew/<TITLE_ID>` still compressed on the console.

### Made for: Play from the USB

Each game is saved in **a folder ready for the console**, with the zstd packs that ampr-emu-ng serves.
- If the destination is the root of a drive (for example `E:\` or a USB drive), each game goes to
  `<destination>/homebrew/<TITLE_ID>`.
- If you choose any other folder, each game goes inside it, with its `<TITLE_ID>`.

Afterwards:
1. Put the folders in `homebrew/` on the USB drive or on the internal storage of the PS5. If you made
   them in another folder, move them there: ShadowMountPlus only looks in `homebrew/`.
2. ShadowMountPlus will see and mount them. Since they do not go through the installer, you set each
   game's fakelib mode in ShadowMountPlus, so that it does not change ampr-emu-ng:
   [test-on-ps5.md](test-on-ps5.md). Kraken Mount comes with its own ampr-emu-ng.

## 3. Decompress and verify

- **Decompress** turns a `.p5z` back into:
  - the **Original dump (bit for bit)**, with every file checked against its SHA-256; or
  - the **Folder for the PS5**, the same one that "Play from the USB" produces.
- **Verify** reads a `.p5z` or a compressed folder and checks that it is still whole. It changes
  nothing. It does not take images or packages (see [Images](#images)). When the dump it was made of
  carried the list of the original package's files, a line under it says whether that dump was complete
  ("Complete: all 1234 files of the original package", or "Incomplete: 3 of the 1234 files of the
  original package are missing"); without it, from the dump tool's list, "All 95 files the dump tool
  wrote are here" or "Damaged copy: ...". Under a `.p5z` with repair data it says what
  it can repair; a damaged one with repair data of its own gets **Repair**, and **Add repair data** and
  **Repair the damaged ones** do it for the list (see [Repair data](#5-repair-data)).
- **Details** shows what a game says: title, ID, version, sizes, files, packs and whether it carries a
  runtime that reads zstd. **Complete** says whether it has every file of the original package ("Yes:
  all 1234 files...", how many are missing, or why it cannot tell: no list, a list of another format, a
  damaged one); **Not in the original package** what it has that the package does not, with its size;
  **Left by dump tools** what of that dump tools leave behind, with its size and whether Compress leaves
  it out (only when the list proves it is not the game's). Without the original package's list, **Dump
  tool's list** says whether the copy has every file its dump tool wrote, with its size, and **Not in the
  dump tool's list** what was added since; it never says complete. An image or a package says what it
  is (a container, also the image inside it) and what its file takes. **Copy report** under each game
  copies a text for an issue on GitHub (see
  [Copy report](#copy-report)). A `.p5z` shows as a card with what it saved against the original, its
  volumes and its repair data (how much, or none), and four buttons: **Verify** and **Decompress** take it to those sections, where you start
  them; **Add DLC…** and **Update with a new dump…** grow it in its place (below). A `.p5z` opened with a double click (see [Windows Explorer](#windows-explorer)) opens here.
- **Add DLC…** asks for DLC packages (`.pkg`) or dumped DLC folders and puts them into the `.p5z` as
  Compress would have (`maxpack add`); **Update with a new dump…** asks for the new version of the game
  (a folder, or an image or package) and adds only the files it changes (`maxpack update`). Only what is
  new is written, at the end of the `.p5z`; then it is verified and, when it had repair data of its own,
  they are made again of the same share. Each runs with its bar (Adding, then Verifying), **Cancel** and
  the end card, which says that a `.p5z` that grew plays from the `.p5z` with MaxPack for the console
  and needs ampr-emu-ng v0.5.1-maxpack.2 or later. Cancelled, failed or cut by a power cut, the `.p5z`
  stays as it was (the engine undoes the append; a power cut, the next time the `.p5z` is read, with a
  notice). Cancelled during the verification that follows, it has grown already: the row says so, and
  that it was not verified (**Verify** on its card does it). When more than half of the game changed,
  the update stops and says that compressing the new dump again gives a smaller `.p5z`; **Update
  anyway** under it goes on. The card then says how many
  times it grew and the space it does not use (the command line's `maxpack compact` makes a copy
  without it). See [the guide](guide.md#add-update-and-compact-grow-a-p5z).
- **Rename**, in Check, names `.p5z` files, dump folders, images and packages by what each one holds,
  with the template of Settings > File names. An image or a package keeps its extension, or gets the one of what it is when it
  was renamed, so its name says it (MaxPack for the console, for one, finds images by their extension).
  Add them and each shows its new name before anything changes; **Rename** applies it. A name that
  something else in the folder already has, two games of the list that would get the same name, a file
  another program has open and a game that cannot be read are skipped, and the row says why. Nothing is
  ever replaced, and the game stays in its folder. A folder for the console keeps its `TITLE_ID`. The
  repair data of a `.p5z` is renamed with it, all or nothing; a name of it that is taken blocks the
  rename as the name of the `.p5z` does.

## 4. Settings

Everything can be reached with the keyboard: the Tab key goes through the page in its order and the
focus always shows; the bars and the buttons that are only a sign say what they are to screen readers,
and what changes by itself (the status of the queue, the notices) is read out. Text reads at 4.5:1 at
least in both themes, and nothing moves when the system asks for less motion.

- **Language** and **Theme** (System, Light or Dark). The window speaks English, Russian, Chinese,
  German, Spanish, French, Japanese, Portuguese (Brazil), Italian, Korean and Arabic (written from right to
  left), each shown by its own name. Until you choose one it uses the language of your system, or English
  if the window does not have it; **System** goes back to that. The window's notice and its question when
  you close it with games at work follow the language too.
- **Runtime for the games that lack one.** The zstd packs are served by ampr-emu-ng, and some dumps
  carry none, or an old `libSceAmpr.sprx` that does not read them. For those games MaxPack puts the
  `libSceAmpr.sprx` of ampr-emu-ng in their `fakelib/`, in both modes. MaxPack comes with its own
  (Settings shows "Built in: ampr-emu-ng v0.5.1-maxpack.2"), which it writes to its settings folder and
  uses by itself: there is nothing to download. **Choose another…** takes a `libSceAmpr.sprx` of your
  own instead, which MaxPack remembers; **Use the built-in one** goes back to the one that comes with it.
  - A `.p5z` is never warned about the runtime: MaxPack for the console puts its own ampr-emu-ng in
    when it plays one. The warnings below are only for folders made for "Play from the USB".
  - MaxPack checks that the one you choose reads zstd (the one of ampr-emu-ng carries the mark
    `AMPRZPK1`). If it does not, it does not accept it and asks for another: with it, the game would be
    compressed and verified fine, but it would not start.
  - If a game still comes out with a runtime that does not read zstd, its row says so when it ends, and
    the summary counts how many games will not start until you give them the one of ampr-emu-ng.
  - If a folder needs one and there is none (the built-in one could not be written and Settings has
    none), MaxPack asks for it once before starting.
  - If you go on without it, it warns you that the game will not start until it has ampr-emu-ng, and
    lets you go on.

- **While working.** **Low priority** runs `maxpack-core` below the other programs, so the PC stays
  usable while it works (below normal on Windows, `nice` on macOS and Linux); it goes a little slower
  when the other programs need the processor. **Threads to compress** says how many threads Compress
  gives `maxpack-core`: all of the processor's (the default), or the ones **Measure this PC** found.
  The measure takes about half a minute and needs no game: a card shows how fast generated data
  compresses with a quarter, half, three quarters and all the threads, at the level of Compress, then
  the speed of the disk of the destination (a temporary file is written there, read back and deleted;
  with no destination chosen, only the processor is measured), and keeps the fewest threads that are
  less than 5 % slower than the fastest: on a PC whose last threads add little, Compress goes as fast
  and leaves the PC freer. **Use them all** goes back to all of them. Decompress and Verify always use
  all of them. **Cancel**, Escape or closing the card stops it, and the threads stay as they were.
  **Leave out what dump tools leave behind** (on by default): the `decrypted/` folder and the
  `.auth_info` files some dump tools leave stay out of what Compress makes, only when the game's own
  list of the original package's files proves they are not the game's; a game without that list keeps
  them.
  **Sound and notice at the end** (on by default): a queue that ends while MaxPack is not in front
  says so with a notice of the system and its sound.
  **Repair data for each .p5z** is the default of the box next to Compress, with its share (1, 5 or
  10 %). **Delete the originals once checked** is the default of the box next to Compress, and **Delete
  them for good, not to the recycle bin** skips the recycle bin.
- **File names.** The name of each `.p5z` and of each original dump unpacked comes from a template:
  `{title}`, `{id}`, `{version}` (the one of `param.json`, such as 01.000.021) and `{region}` (USA,
  EUR, JPN, ASIA or KOR, from the first two letters of the content ID). A bracket whose placeholders
  are all empty goes. The default, `{title} [{id}]`, gives the names of before; the other two add the
  version, and the region and the version. You can write your own; the preview shows what it gives with
  a game of your lists, or with an invented one. A folder for the console keeps its `TITLE_ID`, which
  the console looks for. The region is known for every kind of game: `maxpack-core` reads the content
  ID of its `param.json`, also inside a `.p5z`, an image or a package; a game whose `param.json` gives
  none has no region, and its bracket goes.
- **FPKG passcode.** Only for PS5 packages (`.pkg`) made with a passcode other than the default (32
  zeros, what fake packages use): type its 32 characters (letters, digits or signs, no spaces); empty is
  the default. One that is not 32 such characters is not kept, and the box says why. Once it changes,
  the packages of the lists that needed another passcode are read again with it. It goes to
  `maxpack-core` as `--passcode` for every game it reads; **Copy report** says whether one was given,
  never which.
- **MaxPack for the console:** the console's IP address (as its page shows it; `a.b.c.d:port` when it
  is not on port 8095) and the 6-digit pairing code it shows in its Settings > Receive games from the
  PC, for [Send to the console](#send-to-the-console). The code is kept in the settings file and goes
  only to `maxpack-core`, in its environment, never on a command line, into a log or into **Copy
  report** (which says only whether an address and a code were given).
- **Watched folder:** one folder, off by default; see [Watched folder](#watched-folder).
- **Windows Explorer** (Windows only): the right-click menu, Send to › MaxPack and opening `.p5z`
  files with MaxPack (with their covers as thumbnails), each a switch, off until you turn it on. See
  [Windows Explorer](#windows-explorer).
- **Keyboard** lists the shortcuts (⌘ in place of Ctrl on macOS):
  - **Ctrl+O** adds files and **Ctrl+Shift+O** a folder to the section open;
  - **Ctrl+Enter** starts (as its button, when it can);
  - **Esc** cancels the queue running, once you say yes (also with a checkbox, a choice or a slider in focus;
    not while you type in a field); with a card open, Esc closes the card instead;
  - **Ctrl+P** pauses or resumes the queue;
  - **Ctrl+1** to **Ctrl+5** open Compress, Decompress, Verify, Details and Rename.
- **Internet.** MaxPack's window goes on the internet for nothing, unless you turn on **Look for new
  versions** (off by default; see [What goes on the internet](../README.md#what-goes-on-the-internet)):
  - once each time MaxPack starts, it asks GitHub for the latest release of DeLFuS77/maxpack;
  - only when that is newer does it download the release's `SHA256SUMS` and `SHA256SUMS.minisig`, and it
    offers the new version only when the signature is valid for one of the release keys built into MaxPack
    (the main one, or the spare one that signs if the first is lost);
  - the signature must also name that version (`MaxPack X.Y.Z`), so the signed list of an older release
    is never offered under a newer number;
  - on Windows it then downloads the installer, checks its SHA-256 against the signed list, and **Update**
    runs it (checked again first) and closes MaxPack; not while a queue runs. The installer stays in the
    settings folder (`updates/`) and is used again at the next start, so it is downloaded once; it goes
    once MaxPack is up to date;
  - on macOS, on Linux and in the portable version, **Open its page** opens the release on GitHub;
  - a release not signed, or signed with another key, is not offered, and nothing past its list is
    downloaded. The line under the switch says what it found.

  A build without any release key (`gui/internal/update/release.pub` empty) would say "Not available in
  this build", and its switch could not be turned on.
- **About** has **Copy report** and the links to report a bug or a game.

### Windows Explorer

Three places of MaxPack in the Explorer, each one off until you turn it on in Settings, or tick it in the
installer. Each is for you only (it is written in your part of the registry, or in your own Send to
folder), needs no administrator, and goes away when you turn it off or uninstall MaxPack.

- **Right-click menu.** **Compress with MaxPack** on a folder, an image (`.exfat`, `.ffpkg`, `.ffpfs`,
  `.ffpfsc`) or a PS5 package (`.pkg`), and **Verify with MaxPack** and **Details** on a `.p5z`, each
  with MaxPack's icon. Several files chosen at once go to the window together. On the images and the
  packages it is only that entry: the program that opens them and their icon stay the ones you had
  (other tools open them). A menu that MaxPack 1.0.4 or before put there gets the entries on `.ffpkg`,
  `.ffpfs` and `.ffpfsc`, and one of 1.0.5 the entry on `.pkg`, the first time the window opens.
  On Windows 11 they are under **Show more options** (or Shift+F10): its compact menu only takes the
  entries of apps installed as packages, which MaxPack is not. The menu speaks the language of the
  window, and changes with it.
- **Send to › MaxPack.** MaxPack in the Send to menu of a right click: the files and folders chosen go to
  the window, folders, images and packages to Compress and `.p5z` files to Details.
- **Open .p5z files with MaxPack.** A double click on a `.p5z` opens its card in **Details**. MaxPack is
  also in **Open with** and in Settings › Apps › Default apps. If another program already opens `.p5z`
  files, MaxPack leaves it, as Windows wants: choose MaxPack once in **Open with › Choose another app ›
  Always**, and Settings tells you while that is still to do.

  With it, the Explorer also shows each `.p5z` with its game's cover (its `sce_sys/icon0.png`) as its
  thumbnail, in the views with medium icons or larger, whichever program opens `.p5z` files. The covers
  come from `maxpack-thumbs.dll`, next to `MaxPack.exe`, which the Explorer runs in a process of its
  own. It reads only the start and the end of the `.p5z`, its index and the cover, never the game: a
  `.p5z` still being copied, damaged or that is not a `.p5z` shows the normal icon. If MaxPack 1.0.6 or
  before turned this on, the covers come the first time the window opens. The Explorer keeps the
  thumbnails it made: a folder it had already shown may keep the old icons until Windows makes them
  again (Disk Cleanup › Thumbnails empties that cache).

If MaxPack is moved (or another copy put a place there), Settings says the place is for another copy:
turn it on and it points to this one. The **portable version** writes nothing in Windows, neither in
the registry nor in your folders: its switches stay off and Settings says why. Install MaxPack to have
them.

### Copy report

A plain text for an issue on GitHub: MaxPack's version, the engine's, the system and its version, the
CPU threads, the memory, the settings that matter (mode, level, threads, runtime built in or your own,
low priority, the originals, the template of the names, whether an FPKG passcode was given, what becomes
of the packs of a game packed already) and, for
the games it is about, whether each is complete against the original package (the counts: files
listed, missing, not in the package and left by dump tools, never which), what each one is
(for an image, its kind: "PFS container (.ffpfsc) with an exFAT image inside, compressed", and what its
file takes, which its row for the list is against), what a game packed already is packed with and at what
level, its sizes, its volumes, how many DLC it has or goes
with and how many of them have files of their own, and how it ended. It never has a game's name, its
title ID, a DLC's content ID or a path. For a
`.p5z` (or a game just compressed) it ends with the row for the
[compatibility list](https://github.com/DeLFuS77/maxpack-ps5/blob/main/docs/compatibility.md), with the
sizes and the volumes filled in and a `?` for what only the console knows. **Report a bug** and **Report
a game** open the forms of [maxpack-ps5](https://github.com/DeLFuS77/maxpack-ps5/issues) in your
browser.

### Send to the console

**Send to the console** is on the card of a `.p5z` in Details, and on the end card of Compress when it
made `.p5z` files (they are added to Details, read, and sent from there).

- **Before:** the console's IP address and pairing code must be in Settings (without them the button
  says so and opens Settings), and MaxPack for the console must have Settings > Receive games from the
  PC on.
- **The card** lists the `.p5z` that go and the console's drives, as it says them: internal storage and
  each USB drive with its free space of its size; one where they do not fit with the console's margin
  (256 MiB) says so. The drive chosen is the one of the next time.
- **While it sends,** each row has its bar in two parts, the bytes and then the console's verify, with
  the speed and the time left at the foot; several `.p5z` go one after another. If the connection
  breaks, `maxpack-core` waits a moment and goes on from what the console has; a receive the console
  pauses (idle, a write error) is started again, one stopped on the console is not. **Cancel**, or closing
  the window, stops it: the console keeps what arrived, and the next send goes on from there.
- **Delete what arrived on the console** shows on a row whose send stopped, was stopped on the console,
  stalled (or this PC slept), could not be written, got no answer or arrived damaged, and on one that found
  no room on the drive while part of it is there from before (that part takes room): it asks first, then the
  console deletes what it received of it, and the next send starts from nothing. It is the console's own
  request for it, which a full drive does not refuse; when nothing of it was there, the row says so. While the
  console lets go of that receive it waits, up to 75 s, and its row says so (with the seconds) for the whole
  wait; a stop here (Cancel, closing the window) tells the console at once that the PC left. An engine that had to be killed after a cancel says the console
  may keep some. A console without that request says to update it; one that could not delete says why.
- **Asking for the drives** opens the card at once with "Asking the console for its drives…": **Cancel**
  stops the question, and it gives up after 30 s (a console never reached says "no answer" in about 11 s).
- **The end card** says how many went and how fast. A row that ended well says that the console
  verified it; the others say why in words of their own: no answer, receiving off, wrong code (or too
  many), already there, the console busy, no room, a FAT32 drive, a name it does not take, a drive it
  no longer has, cancelled on the console, or arrived damaged with what did not match (the console keeps
  it aside as `<name>.maxpack-recv`; send it again). A send this PC slept through says the PC slept, not that
  nothing moved on the console.

### Watched folder

Settings > Watched folder: **Choose…** picks the folder, the switch turns it on and off, and **Send them
to the console too** sends what it makes. Turning it on, or choosing another folder, takes a snapshot: what is
there then is not new, and **Compress the N already there** compresses it when you ask. It works while the window is open,
minimized too: no service stays in the system.

- **What counts:** at the top of the folder only, a dump folder (it has `sce_sys/param.json`; a game
  also its `eboot.bin`, a DLC not), an image (`.exfat`, `.ffpkg`, `.ffpfs`, `.ffpfsc`) or a `.pkg`.
  Its subfolders, other files and what MaxPack writes apart (`*.maxpack-tmp`) are not looked at.
- **When:** the folder is looked at every 5 seconds; what is new goes once nothing new has changed (in
  size, date or number of files) for a minute, all of it at once, so a game and its DLC go together.
- **How:** as if dropped on Compress: its row, its bar and the end card, with the destination, level and
  repair data of Compress; nothing is asked.
- **Its original is always kept,** whatever Settings or the queue it joins say of originals: the watched
  folder only compresses (and sends, when asked), and the row says so. A game whose files changed while it was
  compressed (read through each file before and after) fails, nothing made of it is kept or sent, and it comes
  again once it is whole. Without a destination it waits for one. What
  is in the destination already is skipped ("it is in the destination already"), and nothing is
  replaced, but for one case: a game it compressed well that grew or lost files afterwards (its copy stalled
  for a minute, and went on) is compressed again in place of the `.p5z` it made, while the destination still
  has that very file (the size and date it had, kept in `watched.json`), of the same game in the same version;
  written apart and checked as always, only that `.p5z` is replaced, and the row says "it changed after it
  was compressed: compressed again". At most twice in a row (then "compress it by hand"), never for a change
  of dates alone (nor of a game of one file), nor when its version is not known; the page asks for it apart and
  the window decides it again before anything is made (the game must still be as it was handed over, else it
  comes again) and right before the swap, where it reads what was made and what it would replace: both must be
  the game and version planned, that file still the one it made, the watched folder on; else nothing is
  replaced (the row says so). The `.p5z` replaced goes to the recycle bin as `<name>.p5z.maxpack-old-<date>`, or,
  on a drive without one, stays beside it under that name: never deleted for good. A new version of a game (an update dump) whose `.p5z` is in the destination is skipped
  with both versions on its row: **Update with a new dump…** on its card in Details puts it in; an older dump
  is said to be older. Nothing is updated by itself. A game that lacks files of its list (the 1.0.6 check) stays in
  the list with its warning. When a queue of Compress is running, what arrives joins it, with the same
  rules.
- **Never twice:** what ended (compressed, failed, or skipped as in the destination already) is kept in
  `watched.json` next to the settings, with what it was then; it is handed over again only once it changes.
  What never ran (no destination, no room, cancelled, the window closed) is tried again the next time it is
  stable: those waiting in Compress every 30 s and when a destination is chosen, the others in the next run.
- **Still arriving:** on Windows each file is read through itself (a listing may lag behind a file being
  written), and one another program still holds open for writing is still arriving. On Linux, MaxPack looks
  in `/proc` for a file of it that a program of your own user holds open for writing (not the programs of
  other users, nor a writer on another machine of a network share). macOS has no such look: there only the
  quiet minute and the names of downloads tell a file still arriving. All that is pending is looked for in
  `/proc` at once, once per look. A game that waits a couple of minutes for a program writing into it is named
  in Settings ("Waiting for a program that still writes into…"). A `.part`, `.crdownload`,
  `.!qB`, `.!ut` or `.filepart` download, or an empty file in place of a download, is never a game. What systems
  write into folders (`.DS_Store`, `._*`, `Thumbs.db`, `desktop.ini`, `.Spotlight-V100`, `$RECYCLE.BIN`,
  `System Volume Information`...) never makes a dump new or changing. One whose file cannot be opened (not
  because a program writes it) is looked at three times, then left alone, and said, until it changes.
- **Compress the N already there** goes through the same checks: they go once whole, by the next looks.
- **What the window puts there** (Decompress to the watched folder, a folder for the console made there) is
  never new to it.
- **To the console too:** each `.p5z` the watched folder made goes on to the console of Settings, to
  the drive chosen last, once its queue ends. One made again after its game changed, which the console
  has already, says the console has the older copy: delete it there first, then send it again.
- **The folder gone** (a USB drive taken out) is said in Settings, and watching goes on when it is back.
- **A share that drops:** the looks hold nothing else, so closing the window or choosing another folder does
  not wait for a slow listing, and once it is stopped nothing more of it is told; what one listing misses is
  forgotten only once it is missing from 3 in a row, and a listing that fails, an empty one (a share that went
  from its mount point) or one that lists an entry it cannot check forgets nothing.

## 5. Repair data

A `.p5z` can carry standard **PAR2** repair data next to it, and MaxPack repairs a damaged `.p5z` with it
back to the exact bytes it was made with. `maxpack-par2` (par2cmdline-turbo, built from its source) comes
with MaxPack, next to `maxpack-core`; a build of the window without it offers no repair data, and says
why.

- **The files.** `<name>.p5z.par2` (a small index) and `<name>.p5z.volA+B.par2` (the recovery data),
  next to the `.p5z`. Keep them with it and copy them with it. Any PAR2 tool reads them.
- **How much.** 1, 5 or 10 % of the `.p5z`: what the set takes, and the most it can repair. The `.p5z` is
  cut into about 4000 blocks, each a whole number of 4 KiB (so a bad sector is in one block) and 32 MiB
  at most; each damaged spot, from one byte to a whole block, uses up one block of the set. Under a
  `.p5z` with repair data, Verify says it: "Repair data: 5 % — can repair up to 200 blocks of 1.3 MB,
  268.8 MB in all".
- **Its own set only.** A set is used only for the `.p5z` it protects: one file of its size and of the
  MD5 of its first 16 KiB (or, when those 16 KiB are damaged, of the MD5 of some of its other blocks).
  Files named as its set that are of another file show "repair data of another file" and are never
  used, nor replaced; ones that cannot be read show so.
- **Repair.** When Verify finds a `.p5z` damaged and it has repair data of its own, **Repair** shows
  under it; **Repair the damaged ones** does it for all of them in the list. MaxPack first checks that
  the drive of the `.p5z` has room for a whole copy of it (plus a margin), and if not, says so with the
  numbers and does not start. The `.p5z` is moved (on its own drive, so at once) into
  `<name>.p5z.maxpack-repair`, where `maxpack-par2` repairs it as par2 does: it keeps the damaged one as
  `<name>.1` and writes the repaired one. `maxpack-core` verifies the repaired one before it takes the
  `.p5z`'s place, and only then does the damaged one go. If it cannot be repaired (too much damage: the
  row says how many more blocks were needed), what was repaired does not verify, or you cancel, the
  `.p5z` goes back exactly as it was. The bar has two steps, **Repairing** and **Verifying**.
- **A repair cut short.** The folder of the repair has a file that says how far it went. At the next
  start MaxPack offers to put the `.p5z` back: the repaired one if it was already verified, else the
  damaged one as it was. A folder that may have a copy of a `.p5z` that is also in its place is kept,
  and said: look at both. Nothing is ever deleted without asking.
- **Add repair data.** In Verify, **Add repair data** gives a set, of the share chosen above it, to every
  `.p5z` in the list: each is verified by `maxpack-core` first, and only a good one gets one. One that
  already has its own of that share keeps it; of another share, it is replaced.
- **Rename.** Rename takes the set with its `.p5z`. The set keeps, inside, the name the `.p5z` had when
  it was made; MaxPack gives `maxpack-par2` the `.p5z` itself, so a renamed one still verifies and
  repairs. In MultiPar or QuickPar, after a rename, add the `.p5z` by hand when they say the file is
  missing.
- **Copy report** says the share of repair data of Settings, never a name.
- **On the console.** MaxPack for the console ignores the `.par2` files: there is no need to copy them
  there.
- **By hand.** The `par2` commands that do the same are in the
  [guide](guide.md#repair-data-with-par2).

## 6. Warnings

- **FAT32.** FAT32 does not take files over 4 GiB, and a `.p5z` can go over that. If the destination is
  FAT32 and the original game, with the DLC that go inside it, is over 4 GiB, MaxPack tells you before
  starting:
  - **Use a folder** switches to "Play from the USB", whose parts fit in FAT32;
  - **Go on anyway** tries it. MaxPack never writes more to the `.p5z` than fits in FAT32 (4 GiB minus
    one byte): before going over, it stops, deletes what it wrote and tells you it does not fit.

  With an exFAT USB drive there is no limit.
- **Space.** If what the queue may take on a drive is more than is free there, MaxPack says so before
  it starts, with the numbers, and you can choose another destination or **Start anyway**. With
  **Replace**, the new one and the old one have to fit at the same time: if they do not, that game does
  not start.
- **Replace.** If the game is already at the destination, MaxPack asks before touching anything. If you
  accept, the new one is written apart, in `<destination>.maxpack-tmp` (as every new one is), verified,
  and only then is the old one set aside and deleted. If you cancel, or anything fails (for example, you pull out the USB
  drive), the old one stays intact and with no leftovers.
- **The source is never the destination.** If they are the same, or the destination is inside the
  source game, it asks for another.
- **Two games with the same ID** going to the same destination are flagged before starting.
- **No ID.** If a game's `param.json` does not give its ID (TITLE_ID), MaxPack warns you before
  starting with "MaxPack installer": MaxPack for the console does not accept a `.p5z` without one. A
  `param.json` with a BOM or with padding at the end is read fine.
- MaxPack never writes to the source game, has no keys and does not touch DRM. The only encryption it
  reads is that of a debug package (`.pkg`), with the passcode it was made with.
