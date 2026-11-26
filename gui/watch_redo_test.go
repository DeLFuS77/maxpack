// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/internal/watch"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// watchedGame is a dump in a watched folder, handed over by its watcher (as the App has it), with the item the
// page sends of it and where its .p5z goes in dest.
func watchedGame(t *testing.T, a *App, version string) (it jobs.Item, out string, w *watch.Watcher) {
	t.Helper()
	folder, dest := t.TempDir(), t.TempDir()
	game := makeDump(t, folder, "Copper Vale", "PPSA10001", "Copper Vale")
	it = jobs.Item{Path: game, Kind: "folder", Name: "Copper Vale", TitleID: "PPSA10001", Version: version, OriginalSize: 10,
		HasZstdRuntime: true, Watched: true}
	out = jobs.PlanCompress([]jobs.Item{it}, jobs.Options{Mode: jobs.ModeP5z, Dest: dest}, realFS{})[0].Out
	w = &watch.Watcher{Folder: folder} // no quiet: handed over at its first look
	if found, err := w.Poll(); err != nil || len(found) != 1 {
		t.Fatalf("not handed over: %v %v", found, err)
	}
	a.watch = &watching{stop: make(chan struct{}), folder: folder, watcher: w}
	return it, out, w
}

// infoOf makes the engine of the tests say of each file named what info --json says of it.
func infoOf(t *testing.T, infos map[string]map[string]any) {
	t.Helper()
	data, _ := json.Marshal(infos)
	file := filepath.Join(t.TempDir(), "infos.json")
	os.WriteFile(file, data, 0o644)
	t.Setenv(fakecore.Env, "info")
	t.Setenv(fakecore.InfoEnv, file)
}

// A game the watched folder compressed well and that changed afterwards (its copy had stalled): while the
// destination still has the very .p5z the watched folder made of it, the plan says it is made again in its
// place (WatchRedo, and the size and date that file must still have); another file there, it is not.
func TestWatchRedoPlan(t *testing.T) {
	useSettings(t, t.TempDir())
	infoOf(t, map[string]map[string]any{"Copper Vale [PPSA10001].p5z": {"kind": "p5z", "title_id": "PPSA10001", "version": "01.000.000"}})
	a := &App{core: os.Args[0], fs: realFS{}}
	it, out, w := watchedGame(t, a, "01.000.000")
	os.WriteFile(out, []byte("what the watched folder made"), 0o644)
	w.EndedWith(it.Path, true, out)
	choices := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: filepath.Dir(out)}}
	views, err := a.Plan("compress", []jobs.Item{it}, choices)
	if err != nil || !views[0].WatchRedo || views[0].ReplaceIf == nil || len(views[0].Replace) != 1 || views[0].OtherVersion != "" {
		t.Fatalf("%+v %v", views, err)
	}
	// Start plans it again: the Runner gets what the file must still be
	plans, _, _ := a.plans("compress", []jobs.Item{it}, choices)
	if plans[0].ReplaceIf == nil || !plans[0].ReplaceIf.Matches(out) {
		t.Errorf("start: %+v", plans[0])
	}
	// one the user dropped is never made again by itself
	dropped := it
	dropped.Watched = false
	if views, _ := a.Plan("compress", []jobs.Item{dropped}, choices); views[0].WatchRedo || views[0].ReplaceIf != nil {
		t.Errorf("dropped: %+v", views[0])
	}
	// another file there now (the same name): it stays
	time.Sleep(20 * time.Millisecond)
	os.WriteFile(out, []byte("the user's own .p5z"), 0o644)
	if views, _ := a.Plan("compress", []jobs.Item{it}, choices); views[0].WatchRedo || views[0].ReplaceIf != nil {
		t.Errorf("another file: %+v", views[0])
	}
}

// A new version (an update dump) of a game the destination has as a .p5z: the plan says the version there, so
// that the page says both and points to Details › Update with a new dump…; the same version, or another game,
// says nothing.
func TestWatchOtherVersion(t *testing.T) {
	useSettings(t, t.TempDir())
	infoOf(t, map[string]map[string]any{"Copper Vale [PPSA10001].p5z": {"kind": "p5z", "title_id": "PPSA10001", "version": "01.000.000"}})
	a := &App{core: os.Args[0], fs: realFS{}}
	it, out, _ := watchedGame(t, a, "01.002.000")
	os.WriteFile(out, []byte("a .p5z of the game"), 0o644)
	choices := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: filepath.Dir(out)}}
	views, err := a.Plan("compress", []jobs.Item{it}, choices)
	if err != nil || views[0].OtherVersion != "01.000.000" || views[0].WatchRedo {
		t.Fatalf("%+v %v", views, err)
	}
	same := it
	same.Version = "01.000.000"
	other := it
	other.TitleID = "PPSA10002"
	dropped := it
	dropped.Watched = false
	for name, item := range map[string]jobs.Item{"same version": same, "another game": other, "dropped": dropped} {
		if views, _ := a.Plan("compress", []jobs.Item{item}, choices); len(views[0].Replace) > 0 && views[0].OtherVersion != "" {
			t.Errorf("%s: %+v", name, views[0])
		}
	}
}

// What was made of each game that ended well is kept in watched.json with the rest, and a new run has it.
func TestWatchMadeKept(t *testing.T) {
	useSettings(t, t.TempDir())
	folder := t.TempDir()
	saveSettings(t, func(s *settings.Settings) { s.WatchOn, s.WatchFolder = true, folder })
	events := &watchEvents{}
	a := watchApp(events)
	a.StartWatch()
	time.Sleep(100 * time.Millisecond)
	game := makeDump(t, folder, "Ran", "PPSA10001", "Ran")
	if found := events.waitFound(t, 3*time.Second); len(found) != 1 {
		a.stopWatchWait()
		t.Fatalf("found: %v", found)
	}
	out := touch(t, t.TempDir(), "Ran [PPSA10001].p5z", 10)
	a.WatchEnded(game, true, out)
	a.stopWatchWait()
	b := watchApp(&watchEvents{})
	b.StartWatch()
	defer b.stopWatchWait()
	made, ok := b.watcher().watcher.MadeOf(game)
	if !ok || made.Out != out || !made.Matches() {
		t.Errorf("a new run: %+v %v", made, ok)
	}
}

// What a queue puts in the watched folder (Decompress there, a folder for the console made there) is not new to
// it: never handed over, as it is when it is put there.
func TestWatchOwnOutputs(t *testing.T) {
	useSettings(t, t.TempDir())
	folder := t.TempDir()
	a := watchApp(&watchEvents{})
	w := &watch.Watcher{Folder: folder}
	w.Poll()
	a.watch = &watching{stop: make(chan struct{}), folder: folder, watcher: w}
	out := makeDump(t, folder, "Unpacked", "PPSA10001", "Unpacked")
	page := &teller{app: a, plans: jobs.NewQueue([]jobs.Plan{{Out: out}}), mode: jobs.ModeOriginal, drives: realFS{}}
	page.item(0, jobs.StateOK, "")
	if found, _ := w.Poll(); len(found) != 0 {
		t.Errorf("what Decompress put there is new: %v", found)
	}
}
