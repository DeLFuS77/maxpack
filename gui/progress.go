// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"math"
	"sync"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/jobs"
)

// itemEvent is "job:item": game i of the queue is in state now, and why when it failed or was
// skipped. Once done, out is what it made and size what that takes; a folder for the console
// says whether it is in homebrew/ at the root of its drive, where ShadowMountPlus sees it.
type itemEvent struct {
	I        int    `json:"i"`
	State    string `json:"state"`
	Detail   string `json:"detail"`
	Out      string `json:"out,omitempty"`
	Size     uint64 `json:"size,omitempty"`
	Homebrew bool   `json:"homebrew,omitempty"`
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
	plans  []jobs.Plan
	mode   jobs.Mode
	drives jobs.FS
	mutex  sync.Mutex
	speed  rater
	sent   time.Time
}

func (t *teller) item(i int, state, detail string) {
	e := itemEvent{I: i, State: state, Detail: detail}
	if state == jobs.StateOK && t.mode != jobs.ModeVerify && i < len(t.plans) {
		e.Out = t.plans[i].Out
		e.Size = jobs.SizeOf(e.Out)
		if t.mode == jobs.ModeFolder || t.mode == jobs.ModeConsole {
			e.Homebrew = inHomebrew(e.Out, t.drives)
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

type shareRow struct {
	size                uint64
	steps               []string
	step, phase         string
	offset, done, total uint64
	ended               bool
}

// stepsOf are the steps of a game in a queue of a mode, as the Runner does them: what is made is
// verified before it takes its name, but the original dump, which the engine checks file by file
// as it writes it.
func stepsOf(mode jobs.Mode) []string {
	switch mode {
	case jobs.ModeP5z, jobs.ModeFolder:
		return []string{jobs.StatePacking, jobs.StateVerifying}
	case jobs.ModeVerify:
		return []string{jobs.StateVerifying}
	case jobs.ModeConsole:
		return []string{jobs.StateUnpacking, jobs.StateVerifying}
	}
	return []string{jobs.StateUnpacking}
}

func newShare(plans []jobs.Plan, mode jobs.Mode) *share {
	s := &share{rows: make([]shareRow, len(plans))}
	for i, p := range plans {
		s.rows[i] = shareRow{size: p.Item.OriginalSize, steps: stepsOf(mode)}
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
	case jobs.StatePacking, jobs.StateVerifying, jobs.StateUnpacking:
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
	whole := r.offset + r.total
	if r.step != jobs.StateVerifying && r.size > whole {
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
		weight := float64(len(r.steps)) * float64(max(1, r.size))
		whole += weight
		done += r.share() * weight
	}
	if whole > 0 {
		s.shown = math.Max(s.shown, done/whole)
	}
	return s.shown
}
