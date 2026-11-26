// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"errors"
	"os"
	"time"
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

// errNotTheOne: what was set aside to be replaced is not the one it was to be (see swapIf); it is back.
var errNotTheOne = errors.New("what is there is not the one to replace")

// swap puts the new one (tmp) where the old one (out) is: the old one is moved aside to
// <out>.maxpack-old, the new one takes its name and only then is the old one deleted. If any
// step fails, what was done is undone and the old one is as it was. leftover is true when the
// swap was done but the old one could not be deleted.
func swap(out, tmp string) (leftover bool, err error) { return swapIf(out, tmp, nil) }

// swapIf is swap, but once the old one is set aside check (when given) looks at it there: when it says no, the old
// one gets its name back and nothing is replaced (errNotTheOne). Each rename waits a few seconds for a program
// that holds the file (renameWaiting).
func swapIf(out, tmp string, check func(old string) bool) (leftover bool, err error) {
	old := out + OldSuffix
	// a leftover of an earlier run that was cut short
	if err := os.RemoveAll(old); err != nil {
		return false, err
	}
	if err := renameWaiting(out, old); err != nil {
		return false, err
	}
	if check != nil && !check(old) {
		if back := renameWaiting(old, out); back != nil {
			return false, back
		}
		return false, errNotTheOne
	}
	if err := renameWaiting(tmp, out); err != nil {
		if back := renameWaiting(old, out); back != nil {
			return false, back
		}
		return false, err
	}
	return os.RemoveAll(old) != nil, nil
}

// renameWaiting is os.Rename, tried again for up to 5 s while another program holds the file (the Explorer making
// its cover, an antivirus checking it), which usually lets go of it a moment later.
func renameWaiting(from, to string) error {
	var err error
	for try := 0; try < 20; try++ {
		if err = os.Rename(from, to); err == nil || !heldByAnother(err) {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return err
}
