// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import "os"

// swap puts the new one (tmp) where the old one (out) is: the old one is moved aside to
// <out>.maxpack-old, the new one takes its name and only then is the old one deleted. If any
// step fails, what was done is undone and the old one is as it was. leftover is true when the
// swap was done but the old one could not be deleted.
func swap(out, tmp string) (leftover bool, err error) {
	old := out + ".maxpack-old"
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
