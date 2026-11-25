// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/internal/par2"
	"github.com/DeLFuS77/maxpack/gui/internal/par2/par2test"
)

// names are the names in a folder, in order.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var list []string
	for _, entry := range entries {
		list = append(list, entry.Name())
	}
	sort.Strings(list)
	return list
}

// recorder keeps every state and detail of each game, and the phases of its progress.
type recorder struct {
	mu      sync.Mutex
	states  map[int][]string
	details map[int]string
	phases  map[int][]string
}

func newRecorder() *recorder {
	return &recorder{states: map[int][]string{}, details: map[int]string{}, phases: map[int][]string{}}
}

func (r *recorder) item(i int, state, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[i] = append(r.states[i], state)
	r.details[i] = detail
}

func (r *recorder) event(i int, e core.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.phases[i]); n == 0 || r.phases[i][n-1] != e.Phase {
		r.phases[i] = append(r.phases[i], e.Phase)
	}
}

func (r *recorder) last(i int) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.states[i]
	if len(list) == 0 {
		return "", ""
	}
	return list[len(list)-1], r.details[i]
}

func TestPlanCountsRepairData(t *testing.T) {
	dest := t.TempDir()
	it := item("Starfall Odyssey", "PPSA10001", 100*gib)
	set := par2.SetSize(100*gib, 5)
	f := newFS()
	f.free = 100*gib + set/2
	p := PlanCompress([]Item{it}, Options{Mode: ModeP5z, Dest: dest, Repair: 5}, f)[0]
	if p.Need != 100*gib+set || !hasFlag(p.Warnings, "may_not_fit") {
		t.Errorf("need %d (set %d), warnings %v", p.Need, set, p.Warnings)
	}
	// without repair data it fits
	p = PlanCompress([]Item{it}, Options{Mode: ModeP5z, Dest: dest}, f)[0]
	if p.Need != 100*gib || hasFlag(p.Warnings, "may_not_fit") {
		t.Errorf("without repair data: need %d, warnings %v", p.Need, p.Warnings)
	}
	// a folder has none, whatever was asked
	p = PlanCompress([]Item{it}, Options{Mode: ModeFolder, Dest: dest, Repair: 5}, f)[0]
	if p.Need != 100*gib {
		t.Errorf("a folder: need %d", p.Need)
	}
	// the space of the queue counts it
	two := []Item{it, item("Iron Harbor", "PPSA10002", 100*gib)}
	f.free = 1000 * gib
	plans := PlanCompress(two, Options{Mode: ModeP5z, Dest: dest, Repair: 10}, f)
	f.free = 200 * gib
	short := Space(plans, Options{Mode: ModeP5z, Dest: dest, Repair: 10}, f)
	if len(short) != 1 || short[0].Need != 2*(100*gib+par2.SetSize(100*gib, 10)) {
		t.Errorf("space: %+v", short)
	}
}

// compress makes a .p5z of a game with the stand-ins of the engine and of par2, with repair data.
func compress(t *testing.T, par2Mode string, percent int, ctx context.Context, r *Runner) (Plan, Summary) {
	t.Helper()
	t.Setenv(fakecore.Env, "writes")
	t.Setenv(fakecore.Par2Env, par2Mode)
	dest := t.TempDir()
	o := Options{Mode: ModeP5z, Dest: dest, Repair: percent}
	plans := PlanCompress([]Item{item("Starfall Odyssey", "PPSA10001", 10), item("Iron Harbor", "PPSA10002", 10)}, o, newFS())
	r.Core, r.Par2 = os.Args[0], os.Args[0]
	return plans[0], r.Run(ctx, plans, o)
}

