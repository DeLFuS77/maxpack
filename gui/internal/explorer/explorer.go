// SPDX-License-Identifier: GPL-3.0-or-later

// Package explorer puts MaxPack in the Explorer of Windows, each place apart and only when the user
// asks (Settings › Windows Explorer, or the installer): a right-click menu on folders, the images
// ShadowMountPlus mounts (.exfat, .ffpkg, .ffpfs, .ffpfsc), PS5 packages (.pkg) and .p5z files,
// MaxPack in "Send to", and .p5z files opened with MaxPack with a double click, with the game's icon
// as their thumbnail. On the images and the
// packages it is only an entry of the menu: the program that opens them and their icon stay the ones
// other tools gave them. All
// of it is written for this user only, under HKEY_CURRENT_USER and in the user's own Send to folder,
// so it needs no administrator, and it names the copy of MaxPack that wrote it.
//
// What is written and removed is worked out here, as a list of operations on the registry, by pure
// functions the tests check as they are; the Windows side (explorer_windows.go) only carries them
// out, and reads what is there.
//
// Adapted from NXPress (https://github.com/DeLFuS77/NXPress: internal/system/explorer_windows.go
// and the "Send to" shortcut of internal/system/console_windows.go), by the same author, where it is
// under the AGPL-3.0; it is brought here by its author under the GPL-3.0-or-later of MaxPack.
package explorer

import (
	"path/filepath"
	"strings"
)

// The places of MaxPack in the Explorer, by the names the page and the installer give them.
const (
	Menu   = "menu"
	SendTo = "sendto"
	Files  = "files"
)

// Places are all of them, in the order of Settings.
var Places = []string{Menu, SendTo, Files}

// The states of a place: not there, there for this copy of MaxPack, or there for another copy (or
// for this one before it was moved, or only in part), which turning it on writes again.
const (
	Missing = ""
	Here    = "here"
	Moved   = "moved"
)

// ProgID is the kind of file the Explorer gives to MaxPack: a .p5z.
const ProgID = "MaxPack.p5z"

// the name of MaxPack in Default apps, and what it says of itself there
const (
	appName        = "MaxPack"
	appDescription = "Compress PS5 games into .p5z files, verify them and unpack them"
	typeName       = "MaxPack game"
)

// MenuTexts are the entries of the menu, in the language of the window.
type MenuTexts struct {
	Compress string `json:"compress"` // folders and images
	Verify   string `json:"verify"`   // .p5z
	Details  string `json:"details"`  // .p5z
}

func (m MenuTexts) valid() bool {
	return strings.TrimSpace(m.Compress) != "" && strings.TrimSpace(m.Verify) != "" && strings.TrimSpace(m.Details) != ""
}

// What an operation does.
type Action int

const (
	SetValue       Action = iota // writes Data in the value Name of Key ("" is its default), making the key
	DeleteTree                   // removes Key and all it holds
	DeleteValue                  // removes the value Name of Key
	DeleteOwnValue               // removes the value Name of Key only while it holds Data
)

// Op is an operation on a key under HKEY_CURRENT_USER. A removal also takes away the keys it
// leaves empty above it (see emptied).
type Op struct {
	Do              Action
	Key, Name, Data string
}

// a verb of the menu: the class of the files it shows on (under Classes), the name of its key,
// the section of the window it opens in, its text, and whether a later MaxPack added it (1.0.5: the
// images besides .exfat; 1.0.6: the packages), so a menu an earlier one wrote may lack it
type verb struct {
	class, name, section string
	text                 func(MenuTexts) string
	later                bool
}

func compressText(m MenuTexts) string { return m.Compress }

// verbs are the entries of the menu. Classic verbs: Windows 11 shows them under Show more options
// (its compact menu takes only the commands of a packaged app). Their keys are named so that
// Verify comes before Details. The images are the ones the window takes (jobs.ImageExts), each
// under SystemFileAssociations, which adds a verb without being the program of the type.
var verbs = []verb{
	{`Directory`, "MaxPack.Compress", "compress", compressText, false},
	{`SystemFileAssociations\.exfat`, "MaxPack.Compress", "compress", compressText, false},
	{`SystemFileAssociations\.ffpkg`, "MaxPack.Compress", "compress", compressText, true},
	{`SystemFileAssociations\.ffpfs`, "MaxPack.Compress", "compress", compressText, true},
	{`SystemFileAssociations\.ffpfsc`, "MaxPack.Compress", "compress", compressText, true},
	{`SystemFileAssociations\.pkg`, "MaxPack.Compress", "compress", compressText, true},
	{`SystemFileAssociations\.p5z`, "MaxPack.1Verify", "verify", func(m MenuTexts) string { return m.Verify }, false},
	{`SystemFileAssociations\.p5z`, "MaxPack.2Details", "details", func(m MenuTexts) string { return m.Details }, false},
}

