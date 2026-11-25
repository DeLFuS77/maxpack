// SPDX-License-Identifier: GPL-3.0-or-later

package explorer

import (
	"reflect"
	"strings"
	"testing"
)

const testExe = `C:\Users\user\AppData\Local\Programs\MaxPack\MaxPack.exe`

var testTexts = MenuTexts{Compress: "Compress with MaxPack", Verify: "Verify with MaxPack", Details: "Details"}

// The menu is one entry on folders, on the images ShadowMountPlus mounts (.exfat, .ffpkg, .ffpfs,
// .ffpfsc) and on PS5 packages (.pkg), and two on a .p5z, each with the icon of MaxPack, shown for any number of files chosen,
// and starting MaxPack with --section and the file.
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
	want = append(want, verb(classes+`\SystemFileAssociations\.ffpkg\shell\MaxPack.Compress`, "Compress with MaxPack", "compress")...)
	want = append(want, verb(classes+`\SystemFileAssociations\.ffpfs\shell\MaxPack.Compress`, "Compress with MaxPack", "compress")...)
	want = append(want, verb(classes+`\SystemFileAssociations\.ffpfsc\shell\MaxPack.Compress`, "Compress with MaxPack", "compress")...)
	want = append(want, verb(classes+`\SystemFileAssociations\.pkg\shell\MaxPack.Compress`, "Compress with MaxPack", "compress")...)
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
		{Do: DeleteTree, Key: `Software\Classes\SystemFileAssociations\.ffpkg\shell\MaxPack.Compress`},
		{Do: DeleteTree, Key: `Software\Classes\SystemFileAssociations\.ffpfs\shell\MaxPack.Compress`},
		{Do: DeleteTree, Key: `Software\Classes\SystemFileAssociations\.ffpfsc\shell\MaxPack.Compress`},
		{Do: DeleteTree, Key: `Software\Classes\SystemFileAssociations\.pkg\shell\MaxPack.Compress`},
		{Do: DeleteTree, Key: `Software\Classes\SystemFileAssociations\.p5z\shell\MaxPack.1Verify`},
		{Do: DeleteTree, Key: `Software\Classes\SystemFileAssociations\.p5z\shell\MaxPack.2Details`},
	}
	if got := RemoveMenuOps("Software"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%v\nwant\n%v", got, want)
	}
}

// .p5z files open with MaxPack: its ProgID with its icon and --open, in Open with and in Default
// apps; the type opens with it by default only when no program had it (free). Whoever opens them,
// the Explorer shows their icon as their thumbnail with maxpack-thumbs.dll, next to MaxPack.
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
		{Do: SetValue, Key: classes + `\CLSID\{266D6364-17C0-4B3E-BB33-7EEB90C91837}`, Data: "MaxPack .p5z thumbnails"},
		{Do: SetValue, Key: classes + `\CLSID\{266D6364-17C0-4B3E-BB33-7EEB90C91837}\InprocServer32`, Data: `C:\Users\user\AppData\Local\Programs\MaxPack\maxpack-thumbs.dll`},
		{Do: SetValue, Key: classes + `\CLSID\{266D6364-17C0-4B3E-BB33-7EEB90C91837}\InprocServer32`, Name: "ThreadingModel", Data: "Apartment"},
		{Do: SetValue, Key: classes + `\SystemFileAssociations\.p5z\ShellEx\{e357fccd-a995-4576-b01f-234630154e96}`, Data: "{266D6364-17C0-4B3E-BB33-7EEB90C91837}"},
	}
	if got := AddFilesOps("Software", testExe, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("another program had .p5z: got\n%v\nwant\n%v", got, want)
	}
	free := append(append([]Op{}, want...), Op{Do: SetValue, Key: classes + `\.p5z`, Data: "MaxPack.p5z"})
	if got := AddFilesOps("Software", testExe, true); !reflect.DeepEqual(got, free) {
		t.Fatalf("no program had .p5z: got\n%v\nwant\n%v", got, free)
	}
}

