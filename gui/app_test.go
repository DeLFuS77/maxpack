// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/bundled"
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestCoreMissingReported(t *testing.T) {
	t.Setenv("MAXPACK_CORE", filepath.Join(t.TempDir(), "no-existe.exe"))
	useSettings(t, t.TempDir())
	setup := (&App{}).Setup()
	if setup.CoreError == "" {
		t.Fatal("the engine is missing and nothing says so")
	}
	if setup.Version == "" || setup.Lang == "" || setup.Theme != "system" {
		t.Fatalf("incomplete setup: %+v", setup)
	}
}

func TestForwardArgs(t *testing.T) {
	core := os.Getenv("MAXPACK_CORE")
	if core == "" {
		t.Skip("MAXPACK_CORE is missing: the engine is needed")
	}
	exe := filepath.Join(t.TempDir(), "MaxPack.exe")
	tags := "desktop,production"
	if runtime.GOOS == "linux" {
		tags += ",webkit2_41" // WebKitGTK 4.1, the one current distributions have
	}
	build := exec.Command("go", "build", "-tags", tags, "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("does not compile: %v\n%s", err, out)
	}
	want, err := exec.Command(core, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command(exe, "--version").Output()
	if err != nil || strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("--version: %q, %v (expected %q)", got, err, want)
	}
	err = exec.Command(exe, "pack").Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("pack without arguments should exit with 2: %v", err)
	}
	// the arguments of the Explorer open the window only with files: without, they go to
	// maxpack-core, which does not know them, as before
	err = exec.Command(exe, "--section", "verify").Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("--section without files should be maxpack-core's, and exit with 2: %v", err)
	}
	// the installer without what to do says how it is used, and does nothing else
	out, err := exec.Command(exe, "--installer").CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "usage: MaxPack --installer") {
		t.Fatalf("--installer alone: %v %s", err, out)
	}
}

func TestSetupFromSettings(t *testing.T) {
	useSettings(t, t.TempDir())
	a := &App{}
	if err := a.SetSettings(settings.Settings{Lang: "en", Theme: "dark", Mode: "folder", Level: 5}); err != nil {
		t.Fatal(err)
	}
	if got := a.GetSettings(); got.Level != 5 || got.Mode != "folder" {
		t.Fatalf("%+v", got)
	}
	if setup := a.Setup(); setup.Lang != "en" || setup.Theme != "dark" {
		t.Fatalf("%+v", setup)
	}
}

// An engine of another version (one of before --capabilities, or one that lacks something the
// window needs) is said as such, and the page takes it as a missing one; the engine of this
// version is fine.
func TestCoreOtherVersionReported(t *testing.T) {
	useSettings(t, t.TempDir())
	ours := os.Getenv("MAXPACK_CORE")
	t.Setenv("MAXPACK_CORE", os.Args[0])
	for _, mode := range []string{"old", "partial"} {
		t.Setenv(fakecore.Env, mode)
		setup := (&App{}).Setup()
		if setup.CoreError != "maxpack-core is of another version" || !setup.CoreOtherVersion || setup.CoreVersion == "" {
			t.Errorf("%s: %+v", mode, setup)
		}
	}
	if ours == "" {
		t.Skip("MAXPACK_CORE is missing: the engine is needed")
	}
	t.Setenv(fakecore.Env, "")
	t.Setenv("MAXPACK_CORE", ours)
	if setup := (&App{}).Setup(); setup.CoreError != "" || setup.CoreOtherVersion {
		t.Errorf("this version's engine: %+v", setup)
	}
}

// The runtime chosen in Settings has to read zstd (the one of ampr-emu-ng carries AMPRZPK1): any
// other is refused, and nothing is kept.
func TestChooseRuntimeChecksZstd(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "bueno.sprx")
	os.WriteFile(good, append(make([]byte, 300), []byte("..AMPRZPK1..")...), 0o644)
	old := filepath.Join(dir, "viejo.sprx")
	os.WriteFile(old, make([]byte, 300), 0o644)
	var chosen string
	a := &App{ctx: context.Background(), openFile: func(context.Context, wailsruntime.OpenDialogOptions) (string, error) { return chosen, nil }}
	chosen = good
	if path, err := a.ChooseRuntime(); path != good || err != nil {
		t.Errorf("ampr-emu-ng's: %q, %v", path, err)
	}
	chosen = old
	if path, err := a.ChooseRuntime(); path != "" || err == nil || !strings.HasPrefix(err.Error(), "runtime_not_zstd") {
		t.Errorf("one that does not read zstd: %q, %v", path, err)
	}
	chosen = ""
	if path, err := a.ChooseRuntime(); path != "" || err != nil {
		t.Errorf("none chosen: %q, %v", path, err)
	}
}