func classes(software string) string { return software + `\Classes` }

func (v verb) key(software string) string {
	return classes(software) + `\` + v.class + `\shell\` + v.name
}

// Command is what the Explorer runs for a file: this copy of MaxPack, the arguments, and the file.
func Command(executable string, args ...string) string {
	return `"` + executable + `" ` + strings.Join(args, " ") + ` "%1"`
}

func iconOf(executable string) string { return `"` + executable + `",0` }

// AddMenuOps writes the menu under software (Software for the user's own; tests give a key of their
// own): each entry with the icon of MaxPack, shown for any number of files chosen (Player: not only
// up to 15), starts MaxPack with --section, its section and the file. Several files chosen start one
// MaxPack each, which give them to one window.
func AddMenuOps(software, executable string, texts MenuTexts) []Op {
	var ops []Op
	for _, v := range verbs {
		key := v.key(software)
		ops = append(ops,
			Op{Do: SetValue, Key: key, Name: "MUIVerb", Data: v.text(texts)},
			Op{Do: SetValue, Key: key, Name: "Icon", Data: iconOf(executable)},
			Op{Do: SetValue, Key: key, Name: "MultiSelectModel", Data: "Player"},
			Op{Do: SetValue, Key: key + `\command`, Data: Command(executable, "--section", v.section)})
	}
	return ops
}

// RemoveMenuOps takes the menu away.
func RemoveMenuOps(software string) []Op {
	var ops []Op
	for _, v := range verbs {
		ops = append(ops, Op{Do: DeleteTree, Key: v.key(software)})
	}
	return ops
}

// menuCommands are the keys of the commands of the menu, which tell whether it is there and for
// which copy.
func menuCommands(software string) []string {
	var keys []string
	for _, v := range verbs {
		keys = append(keys, v.key(software)+`\command`)
	}
	return keys
}

// capabilities is where MaxPack tells Default apps what it opens.
func capabilities(software string) string { return software + `\MaxPack\Capabilities` }

// The thumbnail provider of .p5z files: maxpack-thumbs.dll (src/thumb/win), next to MaxPack, with
// which the Explorer shows a .p5z's icon as its thumbnail. Its class is registered for this user
// and named as the thumbnail handler of .p5z under SystemFileAssociations, which works whatever
// program opens .p5z. It rides on the place of .p5z files: added and removed with it.
const (
	thumbsCLSID   = "{266D6364-17C0-4B3E-BB33-7EEB90C91837}"
	thumbsFile    = "maxpack-thumbs.dll"
	thumbsName    = "MaxPack .p5z thumbnails"
	thumbsHandler = "{e357fccd-a995-4576-b01f-234630154e96}" // IThumbnailProvider
)

// thumbsDLL is the thumbnail provider of the copy of MaxPack at executable: the one next to it.
func thumbsDLL(executable string) string {
	return executable[:strings.LastIndexAny(executable, `\/`)+1] + thumbsFile
}

func thumbsClass(software string) string { return classes(software) + `\CLSID\` + thumbsCLSID }

func thumbsShellEx(software string) string {
	return classes(software) + `\SystemFileAssociations\.p5z\ShellEx\` + thumbsHandler
}

// thumbsOps register the thumbnail provider of the copy of MaxPack at executable.
func thumbsOps(software, executable string) []Op {
	return []Op{
		{Do: SetValue, Key: thumbsClass(software), Data: thumbsName},
		{Do: SetValue, Key: thumbsClass(software) + `\InprocServer32`, Data: thumbsDLL(executable)},
		{Do: SetValue, Key: thumbsClass(software) + `\InprocServer32`, Name: "ThreadingModel", Data: "Apartment"},
		{Do: SetValue, Key: thumbsShellEx(software), Data: thumbsCLSID},
	}
}

// AddFilesOps makes MaxPack a program of .p5z files: its ProgID, with its icon, that opens one with
// --open; MaxPack in Open with and in Settings › Default apps; and the thumbnail provider, so the
// Explorer shows the icon of each. The type opens with MaxPack with a double click only when free,
// when no program had it: Windows leaves a program the user chose to the user, who chooses MaxPack
// in Open with › Always.
func AddFilesOps(software, executable string, free bool) []Op {
	c := classes(software)
	ops := []Op{
		{Do: SetValue, Key: c + `\` + ProgID, Data: typeName},
		{Do: SetValue, Key: c + `\` + ProgID + `\DefaultIcon`, Data: iconOf(executable)},
		{Do: SetValue, Key: c + `\` + ProgID + `\shell\open\command`, Data: Command(executable, "--open")},
		{Do: SetValue, Key: c + `\.p5z\OpenWithProgids`, Name: ProgID},
		{Do: SetValue, Key: capabilities(software), Name: "ApplicationName", Data: appName},
		{Do: SetValue, Key: capabilities(software), Name: "ApplicationDescription", Data: appDescription},
		{Do: SetValue, Key: capabilities(software) + `\FileAssociations`, Name: ".p5z", Data: ProgID},
		{Do: SetValue, Key: software + `\RegisteredApplications`, Name: appName, Data: capabilities(software)},
	}
	ops = append(ops, thumbsOps(software, executable)...)
	if free {
		ops = append(ops, Op{Do: SetValue, Key: c + `\.p5z`, Data: ProgID})
	}
	return ops
}

// RemoveFilesOps takes MaxPack away from the programs of .p5z files, and its thumbnails; what other
// programs wrote for .p5z stays, and so do a default and a thumbnail handler that are not MaxPack's.
func RemoveFilesOps(software string) []Op {
	c := classes(software)
	return []Op{
		{Do: DeleteValue, Key: c + `\.p5z\OpenWithProgids`, Name: ProgID},
		{Do: DeleteOwnValue, Key: c + `\.p5z`, Data: ProgID},
		{Do: DeleteTree, Key: c + `\` + ProgID},
		{Do: DeleteValue, Key: software + `\RegisteredApplications`, Name: appName},
		{Do: DeleteTree, Key: capabilities(software)},
		{Do: DeleteOwnValue, Key: thumbsShellEx(software), Data: thumbsCLSID},
		{Do: DeleteTree, Key: thumbsClass(software)},
	}
}

// filesCommand is the key of the command a double click runs.
func filesCommand(software string) string {
	return classes(software) + `\` + ProgID + `\shell\open\command`
}

// emptied are the keys a removal may leave empty, from the nearest up, which go when they are: the
// key itself (self: a value of it was removed) and the ones above it, up to Classes or, outside it,
// up to Software, which stay. Keys outside software are never touched.
func emptied(software, key string, self bool) []string {
	stop := software
	if key == classes(software) || strings.HasPrefix(key, classes(software)+`\`) {
		stop = classes(software)
	}
	if !strings.HasPrefix(key, stop+`\`) {
		return nil
	}
	var keys []string
	if !self {
		key = key[:strings.LastIndex(key, `\`)]
	}
	for key != stop {
		keys = append(keys, key)
		key = key[:strings.LastIndex(key, `\`)]
	}
	return keys
}

// commandProgram is the program a command starts, "" for none.
func commandProgram(command string) string {
	if strings.HasPrefix(command, `"`) {
		if end := strings.Index(command[1:], `"`); end >= 0 {
			return command[1 : end+1]
		}
		return ""
	}
	program, _, _ := strings.Cut(command, " ")
	return program
}

