// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// FilesAdd is what the page is told when files reach the window: what goes to the list of each
// space, and the space to show, the one the Explorer asked for ("" when none was: the page shows
// the one that took them).
type FilesAdd struct {
	Compress []string `json:"compress"`
	Verify   []string `json:"verify"`
	Details  []string `json:"details"`
	Show     string   `json:"show"`
}

func (f FilesAdd) empty() bool {
	return len(f.Compress) == 0 && len(f.Verify) == 0 && len(f.Details) == 0
}

// list is the list of a space.
func (f *FilesAdd) list(space string) *[]string {
	switch space {
	case "compress":
		return &f.Compress
	case "verify":
		return &f.Verify
	case "details":
		return &f.Details
	}
	return nil
}

// spaceOf tells the space a path goes to, by its name and whether it is a folder: the one asked
// for when it takes it (Verify takes .p5z files and folders, compressed or not, as the page tells
// them apart; Details takes them all; Compress folders and .exfat images); else Compress for a
// folder or an .exfat image, and Details for a .p5z (any case), whose card offers Verify and
// Decompress. Anything else goes nowhere ("").
func spaceOf(name string, folder bool, asked string) string {
	kind := ""
	switch name = strings.ToLower(name); {
	case folder:
		kind = "folder"
	case strings.HasSuffix(name, ".p5z"):
		kind = "p5z"
	case strings.HasSuffix(name, ".exfat"):
		kind = "exfat"
	default:
		return ""
	}
	takes := map[string]map[string]bool{
		"compress": {"folder": true, "exfat": true},
		"verify":   {"folder": true, "p5z": true},
		"details":  {"folder": true, "exfat": true, "p5z": true},
	}
	if takes[asked][kind] {
		return asked
	}
	if kind == "p5z" {
		return "details"
	}
	return "compress"
}

// routeFiles tells where each path of a launch goes (see spaceOf); a path not there is left out.
// The order is kept. The space asked for shows when it took something.
func routeFiles(start launch) FilesAdd {
	var add FilesAdd
	for _, path := range start.files {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if list := add.list(spaceOf(path, info.IsDir(), start.section)); list != nil {
			*list = append(*list, path)
		}
	}
	if list := add.list(start.section); list != nil && len(*list) > 0 {
		add.Show = start.section
	}
	return add
}

// receive takes what reaches the window, at the start or from another MaxPack. Until the page asks
// for what is pending (PendingFiles, as it loads) it is kept; from then on it is sent to it as the
// event "files:add", so none is lost or told twice.
func (a *App) receive(start launch) {
	add := routeFiles(start)
	if add.empty() {
		return
	}
	a.mutex.Lock()
	if !a.pageReady {
		a.pending.Compress = append(a.pending.Compress, add.Compress...)
		a.pending.Verify = append(a.pending.Verify, add.Verify...)
		a.pending.Details = append(a.pending.Details, add.Details...)
		if add.Show != "" {
			a.pending.Show = add.Show
		}
		a.mutex.Unlock()
		return
	}
	a.mutex.Unlock()
	a.send("files:add", add)
}

// PendingFiles gives the files that came before the page was ready, once, and from then on the
// page is told of new ones with the event "files:add".
func (a *App) PendingFiles() FilesAdd {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	pending := a.pending
	a.pending = FilesAdd{}
	a.pageReady = true
	for _, list := range []*[]string{&pending.Compress, &pending.Verify, &pending.Details} {
		if *list == nil {
			*list = []string{}
		}
	}
	return pending
}

// send tells the page something.
func (a *App) send(event string, data any) {
	if a.emit != nil {
		a.emit(event, data)
		return
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, event, data)
	}
}
