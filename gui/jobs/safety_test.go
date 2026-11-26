// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/internal/trash"
)

// Every output is written apart, not only one that replaces another: the engine writes
// <Out>.maxpack-tmp, and only what was verified takes the name Out.
func TestEveryOutputIsWrittenApart(t *testing.T) {
	dest := t.TempDir()
	it := item("Starfall Odyssey", "PPSA10001", 10)
	packed := it
	packed.Kind = "p5z"
	for _, c := range []struct {
		mode  Mode
		plans func(Options) []Plan
	}{
		{ModeP5z, func(o Options) []Plan { return PlanCompress([]Item{it}, o, newFS()) }},
		{ModeFolder, func(o Options) []Plan { return PlanCompress([]Item{it}, o, newFS()) }},
		{ModeOriginal, func(o Options) []Plan { return PlanDecompress([]Item{packed}, o, newFS()) }},
		{ModeConsole, func(o Options) []Plan { return PlanDecompress([]Item{packed}, o, newFS()) }},
	} {
		p := c.plans(Options{Mode: c.mode, Dest: dest})[0]
		if p.Tmp != p.Out+".maxpack-tmp" || len(p.Replace) != 0 || p.Block != "" {
			t.Errorf("%s: %+v", c.mode, p)
		}
		if !hasArg(p.Args, p.Tmp) || hasArg(p.Args, p.Out) {
			t.Errorf("%s: the engine does not write apart: %v", c.mode, p.Args)
		}
	}
	if p := PlanVerify([]Item{packed})[0]; p.Tmp != "" || p.Out != "" {
		t.Errorf("verify writes: %+v", p)
	}
}

// What is being made is hidden (on Windows) and apart until it is verified; then it takes its
// name, shown, and nothing is left apart.
func TestOutputTakesItsNameOnceVerified(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, mode := range []Mode{ModeP5z, ModeFolder} {
		dest := t.TempDir()
		o := Options{Mode: mode, Dest: dest}
		plans := PlanCompress([]Item{item("Starfall Odyssey", "PPSA10001", 10)}, o, newFS())
		p := plans[0]
		var seen []string
		var mutex sync.Mutex
		r := &Runner{Core: os.Args[0], OnEvent: func(int, core.Event) {
			mutex.Lock()
			defer mutex.Unlock()
			_, tmpErr := os.Stat(p.Tmp)
			_, outErr := os.Stat(p.Out)
			seen = append(seen, strings.Join([]string{boolText(tmpErr == nil), boolText(errors.Is(outErr, fs.ErrNotExist)), boolText(system.IsHidden(p.Tmp))}, ","))
		}}
		on, state, detail := lastDetail()
		r.OnItem = on
		sum := r.Run(context.Background(), plans, o)
		if sum.Done != 1 || *state != StateOK || *detail != "" {
			t.Fatalf("%s: %+v %s %q", mode, sum, *state, *detail)
		}
		hidden := boolText(runtime.GOOS == "windows")
		if len(seen) == 0 || seen[0] != "true,true,"+hidden {
			t.Errorf("%s: while it is made (apart, Out not there, hidden): %v", mode, seen)
		}
		if _, err := os.Stat(p.Tmp); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: something is left apart: %v", mode, err)
		}
		if _, err := os.Stat(p.Out); err != nil || system.IsHidden(p.Out) {
			t.Errorf("%s: the new one is not in its place, or is hidden: %v", mode, err)
		}
	}
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// What came to Out while the game was made (it was not there when the queue was planned) is never
// replaced: the new one goes, and the game says it is there. An empty folder is not in the way.
func TestWhatCameMeanwhileIsNotReplaced(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, mode := range []Mode{ModeP5z, ModeFolder} {
		dest := t.TempDir()
		o := Options{Mode: mode, Dest: dest}
		plans := PlanCompress([]Item{item("Iron Harbor", "PPSA10002", 10)}, o, newFS())
		os.WriteFile(plans[0].Out, []byte("ajeno"), 0o644)
		on, state, detail := lastDetail()
		(&Runner{Core: os.Args[0], OnItem: on}).Run(context.Background(), plans, o)
		if *state != StateError || *detail != "exists" {
			t.Errorf("%s: %s %q", mode, *state, *detail)
		}
		if data, _ := os.ReadFile(plans[0].Out); string(data) != "ajeno" {
			t.Errorf("%s: what came meanwhile changed: %q", mode, data)
		}
		if _, err := os.Stat(plans[0].Tmp); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: the new one was left apart: %v", mode, err)
		}
	}
	dest := t.TempDir()
	o := Options{Mode: ModeFolder, Dest: dest}
	plans := PlanCompress([]Item{item("Iron Harbor", "PPSA10002", 10)}, o, newFS())
	os.MkdirAll(plans[0].Out, 0o755)
	on, state, detail := lastDetail()
	(&Runner{Core: os.Args[0], OnItem: on}).Run(context.Background(), plans, o)
	if _, err := os.Stat(filepath.Join(plans[0].Out, "eboot.bin")); err != nil || *state != StateOK {
		t.Errorf("into an empty folder: %s %q, %v", *state, *detail, err)
	}
}

