// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package fakecore

import (
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"
)

// niceness is the nice value of this process: Linux gives getpriority as 20 less it, the others
// as it is.
func niceness() int {
	value, err := unix.Getpriority(unix.PRIO_PROCESS, 0)
	if err != nil {
		return -100
	}
	if runtime.GOOS == "linux" {
		return 20 - value
	}
	return value
}

func priority() string { return "nice " + strconv.Itoa(niceness()) }

// NormalPriority is what the engine says it runs at without low priority: the nice of the test,
// which an engine it starts takes.
func NormalPriority() string { return priority() }

// LowPriority is what the engine says it runs at with low priority: 10 more than the test, 19 at
// most.
func LowPriority() string { return "nice " + strconv.Itoa(min(19, niceness()+10)) }
