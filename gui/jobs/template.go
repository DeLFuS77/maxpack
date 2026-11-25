// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/DeLFuS77/maxpack/gui/internal/par2"
)

// The names the window gives a .p5z and an unpacked original come from a template, as NXPress
// names its files: {title}, {id}, {version} (the one of param.json, 01.000.021) and {region}
// (by the first two letters of the content ID). A bracket whose placeholders are all empty goes,
// with the spaces before it, and the name is cleaned and cut as before. A folder for the console
// is named by its TITLE_ID whatever the template, as ShadowMountPlus and MaxPack for the console
// look for it.

// DefaultTemplate gives the names of before: "<Title> [<TITLE_ID>]".
const DefaultTemplate = "{title} [{id}]"

// Templates are the ones Settings offers, the default first.
var Templates = []string{DefaultTemplate, "{title} [{id}] [v{version}]", "{title} [{id}] [{region}] [v{version}]"}

// regions are the regions by the first two letters of a content ID.
var regions = map[string]string{"UP": "USA", "EP": "EUR", "JP": "JPN", "HP": "ASIA", "KP": "KOR"}

// Region is the region of a game, by its content ID: USA, EUR, JPN, ASIA or KOR, "" when it is
// none of them or not known.
func Region(it Item) string {
	if len(it.ContentID) < 2 {
		return ""
	}
	return regions[strings.ToUpper(it.ContentID[:2])]
}

var (
	placeholder = regexp.MustCompile(`\{([a-z]+)\}`)
	braces      = regexp.MustCompile(`\{[^{}]*\}`)
	// a bracket and the spaces before it
	bracket = regexp.MustCompile(`\s*(?:\[[^\[\]()]*\]|\([^\[\]()]*\))`)
)

var known = map[string]bool{"title": true, "id": true, "version": true, "region": true}

// ValidTemplate tells why a template is not one: empty, too long, with a placeholder the window
// does not have, without {title} or {id} (every game would get the same name), or with what a
// name cannot have on Windows.
func ValidTemplate(template string) error {
	if strings.TrimSpace(template) == "" {
		return errors.New("the template is empty")
	}
	if len(template) > maxName {
		return fmt.Errorf("the template is longer than %d bytes", maxName)
	}
	for _, found := range braces.FindAllString(template, -1) {
		if !known[strings.Trim(found, "{}")] {
			return fmt.Errorf("%s is not a placeholder: use {title}, {id}, {version} or {region}", found)
		}
	}
	if !strings.Contains(template, "{title}") && !strings.Contains(template, "{id}") {
		return errors.New("the template needs {title} or {id}")
	}
	for _, r := range braces.ReplaceAllString(template, "") {
		if strings.ContainsRune(`<>:"/\|?*{}`, r) || unicode.IsControl(r) {
			return fmt.Errorf("a name cannot have %q", r)
		}
	}
	return nil
}

// expand puts the values in the template: a bracket whose placeholders are all empty goes.
func expand(template string, values map[string]string) string {
	template = bracket.ReplaceAllStringFunc(template, func(group string) string {
		names := placeholder.FindAllStringSubmatch(group, -1)
		if len(names) == 0 {
			return group
		}
		for _, name := range names {
			if values[name[1]] != "" {
				return group
			}
		}
		return ""
	})
	return placeholder.ReplaceAllStringFunc(template, func(found string) string { return values[found[1:len(found)-1]] })
}

// NameOf is the name a template gives a game, with no extension: no characters that Windows
// refuses, at most 200 bytes (the title is cut, whole characters only, so the rest stays). Its
// TITLE_ID is not said again when the title has it already; without a title, the name of its
// folder or image is the title; a template that is not one is the default.
func NameOf(it Item, template string) string { return nameIn(it, template, maxName) }

func nameIn(it Item, template string, room int) string {
	if ValidTemplate(template) != nil {
		template = DefaultTemplate
	}
	title := cleanTitle(it.Name)
	if title == "" {
		title = cleanTitle(baseName(it.Path))
	}
	if title == "" {
		title = "game"
	}
	id := cleanTitle(it.TitleID)
	if strings.Contains(title, id) {
		id = ""
	}
	values := map[string]string{"title": title, "id": id, "version": cleanTitle(it.Version), "region": Region(it)}
	name := cleanTitle(expand(template, values))
	// too long: the title gives way, as much as is over each time
	for n := 0; len(name) > room && values["title"] != "" && n < 8; n++ {
		values["title"] = cut(values["title"], len(values["title"])-(len(name)-room))
		name = cleanTitle(expand(template, values))
	}
	name = cut(name, room)
	if name == "" {
		name = cut(title, room)
	}
	return name
}

// paramContentID is the content ID in the param.json of a dump folder, "" without one.
func paramContentID(folder string) string {
	data, err := os.ReadFile(filepath.Join(folder, "sce_sys", "param.json"))
	if err != nil || len(data) > 1<<20 {
		return ""
	}
	var param struct {
		ContentID string `json:"contentId"`
	}
	if json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &param) != nil {
		return ""
	}
	return param.ContentID
}

// RenamePlan is what Rename would do with a game: from where to where (in its own folder), and
// why it cannot when it cannot: "same_name" (it has it already), "name_taken" (something else has
// it, or a name its repair data would take), "duplicate_target" (another game of the list would take
// it) or "cannot_read" (what it is is not known). Set are the files of the repair data of a .p5z,
// which take its new name with it.
type RenamePlan struct {
	Item     Item
	From, To string
	Block    string
	Set      []Move
}

