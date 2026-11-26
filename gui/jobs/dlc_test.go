// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
)

// What the engine says of a DLC and of the DLC a game carries: a DLC is one, with or without files of
// its own; a .p5z lists the ones in it; an engine of before says nothing of them.
func TestFromInfoDLC(t *testing.T) {
	it := FromInfo("d.pkg", map[string]any{"kind": "fpkg", "title_id": "PPSA00000", "content": "dlc", "dlc_data": true, "dlc": []any{}})
	if !it.IsDLC() || !it.DLCData || len(it.DLC) != 0 {
		t.Errorf("a DLC: %+v", it)
	}
	it = FromInfo("g.p5z", map[string]any{"kind": "p5z", "content": "game", "dlc": []any{
		map[string]any{"content_id": "UP0000-PPSA00000_00-MAXPACKDLCDATA01", "data": true, "folder": "addcont0", "size": float64(312196), "files": float64(6)},
		map[string]any{"content_id": "UP0000-PPSA00000_00-MAXPACKLICENSE01", "data": false, "folder": "", "size": float64(0), "files": float64(0)},
	}})
	want := []DLC{{ContentID: "UP0000-PPSA00000_00-MAXPACKDLCDATA01", Data: true, Folder: "addcont0", Size: 312196},
		{ContentID: "UP0000-PPSA00000_00-MAXPACKLICENSE01"}}
	if it.IsDLC() || !slices.Equal(it.DLC, want) {
		t.Errorf("a .p5z with DLC: %+v", it.DLC)
	}
	if it := FromInfo("x", map[string]any{"kind": "folder"}); it.IsDLC() || it.DLC != nil {
		t.Errorf("an engine of before: %+v", it)
	}
}

// A game compressed with its DLC gives the engine each one with --dlc, in the order the page gives
// them, and the room it needs is the game's and theirs; without any, nothing changes. Decompressing
// and verifying take none.
func TestCompressWithDLC(t *testing.T) {
	it := item("Starfall Odyssey", "PPSA10001", 10*gib)
	it.AddDLC = []string{`D:\dlc\b.pkg`, `D:\dlc\a`}
	it.AddDLCSize = 2 * gib
	for _, mode := range []Mode{ModeP5z, ModeFolder} {
		plans := PlanCompress([]Item{it}, Options{Mode: mode, Dest: t.TempDir()}, newFS())
		args := strings.Join(plans[0].Args, " ")
		if !strings.Contains(args, `--dlc D:\dlc\b.pkg --dlc D:\dlc\a`) {
			t.Errorf("%s: %v", mode, plans[0].Args)
		}
		if plans[0].Need != 12*gib {
			t.Errorf("%s: needs %d", mode, plans[0].Need)
		}
	}
	plain := item("Starfall Odyssey", "PPSA10001", 10*gib)
	if plans := PlanCompress([]Item{plain}, Options{Mode: ModeP5z, Dest: t.TempDir()}, newFS()); slices.Contains(plans[0].Args, "--dlc") {
		t.Errorf("without DLC: %v", plans[0].Args)
	}
	p5z := Item{Path: filepath.Join(t.TempDir(), "x.p5z"), Kind: "p5z", TitleID: "PPSA10001", Name: "X", AddDLC: []string{"d.pkg"}}
	if plans := PlanDecompress([]Item{p5z}, Options{Mode: ModeOriginal, Dest: t.TempDir()}, newFS()); slices.Contains(plans[0].Args, "--dlc") {
		t.Errorf("decompress: %v", plans[0].Args)
	}
	if plans := PlanVerify([]Item{p5z}); slices.Contains(plans[0].Args, "--dlc") {
		t.Errorf("verify: %v", plans[0].Args)
	}
}

// With the real engine: a game compressed with a DLC package and a license folder makes a .p5z that
// says them, and is verified.
func TestCompressWithDLCReal(t *testing.T) {
	engine, fixtures := needCore(t)
	dest := t.TempDir()
	game, err := Describe(engine, filepath.Join(fixtures, "mini.pkg"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Describe(engine, filepath.Join(fixtures, "dlc_data.pkg"))
	if err != nil || !data.IsDLC() || !data.DLCData || data.TitleID != game.TitleID {
		t.Fatalf("the DLC: %+v, %v", data, err)
	}
	license, err := Describe(engine, filepath.Join(fixtures, "dlc_license"))
	if err != nil || !license.IsDLC() || license.DLCData {
		t.Fatalf("the license: %+v, %v", license, err)
	}
	game.AddDLC = []string{data.Path, license.Path}
	game.AddDLCSize = data.OriginalSize // what goes inside: the license adds no files
	o := Options{Mode: ModeP5z, Dest: dest, Level: 3}
	r := &Runner{Core: engine}
	sum := r.Run(context.Background(), PlanCompress([]Item{game}, o, SystemFS()), o)
	if sum.Done != 1 {
		t.Fatalf("%+v", sum)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 {
		t.Fatalf("made: %v", entries)
	}
	// what it saved is against the package's file and what went inside it, as the .p5z holds both
	size, before := SizeOf(filepath.Join(dest, entries[0].Name())), game.SizeBefore()+game.AddDLCSize
	if want := before - min(before, size); sum.Saved != want {
		t.Errorf("saved %d, not %d (%d before, %d made)", sum.Saved, want, before, size)
	}
	made, err := Describe(engine, filepath.Join(dest, entries[0].Name()))
	if err != nil || len(made.DLC) != 2 || !made.DLC[0].Data || made.DLC[0].Folder != "addcont0" || made.DLC[1].Data {
		t.Fatalf("what was made: %+v, %v", made.DLC, err)
	}
	if _, code, err := core.Run(context.Background(), engine, []string{"verify", made.Path}, nil); err != nil || code != 0 {
		t.Fatalf("verify: %d, %v", code, err)
	}
}
