// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package system

// HideConsole has nothing to do: only Windows opens a console for a program started from the
// file manager.
func HideConsole() {}
