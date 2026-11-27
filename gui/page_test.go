// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/jobs"
)

// pageEvents keeps what the App tells the page, and waits for the end of a queue.
type pageEvents struct {
	mutex    sync.Mutex
	items    []itemEvent
	progress []progressEvent
	paused   []bool
	chimes   int
	done     chan jobs.Summary
}

func newPageEvents() *pageEvents { return &pageEvents{done: make(chan jobs.Summary, 4)} }

func (p *pageEvents) emit(event string, data any) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	switch event {
	case "job:item":
		p.items = append(p.items, data.(itemEvent))
	case "job:progress":
		p.progress = append(p.progress, data.(progressEvent))
	case "job:paused":
		p.paused = append(p.paused, data.(pausedEvent).Paused)
	case "job:chime":
		p.chimes++
	case "job:done":
		p.done <- data.(jobs.Summary)
	}
}

func (p *pageEvents) wait(t *testing.T) jobs.Summary {
	t.Helper()
	select {
	case sum := <-p.done:
		return sum
	case <-time.After(2 * time.Minute):
		t.Fatal("the queue did not finish")
	}
	return jobs.Summary{}
}

// states gives the states told of game i, in order.
func (p *pageEvents) states(i int) []string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	var states []string
	for _, e := range p.items {
		if e.I == i {
			states = append(states, e.State)
		}
	}
	return states
}

func (p *pageEvents) last(i int) itemEvent {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	var last itemEvent
	for _, e := range p.items {
		if e.I == i {
			last = e
		}
	}
	return last
}

func quietExtras() extras {
	return extras{Awake: func() func() { return func() {} }, Taskbar: func(string, uint64, uint64) {}, Notify: func(string, string) bool { return true }}
}

func pageApp(engine string, events *pageEvents) *App {
	return &App{core: engine, fs: realFS{}, lang: func() string { return "es" }, extras: quietExtras(), emit: events.emit}
}

func makeDump(t *testing.T, parent, folder, id, title string) string {
	t.Helper()
	dump := filepath.Join(parent, folder)
	if err := os.MkdirAll(filepath.Join(dump, "sce_sys"), 0o755); err != nil {
		t.Fatal(err)
	}
	param := `{"titleId":"` + id + `","titleName":"` + title + `","contentVersion":"01.000.000"}`
	if err := os.WriteFile(filepath.Join(dump, "sce_sys", "param.json"), []byte(param), 0o644); err != nil {
		t.Fatal(err)
	}
	return dump
}

func touch(t *testing.T, parent, name string, size int) string {
	t.Helper()
	path := filepath.Join(parent, name)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExpandSections(t *testing.T) {
	root := t.TempDir()
	dump := makeDump(t, root, "Juego A", "PPSA00001", "Juego A")
	image := touch(t, root, "b.exfat", 1)
	p5z := touch(t, root, "c.p5z", 1)
	upper := touch(t, root, "D.P5Z", 1)
	touch(t, root, "notas.txt", 1)
	other := filepath.Join(root, "otra")
	os.Mkdir(other, 0o755)
	deep := makeDump(t, other, "Profundo", "PPSA00002", "Profundo")
	makeDump(t, deep, "Dentro", "PPSA00003", "Dentro") // inside a dump: the dump is the game
	gone := filepath.Join(root, "no-existe.exfat")
	// the images of ShadowMountPlus, in any case, one level below too
	ufs2 := touch(t, root, "e.ffpkg", 1)
	pfs := touch(t, root, "f.FFPFS", 1)
	pfsc := touch(t, root, "g.ffpfsc", 1)
	touch(t, root, "g.ffpfsc.maxpack-tmp", 1)
	nested := touch(t, other, "h.ffpfsc", 1)
	images := []string{image, ufs2, pfs, pfsc}

	a := &App{}
	cases := []struct {
		section string
		paths   []string
		want    []string
	}{
		// a folder that is not a game: the dumps and images one level below, by name
		{"compress", []string{root}, append([]string{dump}, images...)},
		{"compress", []string{dump}, []string{dump}},
		{"compress", []string{image, p5z, gone}, []string{image}},
		{"compress", []string{pfsc, ufs2, p5z, pfs}, []string{pfsc, ufs2, pfs}},
		{"compress", []string{root, dump, image, root}, append([]string{dump}, images...)},
		{"compress", []string{other}, []string{deep, nested}},
		{"decompress", []string{root}, []string{upper, p5z}},
		{"decompress", []string{p5z, dump, image, pfsc}, []string{p5z}},
		{"verify", []string{root}, []string{upper, dump, p5z}},
		{"verify", []string{pfsc, ufs2}, []string{}},
		{"details", []string{root}, []string{upper, dump, image, p5z, ufs2, pfs, pfsc}},
		{"rename", []string{root}, []string{upper, dump, image, p5z, ufs2, pfs, pfsc}},
		{"details", []string{dump + string(filepath.Separator)}, []string{dump}},
		{"settings", []string{root}, []string{}},
		{"compress", nil, []string{}},
	}
	for _, c := range cases {
		got := a.Expand(c.paths, c.section)
		if got == nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %v:\n  %v\n  expected %v", c.section, c.paths, got, c.want)
		}
	}
}

