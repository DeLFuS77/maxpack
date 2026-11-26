// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/internal/trash"
	"github.com/DeLFuS77/maxpack/gui/internal/watch"
)

// What the watched folder brings keeps its original, always: it is only compressed, whatever the queue it joined
// does with the others and whatever Settings says; its detail says so.
func TestWatchedOriginalsStay(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, c := range []struct {
		originals string
		noBin     bool
	}{{OriginalsDelete, true}, {OriginalsTrash, false}, {OriginalsKeep, false}} {
		bin := &fakeBin{}
		useBin(t, bin)
		it := source(t, "Starfall Odyssey", "PPSA10001")
		it.Watched = true
		o := Options{Mode: ModeP5z, Dest: t.TempDir(), Originals: c.originals, OriginalsNoBin: c.noBin}
		var state, detail string
		sum := (&Runner{Core: os.Args[0], OnItem: func(i int, s, d string) { state, detail = s, d }}).Run(context.Background(), PlanCompress([]Item{it}, o, newFS()), o)
		if state != StateOK || detail != "original_kept_watched" || sum.Done != 1 || sum.Trashed+sum.Deleted != 0 {
			t.Errorf("%s: %s %q %+v", c.originals, state, detail, sum)
		}
		if len(bin.trashed)+len(bin.deleted) != 0 {
			t.Errorf("%s: the original went: %v %v", c.originals, bin.trashed, bin.deleted)
		}
		if _, err := os.Stat(it.Path); err != nil {
			t.Errorf("%s: the original is gone", c.originals)
		}
	}
	// one the user dropped follows the queue, as before
	bin := &fakeBin{}
	useBin(t, bin)
	it := source(t, "Starfall Odyssey", "PPSA10001")
	o := Options{Mode: ModeP5z, Dest: t.TempDir(), Originals: OriginalsDelete}
	var detail string
	(&Runner{Core: os.Args[0], OnItem: func(i int, s, d string) { detail = d }}).Run(context.Background(), PlanCompress([]Item{it}, o, newFS()), o)
	if !strings.Contains(detail, "original_deleted") || len(bin.deleted) != 1 {
		t.Errorf("dropped: %q %v", detail, bin.deleted)
	}
}

// A watched game whose files changed while it was compressed (it was still arriving) fails: what was made is
// taken away, nothing takes its name, and the watched folder brings it again once it is whole.
func TestWatchedChangedWhileCompressed(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	it := source(t, "Starfall Odyssey", "PPSA10001")
	it.Watched = true
	dest := t.TempDir()
	o := Options{Mode: ModeP5z, Dest: dest}
	plans := PlanCompress([]Item{it}, o, newFS())
	var state, detail string
	onItem := func(i int, s, d string) {
		state, detail = s, d
		if s == StateVerifying { // a file more arrives while it is compressed
			os.WriteFile(filepath.Join(it.Path, "late.bin"), []byte("late"), 0o644)
		}
	}
	sum := (&Runner{Core: os.Args[0], OnItem: onItem}).Run(context.Background(), plans, o)
	if state != StateError || detail != "source_changed" || sum.Failed != 1 {
		t.Errorf("%s %q %+v", state, detail, sum)
	}
	if _, err := os.Stat(plans[0].Out); err == nil {
		t.Error("what was made took its name")
	}
	if _, err := os.Stat(plans[0].Out + TmpSuffix); err == nil {
		t.Error("what was made is left apart")
	}
	if _, err := os.Stat(it.Path); err != nil {
		t.Error("the original is gone")
	}
	// one the user dropped is not checked: as before
	plain := source(t, "Starfall Odyssey", "PPSA10001")
	plans = PlanCompress([]Item{plain}, Options{Mode: ModeP5z, Dest: t.TempDir()}, newFS())
	onItem = func(i int, s, d string) {
		state = s
		if s == StateVerifying {
			os.WriteFile(filepath.Join(plain.Path, "late.bin"), []byte("late"), 0o644)
		}
	}
	(&Runner{Core: os.Args[0], OnItem: onItem}).Run(context.Background(), plans, Options{Mode: ModeP5z, Dest: filepath.Dir(plans[0].Out)})
	if state != StateOK {
		t.Errorf("a dropped game: %s", state)
	}
}

