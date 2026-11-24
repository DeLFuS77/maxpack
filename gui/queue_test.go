// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// smallFS is a drive with little room: 100 bytes free, its root the folder given.
type smallFS struct{ root string }

func (s smallFS) Free(string) (uint64, error) { return 100, nil }
func (s smallFS) Type(string) (string, error) { return "NTFS", nil }
func (s smallFS) Root(string) string          { return s.root }

func saveSettings(t *testing.T, change func(*settings.Settings)) {
	t.Helper()
	s := settings.Defaults()
	change(&s)
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
}

// The plans of a queue take from Settings what the page does not choose: the names of the files and
// the priority of the engine. The originals go only when compressing, as the page asked and as
// Settings says (to the recycle bin, or for good).
func TestQueueOptionsFromSettings(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) {
		s.LowPriority, s.NameTemplate, s.DeletePermanently = true, "{title} [{id}] [v{version}]", true
	})
	a := &App{fs: realFS{}}
	it := jobs.Item{Path: t.TempDir(), TitleID: "PPSA10001", Name: "Starfall Odyssey", Version: "01.000.021", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	dest := t.TempDir()
	plans, o, err := a.plans("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest}, DeleteOriginals: true, DeleteNoBin: true})
	if err != nil || !o.LowPriority || o.Originals != jobs.OriginalsDelete || o.OriginalsNoBin {
		t.Fatalf("%+v %v", o, err)
	}
	if filepath.Base(plans[0].Out) != "Starfall Odyssey [PPSA10001] [v01.000.021].p5z" {
		t.Errorf("the template of Settings: %q", plans[0].Out)
	}
	saveSettings(t, func(s *settings.Settings) { s.DeletePermanently = false })
	if _, o, _ := a.plans("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Dest: dest}, DeleteOriginals: true, DeleteNoBin: true}); o.Originals != jobs.OriginalsTrash || !o.OriginalsNoBin {
		t.Errorf("to the recycle bin: %+v", o)
	}
	if _, o, _ := a.plans("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Dest: dest, Originals: jobs.OriginalsDelete}}); o.Originals != jobs.OriginalsKeep {
		t.Errorf("not asked by the page: %+v", o)
	}
	for _, section := range []string{"decompress", "verify"} {
		if _, o, _ := a.plans(section, []jobs.Item{it}, Choices{Options: jobs.Options{Dest: dest}, DeleteOriginals: true}); o.Originals != jobs.OriginalsKeep {
			t.Errorf("%s takes originals away: %+v", section, o)
		}
	}
}

// Before a queue that sends the originals to the recycle bin starts, the page is told what each
// one takes, and which ones cannot go there (their drive has none, or they do not fit in it).
func TestPlanTellsTheOriginals(t *testing.T) {
	useSettings(t, t.TempDir())
	source := makeDump(t, t.TempDir(), "Starfall", "PPSA10001", "Starfall Odyssey")
	touch(t, source, "eboot.bin", 1000)
	it := jobs.Item{Path: source, TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 1000, HasZstdRuntime: true}
	a := &App{fs: realFS{}, trashCheck: func(string) string { return "too_big" }}
	opts := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir()}, DeleteOriginals: true}
	views, err := a.Plan("compress", []jobs.Item{it}, opts)
	if err != nil || views[0].SourceSize != jobs.SizeOf(source) || views[0].SourceSize < 1000 || views[0].NoBin != "too_big" {
		t.Fatalf("%+v %v", views, err)
	}
	saveSettings(t, func(s *settings.Settings) { s.DeletePermanently = true })
	if views, _ := a.Plan("compress", []jobs.Item{it}, opts); views[0].NoBin != "" || views[0].SourceSize == 0 {
		t.Errorf("deleted for good, the bin is not asked: %+v", views[0])
	}
	opts.DeleteOriginals = false
	if views, _ := a.Plan("compress", []jobs.Item{it}, opts); views[0].NoBin != "" || views[0].SourceSize != 0 {
		t.Errorf("originals kept: %+v", views[0])
	}
}

