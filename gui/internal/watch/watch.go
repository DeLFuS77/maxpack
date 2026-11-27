// SPDX-License-Identifier: GPL-3.0-or-later

// Package watch looks at one folder from time to time and says what new arrived in it, once it has stopped
// arriving: the dump folders (sce_sys/param.json), the images of ShadowMountPlus and the PS5 packages at
// its top. It only reads (lists, and the size and date of files): polling works the same on Windows, macOS
// and Linux, on local drives and network shares alike. Before it hands something over it looks at each of its
// files once more through the file itself (Signature): on Windows a listing may say a size and a date that are
// not current while a program writes the file, and a file a program still holds open for writing is still
// arriving (on Windows the open says so; on Linux /proc does, for the user's own programs; macOS has no such
// look). What systems write into the folders they show (.DS_Store, Thumbs.db...) is never part of a dump.
package watch

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The extensions of the images and packages a folder can get (jobs.TakenExts: the images, .pkg, and .zstd, an
// exFAT image with a game packed already in it).
var exts = []string{".exfat", ".ffpkg", ".ffpfs", ".ffpfsc", ".pkg", ".zstd"}

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
	// when the watcher was turned on, by path and signature: not new while it stays so. Made is, for what ended
	// well, what it was and the .p5z made of it (see Made). The window keeps them between runs.
	Done, Existing map[string]string
	Made           map[string]Made

	mutex    sync.Mutex
	inflight map[string]string // handed over this run, not ended: not handed again while it stays so
	skipped  map[string]string // could not be read: not tried again while it stays so
	said     []string          // skipped, not yet told (TakeSkipped)
	seen     map[string]seen
	strongAt map[string]string // what each in flight was, through its files, when it was handed over
	pathOf   map[string]string // the path of each key seen
	missing  map[string]int    // listings in a row that did not have it
}

// Made is what the watched folder made of an item that ended well: the item as it was then (its signature: how
// many files, what they take, the newest date; its game's title ID and version) and the .p5z, with its size and
// date when it was made. Redos is how many times in a row it was made again so (a copy that stalled and went
// on). Once the item changes, a .p5z still exactly so in the destination is the one made of it.
type Made struct {
	Sig     string `json:"sig"`
	Out     string `json:"out"`
	Size    int64  `json:"size"`
	Mtime   int64  `json:"mtime"`
	TitleID string `json:"title_id"`
	Version string `json:"version"`
	Redos   int    `json:"redos"`
}

// Ending is how an item handed over ended (see EndedAs): Final makes it done until it changes; for one compressed
// well, Out is the .p5z made of it, TitleID and Version its game's, and Redone that it was made again in place of
// the one made before.
type Ending struct {
	Final                 bool
	Out, TitleID, Version string
	Redone                bool
}

// Matches tells whether the file at Out is still the one made: the same size and date.
func (m Made) Matches() bool {
	info, err := os.Stat(m.Out)
	return err == nil && info.Mode().IsRegular() && info.Size() == m.Size && info.ModTime().UnixNano() == m.Mtime
}

type seen struct {
	sig, strong string
	since       time.Time
	tries       int       // looks that found a file that could not be opened
	writing     time.Time // since when a program holds a file of it open for writing (zero: none)
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

// stat is fileState, and held heldOpen (variables so the tests stand in).
var (
	stat = fileState
	held = heldOpen
)

// look is what the file or folder at path is now through each of its files (see Signature), without the look at
// the programs that hold it (held): open, a file a program holds open for writing (Windows says so when it is
// opened); downloading, a download at work in it.
func look(path string) (sig string, open, downloading, unreadable bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return "?", false, false, true
	}
	if !info.IsDir() {
		size, mtime, held, err := stat(path)
		if err != nil {
			return "?", false, false, true
		}
		return strconv.FormatInt(size, 10) + "/" + strconv.FormatInt(mtime, 10), held, isDownloading(info.Name()), false
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
		open = open || held
		downloading = downloading || isDownloading(d.Name())
		size += s
		if mtime > newest {
			newest = mtime
		}
	})
	if unreadable {
		return "?", open, downloading, true
	}
	return sigOf(count, size, newest), open, downloading, false
}

// Signature is what the file or folder at path is now, read through each of its files (not from listings), in
// the form of the listing's, what systems write into folders left out: for a file its size and date; for a
// folder how many files and folders, what they take and the newest date. busy is whether one of its files is
// still being written (a program holds it open for writing: on Windows, and on Linux for the user's own
// programs) or downloaded (.part and the like); unreadable, whether one of its files could not be opened for
// another reason (then sig is "?").
func Signature(path string) (sig string, busy, unreadable bool) {
	sig, open, downloading, unreadable := look(path)
	return sig, open || downloading || (!unreadable && held([]string{path})[path]), unreadable
}