// The .p5z the watched folder made of a game that changed afterwards (its copy had stalled, and ended later) is
// made again in its place, and only while it is still that very file (ReplaceIf, its size and date): the row
// says so. Another file there by then (the user's) stays, and the new one goes.
func TestWatchedMadeAgain(t *testing.T) {
	for _, changed := range []bool{false, true} {
		useBin(t, &fakeBin{})
		p, o := madeAgain(t, "01.000.000", "01.000.000")
		var state, detail string
		onItem := func(i int, s, d string) {
			state, detail = s, d
			if s == StateVerifying && changed { // the user puts a file of their own there meanwhile
				os.WriteFile(p.Out, []byte("the user's own .p5z, put there meanwhile"), 0o644)
			}
		}
		redo := func(p Plan) string { return "" } // the window's record says it is still the one made
		(&Runner{Core: os.Args[0], OnItem: onItem, Redo: redo}).Run(context.Background(), []Plan{p}, o)
		data, _ := os.ReadFile(p.Out)
		_, leftover := os.Stat(p.Out + TmpSuffix)
		_, old := os.Stat(p.Out + OldSuffix)
		switch {
		case !changed && (state != StateOK || !hasPart(detail, "watch_redone") || !strings.HasPrefix(string(data), "MAXPKP5Z")):
			t.Errorf("made again: %s %q %q", state, detail, data[:min(len(data), 20)])
		case changed && (state != StateSkipped || detail != "watch_redo_refused" || string(data) != "the user's own .p5z, put there meanwhile"):
			t.Errorf("another file there: %s %q %q", state, detail, data)
		case leftover == nil || old == nil:
			t.Error("what was made, or the old one, is left apart")
		}
	}
}

// Fail closed: a redo is decided again from the window's record and the file on disk before anything is made, and
// again right before the swap. Without the record (the watched folder turned off or changed, the record gone), or
// with no one to ask, nothing is replaced: the game is skipped, the .p5z there stays, and nothing is made at all
// when that is known before.
func TestWatchedRedoFailsClosed(t *testing.T) {
	for _, c := range []struct {
		name   string
		redo   func(p Plan) string
		before bool // refused before anything is made
	}{
		{"no one to ask", nil, true},
		{"refused at once", func(Plan) string { return "watch_redo_refused" }, true},
		{"refused before the swap", nil, false},
		{"not what was handed over", func(Plan) string { return "" }, true},
	} {
		useBin(t, &fakeBin{})
		p, o := madeAgain(t, "01.000.000", "01.000.000")
		if c.name == "not what was handed over" {
			p.HandedSig = ""
		}
		asked := 0
		redo := c.redo
		if c.name == "refused before the swap" {
			redo = func(Plan) string { // yes before it is made; the watched folder turned off meanwhile
				if asked++; asked == 1 {
					return ""
				}
				return "watch_redo_refused"
			}
		}
		var states []string
		var detail string
		r := &Runner{Core: os.Args[0], Redo: redo, OnItem: func(i int, s, d string) { states = append(states, s); detail = d }}
		r.Run(context.Background(), []Plan{p}, o)
		data, _ := os.ReadFile(p.Out)
		packed := slices.Contains(states, StatePacking)
		want := "watch_redo_refused"
		if c.name == "not what was handed over" {
			want = "watch_redo_changed"
		}
		if states[len(states)-1] != StateSkipped || detail != want || string(data) != "what the watched folder made before" || packed == c.before {
			t.Errorf("%s: %v %q %q", c.name, states, detail, data)
		}
		if _, err := os.Stat(p.Out + TmpSuffix); err == nil {
			t.Errorf("%s: what was made is left apart", c.name)
		}
	}
}

