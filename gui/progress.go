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
		e.Size = sizeOn(e.Out)
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
