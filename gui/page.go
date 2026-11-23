// SPDX-License-Identifier: GPL-3.0-or-later

package main

// What the page asks of the window: the games it is given (chosen, dropped or sent), what they
// are, the plans of a queue, and the queue itself.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Game is a game of the list: what jobs plans with, its icon (a data URL, "" when it has none),
// and whether it is compressed already.
type Game struct {
	jobs.Item
	Packed bool
}

// GameDetails is all the engine says of a game, for Detalles: with its packs, the files stored
// in them and their volumes (0 when it is not compressed).
type GameDetails struct {
	Game
	InstallSize, P5zSize uint64
	Files                int
	PackedFiles, Volumes uint64
	HasRuntime           bool
}

// Choices are the options of a queue as the page sends them: the ones jobs plans with, and the
// destinations the user said may be replaced.
type Choices struct {
	jobs.Options
	Replace []string
}

// PlanView is a plan as the page shows it before a queue starts: with what is in the way of it
// takes, to say it when asking whether to replace it.
type PlanView struct {
	jobs.Plan
	ReplaceSize uint64
}

// takes are the games a section works on.
type takes struct{ dumps, images, p5z bool }

var sectionTakes = map[string]takes{
	"compress":   {dumps: true, images: true},
	"decompress": {p5z: true},
	// .p5z and compressed folders; the page tells a folder that is not compressed
	"verify":  {dumps: true, p5z: true},
	"details": {dumps: true, images: true, p5z: true},
}

// the files a section takes in the dialog of Añadir archivos (its folders, with Añadir carpeta)
var sectionPatterns = map[string]string{
	"compress":   "*.exfat",
	"decompress": "*.p5z",
	"verify":     "*.p5z",
	"details":    "*.exfat;*.p5z",
}

var errNoWindow = errors.New("sin ventana")

func (k takes) file(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".exfat":
		return k.images
	case ".p5z":
		return k.p5z
	}
	return false
}

// isDump tells whether a folder is a game: it has sce_sys/param.json, as the engine reads it.
func isDump(path string) bool {
	info, err := os.Stat(filepath.Join(path, "sce_sys", "param.json"))
	return err == nil && !info.IsDir()
}

// pathKey makes paths comparable: clean and, on Windows, in lower case.
func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// Expand gives the games of a section among the paths given: a game itself (a dump folder, an
// .exfat image or a .p5z, as the section takes them), or the ones one level below a folder that
// is not a game. Each comes once, in the order given, and by name within a folder.
func (a *App) Expand(paths []string, section string) []string {
	found := []string{}
	k, ok := sectionTakes[section]
	if !ok {
		return found
	}
	seen := map[string]bool{}
	add := func(path string) {
		if key := pathKey(path); !seen[key] {
			seen[key] = true
			found = append(found, path)
		}
	}
	// a game of the section at path; with below, a folder that is not a game is looked into
	var take func(path string, below bool)
	take = func(path string, below bool) {
		info, err := os.Stat(path)
		switch {
		case err != nil:
		case !info.IsDir():
			if k.file(path) {
				add(path)
			}
		case isDump(path):
			if k.dumps {
				add(path)
			}
		case below:
			entries, _ := os.ReadDir(path)
			for _, entry := range entries {
				take(filepath.Join(path, entry.Name()), false)
			}
		}
	}
	for _, path := range paths {
		take(filepath.Clean(path), true)
	}
	return found
}

// engine is maxpack-core: the one the tests give, else the one next to the program.
func (a *App) engine() (string, error) {
	if a.core != "" {
		return a.core, nil
	}
	return core.Locate()
}

// maxIcon is the most an icon may take: anything bigger is not one, and is left out.
const maxIcon = 4 << 20

