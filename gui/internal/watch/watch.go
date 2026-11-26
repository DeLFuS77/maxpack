// SPDX-License-Identifier: GPL-3.0-or-later

// Package watch looks at one folder from time to time and says what new arrived in it, once it has stopped
// arriving: the dump folders (sce_sys/param.json), the images of ShadowMountPlus and the PS5 packages at
// its top. It only reads (lists, and the size and date of files): polling works the same on Windows, macOS
// and Linux, on local drives and network shares alike. Before it hands something over it looks at each of its
// files once more through the file itself (Signature): on Windows a listing may say a size and a date that are
// not current while a program writes the file, and a file a program still holds open for writing is still
// arriving.
package watch

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The extensions of the images and packages a folder can get (jobs.ImageExts and .pkg).
var exts = []string{".exfat", ".ffpkg", ".ffpfs", ".ffpfsc", ".pkg"}

// What MaxPack writes apart while it works (jobs.TmpSuffix, jobs.OldSuffix) is never new.
var apart = []string{".maxpack-tmp", ".maxpack-old"}

// What browsers and downloaders write while they download (Firefox, Chrome and Edge, qBittorrent, µTorrent and
// others): never a game, and a folder with one inside is still arriving.
var downloading = []string{".part", ".crdownload", ".!qb", ".!ut", ".partial", ".download", ".opdownload"}

func isDownloading(name string) bool {
	name = strings.ToLower(name)
	for _, suffix := range downloading {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// Watcher is a folder watched. Poll is called every few seconds; the rest may be called meanwhile from other
// goroutines.
type Watcher struct {
	Folder string
	// Quiet is how long nothing new may change before what is new is handed over (a minute)
	Quiet time.Duration
	// Now is the clock (time.Now when nil)
	Now func() time.Time
	// Snapshot: the first Poll takes what is there as not new (Existing), as when the watcher is turned on
	Snapshot bool
	// Done is what ended (compressed, or failed, or in the destination already), by path, with what it was when
	// it was handed over (its signature): it is handed again only once that changes. Existing is what was there
	// when the watcher was turned on, by path and signature: not new while it stays so. The window keeps both
	// between runs.
	Done, Existing map[string]string

	mutex    sync.Mutex
	paths    map[string]string // the path of each key, for TakeExisting
	inflight map[string]string // handed over this run, not ended: not handed again while it stays so
	seen     map[string]seen
}

type seen struct {
	sig, strong string
	since       time.Time
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

// candidate tells whether an entry at the top of the folder can be new: a dump folder, an image or a package
// (not one of 0 bytes, nor one a download is still writing next to it).
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
	if !entry.Type().IsRegular() || isDownloading(name) {
		return false
	}
	ok := false
	for _, ext := range exts {
		ok = ok || filepath.Ext(name) == ext
	}
	if !ok {
		return false
	}
	if info, err := entry.Info(); err != nil || info.Size() == 0 {
		return false
	}
	for _, suffix := range downloading {
		if _, err := os.Lstat(path + suffix); err == nil {
			return false
		}
	}
	return true
}

func sigOf(count, size, newest int64) string {
	return strconv.FormatInt(count, 10) + "/" + strconv.FormatInt(size, 10) + "/" + strconv.FormatInt(newest, 10)
}

// cheap is what an entry is by its listing: for a file its size and date, for a folder how many files and
// folders it has, what they take and the newest date among them, and whether a download is at work in it.
func cheap(path string, entry fs.DirEntry) string {
	if !entry.IsDir() {
		info, err := entry.Info()
		if err != nil {
			return "?"
		}
		return strconv.FormatInt(info.Size(), 10) + "/" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	}
	var count, size, newest int64
	busy := false
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			count++ // what cannot be read yet counts as there, and changes the signature when it can
			return nil
		}
		busy = busy || (!d.IsDir() && isDownloading(d.Name()))
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
	if busy {
		return "downloading/" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return sigOf(count, size, newest)
}

// Signature is what the file or folder at path is now, read through each of its files (not from listings), in
// the form of the listing's: for a file its size and date; for a folder how many files and folders, what they
// take and the newest date. busy is whether one of its files is still being written (on Windows: a program
// holds it open for writing) or downloaded (.part and the like). Two signatures that differ are two contents.
func Signature(path string) (sig string, busy bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return "?", false
	}
	if !info.IsDir() {
		size, mtime, held, err := fileState(path)
		if err != nil {
			return "?", false
		}
		return strconv.FormatInt(size, 10) + "/" + strconv.FormatInt(mtime, 10), held || isDownloading(info.Name())
	}
	var count, size, newest int64
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			count++
			return nil
		}
		count++
		var mtime int64
		if d.IsDir() {
			if info, err := d.Info(); err == nil {
				mtime = info.ModTime().UnixNano()
			}
		} else {
			s, m, held, err := fileState(p)
			if err != nil {
				busy = true // a file that cannot be opened now: try again later
				return nil
			}
			busy = busy || held || isDownloading(d.Name())
			size += s
			mtime = m
		}
		if mtime > newest {
			newest = mtime
		}
		return nil
	})
	return sigOf(count, size, newest), busy
}

