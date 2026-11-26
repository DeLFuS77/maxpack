// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"math"
	"sync"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/par2"
	"github.com/DeLFuS77/maxpack/gui/jobs"
)

// itemEvent is "job:item": game i of the queue is in state now, and why when it failed or was
// skipped. Once done, out is what it made and size what that takes; a folder for the console
// says whether it is in homebrew/ at the root of its drive, where ShadowMountPlus sees it; a .p5z
// that got repair data (made with it, or added to it), what its set takes.
type itemEvent struct {
	I        int    `json:"i"`
	State    string `json:"state"`
	Detail   string `json:"detail"`
	Out      string `json:"out,omitempty"`
	Size     uint64 `json:"size,omitempty"`
	Homebrew bool   `json:"homebrew,omitempty"`
	Repair   uint64 `json:"repair,omitempty"`
}

// progressEvent is "job:progress": how far the step of game i is (its phase, from the engine),
// and the speed of the engine in bytes a second.
type progressEvent struct {
	I     int    `json:"i"`
	Phase string `json:"phase"`
	Done  uint64 `json:"done"`
	Total uint64 `json:"total"`
	Rate  uint64 `json:"rate"`
}

// progressEvery is how often the page is told the progress at most: the engine may say more.
const progressEvery = 150 * time.Millisecond

// rateGap: lines of progress closer than this are not measured, the time between them says
// too little.
const rateGap = 250 * time.Millisecond

// rater is the speed of the engine, in bytes a second, from its progress: the bytes done
// between two lines over the time between them, smoothed so that one slow or fast line does not
// make it jump. A new game or step counts its bytes from 0 again: a new measure starts, and the
// speed is kept meanwhile.
type rater struct {
	i       int
	phase   string
	done    uint64
	at      time.Time
	rate    float64
	started bool
}

func (r *rater) add(i int, phase string, done uint64, now time.Time) uint64 {
	switch {
	case !r.started || i != r.i || phase != r.phase || done < r.done:
		r.i, r.phase, r.done, r.at, r.started = i, phase, done, now, true
	case now.Sub(r.at) >= rateGap:
		measured := float64(done-r.done) / now.Sub(r.at).Seconds()
		if r.rate == 0 {
			r.rate = measured
		} else {
			r.rate = 0.7*r.rate + 0.3*measured
		}
		r.done, r.at = done, now
	}
	return uint64(math.Round(r.rate))
}

// teller tells the page how a queue goes: each state of a game, and its progress with the speed,
// at most every progressEvery and always at the end of a step.
type teller struct {
	app    *App
	plans  *jobs.Queue
	mode   jobs.Mode
	repair int
	drives jobs.FS
	mutex  sync.Mutex
	speed  rater
	sent   time.Time
}

// writesOut tells whether a queue of a mode makes something of its own (not only checks, repairs or
// adds repair data to what is there).
func writesOut(mode jobs.Mode) bool {
	return mode != jobs.ModeVerify && mode != jobs.ModeRepair && mode != jobs.ModeProtect && mode != jobs.ModeAddDLC &&
		mode != jobs.ModeUpdate && mode != jobs.ModeSend
}

// setSize is what the repair data of a .p5z takes, 0 when it has none of its own.
func setSize(p5z string) uint64 {
	var size uint64
	if par2.Inspect(p5z).State == par2.StateMatch {
		for _, file := range par2.Files(p5z) {
			size += jobs.SizeOf(file)
		}
	}
	return size
}

func (t *teller) item(i int, state, detail string) {
	e := itemEvent{I: i, State: state, Detail: detail}
	if p, ok := t.plans.Plan(i); ok && state == jobs.StateOK {
		if writesOut(t.mode) {
			e.Out = p.Out
			e.Size = jobs.SizeOf(e.Out)
			if t.mode == jobs.ModeFolder || t.mode == jobs.ModeConsole {
				e.Homebrew = inHomebrew(e.Out, t.drives)
			}
		}
		switch {
		case t.mode == jobs.ModeP5z && t.repair > 0:
			e.Repair = setSize(p.Out)
		case t.mode == jobs.ModeProtect:
			e.Repair = setSize(p.Item.Path)
		}
	}
	t.app.send("job:item", e)
}

func (t *teller) progress(i int, e core.Event, now time.Time) {
	t.mutex.Lock()
	rate := t.speed.add(i, e.Phase, e.Done, now)
	due := now.Sub(t.sent) >= progressEvery || e.Done >= e.Total
	if due {
		t.sent = now
	}
	t.mutex.Unlock()
	if due {
		t.app.send("job:progress", progressEvent{I: i, Phase: e.Phase, Done: e.Done, Total: e.Total, Rate: rate})
	}
}

// share is how far a queue is, from 0 to 1, counted as the page counts it for its bars (stepsOf,
// stepShare, rowShare and progressOf in frontend/app.js): each game weighs its size as many times
// as it has steps; the step at work goes by the bytes of its phases one after the other (copying
// the loose files, then packing the rest) over the size of the game, or over what the
// verification reads; a game that ended counts whole. What it gives never goes back.
type share struct {
	rows  []shareRow
	shown float64
}

