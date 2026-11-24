// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"path/filepath"
	"strings"
)

// longPath lets Windows take a path longer than its 260 characters, as the os package does: an
// absolute path gets the \\?\ prefix (\\?\UNC\ for a network share).
func longPath(path string) string {
	if len(path) < 248 || !filepath.IsAbs(path) || strings.HasPrefix(path, `\\?\`) {
		return path
	}
	path = filepath.Clean(path)
	if strings.HasPrefix(path, `\\`) {
		return `\\?\UNC\` + path[2:]
	}
	return `\\?\` + path
}
