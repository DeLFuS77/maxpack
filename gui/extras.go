// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"sync"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// extras are what MaxPack does around a queue, apart so the tests can stand in for them.
type extras struct {
	Awake   func() func()                          // keeps the PC awake; the function it gives lets it sleep
	Taskbar func(state string, done, total uint64) // "normal", "error" or "none"
	Notify  func(title, body string)
}

// the progress of a queue is told in this many parts for each game
const taskbarPart = 1000

// run does the plans with the Runner and the extras: the PC stays awake meanwhile, the taskbar
// shows the progress of the whole queue (never going back), and one notice tells the summary
// at the end.
func (a *App) run(ctx context.Context, plans []jobs.Plan, o jobs.Options) jobs.Summary {
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
	release := x.Awake()
	defer release()

	total := uint64(len(plans)) * taskbarPart
	var mutex sync.Mutex
	var shown uint64
	show := func(done uint64) {
		mutex.Lock()
		defer mutex.Unlock()
		if done > total {
			done = total
		}
		if done < shown {
			return
		}
		shown = done
		x.Taskbar("normal", done, total)
	}
	show(0)

	path := a.core
	if path == "" {
		path, _ = core.Locate()
	}
	runner := &jobs.Runner{
		Core: path,
		OnEvent: func(i int, e core.Event) {
			if e.Total > 0 {
				show(uint64(i)*taskbarPart + min(e.Done, e.Total)*taskbarPart/e.Total)
			}
			if a.onEvent != nil {
				a.onEvent(i, e)
			}
		},
		OnItem: func(i int, state, detail string) {
			switch state {
			case jobs.StateOK, jobs.StateError, jobs.StateSkipped, jobs.StateCancelled:
				show(uint64(i+1) * taskbarPart)
			}
			if a.onItem != nil {
				a.onItem(i, state, detail)
			}
		},
	}
	sum := runner.Run(ctx, plans, o)

	if sum.Failed > 0 {
		x.Taskbar("error", total, total)
	} else {
		x.Taskbar("none", 0, 0)
	}
	lang := ""
	if a.lang != nil {
		lang = a.lang()
	} else if s, err := settings.Load(); err == nil {
		lang = s.Lang
	}
	x.Notify(noticeText(lang, sum))
	return sum
}
