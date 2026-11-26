// SPDX-License-Identifier: GPL-3.0-or-later

// Package watch looks at one folder from time to time and says what new arrived in it, once it has stopped
// arriving: the dump folders (sce_sys/param.json), the images of ShadowMountPlus and the PS5 packages at
// its top. It only reads (lists, and the size and date of files): polling works the same on Windows, macOS
// and Linux, on local drives and network shares alike. Before it hands something over it looks at each of its
// files once more through the file itself (Signature): on Windows a listing may say a size and a date that are
// not current while a program writes the file, and a file a program still holds open for writing is still
// arriving. What systems write into the folders they show (.DS_Store, Thumbs.db...) is never part of a dump.
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

// What browsers and downloaders write while they download (Firefox, Chrome and Edge, qBittorrent, µTorrent,
// WinSCP and others): never a game, and a folder with one inside is still arriving.
var downloading = []string{".part", ".crdownload", ".!qb", ".!ut", ".partial", ".download", ".opdownload", ".filepart"}

func isDownloading(name string) bool {
	name = strings.ToLower(name)
	for _, suffix := range downloading {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// What systems write into the folders they show or index, and their recycle bins, as the engine leaves them out
// of a dump (mp_complete.c): never part of one, so never what makes one new or changing.
var junkNames = map[string]bool{".ds_store": true, "thumbs.db": true, "desktop.ini": true, "ehthumbs.db": true,
	"ehthumbs_vista.db": true, ".spotlight-v100": true, ".trashes": true, ".fseventsd": true, ".temporaryitems": true,
	".documentrevisions-v100": true, "$recycle.bin": true, "system volume information": true, "__macosx": true,
	".appledouble": true, ".localized": true, "icon\r": true}

func isJunk(name string) bool {
	low := strings.ToLower(name)
	return junkNames[low] || strings.HasPrefix(name, "._")
}

// unreadableTries is how many looks through its files an item gets while one of them cannot be opened (not
// because a program writes it) before it is skipped, until it changes.
const unreadableTries = 3

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
	inflight map[string]string // handed over this run, not ended: not handed again while it stays so
	skipped  map[string]string // could not be read: not tried again while it stays so
	said     []string          // skipped, not yet told (TakeSkipped)
	seen     map[string]seen
}

type seen struct {
	sig, strong string
	since       time.Time
	tries       int // looks that found a file that could not be opened
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
	if isJunk(entry.Name()) {
		return false
	}
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

// walk goes through a dump folder without what systems write into it (each entry once, with its listing).
func walk(path string, each func(p string, d fs.DirEntry, err error)) {
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if p != path && d != nil && isJunk(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		each(p, d, err)
		return nil
	})
}

// cheap is what an entry is by its listing: for a file its size and date, for a folder how many files and
// folders it has, what they take and the newest date of its files, and whether a download is at work in it.
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
	walk(path, func(p string, d fs.DirEntry, err error) {
		if err != nil {
			count++ // what cannot be read yet counts as there, and changes the signature when it can
			return
		}
		busy = busy || (!d.IsDir() && isDownloading(d.Name()))
		info, err := d.Info()
		if err != nil {
			return
		}
		count++
		if d.IsDir() {
			return // a folder's own date changes when the system writes into it: only its files' dates count
		}
		size += info.Size()
		if m := info.ModTime().UnixNano(); m > newest {
			newest = m
		}
	})
	if busy {
		return "downloading/" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return sigOf(count, size, newest)
}

// stat is fileState (a variable so the tests stand in).
var stat = fileState

// Signature is what the file or folder at path is now, read through each of its files (not from listings), in
// the form of the listing's, what systems write into folders left out: for a file its size and date; for a
// folder how many files and folders, what they take and the newest date. busy is whether one of its files is
// still being written (on Windows: a program holds it open for writing) or downloaded (.part and the like);
// unreadable, whether one of its files could not be opened for another reason (then sig is "?").
func Signature(path string) (sig string, busy, unreadable bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return "?", false, true
	}
	if !info.IsDir() {
		size, mtime, held, err := stat(path)
		if err != nil {
			return "?", false, true
		}
		return strconv.FormatInt(size, 10) + "/" + strconv.FormatInt(mtime, 10), held || isDownloading(info.Name()), false
	}
	var count, size, newest int64
	walk(path, func(p string, d fs.DirEntry, err error) {
		if err != nil {
			unreadable = true
			return
		}
		count++
		if d.IsDir() {
			return // as in cheap: only the files' dates count
		}
		s, mtime, held, err := stat(p)
		if err != nil {
			unreadable = true
			return
		}
		busy = busy || held || isDownloading(d.Name())
		size += s
		if mtime > newest {
			newest = mtime
		}
	})
	if unreadable {
		return "?", busy, true
	}
	return sigOf(count, size, newest), busy, false
}

