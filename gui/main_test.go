// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// TestMain lets the test binary act as an engine (see fakecore). The tests keep what the window
// writes to its own folder (the settings, the ampr-emu-ng that comes with it) in a temporary one,
// never in the user's.
func TestMain(m *testing.M) {
	fakecore.Main()
	folder, err := os.MkdirTemp("", "maxpack-settings-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	settings.Dir = func() (string, error) { return folder, nil }
	code := m.Run()
	os.RemoveAll(folder)
	os.Exit(code)
}

// useSettings puts the folder of the settings in folder for a test, and takes it back after.
func useSettings(t *testing.T, folder string) {
	t.Helper()
	old := settings.Dir
	settings.Dir = func() (string, error) { return folder, nil }
	t.Cleanup(func() { settings.Dir = old })
}
