// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
)

// What opens the window, and with what: nothing; only files or folders there (dropped on MaxPack,
// sent to it with Send to); --open and a .p5z (a double click); --section, a section of the menu
// of the Explorer and files. The files of the Explorer open the window even when one is gone
// meanwhile. Anything else is for maxpack-core, as before.
func TestWindowLaunch(t *testing.T) {
	dir := t.TempDir()
	p5z, folder, gone := filepath.Join(dir, "a.p5z"), filepath.Join(dir, "Game"), filepath.Join(dir, "gone.p5z")
	os.WriteFile(p5z, []byte("x"), 0o644)
	os.Mkdir(folder, 0o755)
	for _, c := range []struct {
		args   []string
		window bool
		want   launch
	}{
		{nil, true, launch{}},
		{[]string{p5z, folder}, true, launch{files: []string{p5z, folder}}},
		{[]string{"--open", p5z}, true, launch{files: []string{p5z}, section: "details"}},
		{[]string{"--open", gone}, true, launch{files: []string{}, section: "details"}},
		{[]string{"--section", "verify", p5z, gone}, true, launch{files: []string{p5z}, section: "verify"}},
		{[]string{"--section", "details", p5z}, true, launch{files: []string{p5z}, section: "details"}},
		{[]string{"--section", "compress", folder}, true, launch{files: []string{folder}, section: "compress"}},
		// the menu has no other sections, and needs files
		{[]string{"--section", "rename", p5z}, false, launch{}},
		{[]string{"--section", "verify"}, false, launch{}},
		{[]string{"--section"}, false, launch{}},
		{[]string{"--open"}, false, launch{}},
		{[]string{"--open", "--version"}, true, launch{files: []string{}, section: "details"}},
		// the command line
		{[]string{"info", p5z}, false, launch{}},
		{[]string{"--version"}, false, launch{}},
		{[]string{p5z, "--json"}, false, launch{}},
		{[]string{"pack", folder, "-o", p5z}, false, launch{}},
		{[]string{gone}, false, launch{}},
		{[]string{"--installer", "add"}, false, launch{}},
	} {
		got, ok := windowLaunch(c.args)
		if ok != c.window || ok && !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %+v %v, want %+v %v", c.args, got, ok, c.want, c.window)
		}
	}
}

// The paths given to another MaxPack are made whole from the folder it was started in, so the
// window that takes them finds them: not the options, nor the section after --section.
func TestAbsolute(t *testing.T) {
	folder := t.TempDir()
	other := filepath.Join(t.TempDir(), "b.p5z")
	for _, c := range []struct{ args, want []string }{
		{[]string{"a.p5z", other}, []string{filepath.Join(folder, "a.p5z"), other}},
		{[]string{"--section", "verify", "a.p5z"}, []string{"--section", "verify", filepath.Join(folder, "a.p5z")}},
		{[]string{"--open", "a.p5z"}, []string{"--open", filepath.Join(folder, "a.p5z")}},
		{[]string{"--section", "compress", "Game", other}, []string{"--section", "compress", filepath.Join(folder, "Game"), other}},
		{[]string{}, []string{}},
	} {
		if got := absolute(c.args, folder); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %q, want %q", c.args, got, c.want)
		}
	}
	if got := absolute([]string{"a.p5z"}, ""); !reflect.DeepEqual(got, []string{"a.p5z"}) {
		t.Errorf("without a folder: %q", got)
	}
}

// The hand-off: what another MaxPack was started with (its arguments and its folder, as Wails
// hands them over) goes to the space it asked for, and the window comes to the front; arguments
// for the command line, or files not there, add nothing.
func TestSecondInstanceHandOff(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.p5z", "b.exfat"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	os.Mkdir(filepath.Join(dir, "Game"), 0o755)
	var fronted atomic.Int32
	before := bringToFront
	bringToFront = func() { fronted.Add(1) }
	t.Cleanup(func() { bringToFront = before })
	var sent []FilesAdd
	a := &App{emit: func(event string, data any) {
		if event == "files:add" {
			sent = append(sent, data.(FilesAdd))
		}
	}}
	a.PendingFiles()
	for _, c := range []struct {
		args []string
		want *FilesAdd
	}{
		{[]string{"--section", "verify", "a.p5z"}, &FilesAdd{Verify: []string{filepath.Join(dir, "a.p5z")}, Show: "verify"}},
		{[]string{"--open", "a.p5z"}, &FilesAdd{Details: []string{filepath.Join(dir, "a.p5z")}, Show: "details"}},
		{[]string{"--section", "compress", "Game"}, &FilesAdd{Compress: []string{filepath.Join(dir, "Game")}, Show: "compress"}},
		{[]string{"a.p5z", "b.exfat", "Game"}, &FilesAdd{Compress: []string{filepath.Join(dir, "b.exfat"), filepath.Join(dir, "Game")}, Details: []string{filepath.Join(dir, "a.p5z")}}},
		{[]string{"info", "a.p5z"}, nil},
		{[]string{"--section", "verify", "gone.p5z"}, nil},
	} {
		sent = nil
		a.secondInstance(wailsoptions.SecondInstanceData{Args: c.args, WorkingDirectory: dir})
		switch {
		case c.want == nil && len(sent) != 0:
			t.Errorf("%q: sent %+v", c.args, sent)
		case c.want != nil && (len(sent) != 1 || !reflect.DeepEqual(sent[0], *c.want)):
			t.Errorf("%q: sent %+v, want %+v", c.args, sent, *c.want)
		}
	}
	// every launch brings the window to the front, also one that adds nothing
	for deadline := time.Now().Add(5 * time.Second); fronted.Load() != 6 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	if got := fronted.Load(); got != 6 {
		t.Errorf("brought to the front %d times", got)
	}
}
