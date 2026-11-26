// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/internal/trash"
)

// complete is what the engine says of a dump that has every file of its list, with its size.
func complete() *Completeness {
	return &Completeness{Checkable: true, Source: "dump_index", Listed: 3, Present: 3}
}

// What the watched folder brings follows its own rule for its original, from Settings (WatchOriginals), whatever
// the queue it joined says: to the recycle bin at most, never for good, and the ones the bin cannot take stay.
// And only when it is as it was when its compression began and its list says it is complete; else it stays, and
// its detail says why.
func TestWatchedOriginals(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, c := range []struct {
		name, watchOriginals, noBin string
		completeness                *Completeness
		change                      bool
		note                        string
		trashed                     bool
	}{
		{"to the bin", OriginalsTrash, "", complete(), false, "original_trashed", true},
		{"no bin: stays", OriginalsTrash, trash.NoBin, complete(), false, "original_kept_no_bin", false},
		{"Settings keeps them", OriginalsKeep, "", complete(), false, "", false},
		{"no list to check", OriginalsTrash, "", &Completeness{Checkable: false, Reason: "no_table"}, false, "original_kept_unchecked", false},
		{"files missing", OriginalsTrash, "", &Completeness{Checkable: true, Source: "playgo", Missing: 2}, false, "original_kept_unchecked", false},
		{"other sizes", OriginalsTrash, "", &Completeness{Checkable: true, Source: "dump_index", SizeMismatch: 1}, false, "original_kept_unchecked", false},
		{"changed meanwhile", OriginalsTrash, "", complete(), true, "original_kept_changed", false},
	} {
		bin := &fakeBin{why: c.noBin}
		useBin(t, bin)
		it := source(t, "Starfall Odyssey", "PPSA10001")
		it.Watched, it.Completeness = true, c.completeness
		// the queue it joined deletes for good, those the bin cannot take too: not for this one
		o := Options{Mode: ModeP5z, Dest: t.TempDir(), Originals: OriginalsDelete, OriginalsNoBin: true, WatchOriginals: c.watchOriginals}
		plans := PlanCompress([]Item{it}, o, newFS())
		var state, detail string
		onItem := func(i int, s, d string) {
			state, detail = s, d
			if s == StateVerifying && c.change { // a file more arrives while it is compressed
				os.WriteFile(filepath.Join(it.Path, "late.bin"), []byte("late"), 0o644)
			}
		}
		sum := (&Runner{Core: os.Args[0], OnItem: onItem}).Run(context.Background(), plans, o)
		if state != StateOK || detail != c.note || sum.Done != 1 {
			t.Errorf("%s: %s %q", c.name, state, detail)
		}
		if len(bin.deleted) != 0 {
			t.Errorf("%s: deleted for good: %v", c.name, bin.deleted)
		}
		if (len(bin.trashed) == 1) != c.trashed {
			t.Errorf("%s: trashed %v", c.name, bin.trashed)
		}
		if _, err := os.Stat(it.Path); (err == nil) == c.trashed {
			t.Errorf("%s: the original is there: %v", c.name, err == nil)
		}
	}
	// one the user dropped follows the queue, as before
	bin := &fakeBin{}
	useBin(t, bin)
	it := source(t, "Starfall Odyssey", "PPSA10001")
	o := Options{Mode: ModeP5z, Dest: t.TempDir(), Originals: OriginalsDelete, WatchOriginals: OriginalsKeep}
	var detail string
	(&Runner{Core: os.Args[0], OnItem: func(i int, s, d string) { detail = d }}).Run(context.Background(), PlanCompress([]Item{it}, o, newFS()), o)
	if !strings.Contains(detail, "original_deleted") || len(bin.deleted) != 1 {
		t.Errorf("dropped: %q %v", detail, bin.deleted)
	}
}
