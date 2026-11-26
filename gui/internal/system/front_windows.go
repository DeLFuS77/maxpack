// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package system

import "golang.org/x/sys/windows"

// WindowInFront tells whether the window of MaxPack is the one in front (the foreground window); known is
// always true on Windows.
func WindowInFront() (front, known bool) {
	own := FindOwnWindow(false)
	return own != 0 && windows.GetForegroundWindow() == own, true
}
