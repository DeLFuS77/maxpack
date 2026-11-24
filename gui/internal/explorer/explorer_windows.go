// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package explorer

// What is written in the registry and the Send to folder of this user, and read back to tell
// Settings what is there. Adapted from NXPress (see explorer.go).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// keys are where the places are written. Tests write under a key of their own.
type keys struct {
	// under HKEY_CURRENT_USER: its Classes, RegisteredApplications and MaxPack\Capabilities
	software string
	// where the classes of the computer and of this user are seen together, for the program .p5z
	// had before
	merged     registry.Key
	mergedPath string
	// where the Explorer keeps the program chosen for each type with Open with › Always
	fileExts string
}

var user = keys{
	software: "Software", merged: registry.CLASSES_ROOT,
	fileExts: `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts`,
}

func (k keys) mergedKey(path string) string {
	if k.mergedPath == "" {
		return path
	}
	return k.mergedPath + `\` + path
}

// notifyShell tells the Explorer that the programs of the files changed, so it shows them at
// once. Tests do without.
var notifyShell = func() {
	const assocChanged, idList = 0x08000000, 0
	windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify").Call(assocChanged, idList, 0, 0)
}

// sendToFolder is the folder of the "Send to" menu of this user. Tests give one of their own.
var sendToFolder = func() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_SendTo, windows.KF_FLAG_DEFAULT)
}

// shortcutName is MaxPack in "Send to": the menu shows it without .lnk.
const shortcutName = "MaxPack.lnk"

// Available says whether this system has the places: only the Explorer of Windows.
func Available() bool { return true }

// Executable is this copy of MaxPack, with its links followed: what the places name.
func Executable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	return executable, nil
}

// Read tells what is there, for the copy of MaxPack at executable.
func Read(executable string) State {
	state := State{Available: true}
	var menu []string
	for _, key := range menuCommands(user.software) {
		menu = append(menu, placeOf(readString(registry.CURRENT_USER, key, ""), executable))
	}
	state.Menu = combine(menu)
	state.Files = placeOf(readString(registry.CURRENT_USER, filesCommand(user.software), ""), executable)
	if state.Files != Missing {
		state.FilesOther = opensWithOther(chosenForP5z(), executable)
	}
	state.SendTo = sendToPlace(executable)
	return state
}

// chosenForP5z is the ProgID .p5z opens with: the one chosen with Open with › Always (Windows 11
// 24H2 keeps it in UserChoiceLatest, the ones before in UserChoice), else the one the type has.
func chosenForP5z() string {
	for _, key := range []string{`\.p5z\UserChoiceLatest\ProgId`, `\.p5z\UserChoice`} {
		if chosen := readString(registry.CURRENT_USER, user.fileExts+key, "ProgId"); chosen != "" {
			return chosen
		}
	}
	return readString(user.merged, user.mergedKey(".p5z"), "")
}

// Add puts a place in the Explorer for the copy of MaxPack at executable; the menu in the texts
// given.
func Add(place, executable string, texts MenuTexts) error {
	switch place {
	case Menu:
		if !texts.valid() {
			return errors.New("the menu has a text missing")
		}
		// what an earlier copy left goes first, so the menu is only this one
		if err := apply(user.software, RemoveMenuOps(user.software)); err != nil {
			return err
		}
		if err := apply(user.software, AddMenuOps(user.software, executable, texts)); err != nil {
			return err
		}
	case Files:
		// .p5z is free when no program had it, or when it was MaxPack's (another copy of it)
		had := readString(user.merged, user.mergedKey(".p5z"), "")
		if err := apply(user.software, AddFilesOps(user.software, executable, had == "" || had == ProgID)); err != nil {
			return err
		}
	case SendTo:
		folder, err := sendToFolder()
		if err != nil {
			return err
		}
		return makeShortcut(filepath.Join(folder, shortcutName), executable)
	default:
		return fmt.Errorf("the Explorer has no place %q", place)
	}
	notifyShell()
	return nil
}

// Remove takes a place away, whichever copy of MaxPack it is for; what other programs wrote stays.
func Remove(place string) error {
	switch place {
	case Menu:
		if err := apply(user.software, RemoveMenuOps(user.software)); err != nil {
			return err
		}
	case Files:
		if err := apply(user.software, RemoveFilesOps(user.software)); err != nil {
			return err
		}
	case SendTo:
		folder, err := sendToFolder()
		if err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(folder, shortcutName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	default:
		return fmt.Errorf("the Explorer has no place %q", place)
	}
	notifyShell()
	return nil
}

// Relabel writes the menu again in the texts given (the language of the window), when it is there
// for this copy of MaxPack in other words.
func Relabel(executable string, texts MenuTexts) error {
	if !texts.valid() || Read(executable).Menu != Here {
		return nil
	}
	same := true
	for _, op := range AddMenuOps(user.software, executable, texts) {
		if op.Name == "MUIVerb" && readString(registry.CURRENT_USER, op.Key, op.Name) != op.Data {
			same = false
		}
	}
	if same {
		return nil
	}
	return Add(Menu, executable, texts)
}

func sendToPlace(executable string) string {
	folder, err := sendToFolder()
	if err != nil {
		return Missing
	}
	shortcut := filepath.Join(folder, shortcutName)
	if _, err := os.Stat(shortcut); err != nil {
		return Missing
	}
	if shortcutTo(shortcut, executable) {
		return Here
	}
	return Moved
}

// apply carries the operations out under HKEY_CURRENT_USER, in their order: a removal also takes
// away the keys it leaves empty (see emptied); what is not there to remove is no error.
func apply(software string, ops []Op) error {
	for _, op := range ops {
		var err error
		switch op.Do {
		case SetValue:
			err = writeString(op.Key, op.Name, op.Data)
		case DeleteTree:
			if err = deleteTree(op.Key); err == nil {
				prune(emptied(software, op.Key, false))
			}
		case DeleteValue:
			if err = deleteValue(op.Key, op.Name); err == nil {
				prune(emptied(software, op.Key, true))
			}
		case DeleteOwnValue:
			if readString(registry.CURRENT_USER, op.Key, op.Name) == op.Data {
				if err = deleteValue(op.Key, op.Name); err == nil {
					prune(emptied(software, op.Key, true))
				}
			}
		default:
			err = fmt.Errorf("unknown operation %d", op.Do)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", op.Key, err)
		}
	}
	return nil
}

func readString(root registry.Key, path, name string) string {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	if err != nil {
		return ""
	}
	return value
}

func writeString(path, name, value string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(name, value)
}

// deleteTree removes a key of this user and all it holds; a key not there is no error.
func deleteTree(path string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	names, err := key.ReadSubKeyNames(-1)
	key.Close()
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := deleteTree(path + `\` + name); err != nil {
			return err
		}
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// deleteValue removes a value of a key of this user; one not there is no error.
func deleteValue(path, name string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// prune removes the keys given, nearest first, while they are empty: it stops at the first that
// holds something, or that it cannot remove.
func prune(paths []string) {
	for _, path := range paths {
		key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
		if errors.Is(err, registry.ErrNotExist) {
			continue
		}
		if err != nil {
			return
		}
		info, err := key.Stat()
		key.Close()
		if err != nil || info.SubKeyCount > 0 || info.ValueCount > 0 {
			return
		}
		if registry.DeleteKey(registry.CURRENT_USER, path) != nil {
			return
		}
	}
}

// shortcutTo tells whether a shortcut points to target. One that cannot be read is taken as it
// is: only a path read, and another, says no.
func shortcutTo(shortcut, target string) bool {
	path, err := shortcutTarget(shortcut)
	if err != nil {
		return true
	}
	if strings.EqualFold(filepath.Clean(path), filepath.Clean(target)) {
		return true
	}
	// the same file by another name: a short name of a folder, a link
	pathInfo, err := os.Stat(path)
	if err != nil {
		return false
	}
	targetInfo, err := os.Stat(target)
	return err == nil && os.SameFile(pathInfo, targetInfo)
}
