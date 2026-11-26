// SPDX-License-Identifier: GPL-3.0-or-later

package watch

import (
	"os"
	"path/filepath"
	"testing"
)

// On Linux a file of the item that a process of this user holds open for writing is found in /proc: open for
// writing, the item is held; for reading, or closed, it is not.
func TestHeldOpenOnLinux(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "Game")
	dump(t, game, true)
	if heldOpen([]string{game})[game] {
		t.Fatal("held with nothing open")
	}
	reading, err := os.Open(filepath.Join(game, "eboot.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer reading.Close()
	if heldOpen([]string{game})[game] {
		t.Error("held by a file open for reading")
	}
	writing, err := os.OpenFile(filepath.Join(game, "copying.bin"), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if !heldOpen([]string{game})[game] || !heldOpen([]string{filepath.Join(game, "copying.bin")})[filepath.Join(game, "copying.bin")] {
		t.Error("not held by a file open for writing")
	}
	if heldOpen([]string{filepath.Join(dir, "Gamer")})[filepath.Join(dir, "Gamer")] {
		t.Error("a name that begins like it is held")
	}
	writing.Close()
	if heldOpen([]string{game})[game] {
		t.Error("held once closed")
	}
}
