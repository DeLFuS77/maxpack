// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package core

import (
	"os"
	"syscall"
)

// Suspend stops every thread of a process until Resume (SIGSTOP): for maxpack-par2, which cannot be told to
// pause.
func Suspend(p *os.Process) error { return p.Signal(syscall.SIGSTOP) }

// Resume lets a process that Suspend stopped go on (SIGCONT).
func Resume(p *os.Process) error { return p.Signal(syscall.SIGCONT) }
