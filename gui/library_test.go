// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// libraryFolder makes a folder with two .p5z the engine knows (B.P5Z with an interrupted change), one it does
// not, a text file, and a subfolder named like a .p5z that holds another; the engine is the fake one in "info"
// mode, which keeps its calls. It gives the app, the folder and a function that reads the lines of the calls.
func libraryFolder(t *testing.T) (*App, string, func() []string) {
	t.Helper()
	folder := t.TempDir()
	for name, size := range map[string]int{"A.p5z": 10, "B.P5Z": 20, "bad.p5z": 30, "notes.txt": 5} {
		if err := os.WriteFile(filepath.Join(folder, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(folder, "C.p5z"), 0o755)
	os.WriteFile(filepath.Join(folder, "C.p5z", "D.p5z"), make([]byte, 7), 0o644)
	work := t.TempDir()
	infos := filepath.Join(work, "info.json")
	os.WriteFile(infos, []byte(`{"A.p5z": {"kind": "p5z", "name": "Copper Vale", "title_id": "PPSA01234", "version": "01.002", "interrupted_append": false},
		"B.P5Z": {"kind": "p5z", "name": "Iron Harbor", "title_id": "PPSA01235", "version": "01.000", "interrupted_append": true},
		"other.p5z": {"kind": "p5z", "name": "Salt Fen", "title_id": "PPSA01236", "version": "01.001", "interrupted_append": false}}`), 0o644)
	calls := filepath.Join(work, "calls.txt")
	t.Setenv(fakecore.Env, "info")
	t.Setenv(fakecore.InfoEnv, infos)
	t.Setenv(fakecore.CallsEnv, calls)
	useSettings(t, t.TempDir())
	saved := settings.Defaults()
	saved.LibraryFolder = folder
	if err := settings.Save(saved); err != nil {
		t.Fatal(err)
	}
	read := func() []string {
		data, _ := os.ReadFile(calls)
		var lines []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line != "" {
				lines = append(lines, line)
			}
		}
		return lines
	}
	return &App{core: os.Args[0]}, folder, read
}

func baseNames(items []LibraryItem) []string {
	names := []string{}
	for _, it := range items {
		names = append(names, filepath.Base(it.Path))
	}
	return names
}

// The library lists the .p5z of the first level of its folder, by name without minding the case, and says
// what the engine says of each: an interrupted change, or why one could not be read.
func TestLibraryListsFirstLevelP5z(t *testing.T) {
	a, _, _ := libraryFolder(t)
	view := a.Library(nil)
	if view.Error != "" {
		t.Fatalf("%+v", view)
	}
	if got := strings.Join(baseNames(view.Items), ","); got != "A.p5z,B.P5Z,bad.p5z" {
		t.Fatalf("items: %s", got)
	}
	first, second, bad := view.Items[0], view.Items[1], view.Items[2]
	if first.Name != "Copper Vale" || first.TitleID != "PPSA01234" || first.Version != "01.002" || first.Size != 10 ||
		first.Interrupted || first.Error != "" || first.Other {
		t.Errorf("A: %+v", first)
	}
	if !second.Interrupted || second.Size != 20 {
		t.Errorf("B: %+v", second)
	}
	if bad.Error == "" || bad.Interrupted {
		t.Errorf("bad: %+v", bad)
	}
}

type fileState struct {
	size  int64
	mtime time.Time
}

// folderState is the size and time of everything under folder, each asked of the file itself: on NTFS the
// listing of a folder can give a subfolder's time from before it was last opened, which the walk itself does.
func folderState(t *testing.T, folder string) map[string]fileState {
	t.Helper()
	state := map[string]fileState{}
	filepath.WalkDir(folder, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		state[path] = fileState{info.Size(), info.ModTime()}
		return nil
	})
	return state
}

// Reading the library writes nothing in the folder, and each call of the engine is a read-only one.
func TestLibraryNeverWrites(t *testing.T) {
	a, folder, calls := libraryFolder(t)
	before := folderState(t, folder)
	a.Library(nil)
	after := folderState(t, folder)
	if len(before) != len(after) {
		t.Fatalf("before %v, after %v", before, after)
	}
	for path, was := range before {
		if now, ok := after[path]; !ok || now != was {
			t.Errorf("%s changed: %v, %v", path, was, now)
		}
	}
	lines := calls()
	if len(lines) == 0 {
		t.Fatal("the engine was not asked")
	}
	for _, line := range lines {
		if !strings.Contains("\t"+line+"\t", "\t--read-only\t") {
			t.Errorf("not read-only: %q", line)
		}
	}
}

