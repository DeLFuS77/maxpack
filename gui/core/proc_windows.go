// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package core

import (
	"os"
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the engine gets no console of its own, so none flashes
// when MaxPack was started from the Explorer.
const createNoWindow = 0x08000000

// belowNormalPriority is BELOW_NORMAL_PRIORITY_CLASS: the engine runs below the other programs,
// and so do the threads of zstd it makes.
const belowNormalPriority = 0x00004000

// hide keeps a console window from showing for the engine started by command.
func hide(command *exec.Cmd) *exec.Cmd {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return command
}

// engineCommand is the command that runs the engine with args, below normal with low: Windows
// starts it so, before it makes any thread. Nothing is left to do once it started.
func engineCommand(core string, args []string, low bool) (*exec.Cmd, func(*os.Process)) {
	command := hide(exec.Command(core, args...))
	if low {
		command.SysProcAttr.CreationFlags |= belowNormalPriority
	}
	return command, nil
}