// ErrGone is the error of Poll when the folder is not there (or is not a folder).
var ErrGone = errors.New("the watched folder is not there")

// list is how the folder is listed (a variable so the tests can stand in for a share that is slow or drops).
var list = os.ReadDir

// forgetAfter is how many listings in a row something known must be missing from before it is forgotten: a
// listing of a share that drops may miss entries once.
const forgetAfter = 3

func (w *Watcher) init() {
	if w.Done == nil {
		w.Done = map[string]string{}
	}
	if w.Existing == nil {
		w.Existing = map[string]string{}
	}
	if w.Made == nil {
		w.Made = map[string]Made{}
	}
	if w.seen == nil {
		w.seen, w.inflight, w.skipped, w.missing = map[string]seen{}, map[string]string{}, map[string]string{}, map[string]int{}
		w.pathOf, w.strongAt = map[string]string{}, map[string]string{}
	}
}

// Poll looks at the folder once. It gives what is new (not ended nor handed over this run as it is now, and not
// there when the watcher was turned on), all of it at once, when none of it has changed for Quiet and a look
// through its files finds none still being written: a dump and its DLC that arrive together go together. One
// whose files cannot be opened is skipped after a few looks (TakeSkipped), and holds back nothing else. What is
// missing from forgetAfter listings in a row is forgotten; a listing that fails forgets nothing, nor does one
// that has none of what is known (an empty one of a share that went), nor an entry that is listed but cannot be
// checked. The listing and the look through the files are made without holding the watcher: what the page says
// meanwhile (Ended, TakeExisting...) does not wait for them. The programs that write into what is pending are
// looked for once for all of it (heldOpen).
func (w *Watcher) Poll() ([]string, error) {
	if info, err := os.Stat(w.Folder); err != nil || !info.IsDir() {
		return nil, ErrGone
	}
	entries, err := list(w.Folder)
	if err != nil {
		return nil, err
	}
	type entry struct{ path, sig string }
	var listed []entry
	here := map[string]bool{}
	for _, e := range entries {
		path := filepath.Join(w.Folder, e.Name())
		here[key(path)] = true // listed: there, whatever its check says
		if candidate(path, e) {
			listed = append(listed, entry{path, cheap(path, e)})
		}
	}
	w.mutex.Lock()
	w.init()
	now := w.now()
	var pending []string
	quiet := true
	for _, e := range listed {
		path, sig := e.path, e.sig
		k := key(path)
		w.pathOf[k] = path
		if s, ok := w.seen[k]; !ok || s.sig != sig {
			w.seen[k] = seen{sig: sig, since: now, writing: s.writing}
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
	w.forgetMissing(here)
	w.mutex.Unlock()
	if !quiet || len(pending) == 0 {
		return nil, nil
	}
	// the last look, through the files: one still being written, or whose listing was not current, waits (a
	// minute more); one that cannot be opened is looked at again, then skipped; the others go
	type through struct {
		strong                              string
		open, downloading, unreadable, held bool
	}
	looks := make([]through, len(pending))
	for i, path := range pending {
		looks[i].strong, looks[i].open, looks[i].downloading, looks[i].unreadable = look(path)
	}
	writers := held(pending)
	w.mutex.Lock()
	defer w.mutex.Unlock()
	var whole []string
	for i, path := range pending {
		k := key(path)
		s, ok := w.seen[k]
		if !ok {
			continue
		}
		l := looks[i]
		writing := l.open || writers[path]
		busy := writing || l.downloading
		switch {
		case l.unreadable && !busy:
			if s.tries++; s.tries >= unreadableTries {
				w.skipped[k] = s.sig
				w.said = append(w.said, path)
			}
			s.writing = time.Time{}
			w.seen[k] = s
		case busy || (l.strong != s.sig && l.strong != s.strong):
			next := seen{sig: s.sig, strong: l.strong, since: now}
			if writing {
				next.writing = s.writing
				if next.writing.IsZero() {
					next.writing = now
				}
			}
			w.seen[k] = next
		default:
			s.writing = time.Time{}
			w.seen[k] = s
			w.inflight[k] = s.sig
			w.strongAt[k] = l.strong
			whole = append(whole, path)
		}
	}
	return whole, nil
}

// Writing gives what has waited for at least least because a program holds a file of it open for writing: a copy
// at work, or a program that keeps a file open (the page says it).
func (w *Watcher) Writing(least time.Duration) []string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	now := w.now()
	var paths []string
	for _, s := range w.seenPaths() {
		if v := w.seen[key(s)]; !v.writing.IsZero() && now.Sub(v.writing) >= least {
			paths = append(paths, s)
		}
	}
	sort.Strings(paths)
	return paths
}

// seenPaths are the paths of what was seen, with the watcher held (seen is by key: the path is the folder and the
// key's name as the listing gave it).
func (w *Watcher) seenPaths() []string {
	var paths []string
	for k := range w.seen {
		paths = append(paths, w.pathOf[k])
	}
	return paths
}

// forgetMissing forgets what was not in forgetAfter listings in a row (here is what this one had), with the
// watcher held. An empty listing, or one with none of several things known, counts for nothing.
func (w *Watcher) forgetMissing(here map[string]bool) {
	known := map[string]bool{}
	for _, m := range []map[string]string{w.Done, w.Existing, w.inflight, w.skipped} {
		for k := range m {
			known[k] = true
		}
	}
	for k := range w.seen {
		known[k] = true
	}
	for k := range w.Made {
		known[k] = true
	}
	for k := range w.missing {
		if !known[k] {
			delete(w.missing, k)
		}
	}
	// an empty listing (a share that went, its mount point left), or one with none of several things known (another
	// share in its place) tells nothing of them
	listed := 0
	for k := range known {
		if here[k] {
			listed++
		}
	}
	if len(here) == 0 || (len(known) >= 3 && listed == 0) {
		return
	}
	for k := range known {
		if here[k] {
			delete(w.missing, k)
			continue
		}
		if w.missing[k]++; w.missing[k] < forgetAfter {
			continue
		}
		for _, m := range []map[string]string{w.Done, w.Existing, w.inflight, w.skipped} {
			delete(m, k)
		}
		delete(w.seen, k)
		delete(w.strongAt, k)
		delete(w.pathOf, k)
		delete(w.Made, k)
		delete(w.missing, k)
	}
}

// Ended says how what was handed over ended: final (compressed, failed, or in the destination already) makes it
// done until it changes; else (it never ran, or it was cancelled) it is handed again in the next run.
func (w *Watcher) Ended(path string, final bool) { w.EndedAs(path, Ending{Final: final}) }

// EndedAs is Ended, and for one compressed well what was made of it is kept with it (Made), as it is now: a
// redo counts one more, a fresh one 0. An end with nothing made leaves what was made of it before as it was (that
// .p5z is still the one made of it).
func (w *Watcher) EndedAs(path string, end Ending) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.init()
	k := key(path)
	sig, ok := w.inflight[k]
	if !ok || !end.Final {
		return
	}
	w.Done[k] = sig
	delete(w.inflight, k)
	delete(w.strongAt, k)
	if end.Out == "" {
		return
	}
	if info, err := os.Stat(end.Out); err == nil && info.Mode().IsRegular() {
		redos := 0
		if old, had := w.Made[k]; had && end.Redone {
			redos = old.Redos + 1
		}
		w.Made[k] = Made{Sig: sig, Out: end.Out, Size: info.Size(), Mtime: info.ModTime().UnixNano(), TitleID: end.TitleID,
			Version: end.Version, Redos: redos}
	}
}

