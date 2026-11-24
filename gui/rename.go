// SPDX-License-Identifier: GPL-3.0-or-later

package main

// The names of the files: the template of Settings (File names) with its preview, and Rename, in
// the Check space, which names the games given by what they are, shown before anything changes.

import (
	"errors"
	"path/filepath"

	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// NamePreview is what a template names a game: its .p5z, its unpacked original and its folder for
// the console (always its TITLE_ID). Error: the template is not one, and the names are the ones of
// the default.
type NamePreview struct {
	P5z, Original, Folder string
	Error                 bool
}

// sampleGame is the game of the preview when the lists have none: an invented one, from Europe.
var sampleGame = jobs.Item{Path: "Starfall Odyssey", Name: "Starfall Odyssey", TitleID: "PPSA10001", Version: "01.000.021",
	ContentID: "EP0001-PPSA10001_00-STARFALLODYSSEY0", Kind: "folder"}

// NamePreview names a game of the lists (or the invented one, without) by a template, for Settings.
func (a *App) NamePreview(template string, game *jobs.Item) NamePreview {
	it := sampleGame
	if game != nil && game.Kind != "" {
		it = *game
	}
	preview := NamePreview{Error: jobs.ValidTemplate(template) != nil}
	preview.P5z = jobs.P5zNameAs(it, template)
	preview.Original = jobs.NameOf(it, template)
	folder := jobs.PlanCompress([]jobs.Item{it}, jobs.Options{Mode: jobs.ModeFolder, Dest: "."}, a.drives())
	preview.Folder = filepath.Base(folder[0].Out)
	return preview
}

// RenamePlan gives the new name of each game by the template of Settings, in its own folder, and
// why one cannot take it; nothing changes.
func (a *App) RenamePlan(items []jobs.Item) []jobs.RenamePlan {
	saved, _ := settings.Load()
	return jobs.PlanRename(items, saved.NameTemplate)
}

// Rename names the games as RenamePlan says now: a name taken (also since it was shown), a file
// in use and a game that cannot be read are skipped and said, and nothing is replaced. Not while
// a queue runs: its games are in use.
func (a *App) Rename(items []jobs.Item) ([]jobs.RenameResult, error) {
	if a.working() {
		return nil, errors.New("a job is running")
	}
	results := jobs.Rename(a.RenamePlan(items))
	for _, r := range results {
		if r.State == jobs.StateOK {
			a.log("Renamed: " + r.From + " to " + filepath.Base(r.To))
		}
	}
	return results, nil
}
