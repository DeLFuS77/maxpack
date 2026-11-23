// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package core

import (
	"os/exec"
	"testing"
)

func TestHideSetsNoWindow(t *testing.T) {
	command := hide(exec.Command("x"))
	if command.SysProcAttr == nil || command.SysProcAttr.CreationFlags&createNoWindow == 0 || !command.SysProcAttr.HideWindow {
		t.Fatalf("falta CREATE_NO_WINDOW: %+v", command.SysProcAttr)
	}
}
