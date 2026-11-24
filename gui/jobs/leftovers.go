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
// (<name>.maxpack-tmp, <name>.maxpack-old) is in it while it is there. What a crash, a power cut
// or a USB pulled out leaves is found when MaxPack opens again (Found), and taken away when the
// user says so (Clean).
type Leftovers struct {
	File  string
	mutex sync.Mutex
}

// Leftover is something a queue left apart: where it is and what it takes.
type Leftover struct {
	Path string
	Size uint64
}

// ours tells whether a path is one a queue writes apart: only those are ever taken away.
func ours(path string) bool {
	return strings.HasSuffix(path, TmpSuffix) || strings.HasSuffix(path, OldSuffix)
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
			found = append(found, Leftover{Path: p, Size: SizeOf(p)})
		}
	}
	return found
}

// Clean takes away what is left apart and is there now, and forgets it; it gives the paths that
// could not be taken away, which are kept.
func (l *Leftovers) Clean() []string {
	failed := []string{}
	for _, p := range l.paths() {
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		if err := removeAll(p); err != nil {
			failed = append(failed, p+": "+err.Error())
			continue
		}
		l.Remove(p)
	}
	return failed
}
