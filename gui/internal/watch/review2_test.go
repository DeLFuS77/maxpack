// SPDX-License-Identifier: GPL-3.0-or-later

package watch

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// What systems write into folders they show (Finder, the Explorer, Spotlight, the recycle bins) is no part of a
// dump: it never makes one new nor changing, in the listing or through the files.
func TestJunkIsNotNew(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "Game")
	dump(t, game, true)
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Snapshot = true
	w.Poll()
	before, _, _ := Signature(game)
	for _, name := range []string{".DS_Store", "._eboot.bin", "Thumbs.db", "desktop.ini", "ehthumbs.db", filepath.Join("sce_sys", ".DS_Store"),
		filepath.Join(".Spotlight-V100", "store.db"), filepath.Join("$RECYCLE.BIN", "x"), filepath.Join("System Volume Information", "y"),
		filepath.Join("__MACOSX", "z"), filepath.Join(".fseventsd", "f"), filepath.Join(".Trashes", "t")} {
		write(t, filepath.Join(game, name), "junk")
	}
	if after, _, _ := Signature(game); after != before {
		t.Errorf("the signature changed: %q %q", before, after)
	}
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 0 || w.ExistingCount() != 1 {
		t.Errorf("junk made it new: %v, existing %d", names(found), w.ExistingCount())
	}
}

// WinSCP's .filepart is a download at work too.
func TestFilepart(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.pkg.filepart"), "half")
	write(t, filepath.Join(dir, "b.pkg"), "the name")
	write(t, filepath.Join(dir, "b.pkg.filepart"), "half")
	dump(t, filepath.Join(dir, "Game"), true)
	write(t, filepath.Join(dir, "Game", "data", "x.bin.filepart"), "half")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 0 {
		t.Errorf("%v", names(found))
	}
}

// Compress the N already there: they go as what arrives goes, once they have not changed for a minute and none of
// their files is still being written, by the next Poll; one that changed since the snapshot is new anyway, and is
// no longer counted as already there.
func TestTakeExistingWaitsLikePoll(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.pkg"), "a whole package")
	write(t, filepath.Join(dir, "b.pkg"), "a package being copied")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Snapshot = true
	w.Poll()
	write(t, filepath.Join(dir, "b.pkg"), "a package being copied, and more")
	w.Poll()
	if n := w.ExistingCount(); n != 1 {
		t.Fatalf("existing after b changed: %d", n)
	}
	if n := w.TakeExisting(); n != 1 {
		t.Fatalf("taken: %d", n)
	}
	if found, _ := w.Poll(); len(found) != 0 {
		t.Fatalf("handed before the quiet minute of b: %v", names(found))
	}
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); !slices.Equal(names(found), []string{"a.pkg", "b.pkg"}) {
		t.Fatalf("%v", names(found))
	}
	c.pass(10 * time.Minute)
	if found, _ := w.Poll(); len(found) != 0 {
		t.Errorf("handed twice: %v", names(found))
	}
}

// A file that cannot be opened (not because a program writes it) is tried a few times, then that item is skipped,
// said once, and holds back nothing else; it is tried again once it changes.
func TestUnreadableIsSkipped(t *testing.T) {
	old := stat
	stat = func(path string) (int64, int64, bool, error) {
		if strings.Contains(filepath.Base(path), "bad") {
			return 0, 0, false, errors.New("access is denied")
		}
		return old(path)
	}
	defer func() { stat = old }()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "good.pkg"), "good")
	write(t, filepath.Join(dir, "bad.pkg"), "bad")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); !slices.Equal(names(found), []string{"good.pkg"}) {
		t.Fatalf("the good one waits: %v", names(found))
	}
	var skipped []string
	for i := 0; i < 5; i++ {
		c.pass(5 * time.Second)
		if found, _ := w.Poll(); len(found) != 0 {
			t.Fatalf("handed: %v", names(found))
		}
		skipped = append(skipped, w.TakeSkipped()...)
	}
	if !slices.Equal(names(skipped), []string{"bad.pkg"}) {
		t.Fatalf("skipped: %v", names(skipped))
	}
	// once it changes it is tried again
	write(t, filepath.Join(dir, "bad.pkg"), "bad, and changed")
	stat = old
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); !slices.Equal(names(found), []string{"bad.pkg"}) {
		t.Errorf("changed: %v", names(found))
	}
}

// A page that loads again gets again what was handed over and has not ended (Restart).
func TestRestartForgetsInflight(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.pkg"), "a")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 1 {
		t.Fatal(found)
	}
	w.Restart()
	if found, _ := w.Poll(); len(found) != 1 {
		t.Errorf("after a restart: %v", found)
	}
	_ = os.Remove
}
