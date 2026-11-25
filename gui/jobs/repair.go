// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

// The repair data of a .p5z: a PAR2 set next to it (see internal/par2), made by maxpack-par2 once
// the .p5z is made, verified and in its place (ModeP5z), or for a .p5z that is there already and
// verifies (ModeProtect); and the repair of a damaged .p5z with it (ModeRepair).
//
// A set is made in <name>.p5z.maxpack-par2, a folder of its own, and its files take their names next
// to the .p5z only once par2 made them all: nothing half made is ever next to it.
//
// A repair never lets par2 near the .p5z where it is: the .p5z is moved (renamed, on its own drive)
// into <name>.p5z.maxpack-repair under the name its set has for it, and par2 repairs it there, as it
// does: the damaged one becomes <name>.1 and the repaired one is written as <name>. The engine
// verifies the repaired one before it takes the place of the damaged one, which only then goes. If
// par2 cannot repair it, or what it repaired does not verify, or the repair is cancelled, the .p5z
// goes back as it was. The folder is in the journal, with a file that says how far the repair went,
// so what a crash leaves is offered at the next start (see Leftovers).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/par2"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
)

// The names of what the repair data writes apart: the folder where a set is made, and the one
// where a .p5z is repaired, with the file that says how far that went.
const (
	Par2Suffix   = ".maxpack-par2"
	RepairSuffix = ".maxpack-repair"
	repairState  = "maxpack-repair.json"
)

// The steps of a repair, as its file says them.
const (
	stepMoving   = "moving"   // the .p5z is being moved into the folder of the repair
	stepMoved    = "moved"    // it is there; par2 works on it
	stepVerified = "verified" // the repaired one verified, and is about to take its place
	stepPlaced   = "placed"   // it took its place: what is left is the damaged one
)

type repairStep struct {
	Name string `json:"name"`
	Step string `json:"step"`
}

func writeStep(work string, s repairStep) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	temp := filepath.Join(work, repairState+".tmp")
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, filepath.Join(work, repairState))
}

