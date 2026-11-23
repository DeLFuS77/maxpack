// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package core

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the engine gets no console of its own, so none flashes
// when MaxPack was started from the Explorer.
const createNoWindow = 0x08000000

// hide keeps a console window from showing for the engine started by command.
func hide(command *exec.Cmd) *exec.Cmd {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return command
}
