// SPDX-License-Identifier: GPL-3.0-or-later

package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/portable"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
)

func useFolder(t *testing.T) string {
	t.Helper()
	folder := filepath.Join(t.TempDir(), "datos")
	old := Dir
	Dir = func() (string, error) { return folder, nil }
	t.Cleanup(func() { Dir = old })
	return folder
}

func TestSettingsRoundtrip(t *testing.T) {
	useFolder(t)
	want := Settings{Lang: "en", Theme: "dark", Runtime: `C:\emu\runtime.elf`, LastDest: `D:\Juegos`, Mode: "folder", Level: 7}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || got != want {
		t.Fatalf("%+v, %v; esperaba %+v", got, err, want)
	}
}

func TestSettingsDefaults(t *testing.T) {
	useFolder(t)
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	lang := "en"
	if strings.HasPrefix(strings.ToLower(system.Language()), "es") {
		lang = "es"
	}
	if got.Mode != "p5z" || got.Level != 19 || got.Lang != lang || got.Theme != "system" {
		t.Fatalf("por defecto: %+v (idioma esperado %s)", got, lang)
	}
}

func TestSettingsCorruptFallsBack(t *testing.T) {
	folder := useFolder(t)
	os.MkdirAll(folder, 0o755)
	file := filepath.Join(folder, "settings.json")
	// partial and with bad values: the good ones stay, the bad ones go to the defaults
	os.WriteFile(file, []byte(`{"Theme":"dark","Level":99,"Mode":"nada","Lang":"fr","LastDest":"E:\\x"}`), 0o644)
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != "dark" || got.LastDest != `E:\x` || got.Level != 19 || got.Mode != "p5z" || (got.Lang != "es" && got.Lang != "en") {
		t.Fatalf("parcial: %+v", got)
	}
	os.WriteFile(file, []byte(`{ esto no es json`), 0o644)
	got, err = Load()
	if err != nil || got.Level != 19 || got.Theme != "system" {
		t.Fatalf("roto: %+v, %v", got, err)
	}
	os.WriteFile(file, []byte(`{"Level":"diez","Theme":5}`), 0o644)
	got, err = Load()
	if err != nil || got.Level != 19 || got.Theme != "system" {
		t.Fatalf("tipos malos: %+v, %v", got, err)
	}
}

func TestSettingsAtomicSave(t *testing.T) {
	folder := useFolder(t)
	if err := Save(Defaults()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Fatalf("quedan restos: %v", entries)
	}
}

func TestPortable(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(filepath.Dir(executable), "settings")
	if _, err := os.Stat(folder); err == nil {
		t.Skip("ya hay una carpeta settings junto al ejecutable de prueba")
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Skip("no se puede crear la carpeta junto al ejecutable:", err)
	}
	defer os.RemoveAll(folder)
	if portable.Folder() == "" {
		t.Fatal("portable no ve la carpeta settings")
	}
	if err := Save(Settings{Lang: "es", Theme: "light", Mode: "p5z", Level: 12}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, "settings.json")); err != nil {
		t.Fatalf("no se guardó en la carpeta portable: %v", err)
	}
	if got, _ := Load(); got.Level != 12 || got.Theme != "light" {
		t.Fatalf("%+v", got)
	}
}
