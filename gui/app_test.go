// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/internal/portable"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestCoreMissingReported(t *testing.T) {
	t.Setenv("MAXPACK_CORE", filepath.Join(t.TempDir(), "no-existe.exe"))
	folder := t.TempDir()
	settings.Dir = func() (string, error) { return folder, nil }
	t.Cleanup(func() { settings.Dir = portable.DataFolder })
	setup := (&App{}).Setup()
	if setup.CoreError == "" {
		t.Fatal("falta el motor y no se dice")
	}
	if setup.Version == "" || setup.Lang == "" || setup.Theme != "system" {
		t.Fatalf("setup incompleto: %+v", setup)
	}
}

func TestForwardArgs(t *testing.T) {
	core := os.Getenv("MAXPACK_CORE")
	if core == "" {
		t.Skip("falta MAXPACK_CORE: se necesita el motor")
	}
	exe := filepath.Join(t.TempDir(), "MaxPack.exe")
	tags := "desktop,production"
	if runtime.GOOS == "linux" {
		tags += ",webkit2_41" // WebKitGTK 4.1, the one current distributions have
	}
	build := exec.Command("go", "build", "-tags", tags, "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("no compila: %v\n%s", err, out)
	}
	want, err := exec.Command(core, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command(exe, "--version").Output()
	if err != nil || strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("--version: %q, %v (esperado %q)", got, err, want)
	}
	err = exec.Command(exe, "pack").Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("pack sin argumentos debería salir con 2: %v", err)
	}
}

func TestSetupFromSettings(t *testing.T) {
	folder := t.TempDir()
	settings.Dir = func() (string, error) { return folder, nil }
	t.Cleanup(func() { settings.Dir = portable.DataFolder })
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
	folder := t.TempDir()
	settings.Dir = func() (string, error) { return folder, nil }
	t.Cleanup(func() { settings.Dir = portable.DataFolder })
	ours := os.Getenv("MAXPACK_CORE")
	t.Setenv("MAXPACK_CORE", os.Args[0])
	for _, mode := range []string{"old", "partial"} {
		t.Setenv(fakecore.Env, mode)
		setup := (&App{}).Setup()
		if setup.CoreError != "maxpack-core es de otra versión" || !setup.CoreOtherVersion || setup.CoreVersion == "" {
			t.Errorf("%s: %+v", mode, setup)
		}
	}
	if ours == "" {
		t.Skip("falta MAXPACK_CORE: se necesita el motor")
	}
	t.Setenv(fakecore.Env, "")
	t.Setenv("MAXPACK_CORE", ours)
	if setup := (&App{}).Setup(); setup.CoreError != "" || setup.CoreOtherVersion {
		t.Errorf("el motor de esta versión: %+v", setup)
	}
}

// The runtime chosen in Ajustes has to read zstd (the one of ampr-emu-ng carries AMPRZPK1): any
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
		t.Errorf("el de ampr-emu-ng: %q, %v", path, err)
	}
	chosen = old
	if path, err := a.ChooseRuntime(); path != "" || err == nil || !strings.HasPrefix(err.Error(), "runtime_not_zstd") {
		t.Errorf("uno que no lee zstd: %q, %v", path, err)
	}
	chosen = ""
	if path, err := a.ChooseRuntime(); path != "" || err != nil {
		t.Errorf("sin elegir: %q, %v", path, err)
	}
}
