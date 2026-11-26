// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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

// madeThenChanged: the watched folder compressed the game well (its .p5z at out, of version made), then the game
// changed (change does it) and is handed over again.
func madeThenChanged(t *testing.T, w *watch.Watcher, it jobs.Item, out, made string, change func()) {
	t.Helper()
	os.WriteFile(out, []byte("what the watched folder made"), 0o644)
	w.EndedAs(it.Path, watch.Ending{Final: true, Out: out, TitleID: it.TitleID, Version: made})
	change()
	if found, err := w.Poll(); err != nil || len(found) != 1 {
		t.Fatalf("not handed over again: %v %v", found, err)
	}
}

// more adds a file to the game: its count and size change, as a copy that stalled and went on does.
func more(t *testing.T, it jobs.Item, name string) func() {
	return func() { touch(t, it.Path, name, 1000) }
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

// A game the watched folder compressed well and that grew afterwards (its copy had stalled): while the destination
// still has the very .p5z the watched folder made of it, of this same game in this same version, the plan says it
// is made again in its place (WatchRedo, and the size and date that file must still have); another file there,
// or a game dropped by hand, it is not.
func TestWatchRedoPlan(t *testing.T) {
	useSettings(t, t.TempDir())
	infoOf(t, map[string]map[string]any{})
	a := &App{core: os.Args[0], fs: realFS{}}
	it, out, w := watchedGame(t, a, "01.000.000")
	madeThenChanged(t, w, it, out, "01.000.000", more(t, it, "late.bin"))
	choices := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: filepath.Dir(out)}}
	views, err := a.Plan("compress", []jobs.Item{it}, choices)
	if err != nil || !views[0].WatchRedo || views[0].ReplaceIf == nil || len(views[0].Replace) != 1 || views[0].WatchSkip != "" {
		t.Fatalf("%+v %v", views, err)
	}
	// Start plans it again: the Runner gets what the file must still be, and the window's record agrees
	plans, _, _ := a.plans("compress", []jobs.Item{it}, choices)
	if plans[0].ReplaceIf == nil || !plans[0].ReplaceIf.Matches(out) || a.redoStill(plans[0]) != "" {
		t.Errorf("start: %+v %q", plans[0], a.redoStill(plans[0]))
	}
	// and what the game was when it was handed over, which it must still be when its turn comes
	if sig, _, _ := watch.Signature(it.Path); plans[0].HandedSig == "" || plans[0].HandedSig != sig {
		t.Errorf("handed over as %q, now %q", plans[0].HandedSig, sig)
	}
	dropped := it
	dropped.Watched = false
	if views, _ := a.Plan("compress", []jobs.Item{dropped}, choices); views[0].WatchRedo || views[0].ReplaceIf != nil {
		t.Errorf("dropped: %+v", views[0])
	}
	time.Sleep(20 * time.Millisecond)
	os.WriteFile(out, []byte("the user's own .p5z"), 0o644)
	if views, _ := a.Plan("compress", []jobs.Item{it}, choices); views[0].WatchRedo || views[0].ReplaceIf != nil || views[0].WatchSkip != "watch_exists" {
		t.Errorf("another file: %+v", views[0])
	}
}

