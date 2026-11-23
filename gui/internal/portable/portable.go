// SPDX-License-Identifier: GPL-3.0-or-later

// Package portable tells where MaxPack keeps what it remembers: next to the program when it is
// portable, else in the folders of the user.
package portable

import (
	"os"
	"path/filepath"
	"runtime"
)

// Folder is the folder "settings" next to the program, when there is one: MaxPack then
// keeps everything it remembers there, the size and place of the window, the settings and the
// data of the page (its webview), and nothing in the folders of the user.
func Folder() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	folder := filepath.Join(filepath.Dir(executable), "settings")
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return ""
	}
	return folder
}

// DataFolder is where MaxPack keeps what it remembers: the portable folder, or MaxPack in the
// folder of the settings of the user.
func DataFolder() (string, error) {
	if folder := Folder(); folder != "" {
		return folder, nil
	}
	folder, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(folder, "MaxPack"), nil
}

// WebviewFolder is where the page keeps its settings: in the portable folder when there is one,
// else where the window toolkit puts it (""). On Windows that is a folder named after the
// program in the folder of the settings of the user: the one of maxpack.exe is kept, whatever
// the program is named, so a program named otherwise (as "maxpack (1).exe", by a browser that
// had one already) does not lose them.
func WebviewFolder() string {
	if folder := Folder(); folder != "" {
		return filepath.Join(folder, "webview")
	}
	if folder, err := os.UserConfigDir(); err == nil && runtime.GOOS == "windows" {
		return filepath.Join(folder, "maxpack.exe")
	}
	return ""
}
