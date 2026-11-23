// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
)

// App is what the window asks for: its methods are called from the page.
type App struct {
	ctx   context.Context
	files []string
	// lets the next MaxPack start, once this window takes files
	started func()

	mutex     sync.Mutex
	pending   FilesAdd // files that came before the page asked for them
	pageReady bool
	queue     *queue // the queue running (see Start), nil when there is none

	// closing while a queue runs (see closing.go): the words the page told for the question,
	// and whether the user said to cancel and close
	closeAsk, closeYes, closeNo string
	closeConfirmed              atomic.Bool

	// what the tests stand in for; the zero values are the real ones
	emit    func(event string, data any)
	extras  extras
	core    string        // the engine, when not the one Locate finds
	fs      jobs.FS       // the drives, when not the ones of the system
	lang    func() string // the language of the notices, when not the one of the settings
	onEvent func(i int, e core.Event)
	onItem  func(i int, state, detail string)

	notices      sync.Once
	noticesReady bool
}

// Setup is what the page shows before anything is chosen.
type Setup struct {
	Version     string `json:"version"`
	Core        string `json:"core"`
	CoreVersion string `json:"coreVersion"`
	CoreError   string `json:"coreError"`
	Lang        string `json:"lang"`
	Theme       string `json:"theme"`
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// what the window was started with waits for the page, as what other MaxPack send does
	a.receive(a.files)
	a.files = nil
	if a.started != nil {
		a.started()
	}
}

// Setup says what the window runs with. It never fails: a missing or broken maxpack-core is
// reported in CoreError and the window opens all the same.
func (a *App) Setup() Setup {
	saved, _ := settings.Load()
	setup := Setup{Version: version, Lang: saved.Lang, Theme: saved.Theme}
	path, err := core.Locate()
	if err != nil {
		setup.CoreError = err.Error()
		return setup
	}
	setup.Core = path
	if setup.CoreVersion, err = core.Version(path); err != nil {
		setup.CoreError = err.Error()
	}
	return setup
}

// GetSettings gives what the user chose in earlier runs.
func (a *App) GetSettings() settings.Settings {
	saved, _ := settings.Load()
	return saved
}

// SetSettings keeps what the user chose; what is not valid goes back to its default.
func (a *App) SetSettings(s settings.Settings) error {
	return settings.Save(s)
}

// secondInstance takes the files another MaxPack was started with: this window comes to the
// front and the files go to the page, to Comprimir or Descomprimir by what they are.
func (a *App) secondInstance(data wailsoptions.SecondInstanceData) {
	go system.BringToFront()
	if start, ok := windowLaunch(absolute(data.Args, data.WorkingDirectory)); ok {
		a.receive(start.files)
	}
}
