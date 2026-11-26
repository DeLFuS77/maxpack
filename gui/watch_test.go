// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/settings"
)

// watchEvents keeps what the watcher told the page.
type watchEvents struct {
	mutex  sync.Mutex
	found  [][]string
	status []WatchStatus
}

func (w *watchEvents) emit(event string, data any) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	switch event {
	case "watch:found":
		w.found = append(w.found, data.(watchFound).Paths)
	case "watch:status":
		w.status = append(w.status, data.(WatchStatus))
	}
}

func (w *watchEvents) waitFound(t *testing.T, within time.Duration) [][]string {
	t.Helper()
	for end := time.Now().Add(within); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		w.mutex.Lock()
		n := len(w.found)
		w.mutex.Unlock()
		if n > 0 {
			break
		}
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return append([][]string{}, w.found...)
}

func watchApp(events *watchEvents) *App {
	return &App{emit: events.emit, watchEvery: 10 * time.Millisecond, watchQuiet: 60 * time.Millisecond}
}

// On, the watched folder of Settings is looked at while the window is open: what arrives in it, once it
// stops changing, goes to the page, once; the next run remembers it. Off, nothing is looked at.
func TestWatchHandsOver(t *testing.T) {
	useSettings(t, t.TempDir())
	folder := t.TempDir()
	game := makeDump(t, folder, "Game", "PPSA10001", "Game")
	saveSettings(t, func(s *settings.Settings) { s.WatchFolder = folder })
	events := &watchEvents{}
	a := watchApp(events)
	if status := a.StartWatch(); status.Folder != "" {
		t.Fatalf("off: %+v", status)
	}
	time.Sleep(150 * time.Millisecond)
	if found := events.waitFound(t, 0); len(found) != 0 {
		t.Fatalf("off, it looked: %v", found)
	}
	s, _ := settings.Load()
	s.WatchOn = true
	if err := a.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	found := events.waitFound(t, 3*time.Second)
	if len(found) != 1 || len(found[0]) != 1 || found[0][0] != game {
		t.Fatalf("found: %v", found)
	}
	a.stopWatch()
	if _, err := os.Stat(filepath.Join(must(settings.Dir()), watchedFile)); err != nil {
		t.Errorf("what was handed over is not kept: %v", err)
	}
	// a new run: it was handed over already
	again := &watchEvents{}
	b := watchApp(again)
	if status := b.StartWatch(); status.Folder != folder {
		t.Fatalf("%+v", status)
	}
	time.Sleep(300 * time.Millisecond)
	b.stopWatch()
	if found := again.waitFound(t, 0); len(found) != 0 {
		t.Errorf("handed over again: %v", found)
	}
}

// A watched folder that is not there is told to the page, and then that it is back.
func TestWatchGone(t *testing.T) {
	useSettings(t, t.TempDir())
	folder := filepath.Join(t.TempDir(), "later")
	saveSettings(t, func(s *settings.Settings) { s.WatchOn, s.WatchFolder = true, folder })
	events := &watchEvents{}
	a := watchApp(events)
	a.StartWatch()
	defer a.stopWatch()
	time.Sleep(100 * time.Millisecond)
	os.MkdirAll(folder, 0o755)
	time.Sleep(100 * time.Millisecond)
	events.mutex.Lock()
	defer events.mutex.Unlock()
	if len(events.status) < 2 || events.status[0].Error != "gone" || events.status[len(events.status)-1].Error != "" {
		t.Errorf("%+v", events.status)
	}
}

func must(s string, err error) string {
	if err != nil {
		panic(err)
	}
	return s
}
