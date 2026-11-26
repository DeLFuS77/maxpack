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
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/internal/par2"
	"github.com/DeLFuS77/maxpack/gui/internal/par2/par2test"
)

// What the engine says of a .p5z that grew: how many times, the room it does not use, and an append
// that stopped and was undone when it was read.
func TestFromInfoGrown(t *testing.T) {
	it := FromInfo("g.p5z", map[string]any{"kind": "p5z", "grown": float64(2), "unused_bytes": float64(12345), "recovered": true})
	if it.Grown != 2 || it.UnusedBytes != 12345 || !it.Recovered {
		t.Errorf("%+v", it)
	}
	if it := FromInfo("x", map[string]any{"kind": "p5z"}); it.Grown != 0 || it.UnusedBytes != 0 || it.Recovered {
		t.Errorf("an engine of before: %+v", it)
	}
}

// DLC added to a .p5z, and a new dump of it: the engine works on the .p5z itself (nothing is written
// apart), with the DLC in the order given, or the dump, and --force only when asked; what it may take
// is what is given. Anything but a .p5z cannot go.
func TestPlanGrow(t *testing.T) {
	dir := t.TempDir()
	p5z := filepath.Join(dir, "Game [PPSA10001].p5z")
	os.WriteFile(p5z, []byte("a .p5z"), 0o644)
	dlc := filepath.Join(dir, "dlc.pkg")
	os.WriteFile(dlc, make([]byte, 3000), 0o644)
	it := Item{Path: p5z, Kind: "p5z", TitleID: "PPSA10001", Name: "Game", OriginalSize: 10 * gib, Grow: []string{dlc, `D:\dlc\b`}}
	plans := PlanGrow([]Item{it}, Options{Mode: ModeAddDLC}, newFS())
	p := plans[0]
	if p.Block != "" || p.Out != p5z || p.Tmp != "" || len(p.Replace) != 0 {
		t.Errorf("add: %+v", p)
	}
	if args := strings.Join(p.Args, " "); !strings.HasPrefix(args, "add "+p5z+" --dlc "+dlc+` --dlc D:\dlc\b`) || slices.Contains(p.Args, "--force") {
		t.Errorf("add: %v", p.Args)
	}
	if p.Need < 3000 {
		t.Errorf("add needs %d", p.Need)
	}
	dump := filepath.Join(dir, "v2")
	os.MkdirAll(dump, 0o755)
	os.WriteFile(filepath.Join(dump, "eboot.bin"), make([]byte, 5000), 0o644)
	it.Grow, it.Force = []string{dump}, true
	plans = PlanGrow([]Item{it}, Options{Mode: ModeUpdate}, newFS())
	if args := strings.Join(plans[0].Args, " "); !strings.HasPrefix(args, "update "+p5z+" "+dump) || !slices.Contains(plans[0].Args, "--force") ||
		!slices.Contains(plans[0].Args, "--leave-out-leftovers") {
		t.Errorf("update: %v", plans[0].Args)
	}
	if plans[0].Need < 5000 {
		t.Errorf("update needs %d", plans[0].Need)
	}
	it.Force = false
	plans = PlanGrow([]Item{it}, Options{Mode: ModeUpdate, KeepLeftovers: true}, newFS())
	if slices.Contains(plans[0].Args, "--force") || slices.Contains(plans[0].Args, "--leave-out-leftovers") {
		t.Errorf("update, not forced, leftovers kept: %v", plans[0].Args)
	}
	folder := Item{Path: dir, Kind: "folder", Grow: []string{dlc}}
	if plans := PlanGrow([]Item{folder}, Options{Mode: ModeAddDLC}, newFS()); plans[0].Block != "not_p5z" {
		t.Errorf("a folder: %+v", plans[0])
	}
	none := Item{Path: p5z, Kind: "p5z"}
	if plans := PlanGrow([]Item{none}, Options{Mode: ModeUpdate}, newFS()); plans[0].Block != "nothing_given" {
		t.Errorf("nothing given: %+v", plans[0])
	}
	// with little room: DLC added are all new, and may not fit; what an update adds is known only once the
	// engine compared it, and the engine stops before writing when that does not fit, so the size of the
	// whole new dump is no reason to warn
	little := fakeFS{free: 1000, typ: "NTFS"}
	it.Grow = []string{dlc}
	if p := PlanGrow([]Item{it}, Options{Mode: ModeAddDLC}, little)[0]; !hasWarning(p, "may_not_fit") {
		t.Errorf("DLC of 3000 bytes with 1000 free: %v", p.Warnings)
	}
	it.Grow = []string{dump}
	if p := PlanGrow([]Item{it}, Options{Mode: ModeUpdate}, little)[0]; hasWarning(p, "may_not_fit") || p.Block != "" {
		t.Errorf("an update of a dump of 5000 bytes with 1000 free: %v %q", p.Warnings, p.Block)
	}
}

