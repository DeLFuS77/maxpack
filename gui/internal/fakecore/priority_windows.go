// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package fakecore

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// priority is the priority class of this process, by name.
func priority() string {
	class, err := windows.GetPriorityClass(windows.CurrentProcess())
	switch {
	case err != nil:
		return "unknown: " + err.Error()
	case class == windows.NORMAL_PRIORITY_CLASS:
		return "normal"
	case class == windows.BELOW_NORMAL_PRIORITY_CLASS:
		return "below_normal"
	case class == windows.IDLE_PRIORITY_CLASS:
		return "idle"
	}
	return fmt.Sprintf("class 0x%X", class)
}

// NormalPriority is what the engine says it runs at without low priority: the class of the
// test, which an engine it starts takes.
func NormalPriority() string { return priority() }

// LowPriority is what the engine says it runs at with low priority: below normal.
func LowPriority() string { return "below_normal" }
