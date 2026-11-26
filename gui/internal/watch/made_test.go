// SPDX-License-Identifier: GPL-3.0-or-later

package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// handOver makes the watcher hand over what is in its folder (new, then quiet), and gives it.
func handOver(t *testing.T, w *Watcher, c *clock) []string {
	t.Helper()
	w.Poll()
	c.pass(61 * time.Second)
	found, err := w.Poll()
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// What the watched folder made of an item that ended well is kept with it: the .p5z, its size and its date, so
// that once the item changes the window can tell whether what is in the destination is still that very file.
// It is kept between runs, and forgotten with the item.
func TestMadeIsKept(t *testing.T) {
	dir, dest := t.TempDir(), t.TempDir()
	game := filepath.Join(dir, "Game")
	dump(t, game, true)
	out := filepath.Join(dest, "Game [PPSA10001].p5z")
	write(t, out, "a .p5z")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	if found := handOver(t, w, c); len(found) != 1 {
		t.Fatalf("found %v", found)
	}
	w.EndedWith(game, true, out)
	info, _ := os.Stat(out)
	made, ok := w.MadeOf(game)
	if !ok || made.Out != out || made.Size != info.Size() || made.Mtime != info.ModTime().UnixNano() || made.Sig == "" {
		t.Fatalf("%+v %v", made, ok)
	}
	if !made.Matches() {
		t.Error("the .p5z as it was made does not match")
	}
	os.WriteFile(out, []byte("another .p5z, of the user"), 0o644)
	if made.Matches() {
		t.Error("another file in its place matches")
	}
	// a new run has it
	again := newWatcher(dir, c)
	again.Done, again.Made = w.Done, w.KeptMade()
	if m, ok := again.MadeOf(game); !ok || m != made {
		t.Errorf("a new run: %+v %v", m, ok)
	}
	// an end that is not ok (or of nothing made) leaves what was made of it as it was
	write(t, filepath.Join(game, "eboot.bin"), "a new eboot")
	if found := handOver(t, again, c); len(found) != 1 {
		t.Fatalf("changed: %v", found)
	}
	again.EndedWith(game, true, "")
	if m, _ := again.MadeOf(game); m != made {
		t.Errorf("a failure forgot what was made: %+v", m)
	}
	os.RemoveAll(game)
	for i := 0; i < forgetAfter; i++ {
		again.Poll()
	}
	if _, ok := again.MadeOf(game); ok || len(again.KeptMade()) != 0 {
		t.Error("gone, what was made of it is still kept")
	}
}

// What the window itself puts in the watched folder (Decompress there, a folder for the console made there) is
// never new: it is done as it is when it is put there.
func TestOwnIsNotNew(t *testing.T) {
	dir := t.TempDir()
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Poll()
	game := filepath.Join(dir, "Unpacked")
	dump(t, game, true)
	w.Own(filepath.Join(game, "eboot.bin")) // what is inside an entry makes the entry its own
	if found := handOver(t, w, c); len(found) != 0 {
		t.Fatalf("what the window put there is new: %v", found)
	}
	w.Own(filepath.Join(t.TempDir(), "Elsewhere")) // outside the folder: nothing
	if done, _ := w.Kept(); len(done) != 1 {
		t.Errorf("done: %v", done)
	}
	// changed afterwards, by someone else: new
	write(t, filepath.Join(game, "more.bin"), "more")
	if found := handOver(t, w, c); len(found) != 1 {
		t.Errorf("changed: %v", found)
	}
}

// A file of the item that a program of this user holds open for writing (as /proc shows it on Linux) makes the
// item busy: it waits, as on Windows.
func TestHeldOpenIsBusy(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "Game")
	dump(t, game, true)
	defer func() { held = heldOpen }()
	held = func(root string) bool { return root == game }
	if _, busy, _ := Signature(game); !busy {
		t.Error("held open, not busy")
	}
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	if found := handOver(t, w, c); len(found) != 0 {
		t.Errorf("held open, handed over: %v", found)
	}
	held = func(string) bool { return false }
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 1 {
		t.Errorf("let go, not handed over: %v", found)
	}
}
