// SPDX-License-Identifier: GPL-3.0-or-later

package watch

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// clock is a time the test moves.
type clock struct{ t time.Time }

func (c *clock) now() time.Time       { return c.t }
func (c *clock) pass(d time.Duration) { c.t = c.t.Add(d) }
func newWatcher(dir string, c *clock) *Watcher {
	return &Watcher{Folder: dir, Quiet: time.Minute, Now: c.now}
}

func write(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func dump(t *testing.T, dir string, eboot bool) {
	t.Helper()
	write(t, filepath.Join(dir, "sce_sys", "param.json"), `{"titleId":"PPSA10001"}`)
	if eboot {
		write(t, filepath.Join(dir, "eboot.bin"), "an eboot")
	}
}

func names(paths []string) []string {
	out := []string{}
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	slices.Sort(out)
	return out
}

// What counts: a dump folder (a game with eboot.bin, or a DLC: sce_sys/param.json either way), an image of
// ShadowMountPlus and a .pkg, at the top of the folder only; nothing else, nor what is below a folder that
// is not a dump, nor what MaxPack writes apart while it works.
func TestWhatCounts(t *testing.T) {
	dir := t.TempDir()
	dump(t, filepath.Join(dir, "Game"), true)
	dump(t, filepath.Join(dir, "Game DLC"), false)
	for _, name := range []string{"a.exfat", "b.FFPKG", "c.ffpfs", "d.ffpfsc", "e.pkg", "notes.txt", "f.p5z", "g.zip"} {
		write(t, filepath.Join(dir, name), "x")
	}
	dump(t, filepath.Join(dir, "deeper", "Other"), true)
	write(t, filepath.Join(dir, "deeper", "h.pkg"), "x")
	write(t, filepath.Join(dir, "empty folder", "readme.txt"), "x")
	dump(t, filepath.Join(dir, "Game.maxpack-tmp"), true)
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	if found, err := w.Poll(); err != nil || len(found) != 0 {
		t.Fatalf("at once: %v %v", found, err)
	}
	c.pass(61 * time.Second)
	found, err := w.Poll()
	if want := []string{"Game", "Game DLC", "a.exfat", "b.FFPKG", "c.ffpfs", "d.ffpfsc", "e.pkg"}; err != nil || !slices.Equal(names(found), want) {
		t.Fatalf("%v %v, want %v", names(found), err, want)
	}
	// once handed over, never again while it is as it was
	c.pass(10 * time.Minute)
	if found, _ := w.Poll(); len(found) != 0 {
		t.Errorf("again: %v", names(found))
	}
}

// It waits until nothing new in the folder has changed (size, date, or a file more) for a minute: a dump
// and its DLC still arriving go together, once both are whole.
func TestWaitsUntilQuiet(t *testing.T) {
	dir := t.TempDir()
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	game := filepath.Join(dir, "Game")
	dump(t, game, false)
	w.Poll()
	c.pass(50 * time.Second)
	write(t, filepath.Join(game, "eboot.bin"), "an eboot") // a file more
	w.Poll()
	c.pass(30 * time.Second)
	write(t, filepath.Join(dir, "dlc.pkg"), "a DLC begins")
	if found, _ := w.Poll(); len(found) != 0 {
		t.Fatalf("not quiet yet: %v", names(found))
	}
	c.pass(40 * time.Second) // the game is quiet, the DLC not yet: both wait
	if found, _ := w.Poll(); len(found) != 0 {
		t.Fatalf("the DLC is still arriving: %v", names(found))
	}
	c.pass(20 * time.Second)
	write(t, filepath.Join(dir, "dlc.pkg"), "a DLC begins, and grows") // its size changes
	w.Poll()
	c.pass(59 * time.Second)
	if found, _ := w.Poll(); len(found) != 0 {
		t.Fatalf("59 s: %v", names(found))
	}
	c.pass(2 * time.Second)
	if found, _ := w.Poll(); !slices.Equal(names(found), []string{"Game", "dlc.pkg"}) {
		t.Fatalf("both: %v", names(found))
	}
	// the date of a file changes: it is new again (a failure is tried again only once it changed)
	later := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(game, "eboot.bin"), later, later)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); !slices.Equal(names(found), []string{"Game"}) {
		t.Fatalf("changed: %v", names(found))
	}
}

// What was handed over is remembered (Handed, which the window keeps between runs): the same, unchanged,
// is not handed again; one that went away and came back is.
func TestHandedIsRemembered(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.pkg"), "x")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	w.Poll()
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 1 {
		t.Fatalf("%v", found)
	}
	again := newWatcher(dir, c)
	again.Handed = w.Handed
	again.Poll()
	c.pass(61 * time.Second)
	if found, _ := again.Poll(); len(found) != 0 {
		t.Errorf("a new run hands it again: %v", found)
	}
	os.Remove(filepath.Join(dir, "a.pkg"))
	again.Poll()
	if len(again.Handed) != 0 {
		t.Errorf("gone, still remembered: %v", again.Handed)
	}
	write(t, filepath.Join(dir, "a.pkg"), "x")
	again.Poll()
	c.pass(61 * time.Second)
	if found, _ := again.Poll(); len(found) != 1 {
		t.Errorf("back: %v", found)
	}
}

// A folder that is not there is said; nothing is handed over.
func TestFolderGone(t *testing.T) {
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(filepath.Join(t.TempDir(), "none"), c)
	if found, err := w.Poll(); err == nil || len(found) != 0 {
		t.Errorf("%v %v", found, err)
	}
}