// The space of the whole queue is told before it starts, by drive, with the numbers.
func TestSpaceOfAQueue(t *testing.T) {
	useSettings(t, t.TempDir())
	dest := t.TempDir()
	a := &App{fs: smallFS{root: dest}}
	items := []jobs.Item{
		{Path: t.TempDir(), TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 60, HasZstdRuntime: true},
		{Path: t.TempDir(), TitleID: "PPSA10002", Name: "Iron Harbor", Kind: "folder", OriginalSize: 50, HasZstdRuntime: true},
	}
	short, err := a.Space("compress", items, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest}})
	if err != nil || len(short) != 1 || short[0].Need != 110 || short[0].Free != 100 || short[0].Root != dest {
		t.Fatalf("%+v %v", short, err)
	}
	if short, _ := a.Space("compress", items[:1], Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest}}); len(short) != 0 {
		t.Errorf("one fits: %+v", short)
	}
	if short, _ := a.Space("verify", items, Choices{}); len(short) != 0 {
		t.Errorf("verify: %+v", short)
	}
}

// A queue with the originals to delete for good: each goes once its new one is in its place, and
// the log in the folder of the settings says what was removed. Without it, or when decompressing,
// nothing goes.
func TestStartRemovesOriginals(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	folder := t.TempDir()
	useSettings(t, folder)
	saveSettings(t, func(s *settings.Settings) { s.DeletePermanently = true })
	for _, c := range []struct {
		section string
		ask     bool
		gone    bool
	}{{"compress", false, false}, {"decompress", true, false}, {"compress", true, true}} {
		source := makeDump(t, t.TempDir(), "Starfall", "PPSA10001", "Starfall Odyssey")
		kind := "folder"
		if c.section == "decompress" {
			source = touch(t, t.TempDir(), "Starfall.p5z", 10)
			kind = "p5z"
		}
		it := jobs.Item{Path: source, TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: kind, OriginalSize: 10, HasZstdRuntime: true}
		events := newPageEvents()
		a := pageApp(os.Args[0], events)
		if err := a.Start(c.section, []jobs.Item{it}, Choices{Options: jobs.Options{Dest: t.TempDir()}, DeleteOriginals: c.ask}); err != nil {
			t.Fatal(err)
		}
		sum := events.wait(t)
		_, err := os.Stat(source)
		if gone := errors.Is(err, fs.ErrNotExist); sum.Done != 1 || gone != c.gone || (sum.Deleted == 1) != c.gone {
			t.Errorf("%s ask=%v: %+v, gone %v", c.section, c.ask, sum, gone)
		}
		if c.gone && !strings.Contains(events.last(0).Detail, "original_deleted") {
			t.Errorf("the detail: %+v", events.last(0))
		}
	}
	log, err := os.ReadFile(filepath.Join(folder, "maxpack.log"))
	if err != nil || strings.Count(string(log), "Deleted for good") != 1 || !strings.Contains(string(log), "Starfall") {
		t.Errorf("log: %q %v", log, err)
	}
}

// What a queue cut short left apart is offered when MaxPack opens again, with what it takes, and
// taken away when the user says so.
func TestLeftoversOffered(t *testing.T) {
	folder := t.TempDir()
	useSettings(t, folder)
	a := &App{}
	if found := a.Leftovers(); len(found) != 0 {
		t.Fatalf("%+v", found)
	}
	tmp := touch(t, t.TempDir(), "Starfall Odyssey [PPSA10001].p5z"+jobs.TmpSuffix, 7)
	journal := a.journal()
	journal.Add(tmp)
	found := a.Leftovers()
	if len(found) != 1 || found[0].Path != tmp || found[0].Size != 7 {
		t.Fatalf("%+v", found)
	}
	if failed := a.CleanLeftovers(); len(failed) != 0 {
		t.Fatalf("%v", failed)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, fs.ErrNotExist) || len(a.Leftovers()) != 0 {
		t.Errorf("still there: %v", err)
	}
}

