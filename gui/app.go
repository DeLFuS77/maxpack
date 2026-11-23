// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"

	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is what the window asks for: its methods are called from the page.
type App struct {
	ctx   context.Context
	files []string
	// lets the next MaxPack start, once this window takes files
	started func()
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
	if a.started != nil {
		a.started()
	}
}

// Setup says what the window runs with. It never fails: a missing or broken maxpack-core is
// reported in CoreError and the window opens all the same.
func (a *App) Setup() Setup {
	setup := Setup{Version: version, Lang: "en", Theme: "system"}
	if strings.HasPrefix(strings.ToLower(system.Language()), "es") {
		setup.Lang = "es"
	}
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

// secondInstance takes the files another MaxPack was started with: this window comes to the
// front and the page is told.
func (a *App) secondInstance(data wailsoptions.SecondInstanceData) {
	go system.BringToFront()
	start, ok := windowLaunch(absolute(data.Args, data.WorkingDirectory))
	if !ok || len(start.files) == 0 || a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "opened", start.files)
}