func readStep(work string) (repairStep, bool) {
	var s repairStep
	data, err := os.ReadFile(filepath.Join(work, repairState))
	if err != nil || json.Unmarshal(data, &s) != nil || s.Name == "" || strings.ContainsAny(s.Name, `/\`) {
		return repairStep{}, false
	}
	return s, true
}

// repairMargin is the room a repair asks for past a whole copy of the .p5z.
func repairMargin(size uint64) uint64 { return size/100 + 64<<20 }

func fileSize(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	return uint64(info.Size())
}

func isP5z(it Item) bool {
	return it.Kind == "p5z" && strings.EqualFold(filepath.Ext(it.Path), ".p5z")
}

// PlanProtect plans adding repair data (o.Repair) to .p5z files that are there: each one is verified
// first. Anything that is not a .p5z cannot go ("not_p5z").
func PlanProtect(items []Item, o Options, fs FS) []Plan {
	plans := make([]Plan, len(items))
	for i, it := range items {
		plans[i] = Plan{Item: it, Args: []string{"verify", it.Path}}
		if !isP5z(it) {
			plans[i].Block = "not_p5z"
			continue
		}
		plans[i].Need = par2.SetSize(fileSize(it.Path), o.Repair)
	}
	return plans
}

// PlanRepair plans repairing .p5z files with their repair data: one with no set of its own next to
// it cannot go ("no_repair_data"; "repair_data_other" with the set of another file,
// "repair_data_unreadable" with one that cannot be read). Each needs room for a whole copy of it on
// its drive.
func PlanRepair(items []Item, o Options, fs FS) []Plan {
	plans := make([]Plan, len(items))
	for i, it := range items {
		plans[i] = Plan{Item: it}
		if !isP5z(it) {
			plans[i].Block = "not_p5z"
			continue
		}
		switch par2.Inspect(it.Path).State {
		case par2.StateNone:
			plans[i].Block = "no_repair_data"
		case par2.StateOther:
			plans[i].Block = "repair_data_other"
		case par2.StateUnreadable:
			plans[i].Block = "repair_data_unreadable"
		}
		size := fileSize(it.Path)
		plans[i].Need = size + repairMargin(size)
	}
	return plans
}

// samePercent tells whether a set of a file already has the repair data asked for: the percentage it
// says, or the recovery blocks MaxPack would make.
func samePercent(info par2.Info, size uint64, percent int) bool {
	_, _, recovery := par2.Blocks(size, percent)
	return info.Percent == percent || uint64(info.Recovery) == recovery && info.BlockSize == blockSizeOf(size, percent)
}

func blockSizeOf(size uint64, percent int) uint64 {
	bs, _, _ := par2.Blocks(size, percent)
	return bs
}

// progress tells the progress of par2 for game i, as the engine tells its own: in bytes of the file.
func (r *Runner) progress(i int, phase string, size uint64) func(permille uint64) {
	return func(permille uint64) {
		if r.OnEvent != nil {
			r.OnEvent(i, core.Event{Kind: "progress", Phase: phase, Done: permille * size / 1000, Total: size})
		}
	}
}

// lastLines keeps the lines par2 writes that are not progress, for what it says at its end.
type lastLines struct{ lines []string }

func (l *lastLines) add(line string) {
	l.lines = append(l.lines, line)
	if len(l.lines) > 200 {
		l.lines = l.lines[len(l.lines)-100:]
	}
}

func (l *lastLines) last() string {
	if len(l.lines) == 0 {
		return "no output"
	}
	return l.lines[len(l.lines)-1]
}

// removeSet deletes the set of a .p5z (all the files named as its set); a note when it could not.
func removeSet(p5z string) string {
	for _, file := range par2.Files(p5z) {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "old_repair_data_not_deleted: " + file
		}
	}
	return ""
}

// protect makes the repair data of the .p5z at file (o.Repair percent) next to it, replacing a set of
// its own there; the set of another file is never touched, and the .p5z gets none ("repair_data_other").
// It gives what the set takes, or why it has none: "no_par2", "repair_data_cancelled" or
// "repair_data_failed: <what par2 said>".
func (r *Runner) protect(ctx context.Context, i int, file string, o Options) (uint64, string) {
	if r.Par2 == "" {
		return 0, "no_par2"
	}
	size := fileSize(file)
	if size == 0 {
		return 0, "repair_data_failed: the .p5z is not there"
	}
	old := par2.Inspect(file)
	switch old.State {
	case par2.StateOther:
		return 0, "repair_data_other"
	case par2.StateUnreadable:
		return 0, "repair_data_unreadable"
	}
	tmp := file + Par2Suffix
	r.keep(tmp)
	defer r.forget(tmp)
	fail := func(note string) (uint64, string) {
		if err := removeAll(tmp); err != nil {
			note += "; tmp_not_deleted: " + tmp + ": " + err.Error()
		}
		return 0, note
	}
	if err := removeAll(tmp); err != nil {
		return 0, "repair_data_failed: tmp_not_deleted: " + tmp + ": " + err.Error()
	}
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return fail("repair_data_failed: " + err.Error())
	}
	system.SetHidden(tmp, true)
	base := filepath.Base(file)
	bs, _, recovery := par2.Blocks(size, o.Repair)
	said := &lastLines{}
	meter := par2.NewMeter(par2.Making)
	meter.OnLine, meter.OnProgress = said.add, r.progress(i, "protecting", size)
	args := par2.CreateArgs(par2.Create{File: file, Index: filepath.Join(tmp, base+".par2"), BlockSize: bs, Recovery: recovery, Threads: o.Threads})
	code, err := par2.Run(ctx, r.Par2, args, o.LowPriority, meter)
	switch {
	case errors.Is(err, par2.ErrCancelled) || ctx.Err() != nil:
		return fail("repair_data_cancelled")
	case err != nil:
		return fail("repair_data_failed: " + err.Error())
	case code != 0:
		return fail("repair_data_failed: " + said.last() + " (code " + strconv.Itoa(code) + ")")
	}
	made := par2.Files(filepath.Join(tmp, base))
	if len(made) < 2 || filepath.Base(made[0]) != base+".par2" {
		return fail("repair_data_failed: par2 made no set")
	}
	// the set made takes the place of the old one of its own; the recovery files first, so a set
	// cut short there is still whole (a recovery file has every packet the index has)
	if old.State == par2.StateMatch {
		if note := removeSet(file); note != "" {
			return fail("repair_data_failed: " + note)
		}
	}
	order := append(append([]string{}, made[1:]...), made[0])
	var placed []string
	var total uint64
	for _, from := range order {
		to := filepath.Join(filepath.Dir(file), filepath.Base(from))
		total += fileSize(from)
		if err := renameNew(from, to); err != nil {
			for _, done := range placed {
				os.Remove(done)
			}
			if errors.Is(err, os.ErrExist) {
				return fail("repair_data_failed: exists: " + to)
			}
			return fail("repair_data_failed: " + err.Error())
		}
		placed = append(placed, to)
	}
	if err := removeAll(tmp); err != nil {
		return total, "tmp_not_deleted: " + tmp + ": " + err.Error()
	}
	return total, ""
}

// protectOne adds repair data to a .p5z that is there: a set of its own of the same percentage is
// left as it is ("repair_data_same"), and the set of another file too ("repair_data_other"); else the
// engine verifies the .p5z first, and only a good one gets a set.
func (r *Runner) protectOne(ctx context.Context, i int, p Plan, o Options) outcome {
	file := p.Item.Path
	fail := func(state, detail string) outcome { return outcome{state: state, detail: detail} }
	if r.Par2 == "" {
		return fail(StateError, "no_par2")
	}
	switch info := par2.Inspect(file); info.State {
	case par2.StateOther:
		return fail(StateSkipped, "repair_data_other")
	case par2.StateUnreadable:
		return fail(StateSkipped, "repair_data_unreadable")
	case par2.StateMatch:
		if samePercent(info, fileSize(file), o.Repair) {
			return fail(StateSkipped, "repair_data_same")
		}
	}
	r.item(i, StateVerifying, "")
	if _, ok, failure := r.engine(ctx, i, []string{"verify", file}, o, nil); !ok {
		if failure == "cancelled" {
			return fail(StateCancelled, failure)
		}
		return fail(StateError, "verify_failed: "+failure)
	}
	r.item(i, StateProtecting, "")
	made, note := r.protect(ctx, i, file, o)
	switch {
	case note == "repair_data_cancelled":
		return fail(StateCancelled, "cancelled")
	case made == 0:
		return fail(StateError, note)
	}
	return outcome{state: StateOK, detail: note, protected: made, protect: true}
}

// repairOne repairs a .p5z with its repair data (see the top of this file). It ends "repaired", or
// "repair_not_needed" when par2 finds every block of it as it was made.
func (r *Runner) repairOne(ctx context.Context, i int, p Plan, o Options) outcome {
	file := p.Item.Path
	fail := func(state, detail string) outcome { return outcome{state: state, detail: detail} }
	if r.Par2 == "" {
		return fail(StateError, "no_par2")
	}
	info := par2.Inspect(file)
	switch info.State {
	case par2.StateNone:
		return fail(StateError, "no_repair_data")
	case par2.StateOther:
		return fail(StateError, "repair_data_other")
	case par2.StateUnreadable:
		return fail(StateError, "repair_data_unreadable")
	}
	work := file + RepairSuffix
	if _, err := os.Lstat(work); err == nil {
		return fail(StateError, "repair_folder_exists: "+work)
	}
	size := fileSize(file)
	if free, err := system.FreeSpace(filepath.Dir(file)); err == nil && free < size+repairMargin(size) {
		return fail(StateError, "no_space")
	}
	r.keep(work)
	if err := os.Mkdir(work, 0o755); err != nil {
		r.forget(work)
		return fail(StateError, "io: "+err.Error())
	}
	step := repairStep{Name: info.Name, Step: stepMoving}
	target := filepath.Join(work, info.Name)
	if err := writeStep(work, step); err != nil {
		removeAll(work)
		r.forget(work)
		return fail(StateError, "io: "+err.Error())
	}
	if err := os.Rename(file, target); err != nil {
		removeAll(work)
		r.forget(work)
		if inUse(err) {
			return fail(StateError, "in_use")
		}
		return fail(StateError, "io: "+err.Error())
	}
	step.Step = stepMoved
	writeStep(work, step)
	// back: the .p5z goes back as it was, and the folder with what par2 wrote
	back := func(state, detail string) outcome {
		if note := r.restore(work, file, info.Name); note != "" {
			detail += "; " + note
		}
		return fail(state, detail)
	}

	r.item(i, StateRepairing, "")
	index := info.Index
	if index == "" {
		index = info.Volumes[0]
	}
	said := &lastLines{}
	meter := par2.NewMeter(par2.Repairing)
	meter.OnLine, meter.OnProgress = said.add, r.progress(i, "repairing", size)
	code, err := par2.Run(ctx, r.Par2, par2.RepairArgs(par2.Repair{Index: index, Folder: work, Threads: o.Threads}), o.LowPriority, meter)
	result := par2.Outcome(said.lines)
	switch {
	case errors.Is(err, par2.ErrCancelled) || ctx.Err() != nil:
		return back(StateCancelled, "cancelled")
	case err != nil:
		return back(StateError, "repair_failed: "+err.Error())
	case result.NotPossible || code == par2.ExitNotPossible:
		return back(StateError, "repair_not_possible: "+strconv.Itoa(result.Missing))
	case code == 0 && result.Complete:
		// every block is as it was made: it goes back as it is, and is verified there
		if note := r.restore(work, file, info.Name); note != "" {
			return fail(StateError, note)
		}
		r.item(i, StateVerifying, "")
		if _, ok, failure := r.engine(ctx, i, []string{"verify", file}, o, nil); !ok {
			return fail(endState(false, failure), "verify_failed: "+failure)
		}
		return outcome{state: StateOK, detail: "repair_not_needed"}
	case code != 0 || !result.Repaired:
		return back(StateError, "repair_failed: "+said.last()+" (code "+strconv.Itoa(code)+")")
	}

	// what par2 repaired is verified by the engine before it takes the place of the damaged one
	r.item(i, StateVerifying, "")
	if _, ok, failure := r.engine(ctx, i, []string{"verify", target}, o, nil); !ok {
		if failure == "cancelled" {
			return back(StateCancelled, failure)
		}
		return back(StateError, "repair_verify_failed: "+failure)
	}
	step.Step = stepVerified
	writeStep(work, step)
	if err := renameNew(target, file); err != nil {
		// something came to its place meanwhile: the folder stays, with both, for the user
		return fail(StateError, "repair_place_failed: "+work)
	}
	step.Step = stepPlaced
	writeStep(work, step)
	if err := removeAll(work); err != nil {
		return outcome{state: StateOK, detail: "repaired; repair_backup_not_deleted: " + work}
	}
	r.forget(work)
	return outcome{state: StateOK, detail: "repaired"}
}

// restore puts a .p5z back as it was from the folder of its repair: par2 keeps the damaged one as
// <name>.1 once it starts to write the repaired one; before that it is <name>. What par2 wrote goes,
// and the folder. It gives a note when the .p5z could not go back (the folder then stays, in the
// journal, for the next start to offer).
func (r *Runner) restore(work, file, name string) string {
	original := filepath.Join(work, name)
	if _, err := os.Lstat(original + ".1"); err == nil {
		original += ".1"
	}
	if err := renameNew(original, file); err != nil {
		return "repair_restore_failed: " + work
	}
	if err := removeAll(work); err != nil {
		return "tmp_not_deleted: " + work + ": " + err.Error()
	}
	r.forget(work)
	return ""
}
