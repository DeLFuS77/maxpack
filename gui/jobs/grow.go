// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

// Growing a .p5z (Details › Add DLC… and Update with a new dump…): the engine appends to the .p5z
// itself what is new (maxpack add, maxpack update), so nothing is written apart: if it stops, or the
// power goes, the engine leaves the .p5z as it was (its journal, <name>.p5z.maxpack-append, undoes the
// append the next time it opens it). Once it grew, it is verified; and the repair data it had of its own
// no longer protect it, so they are made again, of the same share.

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/par2"
)

// PlanGrow plans adding the DLC of each item (Grow) to its .p5z (ModeAddDLC), or making it the new dump
// in Grow[0] (ModeUpdate). Out is the .p5z itself; Need is what is given, the most it may add. Anything
// but a .p5z cannot go ("not_p5z"), nor one with nothing given ("nothing_given"). On FAT32 the engine
// is told to stop before the .p5z would pass 4 GiB, as it is told when compressing. DLC that may not fit
// are warned of (may_not_fit): all they have is new. An update is not: what it adds is known only once
// the engine compared the new dump, and the engine then stops before writing anything if that does not
// fit (no_space).
func PlanGrow(items []Item, o Options, fs FS) []Plan {
	plans := make([]Plan, len(items))
	for i, it := range items {
		p := Plan{Item: it, Out: it.Path}
		switch {
		case !isP5z(it):
			p.Block = "not_p5z"
		case len(it.Grow) == 0:
			p.Block = "nothing_given"
		}
		if p.Block != "" {
			plans[i] = p
			continue
		}
		var a []string
		if o.Mode == ModeUpdate {
			a = []string{"update", it.Path, it.Grow[0]}
			if it.Force {
				a = append(a, "--force")
			}
			if !o.KeepLeftovers {
				a = append(a, "--leave-out-leftovers")
			}
			p.Need = SizeOf(it.Grow[0])
		} else {
			a = []string{"add", it.Path}
			for _, dlc := range it.Grow {
				a = append(a, "--dlc", dlc)
				p.Need += SizeOf(dlc)
			}
		}
		if o.Threads > 0 {
			a = append(a, "--threads", strconv.Itoa(o.Threads))
		}
		a = append(a, core.PasscodeArgs()...)
		if kind, err := fs.Type(it.Path); err == nil && strings.EqualFold(kind, "fat32") {
			a = append(a, "--max-file-size", strconv.FormatUint(fat32Max, 10))
		}
		p.Args = a
		if free, err := fs.Free(it.Path); err == nil && o.Mode != ModeUpdate && p.Need > free {
			p.Warnings = append(p.Warnings, "may_not_fit")
		}
		plans[i] = p
	}
	return plans
}

// growFailure is the detail of a refusal of the engine when growing: the codes that the page says for
// Compress in other words get words of their own here.
func growFailure(code string) string {
	switch code {
	case "no_space":
		return "grow_no_space"
	case "too_big":
		return "grow_too_big"
	case "already_packed":
		return "dump_packed"
	}
	return code
}

// growOne grows a .p5z in its place, verifies it and, when it had repair data of its own, makes them
// again. It ends "grown" (or "nothing_to_do": it had all of that already), with "repair_redone" when it
// got its repair data again.
func (r *Runner) growOne(ctx context.Context, i int, p Plan, o Options) outcome {
	fail := func(state, detail string) outcome { return outcome{state: state, detail: detail} }
	// the repair data it has now protect it as it is: the share is kept for the ones made after
	before := par2.Inspect(p.Out)
	r.item(i, StateGrowing, "")
	result, ok, failure := r.engine(ctx, i, p.Args, o, nil)
	if !ok {
		return fail(endState(false, failure), growFailure(failure)) // the engine left it as it was
	}
	nothing, _ := result.Raw["nothing"].(bool)
	notes := []string{"grown"}
	if nothing {
		notes[0] = "nothing_to_do"
	}
	r.item(i, StateVerifying, "")
	if _, ok, failure = r.engine(ctx, i, []string{"verify", p.Out}, o, nil); !ok {
		state := endState(false, failure)
		if failure != "cancelled" {
			failure = "verify_failed: " + failure
		}
		return fail(state, failure)
	}
	end := outcome{state: StateOK}
	if before.State == par2.StateMatch && !nothing {
		// they are of the .p5z as it was: they go, and the ones of what it is now take their place
		if note := removeSet(p.Out); note != "" {
			notes = append(notes, note)
		} else {
			r.log("Repair data of the .p5z as it was deleted: %s", p.Out)
		}
		end.protect = true
		r.item(i, StateProtecting, "")
		again := o
		again.Repair = before.Percent
		made, note := r.protect(ctx, i, p.Out, again)
		end.protected = made
		if note != "" {
			notes = append(notes, note)
		} else if made > 0 {
			notes = append(notes, "repair_redone")
		}
	}
	if _, err := os.Stat(p.Out); err != nil {
		return fail(StateError, "io: "+err.Error())
	}
	end.detail = strings.Join(notes, "; ")
	return end
}
