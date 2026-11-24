// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/DeLFuS77/maxpack/gui/bundled"
	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
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
	// stops the comparison of levels or the measure at work (see tools.go), nil when none is
	toolCancel context.CancelCauseFunc

	// closing while a queue runs (see closing.go): the words the page told for the question,
	// and whether the user said to cancel and close
	closeAsk, closeYes, closeNo string
	// the language the page speaks (TellLanguage), for what the Go side says
	pageLang       string
	closeConfirmed atomic.Bool

	// what the tests stand in for; the zero values are the real ones
	emit    func(event string, data any)
	extras  extras
	core    string        // the engine, when not the one Locate finds
	fs      jobs.FS       // the drives, when not the ones of the system
	lang    func() string // the language of the notices, when not the one of the settings
	onEvent func(i int, e core.Event)
	onItem  func(i int, state, detail string)
	// the dialog that chooses a file, when not the one of the system
	openFile func(ctx context.Context, options wailsruntime.OpenDialogOptions) (string, error)
	// whether an original can go to the recycle bin ("" when it can, else why: "no_bin",
	// "too_big", "too_long"), the clipboard and the browser, when not the ones of the system
	trashCheck func(path string) string
	clipboard  func(text string) error
	openURL    func(url string)
	// the dialog that chooses folders, when not the one of the system
	chooseFolders func() ([]string, error)

	notices      sync.Once
	noticesReady bool

	// the ampr-emu-ng that comes with MaxPack, written once to the folder of the settings (see
	// builtInRuntime): its path, "" when it could not be written
	runtimeOnce sync.Once
	runtimePath string
}

// Setup is what the page shows before anything is chosen.
type Setup struct {
	Version     string `json:"version"`
	Core        string `json:"core"`
	CoreVersion string `json:"coreVersion"`
	CoreError   string `json:"coreError"`
	// CoreOtherVersion: maxpack-core is there but is of another version (CoreError says so)
	CoreOtherVersion bool   `json:"coreOtherVersion"`
	Lang             string `json:"lang"`
	Theme            string `json:"theme"`
	// BuiltInRuntime is the libSceAmpr.sprx of the ampr-emu-ng that comes with MaxPack, in the
	// folder of the settings, and BuiltInRuntimeVersion its release; both "" when it could not be
	// written there (then a game needs one chosen in Settings, as before)
	BuiltInRuntime        string `json:"builtInRuntime"`
	BuiltInRuntimeVersion string `json:"builtInRuntimeVersion"`
	// LogFile is where the window writes what it removed (the originals once checked)
	LogFile string `json:"logFile"`
	// Templates are the templates of the names Settings offers, the default first
	Templates []string `json:"templates"`
	// CPUThreads are the threads of this PC, which Compress gives the engine unless a measure
	// found fewer to be as fast (Settings.Threads)
	CPUThreads int `json:"cpuThreads"`
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// the ampr-emu-ng that comes with MaxPack is written now, before anything asks for it
	a.builtInRuntime()
	// what the window was started with waits for the page, as what other MaxPack send does
	a.receive(a.files)
	a.files = nil
	if a.started != nil {
		a.started()
	}
}

// Setup says what the window runs with. It never fails: a missing or broken maxpack-core, or one
// of another version (one that does not do all the window needs, as --capabilities says), is
// reported in CoreError and the window opens all the same.
func (a *App) Setup() Setup {
	saved, _ := settings.Load()
	setup := Setup{Version: version, Lang: saved.EffectiveLang(), Theme: saved.Theme, LogFile: logFile(), Templates: jobs.Templates, CPUThreads: runtime.NumCPU()}
	if builtIn := a.builtInRuntime(); builtIn != "" {
		setup.BuiltInRuntime, setup.BuiltInRuntimeVersion = builtIn, bundled.Version()
	}
	path, err := core.Locate()
	if err != nil {
		setup.CoreError = err.Error()
		return setup
	}
	setup.Core = path
	if setup.CoreVersion, err = core.Version(path); err != nil {
		setup.CoreError = err.Error()
		return setup
	}
	if missing, err := core.Missing(path); err != nil || len(missing) > 0 {
		setup.CoreError, setup.CoreOtherVersion = "maxpack-core is of another version", true
	}
	return setup
}

// builtInRuntime is the path of the ampr-emu-ng that comes with MaxPack: it is written to the folder
// of the settings once a run, only when it is not there or is not this one (bundled.Install), so
// the engine can be given it. "" when it could not be written.
func (a *App) builtInRuntime() string {
	a.runtimeOnce.Do(func() {
		if folder, err := settings.Dir(); err == nil {
			a.runtimePath, _ = bundled.Install(folder)
		}
	})
	return a.runtimePath
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
// front and the files go to the page, to Compress or Decompress by what they are.
func (a *App) secondInstance(data wailsoptions.SecondInstanceData) {
	go system.BringToFront()
	if start, ok := windowLaunch(absolute(data.Args, data.WorkingDirectory)); ok {
		a.receive(start.files)
	}
}