// ErrGone is the error of Poll when the folder is not there (or is not a folder).
var ErrGone = errors.New("the watched folder is not there")

// Poll looks at the folder once. It gives what is new (not ended nor handed over this run as it is now, and not
// there when the watcher was turned on), all of it at once, when none of it has changed for Quiet and a look
// through its files finds none still being written: a dump and its DLC that arrive together go together. What
// goes away is forgotten.
func (w *Watcher) Poll() ([]string, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.Done == nil {
		w.Done = map[string]string{}
	}
	if w.Existing == nil {
		w.Existing = map[string]string{}
	}
	if w.seen == nil {
		w.seen, w.inflight, w.paths = map[string]seen{}, map[string]string{}, map[string]string{}
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
		w.paths[k] = path
		sig := cheap(path, entry)
		if s, ok := w.seen[k]; !ok || s.sig != sig {
			w.seen[k] = seen{sig: sig, since: now}
		}
		if w.Snapshot {
			w.Existing[k] = sig
			continue
		}
		if w.Done[k] == sig || w.Existing[k] == sig || w.inflight[k] == sig {
			continue
		}
		if strings.HasPrefix(sig, "downloading/") {
			continue // a download at work inside it: not whole, and it holds back nothing else
		}
		pending = append(pending, path)
		if now.Sub(w.seen[k].since) < w.Quiet {
			quiet = false
		}
	}
	w.Snapshot = false
	for _, m := range []map[string]string{w.Done, w.Existing, w.inflight, w.paths} {
		for k := range m {
			if !here[k] {
				delete(m, k)
			}
		}
	}
	for k := range w.seen {
		if !here[k] {
			delete(w.seen, k)
		}
	}
	if !quiet || len(pending) == 0 {
		return nil, nil
	}
	// the last look, through the files: one still being written, or whose listing was not current, waits (a
	// minute more); the others go
	var whole []string
	for _, path := range pending {
		k := key(path)
		s := w.seen[k]
		strong, busy := Signature(path)
		if busy || (strong != s.sig && strong != s.strong) {
			w.seen[k] = seen{sig: s.sig, strong: strong, since: now}
			continue
		}
		w.inflight[k] = s.sig
		whole = append(whole, path)
	}
	return whole, nil
}

// Ended says how what was handed over ended: final (compressed, failed, or in the destination already) makes it
// done until it changes; else (it never ran, or it was cancelled) it is handed again in the next run.
func (w *Watcher) Ended(path string, final bool) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	k := key(path)
	sig, ok := w.inflight[k]
	if !ok || !final {
		return
	}
	if w.Done == nil {
		w.Done = map[string]string{}
	}
	w.Done[k] = sig
	delete(w.inflight, k)
}

// TakeExisting gives what was there when the watcher was turned on and has not changed since (the user asked to
// compress it): it is handed over now, and is no longer "existing".
func (w *Watcher) TakeExisting() []string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.inflight == nil {
		w.seen, w.inflight, w.paths = map[string]seen{}, map[string]string{}, map[string]string{}
	}
	var taken []string
	for k, sig := range w.Existing {
		path := w.paths[k]
		if path == "" {
			continue
		}
		taken = append(taken, path)
		w.inflight[k] = sig
	}
	w.Existing = map[string]string{}
	return taken
}

// Kept is what the window keeps of the watcher between runs: what ended and what was there when it was turned on.
func (w *Watcher) Kept() (done, existing map[string]string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	done, existing = map[string]string{}, map[string]string{}
	for k, v := range w.Done {
		done[k] = v
	}
	for k, v := range w.Existing {
		existing[k] = v
	}
	return done, existing
}

// Existing counts what was there when the watcher was turned on and has not been taken.
func (w *Watcher) ExistingCount() int {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return len(w.Existing)
}
