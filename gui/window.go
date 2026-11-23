// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"runtime"

	"github.com/DeLFuS77/maxpack/gui/internal/portable"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/internal/windowplace"

	"github.com/wailsapp/wails/v2"
	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend
var frontend embed.FS

// appIcon is the icon of the window on Linux; Windows takes it from the resources of the program
// (build/winres) and macOS from its app.
//
//go:embed build/appicon.png
var appIcon []byte

// singleWindow is the lock that keeps MaxPack to one window: the files given to another MaxPack
// (the Explorer, the command line, the Finder) go to the one open.
const singleWindow = "maxpack-window-1"

// runWindow opens the window with the files given, and gives the exit code once it is closed.
func runWindow(start launch) int {
	app := &App{files: start.files}
	single := &wailsoptions.SingleInstanceLock{UniqueId: singleWindow, OnSecondInstanceLaunch: app.secondInstance}
	if runtime.GOOS == "windows" {
		// the files go to the MaxPack open, which comes to the front; one starting meanwhile
		// is waited for, until its window takes files (see startup)
		var another bool
		app.started, another = system.StartWindow(singleWindow)
		defer app.started()
		if another {
			system.AllowForeground()
		}
	}
	// the window opens where it was left and as big (see windowplace)
	place, _ := windowplace.LoadWindowPlace()
	err := wails.Run(&wailsoptions.App{
		Title:            "MaxPack " + version,
		Width:            place.Width,
		Height:           place.Height,
		MinWidth:         windowplace.WindowMinWidth,
		MinHeight:        windowplace.WindowMinHeight,
		StartHidden:      true,
		BackgroundColour: &wailsoptions.RGBA{R: 24, G: 24, B: 27, A: 255},
		AssetServer:      &assetserver.Options{Assets: frontend},
		OnStartup:        app.startup,
		OnDomReady:       func(ctx context.Context) { windowplace.PlaceWindow(ctx) },
		// closing with a queue running is asked first, and the queue is waited for (closing.go)
		OnBeforeClose:      app.beforeClose,
		OnShutdown:         app.closed,
		DragAndDrop:        &wailsoptions.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		SingleInstanceLock: single,
		// files opened on, or dropped on, the Dock icon go where the ones of a second MaxPack go
		Mac:   &mac.Options{OnFileOpen: func(path string) { app.receive([]string{path}) }},
		Linux: &linux.Options{Icon: appIcon, ProgramName: "maxpack"},
		Bind:  []any{app},
		// portable: the page keeps its settings next to the program too
		Windows: &windows.Options{WebviewUserDataPath: portable.WebviewFolder()},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "maxpack:", err)
		return 1
	}
	return 0
}
