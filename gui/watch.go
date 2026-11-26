// SPDX-License-Identifier: GPL-3.0-or-later

package main

// The watched folder (Settings › Watched folder): while the window is open (minimized too), one folder is
// looked at every few seconds, and what arrives in it, once it has stopped changing for a minute, goes to
// the page ("watch:found"), which compresses it as if it had been dropped on Compress (see app.js). Turning it
// on (or choosing another folder) takes a snapshot: what is there then is not new, and the page offers to
// compress it ("Compress the N already there", WatchTakeExisting). What the page says ended (WatchEnded: done
// or failed) is kept in watched.json next to the settings, so a game is not handed over again as it is; what
// never ran is handed again in the next run. Nothing runs with the window closed: what arrived meanwhile is
// found when it opens.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/watch"
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
// when it can), and how many games were there when it was turned on and have not been compressed.
type WatchStatus struct {
	Folder   string `json:"folder"`
	Error    string `json:"error"`
	Existing int    `json:"existing"`
}

// watchFound is "watch:found": what arrived, whole.
type watchFound struct {
	Paths []string `json:"paths"`
}

// watching is the watcher at work: how to stop it, when it stopped, what it watches and its state.
type watching struct {
	stop    chan struct{}
	done    chan struct{}
	folder  string
	watcher *watch.Watcher
}

type watchedJSON struct {
	Folder   string            `json:"folder"`
	Done     map[string]string `json:"done"`
	Existing map[string]string `json:"existing"`
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
// from writing it at once.
var keepMutex sync.Mutex

func keepWatched(folder string, w *watch.Watcher) {
	keepMutex.Lock()
	defer keepMutex.Unlock()
	path, err := watchedPath()
	if err != nil {
		return
	}
	done, existing := w.Kept()
	data, err := json.Marshal(watchedJSON{Folder: folder, Done: done, Existing: existing})
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
		watcher.Done, watcher.Existing = kept.Done, kept.Existing
	} else {
		watcher.Snapshot = true
	}
	w := &watching{stop: make(chan struct{}), done: make(chan struct{}), folder: folder, watcher: watcher}
	a.watch = w
	go a.watchLoop(w, every)
	return WatchStatus{Folder: folder}
}

func (a *App) watchLoop(w *watching, every time.Duration) {
	defer close(w.done)
	tick := time.NewTicker(every)
	defer tick.Stop()
	told, existing := WatchStatus{Error: "-"}, -1
	done, _ := w.watcher.Kept()
	kept := len(done)
	for {
		found, err := w.watcher.Poll()
		status := WatchStatus{Folder: w.folder, Existing: w.watcher.ExistingCount()}
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
			keepWatched(w.folder, w.watcher)
			kept, existing = len(done), status.Existing
		}
		if len(found) > 0 {
			a.send("watch:found", watchFound{Paths: found})
		}
		// what could not be read (a file it may not open): left alone until it changes, and said once
		if skipped := w.watcher.TakeSkipped(); len(skipped) > 0 {
			a.send("watch:skipped", watchFound{Paths: skipped})
		}
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

// WatchEnded is how a game the watched folder brought ended: final (compressed, failed, or in the destination
// already) makes it done until it changes; else (it never ran, or was cancelled) the next run brings it again.
func (a *App) WatchEnded(path string, final bool) {
	if w := a.watcher(); w != nil {
		w.watcher.Ended(path, final)
		if final {
			keepWatched(w.folder, w.watcher)
		}
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
	keepWatched(w.folder, w.watcher)
	return taken
}

// stopWatch stops the watcher, and waits for it.
func (a *App) stopWatch() {
	a.watchMutex.Lock()
	defer a.watchMutex.Unlock()
	a.stopWatchLocked()
}

func (a *App) stopWatchLocked() {
	if a.watch == nil {
		return
	}
	close(a.watch.stop)
	<-a.watch.done
	a.watch = nil
}

// ChooseWatchFolder asks for the folder to watch; "" when none is chosen.
func (a *App) ChooseWatchFolder() (string, error) {
	if a.ctx == nil {
		return "", errNoWindow
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{CanCreateDirectories: true})
}
