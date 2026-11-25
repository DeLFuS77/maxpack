// SPDX-License-Identifier: GPL-3.0-or-later

package main

// MaxPack in the Explorer of Windows (internal/explorer): Settings › Windows Explorer turns each
// place on and off, and the installer runs the program it copied to do the same:
//
//	MaxPack.exe --installer add [menu] [sendto] [files] [--language xx]
//	MaxPack.exe --installer remove
//
// add puts the places chosen in the Explorer for this copy, the menu in the language of the
// installer; remove takes all of MaxPack's away (only an installed MaxPack writes them, and it is
// one per user). A portable MaxPack writes nothing in Windows: it refuses both, as it already
// refuses the notices of the system (see notify.go).

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"

	"github.com/DeLFuS77/maxpack/gui/internal/explorer"
	"github.com/DeLFuS77/maxpack/gui/internal/portable"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// places is what puts MaxPack in the Explorer; tests stand in for it, so they never write the
// registry of the user.
type places struct {
	available  func() bool
	executable func() (string, error)
	read       func(executable string) explorer.State
	add        func(place, executable string, texts explorer.MenuTexts) error
	remove     func(place string) error
	relabel    func(executable string, texts explorer.MenuTexts) error
}

var shell = places{
	available: explorer.Available, executable: explorer.Executable, read: explorer.Read,
	add: explorer.Add, remove: explorer.Remove, relabel: explorer.Relabel,
}

// isPortable tells whether this copy is portable (a "settings" folder next to it).
var isPortable = func() bool { return portable.Folder() != "" }

var errPortable = errors.New("the portable MaxPack writes nothing in Windows: install MaxPack to add it to the Explorer")

// ExplorerPlaces tells Settings › Windows Explorer what is there, for this copy of MaxPack.
func (a *App) ExplorerPlaces() explorer.State {
	if !shell.available() {
		return explorer.State{}
	}
	executable, err := shell.executable()
	if err != nil {
		return explorer.State{Available: true}
	}
	return shell.read(executable)
}

// SetExplorerPlace puts a place in the Explorer (menu, sendto or files) for this copy of MaxPack,
// the menu with the texts of the page, or takes it away; and tells what is there after.
func (a *App) SetExplorerPlace(place string, on bool, texts explorer.MenuTexts) (explorer.State, error) {
	if isPortable() {
		return a.ExplorerPlaces(), errPortable
	}
	executable, err := shell.executable()
	if err != nil {
		return a.ExplorerPlaces(), err
	}
	if on {
		err = shell.add(place, executable, texts)
	} else {
		err = shell.remove(place)
	}
	return a.ExplorerPlaces(), err
}

// RelabelExplorerMenu writes the menu again in the language of the page, when it is there for this
// copy in other words (the installer wrote it in its own language, or the language changed), and
// adds the thumbnails to the place of .p5z files when a MaxPack before this one wrote it.
func (a *App) RelabelExplorerMenu(texts explorer.MenuTexts) {
	if isPortable() {
		return
	}
	if executable, err := shell.executable(); err == nil {
		shell.relabel(executable, texts)
	}
}

const installerUsage = "usage: MaxPack --installer add [menu] [sendto] [files] [--language xx] | remove"

// runInstaller does what the installer asks, and tells on out and errs how it went.
func runInstaller(args []string, out, errs io.Writer) int {
	err := installer(args)
	if err != nil {
		fmt.Fprintln(errs, "maxpack:", err)
		return 1
	}
	fmt.Fprintln(out, "done")
	return 0
}

func installer(args []string) error {
	switch {
	case len(args) > 0 && args[0] == "add":
		parts, language, err := installerParts(args[1:])
		if err != nil {
			return err
		}
		if isPortable() {
			return errPortable
		}
		executable, err := shell.executable()
		if err != nil {
			return err
		}
		texts := menuTexts(language)
		for _, part := range parts {
			if err := shell.add(part, executable, texts); err != nil {
				return fmt.Errorf("%s: %w", part, err)
			}
		}
		return nil
	case len(args) == 1 && args[0] == "remove":
		if isPortable() {
			return errPortable
		}
		var failed []error
		for _, place := range explorer.Places {
			if err := shell.remove(place); err != nil {
				failed = append(failed, fmt.Errorf("%s: %w", place, err))
			}
		}
		return errors.Join(failed...)
	}
	return errors.New(installerUsage)
}

// installerParts are the places an add asks for, in its order, and the language of the menu:
// English unless it is one of the window's.
func installerParts(args []string) (parts []string, language string, err error) {
	language = "en"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--language" && i+1 < len(args):
			i++
			if settings.ValidLang(args[i]) {
				language = args[i]
			}
		case slices.Contains(explorer.Places, args[i]):
			parts = append(parts, args[i])
		default:
			return nil, "", fmt.Errorf("the installer cannot add %q (%s)", args[i], installerUsage)
		}
	}
	return parts, language, nil
}

// menuEntry finds a text of the menu in a file of frontend/i18n: each is on a line of its own
var menuEntry = regexp.MustCompile(`(?m)^  (menuCompress|menuVerify|menuDetails): ("(?:[^"\\]|\\.)*"),`)

// menuTexts are the entries of the menu of the Explorer in a language, as the page words them
// (the page writes them again in its own language, see RelabelExplorerMenu). A text the language
// has not is the English one.
func menuTexts(language string) explorer.MenuTexts {
	names := map[string]string{}
	for _, name := range []string{"en", language} {
		page, err := frontend.ReadFile("frontend/i18n/" + name + ".js")
		if err != nil {
			continue
		}
		for _, match := range menuEntry.FindAllStringSubmatch(string(page), -1) {
			if text, err := strconv.Unquote(match[2]); err == nil && text != "" {
				names[match[1]] = text
			}
		}
	}
	return explorer.MenuTexts{Compress: names["menuCompress"], Verify: names["menuVerify"], Details: names["menuDetails"]}
}
