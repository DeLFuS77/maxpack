// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
)

// A game packed already with zstd packs (Dump Forge, Alembic, a folder for the console) is compressed from
// the files it was made of: info --json says its packs, their level and what its files take unpacked.
func TestFromInfoPacked(t *testing.T) {
	it := FromInfo("x", map[string]any{"kind": "folder", "packed": true, "packs": "zstd", "packed_level": float64(5),
		"unpacked_size": float64(300), "original_size": float64(100), "has_zstd_runtime": true})
	if !it.Packed || it.Packs != "zstd" || it.PackedLevel != 5 || it.UnpackedSize != 300 || !it.PackedZstd() || !it.PackedAlready() || it.CannotCompress() {
		t.Errorf("zstd: %+v", it)
	}
	it = FromInfo("x", map[string]any{"kind": "exfat", "packed": true, "packs": "zstd", "packed_level": float64(19)})
	if !it.PackedZstd() || !it.PackedAlready() || it.CannotCompress() {
		t.Errorf("an image with packs: %+v", it)
	}
	// a game with LZ4 packs is compressed from its files too, always again (its packs cannot be kept); a .p5z
	// is compressed already; a dump is none of them
	if it = FromInfo("x", map[string]any{"kind": "folder", "packs": "lz4", "unpacked_size": float64(300)}); it.PackedZstd() ||
		!it.PackedAlready() || it.CannotCompress() || it.UnpackedSize != 300 {
		t.Errorf("lz4: %+v", it)
	}
	if it = FromInfo("x.p5z", map[string]any{"kind": "p5z", "packed": true}); it.PackedZstd() || it.PackedAlready() || !it.CannotCompress() {
		t.Errorf("p5z: %+v", it)
	}
	if it = FromInfo("x", map[string]any{"kind": "folder"}); it.PackedZstd() || it.PackedAlready() || it.CannotCompress() || it.Packs != "" {
		t.Errorf("dump: %+v", it)
	}
}

func packedItem(t *testing.T) Item {
	it := item("Starfall Odyssey", "PPSA01234", 100)
	it.Path = filepath.Join(t.TempDir(), "PPSA01234-packed")
	it.Packed, it.Packs, it.PackedLevel, it.UnpackedSize = true, "zstd", 5, 300
	return it
}

func lz4Item(t *testing.T) Item {
	it := item("Starfall Odyssey", "PPSA01236", 100)
	it.Path = filepath.Join(t.TempDir(), "PPSA01236-packed")
	it.Packs, it.UnpackedSize, it.HasZstdRuntime = "lz4", 300, true
	return it
}

// Its packs are kept only when the user says so, and only zstd ones (LZ4 ones are always compressed again);
// its runtime is left out, so the one of Settings goes in, even when it read zstd; it needs the room of its
// files unpacked, the most a .p5z of it may take.
func TestCompressPackedGame(t *testing.T) {
	packed, dump, lz4 := packedItem(t), item("Copper Vale", "PPSA01235", 100), lz4Item(t)
	for _, keep := range []bool{false, true} {
		for _, mode := range []Mode{ModeP5z, ModeFolder} {
			o := Options{Mode: mode, Dest: t.TempDir(), KeepPacks: keep, Runtime: "ampr.sprx"}
			plans := PlanCompress([]Item{packed, dump, lz4}, o, newFS())
			if hasArg(plans[0].Args, "--keep-packs") != keep || hasArg(plans[1].Args, "--keep-packs") || hasArg(plans[2].Args, "--keep-packs") {
				t.Errorf("%s keep %v: %v / %v / %v", mode, keep, plans[0].Args, plans[1].Args, plans[2].Args)
			}
			if !hasArg(plans[0].Args, "--runtime") || hasArg(plans[1].Args, "--runtime") || !hasArg(plans[2].Args, "--runtime") {
				t.Errorf("%s runtime: %v / %v / %v", mode, plans[0].Args, plans[1].Args, plans[2].Args)
			}
			if plans[0].Need != 300 || plans[1].Need != 100 || plans[2].Need != 300 {
				t.Errorf("%s need: %d / %d / %d", mode, plans[0].Need, plans[1].Need, plans[2].Need)
			}
			if plans[2].Block != "" {
				t.Errorf("%s: the LZ4 game does not go: %q", mode, plans[2].Block)
			}
		}
	}
	// a folder for the console without a runtime of Settings lacks one: the packed game's own is left out
	plans := PlanCompress([]Item{packed, dump, lz4}, Options{Mode: ModeFolder, Dest: t.TempDir()}, newFS())
	if !hasWarning(plans[0], "needs_runtime") || hasWarning(plans[1], "needs_runtime") || !hasWarning(plans[2], "needs_runtime") {
		t.Errorf("needs_runtime: %v / %v / %v", plans[0].Warnings, plans[1].Warnings, plans[2].Warnings)
	}
}