// Never a .p5z of another version (C2): a dump that changed to another version in the same folder (an update
// dumped over the old one) is not made again in place of the .p5z made of the old one. Newer, the row says both
// versions and how to update it; older, that the destination has a newer one, and nothing is done (M8).
func TestWatchRedoOnlyTheSameVersion(t *testing.T) {
	useSettings(t, t.TempDir())
	infoOf(t, map[string]map[string]any{})
	for _, c := range []struct{ made, now, skip string }{
		{"01.000.000", "01.002.000", "watch_other_version: 01.000.000 01.002.000"},
		{"01.002.000", "01.000.000", "watch_older_version: 01.002.000 01.000.000"},
	} {
		a := &App{core: os.Args[0], fs: realFS{}}
		it, out, w := watchedGame(t, a, c.now)
		madeThenChanged(t, w, it, out, c.made, more(t, it, "update.bin"))
		views, err := a.Plan("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: filepath.Dir(out)}})
		if err != nil || views[0].WatchRedo || views[0].ReplaceIf != nil || views[0].WatchSkip != c.skip {
			t.Errorf("%s over %s: %+v %v", c.now, c.made, views[0], err)
		}
	}
}

// Redos are limited (M1): a change of dates alone (the same files, the same size) is no reason to compress again,
// and after 2 redos in a row a game is not made again by itself, until the user compresses it.
func TestWatchRedoLimits(t *testing.T) {
	useSettings(t, t.TempDir())
	infoOf(t, map[string]map[string]any{})
	a := &App{core: os.Args[0], fs: realFS{}}
	it, out, w := watchedGame(t, a, "01.000.000")
	madeThenChanged(t, w, it, out, "01.000.000", func() {
		later := time.Now().Add(time.Hour)
		os.Chtimes(filepath.Join(it.Path, "sce_sys", "param.json"), later, later)
	})
	choices := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: filepath.Dir(out)}}
	if views, _ := a.Plan("compress", []jobs.Item{it}, choices); views[0].WatchRedo || views[0].WatchSkip != "watch_dates_only" {
		t.Errorf("dates only: %+v", views[0])
	}
	for n := 1; n <= 3; n++ {
		w.EndedAs(it.Path, watch.Ending{Final: true, Out: out, TitleID: it.TitleID, Version: it.Version, Redone: n > 1})
		touch(t, it.Path, "more"+string(rune('0'+n))+".bin", 1000*n)
		if found, _ := w.Poll(); len(found) != 1 {
			t.Fatalf("not handed over: %v", found)
		}
		views, _ := a.Plan("compress", []jobs.Item{it}, choices)
		if want := n <= 2; views[0].WatchRedo != want || (!want && views[0].WatchSkip != "watch_redo_limit") {
			t.Errorf("after %d redos: %+v", n-1, views[0])
		}
	}
}

