// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// FilesAdd is what the page is told when files reach the window: what goes to the list of
// Compress and what to the one of Decompress.
type FilesAdd struct {
	Compress   []string `json:"compress"`
	Decompress []string `json:"decompress"`
}

func (f FilesAdd) empty() bool { return len(f.Compress) == 0 && len(f.Decompress) == 0 }

// routeFiles tells where each path goes: a .p5z (any case) to Decompress; a folder or an
// .exfat to Compress; anything else is left out. The order is kept.
func routeFiles(paths []string) (compress, decompress []string) {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		name := strings.ToLower(path)
		switch {
		case info.IsDir():
			compress = append(compress, path)
		case strings.HasSuffix(name, ".p5z"):
			decompress = append(decompress, path)
		case strings.HasSuffix(name, ".exfat"):
			compress = append(compress, path)
		}
	}
	return compress, decompress
}

// receive takes paths that reach the window, at the start or from another MaxPack. Until the
// page asks for what is pending (PendingFiles, as it loads) they are kept; from then on they
// are sent to it as the event "files:add", so none is lost or told twice.
func (a *App) receive(paths []string) {
	compress, decompress := routeFiles(paths)
	add := FilesAdd{Compress: compress, Decompress: decompress}
	if add.empty() {
		return
	}
	a.mutex.Lock()
	if !a.pageReady {
		a.pending.Compress = append(a.pending.Compress, compress...)
		a.pending.Decompress = append(a.pending.Decompress, decompress...)
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
	if pending.Compress == nil {
		pending.Compress = []string{}
	}
	if pending.Decompress == nil {
		pending.Decompress = []string{}
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
