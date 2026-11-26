// SPDX-License-Identifier: GPL-3.0-or-later

package main

// Sending a .p5z to MaxPack for the console: the console of Settings (its IP address and pairing code), its
// drives for the send dialog, and a queue of Details that sends (see jobs/send.go).

import (
	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// DrivesView is what the send dialog shows: the drives of the console with their free space, or why it
// could not ask ("no_console" when Settings has no address or code, else a code of the engine: "bad_code",
// "receive_off", "unreachable"...) and the engine's words (never the code).
type DrivesView struct {
	Drives  []core.ConsoleDrive `json:"drives"`
	Error   string              `json:"error"`
	Message string              `json:"message"`
}

// ConsoleDrives asks the console of Settings which drives it has, for the send dialog.
func (a *App) ConsoleDrives() DrivesView {
	saved, _ := settings.Load()
	if saved.ConsoleIP == "" || saved.ConsoleCode == "" {
		return DrivesView{Drives: []core.ConsoleDrive{}, Error: "no_console"}
	}
	engine, err := a.engine()
	if err != nil {
		return DrivesView{Drives: []core.ConsoleDrive{}, Error: "io", Message: err.Error()}
	}
	drives, code, err := core.ConsoleDrives(engine, saved.ConsoleIP, saved.ConsoleCode)
	if err != nil {
		return DrivesView{Drives: []core.ConsoleDrive{}, Error: code, Message: err.Error()}
	}
	return DrivesView{Drives: drives}
}