func TestCompressMakesRepairData(t *testing.T) {
	rec := newRecorder()
	r := &Runner{OnItem: rec.item, OnEvent: rec.event}
	p, sum := compress(t, "makes", 5, context.Background(), r)
	if state, detail := rec.last(0); state != StateOK || detail != "" {
		t.Fatalf("%s %q", state, detail)
	}
	if got := strings.Join(rec.states[0], ","); !strings.Contains(got, "packing,verifying,protecting,ok") {
		t.Errorf("states %s", got)
	}
	if !strings.Contains(strings.Join(rec.phases[0], ","), "protecting") {
		t.Errorf("phases %v", rec.phases[0])
	}
	info := par2.Inspect(p.Out)
	if info.State != par2.StateMatch || info.Index != par2.IndexOf(p.Out) || len(info.Volumes) != 1 {
		t.Errorf("the set of the .p5z: %+v", info)
	}
	if sum.Done != 2 || sum.Protected != 2 || sum.ProtectedSize == 0 || sum.Unprotected != 0 {
		t.Errorf("%+v", sum)
	}
	for _, name := range names(t, filepath.Dir(p.Out)) {
		if strings.Contains(name, ".maxpack-") {
			t.Errorf("left apart: %s", name)
		}
	}
}

func TestCompressRepairDataFails(t *testing.T) {
	rec := newRecorder()
	p, sum := compress(t, "fails", 5, context.Background(), &Runner{OnItem: rec.item})
	// the game is done: its .p5z is good; it says that it has no repair data, and why
	state, detail := rec.last(0)
	if state != StateOK || !hasPart(detail, "repair_data_failed") || !strings.Contains(detail, "Invalid option") {
		t.Errorf("%s %q", state, detail)
	}
	if _, err := os.Stat(p.Out); err != nil {
		t.Errorf("the .p5z: %v", err)
	}
	if got := names(t, filepath.Dir(p.Out)); len(got) != 2 {
		t.Errorf("more than the two .p5z: %v", got)
	}
	if sum.Done != 2 || sum.Protected != 0 || sum.Unprotected != 2 || sum.Failed != 0 {
		t.Errorf("%+v", sum)
	}
}

func TestCompressRepairDataCancelled(t *testing.T) {
	rec := newRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &Runner{OnItem: func(i int, state, detail string) {
		rec.item(i, state, detail)
		if state == StateProtecting {
			cancel()
		}
	}}
	p, sum := compress(t, "slow", 5, ctx, r)
	// done, without repair data, and no part of a set left behind
	if state, detail := rec.last(0); state != StateOK || detail != "repair_data_cancelled" {
		t.Errorf("%s %q", state, detail)
	}
	if got := names(t, filepath.Dir(p.Out)); len(got) != 1 || got[0] != filepath.Base(p.Out) {
		t.Errorf("left: %v", got)
	}
	if state, _ := rec.last(1); state != StateCancelled {
		t.Errorf("the next game: %s", state)
	}
	if sum.Done != 1 || sum.Cancelled != 1 || sum.Unprotected != 1 {
		t.Errorf("%+v", sum)
	}
}

func TestCompressWithoutPar2(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	dest := t.TempDir()
	o := Options{Mode: ModeP5z, Dest: dest, Repair: 5}
	plans := PlanCompress([]Item{item("Starfall Odyssey", "PPSA10001", 10)}, o, newFS())
	on, state, detail := lastDetail()
	sum := (&Runner{Core: os.Args[0], OnItem: on}).Run(context.Background(), plans, o)
	if *state != StateOK || *detail != "no_par2" || sum.Unprotected != 1 {
		t.Errorf("%s %q %+v", *state, *detail, sum)
	}
}

// A new .p5z that replaces an old one of the same name takes the old one's set away with it (it
// protected the old one); the set of another file stays.
func TestReplaceTakesTheOldSet(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, own := range []bool{true, false} {
		dest := t.TempDir()
		o := Options{Mode: ModeP5z, Dest: dest}
		it := item("Starfall Odyssey", "PPSA10001", 10)
		out := PlanCompress([]Item{it}, o, newFS())[0].Out
		var files []string
		if own {
			os.WriteFile(out, []byte("the old one, with its set"), 0o644)
			files, _ = par2test.Write(out, filepath.Base(out), 4096, 1)
		} else {
			files = otherSet(t, out)
		}
		plans := PlanCompress([]Item{it}, o, newFS())
		on, state, detail := lastDetail()
		(&Runner{Core: os.Args[0], OnItem: on}).Run(context.Background(), plans, o)
		if *state != StateOK {
			t.Fatalf("%s %q", *state, *detail)
		}
		for _, file := range files {
			if _, err := os.Stat(file); own != errors.Is(err, fs.ErrNotExist) {
				t.Errorf("own set %v: %s: %v", own, filepath.Base(file), err)
			}
		}
	}
}

