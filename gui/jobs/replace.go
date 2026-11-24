// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"errors"
	"os"
)

// errInTheWay: something came to the place of the new one while it was made (it was not there
// when the queue was planned), and it is not replaced.
var errInTheWay = errors.New("something is in its place")

// place gives the new one (tmp) its name, out, when nothing is there: an empty folder there goes
// first (and comes back if the new one cannot take its place); anything else stays as it is, and
// the new one does not take its place (errInTheWay).
func place(out, tmp string) error {
	emptied := false
	switch outState(out) {
	case outFull:
		return errInTheWay
	case outEmpty:
		if err := os.Remove(out); err != nil {
			return err
		}
		emptied = true
	}
	if err := renameNew(tmp, out); err != nil {
		if emptied {
			os.Mkdir(out, 0o755)
		}
		if errors.Is(err, os.ErrExist) {
			return errInTheWay
		}
		return err
	}
	return nil
}

// swap puts the new one (tmp) where the old one (out) is: the old one is moved aside to
// <out>.maxpack-old, the new one takes its name and only then is the old one deleted. If any
// step fails, what was done is undone and the old one is as it was. leftover is true when the
// swap was done but the old one could not be deleted.
func swap(out, tmp string) (leftover bool, err error) {
	old := out + OldSuffix
	// a leftover of an earlier run that was cut short
	if err := os.RemoveAll(old); err != nil {
		return false, err
	}
	if err := os.Rename(out, old); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, out); err != nil {
		if back := os.Rename(old, out); back != nil {
			return false, back
		}
		return false, err
	}
	return os.RemoveAll(old) != nil, nil
}