// iconURL is the icon of a game as a data URL, for the list: the engine writes it in a
// temporary folder, which goes once it is read. A game with no icon has "".
func iconURL(engine, path string) string {
	folder, err := os.MkdirTemp("", "maxpack-icon-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(folder)
	out := filepath.Join(folder, "icon0.png")
	if core.Icon(engine, path, out) != nil {
		return ""
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) == 0 || len(data) > maxIcon {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

// describe asks the engine what a game is, and for its icon.
func (a *App) describe(path string) (Game, map[string]any, error) {
	engine, err := a.engine()
	if err != nil {
		return Game{}, nil, err
	}
	info, err := core.Info(engine, path)
	if err != nil {
		return Game{}, nil, err
	}
	packed, _ := info["packed"].(bool)
	game := Game{Item: jobs.FromInfo(path, info), Packed: packed}
	game.Icon = iconURL(engine, path)
	return game, info, nil
}

// Describe gives a game of the list: its title, ID, version, size and icon.
func (a *App) Describe(path string) (Game, error) {
	game, _, err := a.describe(path)
	return game, err
}

// Details gives all the engine says of a game: Describe, and its sizes installed and as a .p5z,
// its files, its packs and its runtime of ampr-emu-ng.
func (a *App) Details(path string) (GameDetails, error) {
	game, info, err := a.describe(path)
	if err != nil {
		return GameDetails{}, err
	}
	number := func(key string) uint64 { n, _ := info[key].(float64); return uint64(n) }
	carries, _ := info["has_runtime"].(bool)
	return GameDetails{Game: game, InstallSize: number("install_size"), P5zSize: number("p5z_size"),
		Files: int(number("files")), PackedFiles: number("packed_files"), Volumes: number("volumes"),
		HasRuntime: carries}, nil
}

// sectionOptions are the options of a queue of a section: its mode, whatever the page sent.
func sectionOptions(section string, o jobs.Options) (jobs.Options, error) {
	switch section {
	case "compress":
		if o.Mode != jobs.ModeFolder {
			o.Mode = jobs.ModeP5z
		}
	case "decompress":
		if o.Mode != jobs.ModeConsole {
			o.Mode = jobs.ModeOriginal
		}
	case "verify":
		o.Mode = jobs.ModeVerify
	default:
		return o, fmt.Errorf("la sección %q no hace trabajos", section)
	}
	return o, nil
}

// drives are the drives the plans ask: the ones of the system, or the stand-in of the tests.
func (a *App) drives() jobs.FS {
	if a.fs != nil {
		return a.fs
	}
	return jobs.SystemFS()
}

// inHomebrew tells whether a game made for the console is where ShadowMountPlus looks for it:
// a folder in homebrew/ at the root of its drive. Elsewhere (the destination was not the root
// of a drive) it has to be moved there.
func inHomebrew(out string, drives jobs.FS) bool {
	parent := filepath.Dir(filepath.Clean(out))
	return strings.EqualFold(filepath.Base(parent), "homebrew") && pathKey(filepath.Dir(parent)) == pathKey(drives.Root(out))
}

// plans are the plans of a queue of a section.
func (a *App) plans(section string, items []jobs.Item, o jobs.Options) ([]jobs.Plan, jobs.Options, error) {
	o, err := sectionOptions(section, o)
	if err != nil {
		return nil, o, err
	}
	drives := a.drives()
	switch o.Mode {
	case jobs.ModeVerify:
		return jobs.PlanVerify(items), o, nil
	case jobs.ModeOriginal, jobs.ModeConsole:
		return jobs.PlanDecompress(items, o, drives), o, nil
	}
	return jobs.PlanCompress(items, o, drives), o, nil
}

// sizeOn is what a file or a folder takes, its files added up.
func sizeOn(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return uint64(info.Size())
	}
	var total uint64
	filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += uint64(info.Size())
			}
		}
		return nil
	})
	return total
}

