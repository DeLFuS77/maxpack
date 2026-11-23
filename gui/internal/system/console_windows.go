// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package system

import (
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

// HideConsole closes the console Windows opens for MaxPack when it is started from the
// Explorer: then MaxPack is the only program in it. Started from a terminal, it is kept.
func HideConsole() {
	var processes [2]uint32
	count, _, _ := kernel32.NewProc("GetConsoleProcessList").Call(uintptr(unsafe.Pointer(&processes[0])), 2)
	if count != 1 {
		return
	}
	if console, _, _ := kernel32.NewProc("GetConsoleWindow").Call(); console != 0 {
		syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow").Call(console, 0)
	}
	kernel32.NewProc("FreeConsole").Call()
}