// Move is a file that takes another name.
type Move struct{ From, To string }

// setMoves are the files of the set of a .p5z (named after it: see par2.Files) with the names they
// take when the .p5z is renamed from to to.
func setMoves(from, to string) []Move {
	var moves []Move
	base := filepath.Base(from)
	for _, file := range par2.Files(from) {
		name := filepath.Base(file)
		moves = append(moves, Move{From: file, To: filepath.Join(filepath.Dir(to), filepath.Base(to)+name[len(base):])})
	}
	return moves
}

// taken tells whether a name is had by another file (not the same one at another case).
func taken(to, from string) bool { return exists(to) && !sameFile(to, from) }

// RenameResult is how the renaming of a game ended: ok, skipped (with why, the Block of its plan
// or "name_taken" when a name came meanwhile) or error ("in_use", "gone", or "io: ...").
type RenameResult struct {
	From, To      string
	State, Detail string
}

// newName is the name a game takes by the template: a .p5z and an image keep their extension
// (in its case), a dump folder takes the name of an original, and a folder for the console its
// TITLE_ID. A file whose extension is not the one of what the engine says it is (an image renamed:
// MaxPack for the console, for one, finds images by their extension) takes the extension of what
// it is.
func newName(it Item, template string) string {
	switch {
	case it.Kind == "folder" && it.Packed:
		return idName(it)
	case it.Kind == "folder":
		return folderName(it, template)
	}
	ext := filepath.Ext(it.Path)
	if want := kindExt(it.Kind); want != "" && !strings.EqualFold(ext, want) {
		ext = want
	}
	return nameIn(it, template, maxName-len(ext)) + ext
}

// sameFile tells whether two paths are one file: on a file system that ignores case, a change of
// case only.
func sameFile(a, b string) bool {
	first, errA := os.Stat(a)
	second, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(first, second)
}

// PlanRename gives the new name of each game by the template, and changes nothing.
func PlanRename(items []Item, template string) []RenamePlan {
	plans := make([]RenamePlan, len(items))
	for i, it := range items {
		p := RenamePlan{Item: it, From: filepath.Clean(it.Path)}
		plans[i] = p
		if it.Kind == "" {
			plans[i].Block = "cannot_read"
			continue
		}
		if _, err := os.Lstat(p.From); err != nil {
			plans[i].Block = "cannot_read"
			continue
		}
		p.To = filepath.Join(filepath.Dir(p.From), newName(it, template))
		if it.Kind == "p5z" {
			p.Set = setMoves(p.From, p.To)
		}
		switch {
		case p.To == p.From:
			p.Block = "same_name"
		case taken(p.To, p.From):
			// taken by another file; the same file at another case (a file system that ignores
			// case, as Windows' and macOS' usually do) is only a change of case
			p.Block = "name_taken"
		}
		for _, m := range p.Set {
			if p.Block == "" && taken(m.To, m.From) {
				p.Block = "name_taken"
			}
		}
		for j := 0; j < i && p.Block == ""; j++ {
			if plans[j].Block == "" && same(plans[j].To, p.To) {
				p.Block = "duplicate_target"
			}
		}
		plans[i] = p
	}
	return plans
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// renameFile gives a file a name nothing has (a variable so the tests can make it fail).
var renameFile = renameNew

// renameOne gives a file or a folder its new name: a change of case only renames the file that is at
// both names; else nothing that has the new name is ever replaced.
func renameOne(from, to string) error {
	if sameFile(to, from) {
		return os.Rename(from, to)
	}
	return renameFile(from, to)
}

// renameWithSet renames a game and the files of its repair data, all or nothing: the set first, then
// the game; if any of them fails, the ones done go back.
func renameWithSet(p RenamePlan) error {
	var done []Move
	undo := func() {
		for n := len(done) - 1; n >= 0; n-- {
			renameOne(done[n].To, done[n].From)
		}
	}
	for _, m := range p.Set {
		if err := renameOne(m.From, m.To); err != nil {
			undo()
			return err
		}
		done = append(done, m)
	}
	if err := renameOne(p.From, p.To); err != nil {
		undo()
		return err
	}
	return nil
}

// Rename does the plans: a game whose new name (or one of its repair data) something took meanwhile
// is skipped, and nothing is ever replaced. A .p5z takes the files of its repair data with it.
func Rename(plans []RenamePlan) []RenameResult {
	results := make([]RenameResult, len(plans))
	for i, p := range plans {
		r := RenameResult{From: p.From, To: p.To}
		setTaken := false
		for _, m := range p.Set {
			setTaken = setTaken || taken(m.To, m.From)
		}
		switch {
		case p.Block != "":
			r.State, r.Detail = StateSkipped, p.Block
		case !exists(p.From):
			r.State, r.Detail = StateError, "gone"
		case taken(p.To, p.From) || setTaken:
			r.State, r.Detail = StateSkipped, "name_taken"
		default:
			err := renameWithSet(p)
			switch {
			case err == nil:
				r.State = StateOK
			case errors.Is(err, os.ErrExist):
				r.State, r.Detail = StateSkipped, "name_taken"
			case inUse(err):
				r.State, r.Detail = StateError, "in_use"
			default:
				r.State, r.Detail = StateError, "io: "+err.Error()
			}
		}
		results[i] = r
	}
	return results
}
