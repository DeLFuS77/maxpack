// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package system

import (
	"syscall"
	"unsafe"
)

// Language is the language of the user's system, as "es-ES".
func Language() string {
	buffer := make([]uint16, 85)
	length, _, _ := kernel32.NewProc("GetUserDefaultLocaleName").Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer)
}
