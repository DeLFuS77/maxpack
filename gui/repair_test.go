// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/internal/par2/par2test"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// Setup says whether there is maxpack-par2, and what it is: without it (a build of the window
// alone) the page offers no repair data, and says why.
func TestSetupTellsPar2(t *testing.T) {
	useSettings(t, t.TempDir())
	t.Setenv(fakecore.Par2Env, "makes")
	a := &App{par2: os.Args[0]}
	if s := a.Setup(); s.Par2 != os.Args[0] || s.Par2Version != "par2cmdline-turbo version 1.5.0" || s.Par2Error != "" {
		t.Errorf("%+v", s)
	}
	a.par2 = "-"
	if s := a.Setup(); s.Par2 != "" || s.Par2Error == "" {
		t.Errorf("without it: %+v", s)
	}
	t.Setenv("MAXPACK_PAR2", filepath.Join(t.TempDir(), "none"))
	a.par2 = ""
	if s := a.Setup(); s.Par2 != "" || !strings.Contains(s.Par2Error, "MAXPACK_PAR2") {
		t.Errorf("MAXPACK_PAR2 that is not there: %+v", s)
	}
}

// Repair data only for a .p5z and at a percentage the window offers; adding it takes the one of
// Settings when the page sends none; par2 is given the threads of Settings.
func TestPlansOfTheRepairData(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.RepairPercent, s.Threads = 10, 12 })
	a := &App{fs: realFS{}}
	dir := t.TempDir()
	it := jobs.Item{Path: t.TempDir(), TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	for _, c := range []struct {
		mode jobs.Mode
		sent int
		want int
	}{{jobs.ModeP5z, 5, 5}, {jobs.ModeP5z, 3, 0}, {jobs.ModeFolder, 5, 0}} {
		_, o, err := a.plans("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: c.mode, Dest: dir, Repair: c.sent}})
		if err != nil || o.Repair != c.want {
			t.Errorf("%s %d: %d %v", c.mode, c.sent, o.Repair, err)
		}
	}
	p5z := touch(t, dir, "Starfall Odyssey [PPSA10001].p5z", 1000)
	game := jobs.Item{Path: p5z, Kind: "p5z", Name: "Starfall Odyssey", TitleID: "PPSA10001", OriginalSize: 5000}
	_, o, _ := a.plans("verify", []jobs.Item{game}, Choices{Options: jobs.Options{Mode: jobs.ModeProtect}})
	if o.Mode != jobs.ModeProtect || o.Repair != 10 || o.Threads != 12 {
		t.Errorf("protect: %+v", o)
	}
	_, o, _ = a.plans("verify", []jobs.Item{game}, Choices{Options: jobs.Options{Mode: jobs.ModeRepair, Repair: 5}})
	if o.Mode != jobs.ModeRepair || o.Repair != 0 || o.Threads != 12 {
		t.Errorf("repair: %+v", o)
	}
	if _, o, _ = a.plans("verify", []jobs.Item{game}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z}}); o.Mode != jobs.ModeVerify || o.Threads != 0 {
		t.Errorf("verify: %+v", o)
	}
	// a repair needs its .p5z's set; the page is told why it cannot go
	views, err := a.Plan("verify", []jobs.Item{game}, Choices{Options: jobs.Options{Mode: jobs.ModeRepair}})
	if err != nil || views[0].Block != "no_repair_data" {
		t.Errorf("%+v %v", views, err)
	}
}

func TestRepairInfo(t *testing.T) {
	dir := t.TempDir()
	a := &App{}
	p5z := touch(t, dir, "a.p5z", 100_000)
	if got := a.RepairInfo(p5z); got.State != "none" {
		t.Errorf("none: %+v", got)
	}
	par2test.Write(p5z, "a.p5z", 4096, 2)
	// 25 blocks of 4 KiB, 2 of them for repair: 8 %
	got := a.RepairInfo(p5z)
	if got.State != "match" || got.Percent != 8 || got.Blocks != 2 || got.BlockSize != 4096 || got.Capacity != 8192 || got.Bytes == 0 {
		t.Errorf("its own: %+v", got)
	}
	os.WriteFile(p5z, []byte("another"), 0o644)
	if got := a.RepairInfo(p5z); got.State != "other" || got.Percent != 0 {
		t.Errorf("another file's: %+v", got)
	}
	if got := a.RepairInfo(dir); got.State != "" {
		t.Errorf("a folder: %+v", got)
	}
}

