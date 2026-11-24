// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func game(title, id, version, content string) Item {
	return Item{Path: filepath.Join(os.TempDir(), "origen-"+id), TitleID: id, Name: title, Version: version, ContentID: content, Kind: "folder", OriginalSize: 10}
}

// The placeholders of a template, the region by the first two letters of the content ID, and the
// brackets left empty dropped (the space before them too).
func TestNameTemplate(t *testing.T) {
	eu := game("Starfall Odyssey", "PPSA10001", "01.000.021", "EP0001-PPSA10001_00-STARFALLODYSSEY0")
	for _, c := range []struct {
		it       Item
		template string
		want     string
	}{
		{eu, DefaultTemplate, "Starfall Odyssey [PPSA10001]"},
		{eu, "{title} [{id}] [v{version}]", "Starfall Odyssey [PPSA10001] [v01.000.021]"},
		{eu, "{title} [{id}] [{region}] [v{version}]", "Starfall Odyssey [PPSA10001] [EUR] [v01.000.021]"},
		{game("Iron Harbor", "PPSA10002", "01.000.000", "UP0002-PPSA10002_00-IRONHARBOR000000"), "{title} ({region})", "Iron Harbor (USA)"},
		{game("Iron Harbor", "PPSA10002", "", "JP0002-PPSA10002_00-IRONHARBOR000000"), "{region} - {title}", "JPN - Iron Harbor"},
		{game("Neon Meridian", "PPSA10003", "", "HP0003-PPSA10003_00-NEONMERIDIAN0000"), "{title} [{region}]", "Neon Meridian [ASIA]"},
		{game("Neon Meridian", "PPSA10003", "", "KP0003-PPSA10003_00-NEONMERIDIAN0000"), "{title} [{region}]", "Neon Meridian [KOR]"},
		// a content ID of another kind, or none: no region, and no empty brackets
		{game("Neon Meridian", "PPSA10003", "", "XX0003-PPSA10003_00-NEONMERIDIAN0000"), "{title} [{id}] [{region}]", "Neon Meridian [PPSA10003]"},
		{game("Neon Meridian", "PPSA10003", "01.000.000", ""), "{title} [{id}] [{region}] [v{version}]", "Neon Meridian [PPSA10003] [v01.000.000]"},
		// no version, no ID
		{game("Neon Meridian", "PPSA10003", "", ""), "{title} [{id}] [v{version}]", "Neon Meridian [PPSA10003]"},
		{game("Neon Meridian", "", "", ""), "{title} [{id}] [v{version}]", "Neon Meridian"},
		{game("Neon Meridian", "", "", ""), "{id}", "Neon Meridian"},
		// the ID that is in the title already is not said again, as before
		{game("PPSA10004 Demo", "PPSA10004", "", ""), DefaultTemplate, "PPSA10004 Demo"},
		// what a name cannot have on Windows goes, from the title and from the template
		{game("Comet: Rift? <Apart>", "PPSA00001", "", ""), "{title} [{id}].", "Comet Rift Apart [PPSA00001]"},
	} {
		if got := NameOf(c.it, c.template); got != c.want {
			t.Errorf("%q with %q: %q, expected %q", c.it.Name, c.template, got, c.want)
		}
	}
	// without a title, the name of the folder or the image
	it := game("", "PPSA10005", "", "")
	it.Path = filepath.Join("x", "Lost Signal.exfat")
	if got := NameOf(it, DefaultTemplate); got != "Lost Signal [PPSA10005]" {
		t.Errorf("without a title: %q", got)
	}
}

// oldGameName is how 1.0.1 named a game: "<Title> [<TITLE_ID>]", at most room bytes.
func oldGameName(it Item, room int) string {
	title := cleanTitle(it.Name)
	if title == "" {
		title = cleanTitle(baseName(it.Path))
	}
	suffix := ""
	id := cleanTitle(it.TitleID)
	if id != "" && !strings.Contains(title, id) {
		suffix = " [" + id + "]"
	}
	title = cut(title, room-len(suffix))
	if title == "" {
		title = "game"
	}
	return title + suffix
}

