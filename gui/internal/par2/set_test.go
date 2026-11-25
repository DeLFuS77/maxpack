// SPDX-License-Identifier: GPL-3.0-or-later

package par2

import (
	"crypto/md5"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"testing"
)

// protected writes a .p5z (data) with a set next to it, as MaxPack names it: <name>.par2 and one
// recovery file <name>.vol0+N.par2.
func protected(t *testing.T, dir, name string, data []byte, blockSize int, exponents ...uint32) string {
	t.Helper()
	p5z := writeFile(t, filepath.Join(dir, name), data)
	_, index, recovery := fakeSet(name, data, blockSize, exponents...)
	writeFile(t, p5z+".par2", index)
	writeFile(t, p5z+".vol0+"+itoa(len(exponents))+".par2", recovery)
	return p5z
}

func itoa(n int) string { return strconv.Itoa(n) }

func md5ID(data []byte) ID { return ID(md5.Sum(data)) }

func TestFilesOfASet(t *testing.T) {
	dir := t.TempDir()
	p5z := protected(t, dir, "Juego [PPSA10001].p5z", noise(40_000, 1), 4096, 0, 1)
	// what is not of its set: another .p5z whose name starts the same, a file of another kind
	writeFile(t, filepath.Join(dir, "Juego [PPSA10001].p5z.txt"), []byte("x"))
	writeFile(t, filepath.Join(dir, "Juego [PPSA10001].p5z.old.par2"), []byte("x"))
	writeFile(t, filepath.Join(dir, "Juego [PPSA10001].p5z2.par2"), []byte("x"))
	writeFile(t, filepath.Join(dir, "Juego [PPSA10001].p5z.vol1+1.par2.tmp"), []byte("x"))
	got := Files(p5z)
	var names []string
	for _, f := range got {
		names = append(names, filepath.Base(f))
	}
	sort.Strings(names)
	want := []string{"Juego [PPSA10001].p5z.par2", "Juego [PPSA10001].p5z.vol0+2.par2"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("files of the set: %q", names)
	}
	if Files(filepath.Join(dir, "none.p5z")) != nil {
		t.Error("a .p5z with no set has files")
	}
	// a set made by MultiPar or par2 by hand with its volumes named in capitals
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		writeFile(t, filepath.Join(dir, "Juego [PPSA10001].p5z.VOL2+4.PAR2"), []byte("x"))
		if len(Files(p5z)) != 3 {
			t.Errorf("the names of a set in capitals: %q", Files(p5z))
		}
	}
}

func TestInspectMatches(t *testing.T) {
	dir := t.TempDir()
	data := noise(200_000, 7)
	p5z := protected(t, dir, "a.p5z", data, 4096, 0, 1, 2)
	info := Inspect(p5z)
	if info.State != StateMatch {
		t.Fatalf("state %q (%s)", info.State, info.Why)
	}
	// 49 blocks of 4 KiB, 3 recovery blocks: 6 %
	if info.Percent != 6 || info.Recovery != 3 || info.Source != 49 || info.BlockSize != 4096 || info.Capacity != 3*4096 || info.Name != "a.p5z" {
		t.Errorf("info: %+v", info)
	}
	if info.Bytes == 0 || info.Index == "" || len(info.Volumes) != 1 {
		t.Errorf("files: %+v", info)
	}
	// none next to another .p5z
	if got := Inspect(writeFile(t, filepath.Join(dir, "b.p5z"), data)); got.State != StateNone {
		t.Errorf("without a set: %+v", got)
	}
}

