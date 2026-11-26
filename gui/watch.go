// SPDX-License-Identifier: GPL-3.0-or-later

package main

// The watched folder (Settings › Watched folder): while the window is open (minimized too), one folder is
// looked at every few seconds, and what arrives in it, once it has stopped changing for a minute, goes to
// the page ("watch:found"), which compresses it as if it had been dropped on Compress (see app.js). Turning it
// on (or choosing another folder) takes a snapshot: what is there then is not new, and the page offers to
// compress it ("Compress the N already there", WatchTakeExisting). What the page says ended (WatchEnded: done
// or failed, and the .p5z made of what ended well) is kept in watched.json next to the settings, so a game is
// not handed over again as it is; what never ran is handed again in the next run. A game that ended well and
// changed afterwards (its copy had stalled) is made again in place of its .p5z while that is still the very file
// the watched folder made (watchRedo); what the window itself puts in the folder is never new (watchOwn).
// Nothing runs with the window closed: what arrived meanwhile is found when it opens.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/watch"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// How often the folder is looked at, and how long what is new must not change.
const (
	watchEvery = 5 * time.Second
	watchQuiet = time.Minute
)

// watchedFile keeps, of the folder watched, what ended and what was there when it was turned on.
const watchedFile = "watched.json"

// WatchStatus is what the page is told of the watched folder ("watch:status", and StartWatch): the folder
// watched ("" when none is), why it cannot be looked at ("gone": not there, or not a folder; "unreadable"; ""
// when it can), how many games were there when it was turned on and have not been compressed, and the names of
// those that have waited a while for a program that keeps writing into them (", " between them).
type WatchStatus struct {
	Folder   string `json:"folder"`
	Error    string `json:"error"`
	Existing int    `json:"existing"`
	Writing  string `json:"writing"`
}

// watchWritingAfter is how long a game waits for a program writing into it before the status says so.
const watchWritingAfter = 2 * time.Minute

// maxRedos is how many times in a row the watched folder makes a game's .p5z again by itself.
const maxRedos = 2

// watchFound is "watch:found": what arrived, whole.
type watchFound struct {
	Paths []string `json:"paths"`
}

// watching is the watcher at work: how to stop it, what it watches and its state; gone once it was stopped, from
// when it tells and keeps nothing more. mutex holds gone, and is held while it tells the page and keeps
// watched.json: once a stop has it, nothing more is told.
type watching struct {
	stop    chan struct{}
	folder  string
	watcher *watch.Watcher
	mutex   sync.Mutex
	gone    bool
}

type watchedJSON struct {
	Folder   string                `json:"folder"`
	Done     map[string]string     `json:"done"`
	Existing map[string]string     `json:"existing"`
	Made     map[string]watch.Made `json:"made,omitempty"`
}

func watchedPath() (string, error) {
	folder, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(folder, watchedFile), nil
}

// keptOf is what was kept of folder in earlier runs; ok is false when nothing was (a folder never watched, or
// a file of an earlier build, with neither what ended nor what was there): then what is there is not new.
func keptOf(folder string) (kept watchedJSON, ok bool) {
	path, err := watchedPath()
	if err != nil {
		return kept, false
	}
	data, err := os.ReadFile(path)
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &kept) != nil || json.Unmarshal(data, &fields) != nil || kept.Folder != folder {
		return watchedJSON{}, false
	}
	if _, done := fields["done"]; !done {
		if _, existing := fields["existing"]; !existing {
			return watchedJSON{}, false
		}
	}
	return kept, true
}

// keepMutex keeps two writers of watched.json (the watcher's looks, a game's end, Compress the N already there)
// from writing it at once; a stopped one writes nothing (watching.gone).
var keepMutex sync.Mutex

func keepWatched(w *watching) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if !w.gone {
		keepLocked(w)
	}
}

