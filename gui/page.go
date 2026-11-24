// SPDX-License-Identifier: GPL-3.0-or-later

package main

// What the page asks of the window: the games it is given (chosen, dropped or sent), what they
// are, the plans of a queue, and the queue itself.

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/internal/trash"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Game is a game of the list: what jobs plans with (whether it is compressed already too), and its
// icon (a data URL, "" when it has none).
type Game struct {
	jobs.Item
}

// GameDetails is all the engine says of a game, for Details: with its packs, the files stored
// in them and their volumes (0 when it is not compressed).
type GameDetails struct {
	Game
	InstallSize, P5zSize uint64
	Files                int
	PackedFiles, Volumes uint64
	HasRuntime           bool
}

// Choices are the options of a queue as the page sends them: the ones jobs plans with, the
// destinations the user said may be replaced, and, when compressing, whether the originals go
// once checked (to the recycle bin, or for good as Settings says) and whether the ones that cannot
// go to the recycle bin are deleted for good. The names of the files and the priority of the
// engine are the ones of Settings, whatever the page sends.
type Choices struct {
	jobs.Options
	Replace         []string
	DeleteOriginals bool
	DeleteNoBin     bool
}

// PlanView is a plan as the page shows it before a queue starts: with what is in the way of it
// takes, to say it when asking whether to replace it; and, when the originals go once checked,
// what the original takes and why it cannot go to the recycle bin ("no_bin", "too_big",
// "too_long"; "" when it can, or when it is deleted for good).
type PlanView struct {
	jobs.Plan
	ReplaceSize uint64
	SourceSize  uint64
	NoBin       string
}

// takes are the games a section works on.
type takes struct{ dumps, images, p5z bool }

var sectionTakes = map[string]takes{
	"compress": {dumps: true, images: true},
	// Rename names .p5z files, dump folders and images by what they are
	"rename":     {dumps: true, images: true, p5z: true},
	"decompress": {p5z: true},
	// .p5z and compressed folders; the page tells a folder that is not compressed
	"verify":  {dumps: true, p5z: true},
	"details": {dumps: true, images: true, p5z: true},
}

// the files a section takes in the "Add files" dialog (its folders, with "Add folder")
var sectionPatterns = map[string]string{
	"compress":   "*.exfat",
	"decompress": "*.p5z",
	"verify":     "*.p5z",
	"details":    "*.exfat;*.p5z",
	"rename":     "*.exfat;*.p5z",
}

var errNoWindow = errors.New("no window")

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
	game := Game{Item: jobs.FromInfo(path, info)}
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
		return o, fmt.Errorf("the section %q does not do jobs", section)
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

// plans are the plans of a queue of a section. To compress, a game that does not carry a runtime
// that reads zstd is given the one chosen in Settings (o.Runtime) or, when none was chosen, the
// ampr-emu-ng that comes with MaxPack. The names of the files and the priority of the engine are
// the ones of Settings; the originals go only when compressing and when the page asked: to the
// recycle bin, or for good when Settings says so.
func (a *App) plans(section string, items []jobs.Item, choices Choices) ([]jobs.Plan, jobs.Options, error) {
	o, err := sectionOptions(section, choices.Options)
	if err != nil {
		return nil, o, err
	}
	if section == "compress" && o.Runtime == "" {
		o.Runtime = a.builtInRuntime()
	}
	saved, _ := settings.Load()
	o.NameTemplate, o.LowPriority = saved.NameTemplate, saved.LowPriority
	o.Originals, o.OriginalsNoBin = jobs.OriginalsKeep, false
	if section == "compress" && choices.DeleteOriginals {
		o.Originals, o.OriginalsNoBin = jobs.OriginalsTrash, choices.DeleteNoBin
		if saved.DeletePermanently {
			o.Originals, o.OriginalsNoBin = jobs.OriginalsDelete, false
		}
	}
	return planWith(items, o, a.drives()), o, nil
}

// planWith plans games with the options of a queue.
func planWith(items []jobs.Item, o jobs.Options, drives jobs.FS) []jobs.Plan {
	switch o.Mode {
	case jobs.ModeVerify:
		return jobs.PlanVerify(items)
	case jobs.ModeOriginal, jobs.ModeConsole:
		return jobs.PlanDecompress(items, o, drives)
	}
	return jobs.PlanCompress(items, o, drives)
}