// The report of a game for an issue: what MaxPack, its engine and the PC are, the settings that
// matter and how the game is, never its name, title ID or path, whatever its detail says.
func TestReportHasNoNames(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.LowPriority, s.Level, s.Threads = true, 12, 24 })
	// the engine of the test knows no game: the report says what the page told
	t.Setenv(fakecore.Env, "info")
	t.Setenv(fakecore.InfoEnv, filepath.Join(t.TempDir(), "none.json"))
	out := filepath.Join(t.TempDir(), "Starfall Odyssey [PPSA10001].p5z")
	request := ReportRequest{Section: "compress", Mode: "p5z", Games: []ReportGame{{
		Kind: "folder", OriginalSize: 8 << 30, OutSize: 2400 << 20, Volumes: 1, State: "ok",
		Detail: "old_not_deleted: " + out + ".maxpack-old; original_trashed", Out: out,
	}, {
		Kind: "exfat", OriginalSize: 168 << 30, State: "error",
		Detail: "verify_failed: files_bad:2; tmp_not_deleted: E:\\Starfall Odyssey [PPSA10001].p5z.maxpack-tmp: Access is denied.",
	}, {
		Kind: "p5z", Packed: true, OriginalSize: 69 << 30, P5zSize: 33 << 30, Volumes: 9,
	}}}
	a := &App{core: os.Args[0]}
	text := a.reportText(request)
	for _, banned := range []string{"Starfall", "PPSA10001", "Odyssey", out, "Access is denied", "E:\\"} {
		if strings.Contains(text, banned) {
			t.Errorf("the report says %q:\n%s", banned, text)
		}
	}
	for _, want := range []string{
		"MaxPack: " + version, "Engine: maxpack", "System: ", runtime.GOARCH, "CPU threads: ", "RAM: ",
		"level 12", "threads 24", "low priority on", "originals kept", "{title} [{id}]",
		"dump folder", "exFAT image", "8.0 GiB", "2.3 GiB", "Volumes: 1", "Volumes: 9",
		"Result: ok (old_not_deleted, original_trashed)", "Result: error (verify_failed, files_bad:2, tmp_not_deleted)",
		"| 8.0 → 2.3 GiB (−71 %) | p5z | 1 | ? | ? | ? | ? | ? | ? |  |",
		"| 69.0 → 33.0 GiB (−52 %) | p5z | 9 | ? | ? | ? | ? | ? | ? |  |",
		"github.com/DeLFuS77/maxpack/issues/new?template=bug_report.yml",
		"github.com/DeLFuS77/maxpack-ps5/issues/new?template=game_report.yml",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report does not say %q:\n%s", want, text)
		}
	}
	// without a game, what MaxPack and the PC are
	if text := a.reportText(ReportRequest{}); !strings.Contains(text, "CPU threads: ") || strings.Contains(text, "Game 1") {
		t.Errorf("without games:\n%s", text)
	}
	// it goes to the clipboard
	var copied string
	a.clipboard = func(text string) error { copied = text; return nil }
	if text, err := a.CopyReport(request); err != nil || copied != text || text == "" {
		t.Errorf("copied %d bytes, %v", len(copied), err)
	}
}

