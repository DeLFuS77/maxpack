// SPDX-License-Identifier: GPL-3.0-or-later

package main

// What the window keeps in the folder of its settings for a queue: the journal of what is written
// apart (unfinished.json), so that what a crash leaves is offered when MaxPack opens again, and
// the log of the originals removed once checked (maxpack.log).

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

const (
	journalName = "unfinished.json"
	logName     = "maxpack.log"
)

// inSettings is a file of the folder of the settings, "" when there is none.
func inSettings(name string) string {
	folder, err := settings.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(folder, name)
}

// logFile is where the window writes what it removed.
func logFile() string { return inSettings(logName) }

// journal is the journal of what a queue writes apart, nil when there is no folder for it.
func (a *App) journal() *jobs.Leftovers {
	file := inSettings(journalName)
	if file == "" {
		return nil
	}
	return &jobs.Leftovers{File: file}
}

// log writes a line in the log of the window, with when.
func (a *App) log(line string) {
	file := logFile()
	if file == "" {
		return
	}
	os.MkdirAll(filepath.Dir(file), 0o755)
	out, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer out.Close()
	out.WriteString(time.Now().Format("2006-01-02 15:04:05") + "  " + line + "\n")
}

// Leftovers gives what a queue cut short (a crash, a power cut, a USB pulled out) left apart and
// is there now, with what it takes: the page offers to take it away as MaxPack opens. None while a
// queue runs: what it writes apart is its own.
func (a *App) Leftovers() []jobs.Leftover {
	journal := a.journal()
	if journal == nil || a.working() {
		return []jobs.Leftover{}
	}
	return journal.Found()
}

// CleanLeftovers takes away what Leftovers gives, and gives what could not be taken away (with
// why). Only what MaxPack wrote apart is ever taken: names that end in .maxpack-tmp or .maxpack-old
// that its journal has.
func (a *App) CleanLeftovers() []string {
	journal := a.journal()
	if journal == nil {
		return []string{}
	}
	if a.working() {
		return []string{errors.New("a job is running").Error()}
	}
	failed := journal.Clean()
	for _, path := range failed {
		a.log("Could not take away what a queue left: " + path)
	}
	return failed
}
