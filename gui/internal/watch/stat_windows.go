// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package watch

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// fileState is a file's size and date as the file itself says them (a listing may not be current while it is
// written), opened letting other programs only read it: a program that holds it open for writing makes that
// fail with a sharing violation, and the file is held (still being written).
func fileState(path string) (size, mtime int64, held bool, err error) {
	if abs, e := filepath.Abs(path); e == nil && len(abs) >= 248 && !strings.HasPrefix(abs, `\\`) {
		path = `\\?\` + abs
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return 0, 0, true, nil
		}
		return 0, 0, false, err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return 0, 0, false, err
	}
	return int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow), info.LastWriteTime.Nanoseconds(), false, nil
}
