// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
)

// Mode is what is done and where it goes.
type Mode string

const (
	ModeP5z      Mode = "p5z"      // compress to an installer, one .p5z
	ModeFolder   Mode = "folder"   // compress to a folder to play from the USB
	ModeOriginal Mode = "original" // unpack to the original dump
	ModeConsole  Mode = "console"  // unpack to a folder for the console
	ModeVerify   Mode = "verify"   // only check
)

// fat32Max is the most a file may have on FAT32 (4 GiB less one byte).
const fat32Max = 1<<32 - 1

// Item is a game the user gave.
type Item struct {
	Path, TitleID, Name, Version, Kind string
	OriginalSize                       uint64
	HasZstdRuntime                     bool
	Icon                               string
}

// Options are what the user chose for a queue.
type Options struct {
	Mode          Mode
	Dest          string
	Level         int
	Threads       int
	Runtime       string
	ContinueFat32 bool
}

// Plan is what will be done with one game: where it goes, what it replaces, what to tell the
// user and, if it cannot be done, why not. Block is "same_as_source", "no_space" or
// "fat32_too_big" ("" when it can go). Warnings are "fat32", "needs_runtime", "may_not_fit"
// and "duplicate_target". A Plan with Replace is one the user accepted to replace: not going
// on with it is a matter of dropping the plan.
type Plan struct {
	Item     Item
	Out      string
	Tmp      string   // with Replace: where the new one is made, <Out>.maxpack-tmp
	Replace  []string // what exists in the way, and goes when the new one is done and checked
	Warnings []string
	Block    string
	Args     []string // the arguments of the engine, without the ones for the progress
}

// FS is what the plans ask of the drives, so the tests can put their own.
type FS interface {
	Free(path string) (uint64, error)
	Type(path string) (string, error)
	Root(path string) string
}

type systemFS struct{}

func (systemFS) Free(path string) (uint64, error) { return system.FreeSpace(path) }
func (systemFS) Type(path string) (string, error) { return system.FilesystemType(path) }
func (systemFS) Root(path string) string          { return system.VolumeRoot(path) }

// SystemFS asks the operating system.
func SystemFS() FS { return systemFS{} }

// Describe reads what the engine says of a game and gives it as an Item (the icon is left
// for who shows it).
func Describe(engine, path string) (Item, error) {
	info, err := core.Info(engine, path)
	if err != nil {
		return Item{}, err
	}
	return FromInfo(path, info), nil
}

// FromInfo is the Item of the game at path from what `info --json` says of it.
func FromInfo(path string, info map[string]any) Item {
	text := func(key string) string { s, _ := info[key].(string); return s }
	number := func(key string) uint64 { n, _ := info[key].(float64); return uint64(n) }
	flag := func(key string) bool { b, _ := info[key].(bool); return b }
	it := Item{
		Path: path, TitleID: text("title_id"), Name: text("name"), Version: text("version"),
		Kind: text("kind"), OriginalSize: number("original_size"), HasZstdRuntime: flag("has_zstd_runtime"),
	}
	if it.Name == "" {
		it.Name = baseName(path)
	}
	return it
}

func isRoot(dest string, fs FS) bool { return same(fs.Root(dest), dest) }

// consoleOut is where a game for the console goes: <root>/homebrew/<ID> in the root of a drive,
// else <dest>/<ID>.
func consoleOut(it Item, o Options, fs FS) string {
	if isRoot(o.Dest, fs) {
		return filepath.Join(o.Dest, "homebrew", idName(it))
	}
	return filepath.Join(o.Dest, idName(it))
}

// conflicts tells whether the destination and the source are one, or one holds the other.
func conflicts(source, out string) bool {
	if same(source, out) || inside(out, source) || inside(source, out) {
		return true
	}
	a, errA := os.Stat(source)
	b, errB := os.Stat(out)
	return errA == nil && errB == nil && os.SameFile(a, b)
}

// inTheWay tells whether something that is not an empty folder is at out.
func inTheWay(out string) bool {
	info, err := os.Stat(out)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return true
	}
	entries, err := os.ReadDir(out)
	return err != nil || len(entries) > 0
}

func level(o Options) string {
	if o.Level < 1 || o.Level > 22 {
		return "19"
	}
	return strconv.Itoa(o.Level)
}

