// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"errors"
	"strings"
)

// ErrNoMultiPicker: this system has no dialog that takes several folders at once.
var ErrNoMultiPicker = errors.New("no dialog for several folders here")

// lines are the paths of an answer, one a line, without the empty ones and the slash at the end
// (macOS ends a folder with one).
func lines(text string) []string {
	paths := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if len(line) > 1 {
				line = strings.TrimRight(line, "/")
			}
			paths = append(paths, line)
		}
	}
	return paths
}
