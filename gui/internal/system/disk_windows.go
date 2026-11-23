// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// FreeSpace is the room left for this user on the drive of a path (or of the nearest folder
// above it that exists).
func FreeSpace(path string) (uint64, error) {
	from, err := windows.UTF16PtrFromString(Existing(path))
	if err != nil {
		return 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(from, &available, nil, nil); err != nil {
		return 0, err
	}
	return available, nil
}

// VolumeRoot is the root of the drive of a path, as Windows tells it (E:\ for E:\games\x); the
// path itself when Windows cannot tell.
func VolumeRoot(path string) string {
	absolute, err := filepath.Abs(Existing(path))
	if err != nil {
		return path
	}
	from, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return path
	}
	root := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumePathName(from, &root[0], uint32(len(root))); err != nil {
		return path
	}
	return windows.UTF16ToString(root)
}

// FilesystemType names the file system of the drive of a path (FAT32, exFAT, NTFS...), in the
// words Windows uses.
func FilesystemType(path string) (string, error) {
	root, err := windows.UTF16PtrFromString(VolumeRoot(path))
	if err != nil {
		return "", err
	}
	name := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumeInformation(root, nil, 0, nil, nil, nil, &name[0], uint32(len(name))); err != nil {
		return "", err
	}
	return windows.UTF16ToString(name), nil
}
