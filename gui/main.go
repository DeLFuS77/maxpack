// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
)

// version is set by the build of a release
var version = "dev"

func main() {
	args := os.Args[1:]
	// the installer of Windows, putting MaxPack in the Explorer or taking it away (see shell.go)
	if len(args) > 0 && args[0] == "--installer" {
		os.Exit(runInstaller(args[1:], os.Stdout, os.Stderr))
	}
	// the window, with the files it is given. Started from the Explorer, the console Windows
	// opened for it is closed
	if start, ok := windowLaunch(args); ok {
		system.HideConsole()
		os.Exit(runWindow(start))
	}
	os.Exit(forward(args))
}

// launch is what the window opens with: the files it was started with, and the space the
// Explorer asked for them ("" for none: each goes where it fits, see spaceOf).
type launch struct {
	files   []string
	section string
}

// menuSections are the spaces the menu of the Explorer asks for (see internal/explorer).
var menuSections = []string{"compress", "verify", "details"}

// windowLaunch tells whether the arguments open the window, and with what: nothing; only files or
// folders there, dropped on MaxPack or sent to it with Send to; --open and files, a .p5z opened
// with a double click, which shows in Details; or --section, a space of the menu of the Explorer
// and files. The files of the Explorer open the window even when one of them is gone meanwhile.
// Any other arguments are for maxpack-core, as before: it has no --open, --section or --installer.
func windowLaunch(args []string) (launch, bool) {
	switch {
	case len(args) == 0:
		return launch{}, true
	case onlyPaths(args):
		return launch{files: args}, true
	case args[0] == "--open" && len(args) > 1:
		return launch{files: existing(args[1:]), section: "details"}, true
	case args[0] == "--section" && len(args) > 2 && slices.Contains(menuSections, args[1]):
		return launch{files: existing(args[2:]), section: args[1]}, true
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

// existing are the paths there, none an option.
func existing(paths []string) []string {
	there := []string{}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil && !strings.HasPrefix(path, "-") {
			there = append(there, path)
		}
	}
	return there
}

// absolute makes the paths given to another MaxPack whole, from the folder it was started in, so
// the window that takes them finds them: not the options, nor the space after --section.
func absolute(args []string, folder string) []string {
	whole := slices.Clone(args)
	for i, arg := range whole {
		if folder == "" || filepath.IsAbs(arg) || strings.HasPrefix(arg, "-") || i == 1 && whole[0] == "--section" {
			continue
		}
		whole[i] = filepath.Join(folder, arg)
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
