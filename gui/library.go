// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// LibraryItem is a .p5z of the Console tab's list.
type LibraryItem struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	TitleID     string `json:"titleId"`
	Version     string `json:"version"`
	Size        int64  `json:"size"`        // the file's size on disk; its cover is asked apart (LibraryIcon)
	Interrupted bool   `json:"interrupted"` // interrupted_append: shown as "needs repair", never sent
	Other       bool   `json:"other"`       // came from Details, Compress or the watched folder, not the folder
	Error       string `json:"error"`       // why it could not be read, "" when read
}

// LibraryView is the Console tab's list: the library folder's .p5z, then the others asked for.
type LibraryView struct {
	Folder string        `json:"folder"`
	Items  []LibraryItem `json:"items"`
	Error  string        `json:"error"` // "", "no_folder" (none set) or "unreadable" (it cannot be listed)
}

// libraryEntry is what was read of a file, for as long as its size and time are the same.
type libraryEntry struct {
	size     int64
	mtime    time.Time
	passcode string // what it was read with: another passcode reads it again
	item     LibraryItem
}

// libraryIconEntry is the cover of a file (a data URL, "" when it has none), kept as a libraryEntry is.
type libraryIconEntry struct {
	size     int64
	mtime    time.Time
	passcode string
	icon     string
}

// libraryReaders is how many files are read by the engine at once.
const libraryReaders = 4

// readLibraryItem reads a .p5z with the engine's read-only calls: nothing of it is written, not even to undo
// a change left half done (that is said in Interrupted).
func readLibraryItem(engine, path string, size int64) LibraryItem {
	item := LibraryItem{Path: path, Size: size}
	info, err := core.InfoReadOnly(engine, path)
	if err != nil {
		item.Name = filepath.Base(path)
		item.Error = err.Error()
		return item
	}
	text := func(key string) string { s, _ := info[key].(string); return s }
	item.Name, item.TitleID, item.Version = text("name"), text("title_id"), text("version")
	if item.Name == "" {
		item.Name = filepath.Base(path)
	}
	item.Interrupted, _ = info["interrupted_append"].(bool)
	return item
}