// HandedSig is what the item at path was, through its files (as Signature says it), when it was handed over this
// run; "" when it is not in flight.
func (w *Watcher) HandedSig(path string) string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	k := key(path)
	if _, ok := w.inflight[k]; !ok {
		return ""
	}
	if strong := w.strongAt[k]; strong != "" {
		return strong
	}
	return w.inflight[k]
}

// MadeOf is what was made of the item at path when it last ended well, if anything.
func (w *Watcher) MadeOf(path string) (Made, bool) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	made, ok := w.Made[key(path)]
	return made, ok
}

// Own says that the window itself put something at path (what a queue made): the entry at the top of the folder
// that holds it is done as it is now, never new. Outside the folder, nothing.
func (w *Watcher) Own(path string) {
	rel, err := filepath.Rel(w.Folder, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return
	}
	top := filepath.Join(w.Folder, strings.Split(rel, string(filepath.Separator))[0])
	info, err := os.Lstat(top)
	if err != nil {
		return
	}
	entry := fs.FileInfoToDirEntry(info)
	if !candidate(top, entry) {
		return
	}
	sig := cheap(top, entry)
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.init()
	k := key(top)
	w.Done[k] = sig
	delete(w.Existing, k)
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
	w.inflight, w.strongAt = map[string]string{}, map[string]string{}
}

// KeptMade is what was made of what ended well (Made), which the window keeps between runs too.
func (w *Watcher) KeptMade() map[string]Made {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	made := map[string]Made{}
	for k, v := range w.Made {
		made[k] = v
	}
	return made
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
