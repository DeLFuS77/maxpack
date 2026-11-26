// SPDX-License-Identifier: GPL-3.0-or-later

package main

// Looking for new versions (Settings › Internet), off until the user turns it on: then, once each time
// MaxPack starts, it asks GitHub for the latest release, and offers a newer one only once the signature of
// its SHA256SUMS checks with the key below (see internal/update). On Windows the installer is downloaded and
// checked against that list, and Update runs it; on macOS, Linux and in the portable version, Update opens
// the release's page.

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/update"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateKey is the minisign public key that signs the SHA256SUMS of each release: the second line of the
// author's minisign.pub (see docs/releasing.md). It is empty in this build: MaxPack then looks for nothing,
// and Settings says it is not available.
const updateKey = ""

// updateTimeout is the most a look (and the download of the installer) may take.
const updateTimeout = 30 * time.Minute

// UpdateView is what the page is told of new versions: State is "off" (not turned on), "unavailable" (no key
// in this build), "none" (this is the latest), "found" or "error" (Error says why: see update.Error); a
// version found has its page, and Install says that Update runs its installer (else it opens the page).
type UpdateView struct {
	State   string `json:"state"`
	Error   string `json:"error"`
	Version string `json:"version"`
	Page    string `json:"page"`
	Install bool   `json:"install"`
}

// updating is the look of this start: its answer once done, and what it found.
type updating struct {
	done  chan struct{}
	view  UpdateView
	offer update.Offer
}

// updateConfig is how a look is made: GitHub, the key of this build, the file of this system and the folder
// of the downloads (or the stand-in of the tests).
func (a *App) updateConfig() update.Config {
	if a.updates != nil {
		return *a.updates
	}
	c := update.Defaults()
	c.Current, c.PublicKey = version, updateKey
	c.Asset, c.Download = update.AssetFor(runtime.GOOS, runtime.GOARCH, isPortable())
	if folder, err := settings.Dir(); err == nil {
		c.Folder = filepath.Join(folder, "updates")
	} else {
		c.Download = false
	}
	return c
}

// updateAvailable tells whether this build can look for versions: it has a key to check them with.
func (a *App) updateAvailable() bool { return a.updateConfig().PublicKey != "" }

// CheckUpdate looks for a newer version, when Settings says so, at most once each time MaxPack starts: a
// second call (the page loaded again, or the switch turned on later) gets the first answer.
func (a *App) CheckUpdate() UpdateView {
	c := a.updateConfig()
	if c.PublicKey == "" {
		return UpdateView{State: "unavailable"}
	}
	a.updateMutex.Lock()
	if u := a.update; u != nil {
		a.updateMutex.Unlock()
		<-u.done
		return u.view
	}
	if saved, _ := settings.Load(); !saved.UpdateCheck {
		a.updateMutex.Unlock()
		return UpdateView{State: "off"}
	}
	u := &updating{done: make(chan struct{})}
	a.update = u
	a.updateMutex.Unlock()
	defer close(u.done)

	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	offer, err := update.Check(ctx, c)
	var failed *update.Error
	switch {
	case errors.As(err, &failed):
		u.view = UpdateView{State: "error", Error: failed.Code}
	case err != nil:
		u.view = UpdateView{State: "error", Error: "network"}
	case offer.Version == "":
		u.view = UpdateView{State: "none"}
	default:
		u.offer = offer
		u.view = UpdateView{State: "found", Version: offer.Version, Page: offer.Page, Install: offer.File != ""}
	}
	return u.view
}

var errNothingToUpdate = errors.New("no newer version was found")

// Update takes the version found: on Windows its installer, checked again against the signed list, runs and
// MaxPack closes (not while a queue runs); elsewhere the release's page opens.
func (a *App) Update() error {
	a.updateMutex.Lock()
	u := a.update
	a.updateMutex.Unlock()
	if u == nil {
		return errNothingToUpdate
	}
	<-u.done
	if u.view.State != "found" {
		return errNothingToUpdate
	}
	if u.offer.File == "" {
		switch {
		case a.openURL != nil:
			a.openURL(u.offer.Page)
		case a.ctx == nil:
			return errNoWindow
		default:
			wailsruntime.BrowserOpenURL(a.ctx, u.offer.Page)
		}
		return nil
	}
	if a.working() {
		return errBusy
	}
	if err := update.VerifyFile(u.offer.File, u.offer.SHA256); err != nil {
		return err
	}
	run := a.runInstaller
	if run == nil {
		run = func(path string) error { return exec.Command(path).Start() }
	}
	if err := run(u.offer.File); err != nil {
		return err
	}
	switch {
	case a.quit != nil:
		a.quit()
	case a.ctx != nil:
		wailsruntime.Quit(a.ctx)
	}
	return nil
}
