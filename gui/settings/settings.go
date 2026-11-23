// SPDX-License-Identifier: GPL-3.0-or-later

// Package settings keeps what the user chose between runs: language, theme, mode, level, the
// runtime of ampr-emu-ng and the last destination.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/internal/portable"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/jobs"
)

// Settings is what is remembered.
type Settings struct {
	Lang     string // "es" or "en"
	Theme    string // "system", "light" or "dark"
	Runtime  string
	LastDest string
	Mode     jobs.Mode
	Level    int
}

const fileName = "settings.json"

// Dir is where the settings live: the portable folder next to the program when there is one,
// else MaxPack in the folder of the settings of the user (a variable so the tests can move it).
var Dir = portable.DataFolder

// Defaults are the settings of a first run: the language of the system, the system theme, an
// installer (.p5z) and level 19.
func Defaults() Settings {
	lang := "en"
	if strings.HasPrefix(strings.ToLower(system.Language()), "es") {
		lang = "es"
	}
	return Settings{Lang: lang, Theme: "system", Mode: jobs.ModeP5z, Level: 19}
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
	if s.Lang != "es" && s.Lang != "en" {
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
