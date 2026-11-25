// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
)

// The window needs an engine that packs a game with its DLC and says what a DLC is.
func TestEngineMustKnowDLC(t *testing.T) {
	if !slices.Contains(core.Required, "dlc") {
		t.Errorf("Required: %v", core.Required)
	}
}

// A report says how many DLC a game carries or is compressed with, and how many of them have files of
// their own; never their content IDs, which carry the game's title ID.
func TestReportCountsDLC(t *testing.T) {
	a := &App{}
	text := a.reportText(ReportRequest{Section: "details", Games: []ReportGame{
		{Kind: "p5z", P5zSize: 10, OriginalSize: 20, DLC: 3, DLCData: 1},
		{Kind: "folder", OriginalSize: 20, DLC: 1, DLCData: 1},
		{Kind: "folder", OriginalSize: 20, DLC: 2},
		{Kind: "folder", OriginalSize: 20},
	}})
	for _, want := range []string{"DLC: 3 (1 with data, 2 licenses only)", "DLC: 1 (1 with data)", "DLC: 2 (2 licenses only)"} {
		if !strings.Contains(text, want) {
			t.Errorf("the report does not say %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "DLC:") != 3 || strings.Contains(text, "PPSA") {
		t.Errorf("the report:\n%s", text)
	}
}

// The button of a row asks for DLC packages, as Add files does: without the window there is none.
func TestChooseDLCNeedsTheWindow(t *testing.T) {
	if _, err := (&App{}).ChooseDLC(); !errors.Is(err, errNoWindow) {
		t.Errorf("without a window: %v", err)
	}
}

// With the real engine, Details of a DLC says it is one, and of a .p5z made with DLC, which ones it
// carries.
func TestDetailsOfDLC(t *testing.T) {
	engine, fixtures := needCore(t)
	a := &App{core: engine}
	d, err := a.Details(filepath.Join(fixtures, "dlc_data.pkg"))
	if err != nil || !d.IsDLC() || !d.DLCData || d.TitleID != "PPSA00000" {
		t.Fatalf("a DLC: %+v, %v", d.Item, err)
	}
	game, err := a.Details(filepath.Join(fixtures, "mini.pkg"))
	if err != nil || game.IsDLC() || len(game.DLC) != 0 {
		t.Fatalf("a game: %+v, %v", game.Item, err)
	}
}