// fakeJournal keeps what the Runner notes, as the file of the window would.
type fakeJournal struct {
	mutex sync.Mutex
	paths map[string]bool
	added []string
}

func newJournal() *fakeJournal { return &fakeJournal{paths: map[string]bool{}} }

func (j *fakeJournal) Add(path string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.paths[path] = true
	j.added = append(j.added, path)
}

func (j *fakeJournal) Remove(path string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	delete(j.paths, path)
}

func (j *fakeJournal) left() []string {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	var list []string
	for path := range j.paths {
		list = append(list, path)
	}
	sort.Strings(list)
	return list
}

// The journal has what is apart while it is made, and keeps only what could not be taken away:
// what a crash leaves is found at the next start.
func TestJournalKeepsWhatIsLeft(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	dest := t.TempDir()
	o := Options{Mode: ModeP5z, Dest: dest}
	plans := PlanCompress([]Item{item("Neon Meridian", "PPSA10003", 10)}, o, newFS())
	journal := newJournal()
	var during []string
	r := &Runner{Core: os.Args[0], Journal: journal, OnEvent: func(int, core.Event) { during = journal.left() }}
	if sum := r.Run(context.Background(), plans, o); sum.Done != 1 {
		t.Fatalf("%+v", sum)
	}
	if len(during) != 1 || during[0] != plans[0].Tmp {
		t.Errorf("while it is made: %v", during)
	}
	if left := journal.left(); len(left) != 0 {
		t.Errorf("done, and the journal keeps %v", left)
	}

	t.Setenv(fakecore.Env, "writes-stubborn")
	shortGrace(t)
	failingRemove(t, ".maxpack-tmp")
	journal = newJournal()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	(&Runner{Core: os.Args[0], Journal: journal, OnEvent: func(int, core.Event) { cancel() }}).Run(ctx, plans, o)
	if left := journal.left(); len(left) != 1 || left[0] != plans[0].Tmp {
		t.Errorf("what could not be taken away is not kept: %v", left)
	}
}

