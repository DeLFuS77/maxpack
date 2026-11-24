// SPDX-License-Identifier: GPL-3.0-or-later

package core

// niceOf is the nice value of what getpriority gives: Linux gives it as 20 less it.
func niceOf(priority int) int { return 20 - priority }