// ErrGone is the error of Poll when the folder is not there (or is not a folder).
var ErrGone = errors.New("the watched folder is not there")

func (w *Watcher) init() {
	if w.Done == nil {
		w.Done = map[string]string{}
	}
	if w.Existing == nil {
		w.Existing = map[string]string{}
	}
	if w.seen == nil {
		w.seen, w.inflight, w.skipped = map[string]seen{}, map[string]string{}, map[string]string{}
	}
}

// Poll looks at the folder once. It gives what is new (not ended nor handed over this run as it is now, and not
// there when the watcher was turned on), all of it at once, when none of it has changed for Quiet and a look
// through its files finds none still being written: a dump and its DLC that arrive together go together. One
// whose files cannot be opened is skipped after a few looks (TakeSkipped), and holds back nothing else. What goes
// away is forgotten.
func (w *Watcher) Poll() ([]string, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.init()
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
		sig := cheap(path, entry)
		if s, ok := w.seen[k]; !ok || s.sig != sig {
			w.seen[k] = seen{sig: sig, since: now}
		}
		if w.Snapshot {
			w.Existing[k] = sig
			continue
		}
		if old, ok := w.Existing[k]; ok && old != sig {
			delete(w.Existing, k) // it changed since it was turned on: new now
		}
		if w.Done[k] == sig || w.Existing[k] == sig || w.inflight[k] == sig || w.skipped[k] == sig {
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
	for _, m := range []map[string]string{w.Done, w.Existing, w.inflight, w.skipped} {
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
	// minute more); one that cannot be opened is looked at again, then skipped; the others go
	var whole []string
	for _, path := range pending {
		k := key(path)
		s := w.seen[k]
		strong, busy, unreadable := Signature(path)
		switch {
		case unreadable && !busy:
			if s.tries++; s.tries >= unreadableTries {
				w.skipped[k] = s.sig
				w.said = append(w.said, path)
			}
			w.seen[k] = s
		case busy || (strong != s.sig && strong != s.strong):
			w.seen[k] = seen{sig: s.sig, strong: strong, since: now}
		default:
			w.inflight[k] = s.sig
			whole = append(whole, path)
		}
	}
	return whole, nil
}

// Ended says how what was handed over ended: final (compressed, failed, or in the destination already) makes it
// done until it changes; else (it never ran, or it was cancelled) it is handed again in the next run.
func (w *Watcher) Ended(path string, final bool) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.init()
	k := key(path)
	sig, ok := w.inflight[k]
	if !ok || !final {
		return
	}
	w.Done[k] = sig
	delete(w.inflight, k)
}

// TakeExisting makes what was there when the watcher was turned on, and has not changed since, new (the user
// asked to compress it): the next Polls hand it over as they hand over what arrives, once it has not changed for
// Quiet and none of its files is being written. It gives how many.
func (w *Watcher) TakeExisting() int {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.init()
	n := len(w.Existing)
	w.Existing = map[string]string{}
	return n
}

// TakeSkipped gives what was skipped since the last call, as its files could not be opened.
func (w *Watcher) TakeSkipped() []string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	said := w.said
	w.said = nil
	return said
}

// Restart forgets what was handed over this run and has not ended (the page that took it loaded again): it is
// handed again.
func (w *Watcher) Restart() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.inflight = map[string]string{}
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

// ExistingCount counts what was there when the watcher was turned on, has not changed and has not been taken.
func (w *Watcher) ExistingCount() int {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return len(w.Existing)
}
