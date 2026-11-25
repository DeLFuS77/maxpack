// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The images ShadowMountPlus mounts and PS5 packages are taken by their extension, in any case:
// .exfat, .ffpkg, .ffpfs, .ffpfsc and .pkg; nothing else is one.
func TestImageNames(t *testing.T) {
	for name, want := range map[string]bool{
		"game.exfat": true, "GAME.EXFAT": true, "game.ffpkg": true, "game.FFPkg": true, "game.ffpfs": true,
		"game.ffpfsc": true, "game.FFPFSC": true, "D:/dumps/Iron Harbor.ffpfsc": true,
		"game.pkg": true, "Game.PKG": true,
		"game.p5z": false, "game.ffpfscx": false, "game.ffp": false, "ffpkg": false, "game": false, "game.pkgx": false, "pkg": false,
	} {
		if got := IsImageName(name); got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
	for kind, want := range map[string]bool{"exfat": true, "ufs2": true, "pfs": true, "pfsc": true, "fpkg": true, "folder": false, "p5z": false, "": false} {
		if got := IsImage(kind); got != want {
			t.Errorf("kind %q: %v", kind, got)
		}
	}
	if strings.Join(ImageExts, " ") != ".exfat .ffpkg .ffpfs .ffpfsc .pkg" {
		t.Errorf("extensions: %v", ImageExts)
	}
	// the name of the image without its extension is the title of a game that has none
	for _, name := range []string{"Lost Signal.exfat", "Lost Signal.ffpkg", "Lost Signal.FFPFS", "Lost Signal.ffpfsc", "Lost Signal.pkg", "Lost Signal.p5z", "Lost Signal"} {
		it := game("", "PPSA10005", "", "")
		it.Path = filepath.Join("x", name)
		if got := NameOf(it, DefaultTemplate); got != "Lost Signal [PPSA10005]" {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// What the engine says of an image: its kind, what a container holds and whether reading it
// decompresses; and what its file takes on disk, which the window reads itself. A folder or a .p5z
// has no image size.
func TestFromInfoOfAnImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harbor.ffpfsc")
	if err := os.WriteFile(path, make([]byte, 3000), 0o644); err != nil {
		t.Fatal(err)
	}
	it := FromInfo(path, map[string]any{"kind": "pfsc", "inner": "exfat", "compressed": true, "title_id": "PPSA10002", "original_size": float64(5 << 30)})
	if it.Kind != "pfsc" || it.Inner != "exfat" || !it.Compressed || it.OriginalSize != 5<<30 || it.Name != "harbor" || it.ImageSize != 3000 {
		t.Errorf("%+v", it)
	}
	// an engine of before says neither; an image that is not there has no size
	if it := FromInfo(filepath.Join("x", "harbor.exfat"), map[string]any{"kind": "exfat"}); it.Inner != "" || it.Compressed || it.ImageSize != 0 {
		t.Errorf("%+v", it)
	}
	for _, kind := range []string{"folder", "p5z"} {
		if it := FromInfo(path, map[string]any{"kind": kind, "original_size": float64(7)}); it.ImageSize != 0 {
			t.Errorf("%s: %+v", kind, it)
		}
	}
}

// What a game took before it was compressed, as the queue counts what it saved: an image's file on
// disk, else the game itself; an image whose file is not known takes nothing (no saving is claimed).
func TestSizeBefore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ffpkg")
	os.WriteFile(path, make([]byte, 4000), 0o644)
	for _, c := range []struct {
		it   Item
		want uint64
	}{
		{Item{Kind: "folder", Path: dir, OriginalSize: 9000}, 9000},
		{Item{Kind: "pfsc", Path: path, OriginalSize: 9000, ImageSize: 5000}, 4000},
		{Item{Kind: "ufs2", Path: filepath.Join(dir, "gone.ffpkg"), OriginalSize: 9000, ImageSize: 5000}, 5000},
		{Item{Kind: "pfs", Path: filepath.Join(dir, "gone.ffpfs"), OriginalSize: 9000}, 0},
	} {
		if got := c.it.SizeBefore(); got != c.want {
			t.Errorf("%+v: %d, want %d", c.it, got, c.want)
		}
	}
}

// Rename keeps the extension of an image, in its case; when the engine says it is another kind
// than its extension says (a renamed image), it takes the extension of what it is.
func TestRenameImagesByKind(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	at := func(name, kind string, id int) Item {
		return Item{Path: mk(name), Kind: kind, Name: "Iron Harbor", TitleID: "PPSA1000" + string(rune('0'+id)), Version: "01.000.000"}
	}
	items := []Item{
		at("a.exfat", "exfat", 1),
		at("b.ffpkg", "ufs2", 2),
		at("c.ffpfs", "pfs", 3),
		at("d.ffpfsc", "pfsc", 4),
		at("e.FFPFSC", "pfsc", 5),
		// a container named as a PFS image, a UFS2 image named as exFAT, an exFAT image named as a .p5z
		at("f.ffpfs", "pfsc", 6),
		at("g.exfat", "ufs2", 7),
		at("h.p5z", "exfat", 8),
		// a kind this window does not know keeps the extension it has
		at("i.ffpkg", "future", 9),
	}
	plans := PlanRename(items, DefaultTemplate)
	want := []string{
		"Iron Harbor [PPSA10001].exfat", "Iron Harbor [PPSA10002].ffpkg", "Iron Harbor [PPSA10003].ffpfs",
		"Iron Harbor [PPSA10004].ffpfsc", "Iron Harbor [PPSA10005].FFPFSC",
		"Iron Harbor [PPSA10006].ffpfsc", "Iron Harbor [PPSA10007].ffpkg", "Iron Harbor [PPSA10008].exfat",
		"Iron Harbor [PPSA10009].ffpkg",
	}
	for i, w := range want {
		if got := filepath.Base(plans[i].To); got != w || plans[i].Block != "" {
			t.Errorf("%s: %q %q, expected %q", filepath.Base(items[i].Path), got, plans[i].Block, w)
		}
	}
	for i, r := range Rename(plans) {
		if r.State != StateOK {
			t.Errorf("%d: %+v", i, r)
		}
		if _, err := os.Stat(filepath.Join(dir, want[i])); err != nil {
			t.Errorf("%s: %v", want[i], err)
		}
	}
}

// The room a game takes is its own size as the engine says it, not what its image takes: a
// PFSC-compressed container is smaller than the game in it, and the folder or the .p5z made of it
// may take more than the image. What the queue saved, though, is against the image's file: what
// the disk gets back, never more. Checked with a container that MkPFS made (tests/data).
func TestSpaceIsTheGameNotTheImage(t *testing.T) {
	engine, fixtures := needCore(t)
	path := filepath.Join(fixtures, "mkpfs_exfat.ffpfsc")
	it := describe(t, engine, path)
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	file := uint64(stat.Size())
	if it.Kind != "pfsc" || it.Inner != "exfat" || !it.Compressed || it.TitleID == "" || it.OriginalSize <= file || it.ImageSize != file {
		t.Fatalf("the container: %+v (%d bytes)", it, file)
	}
	// room for the image, not for the game
	fs := fakeFS{free: file + (it.OriginalSize-file)/2, typ: "NTFS"}
	for _, mode := range []Mode{ModeFolder, ModeP5z} {
		o := Options{Mode: mode, Dest: t.TempDir()}
		plans := PlanCompress([]Item{it}, o, fs)
		if plans[0].Need != it.OriginalSize || !hasFlag(plans[0].Warnings, "may_not_fit") || plans[0].Block != "" {
			t.Errorf("%s: %+v", mode, plans[0])
		}
		short := Space(plans, o, fs)
		if len(short) != 1 || short[0].Need != it.OriginalSize || short[0].Free != fs.free {
			t.Errorf("%s: %+v", mode, short)
		}
		// with room for the game it fits
		roomy := fakeFS{free: it.OriginalSize, typ: "NTFS"}
		if plans := PlanCompress([]Item{it}, o, roomy); hasFlag(plans[0].Warnings, "may_not_fit") || len(Space(plans, o, roomy)) != 0 {
			t.Errorf("%s with room: %+v", mode, plans[0])
		}
	}
	// and what the queue says it saved is the image's file against what it made, as a .p5z and as
	// a folder, never the game against it
	for _, mode := range []Mode{ModeP5z, ModeFolder} {
		o := Options{Mode: mode, Dest: t.TempDir(), Level: 3}
		plans := PlanCompress([]Item{it}, o, newFS())
		sum := (&Runner{Core: engine}).Run(context.Background(), plans, o)
		made := SizeOf(plans[0].Out)
		if sum.Done != 1 || made == 0 || made >= file || sum.Saved != file-made || len(sum.Top) != 1 || sum.Top[0].Saved != file-made {
			t.Fatalf("%s: %+v, made %d of an image of %d (game %d)", mode, sum, made, file, it.OriginalSize)
		}
	}
}

// An .exfat image is an image too: what was saved is against its file (which takes a little more
// than the game in it); a dump folder's is against the game, which is what it takes.
func TestSavedByKindOfSource(t *testing.T) {
	engine, fixtures := needCore(t)
	image := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	stat, _ := os.Stat(image.Path)
	if image.ImageSize != uint64(stat.Size()) || image.ImageSize == image.OriginalSize {
		t.Fatalf("%+v (file %d)", image, stat.Size())
	}
	dump := filepath.Join(t.TempDir(), "Dump")
	os.MkdirAll(filepath.Join(dump, "sce_sys"), 0o755)
	os.WriteFile(filepath.Join(dump, "sce_sys", "param.json"), []byte(`{"titleId":"PPSA00000","titleName":"Dump","contentVersion":"01.000.000"}`), 0o644)
	os.WriteFile(filepath.Join(dump, "data.bin"), bytes.Repeat([]byte("MaxPack "), 1<<17), 0o644)
	folder := describe(t, engine, dump)
	if folder.Kind != "folder" || folder.ImageSize != 0 || folder.SizeBefore() != folder.OriginalSize {
		t.Fatalf("%+v", folder)
	}
	for _, it := range []Item{image, folder} {
		o := Options{Mode: ModeP5z, Dest: t.TempDir(), Level: 3}
		plans := PlanCompress([]Item{it}, o, newFS())
		sum := (&Runner{Core: engine}).Run(context.Background(), plans, o)
		made := SizeOf(plans[0].Out)
		if want := it.SizeBefore() - made; sum.Done != 1 || sum.Saved != want {
			t.Errorf("%s: %+v, want saved %d", it.Kind, sum, want)
		}
	}
}