func TestInspectOtherFile(t *testing.T) {
	dir := t.TempDir()
	data := noise(100_000, 8)
	// the set of another file, put next to this one with its name
	p5z := writeFile(t, filepath.Join(dir, "a.p5z"), data)
	_, index, recovery := fakeSet("a.p5z", noise(100_000, 9), 4096, 0)
	writeFile(t, p5z+".par2", index)
	writeFile(t, p5z+".vol0+1.par2", recovery)
	if got := Inspect(p5z); got.State != StateOther {
		t.Errorf("a set of another file of the same size: %+v", got)
	}
	// the same file with a byte more: its size is not the one the set protects
	os.WriteFile(p5z, append(noise(100_000, 9), 0), 0o644)
	if got := Inspect(p5z); got.State != StateOther {
		t.Errorf("a set of a file of another size: %+v", got)
	}
	// a set of two files, even if one is this one, is not MaxPack's
	two := t.TempDir()
	p5z = protected(t, two, "a.p5z", data, 4096, 0)
	_, index2, _ := fakeSet("b.p5z", data, 4096, 0)
	os.WriteFile(p5z+".par2", twoFiles(t, index, index2), 0o644)
	os.Remove(p5z + ".vol0+1.par2")
	if got := Inspect(p5z); got.State == StateMatch {
		t.Errorf("a set of two files: %+v", got)
	}
}

// twoFiles is an index whose main packet names two files (the ones of a and b), with their
// descriptions: not a set MaxPack makes.
func twoFiles(t *testing.T, a, b []byte) []byte {
	t.Helper()
	sa, _ := readPacketsFrom(t, a)
	sb, _ := readPacketsFrom(t, b)
	var fileA, fileB []byte
	for _, p := range sa {
		if string(p.kind) == string(typeFileDesc) {
			fileA = p.body
		}
	}
	for _, p := range sb {
		if string(p.kind) == string(typeFileDesc) {
			fileB = p.body
		}
	}
	main := append([]byte{}, sa[0].body[:8]...)
	main = append(main, 2, 0, 0, 0)
	main = append(append(main, fileA[:16]...), fileB[:16]...)
	set := md5ID(main)
	return append(append(packetBytes(set, typeMain, main), packetBytes(set, typeFileDesc, fileA)...), packetBytes(set, typeFileDesc, fileB)...)
}

func readPacketsFrom(t *testing.T, data []byte) ([]packet, error) {
	t.Helper()
	path := writeFile(t, filepath.Join(t.TempDir(), "p.par2"), data)
	return readPackets(path)
}

func TestInspectDamagedStart(t *testing.T) {
	dir := t.TempDir()
	data := noise(300_000, 10)
	p5z := protected(t, dir, "a.p5z", data, 8192, 0, 1, 2, 3)
	// the first 16 KiB are damaged (the header of a .p5z): its own set is still its own, by the
	// checksums of its other blocks, so it can be repaired
	damaged := append([]byte{}, data...)
	for i := 100; i < 200; i++ {
		damaged[i] ^= 0x5A
	}
	os.WriteFile(p5z, damaged, 0o644)
	if got := Inspect(p5z); got.State != StateMatch {
		t.Errorf("a .p5z damaged in its first 16 KiB: %+v", got)
	}
	// every block damaged: nothing says it is the file of the set
	os.WriteFile(p5z, noise(300_000, 11), 0o644)
	if got := Inspect(p5z); got.State != StateOther {
		t.Errorf("a file of the same size and none of its blocks: %+v", got)
	}
}

func TestInspectRefusesNames(t *testing.T) {
	// a set whose file is in a folder, or goes up from it, or is not a .p5z, is not used: repairing
	// writes that name, and the engine checks it as a .p5z
	for _, name := range []string{"sub/a.p5z", `sub\a.p5z`, "../a.p5z", "..", "", "a.bin", ".p5z"} {
		dir := t.TempDir()
		data := noise(20_000, 12)
		p5z := writeFile(t, filepath.Join(dir, "a.p5z"), data)
		_, index, _ := fakeSet(name, data, 4096, 0)
		writeFile(t, p5z+".par2", index)
		if got := Inspect(p5z); got.State == StateMatch {
			t.Errorf("a set of %q is used: %+v", name, got)
		}
	}
}

func TestInspectUnreadable(t *testing.T) {
	dir := t.TempDir()
	p5z := writeFile(t, filepath.Join(dir, "a.p5z"), noise(10_000, 13))
	writeFile(t, p5z+".par2", noise(3000, 14))
	if got := Inspect(p5z); got.State != StateUnreadable {
		t.Errorf("a set that is noise: %+v", got)
	}
}
