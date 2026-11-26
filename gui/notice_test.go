// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"runtime"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// The end of a queue is told by a notice of the system (with its sound) only while the window is not in
// front, and never when Settings says not to. Where no notice could go (the portable version, a system
// without them), or on Linux, whose notices make no sound, the page plays a short sound of its own.
func TestEndNotice(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, c := range []struct {
		front, quiet, sent bool
		notices            int
	}{
		{front: true, sent: true, notices: 0},
		{front: false, sent: true, notices: 1},
		{front: false, sent: false, notices: 1},
		{front: false, quiet: true, sent: true, notices: 0},
	} {
		useSettings(t, t.TempDir())
		saveSettings(t, func(s *settings.Settings) { s.QuietEnd = c.quiet })
		events := newPageEvents()
		a := pageApp(os.Args[0], events)
		notices := 0
		a.extras.Notify = func(string, string) bool { notices++; return c.sent }
		a.front = func() bool { return c.front }
		source := makeDump(t, t.TempDir(), "Starfall", "PPSA10001", "Starfall Odyssey")
		it := jobs.Item{Path: source, TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
		if err := a.Start("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Dest: t.TempDir()}}); err != nil {
			t.Fatal(err)
		}
		if sum := events.wait(t); sum.Done != 1 {
			t.Fatalf("%+v", sum)
		}
		chimes := 0
		if c.notices > 0 && (!c.sent || runtime.GOOS == "linux") {
			chimes = 1
		}
		events.mutex.Lock()
		heard := events.chimes
		events.mutex.Unlock()
		if notices != c.notices || heard != chimes {
			t.Errorf("front %v, quiet %v, sent %v: %d notices and %d sounds, expected %d and %d", c.front, c.quiet, c.sent, notices, heard, c.notices, chimes)
		}
	}
}

// The page tells whether the window is in front; the system's own answer wins where there is one (Windows).
func TestTellFront(t *testing.T) {
	a := &App{}
	a.TellFront(true)
	if !a.pageFront.Load() {
		t.Fatal("the page said it is in front")
	}
	a.TellFront(false)
	if a.pageFront.Load() {
		t.Fatal("the page said it is not in front")
	}
}