// Fail closed (I1): the page asks for the redo apart (Choices.Redo), and the window grants it only when its own
// plan, made again at Start, still says so. The .p5z replaced by the user between the page's plan and Start
// stays: the game is skipped, nothing compressed. One the user said to replace by hand is the user's replace.
func TestWatchRedoConsent(t *testing.T) {
	useSettings(t, t.TempDir())
	t.Setenv(fakecore.Env, "writes")
	events := newPageEvents()
	a := pageApp(os.Args[0], events)
	it, out, w := watchedGame(t, a, "01.000.000")
	madeThenChanged(t, w, it, out, "01.000.000", more(t, it, "late.bin"))
	views, _ := a.Plan("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: filepath.Dir(out)}})
	if !views[0].WatchRedo {
		t.Fatalf("%+v", views[0])
	}
	time.Sleep(20 * time.Millisecond)
	os.WriteFile(out, []byte("the user's own .p5z"), 0o644)
	if err := a.Start("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: filepath.Dir(out)}, Redo: []string{out}}); err != nil {
		t.Fatal(err)
	}
	events.wait(t)
	if last := events.last(0); last.State != jobs.StateSkipped || last.Detail != "watch_redo_refused" {
		t.Errorf("%+v", last)
	}
	if data, _ := os.ReadFile(out); string(data) != "the user's own .p5z" {
		t.Errorf("replaced: %q", data)
	}
	// with no watcher at all (turned off meanwhile), the Runner's own look refuses it too
	a.watch = nil
	if why := a.redoStill(jobs.Plan{Item: it, Out: out, ReplaceIf: &jobs.Fingerprint{}}); why != "watch_redo_refused" {
		t.Errorf("no watcher: %q", why)
	}
}

// What the watched folder ended well keeps the title ID and version of the .p5z it made, as that file says them
// (info --json), not as the page read the game when it was planned; one that cannot be read keeps none, and is
// never made again by itself.
func TestWatchEndedKeepsTheGame(t *testing.T) {
	useSettings(t, t.TempDir())
	infoOf(t, map[string]map[string]any{"Copper Vale [PPSA10001].p5z": {"kind": "p5z", "title_id": "PPSA10001", "version": "01.005.000"}})
	a := &App{core: os.Args[0]}
	it, out, w := watchedGame(t, a, "01.000.000")
	os.WriteFile(out, []byte("made"), 0o644)
	a.WatchEnded(it.Path, true, out, "PPSA10001", "01.000.000", false)
	if made, ok := w.MadeOf(it.Path); !ok || made.TitleID != "PPSA10001" || made.Version != "01.005.000" || made.Redos != 0 {
		t.Errorf("%+v %v", made, ok)
	}
	b := &App{core: os.Args[0]}
	other, out2, w2 := watchedGame(t, b, "01.000.000")
	unreadable := filepath.Join(filepath.Dir(out2), "Other [PPSA10002].p5z")
	os.WriteFile(unreadable, []byte("made"), 0o644)
	b.WatchEnded(other.Path, true, unreadable, "PPSA10002", "01.000.000", false)
	if made, ok := w2.MadeOf(other.Path); !ok || made.TitleID != "" || made.Version != "" {
		t.Errorf("unreadable: %+v %v", made, ok)
	}
}

// No redo when its version is not known on either side (a param.json without one), nor when only the date of a
// game of one file (an image, a package) changed.
func TestWatchRedoNeedsAKnownVersionAndAChange(t *testing.T) {
	useSettings(t, t.TempDir())
	infoOf(t, map[string]map[string]any{})
	a := &App{core: os.Args[0], fs: realFS{}}
	it, out, w := watchedGame(t, a, "")
	madeThenChanged(t, w, it, out, "", more(t, it, "late.bin"))
	choices := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: filepath.Dir(out)}}
	if views, _ := a.Plan("compress", []jobs.Item{it}, choices); views[0].WatchRedo || views[0].WatchSkip != "watch_exists" {
		t.Errorf("no version: %+v", views[0])
	}
	// an image: one file
	folder, dest := t.TempDir(), t.TempDir()
	image := touch(t, folder, "Copper Vale.exfat", 4096)
	img := jobs.Item{Path: image, Kind: "exfat", Name: "Copper Vale", TitleID: "PPSA10001", Version: "01.000.000", OriginalSize: 4096,
		HasZstdRuntime: true, Watched: true}
	imgOut := jobs.PlanCompress([]jobs.Item{img}, jobs.Options{Mode: jobs.ModeP5z, Dest: dest}, realFS{})[0].Out
	iw := &watch.Watcher{Folder: folder}
	if found, _ := iw.Poll(); len(found) != 1 {
		t.Fatalf("not handed over: %v", found)
	}
	b := &App{core: os.Args[0], fs: realFS{}}
	b.watch = &watching{stop: make(chan struct{}), folder: folder, watcher: iw}
	madeThenChanged(t, iw, img, imgOut, "01.000.000", func() {
		later := time.Now().Add(time.Hour)
		os.Chtimes(image, later, later)
	})
	if views, _ := b.Plan("compress", []jobs.Item{img}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest}}); views[0].WatchRedo || views[0].WatchSkip != "watch_dates_only" {
		t.Errorf("an image whose date changed: %+v", views[0])
	}
}

