// SPDX-License-Identifier: GPL-3.0-or-later

package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/portable"
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
	want := Settings{Lang: "en", Theme: "dark", Runtime: `C:\emu\runtime.elf`, LastDest: `D:\Juegos`, Mode: "folder", Level: 7, NameTemplate: "{title} [{id}]",
		RepairData: true, RepairPercent: 10}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || got != want {
		t.Fatalf("%+v, %v; expected %+v", got, err, want)
	}
}

func TestSettingsDefaults(t *testing.T) {
	useFolder(t)
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// no language chosen: the window speaks the one of the system (English when it has none)
	if got.Mode != "p5z" || got.Level != 19 || got.Lang != "" || got.Theme != "system" {
		t.Fatalf("default: %+v", got)
	}
	if lang := got.EffectiveLang(); lang != SystemLang() || !ValidLang(lang) {
		t.Fatalf("language of a first run: %q", lang)
	}
}

func TestSettingsCorruptFallsBack(t *testing.T) {
	folder := useFolder(t)
	os.MkdirAll(folder, 0o755)
	file := filepath.Join(folder, "settings.json")
	// partial and with bad values: the good ones stay, the bad ones go to the defaults
	os.WriteFile(file, []byte(`{"Theme":"dark","Level":99,"Mode":"nada","Lang":"xx","LastDest":"E:\\x"}`), 0o644)
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != "dark" || got.LastDest != `E:\x` || got.Level != 19 || got.Mode != "p5z" || got.Lang != "" {
		t.Fatalf("partial: %+v", got)
	}
	os.WriteFile(file, []byte(`{ esto no es json`), 0o644)
	got, err = Load()
	if err != nil || got.Level != 19 || got.Theme != "system" {
		t.Fatalf("broken: %+v, %v", got, err)
	}
	os.WriteFile(file, []byte(`{"Level":"diez","Theme":5}`), 0o644)
	got, err = Load()
	if err != nil || got.Level != 19 || got.Theme != "system" {
		t.Fatalf("tipos malos: %+v, %v", got, err)
	}
}

// The threads a measure found are kept; 0 is all the CPU's (the default), and a count the engine
// does not take goes back to it.
func TestSettingsThreads(t *testing.T) {
	folder := useFolder(t)
	if got, _ := Load(); got.Threads != 0 {
		t.Fatalf("default: %+v", got)
	}
	s := Defaults()
	s.Threads = 24
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(); got.Threads != 24 {
		t.Fatalf("kept: %+v", got)
	}
	for _, bad := range []string{`{"Threads":-3}`, `{"Threads":5000}`, `{"Threads":"many"}`} {
		os.WriteFile(filepath.Join(folder, "settings.json"), []byte(bad), 0o644)
		if got, _ := Load(); got.Threads != 0 {
			t.Errorf("%s: %+v", bad, got)
		}
	}
}

func TestSettingsAtomicSave(t *testing.T) {
	folder := useFolder(t)
	if err := Save(Defaults()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Fatalf("leftovers remain: %v", entries)
	}
}

func TestPortable(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(filepath.Dir(executable), "settings")
	if _, err := os.Stat(folder); err == nil {
		t.Skip("there is already a settings folder next to the test executable")
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Skip("cannot create the folder next to the executable:", err)
	}
	defer os.RemoveAll(folder)
	if portable.Folder() == "" {
		t.Fatal("portable does not see the settings folder")
	}
	if err := Save(Settings{Lang: "es", Theme: "light", Mode: "p5z", Level: 12}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, "settings.json")); err != nil {
		t.Fatalf("not saved in the portable folder: %v", err)
	}
	if got, _ := Load(); got.Level != 12 || got.Theme != "light" {
		t.Fatalf("%+v", got)
	}
}

// The language of the system by its base: the tag of Windows (pt-BR), of the others (es_ES.UTF-8,
// C) and of the languages the window does not have.
func TestBaseLang(t *testing.T) {
	for tag, want := range map[string]string{
		"es-ES": "es", "pt-BR": "pt", "pt_BR.UTF-8": "pt", "zh-Hans-CN": "zh", "zh_TW": "zh", "ar-EG": "ar",
		"ja-JP": "ja", "ko-KR": "ko", "ru-RU": "ru", "de_DE@euro": "de", "FR-fr": "fr", "it": "it", "en-US": "en",
		"": "en", "C": "en", "POSIX": "en", "C.UTF-8": "en", "fil-PH": "en", "xx": "en",
	} {
		if got := baseLang(tag); got != want {
			t.Errorf("%q: %q, expected %q", tag, got, want)
		}
	}
	if len(Languages) != 11 {
		t.Errorf("%d languages", len(Languages))
	}
	for _, code := range Languages {
		if !ValidLang(code) {
			t.Errorf("%s is not valid", code)
		}
		if got := (Settings{Lang: code}).Normalize().Lang; got != code {
			t.Errorf("the choice of %s is lost: %q", code, got)
		}
	}
	// a choice that is not a language of the window is not a choice
	if (Settings{Lang: "xx"}).Normalize().Lang != "" || ValidLang("") {
		t.Error("an unknown language was kept")
	}
	if got := (Settings{Lang: "ja"}).EffectiveLang(); got != "ja" {
		t.Errorf("the choice is not the language: %q", got)
	}
}

// The choices of 1.0.2 are kept, and off by default: deleting the originals once checked (to the
// recycle bin unless for good), low priority, and the names of the files, today's by default.
func TestSettingsOfTheQueue(t *testing.T) {
	useFolder(t)
	got, err := Load()
	if err != nil || got.DeleteOriginals || got.DeletePermanently || got.LowPriority || got.NameTemplate != "{title} [{id}]" {
		t.Fatalf("defaults: %+v, %v", got, err)
	}
	want := Defaults()
	want.DeleteOriginals, want.DeletePermanently, want.LowPriority = true, true, true
	want.NameTemplate = "{title} [{id}] [{region}] [v{version}]"
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(); err != nil || got != want {
		t.Fatalf("%+v, %v; expected %+v", got, err, want)
	}
	// a template that is not one goes back to the default
	for _, bad := range []string{"", "   ", "{name}", "game", "{title}/{id}"} {
		want.NameTemplate = bad
		if got := want.Normalize(); got.NameTemplate != "{title} [{id}]" {
			t.Errorf("%q: %q", bad, got.NameTemplate)
		}
	}
}

// The repair data of 1.0.4 is off by default, and 5 % is what it proposes; the percentage chosen is
// kept apart from whether it is on, and one the window does not offer goes back to 5 %.
func TestSettingsRepairData(t *testing.T) {
	folder := useFolder(t)
	got, err := Load()
	if err != nil || got.RepairData || got.RepairPercent != 5 {
		t.Fatalf("defaults: %+v, %v", got, err)
	}
	want := Defaults()
	want.RepairData, want.RepairPercent = true, 1
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(); err != nil || got != want {
		t.Fatalf("%+v, %v; expected %+v", got, err, want)
	}
	for _, bad := range []int{0, 2, -5, 100} {
		want.RepairPercent = bad
		if got := want.Normalize(); got.RepairPercent != 5 {
			t.Errorf("%d: %d", bad, got.RepairPercent)
		}
	}
	// the settings of 1.0.3 have none: off, 5 %
	os.WriteFile(filepath.Join(folder, "settings.json"), []byte(`{"Theme":"dark","Level":12}`), 0o644)
	if got, _ := Load(); got.RepairData || got.RepairPercent != 5 || got.Level != 12 {
		t.Errorf("1.0.3: %+v", got)
	}
}
