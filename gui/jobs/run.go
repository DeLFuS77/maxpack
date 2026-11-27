// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/par2"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/internal/watch"
)

// removeAll is how leftovers are deleted (a variable so the tests can make it fail).
var removeAll = os.RemoveAll

// lstat and readDir are how an output is looked at (variables so the tests can make them fail).
var (
	lstat   = os.Lstat
	readDir = os.ReadDir
)

// The states of a game in the queue.
const (
	StateWaiting   = "waiting"
	StateQueued    = "queued" // added to a queue that runs, waiting for its turn
	StatePacking   = "packing"
	StateVerifying = "verifying"
	StateUnpacking = "unpacking"
	// making the repair data of a .p5z, and repairing one with it (see repair.go)
	StateProtecting = "protecting"
	StateRepairing  = "repairing"
	// adding to a .p5z (DLC, or what a new dump changes; see grow.go)
	StateGrowing = "growing"
	// sending a .p5z to the console (see send.go); then verifying while the console verifies it
	StateSending   = "sending"
	StateOK        = "ok"
	StateError     = "error"
	StateSkipped   = "skipped"
	StateCancelled = "cancelled"
)

// TopItem is a game of the ones that saved the most (see Summary for what Saved is against).
type TopItem struct {
	Name, TitleID string
	Saved         uint64
}

// Summary is what a queue did. Saved is what the games done take less than before: a dump folder
// against the game, an image against its file (Item.SizeBefore). NoZstdRuntime are the folders done
// that will not start until given
// a libSceAmpr.sprx that reads zstd (they have none, or one that does not); a .p5z is never one.
// Trashed, Deleted and Kept are the originals that went to the recycle bin, were deleted for good
// or stayed because their drive has no bin (with Options.Originals), and TrashedSize and
// DeletedSize what they take. Protected are the games done that got repair data, ProtectedSize what
// it takes, and Unprotected the ones done that did not get the repair data asked for (their .p5z is
// good: why is in their detail); Repaired, the .p5z repaired.
type Summary struct {
	Done, Failed, Skipped, Cancelled int
	NoZstdRuntime                    int
	Saved                            uint64
	Top                              []TopItem
	Duration                         time.Duration
	Trashed, Deleted, Kept           int
	TrashedSize, DeletedSize         uint64
	Protected, Unprotected           int
	ProtectedSize                    uint64
	Repaired                         int
}

// Journal keeps what a queue writes apart while it works (the new one of a game while it is made,
// and an old one it replaced that could not be deleted), so what a crash or a power cut leaves is
// found at the next start (see Leftovers).
type Journal interface {
	Add(path string)
	Remove(path string)
}

// Runner runs the plans one at a time. OnEvent gets the progress of the engine (and of par2) for the
// game i of the plans; OnItem, each change of its state, and with an error or a skip, why. Journal,
// when there is one, is told what is written apart; Log gets a line for each original removed or
// kept. Par2 is maxpack-par2, "" when there is none (then no repair data is made). Pause, when there is
// one, is the pause of the queue: while it is on, the engine at work pauses, maxpack-par2 is suspended,
// and no game, step or run of either starts.
type Runner struct {
	Core    string
	Par2    string
	OnEvent func(i int, e core.Event)
	OnItem  func(i int, state, detail string)
	Journal Journal
	Log     func(line string)
	Pause   *core.Pause
	// Redo decides again, when its turn comes and right before the swap, whether a game the watched folder brought
	// may be made again in place of its .p5z (Plan.ReplaceIf): "" yes, else why not (the window's record of what
	// it made, see watch.go). Without it, no such game is ever replaced
	Redo func(p Plan) string
}

// redoMadeOf is why what was made of p (its .p5z apart) may not replace the .p5z there, "" when it may: both say
// (info --json) they are the game and the version planned. An unknown version is never the same.
func (r *Runner) redoMadeOf(p Plan) string {
	made, errMade := Describe(r.Core, p.Tmp)
	old, errOld := Describe(r.Core, p.Out)
	switch {
	case errMade != nil || errOld != nil || made.Kind != "p5z" || old.Kind != "p5z" || made.TitleID == "" ||
		!strings.EqualFold(made.TitleID, old.TitleID) || !strings.EqualFold(made.TitleID, p.Item.TitleID):
		return "watch_redo_refused"
	case made.Version == "" || old.Version == "" || made.Version != old.Version || made.Version != p.Item.Version:
		if skip := VersionSkip(old.Version, made.Version); skip != "watch_exists" {
			return skip
		}
		return "watch_redo_refused"
	}
	return ""
}