func TestDescribeIcon(t *testing.T) {
	engine, fixtures := needCore(t)
	dump := makeDump(t, t.TempDir(), "Con icono", "ICON00001", "Con icono")
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 300)...)
	if err := os.WriteFile(filepath.Join(dump, "sce_sys", "icon0.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	// the icon goes through a temporary file, which is gone afterwards
	temp := t.TempDir()
	t.Setenv("TMP", temp)
	t.Setenv("TEMP", temp)
	t.Setenv("TMPDIR", temp)

	a := &App{core: engine}
	game, err := a.Describe(dump)
	if err != nil {
		t.Fatal(err)
	}
	if game.TitleID != "ICON00001" || game.Name != "Con icono" || game.Version != "01.000.000" || game.Kind != "folder" || game.Packed || game.Path != dump {
		t.Fatalf("%+v", game)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(game.Icon, prefix) {
		t.Fatalf("icono: %.60q", game.Icon)
	}
	if data, err := base64.StdEncoding.DecodeString(game.Icon[len(prefix):]); err != nil || !bytes.Equal(data, png) {
		t.Fatalf("the icon is not the game's: %v", err)
	}
	if left, _ := os.ReadDir(temp); len(left) != 0 {
		t.Fatalf("temporary files are left: %v", left)
	}

	// without an icon: the game, with no icon and no error
	plain, err := a.Describe(filepath.Join(fixtures, "basic.exfat"))
	if err != nil || plain.Icon != "" || plain.TitleID != "TEST00001" || plain.Name != "basic" || plain.Kind != "exfat" || plain.OriginalSize == 0 {
		t.Fatalf("%+v %v", plain, err)
	}
	if left, _ := os.ReadDir(temp); len(left) != 0 {
		t.Fatalf("temporary files are left: %v", left)
	}
	if _, err := a.Describe(filepath.Join(temp, "no-existe")); err == nil {
		t.Fatal("something that does not exist is described")
	}
}

func TestDetails(t *testing.T) {
	engine, fixtures := needCore(t)
	image := filepath.Join(fixtures, "basic.exfat")
	p5z := filepath.Join(t.TempDir(), "b.p5z")
	if out, err := exec.Command(engine, "pack", image, "-o", p5z, "--p5z", "--level", "1").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	stat, _ := os.Stat(p5z)
	a := &App{core: engine}
	packed, err := a.Details(p5z)
	if err != nil {
		t.Fatal(err)
	}
	if packed.Kind != "p5z" || !packed.Packed || packed.P5zSize != uint64(stat.Size()) || packed.TitleID != "TEST00001" ||
		packed.Files == 0 || packed.OriginalSize == 0 || packed.InstallSize == 0 || packed.Path != p5z || packed.Name != "b" {
		t.Fatalf("%+v", packed)
	}
	// its packs: the files in them and their volumes
	if packed.PackedFiles == 0 || packed.PackedFiles >= uint64(packed.Files) || packed.Volumes < 1 {
		t.Fatalf("packs: %d files in %d volumes", packed.PackedFiles, packed.Volumes)
	}
	plain, err := a.Details(image)
	if err != nil || plain.Kind != "exfat" || plain.Packed || plain.P5zSize != 0 || plain.HasZstdRuntime || plain.Version != "01.000.000" ||
		plain.PackedFiles != 0 || plain.Volumes != 0 {
		t.Fatalf("%+v %v", plain, err)
	}
}

// rootFS is a drive whose root is root, with room and NTFS.
type rootFS struct{ root string }

func (rootFS) Free(string) (uint64, error) { return 1 << 40, nil }
func (rootFS) Type(string) (string, error) { return "NTFS", nil }
func (f rootFS) Root(path string) string   { return f.root }

// A game for the console is where ShadowMountPlus looks only in homebrew/ at the root of its
// drive.
func TestInHomebrew(t *testing.T) {
	root := filepath.Join(t.TempDir(), "USB")
	cases := []struct {
		out  string
		want bool
	}{
		{filepath.Join(root, "homebrew", "PPSA00001"), true},
		{filepath.Join(root, "HomeBrew", "PPSA00001") + string(filepath.Separator), true},
		{filepath.Join(root, "juegos", "PPSA00001"), false},
		{filepath.Join(root, "juegos", "homebrew", "PPSA00001"), false},
		{filepath.Join(root, "PPSA00001"), false},
		{filepath.Join(root, "homebrew"), false},
	}
	for _, c := range cases {
		if got := inHomebrew(c.out, rootFS{root: root}); got != c.want {
			t.Errorf("%s: %v", c.out, got)
		}
	}
	if inHomebrew(filepath.Join(root, "homebrew", "PPSA00001"), realFS{}) {
		t.Error("with no root known, nothing is in homebrew/")
	}
}

// The page is told, of each game made for the console, whether it went to homebrew/ at the root
// of its drive, where ShadowMountPlus sees it, or somewhere it has to be moved from.
func TestFolderTellsHomebrew(t *testing.T) {
	engine, fixtures := needCore(t)
	image, err := (&App{core: engine}).Describe(filepath.Join(fixtures, "basic.exfat"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rooted := range []bool{true, false} {
		dest := t.TempDir()
		events := newPageEvents()
		a := pageApp(engine, events)
		want := filepath.Join(dest, "TEST00001")
		if rooted {
			a.fs = rootFS{root: dest}
			want = filepath.Join(dest, "homebrew", "TEST00001")
		}
		if err := a.Start("compress", []jobs.Item{image.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeFolder, Dest: dest, Level: 1}}); err != nil {
			t.Fatal(err)
		}
		if sum := events.wait(t); sum.Done != 1 {
			t.Fatalf("%+v %+v", sum, events.items)
		}
		if last := events.last(0); last.Out != want || last.Homebrew != rooted || last.Size == 0 {
			t.Fatalf("root %v: %+v", rooted, last)
		}
	}
	// a .p5z is not a folder for the console: nothing to say
	dest := t.TempDir()
	events := newPageEvents()
	a := pageApp(engine, events)
	a.fs = rootFS{root: dest}
	if err := a.Start("compress", []jobs.Item{image.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest, Level: 1}}); err != nil {
		t.Fatal(err)
	}
	events.wait(t)
	if last := events.last(0); last.Homebrew {
		t.Fatalf("%+v", last)
	}
}

func TestRateFromProgress(t *testing.T) {
	const mib = 1 << 20
	near := func(got uint64, want float64) bool { return math.Abs(float64(got)-want) <= 2 }
	var r rater
	t0 := time.Unix(1000, 0)
	steps := []struct {
		i     int
		phase string
		done  uint64
		at    time.Duration
		want  float64
	}{
		{0, "packing", 0, 0, 0},
		{0, "packing", 100 * mib, time.Second, 100 * mib},
		// too close to the one before to be measured: the speed is kept
		{0, "packing", 150 * mib, 1100 * time.Millisecond, 100 * mib},
		// smoothed: one line does not make it jump
		{0, "packing", 300 * mib, 2 * time.Second, 0.7*100*mib + 0.3*200*mib},
		// a new step starts its bytes from 0: the speed is kept, not taken as a fall
		{0, "verifying", 0, 3 * time.Second, 130 * mib},
		{0, "verifying", 50 * mib, 3500 * time.Millisecond, 0.7*130*mib + 0.3*100*mib},
		// and so does the next game
		{1, "packing", 10, 4 * time.Second, 121 * mib},
		{1, "packing", 10 + 121*mib, 5 * time.Second, 121 * mib},
	}
	for n, s := range steps {
		if got := r.add(s.i, s.phase, s.done, t0.Add(s.at)); !near(got, s.want) {
			t.Fatalf("step %d: %d, expected %.0f", n, got, s.want)
		}
	}
}

func TestPlanTellsReplaceSize(t *testing.T) {
	dest := t.TempDir()
	source := makeDump(t, t.TempDir(), "Juego", "PPSA00001", "Juego")
	item := jobs.Item{Path: source, TitleID: "PPSA00001", Name: "Juego", Kind: "folder", OriginalSize: 1000, HasZstdRuntime: true}
	old := touch(t, dest, jobs.P5zName(item), 1234)
	a := &App{fs: realFS{}}

	plans, err := a.Plan("compress", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest, Level: 3}})
	if err != nil || len(plans) != 1 {
		t.Fatalf("%v %v", plans, err)
	}
	p := plans[0]
	if !reflect.DeepEqual(p.Replace, []string{old}) || p.ReplaceSize != 1234 || p.Tmp != old+".maxpack-tmp" || p.Block != "" {
		t.Fatalf("%+v", p)
	}
	// a folder in the way: all its files count
	folder := filepath.Join(dest, "PPSA00001")
	os.MkdirAll(filepath.Join(folder, "sub"), 0o755)
	touch(t, folder, "a", 100)
	touch(t, filepath.Join(folder, "sub"), "b", 50)
	plans, err = a.Plan("compress", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: jobs.ModeFolder, Dest: dest}})
	if err != nil || plans[0].Out != folder || plans[0].ReplaceSize != 150 {
		t.Fatalf("%+v %v", plans, err)
	}
	// a mode that is not of the section is its default: an installer, the original dump
	plans, _ = a.Plan("compress", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: jobs.ModeConsole, Dest: dest}})
	if !strings.Contains(strings.Join(plans[0].Args, " "), "--p5z") {
		t.Fatalf("%v", plans[0].Args)
	}
	packed := jobs.Item{Path: old, TitleID: "PPSA00001", Name: "Juego", Kind: "p5z", OriginalSize: 1000}
	plans, _ = a.Plan("decompress", []jobs.Item{packed}, Choices{Options: jobs.Options{Mode: jobs.ModeVerify, Dest: t.TempDir()}})
	if !strings.Contains(strings.Join(plans[0].Args, " "), "--original") {
		t.Fatalf("%v", plans[0].Args)
	}
	plans, err = a.Plan("verify", []jobs.Item{packed}, Choices{})
	if err != nil || len(plans) != 1 || plans[0].Args[0] != "verify" || plans[0].Out != "" {
		t.Fatalf("%+v %v", plans, err)
	}
	if _, err := a.Plan("details", []jobs.Item{packed}, Choices{}); err == nil {
		t.Fatal("Details does not make plans")
	}
}