func hasFlag(list []string, w string) bool {
	for _, x := range list {
		if x == w {
			return true
		}
	}
	return false
}

// plan is the work shared by all the plans: the destination, what is in the way, the room.
func plan(items []Item, o Options, fs FS, out func(Item) string, args func(it Item, target string) []string, fat32 bool) []Plan {
	plans := make([]Plan, len(items))
	committed := map[string]uint64{}
	for i, it := range items {
		p := Plan{Item: it, Out: out(it)}
		if conflicts(it.Path, p.Out) {
			p.Block = "same_as_source"
			plans[i] = p
			continue
		}
		if inTheWay(p.Out) {
			p.Replace = []string{p.Out}
			p.Tmp = p.Out + ".maxpack-tmp"
		}
		target := p.Out
		if p.Tmp != "" {
			target = p.Tmp
		}
		p.Args = args(it, target)
		if fat32 {
			if kind, err := fs.Type(o.Dest); err == nil && strings.EqualFold(kind, "fat32") && it.OriginalSize > fat32Max {
				p.Warnings = append(p.Warnings, "fat32")
				if o.ContinueFat32 {
					p.Args = append(p.Args, "--max-file-size", strconv.FormatUint(fat32Max, 10))
				} else {
					p.Block = "fat32_too_big"
				}
			}
		}
		if p.Block == "" {
			root := fs.Root(o.Dest)
			if free, err := fs.Free(o.Dest); err == nil {
				left := uint64(0)
				if free > committed[root] {
					left = free - committed[root]
				}
				switch {
				case it.OriginalSize > left && len(p.Replace) > 0:
					p.Block = "no_space"
				case it.OriginalSize > left:
					p.Warnings = append(p.Warnings, "may_not_fit")
				}
			}
			if p.Block == "" {
				committed[root] += it.OriginalSize
			}
		}
		plans[i] = p
	}
	// two games to the same place: the second would meet the first
	for i := range plans {
		for j := range plans {
			if i != j && same(plans[i].Out, plans[j].Out) && !hasFlag(plans[i].Warnings, "duplicate_target") {
				plans[i].Warnings = append(plans[i].Warnings, "duplicate_target")
			}
		}
	}
	return plans
}

// PlanCompress plans the compression of games to .p5z or to folders (o.Mode).
func PlanCompress(items []Item, o Options, fs FS) []Plan {
	out := func(it Item) string {
		if o.Mode == ModeFolder {
			return consoleOut(it, o, fs)
		}
		return filepath.Join(o.Dest, P5zName(it))
	}
	args := func(it Item, target string) []string {
		kind := "--p5z"
		if o.Mode == ModeFolder {
			kind = "--folder"
		}
		a := []string{"pack", it.Path, "-o", target, kind, "--level", level(o)}
		if o.Threads > 0 {
			a = append(a, "--threads", strconv.Itoa(o.Threads))
		}
		if !it.HasZstdRuntime && o.Runtime != "" {
			a = append(a, "--runtime", o.Runtime)
		}
		return a
	}
	plans := plan(items, o, fs, out, args, o.Mode == ModeP5z)
	for i := range plans {
		if !plans[i].Item.HasZstdRuntime && o.Runtime == "" {
			plans[i].Warnings = append(plans[i].Warnings, "needs_runtime")
		}
	}
	return plans
}

// PlanDecompress plans the unpacking of .p5z files: the original dump (ModeOriginal) or a
// folder for the console (ModeConsole).
func PlanDecompress(items []Item, o Options, fs FS) []Plan {
	out := func(it Item) string {
		if o.Mode == ModeConsole {
			return consoleOut(it, o, fs)
		}
		return filepath.Join(o.Dest, folderName(it))
	}
	args := func(it Item, target string) []string {
		kind := "--original"
		if o.Mode == ModeConsole {
			kind = "--folder"
		}
		a := []string{"unpack", it.Path, kind, "-o", target}
		if o.Threads > 0 {
			a = append(a, "--threads", strconv.Itoa(o.Threads))
		}
		return a
	}
	return plan(items, o, fs, out, args, false)
}

// PlanVerify plans the checking of .p5z files or folders; nothing is written.
func PlanVerify(items []Item) []Plan {
	plans := make([]Plan, len(items))
	for i, it := range items {
		plans[i] = Plan{Item: it, Args: []string{"verify", it.Path}}
	}
	return plans
}