// madeAgain sets up a redo: a watched game whose destination has the .p5z made of it before (version there), and
// the engine of the tests saying the version of what it makes (made) and of what is there; it gives the plan.
func madeAgain(t *testing.T, there, made string) (Plan, Options) {
	t.Helper()
	it := source(t, "Starfall Odyssey", "PPSA10001")
	it.Watched, it.Version = true, "01.000.000"
	o := Options{Mode: ModeP5z, Dest: t.TempDir()}
	out := PlanCompress([]Item{it}, o, newFS())[0].Out
	os.WriteFile(out, []byte("what the watched folder made before"), 0o644)
	infos, _ := json.Marshal(map[string]map[string]any{
		filepath.Base(out):             {"kind": "p5z", "title_id": "PPSA10001", "version": there},
		filepath.Base(out + TmpSuffix): {"kind": "p5z", "title_id": "PPSA10001", "version": made},
	})
	file := filepath.Join(t.TempDir(), "infos.json")
	os.WriteFile(file, infos, 0o644)
	t.Setenv(fakecore.Env, "writes-info")
	t.Setenv(fakecore.InfoEnv, file)
	plans := PlanCompress([]Item{it}, o, newFS())
	info, _ := os.Stat(out)
	plans[0].ReplaceIf = &Fingerprint{Size: info.Size(), Mtime: info.ModTime().UnixNano()}
	plans[0].HandedSig, _, _ = watch.Signature(it.Path)
	return plans[0], o
}

// Right before the swap, what was made and what it would replace are read (their title ID and version): only when
// both are the game and version planned is the old one replaced, and then it goes to the recycle bin under a dated
// name, never deleted for good; without a bin it is kept beside it under that name, and the row says which. A
// source that is not what was planned when its turn comes is not compressed (it comes again).
func TestRedoReadsWhatWasMade(t *testing.T) {
	for _, c := range []struct {
		name, there, made, bin string
		state, detail          string
	}{
		{"the same version", "01.000.000", "01.000.000", "", StateOK, "watch_redone; watch_old_in_bin"},
		{"made of a newer dump", "01.000.000", "01.005.000", "", StateSkipped, "watch_other_version: 01.000.000 01.005.000"},
		{"the .p5z there grew to a newer one", "01.002.000", "01.000.000", "", StateSkipped, "watch_older_version: 01.002.000 01.000.000"},
		{"no recycle bin", "01.000.000", "01.000.000", trash.NoBin, StateOK, "watch_redone; watch_old_kept"},
	} {
		bin := &fakeBin{why: c.bin}
		useBin(t, bin)
		p, o := madeAgain(t, c.there, c.made)
		var state, detail string
		r := &Runner{Core: os.Args[0], Redo: func(Plan) string { return "" }, OnItem: func(i int, s, d string) { state, detail = s, d }}
		r.Run(context.Background(), []Plan{p}, o)
		data, _ := os.ReadFile(p.Out)
		var kept []string
		entries, _ := os.ReadDir(filepath.Dir(p.Out))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), filepath.Base(p.Out)+OldSuffix+"-") {
				kept = append(kept, filepath.Join(filepath.Dir(p.Out), e.Name()))
			}
		}
		switch {
		case state != c.state || !strings.HasPrefix(detail, c.detail):
			t.Errorf("%s: %s %q", c.name, state, detail)
		case c.state == StateSkipped && string(data) != "what the watched folder made before":
			t.Errorf("%s: replaced: %q", c.name, data)
		case c.state == StateOK && c.bin == "" && (len(bin.trashed) != 1 || !strings.HasPrefix(bin.trashed[0], p.Out+OldSuffix+"-") || len(kept) != 0):
			t.Errorf("%s: not in the bin: %v %v", c.name, bin.trashed, kept)
		case c.state == StateOK && c.bin != "" && (len(kept) != 1 || !strings.Contains(detail, kept[0])):
			t.Errorf("%s: not kept beside it: %v %q", c.name, kept, detail)
		case len(bin.deleted) != 0:
			t.Errorf("%s: deleted for good: %v", c.name, bin.deleted)
		}
		if _, err := os.Stat(p.Out + TmpSuffix); err == nil {
			t.Errorf("%s: what was made is left apart", c.name)
		}
	}
	// the source changed after it was planned: not compressed now, and not final (it comes again)
	useBin(t, &fakeBin{})
	p, o := madeAgain(t, "01.000.000", "01.000.000")
	os.WriteFile(filepath.Join(p.Item.Path, "late.bin"), []byte("late"), 0o644)
	var states []string
	var detail string
	(&Runner{Core: os.Args[0], Redo: func(Plan) string { return "" }, OnItem: func(i int, s, d string) { states = append(states, s); detail = d }}).Run(context.Background(), []Plan{p}, o)
	if slices.Contains(states, StatePacking) || states[len(states)-1] != StateSkipped || detail != "watch_redo_changed" {
		t.Errorf("changed since it was planned: %v %q", states, detail)
	}
}
