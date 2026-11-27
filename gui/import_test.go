// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// A .zstd (an exFAT image with a game packed already in it, as ModGames44 names it) is taken where an image is:
// dropped, from Add files and from a folder; the engine reads what it is by its contents.
func TestZstdImageIsTaken(t *testing.T) {
	for _, section := range []string{"compress", "details", "rename"} {
		if !sectionTakes[section].file("Starfall Odyssey.zstd") || !sectionTakes[section].file("x.ZSTD") {
			t.Errorf("%s does not take a .zstd", section)
		}
		if !strings.Contains(sectionPatterns[section], "*.zstd") {
			t.Errorf("the dialog of %s: %s", section, sectionPatterns[section])
		}
	}
	for _, section := range []string{"decompress", "verify"} {
		if sectionTakes[section].file("x.zstd") {
			t.Errorf("%s takes a .zstd", section)
		}
	}
	if got := spaceOf("Starfall Odyssey.zstd", false, ""); got != "compress" {
		t.Errorf("a .zstd goes to %q", got)
	}
	dir := t.TempDir()
	image := filepath.Join(dir, "Starfall Odyssey.zstd")
	os.WriteFile(image, []byte("x"), 0o644)
	a := &App{}
	if found := a.Expand([]string{dir}, "compress"); len(found) != 1 || found[0] != image {
		t.Errorf("Expand: %v", found)
	}
}

// A queue of Compress keeps the packs of a game packed already when Settings says so, whatever the page sent.
func TestQueueTakesKeepPacks(t *testing.T) {
	useSettings(t, t.TempDir())
	packed := jobs.Item{Path: filepath.Join(t.TempDir(), "PPSA01234"), TitleID: "PPSA01234", Name: "Starfall Odyssey", Kind: "folder",
		OriginalSize: 100, Packed: true, Packs: "zstd", PackedLevel: 5, UnpackedSize: 300}
	for _, keep := range []bool{false, true} {
		s := settings.Defaults()
		s.KeepPacks = keep
		if err := settings.Save(s); err != nil {
			t.Fatal(err)
		}
		a := &App{}
		plans, err := a.Plan("compress", []jobs.Item{packed}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir(), KeepPacks: !keep}})
		if err != nil || len(plans) != 1 {
			t.Fatalf("%v %v", plans, err)
		}
		if got := slices.Contains(plans[0].Args, "--keep-packs"); got != keep {
			t.Errorf("Settings keep %v: %v", keep, plans[0].Args)
		}
	}
}

// The window needs an engine that packs a game packed already (and takes --keep-packs), and says its packs.
func TestEngineMustImport(t *testing.T) {
	if !slices.Contains(core.Required, "import") {
		t.Errorf("Required: %v", core.Required)
	}
}

// Settings keeps the choice for the games packed already, and a queue of Compress takes it from there.
func TestKeepPacksFromSettings(t *testing.T) {
	useSettings(t, t.TempDir())
	for _, keep := range []bool{false, true} {
		s := settings.Defaults()
		s.KeepPacks = keep
		if err := settings.Save(s); err != nil {
			t.Fatal(err)
		}
		a := &App{}
		text := a.reportText(ReportRequest{})
		want := map[bool]string{false: "Packed games: compressed again", true: "Packed games: their packs kept"}[keep]
		if !strings.Contains(text, want) {
			t.Errorf("keep %v: the report does not say %q:\n%s", keep, want, text)
		}
	}
}

// A report says what a game packed already is packed with and at what level, and what was made of it.
func TestReportSaysPacks(t *testing.T) {
	a := &App{}
	text := a.reportText(ReportRequest{Section: "compress", Mode: "p5z", Games: []ReportGame{
		{Kind: "folder", Packed: true, Packs: "zstd", PackedLevel: 5, OriginalSize: 20, State: "ok", Detail: "from_packed_kept:5"},
		{Kind: "exfat", Packed: true, Packs: "zstd", PackedLevel: 19, OriginalSize: 20, ImageSize: 22, State: "ok", Detail: "from_packed"},
		{Kind: "folder", Packs: "lz4", OriginalSize: 20, State: "error", Detail: "packs_lz4"},
		{Kind: "folder", OriginalSize: 20},
	}})
	for _, want := range []string{"Packed already: zstd packs, level 5", "Result: ok (from_packed_kept:5)", "Packed already: zstd packs, level 19",
		"Result: ok (from_packed)", "Packed already: LZ4 packs", "Result: error (packs_lz4)"} {
		if !strings.Contains(text, want) {
			t.Errorf("the report does not say %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "Packed already:") != 3 {
		t.Errorf("the report:\n%s", text)
	}
}

// With the real engine: Details of a folder packed at level 3 says its packs and their level.
func TestDetailsOfAPackedGame(t *testing.T) {
	engine, fixtures := needCore(t)
	out := filepath.Join(t.TempDir(), "packed")
	if result, code, err := core.Run(t.Context(), engine, []string{"pack", filepath.Join(fixtures, "basic.exfat"), "-o", out, "--folder", "--level", "3"}, nil); err != nil || code != 0 {
		t.Fatalf("%v %d %v", result.Raw, code, err)
	}
	a := &App{core: engine}
	d, err := a.Details(out)
	if err != nil || !d.PackedZstd() || d.PackedLevel != 3 || d.UnpackedSize == 0 || d.CannotCompress() {
		t.Fatalf("%+v, %v", d.Item, err)
	}
}
