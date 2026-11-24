// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"errors"
	"os/exec"
)

// ChooseFolders asks for folders, several at once, with the GTK chooser of zenity; without zenity,
// ErrNoMultiPicker (the window asks for one, with its own dialog). None when it is closed without
// them.
func ChooseFolders(title string) ([]string, error) {
	zenity, err := exec.LookPath("zenity")
	if err != nil {
		return nil, ErrNoMultiPicker
	}
	out, err := exec.Command(zenity, "--file-selection", "--directory", "--multiple", "--separator=\n", "--title="+title).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return []string{}, nil // cancelled
	}
	if err != nil {
		return nil, err
	}
	return lines(string(out)), nil
}