// Plan gives what a queue of a section would do with the games, before it starts: where each
// goes, what it replaces and how much that takes, what to warn of and what cannot go.
func (a *App) Plan(section string, items []jobs.Item, opts Choices) ([]PlanView, error) {
	plans, _, err := a.plans(section, items, opts.Options)
	if err != nil {
		return nil, err
	}
	views := make([]PlanView, len(plans))
	for i, p := range plans {
		views[i] = PlanView{Plan: p}
		for _, path := range p.Replace {
			views[i].ReplaceSize += sizeOn(path)
		}
	}
	return views, nil
}

// Start starts a queue of a section and returns at once: the page is told each state of a game
// (job:item), its progress (job:progress) and, at the end, what the queue did (job:done). The
// games are planned again, so what changed since Plan counts: something in the way that the
// user did not say may be replaced (opts.Replace) stays as it is, and its game is skipped
// ("exists"). One queue at a time.
func (a *App) Start(section string, items []jobs.Item, opts Choices) error {
	plans, o, err := a.plans(section, items, opts.Options)
	if err != nil {
		return err
	}
	accepted := map[string]bool{}
	for _, path := range opts.Replace {
		accepted[pathKey(path)] = true
	}
	for i, p := range plans {
		if len(p.Replace) > 0 && p.Block == "" && !accepted[pathKey(p.Out)] {
			plans[i].Block = "exists"
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &queue{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	a.mutex.Lock()
	if a.queue != nil {
		a.mutex.Unlock()
		cancel()
		return errors.New("ya hay un trabajo en marcha")
	}
	a.queue = q
	a.mutex.Unlock()
	go func() {
		sum := a.run(ctx, plans, o)
		a.mutex.Lock()
		a.queue = nil
		a.mutex.Unlock()
		cancel()
		close(q.done)
		// told once the next queue can start
		a.send("job:done", sum)
	}()
	return nil
}

// queue is the queue running: its context, how to stop it, and when the Runner returned (with
// what it made and did not verify taken away).
type queue struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// Cancel stops the queue running: the game at work stops without leaving anything half made,
// and the ones waiting are not started.
func (a *App) Cancel() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.queue != nil {
		a.queue.cancel()
	}
}

// working tells whether a queue is running.
func (a *App) working() bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.queue != nil
}

// ChooseFiles asks for files of a section, and gives its games among them.
func (a *App) ChooseFiles(section string) ([]string, error) {
	if a.ctx == nil {
		return []string{}, errNoWindow
	}
	options := wailsruntime.OpenDialogOptions{}
	if pattern := sectionPatterns[section]; pattern != "" {
		options.Filters = []wailsruntime.FileFilter{{DisplayName: strings.ReplaceAll(pattern, ";", ", "), Pattern: pattern}}
	}
	paths, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, options)
	if err != nil {
		return []string{}, err
	}
	return a.Expand(paths, section), nil
}

// ChooseFolder asks for a folder: a game, or a folder with games one level below.
func (a *App) ChooseFolder(section string) ([]string, error) {
	if a.ctx == nil {
		return []string{}, errNoWindow
	}
	path, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{})
	if err != nil || path == "" {
		return []string{}, err
	}
	return a.Expand([]string{path}, section), nil
}

// ChooseDest asks for the folder the games go to, from the last one chosen; "" when none is.
func (a *App) ChooseDest() (string, error) {
	if a.ctx == nil {
		return "", errNoWindow
	}
	options := wailsruntime.OpenDialogOptions{CanCreateDirectories: true}
	if saved, _ := settings.Load(); saved.LastDest != "" {
		if info, err := os.Stat(saved.LastDest); err == nil && info.IsDir() {
			options.DefaultDirectory = saved.LastDest
		}
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, options)
}

// ChooseRuntime asks for the libSceAmpr.sprx of ampr-emu-ng that reads zstd, for the dumps that
// do not carry one; "" when none is chosen.
func (a *App) ChooseRuntime() (string, error) {
	if a.ctx == nil {
		return "", errNoWindow
	}
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Filters: []wailsruntime.FileFilter{{DisplayName: "libSceAmpr.sprx", Pattern: "*.sprx"}},
	})
}