// keepLocked writes watched.json, with w.mutex held.
func keepLocked(w *watching) {
	keepMutex.Lock()
	defer keepMutex.Unlock()
	path, err := watchedPath()
	if err != nil {
		return
	}
	done, existing := w.watcher.Kept()
	data, err := json.Marshal(watchedJSON{Folder: w.folder, Done: done, Existing: existing, Made: w.watcher.KeptMade()})
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	temp := path + ".tmp"
	if os.WriteFile(temp, data, 0o644) == nil {
		os.Rename(temp, path)
	}
}

// StartWatch is called by the page once it listens: from now on the watched folder of Settings is looked
// at (and again with what SetSettings keeps). A folder never watched before is looked at as if it was just
// turned on: what is there is not new. It says what is watched.
func (a *App) StartWatch() WatchStatus {
	a.watchMutex.Lock()
	a.watchListening = true
	if a.watch != nil {
		a.watch.watcher.Restart() // the page loaded again: what it had and did not end comes again
	}
	a.watchMutex.Unlock()
	return a.rewatch(false)
}

// rewatch watches what Settings says, if it is not what is watched already; snapshot (it was just turned on, or
// its folder changed) takes what is there as not new.
func (a *App) rewatch(snapshot bool) WatchStatus {
	saved, _ := settings.Load()
	folder := ""
	if saved.WatchOn {
		folder = saved.WatchFolder
	}
	a.watchMutex.Lock()
	defer a.watchMutex.Unlock()
	if !a.watchListening {
		return WatchStatus{}
	}
	if a.watch != nil && a.watch.folder == folder && !snapshot {
		return WatchStatus{Folder: folder, Existing: a.watch.watcher.ExistingCount()}
	}
	a.stopWatchLocked()
	if folder == "" {
		return WatchStatus{}
	}
	every, quiet := a.watchEvery, a.watchQuiet
	if every == 0 {
		every = watchEvery
	}
	if quiet == 0 {
		quiet = watchQuiet
	}
	watcher := &watch.Watcher{Folder: folder, Quiet: quiet}
	if kept, ok := keptOf(folder); ok && !snapshot {
		watcher.Done, watcher.Existing, watcher.Made = kept.Done, kept.Existing, kept.Made
	} else {
		watcher.Snapshot = true
	}
	w := &watching{stop: make(chan struct{}), folder: folder, watcher: watcher}
	a.watch = w
	a.watchLoops.Add(1)
	go a.watchLoop(w, every)
	return WatchStatus{Folder: folder}
}