// The journal of the window, in a file: what is still there is found with its size, what went
// from a drive that is there is forgotten, and Clean deletes only what MaxPack made apart.
func TestLeftoversFile(t *testing.T) {
	dir := t.TempDir()
	l := &Leftovers{File: filepath.Join(dir, "settings", "unfinished.json")}
	if found := l.Found(); len(found) != 0 {
		t.Fatalf("without a file: %v", found)
	}
	dest := t.TempDir()
	tmp := filepath.Join(dest, "Starfall Odyssey [PPSA10001].p5z.maxpack-tmp")
	os.WriteFile(tmp, []byte("12345"), 0o644)
	folder := filepath.Join(dest, "homebrew", "PPSA10002.maxpack-tmp")
	os.MkdirAll(folder, 0o755)
	os.WriteFile(filepath.Join(folder, "eboot.bin"), []byte("123"), 0o644)
	old := filepath.Join(dest, "Iron Harbor [PPSA10002].p5z.maxpack-old")
	os.WriteFile(old, []byte("1"), 0o644)
	gone := filepath.Join(dest, "gone.p5z.maxpack-tmp")
	away := filepath.Join(dest, "unplugged", "PPSA10003.maxpack-tmp")
	mine := filepath.Join(dest, "my game.p5z")
	os.WriteFile(mine, []byte("mine"), 0o644)
	for _, path := range []string{tmp, folder, old, gone, away, mine} {
		l.Add(path)
	}
	l.Add(tmp)
	found := l.Found()
	sizes := map[string]uint64{}
	for _, f := range found {
		sizes[f.Path] = f.Size
	}
	if len(found) != 3 || sizes[tmp] != 5 || sizes[folder] != 3 || sizes[old] != 1 {
		t.Fatalf("found: %+v", found)
	}
	// what is not there from a drive that is there is forgotten; a folder that is not there (a
	// drive unplugged) is kept for another time
	l2 := &Leftovers{File: l.File}
	if got := l2.paths(); !hasArg(got, away) || hasArg(got, gone) || hasArg(got, mine) {
		t.Errorf("kept: %v", got)
	}
	if failed := l.Clean(); len(failed) != 0 {
		t.Fatalf("Clean: %v", failed)
	}
	for _, path := range []string{tmp, folder, old} {
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is still there: %v", path, err)
		}
	}
	if data, _ := os.ReadFile(mine); string(data) != "mine" {
		t.Error("Clean deleted what MaxPack did not make apart")
	}
	if got := l.paths(); len(got) != 1 || got[0] != away {
		t.Errorf("after Clean the journal has %v", got)
	}
	l.Remove(away)
	if got := l.paths(); len(got) != 0 {
		t.Errorf("after Remove: %v", got)
	}
}

// fakeBin stands in for the recycle bin: what went to it, what was deleted for good, and the
// drives it says have none.
type fakeBin struct {
	trashed, deleted []string
	why              string // the reason of trash.Unavailable for every path, "" when it takes them
}