// The Runner: the .p5z grows in its place and is verified; with repair data of its own before, it gets
// them made again of the same share; a refusal of the engine is said with its code, the ones that
// would read as Compress's in words of their own.
func TestGrowRun(t *testing.T) {
	t.Setenv(fakecore.Env, "grows")
	t.Setenv(fakecore.Par2Env, "makes")
	dir := t.TempDir()
	items := existing(t, dir, 2)
	if _, err := par2test.Write(items[1].Path, filepath.Base(items[1].Path), 4096, 5); err != nil {
		t.Fatal(err)
	}
	for i := range items {
		items[i].Grow = []string{filepath.Join(dir, "dlc.pkg")}
	}
	o := Options{Mode: ModeAddDLC}
	rec := newRecorder()
	sum := (&Runner{Core: os.Args[0], Par2: os.Args[0], OnItem: rec.item}).Run(context.Background(), PlanGrow(items, o, newFS()), o)
	if state, detail := rec.last(0); state != StateOK || detail != "grown" || strings.Join(rec.states[0], ",") != "waiting,growing,verifying,ok" {
		t.Errorf("the first: %v %q", rec.states[0], detail)
	}
	if state, detail := rec.last(1); state != StateOK || detail != "grown; repair_redone" ||
		strings.Join(rec.states[1], ",") != "waiting,growing,verifying,protecting,ok" {
		t.Errorf("the second, with repair data: %v %q", rec.states[1], detail)
	}
	if info := par2.Inspect(items[1].Path); info.State != par2.StateMatch || info.Percent != 5 {
		t.Errorf("its repair data now: %+v", info)
	}
	if sum.Done != 2 || sum.Protected != 1 {
		t.Errorf("%+v", sum)
	}
	if data, _ := os.ReadFile(items[0].Path); !strings.HasSuffix(string(data), "+grown") {
		t.Error("the .p5z did not grow in its place")
	}
	// refused: more than half changed, and the codes that mean something else for Compress
	t.Setenv(fakecore.Env, "mostly")
	o = Options{Mode: ModeUpdate}
	items[0].Grow = []string{dir}
	rec = newRecorder()
	(&Runner{Core: os.Args[0], OnItem: rec.item}).Run(context.Background(), PlanGrow(items[:1], o, newFS()), o)
	if state, detail := rec.last(0); state != StateError || detail != "mostly_changed" {
		t.Errorf("mostly changed: %s %q", state, detail)
	}
	items[0].Force = true
	rec = newRecorder()
	(&Runner{Core: os.Args[0], OnItem: rec.item}).Run(context.Background(), PlanGrow(items[:1], o, newFS()), o)
	if state, _ := rec.last(0); state != StateOK {
		t.Errorf("forced: %s", state)
	}
	for code, want := range map[string]string{"no_space": "grow_no_space", "too_big": "grow_too_big", "already_packed": "dump_packed", "busy": "busy"} {
		if got := growFailure(code); got != want {
			t.Errorf("%s: %s", code, got)
		}
	}
}

// With the real engine: DLC added to a .p5z, then a new dump of it, both verified; a second time the
// same dump is refused (the same version).
func TestGrowReal(t *testing.T) {
	engine, fixtures := needCore(t)
	dir := t.TempDir()
	p5z := filepath.Join(dir, "Game [PPSA00000].p5z")
	if _, _, err := core.RunWith(context.Background(), engine, []string{"pack", filepath.Join(fixtures, "game"), "-o", p5z, "--p5z", "--threads", "2"}, core.RunOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	it, err := Describe(engine, p5z)
	if err != nil || it.Kind != "p5z" || it.Grown != 0 {
		t.Fatalf("%+v %v", it, err)
	}
	it.Grow = []string{filepath.Join(fixtures, "dlc_data")}
	o := Options{Mode: ModeAddDLC}
	rec := newRecorder()
	sum := (&Runner{Core: engine, OnItem: rec.item}).Run(context.Background(), PlanGrow([]Item{it}, o, newFS()), o)
	if state, detail := rec.last(0); state != StateOK || sum.Done != 1 {
		t.Fatalf("add: %s %q", state, detail)
	}
	if it, err = Describe(engine, p5z); err != nil || it.Grown != 1 || len(it.DLC) != 1 {
		t.Fatalf("after add: %+v %v", it, err)
	}
	// a new dump: the game of the fixtures in another version
	v2 := filepath.Join(dir, "v2")
	copyTree(t, filepath.Join(fixtures, "game"), v2)
	param := filepath.Join(v2, "sce_sys", "param.json")
	data, _ := os.ReadFile(param)
	os.WriteFile(param, []byte(strings.Replace(string(data), `"01.000.000"`, `"01.001.000"`, 1)), 0o644)
	it.Grow = []string{v2}
	o = Options{Mode: ModeUpdate}
	rec = newRecorder()
	(&Runner{Core: engine, OnItem: rec.item}).Run(context.Background(), PlanGrow([]Item{it}, o, newFS()), o)
	if state, detail := rec.last(0); state != StateOK {
		t.Fatalf("update: %s %q", state, detail)
	}
	if it, err = Describe(engine, p5z); err != nil || it.Grown != 2 || it.Version != "01.001.000" || len(it.DLC) != 1 {
		t.Fatalf("after update: %+v %v", it, err)
	}
	it.Grow = []string{v2}
	rec = newRecorder()
	(&Runner{Core: engine, OnItem: rec.item}).Run(context.Background(), PlanGrow([]Item{it}, o, newFS()), o)
	if state, detail := rec.last(0); state != StateError || detail != "same_version" {
		t.Errorf("the same dump again: %s %q", state, detail)
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	})
	if err != nil {
		t.Fatal(err)
	}
}
