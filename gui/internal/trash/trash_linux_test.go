// SPDX-License-Identifier: GPL-3.0-or-later

package trash

import "testing"

// An item of a name the trash holds already gets a number, and the paths of a .trashinfo are
// written as the specification asks.
func TestTrashNames(t *testing.T) {
	if got := numbered("Starfall Odyssey", 1); got != "Starfall Odyssey" {
		t.Errorf("1: %q", got)
	}
	if got := numbered("game.exfat", 3); got != "game.3.exfat" {
		t.Errorf("3: %q", got)
	}
	if got := escape("/media/usb/Neon Meridian/ü"); got != "/media/usb/Neon%20Meridian/%C3%BC" {
		t.Errorf("escape: %q", got)
	}
}