// Plan gives what a queue of a section would do with the games, before it starts: where each
// goes, what it replaces and how much that takes, what to warn of and what cannot go; with the
// originals to go, what each takes and whether it can go to the recycle bin.
func (a *App) Plan(section string, items []jobs.Item, opts Choices) ([]PlanView, error) {
	plans, o, err := a.plans(section, items, opts)
	if err != nil {
		return nil, err
	}
	views := make([]PlanView, len(plans))
	for i, p := range plans {
		views[i] = PlanView{Plan: p}
		for _, path := range p.Replace {
			views[i].ReplaceSize += jobs.SizeOf(path)
		}
		if o.Originals != jobs.OriginalsKeep && p.Block == "" {
			views[i].SourceSize = jobs.SizeOf(p.Item.Path)
			if o.Originals == jobs.OriginalsTrash {
				views[i].NoBin = a.binOf(p.Item.Path)
			}
		}
	}
	return views, nil
}

// binOf tells why an original cannot go to the recycle bin, "" when it can.
func (a *App) binOf(path string) string {
	if a.trashCheck != nil {
		return a.trashCheck(path)
	}
	why, ok := trash.Why(trash.Check(path))
	if !ok {
		return ""
	}
	return map[string]string{trash.NoBin: "no_bin", trash.TooBig: "too_big", trash.TooLong: "too_long"}[why.Reason]
}

// Space tells, before a queue starts, the drives where what its games may take is more than is
// free, with the numbers; none when it fits. The page asks it with the games that go. While a
// queue of the section runs, the games it has not started yet count too.
func (a *App) Space(section string, items []jobs.Item, opts Choices) ([]jobs.Drive, error) {
	plans, o, err := a.plans(section, items, opts)
	if err != nil {
		return nil, err
	}
	a.mutex.Lock()
	q := a.queue
	a.mutex.Unlock()
	if q != nil && q.section == section {
		plans, o = append(q.jobs.Waiting(), plans...), q.o
	}
	return jobs.Space(plans, o, a.drives()), nil
}

// Start starts a queue of a section and returns at once: the page is told each state of a game
// (job:item), its progress (job:progress) and, at the end, what the queue did (job:done). The
// games are planned again, so what changed since Plan counts: something in the way that the
// user did not say may be replaced (opts.Replace) stays as it is, and its game is skipped
// ("exists"). One queue at a time; games are added to it with Append.
func (a *App) Start(section string, items []jobs.Item, opts Choices) error {
	plans, o, err := a.plans(section, items, opts)
	if err != nil {
		return err
	}
	consent(plans, opts.Replace)
	ctx, cancel := context.WithCancelCause(context.Background())
	q := &queue{ctx: ctx, cancel: cancel, done: make(chan struct{}), section: section, o: o, jobs: jobs.NewQueue(plans)}
	a.mutex.Lock()
	if a.queue != nil {
		a.mutex.Unlock()
		cancel(nil)
		return errors.New("a job is already running")
	}
	a.queue = q
	a.mutex.Unlock()
	go func() {
		sum := a.runQueue(ctx, q)
		a.mutex.Lock()
		a.queue = nil
		a.mutex.Unlock()
		cancel(nil)
		close(q.done)
		// told once the next queue can start
		a.send("job:done", sum)
	}()
	return nil
}

// consent skips the games whose destination has something the user did not say may be replaced.
func consent(plans []jobs.Plan, replace []string) {
	accepted := map[string]bool{}
	for _, path := range replace {
		accepted[pathKey(path)] = true
	}
	for i, p := range plans {
		if len(p.Replace) > 0 && p.Block == "" && !accepted[pathKey(p.Out)] {
			plans[i].Block = "exists"
		}
	}
}

