// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/jobs"
)

// extras are what MaxPack does around a queue, apart so the tests can stand in for them.
type extras struct {
	Awake   func() func()                          // keeps the PC awake; the function it gives lets it sleep
	Taskbar func(state string, done, total uint64) // "normal", "paused", "error" or "none"
	// Notify shows a notice of the system, and tells whether it could
	Notify func(title, body string) bool
}

// the progress of a queue is told in this many parts
const taskbarParts = 10000

// run does the plans with the Runner and the extras: the PC stays awake meanwhile (but while the
// queue is paused), the taskbar shows the progress of the whole queue as the page counts it (see share;
// never going back) and whether it is paused, the page is told how each game goes (job:item,
// job:progress), and the end is told as endNotice says. The original of a game removed once checked is written in the log, and what a game
// writes apart in the journal of the leftovers.
func (a *App) run(ctx context.Context, plans []jobs.Plan, o jobs.Options) jobs.Summary {
	return a.runQueue(ctx, &queue{o: o, jobs: jobs.NewQueue(plans)})
}

// runQueue is run with a queue that may take more games while it runs (Append).
func (a *App) runQueue(ctx context.Context, q *queue) jobs.Summary {
	o := q.o
	x := a.extras
	if x.Awake == nil {
		x.Awake = system.KeepAwake
	}
	if x.Taskbar == nil {
		x.Taskbar = system.SetTaskbarProgress
	}
	if x.Notify == nil {
		x.Notify = a.notify
	}
	// the PC is kept awake while the queue works, not while it is paused
	var awake sync.Mutex
	release := x.Awake()
	defer func() {
		awake.Lock()
		defer awake.Unlock()
		if release != nil {
			release()
		}
	}()

	const total = taskbarParts
	q.mutex.Lock()
	q.share = newShare(q.jobs.All(), o.Mode, o.Repair)
	for i := range q.share.rows {
		if q.jobs.Removed(i) {
			q.share.remove(i)
		}
	}
	queue := q.share
	q.mutex.Unlock()
	mutex := &q.mutex
	shown, shownState, told := uint64(0), "", false
	// show tells the taskbar how far the queue is and whether it is paused, when that changed; with the
	// mutex held
	show := func() {
		done := min(total, uint64(math.Round(queue.value()*total)))
		state := "normal"
		if q.pause.On() {
			state = "paused"
		}
		if told && done == shown && state == shownState {
			return
		}
		shown, shownState, told = done, state, true
		x.Taskbar(state, done, total)
	}
	mutex.Lock()
	show()
	mutex.Unlock()

	path := a.core
	if path == "" {
		path, _ = core.Locate()
	}
	// no maxpack-par2: a game that asked for repair data says it has none, and why
	program, _ := a.par2Program()
	// the page is told each state and the progress, with the speed
	page := &teller{app: a, plans: q.jobs, mode: o.Mode, repair: o.Repair, drives: a.drives()}
	var journal jobs.Journal
	if j := a.journal(); j != nil {
		journal = j
	}
	// paused or going on again: the taskbar says it, the PC may sleep meanwhile, and the speed is measured
	// anew from where it goes on
	defer q.pause.Listen(func(bool) {
		awake.Lock()
		switch paused := q.pause.On(); {
		case paused && release != nil:
			release()
			release = nil
		case !paused && release == nil:
			release = x.Awake()
		}
		awake.Unlock()
		mutex.Lock()
		show()
		mutex.Unlock()
		page.restart()
	})()
	runner := &jobs.Runner{
		Core:    path,
		Par2:    program,
		Journal: journal,
		Log:     a.log,
		Pause:   q.pause,
		Redo:    a.redoStill,
		OnEvent: func(i int, e core.Event) {
			if e.Total > 0 {
				mutex.Lock()
				queue.progress(i, e.Phase, min(e.Done, e.Total), e.Total)
				show()
				mutex.Unlock()
				page.progress(i, e, time.Now())
			}
			if a.onEvent != nil {
				a.onEvent(i, e)
			}
		},
		OnItem: func(i int, state, detail string) {
			mutex.Lock()
			queue.item(i, state)
			show()
			mutex.Unlock()
			page.item(i, state, detail)
			if a.onItem != nil {
				a.onItem(i, state, detail)
			}
		},
	}
	sum := runner.RunQueue(ctx, q.jobs, o)

	if sum.Failed > 0 {
		x.Taskbar("error", total, total)
	} else {
		x.Taskbar("none", 0, 0)
	}
	a.endNotice(x, sum)
	return sum
}
