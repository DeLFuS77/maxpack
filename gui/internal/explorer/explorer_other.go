// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package explorer

import (
	"errors"
	"os"
)

// Only the Explorer of Windows has these places: elsewhere they are never there, and adding one
// says so.

var errNoExplorer = errors.New("only the Explorer of Windows has these places")

func Available() bool                                     { return false }
func Executable() (string, error)                         { return os.Executable() }
func Read(executable string) State                        { return State{} }
func Add(place, executable string, texts MenuTexts) error { return errNoExplorer }
func Remove(place string) error                           { return nil }
func Relabel(executable string, texts MenuTexts) error    { return nil }