// otherSet writes at path a file with the set of another file next to it, named as its own.
func otherSet(t *testing.T, path string) []string {
	t.Helper()
	os.WriteFile(path, []byte("another file"), 0o644)
	files, err := par2test.Write(path, filepath.Base(path), 4096, 1)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("this file, which has no set"), 0o644)
	return files
}

// existing writes .p5z files (as the stand-in of the engine verifies anything) in a folder.
func existing(t *testing.T, dir string, n int) []Item {
	t.Helper()
	var items []Item
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, "Game "+string(rune('A'+i))+".p5z")
		if err := os.WriteFile(path, bytes.Repeat([]byte{byte(i + 1)}, 400_000+i), 0o644); err != nil {
			t.Fatal(err)
		}
		items = append(items, Item{Path: path, Name: "Game", TitleID: "PPSA1000" + string(rune('1'+i)), Kind: "p5z", OriginalSize: 100_000})
	}
	return items
}

func TestAddRepairData(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	t.Setenv(fakecore.Par2Env, "makes")
	dir := t.TempDir()
	items := existing(t, dir, 3)
	// the second has a set of 5 % already; the third, the set of another file
	if _, err := par2test.Write(items[1].Path, filepath.Base(items[1].Path), 4096, 5); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(items[2].Path)
	other := otherSet(t, items[2].Path)
	os.WriteFile(items[2].Path, data, 0o644)
	run := func(percent int) (*recorder, Summary) {
		o := Options{Mode: ModeProtect, Repair: percent}
		plans := PlanProtect(items, o, newFS())
		rec := newRecorder()
		sum := (&Runner{Core: os.Args[0], Par2: os.Args[0], OnItem: rec.item}).Run(context.Background(), plans, o)
		return rec, sum
	}
	// 400 001 bytes are 98 blocks of 4 KiB: 5 % of them is 5 blocks, as the set there has
	rec, sum := run(5)
	if state, detail := rec.last(0); state != StateOK || detail != "" || strings.Join(rec.states[0], ",") != "waiting,verifying,protecting,ok" {
		t.Errorf("the first: %v %q", rec.states[0], detail)
	}
	if state, detail := rec.last(1); state != StateSkipped || detail != "repair_data_same" {
		t.Errorf("the second, with the same: %s %q", state, detail)
	}
	if state, detail := rec.last(2); state != StateSkipped || detail != "repair_data_other" {
		t.Errorf("the third, with another file's: %s %q", state, detail)
	}
	if sum.Done != 1 || sum.Skipped != 2 || sum.Protected != 1 {
		t.Errorf("%+v", sum)
	}
	for _, file := range other {
		if _, err := os.Stat(file); err != nil {
			t.Errorf("the set of another file was touched: %v", err)
		}
	}
	// another percentage replaces a set of its own, whole
	rec, _ = run(10)
	if state, detail := rec.last(1); state != StateOK || detail != "" {
		t.Errorf("10 %%: %s %q", state, detail)
	}
	if info := par2.Inspect(items[1].Path); info.State != par2.StateMatch || info.Recovery != 10 || len(info.Volumes) != 1 {
		t.Errorf("the set made again: %+v", info)
	}
	// a .p5z that does not verify gets none
	t.Setenv(fakecore.Env, "bad-verify")
	os.Remove(par2.IndexOf(items[0].Path))
	for _, file := range par2.Files(items[0].Path) {
		os.Remove(file)
	}
	rec, _ = run(5)
	if state, detail := rec.last(0); state != StateError || !hasPart(detail, "verify_failed") {
		t.Errorf("damaged: %s %q", state, detail)
	}
	if files := par2.Files(items[0].Path); len(files) != 0 {
		t.Errorf("a set for a damaged .p5z: %v", files)
	}
}

