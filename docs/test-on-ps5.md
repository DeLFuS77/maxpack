# Try a packed game on the PS5

This guide is for checking on your console a game packed with `maxpack pack`. You need:
- a jailbroken PS5 with the usual stack: ELF loader, kstuff and ShadowMountPlus;
- a dump of the game that you own (folder or `.exfat`).

> Always work on a copy: keep the `.p5z` (or the original dump) before deleting anything.

## 1. The runtime

The zstd packs are served by **ampr-emu-ng**. Use the one MaxPack carries, not the one from quer3q's
releases:
- Its `libSceAmpr.sprx` is in `vendor/ampr-emu-ng/libSceAmpr.sprx` of the console repository,
  [DeLFuS77/maxpack-ps5](https://github.com/DeLFuS77/maxpack-ps5).
- It is built in [DeLFuS77/ampr-emu-ng](https://github.com/DeLFuS77/ampr-emu-ng) (the `sprx` artifact of
  its GitHub Actions). `vendor/ampr-emu-ng/SOURCE` says which commit it comes from.
- The `libSceAmpr.sprx` that some dumps carry is the old ampr_emu and does not read these packs.
  `maxpack pack` warns about it: "the runtime ... does not seem to read zstd packs".

## 2. Pack

```
maxpack pack "D:\dumps\MyGame.exfat" -o "D:\forPS5\PPSA00000" --runtime "D:\ampr-emu-ng\libSceAmpr.sprx"
maxpack verify "D:\forPS5\PPSA00000" --against "D:\dumps\MyGame.exfat"
```

To keep the game on the PC taking little space, instead of the folder:

```
maxpack pack "D:\dumps\MyGame.exfat" -o "D:\games\MyGame.p5z" --p5z --runtime "D:\ampr-emu-ng\libSceAmpr.sprx"
maxpack unpack "D:\games\MyGame.p5z" --folder -o "D:\forPS5\PPSA00000"
```

## 3. Take it to the console

1. Copy the whole folder to a path that ShadowMountPlus scans. By default those are
   `/data/homebrew/<TITLE_ID>` on the internal storage, or `homebrew/` on a USB drive
   (`/mnt/usb0..7`) or on `/mnt/ext0..1`.
   - If you copy it over FTP with FileZilla, set it to **binary** mode (Transfer > Transfer type >
     Binary). Otherwise files with no extension, such as `sce_sys/keystone`, fail with
     "504 STOR in ASCII mode is not supported".
2. In ShadowMountPlus, add or rescan the game. Leave its per-game mode as it comes,
   "Default (Full)": you do not need to turn off the fakelib update.
   - ShadowMountPlus uses the game's `fakelib/libSceAmpr.sprx` (ampr-emu-ng) and only adds its
     backport, and says so with the notice "Backport applied: <TITLE_ID>".
   - Checked on 24 Nov 2026 with ShadowMountPlus 1.7beta12 on a PS5 with firmware 13.60.

## 4. What to check (design 6.8)

- On start the notice "ampr-emu-ng: v... (zstd) running" appears: the game uses ampr-emu-ng.
- It starts up to the menu.
- Videos play.
- A save game loads.
- Saving works.
- 10 minutes of play with no odd stutter or freeze.

If something fails, write down which game it was, what it was doing at the time and, if you have the
debug build of ampr-emu-ng, its `/fsfail`. A file that the game opens with `mmap` cannot be packed:
leave it loose by packing again with `--keep-loose "<path or pattern>"`.
