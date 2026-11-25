// SPDX-License-Identifier: GPL-3.0-or-later

package par2

// The set of a .p5z is next to it, named after it as par2 names a set made with -n1:
// <name>.p5z.par2, the index, and <name>.p5z.volA+B.par2, its recovery file. It is the set of that
// .p5z only if it protects one file of the .p5z's size and of the MD5 of its first 16 KiB (the
// hash16k of PAR2): any other is the set of another file, and is never used. A .p5z damaged in its
// first 16 KiB (its header) still knows its own set: by the MD5 of other blocks of the file, which
// its IFSC packet has.

import (
	"crypto/md5"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The states of the set next to a .p5z (Info.State).
const (
	StateNone       = "none"       // no set next to it
	StateMatch      = "match"      // the set of this .p5z
	StateOther      = "other"      // the set of another file: never used
	StateUnreadable = "unreadable" // files named as its set, but no set can be read from them
)

// Info is what the set next to a .p5z is: its state and, for one that can be read, the file it
// protects (Name, as par2 writes it again when it repairs), its blocks and how many it can repair
// (Capacity bytes), as a percentage of the file, and what its files take. Why says why it is not
// the .p5z's own.
type Info struct {
	State     string
	Why       string
	Name      string
	Size      uint64
	BlockSize uint64
	Source    uint64
	Recovery  int
	Percent   int
	Capacity  uint64
	Bytes     uint64
	Index     string
	Volumes   []string
}

// caseless tells whether this system's file names usually ignore case (as Windows' and macOS' do).
var caseless = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

func hasPrefix(s, prefix string) bool {
	if caseless {
		return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
	}
	return strings.HasPrefix(s, prefix)
}

func hasSuffix(s, suffix string) bool {
	if caseless {
		return len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
	}
	return strings.HasSuffix(s, suffix)
}

// IndexOf is the name of the index of the set of a .p5z: <name>.p5z.par2.
func IndexOf(p5z string) string { return p5z + ".par2" }

// isVolume tells whether name is a recovery file of the set of base: <base>.vol<digits>+<digits>.par2
// (or with a -, as older par2 names them).
func isVolume(name, base string) bool {
	prefix, suffix := base+".vol", ".par2"
	if !hasPrefix(name, prefix) || !hasSuffix(name, suffix) || len(name) < len(prefix)+len(suffix)+3 {
		return false
	}
	middle := name[len(prefix) : len(name)-len(suffix)]
	sign := strings.IndexAny(middle, "+-")
	if sign <= 0 || sign == len(middle)-1 {
		return false
	}
	for i, r := range middle {
		if i != sign && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// Files are the files of the set of a .p5z that are there: its index and its recovery files, by
// their names; nil when there are none.
func Files(p5z string) []string {
	index, volumes := files(p5z)
	if index == "" {
		return volumes
	}
	return append([]string{index}, volumes...)
}

func files(p5z string) (index string, volumes []string) {
	dir, base := filepath.Dir(p5z), filepath.Base(p5z)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		switch {
		case name == base+".par2" || caseless && strings.EqualFold(name, base+".par2"):
			index = filepath.Join(dir, name)
		case isVolume(name, base):
			volumes = append(volumes, filepath.Join(dir, name))
		}
	}
	return index, volumes
}

// safeName tells whether the name of a file in a set is the plain name of a .p5z: repairing writes
// it in a folder of the window's, so it may not be in another folder or go up from it, and the engine
// checks what was repaired as a .p5z.
func safeName(name string) bool {
	return name != "" && !strings.ContainsAny(name, `/\:`) && !strings.ContainsRune(name, 0) &&
		len(name) > len(".p5z") && strings.EqualFold(name[len(name)-len(".p5z"):], ".p5z")
}

// Inspect tells what the set next to a .p5z is.
func Inspect(p5z string) Info {
	index, volumes := files(p5z)
	info := Info{State: StateNone, Index: index, Volumes: volumes}
	if index == "" && len(volumes) == 0 {
		return info
	}
	for _, path := range append([]string{index}, volumes...) {
		if fi, err := os.Stat(path); err == nil {
			info.Bytes += uint64(fi.Size())
		}
	}
	paths := volumes
	if index != "" {
		paths = append([]string{index}, volumes...)
	}
	set, err := ReadSet(paths...)
	if err != nil {
		info.State, info.Why = StateUnreadable, err.Error()
		return info
	}
	info.BlockSize, info.Source, info.Recovery = set.BlockSize, set.SourceBlocks(), set.Recovery
	info.Capacity = uint64(set.Recovery) * set.BlockSize
	if info.Source > 0 {
		info.Percent = int((uint64(set.Recovery)*100 + info.Source/2) / info.Source)
	}
	if len(set.Files) == 1 {
		info.Name, info.Size = set.Files[0].Name, set.Files[0].Size
	}
	if ok, why := Protects(set, p5z); !ok {
		info.State, info.Why = StateOther, why
		return info
	}
	info.State = StateMatch
	return info
}

// Protects tells whether a set is the one of the file at path, and why not when it is not.
func Protects(set Set, path string) (bool, string) {
	if len(set.Files) != 1 || set.Others != 0 || set.Missing != 0 {
		return false, "the set is not of one file"
	}
	f := set.Files[0]
	if !safeName(f.Name) {
		return false, "the set names its file " + f.Name
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err.Error()
	}
	defer file.Close()
	fi, err := file.Stat()
	if err != nil {
		return false, err.Error()
	}
	if uint64(fi.Size()) != f.Size {
		return false, "the set is of a file of another size"
	}
	first := make([]byte, 16<<10)
	n, err := io.ReadFull(file, first)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err.Error()
	}
	if md5.Sum(first[:n]) == f.Hash16k {
		return true, ""
	}
	if sameBlocks(file, set.BlockSize, f) {
		return true, ""
	}
	return false, "the set is of another file"
}

// sameBlocks tells whether some blocks of a file past its first 16 KiB are the ones the set has the
// MD5 of: the second, the one in the middle and the last. One is enough to know the file; the others
// may be damaged.
func sameBlocks(file *os.File, blockSize uint64, f File) bool {
	count := uint64(len(f.Blocks))
	if count == 0 || blockSize == 0 || blockSize > 1<<30 {
		return false
	}
	block := make([]byte, blockSize)
	for _, n := range []uint64{1, count / 2, count - 1} {
		if n >= count || n*blockSize < 16<<10 {
			continue
		}
		for i := range block {
			block[i] = 0
		}
		// the last block is padded with zeros, as PAR2 hashes it
		if _, err := file.ReadAt(block, int64(n*blockSize)); err != nil && !errors.Is(err, io.EOF) {
			continue
		}
		if md5.Sum(block) == f.Blocks[n].MD5 {
			return true
		}
	}
	return false
}