func TestPlanProtectAndRepair(t *testing.T) {
	dir := t.TempDir()
	items := existing(t, dir, 3)
	par2test.Write(items[0].Path, filepath.Base(items[0].Path), 4096, 2)
	data, _ := os.ReadFile(items[1].Path)
	otherSet(t, items[1].Path)
	os.WriteFile(items[1].Path, data, 0o644)
	folder := Item{Path: dir, Kind: "folder", Packed: true}
	o := Options{Mode: ModeRepair}
	plans := PlanRepair(append(items, folder), o, newFS())
	for i, want := range []string{"", "repair_data_other", "no_repair_data", "not_p5z"} {
		if plans[i].Block != want {
			t.Errorf("repair %d: %q, expected %q", i, plans[i].Block, want)
		}
	}
	// what a repair needs: a whole copy of the .p5z, and some room
	if need := plans[0].Need; need != 400_000+repairMargin(400_000) {
		t.Errorf("need %d", need)
	}
	if plans := PlanProtect(append(items, folder), Options{Mode: ModeProtect, Repair: 5}, newFS()); plans[3].Block != "not_p5z" || plans[0].Block != "" || plans[0].Need != par2.SetSize(400_000, 5) {
		t.Errorf("protect: %+v", plans)
	}
	// the drive of each .p5z is the one that counts
	f := newFS()
	f.free = 400_000 + repairMargin(400_000)
	if short := Space(PlanRepair(items[:1], o, f), o, f); len(short) != 0 {
		t.Errorf("a repair that fits: %+v", short)
	}
	f.free = 300_000
	if short := Space(PlanRepair(items[:1], o, f), o, f); len(short) != 1 || short[0].Free != 300_000 {
		t.Errorf("a repair that does not fit: %+v", short)
	}
}

func TestRepairThatCannotBeDoneLeavesTheFile(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, mode := range []string{"not-possible", "fails", "slow"} {
		t.Setenv(fakecore.Par2Env, mode)
		dir := t.TempDir()
		items := existing(t, dir, 1)
		before, _ := os.ReadFile(items[0].Path)
		par2test.Write(items[0].Path, filepath.Base(items[0].Path), 4096, 2)
		o := Options{Mode: ModeRepair}
		plans := PlanRepair(items, o, newFS())
		ctx, cancel := context.WithCancel(context.Background())
		rec := newRecorder()
		r := &Runner{Core: os.Args[0], Par2: os.Args[0], OnItem: rec.item, OnEvent: func(i int, e core.Event) {
			if mode == "slow" {
				cancel()
			}
		}}
		r.Run(ctx, plans, o)
		cancel()
		state, detail := rec.last(0)
		switch mode {
		case "not-possible":
			if state != StateError || detail != "repair_not_possible: 7" {
				t.Errorf("%s: %s %q", mode, state, detail)
			}
		case "fails":
			if state != StateError || !hasPart(detail, "repair_failed") {
				t.Errorf("%s: %s %q", mode, state, detail)
			}
		case "slow":
			if state != StateCancelled {
				t.Errorf("%s: %s %q", mode, state, detail)
			}
		}
		// the .p5z is where it was, as it was, and nothing of the repair is left
		if after, err := os.ReadFile(items[0].Path); err != nil || !bytes.Equal(after, before) {
			t.Errorf("%s: the .p5z changed: %v", mode, err)
		}
		if got := names(t, dir); len(got) != 3 {
			t.Errorf("%s: %v", mode, got)
		}
	}
}

func TestRepairFolderInTheWay(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	t.Setenv(fakecore.Par2Env, "makes")
	dir := t.TempDir()
	items := existing(t, dir, 1)
	par2test.Write(items[0].Path, filepath.Base(items[0].Path), 4096, 2)
	// what a repair cut short left: it may have a copy of a .p5z, so it is never deleted here
	work := items[0].Path + RepairSuffix
	os.Mkdir(work, 0o755)
	os.WriteFile(filepath.Join(work, "x"), []byte("x"), 0o644)
	o := Options{Mode: ModeRepair}
	on, state, detail := lastDetail()
	(&Runner{Core: os.Args[0], Par2: os.Args[0], OnItem: on}).Run(context.Background(), PlanRepair(items, o, newFS()), o)
	if *state != StateError || *detail != "repair_folder_exists: "+work {
		t.Errorf("%s %q", *state, *detail)
	}
	if _, err := os.Stat(filepath.Join(work, "x")); err != nil {
		t.Errorf("the folder of the repair was touched: %v", err)
	}
}

