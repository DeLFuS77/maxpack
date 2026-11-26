// SPDX-License-Identifier: GPL-3.0-or-later

package watch

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A look at a folder on a share that is slow does not hold the watcher: what ended, what is kept and what is
// taken are said meanwhile, without waiting for the listing nor for the look through the files.
func TestSlowLookHoldsNothing(t *testing.T) {
	dir := t.TempDir()
	dump(t, filepath.Join(dir, "Game"), true)
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 1 {
		t.Fatalf("found %v", found)
	}
	w.Ended(filepath.Join(dir, "Game"), true)
	for _, slow := range []string{"listing", "files"} {
		blocked, release := make(chan struct{}), make(chan struct{})
		once := false
		hold := func() {
			if !once {
				once = true
				close(blocked)
				<-release
			}
		}
		if slow == "listing" {
			list = func(name string) ([]fs.DirEntry, error) { hold(); return os.ReadDir(name) }
		} else {
			// something new, so that its files are looked at once quiet
			dump(t, filepath.Join(dir, "Other"), true)
			w.Poll()
			c.pass(61 * time.Second)
			stat = func(path string) (int64, int64, bool, error) { hold(); return fileState(path) }
		}
		polled := make(chan struct{})
		go func() { w.Poll(); close(polled) }()
		<-blocked
		done := make(chan struct{})
		go func() {
			w.Ended(filepath.Join(dir, "Game"), true)
			w.Kept()
			w.TakeExisting()
			w.Restart()
			w.TakeSkipped()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("a slow %s holds the watcher", slow)
		}
		close(release)
		<-polled
		list, stat = os.ReadDir, fileState
	}
}

// A listing of a share that drops may miss entries: what ended is forgotten only once it is missing from 3
// listings in a row (it was taken away), and a listing that fails forgets nothing.
func TestMissingThriceToBeForgotten(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "Game")
	dump(t, game, true)
	write(t, filepath.Join(dir, "notes.txt"), "the folder is never empty: an empty listing forgets nothing")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 1 {
		t.Fatalf("found %v", found)
	}
	w.Ended(game, true)
	defer func() { list = os.ReadDir }()
	without := func(name string) ([]fs.DirEntry, error) {
		entries, err := os.ReadDir(name)
		var some []fs.DirEntry
		for _, e := range entries {
			if e.Name() != "Game" {
				some = append(some, e)
			}
		}
		return some, err
	}
	failing := func(string) ([]fs.DirEntry, error) { return nil, errors.New("the share dropped") }
	// missed twice, failed once, missed once more: never 3 in a row of listings that worked
	for _, l := range []func(string) ([]fs.DirEntry, error){without, without, failing, os.ReadDir, without, without} {
		list = l
		w.Poll()
		c.pass(61 * time.Second)
	}
	list = os.ReadDir
	for i := 0; i < 3; i++ {
		if found, _ := w.Poll(); len(found) != 0 {
			t.Fatalf("a listing that missed it made it new: %v", found)
		}
		c.pass(61 * time.Second)
	}
	if done, _ := w.Kept(); len(done) != 1 {
		t.Fatalf("forgotten: %v", done)
	}
	// missing from 3 listings in a row: it went, and is forgotten (it comes again as new)
	list = without
	for i := 0; i < 3; i++ {
		w.Poll()
	}
	if done, _ := w.Kept(); len(done) != 0 {
		t.Errorf("not forgotten: %v", done)
	}
	list = os.ReadDir
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 1 {
		t.Errorf("back: %v", found)
	}
}
