// SPDX-License-Identifier: GPL-3.0-or-later

package main

// Compare the levels on this PC (Compress) and Measure this PC (Settings): one at a time, never
// while a queue runs, told to the page as they go and cancelled with CancelTool.

import (
	"context"
	"errors"

	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// toolProgress is "tool:progress": how far the comparison or the measure is.
type toolProgress struct {
	Done  uint64 `json:"done"`
	Total uint64 `json:"total"`
}

// MeasureResult is what Measure this PC found, and the threads Settings keeps now (0: all).
type MeasureResult struct {
	jobs.Measured
	Threads int
}

// errBusy: a queue, or another comparison or measure, is at work.
var errBusy = errors.New("busy: a queue or a measure is at work")

// startTool takes the place of the one tool that may run: its context, and done to call at its end.
func (a *App) startTool() (context.Context, func(), error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.queue != nil || a.toolCancel != nil {
		return nil, nil, errBusy
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	a.toolCancel = cancel
	return ctx, func() {
		a.mutex.Lock()
		a.toolCancel = nil
		a.mutex.Unlock()
		cancel(nil)
	}, nil
}

// toolRunning tells whether a comparison or a measure is at work.
func (a *App) toolRunning() bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.toolCancel != nil
}

// CancelTool stops the comparison or the measure at work; it ends with the error "cancelled".
func (a *App) CancelTool() { a.stopTool(nil) }

// stopTool cancels the tool at work, if any, with why (core.ErrClosing as the window closes: the
// engine is then never killed, and a measure takes its temporary file away itself).
func (a *App) stopTool(why error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.toolCancel != nil {
		a.toolCancel(why)
	}
}

func (a *App) toolStep(done, total uint64) {
	a.send("tool:progress", toolProgress{Done: done, Total: total})
}

// CompareLevels compares the levels of a game on this PC: the engine packs a sample of it at levels
// 3, 9, 15 and 19 with the threads of Settings, and writes nothing. The page is told each level as
// it is measured ("tool:level") and the progress ("tool:progress").
func (a *App) CompareLevels(it jobs.Item) (jobs.Levels, error) {
	engine, err := a.engine()
	if err != nil {
		return jobs.Levels{}, err
	}
	ctx, done, err := a.startTool()
	if err != nil {
		return jobs.Levels{}, err
	}
	defer done()
	saved, _ := settings.Load()
	return jobs.CompareLevels(ctx, engine, it, saved.Threads, func(row jobs.LevelRow) { a.send("tool:level", row) }, a.toolStep)
}

// MeasurePC measures this PC: how fast it packs at 25, 50, 75 and 100 % of its threads, at the level
// of Settings, and the disk of folder (the destination; none with ""). The threads it recommends
// are kept in Settings, for Compress: 0 (all of them) when it recommends them all. The page is told
// each count tried ("tool:trial") and the progress ("tool:progress").
func (a *App) MeasurePC(folder string) (MeasureResult, error) {
	engine, err := a.engine()
	if err != nil {
		return MeasureResult{}, err
	}
	ctx, done, err := a.startTool()
	if err != nil {
		return MeasureResult{}, err
	}
	defer done()
	saved, _ := settings.Load()
	m, err := jobs.Measure(ctx, engine, folder, saved.Level, func(t jobs.Trial) { a.send("tool:trial", t) }, a.toolStep)
	if err != nil {
		return MeasureResult{}, err
	}
	// read again: the page may have changed other settings meanwhile
	saved, _ = settings.Load()
	saved.Threads = m.ThreadsToKeep()
	if err := settings.Save(saved); err != nil {
		return MeasureResult{}, err
	}
	return MeasureResult{Measured: m, Threads: saved.Threads}, nil
}
