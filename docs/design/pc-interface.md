# MaxPack: PC program with a window (design)

*23 November 2026. It is part of phase 4 of the [general design](maxpack.md) ("GUI"), brought forward
because without a window most people will not use the tool. It is based on
[NXPress](https://github.com/DeLFuS77/NXPress), the Switch compressor by the same author.*

## 1. Intent

Make compressing a PS5 game on the PC **pick the game, compress it and have it ready for the PS5**, in a
window like NXPress's. And the same for the way back: decompressing a `.p5z`.

Why (your words): with commands "it is a hassle and people will not accept it". MaxPack and NXPress have
to look like the same family: same window, same style, same way of working (decided by you: "A, like
NXPress").

## 2. Goals, non-goals and anti-goals

**Goals**
- W1. **One program, like NXPress.** With no arguments it opens the window; with arguments it is the
  usual command line (`maxpack pack ...`).
- W2. **Compress.**
  - One or several games (folder dumps or `.exfat` images) are added with buttons or by dragging them.
  - The destination and "Made for" are chosen:
    - **MaxPack installer** (`.p5z`, the default option);
    - **Play from the USB** (folder ready in `<destination>/homebrew/<TITLE_ID>`).
  - Press Compress. Each game is checked (`verify`) when it finishes.
- W3. **Decompress** a `.p5z` to the original dump (bit for bit) or to the folder for the PS5.
- W4. **Verify** `.p5z` files and compressed folders, and **Details** of a game (title, ID, version,
  sizes, packs, runtime).
- W5. Progress per game and in total, speed and time left, and **Cancel** without leaving anything half
  made. At the end, the result window: "X GB saved", what saved the most and what to do now.
- W6. Windows, macOS and Linux, in Spanish and English and with light and dark themes, like NXPress.
- W7. **The essential extras of NXPress** (decided by you, "the essentials"):
  - the PC does not go to sleep while a job is running;
  - progress on the taskbar icon (Windows) or the Dock (macOS);
  - a system notification when it finishes;
  - a single window: opening the program again or dragging games to its icon adds them to the open
    window;
  - it remembers the window size and position.

**Non-goals (for now)**
- Reading PKG, `.ffpkg`, `.ffpfs` and `.ffpfsc`. They come later, one at a time, starting with PKG
  (decided by you). The window will accept them when the engine reads them.
- Estimating or comparing levels (`analyze`), sending to the console over the network and images as
  output.
- Automatic update, Explorer integration, more languages and publishing releases. All of that is in
  NXPress and can be brought over later.
- The rest of NXPress's extras: shut down when finished, delete the original after compressing, recycle
  bin, exportable log, grid with sorting and selection, presets, threads and priority, and resuming the
  list after a close.

**Anti-goals**
- A half-made `.p5z` or folder that looks good, because of a cancel or an error.
- A game that does not start on the console because it lacks ampr-emu-ng and nobody warned about it.
- Overwriting something without asking.
- MaxPack looking or working differently from NXPress for no reason.

## 3. Constraints

- **The engine stays in C**, the one from phases 0 to 2: `pack`, `unpack`, `verify`, `info`. The window
  does not rewrite it.
- **NXPress** uses Go and Wails v2 (WebView2 on Windows, WebKitGTK on Linux, WKWebView on macOS), with a
  page in HTML, CSS and JS without frameworks (`frontend/`), drag and drop (`EnableFileDrop`), a single
  instance and settings next to the program or in the user's folder. Its `build.yml` builds and
  installs on Windows x64, Linux x64 and ARM64, and macOS with Apple and Intel chips.
- **Licences:** NXPress is AGPL-3.0 and MaxPack is GPL-3.0-or-later. **Decided by you:** what is taken
  from NXPress (of which you are the author) is published in MaxPack under MaxPack's licence,
  GPL-3.0-or-later.
- **Without ampr-emu-ng** in `fakelib/`, the game does not start with ShadowMountPlus, and the PS5
  installer does not carry one inside yet (P4 of phase 2).
- **FAT32** does not take files over 4 GiB: a big `.p5z` does not fit. The folder does, because its
  volumes are smaller.
- **Names:** on Windows `MaxPack.exe` and `maxpack.exe` are the same file name, so they cannot be in the
  same folder.

## 4. Approaches considered

- **A. Go + Wails, like NXPress, with the C engine underneath.** <- **Chosen by you.** The window, the
  style, the languages, the installers and the build come from NXPress.
- **B. Local page in the browser** (C). Faster, but it is a tab, files cannot be dragged and it does not
  look like NXPress.
- **C. Own window in C** (webview library). What Wails already gives done would have to be redone in C.

## 5. Design

### 5.1 Pieces

1. **`maxpack-core`**: the usual C program (the command line), with what the window needs to talk to it
   (5.2). In the downloadable version it goes next to the window under that name, so as not to clash
   with `MaxPack.exe`.
2. **`MaxPack`**: the window, in Go with Wails v2, in `gui/` inside this repository.
   - With no arguments, it opens the window.
   - With arguments, it runs `maxpack-core` with those arguments, handing it the console, and returns
     its exit code, as NXPress does with its own.
3. **The page** (`gui/frontend/`): based on NXPress's (structure, styles, themes and language system),
   with the Switch parts removed and the MaxPack parts added.
4. **Build:** the GitHub workflow builds `maxpack-core` with CMake and the window with Wails on Windows,
   macOS and Linux. It packages like NXPress: installer and portable version on Windows, dmg on macOS,
   and an archive with an installer on Linux.

### 5.2 How the window talks to the engine

The window runs `maxpack-core` as a separate process, one job at a time:
- **`--progress json`** (in `pack`, `unpack` and `verify`): one JSON line per event on stdout, for
  example `{"event":"progress","phase":"packing","done":N,"total":M}`, and at the end
  `{"event":"result",...}` with sizes, savings and failures. The text for people stays on stderr.
- **`maxpack-core info --json <path>`**: what it is (dump, `.exfat` or `.p5z`), title, ID, version,
  original and installed size, number of files, whether it carries a runtime that reads zstd, and
  whether it is a damaged `.p5z`.
- **`maxpack-core icon <path> -o <png>`**: extracts `sce_sys/icon0.png` from a dump, an `.exfat` or a
  `.p5z`, for the covers in the list.
- **Cancel:** with `--control stdin`, the engine reads its input. If it receives `cancel` or the input
  closes, it stops at the next block, deletes what it was writing and exits with code 130:
  - the half-made `.p5z` (`mp_pack_out_p5z` already does it);
  - the output folder, which did not exist or was empty before starting;
  - what was written of an original dump.

  For that the engine gains a progress and cancellation hook in `pack`, `unpack` and `verify`, without
  changing what the command-line user sees.

### 5.3 The window (like NXPress)

```
+ MaxPack 0.x ------------------------------------------------------------------------------+
| Space          |  Compress                                      | Made for                 |
|  Compress      |  PS5 dumps to .p5z or to a ready folder.       | (o) MaxPack installer    |
|  Decompress    |  [Add files] [Add folder] Clear list           |     .p5z: the smallest   |
| Check          |  Game                        Size    State     | ( ) Play from the USB    |
|  Verify        |  [ic] Hollow Knight Silksong 8.39 GB * OK -72% |     folder in homebrew/  |
|  Details       |       PPSA12544, version 01.000.010            | Compression level    19  |
|                |       8.39 GB -> 2.33 GB                       | -----------------o--     |
|                |  [ic] RESIDENT EVIL requiem  133 GB  * 37 %    | Destination  E:\ [Choose]|
| Settings       |  ...                                           | ampr-emu-ng: ok (path)   |
|                |  1 of 2 done, 37 %  ###...  410 MB/s  18 min left          [Cancel]      |
+--------------------------------------------------------------------------------------------+
```

- **Sidebar:** Space (Compress, Decompress), Check (Verify, Details) and Settings.
- **Game list**, with cover (`icon`), name, ID and version, size, state, a bar per game and the saving.
  Games are added with "Add files", with "Add folder" (which looks for dumps and `.exfat` one level
  down) or by dragging them onto the window.
- **Right panel, in Compress:**
  - "Made for", with a short text for each option;
  - compression level (1-22, 19 by default, with shortcuts Light, Medium and Maximum as in NXPress);
  - destination;
  - state of ampr-emu-ng.
- **Right panel, in Decompress:** "Original dump (bit for bit)" or "Folder for the PS5", and the
  destination.
- **Bottom bar:** total, percentage, speed, time left and Cancel.
- **When it finishes,** the result window:
  - "X GB saved" and the games that saved the most;
  - what to do now: "Plug the USB into the PS5 and open it with the MaxPack installer", or
    "ShadowMountPlus will see it in homebrew/".
- **Light and dark themes, and ES and EN languages,** with NXPress's system.

### 5.4 Rules when compressing

- **Name of the `.p5z`:** `<Title> [<TITLE_ID>].p5z`, without characters that Windows does not allow.
  With no title, the name of the folder or the image.
- **Destination:**
  - if it is the root of a drive and the mode is folder, `homebrew/<TITLE_ID>` is used;
  - otherwise, the `.p5z` or the folder go inside the chosen folder.
- **Destination taken:** if the `.p5z` already exists or the folder is not empty, it asks **Replace** or
  **Skip**, saying what is deleted and how much it takes. Replace follows the phase 2 rule: the new one
  is written apart (in `<destination>.maxpack-tmp`), verified, and only then is the old one set aside
  and deleted. That is why both have to fit; if they do not, it says so and does not start. On cancel
  or on failure, the old one stays intact.
- **The source is never the destination:** if the game being compressed is in the place where the result
  would go (for example, compressing `E:\homebrew\PPSA12544` in folder mode to `E:\`), Replace is not
  offered: another destination is asked for.
- **ampr-emu-ng:**
  - if a dump does not carry a `libSceAmpr.sprx` that reads zstd (`info --json` says so), the one in
    Settings is used with `--runtime`;
  - if Settings has none, the path is asked for once, with a short explanation;
  - if it is not given, it clearly warns that the game will not start until it has one, and you can go
    on.
- **Space and FAT32:**
  - if the destination has less space than the original game, it warns: "may not fit";
  - FAT32: the size of the `.p5z` is not known until the end, but it never goes over the original. If
    the destination is FAT32 and the original is over 4 GiB, before starting it warns and offers
    **Use a folder** (recommended) or **Go on anyway**. If, on going on, the `.p5z` reaches 4 GiB, the
    engine stops, deletes what it wrote and says so. If the original is not over 4 GiB, there is no
    need to warn.
- **Repeated games:** if two games in the list have the same ID and the same destination, it warns before
  starting.

### 5.5 Tests

- **Engine (CTest):**
  - `--progress json` in `pack`, `unpack` and `verify`: the lines are valid JSON and the progress ends
    at the total;
  - `info --json` of a dump, an `.exfat`, a `.p5z` and a damaged one;
  - `icon` of the three;
  - cancelling through stdin halfway through each job: nothing is left half made and the code is 130;
  - the output for people does not change (the command-line tests stay green).
- **Window (Go, like NXPress's tests):**
  - the job launcher with the real `maxpack-core` and the test dumps;
  - names of `.p5z` files and destinations;
  - destination taken;
  - FAT32 warning (with a hook that simulates the file system);
  - cancelling;
  - with arguments, it behaves like the command line.
- **Page:** the same checks that NXPress does on its own (ES and EN texts, no `innerHTML` with data).
- **By hand:**
  - compress and decompress the test dumps and Silksong, cancel, themes and languages, and drag files;
  - the Windows installer from the GitHub artifact.

## 6. Decisions I made (you can change them)

- The window lives in `gui/` of this repository, not in a separate one, so that engine and window are
  published together.
- The engine is talked to by running it as a process (`--progress json` and `--control stdin`), not by
  linking it with cgo. That way it builds just as easily on all five systems and the command line stays
  as it is.
- **Names:** the engine is called `maxpack-core` next to the `MaxPack` window; the usual `maxpack` keeps
  existing only for whoever uses just the command line.
- At the start, the window is only in Spanish and English.
- No Pause: Cancel stops the running game and leaves the others waiting.
- Replace writes apart and deletes the old one at the end, with the new one verified, even if that
  needs space for both.
- The source can never be the destination.
- FAT32: a warning based on the size of the original and a clean stop if the `.p5z` reaches 4 GiB,
  instead of estimating the size beforehand.
- The W7 extras are redone around `maxpack-core` following how NXPress does them.
- Level 19 by default, with NXPress's shortcuts.

## 7. What is left to the builder's judgement

How NXPress's page is split (what is copied and what is redone), internal names, the exact format of
the JSON lines, the progress interval, icons and texts, how settings are stored (like NXPress) and how
the build workflow is ordered.