// The window opens only its own links, in the browser of the system.
func TestOpenLink(t *testing.T) {
	var opened []string
	a := &App{openURL: func(url string) { opened = append(opened, url) }}
	for _, name := range []string{"bug", "game"} {
		if err := a.OpenLink(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := a.OpenLink("https://example.com"); err == nil {
		t.Error("a link of the page was opened")
	}
	if len(opened) != 2 || !strings.HasSuffix(opened[0], "template=bug_report.yml") || !strings.HasSuffix(opened[1], "template=game_report.yml") {
		t.Errorf("%v", opened)
	}
}

// The preview of a template in Settings: with a game of the list, or an invented one; a folder
// for the console keeps its TITLE_ID; a template that is not one says so.
func TestNamePreview(t *testing.T) {
	a := &App{}
	p := a.NamePreview("{title} [{id}] [{region}] [v{version}]", nil)
	if p.P5z != "Starfall Odyssey [PPSA10001] [EUR] [v01.000.021].p5z" || p.Original != "Starfall Odyssey [PPSA10001] [EUR] [v01.000.021]" || p.Folder != "PPSA10001" || p.Error {
		t.Errorf("%+v", p)
	}
	it := jobs.Item{Path: "x", Name: "Iron Harbor", TitleID: "PPSA10002", Version: "01.000.000", Kind: "exfat"}
	if p := a.NamePreview("{title} [{id}] [{region}] [v{version}]", &it); p.P5z != "Iron Harbor [PPSA10002] [v01.000.000].p5z" || p.Folder != "PPSA10002" {
		t.Errorf("%+v", p)
	}
	if p := a.NamePreview("{name}", nil); !p.Error || p.P5z != "Starfall Odyssey [PPSA10001].p5z" {
		t.Errorf("a bad template: %+v", p)
	}
}

// Rename with the engine of the test: what each game is comes from info, as Details reads it;
// the names are shown before anything changes, and renaming does what was shown.
func TestRenameWithTheFakeCore(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.NameTemplate = "{title} [{id}] [{region}] [v{version}]" })
	dir := t.TempDir()
	p5z := touch(t, dir, "starfall.p5z", 3)
	image := touch(t, dir, "harbor.exfat", 3)
	dump := makeDump(t, dir, "neon", "PPSA10003", "Neon Meridian")
	os.WriteFile(filepath.Join(dump, "sce_sys", "param.json"), []byte(`{"titleId":"PPSA10003","contentId":"JP0003-PPSA10003_00-NEONMERIDIAN0000"}`), 0o644)
	broken := touch(t, dir, "broken.p5z", 3)
	infos := map[string]map[string]any{
		"starfall.p5z": {"kind": "p5z", "title_id": "PPSA10001", "name": "Starfall Odyssey", "version": "01.000.021", "packed": true},
		"harbor.exfat": {"kind": "exfat", "title_id": "PPSA10002", "name": "Iron Harbor", "version": ""},
		"neon":         {"kind": "folder", "title_id": "PPSA10003", "name": "Neon Meridian", "version": "01.000.000"},
	}
	data, _ := json.Marshal(infos)
	file := filepath.Join(t.TempDir(), "infos.json")
	os.WriteFile(file, data, 0o644)
	t.Setenv(fakecore.Env, "info")
	t.Setenv(fakecore.InfoEnv, file)
	a := &App{core: os.Args[0]}
	var items []jobs.Item
	for _, path := range []string{p5z, image, dump, broken} {
		game, err := a.Describe(path)
		if path == broken {
			if err == nil {
				t.Fatal("a game the engine cannot read was read")
			}
			items = append(items, jobs.Item{Path: path})
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, game.Item)
	}
	plans := a.RenamePlan(items)
	want := []string{"Starfall Odyssey [PPSA10001] [v01.000.021].p5z", "Iron Harbor [PPSA10002].exfat", "Neon Meridian [PPSA10003] [JPN] [v01.000.000]", ""}
	for i, w := range want {
		if got := filepath.Base(plans[i].To); w != "" && got != w || w == "" && plans[i].Block != "cannot_read" {
			t.Errorf("%d: %q %q, expected %q", i, got, plans[i].Block, w)
		}
	}
	if _, err := os.Stat(p5z); err != nil {
		t.Fatal("the plan renamed something")
	}
	results, err := a.Rename(items)
	if err != nil {
		t.Fatal(err)
	}
	for i, w := range want[:3] {
		if results[i].State != jobs.StateOK {
			t.Errorf("%d: %+v", i, results[i])
		}
		if _, err := os.Stat(filepath.Join(dir, w)); err != nil {
			t.Errorf("%s is not there: %v", w, err)
		}
	}
	if results[3].State != jobs.StateSkipped || results[3].Detail != "cannot_read" {
		t.Errorf("the broken one: %+v", results[3])
	}
	// nothing is renamed while a queue runs
	a.queue = &queue{}
	if _, err := a.Rename(items); err == nil {
		t.Error("renamed while a queue runs")
	}
}

