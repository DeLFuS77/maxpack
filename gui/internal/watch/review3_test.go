// SPDX-License-Identifier: GPL-3.0-or-later

package watch

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// What was made of an item that ended well keeps the game and version it was made from, and how many times in a
// row the watched folder made it again; a fresh one (not made again) counts from 0.
func TestEndedAsKeepsGameAndRedos(t *testing.T) {
	dir, dest := t.TempDir(), t.TempDir()
	game := filepath.Join(dir, "Game")
	dump(t, game, true)
	out := filepath.Join(dest, "Game [PPSA10001].p5z")
	write(t, out, "a .p5z")
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	if found := handOver(t, w, c); len(found) != 1 || w.HandedSig(game) == "" {
		t.Fatalf("found %v, handed %q", found, w.HandedSig(game))
	}
	w.EndedAs(game, Ending{Final: true, Out: out, TitleID: "PPSA10001", Version: "01.000.000"})
	if made, _ := w.MadeOf(game); made.TitleID != "PPSA10001" || made.Version != "01.000.000" || made.Redos != 0 {
		t.Fatalf("%+v", made)
	}
	for want := 1; want <= 2; want++ {
		write(t, filepath.Join(game, "more.bin"), string(make([]byte, want)))
		if found := handOver(t, w, c); len(found) != 1 {
			t.Fatalf("changed: %v", found)
		}
		w.EndedAs(game, Ending{Final: true, Out: out, TitleID: "PPSA10001", Version: "01.000.000", Redone: true})
		if made, _ := w.MadeOf(game); made.Redos != want {
			t.Errorf("made again %d times: %+v", want, made)
		}
	}
	write(t, filepath.Join(game, "more.bin"), "xyz")
	handOver(t, w, c)
	w.EndedAs(game, Ending{Final: true, Out: out, TitleID: "PPSA10001", Version: "01.000.000"})
	if made, _ := w.MadeOf(game); made.Redos != 0 {
		t.Errorf("made fresh: %+v", made)
	}
	if w.HandedSig(game) != "" {
		t.Error("ended, still handed over")
	}
}

// A share that lists an entry but whose check of it fails (its sce_sys/param.json cannot be looked at), an empty
// listing of a folder that had entries (a mount point whose share went), or a listing with none of several things
// known: none of them counts toward forgetting. A listing that has the others and not it, three times, does.
func TestOnlyARealListingForgets(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "Game")
	for _, name := range []string{"Game", "Other", "Third"} {
		dump(t, filepath.Join(dir, name), true)
	}
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	found := handOver(t, w, c)
	if len(found) != 3 {
		t.Fatalf("found %v", found)
	}
	for _, path := range found {
		w.Ended(path, true)
	}
	defer func() { list = os.ReadDir }()
	param := filepath.Join(game, "sce_sys", "param.json")
	os.Rename(param, param+".away")
	for i := 0; i < 4; i++ { // listed, but not a game by its check
		w.Poll()
	}
	os.Rename(param+".away", param)
	list = func(string) ([]fs.DirEntry, error) { return nil, nil }
	for i := 0; i < 4; i++ { // empty
		w.Poll()
	}
	stranger := filepath.Join(t.TempDir(), "x")
	dump(t, stranger, true)
	list = func(string) ([]fs.DirEntry, error) { return os.ReadDir(filepath.Dir(stranger)) }
	for i := 0; i < 4; i++ { // none of what is known
		w.Poll()
	}
	if done, _ := w.Kept(); len(done) != 3 {
		t.Fatalf("forgotten: %v", done)
	}
	list = func(name string) ([]fs.DirEntry, error) {
		entries, err := os.ReadDir(name)
		return slices.DeleteFunc(entries, func(e fs.DirEntry) bool { return e.Name() == "Game" }), err
	}
	for i := 0; i < forgetAfter; i++ {
		w.Poll()
	}
	if done, _ := w.Kept(); len(done) != 2 {
		t.Errorf("missing from 3 real listings, not forgotten: %v", done)
	}
}

// The look at the programs that write into what is pending is one for all of them at each look (on Linux a scan
// of /proc), not one each; one held by a writer for a while is said (Writing).
func TestOneLookForWritersAndTheirWait(t *testing.T) {
	dir := t.TempDir()
	var games []string
	for _, name := range []string{"A", "B", "C"} {
		games = append(games, filepath.Join(dir, name))
		dump(t, games[len(games)-1], true)
	}
	calls := 0
	defer func() { held = heldOpen }()
	held = func(roots []string) map[string]bool {
		calls++
		if len(roots) != 3 {
			t.Errorf("looked for %v", roots)
		}
		return map[string]bool{games[0]: true}
	}
	c := &clock{time.Unix(1700000000, 0)}
	w := newWatcher(dir, c)
	found := handOver(t, w, c)
	if calls != 1 || !slices.Equal(names(found), []string{"B", "C"}) {
		t.Fatalf("%d looks, found %v", calls, names(found))
	}
	if len(w.Writing(2*time.Minute)) != 0 {
		t.Error("said at once")
	}
	held = func(roots []string) map[string]bool { return map[string]bool{games[0]: true} }
	for i := 0; i < 4; i++ {
		c.pass(61 * time.Second)
		w.Poll()
	}
	if got := w.Writing(2 * time.Minute); !slices.Equal(got, []string{games[0]}) {
		t.Errorf("writing: %v", got)
	}
	held = func([]string) map[string]bool { return nil }
	c.pass(61 * time.Second)
	if found, _ := w.Poll(); len(found) != 1 || len(w.Writing(2*time.Minute)) != 0 {
		t.Errorf("let go: %v, still writing %v", found, w.Writing(2*time.Minute))
	}
}