func sameProgram(a, b string) bool {
	return a != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// placeOf tells whether a command was written for this copy of MaxPack.
func placeOf(command, executable string) string {
	switch {
	case command == "":
		return Missing
	case sameProgram(commandProgram(command), executable):
		return Here
	default:
		return Moved
	}
}

// menuPlace is the state of the menu from the places of its commands, in the order of verbs: as
// combine, but a menu an earlier MaxPack wrote, here without the entries a later one added, is
// still here (Relabel completes it as the window opens); one of those for another copy, or only
// them, is moved.
func menuPlace(places []string) string {
	var first, later []string
	for i, v := range verbs {
		if v.later {
			later = append(later, places[i])
		} else {
			first = append(first, places[i])
		}
	}
	state := combine(first)
	for _, place := range later {
		switch {
		case state == Here && place == Moved, state == Missing && place != Missing:
			return Moved
		}
	}
	return state
}

// menuComplete tells whether every entry of the menu is there for this copy.
func menuComplete(places []string) bool {
	for _, place := range places {
		if place != Here {
			return false
		}
	}
	return len(places) == len(verbs)
}

// combine is the state of a place made of several keys: here or missing when all are, else moved.
func combine(places []string) string {
	if len(places) == 0 {
		return Missing
	}
	for _, place := range places[1:] {
		if place != places[0] {
			return Moved
		}
	}
	return places[0]
}

// opensWithOther tells whether the program chosen for .p5z (the ProgID Windows keeps for it) is not
// MaxPack: neither its ProgID nor MaxPack chosen as an application in Open with.
func opensWithOther(chosen, executable string) bool {
	base := executable[strings.LastIndexAny(executable, `\/`)+1:]
	return chosen != ProgID && !strings.EqualFold(chosen, `Applications\`+base)
}

// State is what Settings › Windows Explorer shows: each place, and whether .p5z still opens with
// another program, which only the user can change.
type State struct {
	Available  bool   `json:"available"`
	Menu       string `json:"menu"`
	SendTo     string `json:"sendTo"`
	Files      string `json:"files"`
	FilesOther bool   `json:"filesOther"`
}

// Of tells the state of one place.
func (s State) Of(place string) string {
	switch place {
	case Menu:
		return s.Menu
	case SendTo:
		return s.SendTo
	case Files:
		return s.Files
	}
	return Missing
}
