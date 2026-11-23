// SPDX-License-Identifier: GPL-3.0-or-later

package system

import "golang.org/x/sys/unix"

// FilesystemType names the file system of the drive of a path as FAT32 (vfat), exFAT, NTFS or
// the name of the kernel for others.
func FilesystemType(path string) (string, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(Existing(path), &stat); err != nil {
		return "", err
	}
	switch uint32(stat.Type) {
	case 0x4d44:
		return "FAT32", nil
	case 0x2011bab0:
		return "exFAT", nil
	case 0x5346544e:
		return "NTFS", nil
	}
	return "", nil
}