// The default template gives the names of before, so nothing changes for whoever does not touch
// it; the names of the console stay its TITLE_ID whatever the template.
func TestDefaultTemplateIsTheOldName(t *testing.T) {
	for _, it := range []Item{
		game("Starfall Odyssey", "PPSA10001", "01.000.021", "EP0001-PPSA10001_00-X"),
		game("Comet: Rift? <Apart>", "PPSA00001", "", ""),
		game(strings.Repeat("é", 200), "PPSA00001", "", ""),
		game("PPSA10004 Demo", "PPSA10004", "", ""),
		game("", "", "", ""),
	} {
		if got, want := P5zNameAs(it, DefaultTemplate), oldGameName(it, maxName-len(".p5z"))+".p5z"; got != want || P5zName(it) != want {
			t.Errorf("%q: %q, before %q", it.Name, got, want)
		}
		if got, want := folderName(it, DefaultTemplate), oldGameName(it, maxName); got != want || NameOf(it, DefaultTemplate) != want {
			t.Errorf("%q: %q, before %q", it.Name, got, want)
		}
	}
	o := Options{Mode: ModeFolder, Dest: t.TempDir(), NameTemplate: "{title} [{id}] [v{version}]"}
	it := game("Starfall Odyssey", "PPSA10001", "01.000.021", "")
	if p := PlanCompress([]Item{it}, o, newFS())[0]; filepath.Base(p.Out) != "PPSA10001" {
		t.Errorf("a folder for the USB: %q", p.Out)
	}
	o.Mode = ModeP5z
	if p := PlanCompress([]Item{it}, o, newFS())[0]; filepath.Base(p.Out) != "Starfall Odyssey [PPSA10001] [v01.000.021].p5z" {
		t.Errorf("a .p5z: %q", p.Out)
	}
	it.Kind = "p5z"
	o.Mode = ModeOriginal
	if p := PlanDecompress([]Item{it}, o, newFS())[0]; filepath.Base(p.Out) != "Starfall Odyssey [PPSA10001] [v01.000.021]" {
		t.Errorf("an original: %q", p.Out)
	}
	o.Mode = ModeConsole
	if p := PlanDecompress([]Item{it}, o, newFS())[0]; filepath.Base(p.Out) != "PPSA10001" {
		t.Errorf("a folder for the PS5: %q", p.Out)
	}
}

// A name too long is cut in its title, whole characters only, and the rest of the template stays.
func TestNameTemplateLength(t *testing.T) {
	it := game(strings.Repeat("Ω", 150), "PPSA10001", "01.000.021", "EP0001-PPSA10001_00-X")
	got := NameOf(it, "{title} [{id}] [{region}] [v{version}]")
	if len(got) > maxName || !strings.HasSuffix(got, " [PPSA10001] [EUR] [v01.000.021]") || !strings.HasPrefix(got, "ΩΩ") {
		t.Errorf("%d bytes: %q", len(got), got)
	}
	if p5z := P5zNameAs(it, "{title} [{id}] [{region}] [v{version}]"); len(p5z) > maxName || !strings.HasSuffix(p5z, "].p5z") {
		t.Errorf(".p5z: %d bytes, %q", len(p5z), p5z)
	}
	// a template longer than the room with no title to cut is cut whole
	long := "{id}" + strings.Repeat(" x", 98)
	if got := NameOf(it, long); len(got) > maxName || !strings.HasPrefix(got, "PPSA10001 x") {
		t.Errorf("long template: %d bytes, %q", len(got), got)
	}
}

func TestValidTemplate(t *testing.T) {
	for _, good := range []string{DefaultTemplate, "{title} [{id}] [v{version}]", "{title} [{id}] [{region}] [v{version}]", "{id} - {title}", "{title}"} {
		if err := ValidTemplate(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"", "  ", "game", "{version}", "{name}", "{title}/{id}", `{title}\{id}`, "{title}: {id}", "{title} {", "{title}\x01"} {
		if err := ValidTemplate(bad); err == nil {
			t.Errorf("%q is taken", bad)
		}
	}
}

// Rename names each game by the template, in its own folder: a .p5z and an image keep their
// extension, a dump folder takes the name of an original and a folder for the console its
// TITLE_ID. Nothing is renamed by planning; a name taken by something there, or by another game
// of the list, and a game that cannot be read are skipped and said.
func TestRenamePlan(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, folder bool) string {
		path := filepath.Join(dir, name)
		if folder {
			os.MkdirAll(filepath.Join(path, "sce_sys"), 0o755)
		} else {
			os.WriteFile(path, []byte("x"), 0o644)
		}
		return path
	}
	p5z := Item{Path: mk("starfall.p5z", false), Kind: "p5z", Name: "Starfall Odyssey", TitleID: "PPSA10001", Version: "01.000.021"}
	image := Item{Path: mk("harbor.exfat", false), Kind: "exfat", Name: "Iron Harbor", TitleID: "PPSA10002", Version: "01.000.000"}
	dump := Item{Path: mk("neon-dump", true), Kind: "folder", Name: "Neon Meridian", TitleID: "PPSA10003", Version: "01.000.000"}
	packed := Item{Path: mk("packed", true), Kind: "folder", Packed: true, Name: "Lost Signal", TitleID: "PPSA10004"}
	done := Item{Path: mk("Quiet Tide [PPSA10005].p5z", false), Kind: "p5z", Name: "Quiet Tide", TitleID: "PPSA10005"}
	mk("Glass Orbit [PPSA10006].p5z", false)
	taken := Item{Path: mk("orbit.p5z", false), Kind: "p5z", Name: "Glass Orbit", TitleID: "PPSA10006"}
	twin := Item{Path: mk("starfall copy.p5z", false), Kind: "p5z", Name: "Starfall Odyssey", TitleID: "PPSA10001", Version: "01.000.021"}
	unread := Item{Path: mk("broken.p5z", false)}
	plans := PlanRename([]Item{p5z, image, dump, packed, done, taken, twin, unread}, "{title} [{id}] [v{version}]")
	want := []struct{ name, block string }{
		{"Starfall Odyssey [PPSA10001] [v01.000.021].p5z", ""},
		{"Iron Harbor [PPSA10002] [v01.000.000].exfat", ""},
		{"Neon Meridian [PPSA10003] [v01.000.000]", ""},
		{"PPSA10004", ""},
		{"Quiet Tide [PPSA10005].p5z", "same_name"},
		{"Glass Orbit [PPSA10006].p5z", "exists"},
		{"Starfall Odyssey [PPSA10001] [v01.000.021].p5z", "duplicate_target"},
		{"", "cannot_read"},
	}
	for i, w := range want {
		p := plans[i]
		if filepath.Base(p.To) != w.name && w.name != "" || p.Block != w.block {
			t.Errorf("%d %s: %q %q, expected %q %q", i, filepath.Base(p.From), filepath.Base(p.To), p.Block, w.name, w.block)
		}
		if w.name != "" && filepath.Dir(p.To) != filepath.Dir(p.From) {
			t.Errorf("%d: goes to another folder: %q", i, p.To)
		}
	}
	if _, err := os.Stat(p5z.Path); err != nil {
		t.Error("planning renamed something")
	}
}

