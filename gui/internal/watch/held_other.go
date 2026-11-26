// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !linux

package watch

// heldOpen is the look at the programs that hold a file open for writing, which only Linux has (/proc): on
// Windows each file is opened letting others only read it (stat_windows.go), which a writer makes fail; macOS
// has no such look, and only the quiet minute and the downloads' own names tell a file still arriving.
func heldOpen([]string) map[string]bool { return nil }
