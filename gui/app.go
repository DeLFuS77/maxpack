// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeLFuS77/maxpack/gui/bundled"
	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/par2"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is what the window asks for: its methods are called from the page.
type App struct {
	ctx context.Context
	// what the window was started with, until the page takes it
	start launch
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
	par2    string        // maxpack-par2, when not the one par2.Locate finds ("-" for none)
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

	// the watched folder (see watch.go): the watcher at work, whether the page listens, and how often and how
	// long when not the ones of watch.go (the tests)
	watchMutex             sync.Mutex
	watch                  *watching
	watchListening         bool
	watchEvery, watchQuiet time.Duration
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
	// Explorer: this system has Settings › Windows Explorer (see shell.go); Portable: this copy
	// keeps everything next to it, and writes nothing in Windows
	Explorer bool `json:"explorer"`
	Portable bool `json:"portable"`
	// Par2 is maxpack-par2, which makes and uses the repair data of a .p5z, and Par2Version what it
	// says of itself; Par2Error, why there is none (a build of the window without it): the page then
	// offers no repair data, and says why
	Par2        string `json:"par2"`
	Par2Version string `json:"par2Version"`
	Par2Error   string `json:"par2Error"`
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// the ampr-emu-ng that comes with MaxPack is written now, before anything asks for it
	a.builtInRuntime()
	// what the window was started with waits for the page, as what other MaxPack send does
	a.receive(a.start)
	a.start = launch{}
	if a.started != nil {
		a.started()
	}
}

// Setup says what the window runs with. It never fails: a missing or broken maxpack-core, or one
// of another version (one that does not do all the window needs, as --capabilities says), is
// reported in CoreError and the window opens all the same.
func (a *App) Setup() Setup {
	saved, _ := settings.Load()
	core.SetPasscode(saved.Passcode)
	setup := Setup{Version: version, Lang: saved.EffectiveLang(), Theme: saved.Theme, LogFile: logFile(), Templates: jobs.Templates, CPUThreads: runtime.NumCPU(),
		Explorer: shell.available(), Portable: isPortable()}
	if builtIn := a.builtInRuntime(); builtIn != "" {
		setup.BuiltInRuntime, setup.BuiltInRuntimeVersion = builtIn, bundled.Version()
	}
	if program, err := a.par2Program(); err != nil {
		setup.Par2Error = err.Error()
	} else if v, err := par2.Version(program); err != nil {
		setup.Par2Error = err.Error()
	} else {
		setup.Par2, setup.Par2Version = program, v
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

// par2Program is maxpack-par2: the one the tests give, else the one next to the program.
func (a *App) par2Program() (string, error) {
	switch a.par2 {
	case "":
		return par2.Locate()
	case "-":
		return "", errors.New("maxpack-par2 not found")
	}
	return a.par2, nil
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

// SetSettings keeps what the user chose; what is not valid goes back to its default. The passcode
// of PS5 packages goes to the engine's runs from now on, and the watched folder is the one it says.
func (a *App) SetSettings(s settings.Settings) error {
	core.SetPasscode(s.Normalize().Passcode)
	if err := settings.Save(s); err != nil {
		return err
	}
	// the watched folder of Settings from now on
	a.rewatch()
	return nil
}

// bringToFront puts this window in front of the others (a variable, for the tests).
var bringToFront = system.BringToFront

// secondInstance takes what another MaxPack was started with (from the menu of the Explorer, Send
// to, a double click on a .p5z, or files dropped on MaxPack): this window comes to the front and
// the files go to the page, to the space the Explorer asked for or the one that fits them (see
// spaceOf). Wails calls it from its own goroutine, which the window coming to the front may wait
// for, so that goes apart.
func (a *App) secondInstance(data wailsoptions.SecondInstanceData) {
	go bringToFront()
	if start, ok := windowLaunch(absolute(data.Args, data.WorkingDirectory)); ok {
		a.receive(start)
	}
}