// putAside is what becomes of the .p5z a redo replaced, set apart at <out>.maxpack-old: under a dated name
// (<out>.maxpack-old-<date>), to the recycle bin; where there is none, it stays beside it under that name. Never
// deleted for good. It gives the note of the row.
func (r *Runner) putAside(out string) string {
	old := out + OldSuffix
	dated := old + "-" + time.Now().Format("20060102-150405")
	if err := os.Rename(old, dated); err != nil {
		r.keep(old)
		return "old_not_deleted: " + old
	}
	r.forget(old)
	if err := moveToTrash(dated); err == nil {
		r.log("The .p5z made before, to the recycle bin: %s", dated)
		return "watch_old_in_bin"
	}
	r.log("The .p5z made before, kept beside it (no recycle bin there): %s", dated)
	return "watch_old_kept: " + dated
}

// redoRefused is why the .p5z the watched folder made of p may not be replaced now, "" when it may: the window's
// record says so, and the file there is still that one.
func (r *Runner) redoRefused(p Plan) string {
	if r.Redo == nil || p.ReplaceIf == nil || !p.ReplaceIf.Matches(p.Out) {
		return "watch_redo_refused"
	}
	return r.Redo(p)
}

// hold waits while the queue is paused, before anything starts; an error once ctx ended.
func (r *Runner) hold(ctx context.Context) error {
	if err := r.Pause.Wait(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *Runner) item(i int, state, detail string) {
	if r.OnItem != nil {
		r.OnItem(i, state, detail)
	}
}

func (r *Runner) log(format string, args ...any) {
	if r.Log != nil {
		r.Log(fmt.Sprintf(format, args...))
	}
}

// keep tells the journal a path written apart, and forget that it is gone.
func (r *Runner) keep(path string) {
	if r.Journal != nil {
		r.Journal.Add(path)
	}
}

func (r *Runner) forget(path string) {
	if _, err := lstat(path); r.Journal != nil && errors.Is(err, fs.ErrNotExist) {
		r.Journal.Remove(path)
	}
}

// engine runs one command of the engine; ok is whether it said so. onEvent, when given, sees
// each line first.
func (r *Runner) engine(ctx context.Context, i int, args []string, o Options, onEvent func(core.Event)) (result core.Event, ok bool, failure string) {
	if r.hold(ctx) != nil {
		return result, false, "cancelled"
	}
	options := core.RunOptions{Low: o.LowPriority, Pause: r.Pause}
	if len(args) > 0 && args[0] == "send" && o.ConsoleCode != "" {
		options.Env = []string{SendCodeEnv + "=" + o.ConsoleCode}
	}
	result, code, err := core.RunWith(ctx, r.Core, args, options, func(e core.Event) {
		if onEvent != nil {
			onEvent(e)
		}
		if r.OnEvent != nil {
			r.OnEvent(i, e)
		}
	})
	if err != nil {
		return result, false, err.Error()
	}
	if okay, _ := result.Raw["ok"].(bool); okay && code == 0 {
		return result, true, ""
	}
	if code == 130 || ctx.Err() != nil {
		return result, false, "cancelled"
	}
	if e, _ := result.Raw["error"].(string); e != "" {
		return result, false, e
	}
	if bad, _ := result.Raw["files_bad"].(float64); bad > 0 {
		return result, false, fmt.Sprintf("files_bad:%d", int(bad))
	}
	return result, false, fmt.Sprintf("exit_%d", code)
}

func number(e core.Event, key string) uint64 {
	n, _ := e.Raw[key].(float64)
	return uint64(n)
}

// outcome is how a game ended: its state, why, what it saved and what became of its original.
type outcome struct {
	saved         uint64
	state, detail string
	original      originalFate
	// the repair data made for it (what it takes), and whether some was asked for
	protected uint64
	protect   bool
}

// Run does the plans in order: a game is packed and then verified (or unpacked, or only
// verified, as o.Mode says), and the next one begins whatever happened to it. A plan with a
// Block is skipped. Every game is made apart, in Tmp, verified, and only then takes its name (with
// Replace, the place of the old one, which stays whole if anything fails or is cancelled). With
// o.Originals, the original of a game compressed goes once its new one is in its place.
func (r *Runner) Run(ctx context.Context, plans []Plan, o Options) Summary {
	return r.RunQueue(ctx, NewQueue(plans), o)
}

// RunQueue is Run with a queue that may take more games while it runs (see Queue): each one goes
// as the ones that were there, and one taken out before its turn is neither done nor counted.
func (r *Runner) RunQueue(ctx context.Context, q *Queue, o Options) Summary {
	started := time.Now()
	var sum Summary
	for i, n := 0, q.Len(); i < n; i++ {
		r.item(i, StateWaiting, "")
	}
	for {
		// the next game waits while paused; cancelled meanwhile, it is cancelled as the others. With none left the
		// queue ends now, paused or not: nothing would be resumed
		i, p, more, wait := q.take(r.Pause.On() && ctx.Err() == nil)
		if wait {
			r.hold(ctx)
			continue
		}
		if !more {
			break
		}
		switch {
		case p.Block != "":
			sum.Skipped++
			r.item(i, StateSkipped, p.Block)
			continue
		case ctx.Err() != nil:
			sum.Cancelled++
			r.item(i, StateCancelled, "")
			continue
		}
		end := r.one(ctx, i, p, o)
		r.item(i, end.state, end.detail)
		switch end.state {
		case StateOK:
			sum.Done++
			if hasPart(end.detail, "runtime_without_zstd") {
				sum.NoZstdRuntime++
			}
			sum.Saved += end.saved
			if end.saved > 0 {
				sum.Top = append(sum.Top, TopItem{Name: p.Item.Name, TitleID: p.Item.TitleID, Saved: end.saved})
			}
			switch end.original.how {
			case originalTrashed:
				sum.Trashed++
				sum.TrashedSize += end.original.size
			case originalDeleted:
				sum.Deleted++
				sum.DeletedSize += end.original.size
			case originalKept:
				sum.Kept++
			}
			switch {
			case end.protected > 0:
				sum.Protected++
				sum.ProtectedSize += end.protected
			case end.protect:
				sum.Unprotected++
			}
			if o.Mode == ModeRepair && hasPart(end.detail, "repaired") {
				sum.Repaired++
			}
		case StateCancelled:
			sum.Cancelled++
		case StateSkipped:
			sum.Skipped++
		default:
			sum.Failed++
		}
	}
	sort.SliceStable(sum.Top, func(a, b int) bool { return sum.Top[a].Saved > sum.Top[b].Saved })
	if len(sum.Top) > 3 {
		sum.Top = sum.Top[:3]
	}
	sum.Duration = time.Since(started)
	return sum
}

// writesFolder tells whether the output of a mode is a folder (else it is a .p5z).
func writesFolder(mode Mode) bool { return mode != ModeP5z }

// one does a plan.
func (r *Runner) one(ctx context.Context, i int, p Plan, o Options) outcome {
	fail := func(state, detail string) outcome { return outcome{state: state, detail: detail} }
	switch o.Mode {
	case ModeVerify:
		r.item(i, StateVerifying, "")
		_, ok, failure := r.engine(ctx, i, p.Args, o, nil)
		return fail(endState(ok, failure), failure)
	case ModeRepair:
		return r.repairOne(ctx, i, p, o)
	case ModeProtect:
		return r.protectOne(ctx, i, p, o)
	case ModeAddDLC, ModeUpdate:
		return r.growOne(ctx, i, p, o)
	case ModeSend:
		return r.sendOne(ctx, i, p, o)
	}
	// the drive may be gone (a USB taken out): say so instead of making its folders again
	if info, err := os.Stat(o.Dest); err != nil || !info.IsDir() {
		return fail(StateError, "dest_missing")
	}
	// the .p5z the watched folder made of it, made again: only while the window's record still says so (fail
	// closed), decided now before anything is made, and again before the swap
	redo := len(p.Replace) > 0 && p.ReplaceIf != nil
	var before string
	var beforeBusy bool
	if redo {
		if why := r.redoRefused(p); why != "" {
			return fail(StateSkipped, why)
		}
		// the source must still be what was handed over and planned: else (a copy that went on meanwhile, another
		// version written over it) it is not compressed now, and comes again as it is then
		if before, beforeBusy, _ = watch.Signature(p.Item.Path); beforeBusy || p.HandedSig == "" || before != p.HandedSig {
			r.log("Not made again now, it changed since it was planned: %s", p.Item.Path)
			return fail(StateSkipped, "watch_redo_changed")
		}
	}
	if p.Tmp == "" {
		p.Tmp = p.Out + TmpSuffix
	}
	r.keep(p.Tmp)
	defer r.forget(p.Tmp)
	// a leftover of an earlier run: if it cannot go, say so, not "exists" from the engine
	if err := removeAll(p.Tmp); err != nil {
		return fail(StateError, "tmp_not_deleted: "+p.Tmp+": "+err.Error())
	}
	// anything that does not end well takes away what was written apart, whoever wrote it (the
	// engine for its own failures, but not when it is killed, nor for a verification that fails):
	// what is at Out is never touched. When it cannot be taken away, why goes with the detail
	discard := func(detail string) string {
		if err := removeAll(p.Tmp); err != nil {
			return detail + "; tmp_not_deleted: " + p.Tmp + ": " + err.Error()
		}
		return detail
	}
	if err := os.MkdirAll(filepath.Dir(p.Out), 0o755); err != nil {
		return fail(StateError, "io: "+err.Error())
	}
	// what is made is hidden until it is verified and has its name: a folder is made here (the
	// engine writes in an empty one), a .p5z as soon as the engine tells its first progress
	hidden := false
	if writesFolder(o.Mode) {
		if err := os.Mkdir(p.Tmp, 0o755); err != nil {
			return fail(StateError, "io: "+err.Error())
		}
		hidden = system.SetHidden(p.Tmp, true) == nil
	}
	hide := func(core.Event) {
		if !hidden {
			hidden = system.SetHidden(p.Tmp, true) == nil
		}
	}

	working, compress := StateUnpacking, o.Mode == ModeP5z || o.Mode == ModeFolder
	if compress {
		working = StatePacking
	}
	r.item(i, working, "")
	// what the watched folder brought: as it is now, to tell whether it changed while it was compressed
	ro := originalsFor(p, o)
	if compress && p.Item.Watched && !redo {
		before, beforeBusy, _ = watch.Signature(p.Item.Path)
	}
	result, ok, failure := r.engine(ctx, i, p.Args, o, hide)
	if !ok {
		return fail(endState(false, failure), discard(failure))
	}
	// a folder made with a runtime that does not read zstd will not start: said, and it goes on. A
	// .p5z is not: MaxPack for the console puts its own ampr-emu-ng in when it plays one
	var notes []string
	if bad, _ := result.Raw["runtime_without_zstd"].(bool); bad && o.Mode == ModeFolder {
		notes = append(notes, "runtime_without_zstd")
	}
	// made from a game packed already: compressed again, or with its packs kept at their level
	if from, _ := result.Raw["from_packed"].(bool); from && compress {
		if kept, _ := result.Raw["kept_packs"].(bool); kept {
			notes = append(notes, fmt.Sprintf("from_packed_kept:%d", number(result, "packed_level")))
		} else {
			notes = append(notes, "from_packed")
		}
	}
	// "unpack --original" checks every file against its SHA-256 as it writes it; the others are
	// verified by the engine before they take their name
	saved := uint64(0)
	if o.Mode != ModeOriginal {
		r.item(i, StateVerifying, "")
		_, ok, failure = r.engine(ctx, i, []string{"verify", p.Tmp}, o, nil)
		if !ok {
			state := endState(false, failure)
			if failure != "cancelled" {
				failure = "verify_failed: " + failure
			}
			return fail(state, discard(failure))
		}
		// against what the game took before: an image's file, what the disk gets back (an .ffpfsc is
		// compressed, and the game in it is bigger than the file), else the game the engine read; and,
		// as what was made holds them too, the DLC that went inside it (the engine's count has them)
		if original, output := number(result, "original_size"), number(result, "output_size"); compress {
			// (the engine's is the game's files unpacked: of a game packed already, what it takes on disk too)
			if IsImage(p.Item.Kind) || p.Item.PackedAlready() {
				if original = p.Item.SizeBefore(); original > 0 {
					original += p.Item.AddDLCSize
				}
			}
			if original > output {
				saved = original - output
			}
		}
	}
	if ctx.Err() != nil {
		return fail(StateCancelled, discard("cancelled"))
	}
	// still arriving while it was compressed: what was made is not what will be there
	if compress && p.Item.Watched && sourceChanged(p, before, beforeBusy) {
		r.log("Changed while it was compressed, nothing made of it kept: %s", p.Item.Path)
		return fail(StateError, discard("source_changed"))
	}
	system.SetHidden(p.Tmp, false)
	// the .p5z the watched folder made of it is made again only while that very file is there and the window's
	// record still says so: else the .p5z there stays, and the new one goes
	var check func(old string) bool
	if redo {
		if why := r.redoRefused(p); why != "" {
			r.log("Not made again in its place, its record or the file there no longer says so: %s", p.Out)
			return fail(StateSkipped, discard(why))
		}
		// what was made, and what it would replace, as they say themselves: the game and the version planned, both
		if why := r.redoMadeOf(p); why != "" {
			r.log("Not made again in its place, what was made or what is there is another game or version: %s", p.Out)
			return fail(StateSkipped, discard(why))
		}
		check = p.ReplaceIf.Matches           // once set aside too (a rename keeps size and date)
		notes = append(notes, "watch_redone") // it changed after it was compressed: compressed again
	}
	if len(p.Replace) > 0 {
		// the repair data of the old .p5z goes with it: it protected that one
		oldSet := o.Mode == ModeP5z && par2.Inspect(p.Out).State == par2.StateMatch
		// the old one is in the journal while it is apart: a cut between the renames puts it back (Clean)
		r.keep(p.Out + OldSuffix)
		var leftover bool
		var err error
		if redo {
			err = swapKeeping(p.Out, p.Tmp, check) // the old one stays apart: it is never deleted for good
		} else {
			leftover, err = swapIf(p.Out, p.Tmp, check)
		}
		if !leftover && !redo {
			r.forget(p.Out + OldSuffix)
		}
		if errors.Is(err, errNotTheOne) {
			r.forget(p.Out + OldSuffix)
			return fail(StateSkipped, discard("watch_redo_refused"))
		}
		if err != nil {
			r.forget(p.Out + OldSuffix)
			return fail(StateError, discard("swap_failed: "+err.Error()))
		}
		if redo {
			notes = append(notes, r.putAside(p.Out))
		}
		if leftover {
			r.keep(p.Out + OldSuffix)
			notes = append(notes, "old_not_deleted: "+p.Out+OldSuffix)
		}
		if oldSet {
			if note := removeSet(p.Out); note != "" {
				notes = append(notes, note)
			} else {
				r.log("Repair data of the old one deleted with it: %s", p.Out)
			}
		}
	} else if err := place(p.Out, p.Tmp); err != nil {
		if errors.Is(err, errInTheWay) {
			return fail(StateError, discard("exists"))
		}
		return fail(StateError, discard("io: "+err.Error()))
	}
	end := outcome{saved: saved, state: StateOK}
	if compress && p.Item.Watched {
		notes = append(notes, "original_kept_watched") // the watched folder never takes an original away
	} else if compress && ro.Originals != OriginalsKeep && !p.KeepOriginal {
		end.original = r.removeOriginal(p, ro)
		if end.original.note != "" {
			notes = append(notes, end.original.note)
		}
	}
	// the repair data comes last: the .p5z is done and good whatever becomes of it
	if o.Mode == ModeP5z && o.Repair > 0 {
		end.protect = true
		r.item(i, StateProtecting, "")
		made, note := r.protect(ctx, i, p.Out, o)
		end.protected = made
		if note != "" {
			notes = append(notes, note)
		}
	}
	end.detail = strings.Join(notes, "; ")
	return end
}

// What an output is before a game starts.
const (
	outMissing = iota // nothing there
	outEmpty          // an empty folder
	outFull           // a file, or a folder with something in it (or that cannot be read)
)

func outState(path string) int {
	info, err := lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return outMissing
	case err != nil, !info.IsDir():
		return outFull // not being able to look is not "nothing there"
	}
	if entries, err := readDir(path); err == nil && len(entries) == 0 {
		return outEmpty
	}
	return outFull
}

// hasPart tells whether a detail ("code; code: path; ...") has the code.
func hasPart(detail, code string) bool {
	for _, part := range strings.Split(detail, "; ") {
		if part == code || strings.HasPrefix(part, code+": ") {
			return true
		}
	}
	return false
}

func endState(ok bool, failure string) string {
	switch {
	case ok:
		return StateOK
	case failure == "cancelled":
		return StateCancelled
	}
	return StateError
}
