// SPDX-License-Identifier: GPL-3.0-or-later

// Package settings keeps what the user chose between runs: language, theme, mode, level, the
// runtime of ampr-emu-ng, the last destination, what becomes of the originals, the priority of the
// engine, the names of the files, the threads it compresses with and the repair data of a .p5z.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/internal/par2"
	"github.com/DeLFuS77/maxpack/gui/internal/portable"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/jobs"
)

// Languages are the languages of the window, by their code, the same as the page of MaxPack on the
// console has (the order is the one of Settings).
var Languages = []string{"en", "ru", "zh", "de", "es", "fr", "ja", "pt", "it", "ko", "ar"}

// ValidLang tells whether code is one of the languages of the window.
func ValidLang(code string) bool {
	for _, l := range Languages {
		if l == code {
			return true
		}
	}
	return false
}

// SystemLang is the language of the system when the window has it, by the base of its tag
// (pt-BR and pt_BR.UTF-8 are pt, zh-Hant-TW is zh), else English.
func SystemLang() string { return baseLang(system.Language()) }

// baseLang is the language of a tag of the system ("es-ES", "pt_BR.UTF-8", "zh-Hant-TW"), when the
// window has it, else English.
func baseLang(tag string) string {
	tag = strings.ToLower(tag)
	if i := strings.IndexAny(tag, "-_.@"); i >= 0 {
		tag = tag[:i]
	}
	if ValidLang(tag) {
		return tag
	}
	return "en"
}

// Settings is what is remembered.
type Settings struct {
	Lang     string // one of Languages, or "" when none was chosen: the language of the system
	Theme    string // "system", "light" or "dark"
	Runtime  string
	LastDest string
	Mode     jobs.Mode
	Level    int
	// DeleteOriginals is what Compress starts with: each original goes once its new one is made,
	// verified and in its place; to the recycle bin, or for good with DeletePermanently
	DeleteOriginals   bool
	DeletePermanently bool
	// LowPriority runs the engine below the other programs, so the PC stays usable
	LowPriority bool
	// NameTemplate names a .p5z and an unpacked original (jobs.NameOf)
	NameTemplate string
	// Threads are the threads Compress gives the engine, from Measure this PC; 0 is all the CPU's
	Threads int
	// RepairData gives each .p5z Compress makes repair data (a PAR2 set next to it), of
	// RepairPercent of it: 1, 5 or 10, kept also while it is off
	RepairData    bool
	RepairPercent int
	// KeepLeftovers keeps in what Compress makes what dump tools leave behind (a decrypted/ folder,
	// *.auth_info); off by default, so it is left out when the game's own list of the original
	// package's files proves it is not the game's (jobs.Options.KeepLeftovers)
	KeepLeftovers bool
}

const fileName = "settings.json"

// Dir is where the settings live: the portable folder next to the program when there is one,
// else MaxPack in the folder of the settings of the user (a variable so the tests can move it).
var Dir = portable.DataFolder

// Defaults are the settings of a first run: no language chosen (the system's), the system theme,
// an installer (.p5z), level 19, the originals kept, the normal priority, the names of before, and
// no repair data (5 % when it is turned on).
func Defaults() Settings {
	return Settings{Theme: "system", Mode: jobs.ModeP5z, Level: 19, NameTemplate: jobs.DefaultTemplate, RepairPercent: par2.DefaultPercent}
}

// EffectiveLang is the language the window speaks: the one chosen, else the one of the system,
// else English.
func (s Settings) EffectiveLang() string {
	if ValidLang(s.Lang) {
		return s.Lang
	}
	return SystemLang()
}

func validMode(m jobs.Mode) bool {
	switch m {
	case jobs.ModeP5z, jobs.ModeFolder, jobs.ModeOriginal, jobs.ModeConsole, jobs.ModeVerify:
		return true
	}
	return false
}

// Normalize puts the defaults in place of every field that is not valid.
func (s Settings) Normalize() Settings {
	d := Defaults()
	if !ValidLang(s.Lang) {
		s.Lang = d.Lang
	}
	if s.Theme != "system" && s.Theme != "light" && s.Theme != "dark" {
		s.Theme = d.Theme
	}
	if !validMode(s.Mode) {
		s.Mode = d.Mode
	}
	if s.Level < 1 || s.Level > 22 {
		s.Level = d.Level
	}
	if jobs.ValidTemplate(s.NameTemplate) != nil {
		s.NameTemplate = d.NameTemplate
	}
	if s.Threads < 0 || s.Threads > 1024 {
		s.Threads = d.Threads
	}
	if !par2.ValidPercent(s.RepairPercent) {
		s.RepairPercent = d.RepairPercent
	}
	return s
}

func path() (string, error) {
	folder, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(folder, fileName), nil
}

// Load reads the settings. Without a file, or with a broken one, the fields not there or not
// valid are the defaults: it fails only if the file cannot be read for some other reason.
func Load() (Settings, error) {
	file, err := path()
	if err != nil {
		return Defaults(), err
	}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), err
	}
	// field by field, so one bad field does not take the others with it
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return Defaults(), nil
	}
	s := Settings{}
	read := func(name string, into any) {
		if raw, ok := fields[name]; ok {
			json.Unmarshal(raw, into)
		}
	}
	read("Lang", &s.Lang)
	read("Theme", &s.Theme)
	read("Runtime", &s.Runtime)
	read("LastDest", &s.LastDest)
	read("Mode", &s.Mode)
	read("Level", &s.Level)
	read("DeleteOriginals", &s.DeleteOriginals)
	read("DeletePermanently", &s.DeletePermanently)
	read("LowPriority", &s.LowPriority)
	read("NameTemplate", &s.NameTemplate)
	read("Threads", &s.Threads)
	read("RepairData", &s.RepairData)
	read("RepairPercent", &s.RepairPercent)
	read("KeepLeftovers", &s.KeepLeftovers)
	return s.Normalize(), nil
}

// Save writes the settings whole or not at all: a temporary file, then renamed over the old one.
func Save(s Settings) error {
	file, err := path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.Normalize(), "", "  ")
	if err != nil {
		return err
	}
	folder := filepath.Dir(file)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(folder, fileName+".*.tmp")
	if err != nil {
		return err
	}
	_, err = temp.Write(data)
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp.Name(), file)
	}
	if err != nil {
		os.Remove(temp.Name())
	}
	return err
}