// Once made, its row says what it was made from: a packed game compressed again, or with its packs kept
// at their level.
func TestPackedGameSaysWhatItWasMadeFrom(t *testing.T) {
	t.Setenv(fakecore.Env, "from-packed")
	for _, c := range []struct {
		keep bool
		note string
	}{{false, "from_packed"}, {true, "from_packed_kept:5"}} {
		packed := packedItem(t)
		o := Options{Mode: ModeP5z, Dest: t.TempDir(), KeepPacks: c.keep}
		on, state, detail := lastDetail()
		sum := (&Runner{Core: os.Args[0], OnItem: on}).Run(context.Background(), PlanCompress([]Item{packed}, o, newFS()), o)
		if sum.Done != 1 || *state != StateOK || !hasPart(*detail, c.note) {
			t.Errorf("keep %v: %+v, %s %q", c.keep, sum, *state, *detail)
		}
		// saved against what the packed folder takes on disk (100), not its files unpacked (the engine's 10)
		if sum.Saved != 100-8 {
			t.Errorf("keep %v: saved %d", c.keep, sum.Saved)
		}
	}
	// one with LZ4 packs is compressed again, Settings' choice aside, and saved against its folder too
	for _, keep := range []bool{false, true} {
		o := Options{Mode: ModeP5z, Dest: t.TempDir(), KeepPacks: keep}
		on, state, detail := lastDetail()
		sum := (&Runner{Core: os.Args[0], OnItem: on}).Run(context.Background(), PlanCompress([]Item{lz4Item(t)}, o, newFS()), o)
		if sum.Done != 1 || *state != StateOK || !hasPart(*detail, "from_packed") || sum.Saved != 100-8 {
			t.Errorf("lz4 keep %v: %+v, %s %q", keep, sum, *state, *detail)
		}
	}
	// a dump says nothing of it
	o := Options{Mode: ModeP5z, Dest: t.TempDir()}
	on, state, detail := lastDetail()
	dump := item("Copper Vale", "PPSA01235", 100)
	dump.Path = filepath.Join(t.TempDir(), "PPSA01235-dump")
	sum := (&Runner{Core: os.Args[0], OnItem: on}).Run(context.Background(), PlanCompress([]Item{dump}, o, newFS()), o)
	if sum.Done != 1 || *state != StateOK || *detail != "" {
		t.Errorf("dump: %+v, %s %q", sum, *state, *detail)
	}
}

// With the real engine: a folder packed at level 3 is described as packed, and compressed again (or with its
// packs kept) into a .p5z that verifies, its row saying so.
func TestCompressPackedGameReal(t *testing.T) {
	engine, fixtures := needCore(t)
	dump := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	o := Options{Mode: ModeFolder, Dest: t.TempDir(), Level: 3}
	plans := PlanCompress([]Item{dump}, o, newFS())
	if sum := (&Runner{Core: engine}).Run(context.Background(), plans, o); sum.Done != 1 {
		t.Fatalf("the packed folder: %+v", sum)
	}
	packed := describe(t, engine, plans[0].Out)
	if !packed.PackedZstd() || packed.PackedLevel != 3 || packed.UnpackedSize != dump.OriginalSize {
		t.Fatalf("%+v", packed)
	}
	for _, c := range []struct {
		keep bool
		note string
	}{{false, "from_packed"}, {true, "from_packed_kept:3"}} {
		o := Options{Mode: ModeP5z, Dest: t.TempDir(), Level: 9, KeepPacks: c.keep}
		on, state, detail := lastDetail()
		plans := PlanCompress([]Item{packed}, o, newFS())
		sum := (&Runner{Core: engine, OnItem: on}).Run(context.Background(), plans, o)
		if sum.Done != 1 || *state != StateOK || !hasPart(*detail, c.note) {
			t.Errorf("keep %v: %+v, %s %q", c.keep, sum, *state, *detail)
		}
		made := describe(t, engine, plans[0].Out)
		if made.Kind != "p5z" || made.OriginalSize != dump.OriginalSize {
			t.Errorf("keep %v: %+v", c.keep, made)
		}
		// what the disk gets back: the packed folder against its .p5z, not the game unpacked against it
		info, _ := os.Stat(plans[0].Out)
		if want := uint64(0); info != nil && packed.OriginalSize > uint64(info.Size()) {
			want = packed.OriginalSize - uint64(info.Size())
			if sum.Saved != want {
				t.Errorf("keep %v: saved %d, the disk got back %d", c.keep, sum.Saved, want)
			}
		} else if sum.Saved != want {
			t.Errorf("keep %v: saved %d, the .p5z is not smaller", c.keep, sum.Saved)
		}
	}
}
