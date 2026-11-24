// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows && !darwin && !linux

package system

// ChooseFolders is not here: the window asks for one folder with its own dialog.
func ChooseFolders(title string) ([]string, error) { return nil, ErrNoMultiPicker }
