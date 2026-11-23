// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
)

// The states of a game in the queue.
const (
	StateWaiting   = "waiting"
	StatePacking   = "packing"
	StateVerifying = "verifying"
	StateUnpacking = "unpacking"
	StateOK        = "ok"
	StateError     = "error"
	StateSkipped   = "skipped"
	StateCancelled = "cancelled"
)

// TopItem is a game of the ones that saved the most.
type TopItem struct {
	Name, TitleID string
	Saved         uint64
}

// Summary is what a queue did.
type Summary struct {
	Done, Failed, Skipped, Cancelled int
	Saved                            uint64
	Top                              []TopItem
	Duration                         time.Duration
}

// Runner runs the plans one at a time. OnEvent gets the progress of the engine for the game
// i of the plans; OnItem, each change of its state, and with an error or a skip, why.
type Runner struct {
	Core    string
	OnEvent func(i int, e core.Event)
	OnItem  func(i int, state, detail string)
}

func (r *Runner) item(i int, state, detail string) {
	if r.OnItem != nil {
		r.OnItem(i, state, detail)
	}
}

// engine runs one command of the engine; ok is whether it said so.
func (r *Runner) engine(ctx context.Context, i int, args []string) (result core.Event, ok bool, failure string) {
	result, code, err := core.Run(ctx, r.Core, args, func(e core.Event) {
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

// Run does the plans in order: a game is packed and then verified (or unpacked, or only
// verified, as o.Mode says), and the next one begins whatever happened to it. A plan with a
// Block is skipped. With Replace the new one is made in Tmp, verified, and only then put in
// place of the old one, which stays whole if anything fails or is cancelled.
func (r *Runner) Run(ctx context.Context, plans []Plan, o Options) Summary {
	started := time.Now()
	var sum Summary
	for i := range plans {
		r.item(i, StateWaiting, "")
	}
	for i, p := range plans {
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
		saved, state, detail := r.one(ctx, i, p, o)
		r.item(i, state, detail)
		switch state {
		case StateOK:
			sum.Done++
			sum.Saved += saved
			if saved > 0 {
				sum.Top = append(sum.Top, TopItem{Name: p.Item.Name, TitleID: p.Item.TitleID, Saved: saved})
			}
		case StateCancelled:
			sum.Cancelled++
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

// one does a plan; it gives what it saved, the state it ended in and why if it was not ok.
func (r *Runner) one(ctx context.Context, i int, p Plan, o Options) (saved uint64, state, detail string) {
	if o.Mode == ModeVerify {
		r.item(i, StateVerifying, "")
		_, ok, failure := r.engine(ctx, i, p.Args)
		return 0, endState(ok, failure), failure
	}
	// the drive may be gone (a USB taken out): say so instead of making its folders again
	if info, err := os.Stat(o.Dest); err != nil || !info.IsDir() {
		return 0, StateError, "dest_missing"
	}
	target := p.Out
	if p.Tmp != "" {
		target = p.Tmp
		os.RemoveAll(p.Tmp)
	}
	// what this run made and has to take away if it does not end well; with no Replace the
	// engine already does it for its own failures, but not for a verification that fails
	made := false
	discard := func() {
		if made || p.Tmp != "" {
			os.RemoveAll(target)
		}
	}
	if err := os.MkdirAll(filepath.Dir(p.Out), 0o755); err != nil {
		return 0, StateError, "io: " + err.Error()
	}

	working, compress := StateUnpacking, o.Mode == ModeP5z || o.Mode == ModeFolder
	if compress {
		working = StatePacking
	}
	r.item(i, working, "")
	result, ok, failure := r.engine(ctx, i, p.Args)
	if !ok {
		discard()
		return 0, endState(false, failure), failure
	}
	made = true
	if compress {
		r.item(i, StateVerifying, "")
		_, ok, failure = r.engine(ctx, i, []string{"verify", target})
		if !ok {
			discard()
			if failure != "cancelled" {
				failure = "verify_failed: " + failure
			}
			return 0, endState(false, failure), failure
		}
		if original, output := number(result, "original_size"), number(result, "output_size"); original > output {
			saved = original - output
		}
	}
	if ctx.Err() != nil {
		discard()
		return 0, StateCancelled, "cancelled"
	}
	if p.Tmp != "" {
		leftover, err := swap(p.Out, p.Tmp)
		if err != nil {
			os.RemoveAll(p.Tmp)
			return 0, StateError, "swap_failed: " + err.Error()
		}
		if leftover {
			return saved, StateOK, "old_not_deleted"
		}
	}
	return saved, StateOK, ""
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
