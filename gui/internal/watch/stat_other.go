// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package watch

import "os"

// fileState is a file's size and date (lstat is current on macOS and Linux). The open does not tell a file still
// held open for writing apart there: on Linux /proc does (heldOpen, for the user's own programs); on macOS only
// the quiet minute and the downloads' own names do.
func fileState(path string) (size, mtime int64, held bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, 0, false, err
	}
	return info.Size(), info.ModTime().UnixNano(), false, nil
}