// Append adds games to the queue of a section that runs: they are planned with its options, told
// to the page as queued (job:item) and done after the ones there. A game whose destination has
// something in it is skipped when its turn comes, unless opts.Replace says it may be replaced;
// with keepOriginals the originals of these games stay, whatever the queue does with the others.
// It gives the index in the queue of the first one; an error when no queue of that section runs
// or it is cancelled (the page then leaves them as they are).
func (a *App) Append(section string, items []jobs.Item, replace []string, keepOriginals bool) (int, error) {
	a.mutex.Lock()
	q := a.queue
	a.mutex.Unlock()
	if q == nil || q.section != section || q.ctx.Err() != nil {
		return 0, errors.New("no queue of this section is running")
	}
	plans := planWith(items, q.o, a.drives())
	consent(plans, replace)
	q.mutex.Lock()
	defer q.mutex.Unlock()
	for i := range plans {
		plans[i].KeepOriginal = keepOriginals
		// the same place as a game of the queue: the second meets the first
		for n := 0; n < q.jobs.Len(); n++ {
			if other, _ := q.jobs.Plan(n); other.Out != "" && pathKey(other.Out) == pathKey(plans[i].Out) {
				plans[i].Warnings = append(plans[i].Warnings, "duplicate_target")
				break
			}
		}
	}
	first, ok := q.jobs.Add(plans...)
	if !ok {
		return 0, errors.New("the queue has ended")
	}
	if q.share != nil {
		q.share.add(plans, q.o.Mode)
	}
	for i := range plans {
		a.send("job:item", itemEvent{I: first + i, State: jobs.StateQueued})
	}
	return first, nil
}

// Unqueue takes game i out of the queue of a section before its turn; false when it started
// already, or no queue of that section runs.
func (a *App) Unqueue(section string, i int) bool {
	a.mutex.Lock()
	q := a.queue
	a.mutex.Unlock()
	if q == nil || q.section != section {
		return false
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if !q.jobs.Remove(i) {
		return false
	}
	if q.share != nil {
		q.share.remove(i)
	}
	return true
}

// queue is the queue running: its context, how to stop it (with why: core.ErrClosing when the
// window closes), when the Runner returned (with what it made and did not verify taken away), its
// section and options, its games, and how far it is (share, with the mutex of its progress).
type queue struct {
	ctx     context.Context
	cancel  context.CancelCauseFunc
	done    chan struct{}
	section string
	o       jobs.Options
	jobs    *jobs.Queue
	mutex   sync.Mutex
	share   *share
}

// Cancel stops the queue running: the game at work stops without leaving anything half made,
// and the ones waiting are not started.
func (a *App) Cancel() { a.stop(nil) }

// stop cancels the queue running, if any, with why.
func (a *App) stop(why error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.queue != nil {
		a.queue.cancel(why)
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

// ChooseFolder asks for folders, several at once where the dialog of the system takes them (on
// Windows, macOS, and Linux with zenity; else one, with the dialog of the window): each one a
// game, or a folder with games one level below.
func (a *App) ChooseFolder(section string) ([]string, error) {
	if a.ctx == nil {
		return []string{}, errNoWindow
	}
	choose := a.chooseFolders
	if choose == nil {
		choose = func() ([]string, error) { return system.ChooseFolders("MaxPack") }
	}
	paths, err := choose()
	if errors.Is(err, system.ErrNoMultiPicker) {
		var path string
		path, err = wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{})
		paths = []string{path}
	}
	if err != nil {
		return []string{}, err
	}
	chosen := []string{}
	for _, path := range paths {
		if path != "" {
			chosen = append(chosen, path)
		}
	}
	return a.Expand(chosen, section), nil
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
// do not carry one, in place of the one that comes with MaxPack; "" when none is chosen. One that
// does not read zstd is refused (the error starts with "runtime_not_zstd", which the page says in
// its language): a game given it would pack, verify and not start.
func (a *App) ChooseRuntime() (string, error) {
	if a.ctx == nil {
		return "", errNoWindow
	}
	open := wailsruntime.OpenFileDialog
	if a.openFile != nil {
		open = a.openFile
	}
	path, err := open(a.ctx, wailsruntime.OpenDialogOptions{
		Filters: []wailsruntime.FileFilter{{DisplayName: "libSceAmpr.sprx", Pattern: "*.sprx"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := readsZstd(path); err != nil {
		return "", err
	}
	return path, nil
}

// runtimeMax is the most a libSceAmpr.sprx may take, as the engine takes it.
const runtimeMax = 64 << 20

// readsZstd tells whether a libSceAmpr.sprx reads the packs zstd: the one of ampr-emu-ng carries
// the mark AMPRZPK1, as the engine looks for it.
func readsZstd(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Size() == 0 || info.Size() > runtimeMax {
		return fmt.Errorf("runtime_not_zstd: %s does not look like a libSceAmpr.sprx (%d bytes)", path, info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Contains(data, []byte("AMPRZPK1")) {
		return fmt.Errorf("runtime_not_zstd: %s does not read zstd: it is not ampr-emu-ng's libSceAmpr.sprx", path)
	}
	return nil
}
