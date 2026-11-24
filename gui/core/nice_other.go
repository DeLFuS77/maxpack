// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows && !linux

package core

// niceOf is the nice value of what getpriority gives, which is the nice value itself here.
func niceOf(priority int) int { return priority }