// Rename takes the set of a .p5z with it, all or nothing, and is blocked as it is for the .p5z by a
// name of the set that is taken.
func TestRenameTakesTheSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "starfall.p5z")
	os.WriteFile(path, []byte("the game"), 0o644)
	set, _ := par2test.Write(path, "starfall.p5z", 4096, 3)
	it := Item{Path: path, Kind: "p5z", Name: "Starfall Odyssey", TitleID: "PPSA10001"}
	plans := PlanRename([]Item{it}, DefaultTemplate)
	newName := filepath.Join(dir, "Starfall Odyssey [PPSA10001].p5z")
	if plans[0].Block != "" || plans[0].To != newName || len(plans[0].Set) != 2 {
		t.Fatalf("%+v", plans[0])
	}
	results := Rename(plans)
	if results[0].State != StateOK {
		t.Fatalf("%+v", results[0])
	}
	want := []string{"Starfall Odyssey [PPSA10001].p5z", "Starfall Odyssey [PPSA10001].p5z.par2", "Starfall Odyssey [PPSA10001].p5z.vol0+3.par2"}
	if got := names(t, dir); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%v", got)
	}
	// the set keeps the name it had for it, and is still its own
	if info := par2.Inspect(newName); info.State != par2.StateMatch || info.Name != "starfall.p5z" {
		t.Errorf("%+v", info)
	}
	for _, file := range set {
		if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("left at its old name: %s", file)
		}
	}
}

func TestRenameOfASetAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "starfall.p5z")
	os.WriteFile(path, []byte("the game"), 0o644)
	par2test.Write(path, "starfall.p5z", 4096, 3)
	it := Item{Path: path, Kind: "p5z", Name: "Starfall Odyssey", TitleID: "PPSA10001"}
	// a name of the set is taken: the plan says so
	taken := filepath.Join(dir, "Starfall Odyssey [PPSA10001].p5z.par2")
	os.WriteFile(taken, []byte("ajeno"), 0o644)
	if plans := PlanRename([]Item{it}, DefaultTemplate); plans[0].Block != "name_taken" {
		t.Errorf("%+v", plans[0])
	}
	os.Remove(taken)
	// it is taken between the plan and the click: nothing is renamed
	plans := PlanRename([]Item{it}, DefaultTemplate)
	os.WriteFile(filepath.Join(dir, "Starfall Odyssey [PPSA10001].p5z.vol0+3.par2"), []byte("ajeno"), 0o644)
	results := Rename(plans)
	if results[0].State != StateSkipped || results[0].Detail != "name_taken" {
		t.Errorf("%+v", results[0])
	}
	want := []string{"Starfall Odyssey [PPSA10001].p5z.vol0+3.par2", "starfall.p5z", "starfall.p5z.par2", "starfall.p5z.vol0+3.par2"}
	if got := names(t, dir); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%v", got)
	}
	// the .p5z cannot take its name when its turn comes: what of its set was renamed goes back
	os.Remove(filepath.Join(dir, "Starfall Odyssey [PPSA10001].p5z.vol0+3.par2"))
	plans = PlanRename([]Item{it}, DefaultTemplate)
	failing := renameFile
	t.Cleanup(func() { renameFile = failing })
	renameFile = func(from, to string) error {
		if to == plans[0].To {
			return errors.New("in use")
		}
		return failing(from, to)
	}
	results = Rename(plans)
	if results[0].State != StateError || results[0].Detail != "io: in use" {
		t.Errorf("%+v", results[0])
	}
	want = []string{"starfall.p5z", "starfall.p5z.par2", "starfall.p5z.vol0+3.par2"}
	if got := names(t, dir); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%v", got)
	}
}

