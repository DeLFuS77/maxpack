// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"errors"
	"os/exec"
	"strings"
)

// ChooseFolders asks for folders, several at once, with the folder panel of macOS ("choose folder
// with multiple selections allowed", through osascript); none when it is closed without them.
func ChooseFolders(title string) ([]string, error) {
	script := []string{
		`set chosen to choose folder with prompt ` + appleText(title) + ` with multiple selections allowed`,
		`set out to ""`,
		`repeat with f in chosen`,
		`set out to out & POSIX path of f & linefeed`,
		`end repeat`,
		`return out`,
	}
	args := []string{}
	for _, line := range script {
		args = append(args, "-e", line)
	}
	out, err := exec.Command("osascript", args...).Output()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit) && strings.Contains(string(exit.Stderr), "-128"):
		return []string{}, nil // cancelled
	case errors.Is(err, exec.ErrNotFound):
		return nil, ErrNoMultiPicker
	case err != nil:
		return nil, err
	}
	return lines(string(out)), nil
}

// appleText is a text as AppleScript writes it, in quotes.
func appleText(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