// argsOf is the arguments of the engine of the first plan, one after the other with a NUL.
func argsOf(t *testing.T, a *App, mode jobs.Mode, item jobs.Item, runtime string) ([]string, string) {
	t.Helper()
	plans, err := a.Plan("compress", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: mode, Dest: t.TempDir(), Runtime: runtime}})
	if err != nil || len(plans) != 1 {
		t.Fatalf("%s: %v %v", mode, plans, err)
	}
	return plans[0].Warnings, strings.Join(plans[0].Args, "\x00")
}

// The ampr-emu-ng that comes with MaxPack is written once to the folder of the settings (at the
// start of the window) and said by Setup, with its version. A queue to compress gives it, in both
// modes, to the games that do not carry one, and nothing warns of the runtime; one chosen in
// Settings wins, as before.
func TestBuiltInRuntime(t *testing.T) {
	folder := t.TempDir()
	useSettings(t, folder)
	a := &App{fs: realFS{}}
	a.startup(context.Background())
	want := filepath.Join(folder, "ampr-emu-ng", bundled.FileName)
	data, err := os.ReadFile(want)
	if sum := sha256.Sum256(data); err != nil || hex.EncodeToString(sum[:]) != bundled.SHA256 {
		t.Fatalf("not written at the start: %v", err)
	}
	if setup := a.Setup(); setup.BuiltInRuntime != want || setup.BuiltInRuntimeVersion != "v0.5.1-maxpack.1" {
		t.Fatalf("%+v", setup)
	}
	item := jobs.Item{Path: filepath.Join(t.TempDir(), "Juego"), TitleID: "PPSA10001", Name: "Juego", Kind: "folder", OriginalSize: 10}
	for _, mode := range []jobs.Mode{jobs.ModeP5z, jobs.ModeFolder} {
		warnings, args := argsOf(t, a, mode, item, "")
		if !strings.Contains(args, "--runtime\x00"+want) || strings.Contains(strings.Join(warnings, " "), "needs_runtime") {
			t.Errorf("%s with the built-in one: %v %q", mode, warnings, args)
		}
		chosen := filepath.Join(folder, "elegido.sprx")
		if _, args := argsOf(t, a, mode, item, chosen); !strings.Contains(args, "--runtime\x00"+chosen) || strings.Contains(args, want) {
			t.Errorf("%s with one chosen: %q", mode, args)
		}
	}
	// a game that carries one that reads zstd keeps its own
	item.HasZstdRuntime = true
	if _, args := argsOf(t, a, jobs.ModeFolder, item, ""); strings.Contains(args, "--runtime") {
		t.Errorf("with its own: %q", args)
	}
}

// When the built-in one cannot be written, Setup says none and the window works as before it: a
// folder without a runtime is warned of, a .p5z is not.
func TestWithoutBuiltInRuntime(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	useSettings(t, blocked)
	a := &App{fs: realFS{}}
	a.startup(context.Background())
	if setup := a.Setup(); setup.BuiltInRuntime != "" || setup.BuiltInRuntimeVersion != "" {
		t.Fatalf("%+v", setup)
	}
	item := jobs.Item{Path: filepath.Join(t.TempDir(), "Juego"), TitleID: "PPSA10001", Name: "Juego", Kind: "folder", OriginalSize: 10}
	if warnings, args := argsOf(t, a, jobs.ModeFolder, item, ""); strings.Contains(args, "--runtime") || !strings.Contains(strings.Join(warnings, " "), "needs_runtime") {
		t.Errorf("folder: %v %q", warnings, args)
	}
	if warnings, args := argsOf(t, a, jobs.ModeP5z, item, ""); strings.Contains(args, "--runtime") || strings.Contains(strings.Join(warnings, " "), "needs_runtime") {
		t.Errorf("p5z: %v %q", warnings, args)
	}
}
