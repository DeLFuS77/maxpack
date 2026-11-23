// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package core

import "os/exec"

// hide has nothing to do: only Windows opens a console for a program that has none.
func hide(command *exec.Cmd) *exec.Cmd { return command }
