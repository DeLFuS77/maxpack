// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// testPasscode is the one make_fpkg.py made pass.pkg with.
const testPasscode = "MaxPackTestPasscode-0123456789ab"

// A PS5 package goes where an image goes: Compress, Details and Rename take a .pkg, the routing of the
// Explorer and the command line sends it to Compress, a report says what it is (never its name).
func TestPackageIsTakenAsAnImage(t *testing.T) {
	for _, section := range []string{"compress", "details", "rename"} {
		if !sectionTakes[section].file(`D:\pkgs\Copper Vale.PKG`) {
			t.Errorf("%s does not take a .pkg", section)
		}
		if !strings.Contains(sectionPatterns[section], "*.pkg") {
			t.Errorf("the dialog of %s: %s", section, sectionPatterns[section])
		}
	}
	for _, section := range []string{"decompress", "verify"} {
		if sectionTakes[section].file("game.pkg") {
			t.Errorf("%s takes a .pkg", section)
		}
	}
	if got := spaceOf("Copper Vale.pkg", false, ""); got != "compress" {
		t.Errorf("a .pkg goes to %q", got)
	}
	if got := spaceOf("game.pkg", false, "verify"); got != "compress" {
		t.Errorf("a .pkg asked for Verify goes to %q", got)
	}
	for _, c := range []struct {
		g    ReportGame
		want string
	}{
		{ReportGame{Kind: "fpkg", Compressed: true}, "PS5 package (.pkg), compressed"},
		{ReportGame{Kind: "fpkg"}, "PS5 package (.pkg)"},
	} {
		if got := kindWords(c.g); got != c.want {
			t.Errorf("%+v: %q", c.g, got)
		}
	}
}

// The passcode of Settings goes to every run of the engine that reads a game (info, icon, pack,
// analyze --levels), only when one is given; one that is not 32 printable characters is not kept.
func TestPasscodeGoesToTheEngine(t *testing.T) {
	t.Cleanup(func() { core.SetPasscode("") })
	core.SetPasscode("")
	if args := core.PasscodeArgs(); len(args) != 0 {
		t.Errorf("the default gives %v", args)
	}
	item := jobs.Item{Path: `D:\pkgs\a.pkg`, Kind: "fpkg", TitleID: "PPSA10007", Name: "A", OriginalSize: 10}
	plans := jobs.PlanCompress([]jobs.Item{item}, jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir(), Level: 3}, realFS{})
	if slices.Contains(plans[0].Args, "--passcode") {
		t.Errorf("pack with the default: %v", plans[0].Args)
	}
	core.SetPasscode(testPasscode)
	if args := core.PasscodeArgs(); strings.Join(args, " ") != "--passcode "+testPasscode {
		t.Errorf("%v", args)
	}
	plans = jobs.PlanCompress([]jobs.Item{item}, jobs.Options{Mode: jobs.ModeFolder, Dest: t.TempDir(), Level: 3}, realFS{})
	if i := slices.Index(plans[0].Args, "--passcode"); i < 0 || plans[0].Args[i+1] != testPasscode {
		t.Errorf("pack: %v", plans[0].Args)
	}
	for _, c := range []struct {
		p  string
		ok bool
	}{
		{testPasscode, true}, {strings.Repeat("0", 32), true}, {strings.Repeat("0", 31), false},
		{strings.Repeat("0", 33), false}, {"MaxPackTestPasscode 0123456789ab", false}, {"MaxPackTestPasscode-0123456789añ", false},
	} {
		if core.ValidPasscode(c.p) != c.ok {
			t.Errorf("%q: %v", c.p, !c.ok)
		}
	}
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.Passcode = "short" })
	if saved, _ := settings.Load(); saved.Passcode != "" {
		t.Errorf("a bad passcode is kept: %q", saved.Passcode)
	}
	a := &App{}
	if err := a.SetSettings(settings.Settings{Passcode: testPasscode}); err != nil {
		t.Fatal(err)
	}
	if saved, _ := settings.Load(); saved.Passcode != testPasscode {
		t.Errorf("not kept: %q", saved.Passcode)
	}
	if args := core.PasscodeArgs(); len(args) != 2 {
		t.Errorf("SetSettings does not give it to the engine: %v", args)
	}
	// the report says a passcode was given, never which
	core.SetPasscode("")
	if text := a.reportText(ReportRequest{}); !strings.Contains(text, "FPKG passcode given") || strings.Contains(text, testPasscode) {
		t.Errorf("report: %s", text)
	}
}

