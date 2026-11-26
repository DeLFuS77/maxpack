// SPDX-License-Identifier: GPL-3.0-or-later

package main

// The watched folder (Settings › Watched folder): while the window is open (minimized too), one folder is
// looked at every few seconds, and what arrives in it, once it has stopped changing for a minute, goes to
// the page ("watch:found"), which compresses it as if it had been dropped on Compress (see app.js). Nothing
// runs with the window closed: what arrived meanwhile is found when it opens. What was handed over is kept
// in watched.json next to the settings, so a game is never handed over twice as it is (one that failed is
// tried again only once it changes).

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

// watchedFile keeps what was handed over, of which folder.
const watchedFile = "watched.json"

// WatchStatus is what the page is told of the watched folder ("watch:status", and StartWatch): the folder
// watched ("" when none is) and why it cannot be looked at: "gone" (not there, or not a folder) or
// "unreadable"; "" when it can.
type WatchStatus struct {
	Folder string `json:"folder"`
	Error  string `json:"error"`
}

// watchFound is "watch:found": what arrived, whole.
type watchFound struct {
	Paths []string `json:"paths"`
}

// watching is the watcher at work: how to stop it, when it stopped, and what it watches.
type watching struct {
	stop   chan struct{}
	done   chan struct{}
	folder string
}

type watchedJSON struct {
	Folder string            `json:"folder"`
	Handed map[string]string `json:"handed"`
}

func watchedPath() (string, error) {
	folder, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(folder, watchedFile), nil
}

// handedOf is what was handed over of folder in earlier runs.
func handedOf(folder string) map[string]string {
	path, err := watchedPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var kept watchedJSON
	if json.Unmarshal(data, &kept) != nil || kept.Folder != folder {
		return nil
	}
	return kept.Handed
}

func keepHanded(folder string, handed map[string]string) {
	path, err := watchedPath()
	if err != nil {
		return
	}
	data, err := json.Marshal(watchedJSON{Folder: folder, Handed: handed})
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
// at (and again with what SetSettings keeps). It says what is watched.
func (a *App) StartWatch() WatchStatus {
	a.watchMutex.Lock()
	a.watchListening = true
	a.watchMutex.Unlock()
	return a.rewatch()
}

// rewatch watches what Settings says, if it is not what is watched already.
func (a *App) rewatch() WatchStatus {
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
	if a.watch != nil && a.watch.folder == folder {
		return WatchStatus{Folder: folder}
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
	w := &watching{stop: make(chan struct{}), done: make(chan struct{}), folder: folder}
	a.watch = w
	go a.watchLoop(w, every, quiet)
	return WatchStatus{Folder: folder}
}

func (a *App) watchLoop(w *watching, every, quiet time.Duration) {
	defer close(w.done)
	watcher := &watch.Watcher{Folder: w.folder, Quiet: quiet, Handed: handedOf(w.folder)}
	tick := time.NewTicker(every)
	defer tick.Stop()
	told, kept := "-", len(watcher.Handed)
	for {
		found, err := watcher.Poll()
		why := ""
		switch {
		case errors.Is(err, watch.ErrGone):
			why = "gone"
		case err != nil:
			why = "unreadable"
		}
		if why != told {
			a.send("watch:status", WatchStatus{Folder: w.folder, Error: why})
			told = why
		}
		if len(found) > 0 || len(watcher.Handed) != kept {
			keepHanded(w.folder, watcher.Handed)
			kept = len(watcher.Handed)
		}
		if len(found) > 0 {
			a.send("watch:found", watchFound{Paths: found})
		}
		select {
		case <-w.stop:
			return
		case <-tick.C:
		}
	}
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
