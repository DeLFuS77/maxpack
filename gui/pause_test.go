// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// pauseExtras stands in for the extras, keeping the states of the taskbar and how many hold the PC awake.
type pauseExtras struct {
	mutex   sync.Mutex
	states  []string
	holding int
}

func (x *pauseExtras) extras() extras {
	return extras{
		Awake: func() func() {
			x.mutex.Lock()
			x.holding++
			x.mutex.Unlock()
			return func() {
				x.mutex.Lock()
				x.holding--
				x.mutex.Unlock()
			}
		},
		Taskbar: func(state string, done, total uint64) {
			x.mutex.Lock()
			x.states = append(x.states, state)
			x.mutex.Unlock()
		},
		Notify: func(string, string) bool { return true },
	}
}

func (x *pauseExtras) now() (state string, holding int) {
	x.mutex.Lock()
	defer x.mutex.Unlock()
	if len(x.states) > 0 {
		state = x.states[len(x.states)-1]
	}
	return state, x.holding
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal(what)
		}
	}
}

// Pause and Resume a queue of Compress: the engine at work is told so, the page hears "job:paused", the taskbar
// shows it paused and the PC may sleep meanwhile; resumed, the game ends well.
func TestPauseAndResumeAQueue(t *testing.T) {
	useSettings(t, t.TempDir())
	t.Setenv(fakecore.Env, "pauses")
	lines := filepath.Join(t.TempDir(), "lines")
	t.Setenv(fakecore.LinesEnv, lines)
	events := newPageEvents()
	x := &pauseExtras{}
	a := pageApp(os.Args[0], events)
	a.extras = x.extras()
	if err := a.Pause(); err == nil {
		t.Fatal("paused with no queue running")
	}
	source := makeDump(t, t.TempDir(), "Starfall", "PPSA10001", "Starfall Odyssey")
	it := jobs.Item{Path: source, TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	if err := a.Start("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Dest: t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "no progress", func() bool {
		events.mutex.Lock()
		defer events.mutex.Unlock()
		return len(events.progress) > 0
	})
	if err := a.Pause(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the engine was not told to pause", func() bool { data, _ := os.ReadFile(lines); return string(data) == "pause\n" })
	if state, holding := x.now(); state != "paused" || holding != 0 {
		t.Fatalf("paused: the taskbar says %q, %d hold the PC awake", state, holding)
	}
	if !a.queuePaused() {
		t.Fatal("the queue does not say it is paused")
	}
	if err := a.Resume(); err != nil {
		t.Fatal(err)
	}
	sum := events.wait(t)
	if sum.Done != 1 {
		t.Fatalf("%+v %v", sum, events.states(0))
	}
	if data, _ := os.ReadFile(lines); string(data) != "pause\nresume\n" {
		t.Fatalf("the engine read %q", data)
	}
	events.mutex.Lock()
	told := fmt.Sprint(events.paused)
	events.mutex.Unlock()
	if told != "[true false]" {
		t.Fatalf("the page heard %s", told)
	}
	if _, holding := x.now(); holding != 0 {
		t.Fatalf("%d still hold the PC awake", holding)
	}
	if err := a.Resume(); err == nil {
		t.Fatal("resumed with no queue running")
	}
}

// A queue paused and then cancelled (Cancel, or the window closing) cancels the game at work.
func TestCancelAPausedQueue(t *testing.T) {
	useSettings(t, t.TempDir())
	t.Setenv(fakecore.Env, "pauses")
	t.Setenv(fakecore.LinesEnv, filepath.Join(t.TempDir(), "lines"))
	events := newPageEvents()
	a := pageApp(os.Args[0], events)
	source := makeDump(t, t.TempDir(), "Starfall", "PPSA10001", "Starfall Odyssey")
	it := jobs.Item{Path: source, TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	dest := t.TempDir()
	if err := a.Start("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Dest: dest}}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "no progress", func() bool {
		events.mutex.Lock()
		defer events.mutex.Unlock()
		return len(events.progress) > 0
	})
	if err := a.Pause(); err != nil {
		t.Fatal(err)
	}
	a.Cancel()
	if sum := events.wait(t); sum.Cancelled != 1 {
		t.Fatalf("%+v", sum)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("something was left in the destination: %v", entries)
	}
}

// While a queue is paused the watched folder is not looked at (the disk stays free): what arrives is found
// once it goes on.
func TestWatchWaitsWhilePaused(t *testing.T) {
	useSettings(t, t.TempDir())
	folder := t.TempDir()
	saveSettings(t, func(s *settings.Settings) { s.WatchFolder, s.WatchOn = folder, true })
	events := &watchEvents{}
	a := watchApp(events)
	defer a.stopWatch()
	pause := core.NewPause()
	a.queue = &queue{section: "compress", pause: pause, done: make(chan struct{})}
	a.queue.ctx, a.queue.cancel = contextForTest()
	a.StartWatch()
	// the first look takes what is there as not new; then the queue pauses, and a game arrives
	waitUntil(t, "the folder was never looked at", func() bool {
		events.mutex.Lock()
		defer events.mutex.Unlock()
		return len(events.status) > 0
	})
	pause.Set(true)
	time.Sleep(50 * time.Millisecond)
	makeDump(t, folder, "New", "PPSA10002", "New")
	if found := events.waitFound(t, 400*time.Millisecond); len(found) != 0 {
		t.Fatalf("looked at while paused: %v", found)
	}
	pause.Set(false)
	if found := events.waitFound(t, 5*time.Second); len(found) != 1 {
		t.Fatalf("not found once resumed: %v", found)
	}
}

func contextForTest() (context.Context, context.CancelCauseFunc) {
	return context.WithCancelCause(context.Background())
}
