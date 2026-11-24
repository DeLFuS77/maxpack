// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package jobs

import (
	"errors"
	"io/fs"
	"os"
)

// renameNew gives a file or a folder a new name only when nothing has that name: a file by a hard
// link, which fails when the name is taken, and its old name goes once the new one is there. On a
// drive without hard links (FAT, exFAT, some network shares), and for a folder, the name is
// looked at first: os.Rename would replace a file there, and an empty folder.
func renameNew(from, to string) error {
	if info, err := os.Lstat(from); err == nil && !info.IsDir() {
		err := os.Link(from, to)
		if err == nil {
			os.Remove(from)
			return nil
		}
		if errors.Is(err, fs.ErrExist) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: fs.ErrExist}
		}
	}
	if _, err := os.Lstat(to); err == nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: fs.ErrExist}
	}
	return os.Rename(from, to)
}
