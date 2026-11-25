// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// copyImage copies an image of the fixtures to a folder of the test, under another name.
func copyImage(t *testing.T, from, to string) string {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return to
}

// A game kept as a ShadowMountPlus image moves to a .p5z in one step, as the user does it in the
// window: the folder that has the image is given to Compress, the engine says what it is (a PFS
// container MkPFS made, PFSC-compressed, with an exFAT image inside, from tests/data), the plan
// counts the game's size and not the image's, and the queue packs it to the .p5z the engine makes
// of it, verifies it and deletes the image (the original) for good, as Settings says; what it says it
// saved is against the image's file, what the disk got back. That .p5z decompresses to the game's
// folder.
func TestCompressAnImageThroughTheQueue(t *testing.T) {
	engine, fixtures := needCore(t)
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.DeletePermanently = true })
	dumps := t.TempDir()
	image := copyImage(t, filepath.Join(fixtures, "mkpfs_exfat.ffpfsc"), filepath.Join(dumps, "Tool Test.ffpfsc"))
	touch(t, dumps, "notes.txt", 3)
	stat, err := os.Stat(image)
	if err != nil {
		t.Fatal(err)
	}
	imageSize := uint64(stat.Size())

	events := newPageEvents()
	a := pageApp(engine, events)
	found := a.Expand([]string{dumps}, "compress")
	if len(found) != 1 || found[0] != image {
		t.Fatalf("Expand: %v", found)
	}
	game, err := a.Describe(image)
	if err != nil {
		t.Fatal(err)
	}
	if game.Kind != "pfsc" || game.Inner != "exfat" || !game.Compressed || game.TitleID != "BREW00000" || game.OriginalSize <= imageSize || game.ImageSize != imageSize {
		t.Fatalf("%+v (the image takes %d)", game.Item, imageSize)
	}
	dest := t.TempDir()
	choices := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest, Level: 3}, DeleteOriginals: true}
	views, err := a.Plan("compress", []jobs.Item{game.Item}, choices)
	if err != nil || len(views) != 1 || views[0].Block != "" {
		t.Fatalf("%+v %v", views, err)
	}
	// the queue counts the game; deleting the original frees what the image takes
	if views[0].Need != game.OriginalSize || views[0].SourceSize != imageSize {
		t.Fatalf("need %d, source %d: %+v", views[0].Need, views[0].SourceSize, views[0])
	}
	// what the engine makes of the image with the same arguments, to compare
	want := filepath.Join(t.TempDir(), "ref.p5z")
	args := append([]string{}, views[0].Args...)
	for i := range args {
		if args[i] == views[0].Tmp {
			args[i] = want
		}
	}
	if result, code, err := core.Run(context.Background(), engine, args, nil); err != nil || code != 0 || result.Raw["ok"] != true {
		t.Fatalf("%v: %d %+v %v", args, code, result.Raw, err)
	}

	if err := a.Start("compress", []jobs.Item{game.Item}, choices); err != nil {
		t.Fatal(err)
	}
	sum := events.wait(t)
	if got := strings.Join(events.states(0), " "); got != "waiting packing verifying ok" {
		t.Fatalf("states: %s, %+v", got, events.last(0))
	}
	out := filepath.Join(dest, jobs.P5zName(game.Item))
	made, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if reference, _ := os.ReadFile(want); len(made) == 0 || !bytes.Equal(made, reference) {
		t.Errorf("the .p5z is not the engine's: %d and %d bytes", len(made), len(reference))
	}
	if last := events.last(0); last.Out != out || last.Size != uint64(len(made)) || !strings.Contains(last.Detail, "original_deleted") {
		t.Errorf("%+v", last)
	}
	if sum.Done != 1 || sum.Deleted != 1 || sum.DeletedSize != imageSize || sum.Saved != imageSize-uint64(len(made)) ||
		len(sum.Top) != 1 || sum.Top[0].Saved != sum.Saved {
		t.Errorf("%+v (game %d, made %d)", sum, game.OriginalSize, len(made))
	}
	if _, err := os.Stat(image); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the image is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dumps, "notes.txt")); err != nil {
		t.Errorf("what was next to it went: %v", err)
	}

	// the .p5z gives the game's folder back
	packed, err := a.Details(out)
	if err != nil || packed.Kind != "p5z" || !packed.Packed || packed.OriginalSize != game.OriginalSize {
		t.Fatalf("%+v %v", packed, err)
	}
	unpacked := t.TempDir()
	events = newPageEvents()
	a = pageApp(engine, events)
	if err := a.Start("decompress", []jobs.Item{packed.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeOriginal, Dest: unpacked}}); err != nil {
		t.Fatal(err)
	}
	if sum := events.wait(t); sum.Done != 1 {
		t.Fatalf("%+v %v", sum, events.states(0))
	}
	last := events.last(0)
	if info, err := os.Stat(last.Out); err != nil || !info.IsDir() || last.Size != game.OriginalSize {
		t.Fatalf("%+v: %v", last, err)
	}
	folder, err := a.Details(last.Out)
	if err != nil || folder.Kind != "folder" || folder.TitleID != "BREW00000" || folder.OriginalSize != game.OriginalSize || folder.Files != packed.Files {
		t.Fatalf("the folder: %+v %v", folder, err)
	}
}

// Every kind of image goes through the queue to a .p5z and to a folder for the console (a renamed
// one too, by what it is), and stays where it was when the originals are kept.
func TestCompressEveryImage(t *testing.T) {
	engine, fixtures := needCore(t)
	useSettings(t, t.TempDir())
	dumps := t.TempDir()
	var items []jobs.Item
	for _, c := range []struct{ from, as, kind string }{
		{"basic.ffpkg", "a.ffpkg", "ufs2"},
		{"mkpfs_raw.ffpfs", "b.ffpfs", "pfs"},
		{"mkpfs_ufs2.ffpfsc", "c.ffpfsc", "pfsc"},
		{"mkpfs_pfs.ffpfsc", "d.ffpfsc", "pfsc"},
		{"basic.ffpfs", "renamed.exfat", "pfs"},
	} {
		path := copyImage(t, filepath.Join(fixtures, c.from), filepath.Join(dumps, c.as))
		game, err := (&App{core: engine}).Describe(path)
		if err != nil || game.Kind != c.kind {
			t.Fatalf("%s: %+v %v", c.as, game.Item, err)
		}
		// each a game of its own, so they do not meet at their destination
		game.TitleID = "PPSA0000" + string(rune('1'+len(items)))
		items = append(items, game.Item)
	}
	for _, mode := range []jobs.Mode{jobs.ModeP5z, jobs.ModeFolder} {
		dest := t.TempDir()
		events := newPageEvents()
		a := pageApp(engine, events)
		if err := a.Start("compress", items, Choices{Options: jobs.Options{Mode: mode, Dest: dest, Level: 1}}); err != nil {
			t.Fatal(err)
		}
		if sum := events.wait(t); sum.Done != len(items) || sum.Failed != 0 {
			t.Fatalf("%s: %+v", mode, sum)
		}
		for i, it := range items {
			last := events.last(i)
			if last.State != jobs.StateOK || last.Out == "" || last.Size == 0 {
				t.Errorf("%s %s: %+v", mode, filepath.Base(it.Path), last)
			}
			if _, err := os.Stat(it.Path); err != nil {
				t.Errorf("%s: the image was taken away without being asked: %v", filepath.Base(it.Path), err)
			}
		}
	}
}
