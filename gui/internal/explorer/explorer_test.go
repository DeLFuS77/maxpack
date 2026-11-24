// SPDX-License-Identifier: GPL-3.0-or-later

package explorer

import (
	"reflect"
	"testing"
)

const testExe = `C:\Users\user\AppData\Local\Programs\MaxPack\MaxPack.exe`

var testTexts = MenuTexts{Compress: "Compress with MaxPack", Verify: "Verify with MaxPack", Details: "Details"}

// The menu is one entry on folders and .exfat images, and two on a .p5z, each with the icon of
// MaxPack, shown for any number of files chosen, and starting MaxPack with --section and the file.
func TestAddMenuOps(t *testing.T) {
	classes := `Software\Classes`
	icon := `"` + testExe + `",0`
	verb := func(key, text, section string) []Op {
		return []Op{
			{Do: SetValue, Key: key, Name: "MUIVerb", Data: text},
			{Do: SetValue, Key: key, Name: "Icon", Data: icon},
			{Do: SetValue, Key: key, Name: "MultiSelectModel", Data: "Player"},
			{Do: SetValue, Key: key + `\command`, Data: `"` + testExe + `" --section ` + section + ` "%1"`},
		}
	}
	var want []Op
	want = append(want, verb(classes+`\Directory\shell\MaxPack.Compress`, "Compress with MaxPack", "compress")...)
	want = append(want, verb(classes+`\SystemFileAssociations\.exfat\shell\MaxPack.Compress`, "Compress with MaxPack", "compress")...)
	want = append(want, verb(classes+`\SystemFileAssociations\.p5z\shell\MaxPack.1Verify`, "Verify with MaxPack", "verify")...)
	want = append(want, verb(classes+`\SystemFileAssociations\.p5z\shell\MaxPack.2Details`, "Details", "details")...)
	if got := AddMenuOps("Software", testExe, testTexts); !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%v\nwant\n%v", got, want)
	}
}

// Removing the menu takes away its keys and nothing else.
func TestRemoveMenuOps(t *testing.T) {
	want := []Op{
		{Do: DeleteTree, Key: `Software\Classes\Directory\shell\MaxPack.Compress`},
		{Do: DeleteTree, Key: `Software\Classes\SystemFileAssociations\.exfat\shell\MaxPack.Compress`},
		{Do: DeleteTree, Key: `Software\Classes\SystemFileAssociations\.p5z\shell\MaxPack.1Verify`},
		{Do: DeleteTree, Key: `Software\Classes\SystemFileAssociations\.p5z\shell\MaxPack.2Details`},
	}
	if got := RemoveMenuOps("Software"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%v\nwant\n%v", got, want)
	}
}

// .p5z files open with MaxPack: its ProgID with its icon and --open, in Open with and in Default
// apps; the type opens with it by default only when no program had it (free).
func TestAddFilesOps(t *testing.T) {
	classes := `Software\Classes`
	want := []Op{
		{Do: SetValue, Key: classes + `\MaxPack.p5z`, Data: "MaxPack game"},
		{Do: SetValue, Key: classes + `\MaxPack.p5z\DefaultIcon`, Data: `"` + testExe + `",0`},
		{Do: SetValue, Key: classes + `\MaxPack.p5z\shell\open\command`, Data: `"` + testExe + `" --open "%1"`},
		{Do: SetValue, Key: classes + `\.p5z\OpenWithProgids`, Name: "MaxPack.p5z"},
		{Do: SetValue, Key: `Software\MaxPack\Capabilities`, Name: "ApplicationName", Data: "MaxPack"},
		{Do: SetValue, Key: `Software\MaxPack\Capabilities`, Name: "ApplicationDescription", Data: "Compress PS5 games into .p5z files, verify them and unpack them"},
		{Do: SetValue, Key: `Software\MaxPack\Capabilities\FileAssociations`, Name: ".p5z", Data: "MaxPack.p5z"},
		{Do: SetValue, Key: `Software\RegisteredApplications`, Name: "MaxPack", Data: `Software\MaxPack\Capabilities`},
	}
	if got := AddFilesOps("Software", testExe, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("another program had .p5z: got\n%v\nwant\n%v", got, want)
	}
	free := append(append([]Op{}, want...), Op{Do: SetValue, Key: classes + `\.p5z`, Data: "MaxPack.p5z"})
	if got := AddFilesOps("Software", testExe, true); !reflect.DeepEqual(got, free) {
		t.Fatalf("no program had .p5z: got\n%v\nwant\n%v", got, free)
	}
}

// Removing them takes away what MaxPack wrote, and the default of .p5z only when it is MaxPack's.
func TestRemoveFilesOps(t *testing.T) {
	want := []Op{
		{Do: DeleteValue, Key: `Software\Classes\.p5z\OpenWithProgids`, Name: "MaxPack.p5z"},
		{Do: DeleteOwnValue, Key: `Software\Classes\.p5z`, Data: "MaxPack.p5z"},
		{Do: DeleteTree, Key: `Software\Classes\MaxPack.p5z`},
		{Do: DeleteValue, Key: `Software\RegisteredApplications`, Name: "MaxPack"},
		{Do: DeleteTree, Key: `Software\MaxPack\Capabilities`},
	}
	if got := RemoveFilesOps("Software"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%v\nwant\n%v", got, want)
	}
}

