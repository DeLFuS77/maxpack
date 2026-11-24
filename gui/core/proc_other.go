// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package core

import (
	"os"
	"os/exec"
	"syscall"
)

// hide has nothing to do: only Windows opens a console for a program that has none.
func hide(command *exec.Cmd) *exec.Cmd { return command }

// lowNice is how much nicer the engine runs with low priority (19 at most, as nice takes it).
const lowNice = 10

// engineCommand is the command that runs the engine with args. With low it goes through nice,
// which takes the engine's place (the same process), so every thread it makes is nicer: on
// Linux a thread takes the nice of the one that makes it, and setting it after the start could
// miss the first ones. Without nice, the engine is made nicer as soon as it starts.
func engineCommand(core string, args []string, low bool) (*exec.Cmd, func(*os.Process)) {
	if !low {
		return exec.Command(core, args...), nil
	}
	if nice, err := exec.LookPath("nice"); err == nil {
		return exec.Command(nice, append([]string{"-n", "10", core}, args...)...), nil
	}
	return exec.Command(core, args...), func(process *os.Process) {
		now, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
		if err != nil {
			return
		}
		syscall.Setpriority(syscall.PRIO_PROCESS, process.Pid, min(19, niceOf(now)+lowNice))
	}
}
