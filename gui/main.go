// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
)

// version is set by the build of a release
var version = "dev"

func main() {
	args := os.Args[1:]
	// the window, with the files it is given. Started from the Explorer, the console Windows
	// opened for it is closed
	if start, ok := windowLaunch(args); ok {
		system.HideConsole()
		os.Exit(runWindow(start))
	}
	os.Exit(forward(args))
}

// launch is what the window opens with: the files it was started with.
type launch struct {
	files []string
}

// windowLaunch tells whether the arguments open the window: none, or only files or folders there,
// dropped on MaxPack or sent to it from the Explorer. Any other arguments are for maxpack-core.
func windowLaunch(args []string) (launch, bool) {
	if len(args) == 0 {
		return launch{}, true
	}
	if onlyPaths(args) {
		return launch{files: args}, true
	}
	return launch{}, false
}

// onlyPaths tells whether every argument is a file or folder there, none an option.
func onlyPaths(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return false
		}
		if _, err := os.Stat(arg); err != nil {
			return false
		}
	}
	return len(args) > 0
}

// absolute makes the paths given to another MaxPack whole, from the folder it was started in, so
// the window that takes them finds them.
func absolute(args []string, folder string) []string {
	whole := make([]string, len(args))
	for i, arg := range args {
		whole[i] = arg
		if folder != "" && !filepath.IsAbs(arg) && !strings.HasPrefix(arg, "-") {
			whole[i] = filepath.Join(folder, arg)
		}
	}
	return whole
}

// forward runs maxpack-core with the arguments, with the input, output and errors of this
// process, and gives its exit code.
func forward(args []string) int {
	path, err := core.Locate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "maxpack:", err)
		return 1
	}
	command := exec.Command(path, args...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "maxpack:", err)
		return 1
	}
	return 0
}