// Once stopWatch returns, the watcher at work tells the page nothing more (M2): a stop that comes while it tells
// what it found waits for that, and what it found is never told after.
func TestWatchTellsNothingOnceStopped(t *testing.T) {
	useSettings(t, t.TempDir())
	folder := t.TempDir()
	saveSettings(t, func(s *settings.Settings) { s.WatchOn, s.WatchFolder = true, folder })
	var mutex sync.Mutex
	var said []string
	stopped := make(chan struct{})
	a := &App{watchEvery: 10 * time.Millisecond, watchQuiet: 10 * time.Millisecond}
	a.emit = func(event string, data any) {
		mutex.Lock()
		said = append(said, event)
		mutex.Unlock()
		if event == "watch:status" {
			go func() {
				a.stopWatch()
				mutex.Lock()
				said = append(said, "stopped")
				mutex.Unlock()
				close(stopped)
			}()
			time.Sleep(100 * time.Millisecond) // the stop comes while the rest is told
		}
	}
	a.watchPoll = func(w *watch.Watcher) ([]string, error) { return []string{filepath.Join(folder, "Game")}, nil }
	a.StartWatch()
	<-stopped
	a.watchLoops.Wait()
	mutex.Lock()
	defer mutex.Unlock()
	at := slices.Index(said, "stopped")
	if slices.Contains(said[at+1:], "watch:found") {
		t.Errorf("told after it was stopped: %v", said)
	}
}

// A game a program keeps writing into (a file of it open for writing, as a copy holds it) waits, and once it has
// waited a while the watched folder's status says which one (M9).
func TestWatchStatusSaysWhoWrites(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS has no look at the programs that write a file")
	}
	useSettings(t, t.TempDir())
	folder := t.TempDir()
	saveSettings(t, func(s *settings.Settings) { s.WatchOn, s.WatchFolder = true, folder })
	events := &watchEvents{}
	a := watchApp(events)
	a.watchWritingAfter = time.Millisecond
	a.StartWatch()
	defer a.stopWatchWait()
	time.Sleep(100 * time.Millisecond)
	game := makeDump(t, folder, "Copying", "PPSA10001", "Copying")
	f, err := os.OpenFile(filepath.Join(game, "eboot.bin"), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	waitUntil(t, "the status never said who writes", func() bool { return strings.Contains(events.lastStatus().Writing, "Copying") })
	f.Close()
	waitUntil(t, "the status still says it once let go", func() bool { return events.lastStatus().Writing == "" })
}

// What the watched folder ended well is kept in watched.json with the rest, and a new run has it.
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
	infoOf(t, map[string]map[string]any{"Ran [PPSA10001].p5z": {"kind": "p5z", "title_id": "PPSA10001", "version": "01.000.000"}})
	a.core = os.Args[0]
	a.WatchEnded(game, true, out, "PPSA10001", "01.000.000", false)
	a.stopWatchWait()
	b := watchApp(&watchEvents{})
	b.StartWatch()
	defer b.stopWatchWait()
	made, ok := b.watcher().watcher.MadeOf(game)
	if !ok || made.Out != out || !made.Matches() || made.Version != "01.000.000" {
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

// A .p5z in the destination that the watched folder did not make, of the same game in another version: the plan
// says it with both versions (read with info --json), newer or older; the same version says nothing more.
func TestWatchOtherVersionNotMade(t *testing.T) {
	useSettings(t, t.TempDir())
	infoOf(t, map[string]map[string]any{"Copper Vale [PPSA10001].p5z": {"kind": "p5z", "title_id": "PPSA10001", "version": "01.000.000"}})
	for version, skip := range map[string]string{"01.002.000": "watch_other_version: 01.000.000 01.002.000",
		"00.900.000": "watch_older_version: 01.000.000 00.900.000", "01.000.000": "watch_exists"} {
		a := &App{core: os.Args[0], fs: realFS{}}
		it, out, _ := watchedGame(t, a, version)
		os.WriteFile(out, []byte("a .p5z of the game"), 0o644)
		views, err := a.Plan("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: filepath.Dir(out)}})
		if err != nil || views[0].WatchSkip != skip || views[0].WatchRedo {
			t.Errorf("%s: %+v %v", version, views[0], err)
		}
	}
}