// A queue that compresses with repair data tells the page what the set of each game takes, and its
// summary counts them; one that adds repair data to a .p5z does the same.
func TestStartWithRepairData(t *testing.T) {
	useSettings(t, t.TempDir())
	t.Setenv(fakecore.Env, "writes")
	t.Setenv(fakecore.Par2Env, "makes")
	events := newPageEvents()
	a := pageApp(os.Args[0], events)
	a.par2 = os.Args[0]
	dest := t.TempDir()
	it := jobs.Item{Path: t.TempDir(), TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	if err := a.Start("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest, Repair: 5}}); err != nil {
		t.Fatal(err)
	}
	sum := events.wait(t)
	last := events.last(0)
	if sum.Protected != 1 || last.State != jobs.StateOK || last.Repair == 0 || last.Out == "" {
		t.Errorf("%+v %+v", sum, last)
	}
	if got := strings.Join(events.states(0), ","); !strings.Contains(got, "verifying,protecting,ok") {
		t.Errorf("states %s", got)
	}
	// and added to a .p5z there: no output of its own, the size of the set
	events = newPageEvents()
	a = pageApp(os.Args[0], events)
	a.par2 = os.Args[0]
	game := jobs.Item{Path: touch(t, t.TempDir(), "b.p5z", 50_000), Kind: "p5z", Name: "B", OriginalSize: 90_000}
	if err := a.Start("verify", []jobs.Item{game}, Choices{Options: jobs.Options{Mode: jobs.ModeProtect, Repair: 1}}); err != nil {
		t.Fatal(err)
	}
	sum = events.wait(t)
	if last := events.last(0); sum.Protected != 1 || last.State != jobs.StateOK || last.Repair == 0 || last.Out != "" {
		t.Errorf("added: %+v %+v", sum, last)
	}
}

// The bar of a game with repair data has three steps; the one of the repair data goes by the bytes
// of the .p5z it reads, as the verification does.
func TestShareWithRepairData(t *testing.T) {
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
	q := newShare([]jobs.Plan{{Item: jobs.Item{OriginalSize: 9000}}}, jobs.ModeP5z, 5)
	q.item(0, jobs.StateVerifying)
	if got := q.value(); !near(got, 1.0/3) {
		t.Fatalf("verifying: %v", got)
	}
	q.item(0, jobs.StateProtecting)
	q.progress(0, "protecting", 500, 1000)
	if got := q.value(); !near(got, 2.0/3+0.5/3) {
		t.Fatalf("halfway through the repair data: %v", got)
	}
	if steps := stepsOf(jobs.ModeRepair, 0); strings.Join(steps, ",") != "repairing,verifying" {
		t.Errorf("repair: %v", steps)
	}
	if steps := stepsOf(jobs.ModeProtect, 5); strings.Join(steps, ",") != "verifying,protecting" {
		t.Errorf("protect: %v", steps)
	}
	if steps := stepsOf(jobs.ModeFolder, 5); strings.Join(steps, ",") != "packing,verifying" {
		t.Errorf("a folder: %v", steps)
	}
}

// Copy report says whether Compress makes repair data, and how much: no names, no paths.
func TestReportSaysRepairData(t *testing.T) {
	useSettings(t, t.TempDir())
	a := &App{core: os.Args[0]}
	if text := a.reportText(ReportRequest{}); !strings.Contains(text, "repair data off") {
		t.Errorf("%s", text)
	}
	saveSettings(t, func(s *settings.Settings) { s.RepairData, s.RepairPercent = true, 10 })
	if text := a.reportText(ReportRequest{}); !strings.Contains(text, "repair data 10 %") {
		t.Errorf("%s", text)
	}
	// what a game says of its repair data goes as its codes, never with the paths after them
	got := codesOf("repair_data_failed: E:\\Juego [PPSA10001].p5z: Invalid option; old_repair_data_not_deleted: E:\\x.par2")
	if got != "repair_data_failed, old_repair_data_not_deleted" {
		t.Errorf("%q", got)
	}
}