func TestStartNeedsConsentToReplace(t *testing.T) {
	dest := t.TempDir()
	source := makeDump(t, t.TempDir(), "Juego", "PPSA00001", "Juego")
	item := jobs.Item{Path: source, TitleID: "PPSA00001", Name: "Juego", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	old := touch(t, dest, jobs.P5zName(item), 5)
	events := newPageEvents()
	a := pageApp(filepath.Join(t.TempDir(), "no-existe.exe"), events)
	// what is in the way and was not accepted is not touched: something new may have come there
	// since the user was asked
	if err := a.Start("compress", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest}}); err != nil {
		t.Fatal(err)
	}
	sum := events.wait(t)
	if sum.Skipped != 1 || sum.Done+sum.Failed != 0 {
		t.Fatalf("%+v", sum)
	}
	if last := events.last(0); last.State != jobs.StateSkipped || last.Detail != "exists" {
		t.Fatalf("%+v", last)
	}
	if data, _ := os.ReadFile(old); string(data) != "xxxxx" {
		t.Fatalf("the old one changed: %q", data)
	}

	engine, fixtures := needCore(t)
	image, err := (&App{core: engine}).Describe(filepath.Join(fixtures, "basic.exfat"))
	if err != nil {
		t.Fatal(err)
	}
	out := touch(t, dest, jobs.P5zName(image.Item), 5)
	events = newPageEvents()
	a = pageApp(engine, events)
	if err := a.Start("compress", []jobs.Item{image.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest, Level: 1}, Replace: []string{out}}); err != nil {
		t.Fatal(err)
	}
	if sum := events.wait(t); sum.Done != 1 {
		t.Fatalf("%+v %+v", sum, events.items)
	}
	stat, err := os.Stat(out)
	if err != nil || stat.Size() <= 5 {
		t.Fatalf("it was not replaced: %v %v", stat, err)
	}
	if last := events.last(0); last.State != jobs.StateOK || last.Out != out || last.Size != uint64(stat.Size()) {
		t.Fatalf("%+v", last)
	}
}