func useBin(t *testing.T, bin *fakeBin) {
	t.Helper()
	oldTrash, oldRemove := moveToTrash, removeForGood
	moveToTrash = func(path string) error {
		if bin.why != "" {
			return &trash.Unavailable{Reason: bin.why, Drive: filepath.VolumeName(path) + `\`}
		}
		bin.trashed = append(bin.trashed, path)
		return os.RemoveAll(path)
	}
	removeForGood = func(path string) error {
		bin.deleted = append(bin.deleted, path)
		return os.RemoveAll(path)
	}
	t.Cleanup(func() { moveToTrash, removeForGood = oldTrash, oldRemove })
}

// a dump to compress, with its files (10 bytes)
func source(t *testing.T, title, id string) Item {
	t.Helper()
	it := item(title, id, 10)
	it.Path = filepath.Join(t.TempDir(), title)
	os.MkdirAll(filepath.Join(it.Path, "sce_sys"), 0o755)
	os.WriteFile(filepath.Join(it.Path, "sce_sys", "param.json"), []byte("0123456789"), 0o644)
	return it
}

// The original goes only once its new one is verified and in its place: to the recycle bin, or
// for good when asked; where there is no bin it stays, unless the user said to delete those for
// good. Each is told in its detail, counted, and logged.
func TestOriginalGoesOnceChecked(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, c := range []struct {
		originals string
		noBin     string
		delete    bool
		note      string
		trashed   bool
		deleted   bool
	}{
		{"", "", false, "", false, false},
		{OriginalsTrash, "", false, "original_trashed", true, false},
		{OriginalsTrash, trash.TooBig, false, "original_kept_too_big", false, false},
		{OriginalsTrash, trash.NoBin, false, "original_kept_no_bin", false, false},
		{OriginalsTrash, trash.TooLong, true, "original_deleted", false, true},
		{OriginalsDelete, "", false, "original_deleted", false, true},
	} {
		bin := &fakeBin{why: c.noBin}
		useBin(t, bin)
		it := source(t, "Starfall Odyssey", "PPSA10001")
		o := Options{Mode: ModeP5z, Dest: t.TempDir(), Originals: c.originals, OriginalsNoBin: c.delete}
		plans := PlanCompress([]Item{it}, o, newFS())
		var log []string
		on, state, detail := lastDetail()
		var atRemoval bool
		removeAt := moveToTrash
		moveToTrash = func(path string) error {
			_, err := os.Stat(plans[0].Out)
			atRemoval = err == nil
			return removeAt(path)
		}
		sum := (&Runner{Core: os.Args[0], OnItem: on, Log: func(line string) { log = append(log, line) }}).Run(context.Background(), plans, o)
		say := c.originals + "/" + c.noBin + ": "
		if sum.Done != 1 || *state != StateOK || *detail != c.note {
			t.Errorf(say+"%+v %s %q", sum, *state, *detail)
		}
		_, err := os.Stat(it.Path)
		if gone := errors.Is(err, fs.ErrNotExist); gone != (c.trashed || c.deleted) {
			t.Errorf(say+"the original gone: %v", gone)
		}
		if c.trashed && (len(bin.trashed) != 1 || !atRemoval || sum.Trashed != 1 || sum.TrashedSize != 10) {
			t.Errorf(say+"to the bin: %v, new one in place %v, %+v", bin.trashed, atRemoval, sum)
		}
		if c.deleted && (len(bin.deleted) != 1 || sum.Deleted != 1 || sum.DeletedSize != 10) {
			t.Errorf(say+"deleted: %v, %+v", bin.deleted, sum)
		}
		if strings.HasPrefix(c.note, "original_kept") && sum.Kept != 1 {
			t.Errorf(say+"kept: %+v", sum)
		}
		if c.originals != "" && (len(log) != 1 || !strings.Contains(log[0], it.Path) || !strings.Contains(log[0], plans[0].Out)) {
			t.Errorf(say+"log: %q", log)
		}
		if c.originals == "" && len(log) != 0 {
			t.Errorf(say+"logged with nothing removed: %q", log)
		}
	}
}

// The original never goes when the new one is not whole and in its place: a verification that
// fails, a cancel, something that came to its place meanwhile; nor when the queue does not
// compress.
func TestOriginalStaysWhenNotDone(t *testing.T) {
	bin := &fakeBin{}
	useBin(t, bin)
	check := func(say string, it Item) {
		t.Helper()
		if _, err := os.Stat(filepath.Join(it.Path, "sce_sys", "param.json")); err != nil {
			t.Errorf("%s: the original went: %v", say, err)
		}
	}
	t.Setenv(fakecore.Env, "bad-verify")
	it := source(t, "Starfall Odyssey", "PPSA10001")
	o := Options{Mode: ModeFolder, Dest: t.TempDir(), Originals: OriginalsDelete}
	on, state, detail := lastDetail()
	(&Runner{Core: os.Args[0], OnItem: on}).Run(context.Background(), PlanCompress([]Item{it}, o, newFS()), o)
	if *state != StateError || !strings.HasPrefix(*detail, "verify_failed") {
		t.Errorf("verification: %s %q", *state, *detail)
	}
	check("verification failed", it)

	t.Setenv(fakecore.Env, "writes")
	plans := PlanCompress([]Item{it}, o, newFS())
	os.WriteFile(plans[0].Out, []byte("ajeno"), 0o644)
	(&Runner{Core: os.Args[0]}).Run(context.Background(), plans, o)
	check("something came meanwhile", it)

	t.Setenv(fakecore.Env, "writes-stubborn")
	shortGrace(t)
	runCancelled(t, PlanCompress([]Item{it}, o, newFS()), o)
	check("cancelled", it)

	t.Setenv(fakecore.Env, "writes")
	packed := it
	packed.Kind = "p5z"
	for _, mode := range []Mode{ModeOriginal, ModeConsole} {
		od := Options{Mode: mode, Dest: t.TempDir(), Originals: OriginalsDelete}
		(&Runner{Core: os.Args[0]}).Run(context.Background(), PlanDecompress([]Item{packed}, od, newFS()), od)
		check(string(mode), it)
	}
	ov := Options{Mode: ModeVerify, Originals: OriginalsDelete}
	(&Runner{Core: os.Args[0]}).Run(context.Background(), PlanVerify([]Item{packed}), ov)
	check("verify", it)
	if len(bin.trashed)+len(bin.deleted) != 0 {
		t.Errorf("removed: %v %v", bin.trashed, bin.deleted)
	}
}

// The space of a queue, by drive: what the games that go may take (no more than their size),
// against what is free; only the drives where it does not fit are told.
func TestSpaceOfTheQueue(t *testing.T) {
	dest := t.TempDir()
	f := fakeFS{free: 1000 * gib, typ: "NTFS", roots: []string{dest}}
	a, b := item("Starfall Odyssey", "PPSA10001", 600*gib), item("Iron Harbor", "PPSA10002", 500*gib)
	o := Options{Mode: ModeP5z, Dest: dest}
	short := Space(PlanCompress([]Item{a, b}, o, f), o, f)
	if len(short) != 1 || short[0].Need != 1100*gib || short[0].Free != 1000*gib || !same(short[0].Root, dest) {
		t.Fatalf("%+v", short)
	}
	if short := Space(PlanCompress([]Item{a}, o, f), o, f); len(short) != 0 {
		t.Errorf("one fits: %+v", short)
	}
	// a game that cannot go does not count
	plans := PlanCompress([]Item{a, b}, o, f)
	plans[1].Block = "exists"
	if short := Space(plans, o, f); len(short) != 0 {
		t.Errorf("with one skipped: %+v", short)
	}
	if short := Space(PlanVerify([]Item{a, b}), Options{Mode: ModeVerify}, f); len(short) != 0 {
		t.Errorf("verify writes nothing: %+v", short)
	}
}

// A swap checks the old one once it is set aside (a rename keeps its size and date): another file there by then
// goes back to its name, and nothing is replaced (M5).
func TestSwapChecksTheOld(t *testing.T) {
	dir := t.TempDir()
	out, tmp := filepath.Join(dir, "g.p5z"), filepath.Join(dir, "g.p5z"+TmpSuffix)
	os.WriteFile(out, []byte("the user's"), 0o644)
	os.WriteFile(tmp, []byte("the new one"), 0o644)
	if _, err := swapIf(out, tmp, func(string) bool { return false }); !errors.Is(err, errNotTheOne) {
		t.Fatalf("%v", err)
	}
	if data, _ := os.ReadFile(out); string(data) != "the user's" {
		t.Errorf("out: %q", data)
	}
	if _, err := os.Stat(out + OldSuffix); err == nil {
		t.Error("the old one is left apart")
	}
	if _, err := swapIf(out, tmp, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(out); string(data) != "the new one" {
		t.Errorf("swapped: %q", data)
	}
}

// What a power cut between the two renames of a swap leaves: the new one apart and the old one apart, and nothing
// at its name. Clean puts the old one back (it was journaled before the swap), not away (M5).
func TestCleanPutsTheOldBack(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "g.p5z")
	os.WriteFile(out+OldSuffix, []byte("the one it had"), 0o644)
	os.WriteFile(out+TmpSuffix, []byte("half of the new one"), 0o644)
	l := &Leftovers{File: filepath.Join(t.TempDir(), "leftovers.json")}
	l.Add(out + OldSuffix)
	l.Add(out + TmpSuffix)
	found := l.Found()
	if len(found) != 2 || found[0].Restore != out {
		t.Fatalf("%+v", found)
	}
	if failed := l.Clean(); len(failed) != 0 {
		t.Fatal(failed)
	}
	if data, _ := os.ReadFile(out); string(data) != "the one it had" {
		t.Errorf("put back: %q", data)
	}
	if _, err := os.Stat(out + TmpSuffix); err == nil {
		t.Error("the half-made one stays")
	}
	if len(l.Found()) != 0 {
		t.Error("still kept")
	}
}
