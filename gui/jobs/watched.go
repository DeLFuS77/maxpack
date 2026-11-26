// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

// What the watched folder brings (Item.Watched) is compressed with nobody there to say that it had ended
// arriving, so its original has a rule of its own: Settings' (Options.WatchOriginals), whatever the queue it
// joined says; to the recycle bin at most, never for good, and the ones the bin cannot take stay. And it goes
// only when its list said it was complete (every file, of its size) and it is, file by file, as it was when its
// compression began; else it stays, and its detail says why.

import "github.com/DeLFuS77/maxpack/gui/internal/watch"

// originalsFor is o with the rule for the original of p.
func originalsFor(p Plan, o Options) Options {
	if !p.Item.Watched {
		return o
	}
	o.Originals, o.OriginalsNoBin = OriginalsKeep, false
	if o.WatchOriginals == OriginalsTrash || o.WatchOriginals == OriginalsDelete {
		o.Originals = OriginalsTrash // never for good
	}
	return o
}

// watchedKeep says why the original of a watched game stays although its rule says it goes ("" when it may go):
// its list did not say it was complete, or it changed since before (its signature when its compression began).
func watchedKeep(p Plan, before string) string {
	c := p.Item.Completeness
	if c == nil || !c.Checkable || c.Missing > 0 || c.SizeMismatch > 0 {
		return "original_kept_unchecked"
	}
	if after, busy := watch.Signature(p.Item.Path); busy || before == "" || after != before {
		return "original_kept_changed"
	}
	return ""
}

// watchedKeepOf is watchedKeep for what the watched folder brought, "" for anything else.
func watchedKeepOf(p Plan, before string) string {
	if !p.Item.Watched {
		return ""
	}
	return watchedKeep(p, before)
}