// add counts games added to the queue (Append), at its end.
func (s *share) add(plans []jobs.Plan, mode jobs.Mode, repair int) {
	for _, p := range plans {
		s.rows = append(s.rows, shareRow{size: p.Item.OriginalSize, steps: stepsOf(mode, repair)})
	}
}

// remove stops counting game i, taken out of the queue before its turn.
func (s *share) remove(i int) {
	if i >= 0 && i < len(s.rows) {
		s.rows[i].removed = true
	}
}

type shareRow struct {
	size                uint64
	steps               []string
	step, phase         string
	offset, done, total uint64
	ended, removed      bool
}

// stepsOf are the steps of a game in a queue of a mode, as the Runner does them: what is made is
// verified before it takes its name, but the original dump, which the engine checks file by file
// as it writes it; a .p5z with repair data gets it once it is in its place; a .p5z is verified
// before it gets repair data, and after it is repaired.
func stepsOf(mode jobs.Mode, repair int) []string {
	switch mode {
	case jobs.ModeP5z:
		if repair > 0 {
			return []string{jobs.StatePacking, jobs.StateVerifying, jobs.StateProtecting}
		}
		return []string{jobs.StatePacking, jobs.StateVerifying}
	case jobs.ModeFolder:
		return []string{jobs.StatePacking, jobs.StateVerifying}
	case jobs.ModeVerify:
		return []string{jobs.StateVerifying}
	case jobs.ModeProtect:
		return []string{jobs.StateVerifying, jobs.StateProtecting}
	case jobs.ModeRepair:
		return []string{jobs.StateRepairing, jobs.StateVerifying}
	case jobs.ModeConsole:
		return []string{jobs.StateUnpacking, jobs.StateVerifying}
	case jobs.ModeAddDLC, jobs.ModeUpdate:
		// it grows in its place, is verified, and gets its repair data again when it had them
		if repair > 0 {
			return []string{jobs.StateGrowing, jobs.StateVerifying, jobs.StateProtecting}
		}
		return []string{jobs.StateGrowing, jobs.StateVerifying}
	case jobs.ModeSend:
		// the bytes go to the console, which then verifies them
		return []string{jobs.StateSending, jobs.StateVerifying}
	}
	return []string{jobs.StateUnpacking}
}

func newShare(plans []jobs.Plan, mode jobs.Mode, repair int) *share {
	s := &share{rows: make([]shareRow, len(plans))}
	for i, p := range plans {
		s.rows[i] = shareRow{size: p.Item.OriginalSize, steps: stepsOf(mode, repair)}
	}
	return s
}

// item is a state of game i, from the Runner: a new step starts from nothing, and an end counts
// it whole.
func (s *share) item(i int, state string) {
	if i < 0 || i >= len(s.rows) {
		return
	}
	r := &s.rows[i]
	switch state {
	case jobs.StatePacking, jobs.StateVerifying, jobs.StateUnpacking, jobs.StateProtecting, jobs.StateRepairing, jobs.StateGrowing:
		if state != r.step {
			r.step, r.phase, r.offset, r.done, r.total = state, "", 0, 0, 0
		}
	case jobs.StateOK, jobs.StateError, jobs.StateSkipped, jobs.StateCancelled:
		r.ended = true
	}
}

// progress is a line of progress of the engine for game i: a new phase of the same step counts
// the bytes of the one before as done.
func (s *share) progress(i int, phase string, done, total uint64) {
	if i < 0 || i >= len(s.rows) || total == 0 {
		return
	}
	r := &s.rows[i]
	if r.phase != "" && phase != r.phase {
		r.offset += r.total
	}
	r.phase, r.done, r.total = phase, done, total
}

func (r shareRow) stepShare() float64 {
	if r.total == 0 {
		return 0
	}
	// packing and unpacking go through the game's size; the other steps through the bytes of the
	// file they read, which their own progress says
	whole := r.offset + r.total
	if (r.step == jobs.StatePacking || r.step == jobs.StateUnpacking) && r.size > whole {
		whole = r.size
	}
	return math.Min(1, float64(r.offset+r.done)/float64(whole))
}

func (r shareRow) share() float64 {
	if r.ended {
		return 1
	}
	for index, step := range r.steps {
		if step == r.step {
			return (float64(index) + r.stepShare()) / float64(len(r.steps))
		}
	}
	return 0
}

// value is how far the queue is.
func (s *share) value() float64 {
	var whole, done float64
	for _, r := range s.rows {
		if r.removed {
			continue
		}
		weight := float64(len(r.steps)) * float64(max(1, r.size))
		whole += weight
		done += r.share() * weight
	}
	if whole > 0 {
		s.shown = math.Max(s.shown, done/whole)
	}
	return s.shown
}
