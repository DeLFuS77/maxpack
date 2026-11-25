// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// What dump tools leave behind is left out by default (Settings, "Leave out what dump tools leave
// behind"), whatever the page sends: only a game whose own list proves it is not the game's loses
// it, which the engine decides. Settings keeps it when told.
func TestLeftoversFromSettings(t *testing.T) {
	useSettings(t, t.TempDir())
	if settings.Defaults().KeepLeftovers {
		t.Error("the leftovers are kept by default")
	}
	a := &App{fs: realFS{}}
	it := jobs.Item{Path: t.TempDir(), TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	choices := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir(), KeepLeftovers: true}}
	plans, o, err := a.plans("compress", []jobs.Item{it}, choices)
	if err != nil || o.KeepLeftovers || !hasArg(plans[0].Args, "--leave-out-leftovers") {
		t.Fatalf("%v %+v %v", plans[0].Args, o, err)
	}
	saveSettings(t, func(s *settings.Settings) { s.KeepLeftovers = true })
	if plans, _, _ := a.plans("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: jobs.ModeFolder, Dest: t.TempDir()}}); hasArg(plans[0].Args, "--leave-out-leftovers") {
		t.Errorf("kept: %v", plans[0].Args)
	}
	saved, _ := settings.Load()
	if !saved.KeepLeftovers {
		t.Error("the setting is not kept")
	}
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// Details (and the list) carry what the engine says of a game against the list of the original
// package's files: the counts, for the card and its line.
func TestDetailsSayCompleteness(t *testing.T) {
	infos := filepath.Join(t.TempDir(), "info.json")
	os.WriteFile(infos, []byte(`{"Iron Harbor": {"kind": "folder", "title_id": "PPSA10002", "original_size": 100,
		"completeness": {"checkable": true, "reason": "", "listed": 1234, "present": 1231, "missing": 3, "extra_count": 2,
		"extra_bytes": 900, "leftover_count": 1, "leftover_bytes": 800, "extra_paths": ["decrypted/eboot.bin"]}}}`), 0o644)
	t.Setenv(fakecore.Env, "info")
	t.Setenv(fakecore.InfoEnv, infos)
	a := &App{core: os.Args[0]}
	d, err := a.Details(filepath.Join(t.TempDir(), "Iron Harbor"))
	if err != nil {
		t.Fatal(err)
	}
	if c := d.Completeness; c == nil || !c.Checkable || c.Missing != 3 || c.Listed != 1234 || c.LeftoverBytes != 800 {
		t.Errorf("%+v", c)
	}
}

// A report says how complete each game is, in numbers: never a name or a path of its files.
func TestReportSaysCompleteness(t *testing.T) {
	useSettings(t, t.TempDir())
	t.Setenv(fakecore.Env, "info")
	t.Setenv(fakecore.InfoEnv, filepath.Join(t.TempDir(), "none.json"))
	a := &App{core: os.Args[0]}
	games := []ReportGame{
		{Kind: "folder", OriginalSize: 10 << 30, Completeness: &jobs.Completeness{Checkable: true, Listed: 1234, Present: 1234}},
		{Kind: "exfat", OriginalSize: 10 << 30, Completeness: &jobs.Completeness{Checkable: true, Listed: 1234, Present: 1231, Missing: 3,
			ExtraCount: 2, ExtraBytes: 3 << 30, LeftoverCount: 1, LeftoverBytes: 2 << 30}},
		{Kind: "folder", OriginalSize: 10 << 30, Completeness: &jobs.Completeness{Reason: "no_table", LeftoverCount: 4, LeftoverBytes: 1 << 30}},
		{Kind: "folder", OriginalSize: 10 << 30, Completeness: &jobs.Completeness{Reason: "unknown_format"}},
		{Kind: "folder", OriginalSize: 10 << 30},
	}
	text := a.reportText(ReportRequest{Section: "details", Games: games})
	for _, want := range []string{
		"Completeness: complete, all 1234 files of the original package\n",
		"Completeness: incomplete, 3 of the 1234 files of the original package missing\n",
		"Not in the original package: 2 files, 3.0 GiB (3221225472 bytes); left by dump tools: 1 file, 2.0 GiB (2147483648 bytes)\n",
		"Completeness: not checked (no_table)\n",
		"Left by dump tools, not proven: 4 files, 1.0 GiB (1073741824 bytes)\n",
		"Completeness: not checked (unknown_format)\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report does not say %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "Completeness: ") != 4 {
		t.Errorf("a game the engine said nothing of has a completeness:\n%s", text)
	}
}