func TestStartTellsThePage(t *testing.T) {
	engine, fixtures := needCore(t)
	image, err := (&App{core: engine}).Describe(filepath.Join(fixtures, "basic.exfat"))
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	events := newPageEvents()
	a := pageApp(engine, events)
	if err := a.Start("compress", []jobs.Item{image.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest, Level: 3}}); err != nil {
		t.Fatal(err)
	}
	sum := events.wait(t)
	if sum.Done != 1 || sum.Saved == 0 || len(sum.Top) != 1 {
		t.Fatalf("%+v", sum)
	}
	if got := strings.Join(events.states(0), " "); got != "waiting packing verifying ok" {
		t.Fatalf("estados: %s", got)
	}
	out := filepath.Join(dest, jobs.P5zName(image.Item))
	stat, _ := os.Stat(out)
	if last := events.last(0); last.Out != out || stat == nil || last.Size != uint64(stat.Size()) {
		t.Fatalf("%+v", last)
	}
	events.mutex.Lock()
	progress := append([]progressEvent{}, events.progress...)
	events.mutex.Unlock()
	if len(progress) == 0 {
		t.Fatal("no progress")
	}
	phases := map[string]bool{}
	for _, p := range progress {
		phases[p.Phase] = true
		if p.I != 0 || p.Total == 0 || p.Done > p.Total {
			t.Fatalf("%+v", p)
		}
	}
	if !phases["packing"] || !phases["verifying"] {
		t.Fatalf("fases: %v", phases)
	}
	// the end of every step is told, whatever the pace of the lines
	if end := progress[len(progress)-1]; end.Done != end.Total || end.Phase != "verifying" {
		t.Fatalf("%+v", end)
	}

	// the .p5z made, verified and then unpacked to the original dump
	packed, err := a.Describe(out)
	if err != nil || !packed.Packed {
		t.Fatalf("%+v %v", packed, err)
	}
	events = newPageEvents()
	a = pageApp(engine, events)
	if err := a.Start("verify", []jobs.Item{packed.Item}, Choices{}); err != nil {
		t.Fatal(err)
	}
	if sum := events.wait(t); sum.Done != 1 || strings.Join(events.states(0), " ") != "waiting verifying ok" {
		t.Fatalf("%+v %v", sum, events.states(0))
	}
	unpacked := t.TempDir()
	events = newPageEvents()
	a = pageApp(engine, events)
	if err := a.Start("decompress", []jobs.Item{packed.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeOriginal, Dest: unpacked}}); err != nil {
		t.Fatal(err)
	}
	if sum := events.wait(t); sum.Done != 1 || strings.Join(events.states(0), " ") != "waiting unpacking ok" {
		t.Fatalf("%+v %v", sum, events.items)
	}
	if last := events.last(0); last.Out == "" || last.Size != image.OriginalSize {
		t.Fatalf("%+v (original %d)", last, image.OriginalSize)
	}
}

