// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package system

// SetTaskbarProgress shows the progress of the work on the button of the taskbar: on Windows
// only. The Dock of macOS and the launchers of Linux show none for now.
func SetTaskbarProgress(state string, done uint64, total uint64) {}