// A file read once is not read again until its size or time changes.
func TestLibraryCachesBySizeAndTime(t *testing.T) {
	a, folder, calls := libraryFolder(t)
	a.Library(nil)
	seen := len(calls())
	a.Library(nil)
	if n := len(calls()); n != seen {
		t.Fatalf("a second look asked the engine %d more times", n-seen)
	}
	f, err := os.OpenFile(filepath.Join(folder, "A.p5z"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{1})
	f.Close()
	view := a.Library(nil)
	infoCalls := 0
	for _, line := range calls()[seen:] {
		fields := strings.Split(line, "\t")
		if fields[0] == "info" {
			infoCalls++
		}
		if !strings.Contains(line, "A.p5z") || strings.Contains(line, "B.P5Z") || strings.Contains(line, "bad.p5z") {
			t.Errorf("a call for another file: %q", line)
		}
	}
	if infoCalls != 1 {
		t.Errorf("%d info calls after A.p5z grew, expected 1: %q", infoCalls, calls()[seen:])
	}
	if view.Items[0].Size != 11 {
		t.Errorf("size %d", view.Items[0].Size)
	}
}

// Files from elsewhere come last, marked; one already in the folder is not repeated.
func TestLibraryOthers(t *testing.T) {
	a, folder, _ := libraryFolder(t)
	elsewhere := filepath.Join(t.TempDir(), "other.p5z")
	os.WriteFile(elsewhere, make([]byte, 40), 0o644)
	view := a.Library([]string{elsewhere, filepath.Join(folder, "A.p5z"), elsewhere})
	if got := strings.Join(baseNames(view.Items), ","); got != "A.p5z,B.P5Z,bad.p5z,other.p5z" {
		t.Fatalf("items: %s", got)
	}
	last := view.Items[3]
	if !last.Other || last.Name != "Salt Fen" || last.Size != 40 || last.Error != "" {
		t.Errorf("%+v", last)
	}
	for _, it := range view.Items[:3] {
		if it.Other {
			t.Errorf("in the folder: %+v", it)
		}
	}
}

// With no folder set, the view says so and still lists the others.
func TestLibraryNoFolder(t *testing.T) {
	a, _, _ := libraryFolder(t)
	if err := settings.Save(settings.Defaults()); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "other.p5z")
	os.WriteFile(elsewhere, make([]byte, 40), 0o644)
	view := a.Library([]string{elsewhere})
	if view.Error != "no_folder" || view.Folder != "" || len(view.Items) != 1 || !view.Items[0].Other {
		t.Errorf("%+v", view)
	}
}

// A folder that cannot be listed says so.
func TestLibraryUnreadable(t *testing.T) {
	a, folder, _ := libraryFolder(t)
	saved := settings.Defaults()
	saved.LibraryFolder = filepath.Join(folder, "gone")
	settings.Save(saved)
	if view := a.Library(nil); view.Error != "unreadable" || len(view.Items) != 0 {
		t.Errorf("%+v", view)
	}
}

// What was read of a file (or why it could not be) is read again when the passcode changes, though the file
// did not: a wrong passcode must not leave its error on screen.
func TestLibraryRereadsAfterPasscodeChange(t *testing.T) {
	t.Cleanup(func() { core.SetPasscode("") })
	core.SetPasscode("")
	a, _, calls := libraryFolder(t)
	a.Library(nil)
	seen := len(calls())
	a.Library(nil)
	if n := len(calls()); n != seen {
		t.Fatalf("a second look asked the engine %d more times", n-seen)
	}
	core.SetPasscode(testPasscode)
	a.Library(nil)
	infoCalls := 0
	for _, line := range calls()[seen:] {
		if strings.HasPrefix(line, "info\t") {
			infoCalls++
		}
	}
	// A.p5z, B.P5Z and bad.p5z: each is read again, with the passcode
	if infoCalls != 3 {
		t.Errorf("%d info calls after the passcode changed, expected 3: %q", infoCalls, calls()[seen:])
	}
	seen = len(calls())
	a.Library(nil)
	if n := len(calls()); n != seen {
		t.Errorf("with the same passcode, %d more calls", n-seen)
	}
}
