// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package system

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
)

// preventSleep keeps the computer from going to sleep until the function it gives is called:
// caffeinate on macOS, systemd-inhibit on Linux, when there. Both end with MaxPack too, if it
// ends without calling it.
func preventSleep() func() {
	var command *exec.Cmd
	if runtime.GOOS == "darwin" {
		command = exec.Command("caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	} else {
		command = exec.Command("systemd-inhibit", "--what=sleep:idle", "--who=MaxPack", "--why=Files at work", "--mode=block", "tail", "--pid="+strconv.Itoa(os.Getpid()), "-f", "/dev/null")
	}
	if command.Start() != nil {
		return func() {}
	}
	return func() {
		command.Process.Kill()
		command.Wait()
	}
}