// A test writes everything under a key of its own: the keys of the ops follow it.
func TestOpsUnderAnotherKey(t *testing.T) {
	software := `Software\MaxPackTest-1`
	for _, ops := range [][]Op{AddMenuOps(software, testExe, testTexts), RemoveMenuOps(software), AddFilesOps(software, testExe, true), RemoveFilesOps(software)} {
		for _, op := range ops {
			if len(op.Key) <= len(software) || op.Key[:len(software)+1] != software+`\` {
				t.Errorf("%v is not under %s", op, software)
			}
		}
	}
	if got := AddFilesOps(software, testExe, false); got[len(got)-1].Data != software+`\MaxPack\Capabilities` {
		t.Errorf("RegisteredApplications names %q", got[len(got)-1].Data)
	}
}

// The keys a removal may leave empty, which go then, from the nearest: the key of a value removed
// and the ones above it, or the ones above a key removed; up to Classes, or up to Software, which
// stay, and never a key outside them.
func TestEmptiedKeys(t *testing.T) {
	for _, c := range []struct {
		key  string
		self bool
		want []string
	}{
		{`Software\Classes\SystemFileAssociations\.p5z\shell\MaxPack.1Verify`, false,
			[]string{`Software\Classes\SystemFileAssociations\.p5z\shell`, `Software\Classes\SystemFileAssociations\.p5z`, `Software\Classes\SystemFileAssociations`}},
		{`Software\Classes\.p5z\OpenWithProgids`, true, []string{`Software\Classes\.p5z\OpenWithProgids`, `Software\Classes\.p5z`}},
		{`Software\Classes\.p5z`, true, []string{`Software\Classes\.p5z`}},
		{`Software\Classes\MaxPack.p5z`, false, nil},
		{`Software\MaxPack\Capabilities`, false, []string{`Software\MaxPack`}},
		{`Software\RegisteredApplications`, true, []string{`Software\RegisteredApplications`}},
		{`Software\Classes`, true, nil},
		{`Software`, true, nil},
		{`Elsewhere\Key`, true, nil},
	} {
		if got := emptied("Software", c.key, c.self); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s (%v): %v, want %v", c.key, c.self, got, c.want)
		}
	}
}

// The program a command starts, quoted or not.
func TestCommandProgram(t *testing.T) {
	for command, want := range map[string]string{
		`"C:\Program Files\MaxPack\MaxPack.exe" --open "%1"`: `C:\Program Files\MaxPack\MaxPack.exe`,
		`C:\Tools\MaxPack.exe "%1"`:                          `C:\Tools\MaxPack.exe`,
		`"C:\unclosed`:                                       "",
		"":                                                   "",
	} {
		if got := commandProgram(command); got != want {
			t.Errorf("%q: %q", command, got)
		}
	}
	if got := Command(`C:\Program Files\MaxPack\MaxPack.exe`, "--section", "verify"); got != `"C:\Program Files\MaxPack\MaxPack.exe" --section verify "%1"` {
		t.Errorf("command: %s", got)
	}
}

// A place is missing, here (for this copy of MaxPack) or moved (for another copy, or partly
// there); the case of a path does not matter on Windows.
func TestPlaces(t *testing.T) {
	here := `"` + testExe + `" --open "%1"`
	for _, c := range []struct {
		command, want string
	}{
		{"", Missing},
		{here, Here},
		{`"C:\USERS\USER\APPDATA\LOCAL\PROGRAMS\MAXPACK\MAXPACK.EXE" --open "%1"`, Here},
		{`"D:\Old\MaxPack.exe" --open "%1"`, Moved},
	} {
		if got := placeOf(c.command, testExe); got != c.want {
			t.Errorf("%q: %q, want %q", c.command, got, c.want)
		}
	}
	for _, c := range []struct {
		places []string
		want   string
	}{
		{[]string{Missing, Missing}, Missing},
		{[]string{Here, Here}, Here},
		{[]string{Here, Missing}, Moved},
		{[]string{Here, Moved}, Moved},
		{[]string{Moved, Moved}, Moved},
		{nil, Missing},
	} {
		if got := combine(c.places); got != c.want {
			t.Errorf("%v: %q, want %q", c.places, got, c.want)
		}
	}
}

// Whether .p5z opens with another program: the one chosen with Open with › Always, else the one
// the type had; MaxPack's ProgID, or MaxPack chosen as an application, are MaxPack.
func TestOpensWithOther(t *testing.T) {
	for _, c := range []struct {
		chosen string
		want   bool
	}{
		{"MaxPack.p5z", false},
		{`Applications\MaxPack.exe`, false},
		{`Applications\maxpack.EXE`, false},
		{"Other.Program", true},
		{"", true},
	} {
		if got := opensWithOther(c.chosen, testExe); got != c.want {
			t.Errorf("%q: %v", c.chosen, got)
		}
	}
}

// The places and the texts of the menu are what the page and the installer name them.
func TestPlaceNames(t *testing.T) {
	if !reflect.DeepEqual(Places, []string{"menu", "sendto", "files"}) {
		t.Fatalf("places: %v", Places)
	}
	if !testTexts.valid() || (MenuTexts{Compress: "a", Verify: "b"}).valid() || (MenuTexts{Compress: " ", Verify: "b", Details: "c"}).valid() {
		t.Error("menu texts: a text is missing or blank")
	}
}