func TestStartOneAtATime(t *testing.T) {
	events := newPageEvents()
	a := pageApp(filepath.Join(t.TempDir(), "no-existe.exe"), events)
	awake := make(chan struct{})
	a.extras.Awake = func() func() { <-awake; return func() {} }
	item := jobs.Item{Path: t.TempDir(), TitleID: "PPSA00001", Name: "x", Kind: "folder", OriginalSize: 1, HasZstdRuntime: true}
	o := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir()}}
	if err := a.Start("compress", []jobs.Item{item}, o); err != nil {
		t.Fatal(err)
	}
	if err := a.Start("compress", []jobs.Item{item}, o); err == nil {
		t.Fatal("two queues at once")
	}
	a.Cancel()
	close(awake)
	if sum := events.wait(t); sum.Cancelled != 1 {
		t.Fatalf("%+v", sum)
	}
	// once told the end, the next one starts
	if err := a.Start("compress", []jobs.Item{item}, o); err != nil {
		t.Fatal(err)
	}
	events.wait(t)
	a.Cancel() // with nothing running, nothing happens
	if err := a.Start("details", []jobs.Item{item}, o); err == nil {
		t.Fatal("Details does not start anything")
	}
}

// The dialog of Add files shows the images ShadowMountPlus mounts, the PS5 packages and the .zstd images
// wherever a section takes them.
func TestSectionPatterns(t *testing.T) {
	images := "*.exfat;*.ffpkg;*.ffpfs;*.ffpfsc;*.pkg;*.zstd"
	for section, want := range map[string]string{"compress": images, "details": images + ";*.p5z", "rename": images + ";*.p5z", "decompress": "*.p5z", "verify": "*.p5z"} {
		if got := sectionPatterns[section]; got != want {
			t.Errorf("%s: %q, want %q", section, got, want)
		}
	}
}