// A game kept as a PS5 debug package moves to a .p5z in one step, as the user does it in the window:
// the folder that has it is given to Compress, the engine says what it is (a Kraken-compressed
// package make_fpkg.py made of its game/ folder), the plan counts the game's size and not the
// package's, the queue packs it to the .p5z the engine makes of it, verifies it and deletes the
// package for good, as Settings says; what it says it saved is against the package's file. The .p5z
// has the files of the package (the game folder's and what the builder added).
func TestCompressAPackageThroughTheQueue(t *testing.T) {
	engine, fixtures := needCore(t)
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.DeletePermanently = true })
	t.Cleanup(func() { core.SetPasscode("") })
	dumps := t.TempDir()
	pkg := copyImage(t, filepath.Join(fixtures, "game.pkg"), filepath.Join(dumps, "Test Game.pkg"))
	stat, err := os.Stat(pkg)
	if err != nil {
		t.Fatal(err)
	}
	pkgSize := uint64(stat.Size())

	events := newPageEvents()
	a := pageApp(engine, events)
	a.Setup()
	if found := a.Expand([]string{dumps}, "compress"); len(found) != 1 || found[0] != pkg {
		t.Fatalf("Expand: %v", found)
	}
	game, err := a.Describe(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if game.Kind != "fpkg" || !game.Compressed || game.TitleID != "PPSA00000" || game.ContentID != "UP0000-PPSA00000_00-MAXPACKTEST00000" ||
		game.ImageSize != pkgSize || game.OriginalSize == 0 {
		t.Fatalf("%+v (the package takes %d)", game.Item, pkgSize)
	}
	details, err := a.Details(pkg)
	if err != nil || details.ImageSize != pkgSize || details.Files == 0 {
		t.Fatalf("Details: %+v %v", details, err)
	}
	dest := t.TempDir()
	choices := Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest, Level: 3}, DeleteOriginals: true}
	views, err := a.Plan("compress", []jobs.Item{game.Item}, choices)
	if err != nil || len(views) != 1 || views[0].Block != "" {
		t.Fatalf("%+v %v", views, err)
	}
	if views[0].Need != game.OriginalSize || views[0].SourceSize != pkgSize {
		t.Fatalf("need %d, source %d: %+v", views[0].Need, views[0].SourceSize, views[0])
	}
	want := filepath.Join(t.TempDir(), "ref.p5z")
	args := append([]string{}, views[0].Args...)
	for i := range args {
		if args[i] == views[0].Tmp {
			args[i] = want
		}
	}
	if result, code, err := core.Run(context.Background(), engine, args, nil); err != nil || code != 0 || result.Raw["ok"] != true {
		t.Fatalf("%v: %d %+v %v", args, code, result.Raw, err)
	}
	if err := a.Start("compress", []jobs.Item{game.Item}, choices); err != nil {
		t.Fatal(err)
	}
	sum := events.wait(t)
	if got := strings.Join(events.states(0), " "); got != "waiting packing verifying ok" {
		t.Fatalf("states: %s, %+v", got, events.last(0))
	}
	out := filepath.Join(dest, jobs.P5zName(game.Item))
	made, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if reference, _ := os.ReadFile(want); len(made) == 0 || !bytes.Equal(made, reference) {
		t.Errorf("the .p5z is not the engine's: %d and %d bytes", len(made), len(reference))
	}
	if sum.Done != 1 || sum.Deleted != 1 || sum.DeletedSize != pkgSize || sum.Saved != pkgSize-uint64(len(made)) {
		t.Errorf("%+v (package %d, made %d)", sum, pkgSize, len(made))
	}
	if _, err := os.Stat(pkg); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the package is still there: %v", err)
	}
	packed, err := a.Details(out)
	if err != nil || packed.Kind != "p5z" || packed.OriginalSize != game.OriginalSize || packed.Files != details.Files {
		t.Fatalf("%+v %v", packed, err)
	}
}

// A package made with another passcode: Describe says so (the page tells where to give it); with the
// passcode in Settings it is read, compressed and verified like any other.
func TestPackageWithAnotherPasscode(t *testing.T) {
	engine, fixtures := needCore(t)
	useSettings(t, t.TempDir())
	t.Cleanup(func() { core.SetPasscode("") })
	dumps := t.TempDir()
	pkg := copyImage(t, filepath.Join(fixtures, "pass.pkg"), filepath.Join(dumps, "Locked.pkg"))
	events := newPageEvents()
	a := pageApp(engine, events)
	a.Setup()
	if _, err := a.Describe(pkg); err == nil || !strings.Contains(err.Error(), "--passcode") {
		t.Fatalf("with the default: %v", err)
	}
	if err := a.SetSettings(settings.Settings{Passcode: "WrongPasscode-0123456789abcdefgh"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Describe(pkg); err == nil || !strings.Contains(err.Error(), "--passcode") {
		t.Fatalf("with a wrong one: %v", err)
	}
	if err := a.SetSettings(settings.Settings{Passcode: testPasscode}); err != nil {
		t.Fatal(err)
	}
	game, err := a.Describe(pkg)
	if err != nil || game.Kind != "fpkg" || game.TitleID != "PPSA00000" {
		t.Fatalf("%+v %v", game.Item, err)
	}
	dest := t.TempDir()
	if err := a.Start("compress", []jobs.Item{game.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeFolder, Dest: dest, Level: 1}}); err != nil {
		t.Fatal(err)
	}
	if sum := events.wait(t); sum.Done != 1 || sum.Failed != 0 {
		t.Fatalf("%+v %v", sum, events.last(0))
	}
	if _, err := os.Stat(pkg); err != nil {
		t.Errorf("the package went without being asked: %v", err)
	}
	// and a queue that starts without it fails with the engine's own code, which the page words
	core.SetPasscode("")
	o := jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir(), Level: 1}
	plan := jobs.PlanCompress([]jobs.Item{game.Item}, o, realFS{})[0]
	result, code, err := core.Run(context.Background(), engine, plan.Args, nil)
	if err != nil || code != 1 || result.Raw["error"] != "passcode" {
		t.Errorf("%v: %d %+v %v", plan.Args, code, result.Raw, err)
	}
}
