// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package system

import "golang.org/x/sys/windows"

// SetHidden hides a file or a folder from the Explorer (the attribute hidden), or shows it
// again: what MaxPack writes is hidden until it is verified and has its name.
func SetHidden(path string, hidden bool) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	next := attributes &^ windows.FILE_ATTRIBUTE_HIDDEN
	if hidden {
		next |= windows.FILE_ATTRIBUTE_HIDDEN
	}
	if next == attributes {
		return nil
	}
	// a file with no other attribute takes FILE_ATTRIBUTE_NORMAL
	if next == 0 {
		next = windows.FILE_ATTRIBUTE_NORMAL
	}
	return windows.SetFileAttributes(name, next)
}

// IsHidden tells whether a file or a folder is hidden from the Explorer.
func IsHidden(path string) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attributes, err := windows.GetFileAttributes(name)
	return err == nil && attributes&windows.FILE_ATTRIBUTE_HIDDEN != 0
}
