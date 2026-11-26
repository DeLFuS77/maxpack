// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
)

// What the watched folder brings keeps its original, always: it is only compressed, whatever the queue it joined
// does with the others and whatever Settings says; its detail says so.
func TestWatchedOriginalsStay(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, c := range []struct {
		originals string
		noBin     bool
	}{{OriginalsDelete, true}, {OriginalsTrash, false}, {OriginalsKeep, false}} {
		bin := &fakeBin{}
		useBin(t, bin)
		it := source(t, "Starfall Odyssey", "PPSA10001")
		it.Watched = true
		o := Options{Mode: ModeP5z, Dest: t.TempDir(), Originals: c.originals, OriginalsNoBin: c.noBin}
		var state, detail string
		sum := (&Runner{Core: os.Args[0], OnItem: func(i int, s, d string) { state, detail = s, d }}).Run(context.Background(), PlanCompress([]Item{it}, o, newFS()), o)
		if state != StateOK || detail != "original_kept_watched" || sum.Done != 1 || sum.Trashed+sum.Deleted != 0 {
			t.Errorf("%s: %s %q %+v", c.originals, state, detail, sum)
		}
		if len(bin.trashed)+len(bin.deleted) != 0 {
			t.Errorf("%s: the original went: %v %v", c.originals, bin.trashed, bin.deleted)
		}
		if _, err := os.Stat(it.Path); err != nil {
			t.Errorf("%s: the original is gone", c.originals)
		}
	}
	// one the user dropped follows the queue, as before
	bin := &fakeBin{}
	useBin(t, bin)
	it := source(t, "Starfall Odyssey", "PPSA10001")
	o := Options{Mode: ModeP5z, Dest: t.TempDir(), Originals: OriginalsDelete}
	var detail string
	(&Runner{Core: os.Args[0], OnItem: func(i int, s, d string) { detail = d }}).Run(context.Background(), PlanCompress([]Item{it}, o, newFS()), o)
	if !strings.Contains(detail, "original_deleted") || len(bin.deleted) != 1 {
		t.Errorf("dropped: %q %v", detail, bin.deleted)
	}
}

// A watched game whose files changed while it was compressed (it was still arriving) fails: what was made is
// taken away, nothing takes its name, and the watched folder brings it again once it is whole.
func TestWatchedChangedWhileCompressed(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	it := source(t, "Starfall Odyssey", "PPSA10001")
	it.Watched = true
	dest := t.TempDir()
	o := Options{Mode: ModeP5z, Dest: dest}
	plans := PlanCompress([]Item{it}, o, newFS())
	var state, detail string
	onItem := func(i int, s, d string) {
		state, detail = s, d
		if s == StateVerifying { // a file more arrives while it is compressed
			os.WriteFile(filepath.Join(it.Path, "late.bin"), []byte("late"), 0o644)
		}
	}
	sum := (&Runner{Core: os.Args[0], OnItem: onItem}).Run(context.Background(), plans, o)
	if state != StateError || detail != "source_changed" || sum.Failed != 1 {
		t.Errorf("%s %q %+v", state, detail, sum)
	}
	if _, err := os.Stat(plans[0].Out); err == nil {
		t.Error("what was made took its name")
	}
	if _, err := os.Stat(plans[0].Out + TmpSuffix); err == nil {
		t.Error("what was made is left apart")
	}
	if _, err := os.Stat(it.Path); err != nil {
		t.Error("the original is gone")
	}
	// one the user dropped is not checked: as before
	plain := source(t, "Starfall Odyssey", "PPSA10001")
	plans = PlanCompress([]Item{plain}, Options{Mode: ModeP5z, Dest: t.TempDir()}, newFS())
	onItem = func(i int, s, d string) {
		state = s
		if s == StateVerifying {
			os.WriteFile(filepath.Join(plain.Path, "late.bin"), []byte("late"), 0o644)
		}
	}
	(&Runner{Core: os.Args[0], OnItem: onItem}).Run(context.Background(), plans, Options{Mode: ModeP5z, Dest: filepath.Dir(plans[0].Out)})
	if state != StateOK {
		t.Errorf("a dropped game: %s", state)
	}
}
