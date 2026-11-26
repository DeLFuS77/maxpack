// SPDX-License-Identifier: GPL-3.0-or-later

package main

// Sending a .p5z to MaxPack for the console: the console of Settings (its IP address and pairing code), its
// drives for the send dialog, and a queue of Details that sends (see jobs/send.go).

import (
	"context"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// drivesTimeout is how long the send dialog waits for the console's drives (the engine says "no answer" in
// about 11 s for a console it never reaches).
const drivesTimeout = 30 * time.Second

// discardTimeout is how long Delete what arrived may take: the drives, up to 75 s while the console lets go of a
// receive, and the requests around them; the engine is never stopped during such a wait.
const discardTimeout = 5 * time.Minute

// DrivesView is what the send dialog shows: the drives of the console with their free space, or why it
// could not ask ("no_console" when Settings has no address or code, else a code of the engine: "bad_code",
// "receive_off", "unreachable"...) and the engine's words (never the code).
type DrivesView struct {
	Drives  []core.ConsoleDrive `json:"drives"`
	Error   string              `json:"error"`
	Message string              `json:"message"`
}

// ConsoleDrives asks the console of Settings which drives it has, for the send dialog: one question at a time,
// which gives up after drivesTimeout and which CancelConsoleDrives stops.
func (a *App) ConsoleDrives() DrivesView {
	saved, _ := settings.Load()
	if saved.ConsoleIP == "" || saved.ConsoleCode == "" {
		return DrivesView{Drives: []core.ConsoleDrive{}, Error: "no_console"}
	}
	engine, err := a.engine()
	if err != nil {
		return DrivesView{Drives: []core.ConsoleDrive{}, Error: "io", Message: err.Error()}
	}
	timeout := a.drivesTimeout
	if timeout == 0 {
		timeout = drivesTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	a.mutex.Lock()
	if a.drivesCancel != nil {
		a.mutex.Unlock()
		return DrivesView{Drives: []core.ConsoleDrive{}, Error: "busy_asking"}
	}
	a.drivesCancel = cancel
	a.mutex.Unlock()
	defer func() {
		a.mutex.Lock()
		a.drivesCancel = nil
		a.mutex.Unlock()
	}()
	drives, code, err := core.ConsoleDrives(ctx, engine, saved.ConsoleIP, saved.ConsoleCode)
	if err != nil {
		return DrivesView{Drives: []core.ConsoleDrive{}, Error: code, Message: err.Error()}
	}
	return DrivesView{Drives: drives}
}

// CancelConsoleDrives stops the question of ConsoleDrives at work, if any.
func (a *App) CancelConsoleDrives() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.drivesCancel != nil {
		a.drivesCancel()
	}
}

// DiscardView is what Delete what arrived on the console did: whether the console had anything of it, and what,
// or why it did nothing ("no_console", "discard_busy": a receive of it is at work there, or a code of the engine:
// "delete_failed" with the console's words in Detail, "discard_unsupported"...).
type DiscardView struct {
	Had     uint64 `json:"had"`
	Deleted bool   `json:"deleted"`
	Error   string `json:"error"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

// discardWaiting is "discard:waiting": the console is letting go of the receive of path, and is asked again in
// that many seconds.
type discardWaiting struct {
	Path    string `json:"path"`
	Seconds int    `json:"seconds"`
}

// DiscardOnConsole deletes what the console of Settings received of the .p5z at path on drive ("" for the
// engine's choice): what a send stopped or cancelled left there. The page asks first, and is told while the
// console lets go of a receive of it (discard:waiting).
func (a *App) DiscardOnConsole(path, drive string) DiscardView {
	saved, _ := settings.Load()
	if saved.ConsoleIP == "" || saved.ConsoleCode == "" {
		return DiscardView{Error: "no_console"}
	}
	engine, err := a.engine()
	if err != nil {
		return DiscardView{Error: "io", Message: err.Error()}
	}
	// the drives (a minute at most), the wait while the console lets go of it (75 s at most) and its requests
	ctx, cancel := context.WithTimeout(context.Background(), discardTimeout)
	defer cancel()
	done, err := core.DiscardOnConsole(ctx, engine, saved.ConsoleIP, saved.ConsoleCode, path, drive, func(seconds int) {
		a.send("discard:waiting", discardWaiting{Path: path, Seconds: seconds})
	})
	if done.Failure == "busy" {
		done.Failure = "discard_busy" // the console is at work on that very receive, not on another one
	}
	if err != nil {
		return DiscardView{Error: done.Failure, Message: err.Error(), Detail: done.Detail}
	}
	return DiscardView{Had: done.Had, Deleted: done.Deleted}
}
