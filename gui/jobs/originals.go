// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/DeLFuS77/maxpack/gui/internal/trash"
)

// The original of a game compressed goes, with Options.Originals, only once its .p5z or folder
// was made, verified and took its name: to the recycle bin of the system (OriginalsTrash), from
// where it can be put back until the bin is emptied, or for good (OriginalsDelete), which frees its
// space at once. Where there is no recycle bin (a network folder, a USB drive, a game bigger than
// the bin holds), it stays, or goes for good when the user said so (OriginalsNoBin). Decompress
// and Verify never take an original away.

// moveToTrash and removeForGood are how an original goes (variables so the tests stand in).
var (
	moveToTrash   = trash.Move
	removeForGood = os.RemoveAll
)

// How an original ended.
const (
	originalStays   = iota // nothing was asked, or it could not go (with why)
	originalTrashed        // in the recycle bin
	originalDeleted        // deleted for good
	originalKept           // kept: its drive has no recycle bin, or it does not fit in it
)

// originalFate is what became of an original: how, the size it took, and the note for the
// detail of its game ("original_trashed", "original_deleted", "original_kept_<why>" or
// "original_not_removed: <why>").
type originalFate struct {
	how  int
	size uint64
	note string
}

// keptCodes are the notes of an original kept, by why it could not go to the recycle bin.
var keptCodes = map[string]string{
	trash.NoBin:   "original_kept_no_bin",
	trash.TooBig:  "original_kept_too_big",
	trash.TooLong: "original_kept_too_long",
}

// removeOriginal takes away the original of a game whose new one is in its place, as o says, and
// logs it. It does nothing when the new one is not there, or the original is not one it may
// take: the root of a drive, or the same place as the new one.
func (r *Runner) removeOriginal(p Plan, o Options) originalFate {
	source := filepath.Clean(p.Item.Path)
	if _, err := os.Stat(p.Out); err != nil || source == "" || filepath.Dir(source) == source || conflicts(source, p.Out) {
		return originalFate{}
	}
	if _, err := os.Lstat(source); err != nil {
		return originalFate{}
	}
	size := SizeOf(source)
	about := fmt.Sprintf("%s (%s), once %s was verified", source, sizeWords(size), p.Out)
	if o.Originals == OriginalsTrash {
		err := moveToTrash(source)
		if err == nil {
			r.log("Sent to the recycle bin: %s", about)
			return originalFate{how: originalTrashed, size: size, note: "original_trashed"}
		}
		why, ok := trash.Why(err)
		switch {
		case !ok:
			r.log("Could not send to the recycle bin, kept: %s: %v", about, err)
			return originalFate{note: "original_not_removed: " + err.Error()}
		case !o.OriginalsNoBin:
			r.log("Kept (%s): %s", why.Reason, about)
			code := keptCodes[why.Reason]
			if code == "" {
				code = "original_kept_no_bin"
			}
			return originalFate{how: originalKept, note: code}
		}
		if err := removeForGood(source); err != nil {
			r.log("Could not delete, %s: %s: %v", why.Reason, about, err)
			return originalFate{note: "original_not_removed: " + err.Error()}
		}
		r.log("Deleted for good (%s): %s", why.Reason, about)
		return originalFate{how: originalDeleted, size: size, note: "original_deleted"}
	}
	if err := removeForGood(source); err != nil {
		r.log("Could not delete: %s: %v", about, err)
		return originalFate{note: "original_not_removed: " + err.Error()}
	}
	r.log("Deleted for good: %s", about)
	return originalFate{how: originalDeleted, size: size, note: "original_deleted"}
}

// SizeOf is what a file or a folder takes, its files added up.
func SizeOf(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return uint64(info.Size())
	}
	var total uint64
	filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += uint64(info.Size())
			}
		}
		return nil
	})
	return total
}

// sizeWords is a size for the log: in steps of 1024 with one decimal, and its bytes.
func sizeWords(bytes uint64) string {
	units := []string{"KB", "MB", "GB", "TB"}
	value, unit := float64(bytes), -1
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit < 0 {
		return fmt.Sprintf("%d bytes", bytes)
	}
	return fmt.Sprintf("%.1f %s, %d bytes", value, units[unit], bytes)
}
