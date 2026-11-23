//go:build !windows

// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

// FreeSpace is the room left for this user on the drive of a path (or of the nearest folder
// above it that exists).
func FreeSpace(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(Existing(path), &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

// VolumeRoot is the top folder that is on the same drive as a path: where the drive is mounted.
func VolumeRoot(path string) string {
	here, err := filepath.Abs(Existing(path))
	if err != nil {
		return path
	}
	device := func(p string) (uint64, bool) {
		var stat unix.Stat_t
		if err := unix.Stat(p, &stat); err != nil {
			return 0, false
		}
		return uint64(stat.Dev), true
	}
	want, ok := device(here)
	if !ok {
		return path
	}
	for {
		parent := filepath.Dir(here)
		if parent == here {
			return here
		}
		if dev, ok := device(parent); !ok || dev != want {
			return here
		}
		here = parent
	}
}
