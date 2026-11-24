// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows && !linux && !darwin

package trash

// Elsewhere MaxPack knows no recycle bin: the originals stay, or are deleted for good when asked.

func move(path string) error {
	return &Unavailable{Reason: NoBin, Drive: "/"}
}

func check(path string) error {
	return &Unavailable{Reason: NoBin, Drive: "/"}
}
