// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"path/filepath"
	"testing"
)

// What the engine says of a game against the list of the original package's files it carries: the
// counts, and why it could not be checked; never the paths, which stay in the engine's JSON. An
// engine of before says nothing (nil).
func TestFromInfoCompleteness(t *testing.T) {
	it := FromInfo("x", map[string]any{"kind": "folder", "completeness": map[string]any{
		"checkable": true, "reason": "", "listed": float64(8), "present": float64(6), "missing": float64(2),
		"extra_count": float64(4), "extra_bytes": float64(11184), "leftover_count": float64(3), "leftover_bytes": float64(11064),
		"extra_paths": []any{"decrypted/eboot.bin"},
	}})
	c := it.Completeness
	if c == nil || !c.Checkable || c.Reason != "" || c.Listed != 8 || c.Present != 6 || c.Missing != 2 || c.ExtraCount != 4 ||
		c.ExtraBytes != 11184 || c.LeftoverCount != 3 || c.LeftoverBytes != 11064 {
		t.Errorf("%+v", c)
	}
	it = FromInfo("x", map[string]any{"kind": "exfat", "completeness": map[string]any{"checkable": false, "reason": "no_table",
		"leftover_count": float64(1), "leftover_bytes": float64(7000)}})
	if c := it.Completeness; c == nil || c.Checkable || c.Reason != "no_table" || c.LeftoverCount != 1 || c.LeftoverBytes != 7000 {
		t.Errorf("%+v", c)
	}
	if it := FromInfo("x", map[string]any{"kind": "folder"}); it.Completeness != nil {
		t.Errorf("an engine of before: %+v", it.Completeness)
	}
	// missing is what a game lacks only when it could be checked
	if (&Completeness{Missing: 3}).MissingFiles() != 0 || (&Completeness{Checkable: true, Missing: 3}).MissingFiles() != 3 {
		t.Error("MissingFiles")
	}
	var none *Completeness
	if none.MissingFiles() != 0 || none.DumpTool() {
		t.Error("MissingFiles of nil")
	}
	// against the dump tool's list: which list it was, and the files of another size
	it = FromInfo("x", map[string]any{"kind": "folder", "completeness": map[string]any{"checkable": true, "reason": "", "source": "dump_index",
		"listed": float64(13), "present": float64(10), "missing": float64(2), "size_mismatch": float64(1)}})
	if c := it.Completeness; c == nil || c.Source != "dump_index" || c.SizeMismatch != 1 || c.MissingFiles() != 2 || !c.DumpTool() {
		t.Errorf("%+v", c)
	}
	if c := completenessOf(map[string]any{"completeness": map[string]any{"checkable": true, "source": "playgo"}}); c.DumpTool() {
		t.Errorf("playgo: %+v", c)
	}
}

// Compress leaves out what dump tools leave behind, which the engine does only when the game's own
// list proves it is not the game's; KeepLeftovers keeps it. Nothing else takes the option.
func TestCompressLeavesOutLeftovers(t *testing.T) {
	items := []Item{item("Starfall Odyssey", "PPSA10001", 10*gib)}
	for _, mode := range []Mode{ModeP5z, ModeFolder} {
		o := Options{Mode: mode, Dest: t.TempDir()}
		if plans := PlanCompress(items, o, newFS()); !hasArg(plans[0].Args, "--leave-out-leftovers") {
			t.Errorf("%s: %v", mode, plans[0].Args)
		}
		o.KeepLeftovers = true
		if plans := PlanCompress(items, o, newFS()); hasArg(plans[0].Args, "--leave-out-leftovers") {
			t.Errorf("%s kept: %v", mode, plans[0].Args)
		}
	}
	p5z := Item{Path: filepath.Join(t.TempDir(), "a.p5z"), TitleID: "PPSA10001", Name: "A", Kind: "p5z", OriginalSize: gib, Packed: true}
	for _, plans := range [][]Plan{PlanDecompress([]Item{p5z}, Options{Mode: ModeOriginal, Dest: t.TempDir()}, newFS()), PlanVerify([]Item{p5z})} {
		if hasArg(plans[0].Args, "--leave-out-leftovers") {
			t.Errorf("%v", plans[0].Args)
		}
	}
}

// With the real engine, the dump tool's list of a dump without the table: a fine dump, and a copy cut
// short, whose .p5z says the same (it keeps the dump tool's list).
func TestDumpIndexWithTheEngine(t *testing.T) {
	engine, fixtures := needCore(t)
	di := filepath.Join(fixtures, "dumpindex")
	if c := describe(t, engine, filepath.Join(di, "ok")).Completeness; c == nil || !c.DumpTool() || c.Listed != 13 || c.MissingFiles() != 0 || c.SizeMismatch != 0 {
		t.Errorf("ok: %+v", c)
	}
	broken := describe(t, engine, filepath.Join(di, "broken"))
	if c := broken.Completeness; c == nil || !c.DumpTool() || c.Missing != 2 || c.SizeMismatch != 1 {
		t.Fatalf("broken: %+v", c)
	}
	o := Options{Mode: ModeP5z, Dest: t.TempDir(), Level: 3}
	plans := PlanCompress([]Item{broken}, o, newFS())
	s := &states{list: map[int][]string{}}
	r := &Runner{Core: engine, OnItem: s.on}
	if sum := r.Run(context.Background(), plans, o); sum.Done != 1 {
		t.Fatalf("%+v, states %v", sum, s.list)
	}
	if c := describe(t, engine, plans[0].Out).Completeness; c == nil || !c.DumpTool() || c.Missing != 2 || c.SizeMismatch != 1 || c.LeftoverCount != 1 {
		t.Errorf("the .p5z: %+v", c)
	}
}

// With the real engine: what it says of the dumps of make_playgo.py, and a queue that leaves out the
// leftovers the table proves are not the game's, whose .p5z is still complete.
func TestCompletenessWithTheEngine(t *testing.T) {
	engine, fixtures := needCore(t)
	pg := filepath.Join(fixtures, "playgo")
	missing := describe(t, engine, filepath.Join(pg, "missing"))
	if c := missing.Completeness; c == nil || !c.Checkable || c.Missing != 2 || c.Listed != 8 {
		t.Fatalf("missing: %+v", c)
	}
	if c := describe(t, engine, filepath.Join(pg, "notable")).Completeness; c == nil || c.Checkable || c.Reason != "no_table" || c.LeftoverCount != 1 {
		t.Errorf("notable: %+v", c)
	}
	it := describe(t, engine, filepath.Join(pg, "leftovers"))
	if c := it.Completeness; c == nil || !c.Checkable || c.Missing != 0 || c.LeftoverCount != 3 {
		t.Fatalf("leftovers: %+v", c)
	}
	o := Options{Mode: ModeP5z, Dest: t.TempDir(), Level: 3}
	plans := PlanCompress([]Item{it}, o, newFS())
	s := &states{list: map[int][]string{}}
	r := &Runner{Core: engine, OnItem: s.on}
	if sum := r.Run(context.Background(), plans, o); sum.Done != 1 || sum.Failed != 0 {
		t.Fatalf("%+v, states %v", sum, s.list)
	}
	made := describe(t, engine, plans[0].Out)
	if c := made.Completeness; c == nil || !c.Checkable || c.Missing != 0 || c.Present != 8 || c.LeftoverCount != 0 {
		t.Errorf("the .p5z: %+v", c)
	}
	if made.OriginalSize != it.OriginalSize-11064 {
		t.Errorf("the .p5z has %d bytes of the dump, not %d", made.OriginalSize, it.OriginalSize-11064)
	}
}