// Removing them takes away what MaxPack wrote, the thumbnails too, and the default of .p5z and its
// thumbnail handler only while they are MaxPack's.
func TestRemoveFilesOps(t *testing.T) {
	want := []Op{
		{Do: DeleteValue, Key: `Software\Classes\.p5z\OpenWithProgids`, Name: "MaxPack.p5z"},
		{Do: DeleteOwnValue, Key: `Software\Classes\.p5z`, Data: "MaxPack.p5z"},
		{Do: DeleteTree, Key: `Software\Classes\MaxPack.p5z`},
		{Do: DeleteValue, Key: `Software\RegisteredApplications`, Name: "MaxPack"},
		{Do: DeleteTree, Key: `Software\MaxPack\Capabilities`},
		{Do: DeleteOwnValue, Key: `Software\Classes\SystemFileAssociations\.p5z\ShellEx\{e357fccd-a995-4576-b01f-234630154e96}`, Data: "{266D6364-17C0-4B3E-BB33-7EEB90C91837}"},
		{Do: DeleteTree, Key: `Software\Classes\CLSID\{266D6364-17C0-4B3E-BB33-7EEB90C91837}`},
	}
	if got := RemoveFilesOps("Software"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%v\nwant\n%v", got, want)
	}
}

// The thumbnail provider is the one next to the copy of MaxPack, whatever its folder.
func TestThumbsDLL(t *testing.T) {
	for executable, want := range map[string]string{
		testExe:                      `C:\Users\user\AppData\Local\Programs\MaxPack\maxpack-thumbs.dll`,
		`D:\Games\Tools\MaxPack.exe`: `D:\Games\Tools\maxpack-thumbs.dll`,
		`\\server\share\MaxPack.exe`: `\\server\share\maxpack-thumbs.dll`,
		`C:/Tools/MaxPack.exe`:       `C:/Tools/maxpack-thumbs.dll`,
	} {
		if got := thumbsDLL(executable); got != want {
			t.Errorf("%s: %s, want %s", executable, got, want)
		}
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
	for _, op := range AddFilesOps(software, testExe, false) {
		if op.Key == software+`\RegisteredApplications` && op.Data != software+`\MaxPack\Capabilities` {
			t.Errorf("RegisteredApplications names %q", op.Data)
		}
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

// On the images and the packages MaxPack only adds its entry to the menu: it never writes the
// program that opens them nor their icon (other tools open them), and every key it writes there is
// its own verb.
func TestMenuLeavesTheImagesAlone(t *testing.T) {
	images := []string{".exfat", ".ffpkg", ".ffpfs", ".ffpfsc", ".pkg"}
	for _, ops := range [][]Op{AddMenuOps("Software", testExe, testTexts), RemoveMenuOps("Software")} {
		for _, op := range ops {
			for _, ext := range images {
				if strings.Contains(op.Key+"\\", "\\"+ext+"\\") && !strings.HasPrefix(op.Key, `Software\Classes\SystemFileAssociations\`+ext+`\shell\MaxPack.Compress`) {
					t.Errorf("%v is not MaxPack's entry", op)
				}
			}
		}
	}
	for _, op := range AddFilesOps("Software", testExe, true) {
		for _, ext := range images {
			if strings.Contains(op.Key, ext) || op.Name == ext {
				t.Errorf("opening .p5z files writes %v", op)
			}
		}
	}
}

// The menu is this copy's when all its entries are; a menu an earlier MaxPack wrote, without the
// entries on .ffpkg, .ffpfs, .ffpfsc and .pkg, is still this copy's (it is completed when the window
// opens, see Relabel), and complete only with them. Anything else is moved: for another copy, or
// only in part.
func TestMenuPlace(t *testing.T) {
	all := func(place string) []string {
		places := make([]string, len(verbs))
		for i := range places {
			places[i] = place
		}
		return places
	}
	// the first entries as given, the later ones as later
	mixed := func(first, later string) []string {
		places := all(first)
		for i, v := range verbs {
			if v.later {
				places[i] = later
			}
		}
		return places
	}
	var later []string
	for _, v := range verbs {
		if v.later {
			later = append(later, v.class)
		}
	}
	if strings.Join(later, " ") != `SystemFileAssociations\.ffpkg SystemFileAssociations\.ffpfs SystemFileAssociations\.ffpfsc SystemFileAssociations\.pkg` {
		t.Fatalf("the entries a later MaxPack added: %v", later)
	}
	partFirst := all(Here)
	partFirst[0] = Missing
	for _, c := range []struct {
		name     string
		places   []string
		want     string
		complete bool
	}{
		{"none", all(Missing), Missing, false},
		{"all here", all(Here), Here, true},
		{"all for another copy", all(Moved), Moved, false},
		{"an earlier MaxPack's", mixed(Here, Missing), Here, false},
		{"an earlier MaxPack's, the new ones for another copy", mixed(Here, Moved), Moved, false},
		{"only the new ones", mixed(Missing, Here), Moved, false},
		{"one of the first ones gone", partFirst, Moved, false},
	} {
		if got := menuPlace(c.places); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if got := menuComplete(c.places); got != c.complete {
			t.Errorf("%s: complete %v", c.name, got)
		}
	}
}
