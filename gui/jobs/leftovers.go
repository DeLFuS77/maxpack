// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Leftovers is the Journal of the window, in a file of its folder: what a queue writes apart
// (<name>.maxpack-tmp, <name>.maxpack-old, the folder where the repair data of a .p5z is made,
// <name>.p5z.maxpack-par2, and the one where a .p5z is repaired, <name>.p5z.maxpack-repair) is in it
// while it is there. What a crash, a power cut or a USB pulled out leaves is found when MaxPack
// opens again (Found), and taken away, or put back, when the user says so (Clean).
type Leftovers struct {
	File  string
	mutex sync.Mutex
}

// Leftover is something a queue left apart: where it is and what it takes. The folder of a repair
// cut short may have the .p5z in it: Restore is then the .p5z that Clean puts back (the one that
// verified, else the damaged one as it was); Keep, that the .p5z is in its place and the folder has
// a copy of it too, so neither is touched and the user is told.
type Leftover struct {
	Path    string
	Size    uint64
	Restore string
	Keep    bool
}

// ours tells whether a path is one a queue writes apart: only those are ever taken away.
func ours(path string) bool {
	for _, suffix := range []string{TmpSuffix, OldSuffix, Par2Suffix, RepairSuffix} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

// repairFate is what becomes of the folder of a repair cut short (see repair.go): the file in it that
// goes back to the .p5z's place when the .p5z is not there ("" when none does), or keep when the
// .p5z is there and the folder may still have a copy of it that is not the damaged one left once
// the repaired one took its place. Else the folder goes.
func repairFate(work string) (from string, keep bool) {
	file := strings.TrimSuffix(work, RepairSuffix)
	_, err := os.Lstat(file)
	there := err == nil
	step, known := readStep(work)
	if !known {
		entries, _ := os.ReadDir(work)
		for _, entry := range entries {
			if entry.Name() != repairState && entry.Name() != repairState+".tmp" {
				return "", true
			}
		}
		return "", false
	}
	inside := filepath.Join(work, step.Name)
	repaired, original := inside, inside+".1"
	_, errRepaired := os.Lstat(repaired)
	_, errOriginal := os.Lstat(original)
	if there {
		if step.Step == stepPlaced || errRepaired != nil && errOriginal != nil || step.Step == stepMoving && errRepaired != nil {
			return "", false
		}
		return "", true
	}
	switch {
	case step.Step == stepVerified && errRepaired == nil:
		return repaired, false
	case errOriginal == nil:
		return original, false
	case errRepaired == nil:
		// par2 had not begun: the one in the folder is the .p5z as it was
		return repaired, false
	}
	return "", false
}

// read gives the paths of the file; none when there is none or it cannot be read.
func (l *Leftovers) read() []string {
	data, err := os.ReadFile(l.File)
	if err != nil {
		return nil
	}
	var paths []string
	if json.Unmarshal(data, &paths) != nil {
		return nil
	}
	return paths
}

// write keeps the paths whole or not at all: a temporary file, then renamed over the old one.
func (l *Leftovers) write(paths []string) {
	if len(paths) == 0 {
		os.Remove(l.File)
		return
	}
	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(l.File), 0o755) != nil {
		return
	}
	temp, err := os.CreateTemp(filepath.Dir(l.File), filepath.Base(l.File)+".*.tmp")
	if err != nil {
		return
	}
	_, err = temp.Write(data)
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp.Name(), l.File)
	}
	if err != nil {
		os.Remove(temp.Name())
	}
}

// change gives the paths of the file to edit, and writes what it gives back.
func (l *Leftovers) change(edit func(paths []string) []string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.write(edit(l.read()))
}

// Add keeps a path written apart (once).
func (l *Leftovers) Add(path string) {
	l.change(func(paths []string) []string {
		for _, p := range paths {
			if p == path {
				return paths
			}
		}
		return append(paths, path)
	})
}

// Remove forgets a path.
func (l *Leftovers) Remove(path string) {
	l.change(func(paths []string) []string {
		kept := paths[:0]
		for _, p := range paths {
			if p != path {
				kept = append(kept, p)
			}
		}
		return kept
	})
}

// paths are the paths kept, once the ones that are gone are forgotten: a path that is not ours,
// or that is not there while its folder is (a drive unplugged keeps them for another time).
func (l *Leftovers) paths() []string {
	var kept []string
	l.change(func(paths []string) []string {
		kept = kept[:0]
		for _, p := range paths {
			if !ours(p) {
				continue
			}
			if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
				if _, err := os.Stat(filepath.Dir(p)); err == nil {
					continue
				}
			}
			kept = append(kept, p)
		}
		return kept
	})
	return kept
}

// Found gives what is left apart and is there now, with what it takes.
func (l *Leftovers) Found() []Leftover {
	found := []Leftover{}
	for _, p := range l.paths() {
		if _, err := os.Lstat(p); err == nil {
			left := Leftover{Path: p, Size: SizeOf(p)}
			if strings.HasSuffix(p, RepairSuffix) {
				from, keep := repairFate(p)
				left.Keep = keep
				if from != "" {
					left.Restore = strings.TrimSuffix(p, RepairSuffix)
				}
			}
			found = append(found, left)
		}
	}
	return found
}

// Clean takes away what is left apart and is there now, and forgets it: the folder of a repair cut
// short puts its .p5z back first, and one that may have a copy of a .p5z in its place stays. It
// gives the paths that could not be taken away (or put back), which are kept, with why.
func (l *Leftovers) Clean() []string {
	failed := []string{}
	for _, p := range l.paths() {
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		if strings.HasSuffix(p, RepairSuffix) {
			from, keep := repairFate(p)
			if keep {
				failed = append(failed, p+": kept: "+strings.TrimSuffix(p, RepairSuffix)+" is there, and this folder has a copy of it")
				continue
			}
			if from != "" {
				if err := renameNew(from, strings.TrimSuffix(p, RepairSuffix)); err != nil {
					failed = append(failed, p+": "+err.Error())
					continue
				}
			}
		}
		if err := removeAll(p); err != nil {
			failed = append(failed, p+": "+err.Error())
			continue
		}
		l.Remove(p)
	}
	return failed
}
