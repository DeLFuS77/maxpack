// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows && !darwin && !linux

package system

import "runtime"

// OSVersion is the name of the system, for a report.
func OSVersion() string { return runtime.GOOS }

// TotalMemory is 0: the memory of the PC is not known here.
func TotalMemory() uint64 { return 0 }
