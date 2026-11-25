// SPDX-License-Identifier: GPL-3.0-or-later

package core

import (
	"os"
	"os/exec"
)

// Command is the command that runs another program of MaxPack (maxpack-par2) as the engine runs:
// with no console of its own on Windows and, with low, below the other programs (see RunOptions).
// started, when not nil, is to be called with the process once it started.
func Command(program string, args []string, low bool) (command *exec.Cmd, started func(*os.Process)) {
	return engineCommand(program, args, low)
}
