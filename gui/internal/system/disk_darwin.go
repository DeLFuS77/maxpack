// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"strings"

	"golang.org/x/sys/unix"
)

// FilesystemType names the file system of the drive of a path (msdos is FAT32).
func FilesystemType(path string) (string, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(Existing(path), &stat); err != nil {
		return "", err
	}
	name := make([]byte, 0, len(stat.Fstypename))
	for _, c := range stat.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	switch strings.ToLower(string(name)) {
	case "msdos":
		return "FAT32", nil
	case "exfat":
		return "exFAT", nil
	}
	return string(name), nil
}
