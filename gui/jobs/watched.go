// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

// What the watched folder brings (Item.Watched) is compressed with nobody there to say that it had ended
// arriving, so it never loses its original: it is only compressed (and sent, when asked), whatever Settings or
// the queue it joined say of originals. And it is compressed only as it was when its compression began: one
// whose files changed meanwhile (it was still arriving) fails, what was made of it goes, and the watched folder
// brings it again once it is whole.

import "github.com/DeLFuS77/maxpack/gui/internal/watch"

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
