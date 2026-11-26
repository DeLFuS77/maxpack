// SPDX-License-Identifier: GPL-3.0-or-later

package watch

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// Downloads at work are not games: a browser's .part, .crdownload, qBittorrent's .!qB, and the empty file a
// browser puts in place of the one it downloads; a folder with one of them inside is still arriving.
func TestPlaceholders(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.pkg.part", "b.pkg.crdownload", "c.ffpkg.!qB", "d.pkg.!ut"} {
		write(t, filepath.Join(dir, name), "half")
	}
	write(t, filepath.Join(dir, "empty.pkg"), "")        // a browser's placeholder of 0 bytes
	write(t, filepath.Join(dir, "next.pkg"), "the name") // and one with the data next to it
	write(t, filepath.Join(dir, "next.pkg.part"), "data")
	dump(t, filepath.Join(dir, "Game"), true)
	write(t, filepath.Join(dir, "Game", "data", "big.bin.part"), "still coming")
	write(t, filepath.Join(dir, "whole.pkg"), "a whole package")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); !slices.Equal(names(found), []string{"whole.pkg"}) {
		t.Fatalf("%v", names(found))
	}
	os.Remove(filepath.Join(dir, "Game", "data", "big.bin.part"))
	write(t, filepath.Join(dir, "Game", "data", "big.bin"), "it came")
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); !slices.Equal(names(found), []string{"Game"}) {
		t.Fatalf("once its download ended: %v", names(found))
	}
}

// Turning it on takes a snapshot: what is there already is not new (Existing says how many, TakeExisting gives
// them when the user asks); what arrives afterwards is, and so is one already there that changes.
func TestSnapshot(t *testing.T) {
	dir := t.TempDir()
	dump(t, filepath.Join(dir, "Old"), true)
	write(t, filepath.Join(dir, "old.pkg"), "an old package")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Snapshot = true
	if found, _ := w.Poll(); len(found) != 0 {
		t.Fatalf("snapshot: %v", found)
	}
	if n := len(w.Existing); n != 2 {
		t.Fatalf("existing: %d", n)
	}
	write(t, filepath.Join(dir, "new.pkg"), "a new package")
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); !slices.Equal(names(found), []string{"new.pkg"}) {
		t.Fatalf("what arrived: %v", names(found))
	}
	if taken := w.TakeExisting(); taken != 2 || w.ExistingCount() != 0 {
		t.Fatalf("taken: %d, left %d", taken, w.ExistingCount())
	}
	if found, _ := w.Poll(); !slices.Equal(names(found), []string{"Old", "old.pkg"}) {
		t.Fatalf("taken, then handed: %v", names(found))
	}
	c.pass(10 * time.Minute)
	if found, _ := w.Poll(); len(found) != 0 {
		t.Errorf("taken, then handed again: %v", names(found))
	}
}

// Only a compression that ended counts as done (Ended, final): what never ran is handed again in the next run,
// what ended (well or failing) only once it changes.
func TestEnded(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.pkg"), "a")
	write(t, filepath.Join(dir, "b.pkg"), "b")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 2 {
		t.Fatalf("%v", found)
	}
	w.Ended(filepath.Join(dir, "a.pkg"), true)  // compressed (or it failed)
	w.Ended(filepath.Join(dir, "b.pkg"), false) // never ran: no destination, cancelled, the window closed
	c.pass(10 * time.Minute)
	if found, _ := w.Poll(); len(found) != 0 {
		t.Errorf("this run hands them again: %v", names(found))
	}
	next := newWatcher(dir, c)
	next.Done = w.Done
	next.Poll()
	c.pass(61 * time.Second)
	if found, _ := next.Poll(); !slices.Equal(names(found), []string{"b.pkg"}) {
		t.Errorf("the next run: %v", names(found))
	}
}

// On Windows a file another program still has open for writing is still arriving, whatever its listing says.
func TestOpenForWritingIsBusy(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the sharing check is Windows'")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.pkg")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("being written")
	if _, busy, _ := Signature(path); !busy {
		t.Error("a file open for writing is not busy")
	}
	f.Close()
	if _, busy, _ := Signature(path); busy {
		t.Error("a closed file is busy")
	}
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	g, _ := os.OpenFile(path, os.O_WRONLY, 0o644)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 0 {
		t.Errorf("handed while open for writing: %v", names(found))
	}
	g.Close()
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 1 {
		t.Errorf("once closed: %v", names(found))
	}
}

// The signature of a dump: its files, their sizes and dates, through handles; a change of any is another one.
func TestSignature(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "Game")
	dump(t, game, true)
	a, busy, unreadable := Signature(game)
	if busy || unreadable || a == "" {
		t.Fatalf("%q %v %v", a, busy, unreadable)
	}
	if b, _, _ := Signature(game); b != a {
		t.Errorf("not stable: %q %q", a, b)
	}
	write(t, filepath.Join(game, "eboot.bin"), "another eboot")
	if b, _, _ := Signature(game); b == a {
		t.Error("a change of size is the same")
	}
}