// Renaming does what the plan says and nothing else: a name that came meanwhile is not replaced,
// and one in use is said; on Windows and macOS a change of case only is a rename too.
func TestRename(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.p5z")
	b := filepath.Join(dir, "b.p5z")
	c := filepath.Join(dir, "c.p5z")
	for _, path := range []string{a, b, c} {
		os.WriteFile(path, []byte(filepath.Base(path)), 0o644)
	}
	items := []Item{
		{Path: a, Kind: "p5z", Name: "Starfall Odyssey", TitleID: "PPSA10001"},
		{Path: b, Kind: "p5z", Name: "Iron Harbor", TitleID: "PPSA10002"},
		{Path: c, Kind: "p5z", Name: "Neon Meridian", TitleID: "PPSA10003"},
	}
	plans := PlanRename(items, DefaultTemplate)
	// between the plan and the click, something takes the name of the second one
	os.WriteFile(plans[1].To, []byte("ajeno"), 0o644)
	results := Rename(plans)
	if results[0].State != StateOK || results[1].State != StateSkipped || results[1].Detail != "exists" || results[2].State != StateOK {
		t.Fatalf("%+v", results)
	}
	if data, _ := os.ReadFile(plans[0].To); string(data) != "a.p5z" {
		t.Errorf("the first: %q", data)
	}
	if data, _ := os.ReadFile(plans[1].To); string(data) != "ajeno" {
		t.Errorf("what came meanwhile changed: %q", data)
	}
	if _, err := os.Stat(b); err != nil {
		t.Errorf("the second is gone: %v", err)
	}
	if _, err := os.Stat(a); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old name is still there: %v", err)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		lower := filepath.Join(dir, "neon meridian [PPSA10003].p5z")
		os.Rename(plans[2].To, lower)
		again := PlanRename([]Item{{Path: lower, Kind: "p5z", Name: "Neon Meridian", TitleID: "PPSA10003"}}, DefaultTemplate)
		if again[0].Block != "" {
			t.Fatalf("a change of case: %+v", again[0])
		}
		if r := Rename(again); r[0].State != StateOK {
			t.Fatalf("%+v", r)
		}
		entries, _ := os.ReadDir(dir)
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if !strings.Contains(strings.Join(names, "|"), "Neon Meridian [PPSA10003].p5z") {
			t.Errorf("the case did not change: %v", names)
		}
	}
	// a game that is gone meanwhile
	gone := PlanRename([]Item{{Path: filepath.Join(dir, "gone.p5z"), Kind: "p5z", Name: "Lost Signal", TitleID: "PPSA10004"}}, DefaultTemplate)
	if r := Rename(gone); r[0].State == StateOK {
		t.Errorf("a game that is not there: %+v", r)
	}
}

// The region and the content ID: from what the engine says when it says it, else from the
// param.json of a dump folder.
func TestContentID(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "dump")
	os.MkdirAll(filepath.Join(dump, "sce_sys"), 0o755)
	os.WriteFile(filepath.Join(dump, "sce_sys", "param.json"), append([]byte{0xEF, 0xBB, 0xBF}, "{\"titleId\":\"PPSA10001\",\"contentId\":\"UP0001-PPSA10001_00-STARFALLODYSSEY0\"}"...), 0o644)
	if it := FromInfo(dump, map[string]any{"title_id": "PPSA10001", "kind": "folder"}); it.ContentID != "UP0001-PPSA10001_00-STARFALLODYSSEY0" || Region(it) != "USA" {
		t.Errorf("from param.json: %+v", it)
	}
	if it := FromInfo("x.p5z", map[string]any{"kind": "p5z", "content_id": "EP0001-PPSA10001_00-X"}); Region(it) != "EUR" {
		t.Errorf("from the engine: %+v", it)
	}
	if it := FromInfo("x.p5z", map[string]any{"kind": "p5z"}); it.ContentID != "" || Region(it) != "" {
		t.Errorf("none: %+v", it)
	}
}