// Games added to the section whose queue runs join it: told as queued, done after the others with
// its options, and counted in its summary; one taken out before its turn is not done. Another
// section, or a queue that ended, takes nothing.
func TestAppendJoinsTheQueue(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	useSettings(t, t.TempDir())
	dest := t.TempDir()
	events := newPageEvents()
	a := pageApp(os.Args[0], events)
	it := func(id, name string) jobs.Item {
		return jobs.Item{Path: makeDump(t, t.TempDir(), name, id, name), TitleID: id, Name: name, Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	}
	var appended int
	var appendErr, otherErr error
	var removed, removedAgain bool
	done := false
	a.onItem = func(i int, state, detail string) {
		if i != 0 || state != jobs.StatePacking || done {
			return
		}
		done = true
		appended, appendErr = a.Append("compress", []jobs.Item{it("PPSA10002", "Iron Harbor"), it("PPSA10003", "Neon Meridian")}, nil, false)
		_, otherErr = a.Append("decompress", []jobs.Item{it("PPSA10004", "Lost Signal")}, nil, false)
		removed = a.Unqueue("compress", appended+1)
		removedAgain = a.Unqueue("compress", 0)
	}
	if err := a.Start("compress", []jobs.Item{it("PPSA10001", "Starfall Odyssey")}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest}}); err != nil {
		t.Fatal(err)
	}
	sum := events.wait(t)
	if appendErr != nil || appended != 1 || otherErr == nil || !removed || removedAgain {
		t.Fatalf("append %d %v, other %v, removed %v %v", appended, appendErr, otherErr, removed, removedAgain)
	}
	if sum.Done != 2 || sum.Skipped+sum.Failed+sum.Cancelled != 0 {
		t.Fatalf("%+v", sum)
	}
	if got := strings.Join(events.states(1), " "); got != "queued packing verifying ok" {
		t.Errorf("the one added: %s", got)
	}
	if got := strings.Join(events.states(2), " "); got != "queued" {
		t.Errorf("the one taken out: %s", got)
	}
	if last := events.last(1); filepath.Base(last.Out) != "Iron Harbor [PPSA10002].p5z" {
		t.Errorf("not made with the options of the queue: %+v", last)
	}
	if _, err := a.Append("compress", []jobs.Item{it("PPSA10005", "Quiet Tide")}, nil, false); err == nil {
		t.Error("added to a queue that ended")
	}
}

// Several folders, files and dumps given at once (dropped on the window, or chosen together) are
// each taken as one folder is: a game, or the games right below it.
func TestSeveralFoldersAtOnce(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	a1 := makeDump(t, first, "Starfall", "PPSA10001", "Starfall Odyssey")
	a2 := makeDump(t, first, "Harbor", "PPSA10002", "Iron Harbor")
	b1 := makeDump(t, second, "Neon", "PPSA10003", "Neon Meridian")
	single := makeDump(t, t.TempDir(), "Signal", "PPSA10004", "Lost Signal")
	image := touch(t, t.TempDir(), "tide.exfat", 1)
	a := &App{}
	got := a.Expand([]string{first, second, single, image}, "compress")
	want := []string{a2, a1, b1, single, image}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%v\nexpected %v", got, want)
	}
	compress, decompress := routeFiles([]string{first, second, image, touch(t, t.TempDir(), "x.p5z", 1)})
	if len(compress) != 3 || len(decompress) != 1 {
		t.Errorf("dropped: %v %v", compress, decompress)
	}
	// the dialog of several folders gives each to Expand
	a.ctx = context.Background()
	a.chooseFolders = func() ([]string, error) { return []string{first, single}, nil }
	if got, err := a.ChooseFolder("compress"); err != nil || strings.Join(got, "|") != strings.Join([]string{a2, a1, single}, "|") {
		t.Errorf("%v %v", got, err)
	}
}
