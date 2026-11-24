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
// why it cannot when it cannot: "same_name" (it has it already), "exists" (something else has
// it), "duplicate_target" (another game of the list would take it) or "cannot_read" (what it
// is is not known).
type RenamePlan struct {
	Item     Item
	From, To string
	Block    string
}

// RenameResult is how the renaming of a game ended: ok, skipped (with why, the Block of its plan
// or "exists" when a name came meanwhile) or error ("in_use", "gone", or "io: ...").
type RenameResult struct {
	From, To      string
	State, Detail string
}

// newName is the name a game takes by the template: a .p5z and an image keep their extension,
// a dump folder takes the name of an original, and a folder for the console its TITLE_ID.
func newName(it Item, template string) string {
	switch {
	case it.Kind == "folder" && it.Packed:
		return idName(it)
	case it.Kind == "folder":
		return folderName(it, template)
	}
	ext := filepath.Ext(it.Path)
	return nameIn(it, template, maxName-len(ext)) + ext
}

// sameFile tells whether two paths are one file: a change of case only, on Windows and macOS.
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
		switch {
		case p.To == p.From:
			p.Block = "same_name"
		case !same(p.To, p.From) && exists(p.To):
			p.Block = "exists"
		case same(p.To, p.From) && !sameFile(p.To, p.From) && exists(p.To):
			p.Block = "exists"
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

// Rename does the plans: a game whose new name something took meanwhile is skipped, and nothing
// is ever replaced.
func Rename(plans []RenamePlan) []RenameResult {
	results := make([]RenameResult, len(plans))
	for i, p := range plans {
		r := RenameResult{From: p.From, To: p.To}
		switch {
		case p.Block != "":
			r.State, r.Detail = StateSkipped, p.Block
		case !exists(p.From):
			r.State, r.Detail = StateError, "gone"
		case exists(p.To) && !(same(p.To, p.From) && sameFile(p.To, p.From)):
			r.State, r.Detail = StateSkipped, "exists"
		default:
			var err error
			if same(p.To, p.From) {
				// a change of case only: the file is the one at both names
				err = os.Rename(p.From, p.To)
			} else {
				err = renameNew(p.From, p.To)
			}
			switch {
			case err == nil:
				r.State = StateOK
			case errors.Is(err, os.ErrExist):
				r.State, r.Detail = StateSkipped, "exists"
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