func (a *App) watchLoop(w *watching, every time.Duration) {
	defer a.watchLoops.Done()
	poll := a.watchPoll
	if poll == nil {
		poll = (*watch.Watcher).Poll
	}
	writingAfter := a.watchWritingAfter
	if writingAfter == 0 {
		writingAfter = watchWritingAfter
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	told, existing := WatchStatus{Error: "-"}, -1
	done, _ := w.watcher.Kept()
	kept := len(done)
	for {
		// while a queue is paused the folder is not looked at: the disk stays free
		if a.queuePaused() {
			select {
			case <-w.stop:
				return
			case <-tick.C:
			}
			continue
		}
		found, err := poll(w.watcher)
		// all of what this look found is told at once, unless it was stopped meanwhile (the window closes, another
		// folder): then it tells and keeps nothing more, and a stop that comes meanwhile waits for it
		w.mutex.Lock()
		if w.gone {
			w.mutex.Unlock()
			return
		}
		status := WatchStatus{Folder: w.folder, Existing: w.watcher.ExistingCount(), Writing: writingNames(w.watcher.Writing(writingAfter))}
		switch {
		case errors.Is(err, watch.ErrGone):
			status.Error = "gone"
		case err != nil:
			status.Error = "unreadable"
		}
		if status != told {
			a.send("watch:status", status)
			told = status
		}
		if done, _ := w.watcher.Kept(); len(done) != kept || status.Existing != existing {
			keepLocked(w)
			kept, existing = len(done), status.Existing
		}
		if len(found) > 0 {
			a.send("watch:found", watchFound{Paths: found})
		}
		// what could not be read (a file it may not open): left alone until it changes, and said once
		if skipped := w.watcher.TakeSkipped(); len(skipped) > 0 {
			a.send("watch:skipped", watchFound{Paths: skipped})
		}
		w.mutex.Unlock()
		select {
		case <-w.stop:
			return
		case <-tick.C:
		}
	}
}

// watcher is the watcher at work, nil when none is.
func (a *App) watcher() *watching {
	a.watchMutex.Lock()
	defer a.watchMutex.Unlock()
	return a.watch
}

// writingNames are the names of paths, for the status.
func writingNames(paths []string) string {
	names := make([]string, len(paths))
	for i, path := range paths {
		names[i] = filepath.Base(path)
	}
	return strings.Join(names, ", ")
}

// WatchEnded is how a game the watched folder brought ended: final (compressed, failed, or in the destination
// already) makes it done until it changes; else (it never ran, or was cancelled) the next run brings it again.
// For one compressed well, out is the .p5z made of it, titleID and version its game's, and redone that it was
// made again in place of the one made before: they are kept with it, as it is now ("" else).
func (a *App) WatchEnded(path string, final bool, out, titleID, version string, redone bool) {
	if w := a.watcher(); w != nil {
		w.watcher.EndedAs(path, watch.Ending{Final: final, Out: out, TitleID: titleID, Version: version, Redone: redone})
		if final {
			keepWatched(w)
		}
	}
}

// watchRedo looks at each plan of a game the watched folder brought that has something in its way, which it never
// replaces but in one case: the very .p5z it made of that game (the size and date it had), of the same game in
// the same version, when the game grew or lost files since (a copy that stalled and went on), at most maxRedos
// times in a row. Then the plan gets what that file must still be when the new one takes its place (ReplaceIf);
// else why not (WatchSkip). The page asks for it apart (Choices.Redo), and the Runner decides again (redoStill).
func (a *App) watchRedo(plans []jobs.Plan) {
	w := a.watcher()
	for i := range plans {
		p := &plans[i]
		if !p.Item.Watched || p.Block != "" || len(p.Replace) == 0 {
			continue
		}
		p.ReplaceIf, p.WatchSkip = nil, "watch_exists"
		if w == nil {
			continue
		}
		made, ok := w.watcher.MadeOf(p.Item.Path)
		if !ok || pathKey(made.Out) != pathKey(p.Out) || !made.Matches() {
			continue // not what it made: never replaced
		}
		if p.WatchSkip = redoSkip(made, p.Item, w.watcher.HandedSig(p.Item.Path)); p.WatchSkip == "" {
			p.ReplaceIf = &jobs.Fingerprint{Size: made.Size, Mtime: made.Mtime}
		}
	}
}

// redoSkip is why the .p5z made (made) of a game handed over again as it is now (now, its signature) is not made
// again, "" when it is.
func redoSkip(made watch.Made, it jobs.Item, now string) string {
	switch {
	case made.TitleID == "" || !strings.EqualFold(made.TitleID, it.TitleID):
		return "watch_exists"
	case made.Version != it.Version:
		return versionSkip(made.Version, it.Version) // never a .p5z of another version
	case now == "":
		return "watch_exists"
	case sameFiles(made.Sig, now):
		return "watch_dates_only"
	case made.Redos >= maxRedos:
		return "watch_redo_limit"
	}
	return ""
}

// sameFiles tells whether two signatures (count/size/newest) have the same files and size: only dates changed.
func sameFiles(a, b string) bool {
	x, y := strings.Split(a, "/"), strings.Split(b, "/")
	return len(x) == 3 && len(y) == 3 && x[0] == y[0] && x[1] == y[1]
}

// versionSkip is the detail of a dump of version dump whose game is in the destination in version there: a newer
// dump points to Details › Update with a new dump…, an older one says the destination has a newer one, and one that
// cannot be told apart says it is there.
func versionSkip(there, dump string) string {
	if strings.ContainsAny(there+dump, " \t") {
		return "watch_exists"
	}
	switch c, ok := compareVersions(dump, there); {
	case !ok || c == 0:
		return "watch_exists"
	case c > 0:
		return "watch_other_version: " + there + " " + dump
	}
	return "watch_older_version: " + there + " " + dump
}

// compareVersions compares two versions of a game ("01.002.000"), part by part as numbers: -1, 0 or 1, and ok
// false when either is not so written.
func compareVersions(a, b string) (int, bool) {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	if a == "" || b == "" || len(x) != len(y) {
		return 0, false
	}
	for i := range x {
		m, errM := strconv.Atoi(x[i])
		n, errN := strconv.Atoi(y[i])
		if errM != nil || errN != nil {
			return 0, false
		}
		if m != n {
			if m < n {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// watchVersions, for the plans of the page: a game the watched folder brought whose destination has a .p5z it did
// not make, of the same game (title ID, read with info --json) in another version, says both (see versionSkip).
func (a *App) watchVersions(plans []jobs.Plan) {
	for i := range plans {
		p := &plans[i]
		if !p.Item.Watched || p.Block != "" || len(p.Replace) == 0 || p.ReplaceIf != nil || p.WatchSkip != "watch_exists" ||
			p.Item.TitleID == "" || p.Item.Version == "" || !strings.EqualFold(filepath.Ext(p.Out), ".p5z") {
			continue
		}
		engine, err := a.engine()
		if err != nil {
			return
		}
		if there, err := jobs.Describe(engine, p.Out); err == nil && there.Kind == "p5z" && strings.EqualFold(there.TitleID, p.Item.TitleID) {
			p.WatchSkip = versionSkip(there.Version, p.Item.Version)
		}
	}
}

// redoStill is the Runner's look, when its turn comes and right before the swap, at a game to make again in place
// of the .p5z the watched folder made of it: "" while the watcher at work still has that record, of that game and
// version, and the file is still exactly so; else why not. No watcher (turned off, or another folder) is a no.
func (a *App) redoStill(p jobs.Plan) string {
	w := a.watcher()
	if w == nil || p.ReplaceIf == nil {
		return "watch_redo_refused"
	}
	made, ok := w.watcher.MadeOf(p.Item.Path)
	if !ok || pathKey(made.Out) != pathKey(p.Out) || made.Size != p.ReplaceIf.Size || made.Mtime != p.ReplaceIf.Mtime ||
		!made.Matches() || !strings.EqualFold(made.TitleID, p.Item.TitleID) || made.Version != p.Item.Version || made.Redos >= maxRedos {
		return "watch_redo_refused"
	}
	return ""
}

// watchOwn tells the watcher that the window put out there itself (what a queue made): if it is in the watched
// folder, it is not new.
func (a *App) watchOwn(out string) {
	if w := a.watcher(); w != nil && out != "" {
		w.watcher.Own(out)
	}
}

// WatchTakeExisting makes the games that were in the watched folder when it was turned on new to it (the page
// offers to compress them): they come in "watch:found" as what arrives comes, once whole. It gives how many.
func (a *App) WatchTakeExisting() int {
	w := a.watcher()
	if w == nil {
		return 0
	}
	taken := w.watcher.TakeExisting()
	keepWatched(w)
	return taken
}

// stopWatch stops the watcher. It does not wait for a look at work (a share that dropped may take long): that
// one ends on its own, and tells and keeps nothing.
func (a *App) stopWatch() {
	a.watchMutex.Lock()
	defer a.watchMutex.Unlock()
	a.stopWatchLocked()
}

func (a *App) stopWatchLocked() {
	if a.watch == nil {
		return
	}
	w := a.watch
	w.mutex.Lock()
	w.gone = true
	close(w.stop)
	w.mutex.Unlock()
	a.watch = nil
}

// ChooseWatchFolder asks for the folder to watch; "" when none is chosen.
func (a *App) ChooseWatchFolder() (string, error) {
	if a.ctx == nil {
		return "", errNoWindow
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{CanCreateDirectories: true})
}