// Details of an image says what it is by its contents (a renamed one too): its kind, what a
// container holds, whether it is compressed, and what its file takes, which for a compressed one
// is less than the game in it.
func TestDetailsOfImages(t *testing.T) {
	engine, fixtures := needCore(t)
	useSettings(t, t.TempDir())
	renamed := filepath.Join(t.TempDir(), "renamed.exfat")
	data, err := os.ReadFile(filepath.Join(fixtures, "basic.ffpkg"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(renamed, data, 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{core: engine}
	for _, c := range []struct {
		path, kind, inner string
		compressed        bool
	}{
		{filepath.Join(fixtures, "basic.exfat"), "exfat", "", false},
		{filepath.Join(fixtures, "basic.ffpkg"), "ufs2", "", false},
		{renamed, "ufs2", "", false},
		{filepath.Join(fixtures, "basic.ffpfs"), "pfs", "", false},
		{filepath.Join(fixtures, "basic_c.ffpfs"), "pfs", "", true},
		{filepath.Join(fixtures, "mkpfs_exfat.ffpfsc"), "pfsc", "exfat", true},
		{filepath.Join(fixtures, "mkpfs_ufs2.ffpfsc"), "pfsc", "ufs2", true},
		{filepath.Join(fixtures, "mkpfs_pfs.ffpfsc"), "pfsc", "pfs", true},
	} {
		stat, err := os.Stat(c.path)
		if err != nil {
			t.Fatal(err)
		}
		d, err := a.Details(c.path)
		if err != nil {
			t.Errorf("%s: %v", c.path, err)
			continue
		}
		if d.Kind != c.kind || d.Inner != c.inner || d.Compressed != c.compressed || d.Packed || d.ImageSize != uint64(stat.Size()) ||
			d.TitleID == "" || d.Files == 0 || d.OriginalSize == 0 {
			t.Errorf("%s: %+v (file %d bytes)", filepath.Base(c.path), d, stat.Size())
		}
		if c.compressed && d.ImageSize >= d.OriginalSize {
			t.Errorf("%s: the image takes %d, the game %d", filepath.Base(c.path), d.ImageSize, d.OriginalSize)
		}
	}
	// a folder and a .p5z are not images
	dump := makeDump(t, t.TempDir(), "Dump", "PPSA00001", "Dump")
	if d, err := a.Details(dump); err != nil || d.ImageSize != 0 || d.Kind != "folder" {
		t.Errorf("a folder: %+v %v", d, err)
	}
	// Rename gives the renamed one the extension of what it is
	game, err := a.Describe(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if plans := a.RenamePlan([]jobs.Item{game.Item}); filepath.Ext(plans[0].To) != ".ffpkg" {
		t.Errorf("rename: %+v", plans[0])
	}
}

// Details grows a .p5z: Add DLC… and Update with a new dump… are queues of their own there, in the
// .p5z's place; it does nothing else.
func TestDetailsGrowsAP5z(t *testing.T) {
	dir := t.TempDir()
	p5z := touch(t, dir, "Game [PPSA00001].p5z", 10)
	item := jobs.Item{Path: p5z, TitleID: "PPSA00001", Name: "Game", Kind: "p5z", OriginalSize: 1000, Grow: []string{filepath.Join(dir, "d.pkg")}}
	a := &App{fs: realFS{}}
	plans, err := a.Plan("details", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: jobs.ModeAddDLC}})
	if err != nil || len(plans) != 1 || plans[0].Args[0] != "add" || plans[0].Out != p5z || plans[0].Tmp != "" {
		t.Fatalf("%+v %v", plans, err)
	}
	if plans, err = a.Plan("details", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: jobs.ModeUpdate}}); err != nil || plans[0].Args[0] != "update" {
		t.Fatalf("%+v %v", plans, err)
	}
	if _, err := a.Plan("details", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: jobs.ModeVerify}}); err == nil {
		t.Fatal("Details verifies nothing")
	}

	engine, fixtures := needCore(t)
	out := filepath.Join(t.TempDir(), "Game [PPSA00000].p5z")
	if _, _, err := core.RunWith(context.Background(), engine, []string{"pack", filepath.Join(fixtures, "game"), "-o", out, "--p5z", "--threads", "2"}, core.RunOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	game, err := (&App{core: engine}).Details(out)
	if err != nil || game.Grown != 0 {
		t.Fatalf("%+v %v", game, err)
	}
	game.Item.Grow = []string{filepath.Join(fixtures, "dlc_data")}
	events := newPageEvents()
	a = pageApp(engine, events)
	if err := a.Start("details", []jobs.Item{game.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeAddDLC}}); err != nil {
		t.Fatal(err)
	}
	if sum := events.wait(t); sum.Done != 1 || strings.Join(events.states(0), " ") != "waiting growing verifying ok" {
		t.Fatalf("%+v %v", sum, events.items)
	}
	if last := events.last(0); last.Detail != "grown" || last.Out != "" {
		t.Fatalf("%+v", last)
	}
	if game, err = (&App{core: engine}).Details(out); err != nil || game.Grown != 1 || len(game.DLC) != 1 {
		t.Fatalf("%+v %v", game, err)
	}
}