// libraryIcon is the icon of a .p5z as a data URL, as iconURL gives it, read without writing the .p5z.
func libraryIcon(engine, path string) string {
	folder, err := os.MkdirTemp("", "maxpack-icon-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(folder)
	out := filepath.Join(folder, "icon0.png")
	if core.IconReadOnly(engine, path, out) != nil {
		return ""
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) == 0 || len(data) > maxIcon {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

// Library lists the .p5z of the library folder (its first level only), by name, and then the others
// given that are not in it, marked Other. It writes nothing: a .p5z is read with the engine's read-only
// calls, and what was read is kept while its size and time stay the same.
func (a *App) Library(others []string) LibraryView {
	saved, _ := settings.Load()
	view := LibraryView{Folder: saved.LibraryFolder, Items: []LibraryItem{}}
	var paths []string
	inFolder := map[string]bool{}
	if view.Folder == "" {
		view.Error = "no_folder"
	} else if entries, err := os.ReadDir(view.Folder); err != nil {
		view.Error = "unreadable"
	} else {
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(strings.ToLower(entry.Name()), ".p5z") {
				paths = append(paths, filepath.Join(view.Folder, entry.Name()))
			}
		}
		sort.SliceStable(paths, func(i, j int) bool {
			x, y := strings.ToLower(filepath.Base(paths[i])), strings.ToLower(filepath.Base(paths[j]))
			if x != y {
				return x < y
			}
			return paths[i] < paths[j]
		})
		for _, path := range paths {
			inFolder[pathKey(path)] = true
		}
	}
	count := len(paths)
	for _, path := range others {
		if key := pathKey(path); path != "" && !inFolder[key] {
			inFolder[key] = true
			paths = append(paths, path)
		}
	}
	items := make([]LibraryItem, len(paths))
	engine, engineErr := a.engine()
	var wait sync.WaitGroup
	slots := make(chan struct{}, libraryReaders)
	for i, path := range paths {
		wait.Add(1)
		slots <- struct{}{}
		go func() {
			defer wait.Done()
			defer func() { <-slots }()
			items[i] = a.libraryItem(engine, engineErr, path)
			items[i].Other = i >= count
		}()
	}
	wait.Wait()
	view.Items = append(view.Items, items...)
	return view
}

// libraryItem reads one file, or gives what was kept of it when its size and time are the same.
func (a *App) libraryItem(engine string, engineErr error, path string) LibraryItem {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		message := "not a file"
		if err != nil {
			message = err.Error()
		}
		return LibraryItem{Path: path, Name: filepath.Base(path), Error: message}
	}
	key := pathKey(path)
	passcode := strings.Join(core.PasscodeArgs(), " ")
	a.libraryMutex.Lock()
	kept, ok := a.libraryCache[key]
	a.libraryMutex.Unlock()
	if ok && kept.size == info.Size() && kept.mtime.Equal(info.ModTime()) && kept.passcode == passcode {
		return kept.item
	}
	if engineErr != nil {
		return LibraryItem{Path: path, Name: filepath.Base(path), Size: info.Size(), Error: engineErr.Error()}
	}
	item := readLibraryItem(engine, path, info.Size())
	// a file the engine could not read is kept too: it is the same file until its size or time changes
	a.libraryMutex.Lock()
	if a.libraryCache == nil {
		a.libraryCache = map[string]libraryEntry{}
	}
	a.libraryCache[key] = libraryEntry{info.Size(), info.ModTime(), passcode, item}
	a.libraryMutex.Unlock()
	return item
}

// ForgetLibraryErrors drops what was kept of the files that could not be read, so that the next Library reads
// them again (Read again); what was read stays kept.
func (a *App) ForgetLibraryErrors() {
	a.libraryMutex.Lock()
	defer a.libraryMutex.Unlock()
	for key, kept := range a.libraryCache {
		if kept.item.Error != "" {
			delete(a.libraryCache, key)
		}
	}
}

// LibraryIcon is the cover of a .p5z of the Console tab's list as a data URL, "" when it has none or is not a
// file. It is asked apart from Library, which the page reads often, with the engine's read-only icon, and kept
// (none too) while the file's size and time and the passcode are the same.
func (a *App) LibraryIcon(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	key, passcode := pathKey(path), strings.Join(core.PasscodeArgs(), " ")
	a.libraryMutex.Lock()
	kept, ok := a.libraryIcons[key]
	a.libraryMutex.Unlock()
	if ok && kept.size == info.Size() && kept.mtime.Equal(info.ModTime()) && kept.passcode == passcode {
		return kept.icon
	}
	engine, err := a.engine()
	if err != nil {
		return ""
	}
	icon := libraryIcon(engine, path)
	a.libraryMutex.Lock()
	if a.libraryIcons == nil {
		a.libraryIcons = map[string]libraryIconEntry{}
	}
	a.libraryIcons[key] = libraryIconEntry{info.Size(), info.ModTime(), passcode, icon}
	a.libraryMutex.Unlock()
	return icon
}

// ChooseLibraryFolder asks for the library folder, from the one set (else the last destination), and keeps
// it; "" when the dialog is closed without one.
func (a *App) ChooseLibraryFolder() (string, error) {
	if a.ctx == nil {
		return "", errNoWindow
	}
	options := wailsruntime.OpenDialogOptions{}
	saved, _ := settings.Load()
	for _, start := range []string{saved.LibraryFolder, saved.LastDest} {
		if start == "" {
			continue
		}
		if info, err := os.Stat(start); err == nil && info.IsDir() {
			options.DefaultDirectory = start
			break
		}
	}
	chosen, err := wailsruntime.OpenDirectoryDialog(a.ctx, options)
	if err != nil || chosen == "" {
		return "", err
	}
	saved.LibraryFolder = chosen
	if err := settings.Save(saved); err != nil {
		return "", err
	}
	return saved.Normalize().LibraryFolder, nil
}
