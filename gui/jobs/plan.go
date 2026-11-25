// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/par2"
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
	ModeRepair   Mode = "repair"   // repair a .p5z with its repair data (PAR2), then check it
	ModeProtect  Mode = "protect"  // check a .p5z, then add repair data to it
)

// fat32Max is the most a file may have on FAT32 (4 GiB less one byte).
const fat32Max = 1<<32 - 1

// Item is a game the user gave. ContentID is the one of its param.json ("EP0001-PPSA10001_00-..."),
// which tells its region; "" when it is not known. Kind is what the engine says it is: "folder",
// "p5z" or an image ("exfat", "ufs2", "pfs", or "pfsc" for a PFS container); Inner, for a container,
// the image that holds the game, and Compressed, whether reading it decompresses PFSC blocks.
// OriginalSize is the size of the game, which for a compressed image is more than its file takes;
// ImageSize, what the file of an image takes on disk (0 for a folder or a .p5z).
type Item struct {
	Path, TitleID, Name, Version, Kind string
	Inner                              string
	Compressed                         bool
	ContentID                          string
	OriginalSize                       uint64
	ImageSize                          uint64
	HasZstdRuntime                     bool
	Packed                             bool
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
	// Originals is what becomes of a game once its .p5z or folder is made, verified and in its
	// place, only when compressing: OriginalsKeep, OriginalsTrash or OriginalsDelete
	Originals string
	// OriginalsNoBin: with OriginalsTrash, the originals that cannot go to the recycle bin (their
	// drive has none, or they do not fit in it) are deleted for good; else they stay
	OriginalsNoBin bool
	// LowPriority runs the engine below the other programs (see core.RunOptions)
	LowPriority bool
	// NameTemplate names a .p5z and an unpacked original (see NameOf); "" is DefaultTemplate. A
	// folder for the console is named by its TITLE_ID whatever it says
	NameTemplate string
	// Repair is the repair data each .p5z gets once it is made, verified and in its place
	// (ModeP5z), or that is added to each one (ModeProtect), as a percentage of it: 1, 5 or 10; 0 is
	// none. A folder never has any
	Repair int
}

// What becomes of the original of a game once its new one is checked (Options.Originals).
const (
	OriginalsKeep   = ""
	OriginalsTrash  = "trash"  // to the recycle bin of the system
	OriginalsDelete = "delete" // deleted for good
)

// The names of what a queue writes apart: the new one while it is made and verified, and the old
// one it replaces while the new one takes its place. Both end with the game; one a crash leaves
// is found at the next start (see Leftovers).
const (
	TmpSuffix = ".maxpack-tmp"
	OldSuffix = ".maxpack-old"
)

// Plan is what will be done with one game: where it goes, what it replaces, what to tell the
// user and, if it cannot be done, why not. Block is "same_as_source", "no_space" or
// "fat32_too_big" ("" when it can go). Warnings are "fat32", "needs_runtime" (only for a folder),
// "may_not_fit", "duplicate_target" and "no_title_id". A Plan with Replace is one the user accepted
// to replace: not going on with it is a matter of dropping the plan.
type Plan struct {
	Item Item
	Out  string
	// Tmp is where the new one is made, <Out>.maxpack-tmp, hidden on Windows: only once it is
	// verified does it take the name Out (or, with Replace, the place of the old one)
	Tmp      string
	Replace  []string // what exists in the way, and goes when the new one is done and checked
	Warnings []string
	Block    string
	Args     []string // the arguments of the engine, without the ones for the progress
	// Need is the most the game may take on the drive it is written to (or repaired on), which the
	// space of a queue adds up: its original size (the game's, not what its image takes), with its
	// repair data when it gets some
	Need uint64
	// KeepOriginal keeps the original of this game whatever Options.Originals says: a game added
	// to a queue that runs, whose original the user said to keep
	KeepOriginal bool
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
		Kind: text("kind"), Inner: text("inner"), Compressed: flag("compressed"), OriginalSize: number("original_size"),
		HasZstdRuntime: flag("has_zstd_runtime"), Packed: flag("packed"), ContentID: text("content_id"),
	}
	if it.Name == "" {
		it.Name = baseName(path)
	}
	if IsImage(it.Kind) {
		it.ImageSize = fileSize(path)
	}
	// the engine says the content ID of every game (1.0.2 on); with one of before, a dump folder
	// has it in its param.json
	if it.ContentID == "" && it.Kind == "folder" {
		it.ContentID = paramContentID(path)
	}
	return it
}

// SizeBefore is what a game took before it was compressed, which is what the queue counts what it
// saved against: an image's file on disk (as it is now, else as it was when it was read), else the
// game itself. An image whose file is not known gives 0: nothing it saved is claimed, as no saving
// may be said that the disk did not get back. The room a game needs is still its OriginalSize.
func (it Item) SizeBefore() uint64 {
	if !IsImage(it.Kind) {
		return it.OriginalSize
	}
	if size := fileSize(it.Path); size > 0 {
		return size
	}
	return it.ImageSize
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
		p := Plan{Item: it, Out: out(it), Need: it.OriginalSize}
		if o.Mode == ModeP5z && o.Repair > 0 {
			p.Need += par2.SetSize(it.OriginalSize, o.Repair)
		}
		if conflicts(it.Path, p.Out) {
			p.Block = "same_as_source"
			plans[i] = p
			continue
		}
		// every game is written apart, and takes its name only once verified
		p.Tmp = p.Out + TmpSuffix
		if inTheWay(p.Out) {
			p.Replace = []string{p.Out}
		}
		p.Args = args(it, p.Tmp)
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
				case p.Need > left && len(p.Replace) > 0:
					p.Block = "no_space"
				case p.Need > left:
					p.Warnings = append(p.Warnings, "may_not_fit")
				}
			}
			if p.Block == "" {
				committed[root] += p.Need
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
		return filepath.Join(o.Dest, P5zNameAs(it, o.NameTemplate))
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
		a = append(a, core.PasscodeArgs()...)
		if !it.HasZstdRuntime && o.Runtime != "" {
			a = append(a, "--runtime", o.Runtime)
		}
		return a
	}
	plans := plan(items, o, fs, out, args, o.Mode == ModeP5z)
	for i := range plans {
		// a folder plays with the runtime in its fakelib/; a .p5z is given ampr-emu-ng by MaxPack
		// for the console when it plays it, so it never needs one
		if o.Mode == ModeFolder && !plans[i].Item.HasZstdRuntime && o.Runtime == "" {
			plans[i].Warnings = append(plans[i].Warnings, "needs_runtime")
		}
		// the installer of the PS5 refuses a .p5z with no TITLE_ID
		if o.Mode == ModeP5z && plans[i].Item.TitleID == "" && plans[i].Block == "" {
			plans[i].Warnings = append(plans[i].Warnings, "no_title_id")
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
		return filepath.Join(o.Dest, folderName(it, o.NameTemplate))
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
