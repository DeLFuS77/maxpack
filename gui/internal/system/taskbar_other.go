// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows && !darwin

package system

// SetTaskbarProgress shows the progress of the work on the button of the taskbar: on Windows
// only; macOS shows it in the Dock (taskbar_darwin.go), Linux shows none.
func SetTaskbarProgress(state string, done uint64, total uint64) {}
