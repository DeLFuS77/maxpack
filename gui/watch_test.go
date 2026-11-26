// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/json"
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

func (w *watchEvents) lastStatus() WatchStatus {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if len(w.status) == 0 {
		return WatchStatus{}
	}
	return w.status[len(w.status)-1]
}

func watchApp(events *watchEvents) *App {
	return &App{emit: events.emit, watchEvery: 10 * time.Millisecond, watchQuiet: 60 * time.Millisecond}
}

// Turning the watched folder on takes a snapshot: what is there already is not new, and the page is told how
// many there are, which it compresses only when asked (WatchTakeExisting). What arrives afterwards goes, once it
// stops changing. Off, nothing is looked at.
func TestWatchSnapshotAndArrivals(t *testing.T) {
	useSettings(t, t.TempDir())
	folder := t.TempDir()
	old := makeDump(t, folder, "Old", "PPSA10001", "Old")
	saveSettings(t, func(s *settings.Settings) { s.WatchFolder = folder })
	events := &watchEvents{}
	a := watchApp(events)
	defer a.stopWatch()
	if status := a.StartWatch(); status.Folder != "" {
		t.Fatalf("off: %+v", status)
	}
	s, _ := settings.Load()
	s.WatchOn = true
	if err := a.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if found := events.waitFound(t, 0); len(found) != 0 {
		t.Fatalf("what was there is new: %v", found)
	}
	if status := events.lastStatus(); status.Existing != 1 || status.Folder != folder {
		t.Fatalf("status: %+v", status)
	}
	// asked for, what was there goes as what arrives goes, by the watcher's next looks
	if taken := a.WatchTakeExisting(); taken != 1 {
		t.Fatalf("taken: %d", taken)
	}
	found := events.waitFound(t, 3*time.Second)
	if len(found) != 1 || len(found[0]) != 1 || found[0][0] != old {
		t.Fatalf("found: %v", found)
	}
	fresh := makeDump(t, folder, "New", "PPSA10002", "New")
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end) && len(events.waitFound(t, 0)) < 2; time.Sleep(10 * time.Millisecond) {
	}
	if found := events.waitFound(t, 0); len(found) != 2 || len(found[1]) != 1 || found[1][0] != fresh {
		t.Fatalf("found: %v", found)
	}
}

// A watched.json of an earlier build (with neither "done" nor "existing") counts as a snapshot: nothing that is
// there becomes new.
func TestWatchOldStateIsASnapshot(t *testing.T) {
	useSettings(t, t.TempDir())
	folder := t.TempDir()
	makeDump(t, folder, "Old", "PPSA10001", "Old")
	saveSettings(t, func(s *settings.Settings) { s.WatchOn, s.WatchFolder = true, folder })
	old, _ := json.Marshal(map[string]any{"folder": folder, "handed": map[string]string{}})
	os.WriteFile(filepath.Join(must(settings.Dir()), watchedFile), old, 0o644)
	events := &watchEvents{}
	a := watchApp(events)
	a.StartWatch()
	defer a.stopWatch()
	time.Sleep(300 * time.Millisecond)
	if found := events.waitFound(t, 0); len(found) != 0 || events.lastStatus().Existing != 1 {
		t.Errorf("found %v, status %+v", found, events.lastStatus())
	}
}

// What was handed over counts as done only once the page says its compression ended: the next run hands again
// what never ran, not what ended.
func TestWatchDoneOnlyWhenEnded(t *testing.T) {
	useSettings(t, t.TempDir())
	folder := t.TempDir()
	saveSettings(t, func(s *settings.Settings) { s.WatchOn, s.WatchFolder = true, folder })
	events := &watchEvents{}
	a := watchApp(events)
	a.StartWatch() // a folder never watched: what is there (nothing) is not new
	time.Sleep(100 * time.Millisecond)
	ran := makeDump(t, folder, "Ran", "PPSA10001", "Ran")
	never := makeDump(t, folder, "Never", "PPSA10002", "Never")
	found := events.waitFound(t, 3*time.Second)
	if len(found) != 1 || len(found[0]) != 2 {
		a.stopWatch()
		t.Fatalf("found: %v", found)
	}
	a.WatchEnded(ran, true)
	a.WatchEnded(never, false)
	a.stopWatch()
	if _, err := os.Stat(filepath.Join(must(settings.Dir()), watchedFile)); err != nil {
		t.Errorf("what ended is not kept: %v", err)
	}
	again := &watchEvents{}
	b := watchApp(again)
	b.StartWatch()
	defer b.stopWatch()
	found = again.waitFound(t, 3*time.Second)
	if len(found) != 1 || len(found[0]) != 1 || found[0][0] != never {
		t.Errorf("the next run: %v", found)
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