// cutShort is what a repair of <dir>/game.p5z cut short at a step leaves: its folder with the file
// of its step and, as they are named there, the files given (name: content); the .p5z itself when
// there is given.
func cutShort(t *testing.T, step string, there string, files map[string]string) (p5z, work string, l *Leftovers) {
	t.Helper()
	dir := t.TempDir()
	p5z = filepath.Join(dir, "game.p5z")
	work = p5z + RepairSuffix
	os.Mkdir(work, 0o755)
	if step != "" {
		writeStep(work, repairStep{Name: "game.p5z", Step: step})
	}
	for name, content := range files {
		os.WriteFile(filepath.Join(work, name), []byte(content), 0o644)
	}
	if there != "" {
		os.WriteFile(p5z, []byte(there), 0o644)
	}
	l = &Leftovers{File: filepath.Join(t.TempDir(), "unfinished.json")}
	l.Add(work)
	return p5z, work, l
}

// What a repair cut short leaves is offered at the next start with the safe choice: the .p5z goes
// back (the one that verified, else the damaged one as it was); the folder goes only when the .p5z
// is in its place and the folder has nothing else of it, and nothing is ever deleted unasked.
func TestLeftoversOfARepair(t *testing.T) {
	cases := []struct {
		about, step, there string
		files              map[string]string
		restore, keep      bool
		want               string // what the .p5z is after Clean
	}{
		{"cut while par2 wrote", stepMoved, "", map[string]string{"game.p5z.1": "damaged", "game.p5z": "half"}, true, false, "damaged"},
		{"cut before par2 began", stepMoved, "", map[string]string{"game.p5z": "damaged"}, true, false, "damaged"},
		{"cut once it verified", stepVerified, "", map[string]string{"game.p5z.1": "damaged", "game.p5z": "repaired"}, true, false, "repaired"},
		{"cut once in its place", stepPlaced, "repaired", map[string]string{"game.p5z.1": "damaged"}, false, false, "repaired"},
		{"cut before it moved", stepMoving, "damaged", nil, false, false, "damaged"},
		{"the .p5z there, and a copy in the folder", stepMoved, "something", map[string]string{"game.p5z.1": "damaged"}, false, true, "something"},
		{"no step, the .p5z gone", "", "", map[string]string{"game.p5z": "damaged"}, false, true, ""},
	}
	for _, c := range cases {
		p5z, work, l := cutShort(t, c.step, c.there, c.files)
		found := l.Found()
		if len(found) != 1 || found[0].Path != work || (found[0].Restore != "") != c.restore || found[0].Keep != c.keep {
			t.Errorf("%s: %+v", c.about, found)
			continue
		}
		if c.restore && found[0].Restore != p5z {
			t.Errorf("%s: goes back to %q", c.about, found[0].Restore)
		}
		failed := l.Clean()
		if c.keep != (len(failed) == 1) {
			t.Errorf("%s: %v", c.about, failed)
		}
		data, err := os.ReadFile(p5z)
		if c.want == "" && err == nil || c.want != "" && string(data) != c.want {
			t.Errorf("%s: the .p5z is %q (%v)", c.about, data, err)
		}
		if _, err := os.Stat(work); c.keep == errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: the folder: %v", c.about, err)
		}
	}
	// the folder where a set was made goes as the others do
	dir := t.TempDir()
	tmp := filepath.Join(dir, "game.p5z"+Par2Suffix)
	os.Mkdir(tmp, 0o755)
	os.WriteFile(filepath.Join(tmp, "game.p5z.par2"), []byte("half"), 0o644)
	l := &Leftovers{File: filepath.Join(t.TempDir(), "unfinished.json")}
	l.Add(tmp)
	if found := l.Found(); len(found) != 1 || found[0].Restore != "" || found[0].Keep {
		t.Errorf("%+v", found)
	}
	if failed := l.Clean(); len(failed) != 0 {
		t.Errorf("%v", failed)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%v", err)
	}
}
