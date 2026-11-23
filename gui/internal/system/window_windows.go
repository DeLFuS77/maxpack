// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package system

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetWindowTextW = windows.NewLazySystemDLL("user32.dll").NewProc("GetWindowTextW")

func getWindowText(window windows.HWND, text *uint16, size int32) (int32, error) {
	length, _, err := procGetWindowTextW.Call(uintptr(window), uintptr(unsafe.Pointer(text)), uintptr(size))
	return int32(length), err
}

// the window found by FindOwnWindow, and whether a hidden one is looked for; the callback is made
// once, as Windows takes a few only
var (
	findMutex   sync.Mutex
	foundWindow windows.HWND
	findHidden  bool
	findWindow  = syscall.NewCallback(func(window windows.HWND, _ uintptr) uintptr {
		var owner uint32
		windows.GetWindowThreadProcessId(window, &owner)
		if owner != windows.GetCurrentProcessId() || !findHidden && !windows.IsWindowVisible(window) {
			return 1
		}
		// the window of Wails, not a message window titled MaxPack
		class := make([]uint16, 64)
		if length, _ := windows.GetClassName(window, &class[0], int32(len(class))); windows.UTF16ToString(class[:max(length, 0)]) != "wailsWindow" {
			return 1
		}
		title := make([]uint16, 64)
		length, _ := getWindowText(window, &title[0], int32(len(title)))
		if name := windows.UTF16ToString(title[:max(length, 0)]); len(name) >= 7 && name[:7] == "MaxPack" {
			foundWindow = window
			return 0
		}
		return 1
	})
)

// FindOwnWindow is the main window of this process, titled MaxPack; with hidden, also before it
// is shown.
func FindOwnWindow(hidden bool) windows.HWND {
	findMutex.Lock()
	defer findMutex.Unlock()
	foundWindow, findHidden = 0, hidden
	windows.EnumWindows(findWindow, nil)
	return foundWindow
}
