# Images made by the real tools

Small ShadowMountPlus images of one made-up game (`BREW00000`, 38 files: deep folders, an empty
file, text, zeros, noise, more than 768 KiB in one file), made with the tool that makes them for
people, so the readers are tested against more than MaxPack's own generators. They are gzipped;
`tests/tools/make_images.py` decompresses them into the fixtures folder, and `test_image_pack`
checks that each lists and reads exactly the game's files and packs to the same `.p5z` as the game
as a folder.

| file | what it is |
|---|---|
| `mkpfs_plain.ffpfs.gz` | `.ffpfs`: PFS image with the game at its root, nothing compressed |
| `mkpfs_raw.ffpfs.gz` | `.ffpfs`: the same with MkPFS's per-file PFSC compression (`--raw`) |
| `mkpfs_exfat.ffpfsc.gz` | `.ffpfsc`: PFS container holding an exFAT image, PFSC-compressed (ShadowMountPlus's documented command) |
| `mkpfs_ufs2.ffpfsc.gz` | `.ffpfsc`: the same holding a UFS2 image (`.ffpkg`) |
| `mkpfs_pfs.ffpfsc.gz` | `.ffpfsc`: the same holding a PFS image (MkPFS's two passes) |

## Tool and commands

MkPFS 1.1.0 (https://github.com/PSBrew/MkPFS, GPL-3.0), installed with `pip install mkpfs` into a
virtual environment (Python 3.15.0 on Windows 11, zlib-ng 1.1.0 as its compression backend). The
game folder and the UFS2 image come from MaxPack's generator:

```
python tests/tools/make_images.py --out work --tool-game     # work/game and work/BREW00000.ffpkg
cd work
mkpfs pack folder --raw --no-compress --no-adjust-output-file-extension game mkpfs_plain.ffpfs
mkpfs pack folder --raw --no-adjust-output-file-extension game mkpfs_raw.ffpfs
mkpfs pack exfat game BREW00000.exfat
mkpfs pack file --verify --version PS5 --inode-bits 32 --no-adjust-output-file-extension BREW00000.exfat mkpfs_exfat.ffpfsc
mkpfs pack file --verify --version PS5 --inode-bits 32 --no-adjust-output-file-extension BREW00000.ffpkg mkpfs_ufs2.ffpfsc
mkpfs pack folder --raw --no-compress --no-adjust-output-file-extension game BREW00000.dat
mkpfs pack file --verify --version PS5 --inode-bits 32 --no-adjust-output-file-extension BREW00000.dat mkpfs_pfs.ffpfsc
gzip -9 -n *.ffpfs *.ffpfsc
```

`mkpfs pack folder` without `--raw` (MkPFS's default) gives the same image as `pack exfat` plus
`pack file` but for the build time in its inodes, so it is not kept twice.

The UFS2 image inside `mkpfs_ufs2.ffpfsc` is `make_ufs2.py`'s (the documented profile: 64 KiB
blocks and fragments): UFS2Tool and FreeBSD's `newfs`/`makefs` could not be run where these were
made, so no `.ffpkg` made by a real tool is here yet.

MkPFS writes the time it built the image as every PFS inode's date, and 2024-01-01 00:00:00 as
every date of its exFAT images; those are the dates a game packed from these images gets.

## A PS5 debug package made by another builder

`lpp120-mixed.pkg.gz` is `ProsperoPkgTool.CompatibilityTests/Fixtures/lpp120-mixed.pkg` from
pearlxcore/ProsperoPkgTool (https://github.com/pearlxcore/ProsperoPkgTool, MIT; SHA-256 of the
package `c7c92758f23ceff97121282067f0f78d018fd55aae16ed64ac0b5c20660b58db`), gzipped with
`gzip -9 -n`. It is a generated test package, not a game: its README says it was built by
LibProsperoPkg 1.2.0 with its default compression and the all-zero passcode, from a minimal
`param.json` (content ID `UP0000-PPSA12345_00-TESTPACKAGE00000`) and an 8 MiB `payload.bin` whose
SHA-256 is `084B53568666B6B3BA37C3721A94E899909660B5EA03B8D4CE0A4752F9401A65`. `test_cli` checks
that MaxPack reads it as an FPKG and that the `payload.bin` it unpacks has that SHA-256, so the
package reader is tested against a builder that is not MaxPack's own generator.
