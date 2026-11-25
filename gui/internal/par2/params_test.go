// SPDX-License-Identifier: GPL-3.0-or-later

package par2

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestBlocksOfAFile(t *testing.T) {
	const gb, mib = 1_000_000_000, 1 << 20
	cases := []struct {
		size                   uint64
		percent                int
		blockSize, source, rec uint64
	}{
		// a small file: blocks of 4 KiB at least, and always a recovery block
		{10_000, 5, 4096, 3, 1},
		{2_000_000, 1, 4096, 489, 5},
		// about 4000 blocks, each a whole number of 4 KiB (a bad sector is in one block only)
		{5_631_804_655, 5, 1_409_024, 3997, 200},
		{5_631_804_655, 1, 1_409_024, 3997, 40},
		{5_631_804_655, 10, 1_409_024, 3997, 400},
		{100 * gb, 5, 25_001_984, 4000, 200},
		// a big game: blocks of 32 MiB at most, so more of them
		{200 * gb, 5, 32 * mib, 5961, 298},
		// past 1 TiB: the 32768 blocks PAR2 takes at most
		{2_000 * gb, 1, 61_038_592, 32767, 328},
	}
	for _, c := range cases {
		bs, source, rec := Blocks(c.size, c.percent)
		if bs != c.blockSize || source != c.source || rec != c.rec {
			t.Errorf("%d bytes at %d %%: blocks of %d, %d, %d recovery; expected %d, %d, %d", c.size, c.percent, bs, source, rec, c.blockSize, c.source, c.rec)
		}
		if bs%4096 != 0 || source > MaxSourceBlocks {
			t.Errorf("%d bytes: blocks of %d, %d of them", c.size, bs, source)
		}
	}
}

func TestSetSize(t *testing.T) {
	// what the set takes: its recovery blocks, with the packets that describe the file in the index
	// and again in the recovery file (par2 writes them as often as its recovery blocks have bits);
	// the sets par2 makes are compared with it in TestCreateRealSet
	for _, c := range []struct {
		size    uint64
		percent int
	}{{5_631_804_655, 5}, {5_631_804_655, 1}, {100_000_000_000, 10}, {10_000, 5}} {
		bs, source, rec := Blocks(c.size, c.percent)
		got, least := SetSize(c.size, c.percent), rec*(bs+68)+source*20*2
		if got < least || got > least+least/100+64<<10 {
			t.Errorf("%d bytes at %d %%: %d, at least %d", c.size, c.percent, got, least)
		}
	}
	if SetSize(0, 5) != 0 || SetSize(10, 0) != 0 {
		t.Error("nothing to protect takes something")
	}
}

func TestCreateArgs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Juegos")
	file := filepath.Join(dir, "Juego [PPSA10001].p5z")
	index := filepath.Join(dir, "Juego [PPSA10001].p5z.maxpack-par2", "Juego [PPSA10001].p5z.par2")
	got := CreateArgs(Create{File: file, Index: index, BlockSize: 1_409_024, Recovery: 200, Threads: 24})
	// the file is named in the set by its name alone: the base path is its folder
	want := []string{"create", "-s1409024", "-c200", "-n1", "-m960", "-t24", "-B" + dir, "--", index, file}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q\nexpected %q", got, want)
	}
	// all the threads: par2 takes the ones it detects
	got = CreateArgs(Create{File: file, Index: index, BlockSize: 4096, Recovery: 1})
	want = []string{"create", "-s4096", "-c1", "-n1", "-m960", "-B" + dir, "--", index, file}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q\nexpected %q", got, want)
	}
}

func TestRepairArgs(t *testing.T) {
	dir := t.TempDir()
	index, folder := filepath.Join(dir, "a.p5z.par2"), filepath.Join(dir, "a.p5z.maxpack-repair")
	// the file is looked for, and written again, in the folder of the repair
	got := RepairArgs(Repair{Index: index, Folder: folder, Threads: 8})
	want := []string{"repair", "-m960", "-t8", "-B" + folder, "--", index}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q\nexpected %q", got, want)
	}
	// verifying a .p5z by its own name, wherever its set says it was
	file := filepath.Join(dir, "b.p5z")
	got = VerifyArgs(index, file, 0)
	want = []string{"verify", "-m960", "-B" + dir, "--", index, file}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q\nexpected %q", got, want)
	}
}
