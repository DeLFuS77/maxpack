// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// renameNew gives a file or a folder a new name only when nothing has that name: Windows moves it
// without MOVEFILE_REPLACE_EXISTING, so what came there meanwhile is never replaced (os.Rename
// would replace a file).
func renameNew(from, to string) error {
	source, err := windows.UTF16PtrFromString(longPath(from))
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	target, err := windows.UTF16PtrFromString(longPath(to))
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	if err := windows.MoveFileEx(source, target, 0); err != nil {
		if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_FILE_EXISTS {
			err = os.ErrExist
		}
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}

// inUse tells whether a rename failed because another program has the file open.
func inUse(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// heldByAnother tells whether a rename may work a moment later: another program has the file open without
// sharing its delete (a sharing violation, or an access denied as some scanners give).
func heldByAnother(err error) bool { return inUse(err) || errors.Is(err, windows.ERROR_ACCESS_DENIED) }
