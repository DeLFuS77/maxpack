// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

// What the watched folder brings (Item.Watched) is compressed with nobody there to say that it had ended
// arriving, so it never loses its original: it is only compressed (and sent, when asked), whatever Settings or
// the queue it joined say of originals. And it is compressed only as it was when its compression began: one
// whose files changed meanwhile (it was still arriving) fails, what was made of it goes, and the watched folder
// brings it again once it is whole.

import (
	"strconv"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/internal/watch"
)

// originalsFor is o with the rule for the original of p: what the watched folder brought keeps it, always.
func originalsFor(p Plan, o Options) Options {
	if p.Item.Watched {
		o.Originals, o.OriginalsNoBin = OriginalsKeep, false
	}
	return o
}

// sourceChanged tells whether the source of a watched game is not, file by file, what it was before its
// compression (before, and whether it was being written then), or could not be read either time.
func sourceChanged(p Plan, before string, beforeBusy bool) bool {
	after, busy, unreadable := watch.Signature(p.Item.Path)
	return beforeBusy || busy || unreadable || before == "" || before == "?" || after != before
}

// VersionSkip is the detail of a game the watched folder brought in version dump whose .p5z in the destination is
// of version there: a newer dump points to Details › Update with a new dump… ("watch_other_version: there dump"),
// an older one says the destination has a newer one ("watch_older_version: there dump"), and versions that cannot
// be told apart (or the same) say it is there ("watch_exists").
func VersionSkip(there, dump string) string {
	if strings.ContainsAny(there+dump, " \t") {
		return "watch_exists"
	}
	switch c, ok := CompareVersions(dump, there); {
	case !ok || c == 0:
		return "watch_exists"
	case c > 0:
		return "watch_other_version: " + there + " " + dump
	}
	return "watch_older_version: " + there + " " + dump
}

// CompareVersions compares two versions of a game ("01.002.000"), part by part as numbers: -1, 0 or 1, and ok
// false when either is not so written.
func CompareVersions(a, b string) (int, bool) {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	if a == "" || b == "" || len(x) != len(y) {
		return 0, false
	}
	for i := range x {
		m, errM := strconv.Atoi(x[i])
		n, errN := strconv.Atoi(y[i])
		if errM != nil || errN != nil {
			return 0, false
		}
		if m != n {
			if m < n {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}
