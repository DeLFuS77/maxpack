// SPDX-License-Identifier: GPL-3.0-or-later

// Package watch looks at one folder from time to time and says what new arrived in it, once it has stopped
// arriving: the dump folders (sce_sys/param.json), the images of ShadowMountPlus and the PS5 packages at
// its top. It only reads (lists and the size and date of files): polling works the same on Windows, macOS
// and Linux, on local drives and network shares alike.
package watch

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// The extensions of the images and packages a folder can get (jobs.ImageExts and .pkg).
var exts = []string{".exfat", ".ffpkg", ".ffpfs", ".ffpfsc", ".pkg"}

// What MaxPack writes apart while it works (jobs.TmpSuffix, jobs.OldSuffix) is never new.
var apart = []string{".maxpack-tmp", ".maxpack-old"}

// Watcher is a folder watched. Poll is called every few seconds.
type Watcher struct {
	Folder string
	// Quiet is how long nothing new may change before what is new is handed over (a minute)
	Quiet time.Duration
	// Now is the clock (time.Now when nil)
	Now func() time.Time
	// Handed is what was handed over, by path, with what it was then (its signature): it is handed again
	// only once that changes. The window keeps it between runs, so a game that failed is not tried again
	// until it changes.
	Handed map[string]string
	seen   map[string]seen
}

type seen struct {
	sig   string
	since time.Time
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// key makes paths comparable: on Windows and macOS, case does not tell two files apart.
func key(path string) string {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(path)
	}
	return path
}

// candidate tells whether an entry at the top of the folder can be new: a dump folder, an image or a package.
func candidate(path string, entry fs.DirEntry) bool {
	name := strings.ToLower(entry.Name())
	for _, suffix := range apart {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	if entry.IsDir() {
		info, err := os.Stat(filepath.Join(path, "sce_sys", "param.json"))
		return err == nil && !info.IsDir()
	}
	if !entry.Type().IsRegular() {
		return false
	}
	for _, ext := range exts {
		if filepath.Ext(name) == ext {
			return true
		}
	}
	return false
}

// signature is what an entry is now: for a file its size and date, for a folder how many files and folders it
// has, what they take and the newest date among them. Anything that changes it is still arriving.
func signature(path string, entry fs.DirEntry) string {
	if !entry.IsDir() {
		info, err := entry.Info()
		if err != nil {
			return "?"
		}
		return strconv.FormatInt(info.Size(), 10) + "/" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	}
	var count, size, newest int64
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			count++ // what cannot be read yet counts as there, and changes the signature when it can
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		count++
		if !d.IsDir() {
			size += info.Size()
		}
		if m := info.ModTime().UnixNano(); m > newest {
			newest = m
		}
		return nil
	})
	return strconv.FormatInt(count, 10) + "/" + strconv.FormatInt(size, 10) + "/" + strconv.FormatInt(newest, 10)
}

// ErrGone is the error of Poll when the folder is not there (or is not a folder).
var ErrGone = errors.New("the watched folder is not there")

// Poll looks at the folder once. It gives what is new (not handed over yet, or changed since), all of it at
// once, when none of it has changed for Quiet: a dump and its DLC that arrive together go together. What
// goes away is forgotten.
func (w *Watcher) Poll() ([]string, error) {
	if w.Handed == nil {
		w.Handed = map[string]string{}
	}
	if w.seen == nil {
		w.seen = map[string]seen{}
	}
	if info, err := os.Stat(w.Folder); err != nil || !info.IsDir() {
		return nil, ErrGone
	}
	entries, err := os.ReadDir(w.Folder)
	if err != nil {
		return nil, err
	}
	now, here := w.now(), map[string]bool{}
	var pending []string
	quiet := true
	for _, entry := range entries {
		path := filepath.Join(w.Folder, entry.Name())
		if !candidate(path, entry) {
			continue
		}
		k := key(path)
		here[k] = true
		sig := signature(path, entry)
		if s, ok := w.seen[k]; !ok || s.sig != sig {
			w.seen[k] = seen{sig, now}
		}
		if w.Handed[k] == sig {
			continue
		}
		pending = append(pending, path)
		if now.Sub(w.seen[k].since) < w.Quiet {
			quiet = false
		}
	}
	for k := range w.seen {
		if !here[k] {
			delete(w.seen, k)
		}
	}
	for k := range w.Handed {
		if !here[k] {
			delete(w.Handed, k)
		}
	}
	if !quiet || len(pending) == 0 {
		return nil, nil
	}
	for _, path := range pending {
		w.Handed[key(path)] = w.seen[key(path)].sig
	}
	return pending, nil
}
