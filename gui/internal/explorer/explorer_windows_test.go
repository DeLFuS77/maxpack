// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package explorer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// testKeys writes the places under a key of the test, deleted at its end, and "Send to" in a
// folder of its own: the registry of the user and the Explorer are not touched.
func testKeys(t *testing.T) (software, sendTo string) {
	t.Helper()
	software = fmt.Sprintf(`Software\MaxPackTest-%d-%s`, os.Getpid(), t.Name())
	sendTo = t.TempDir()
	before, notify, folder := user, notifyShell, sendToFolder
	user = keys{software: software, merged: registry.CURRENT_USER, mergedPath: software + `\Classes`, fileExts: software + `\FileExts`}
	notifyShell = func() {}
	sendToFolder = func() (string, error) { return sendTo, nil }
	t.Cleanup(func() {
		user, notifyShell, sendToFolder = before, notify, folder
		if err := deleteTree(software); err != nil {
			t.Errorf("the key of the test stays: %v", err)
		}
		if exists(t, software) {
			t.Errorf("the key of the test stays")
		}
	})
	return software, sendTo
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	key.Close()
	return true
}

func value(path, name string) string { return readString(registry.CURRENT_USER, path, name) }

// Added, the menu holds its entries in the language given, each starting MaxPack with --section;
// it is for this copy, not for another; it is written again in another language only for this
// copy; removed, it goes with the keys it left empty, and what another program put stays.
func TestMenuAddedAndRemoved(t *testing.T) {
	software, _ := testKeys(t)
	classes := software + `\Classes`
	exe := `C:\Tools\MaxPack\MaxPack.exe`
	if err := writeString(classes+`\Directory\shell\Other`, "", "Another program"); err != nil {
		t.Fatal(err)
	}
	if got := Read(exe).Menu; got != Missing {
		t.Fatalf("before: %q", got)
	}
	if err := Add(Menu, exe, MenuTexts{Compress: "Compress with MaxPack", Verify: "Verify with MaxPack"}); err == nil {
		t.Fatal("a menu with a text missing")
	}
	if exists(t, classes+`\SystemFileAssociations`) {
		t.Fatal("a menu with a text missing was written")
	}
	texts := MenuTexts{Compress: "Compress with MaxPack", Verify: "Verify with MaxPack", Details: "Details"}
	if err := Add(Menu, exe, texts); err != nil {
		t.Fatal(err)
	}
	if got := Read(exe).Menu; got != Here {
		t.Fatalf("added: %q", got)
	}
	if got := Read(`D:\Elsewhere\MaxPack.exe`).Menu; got != Moved {
		t.Fatalf("for another copy: %q", got)
	}
	for path, want := range map[string]string{
		`\Directory\shell\MaxPack.Compress`:                     "Compress with MaxPack",
		`\SystemFileAssociations\.exfat\shell\MaxPack.Compress`: "Compress with MaxPack",
		`\SystemFileAssociations\.p5z\shell\MaxPack.1Verify`:    "Verify with MaxPack",
		`\SystemFileAssociations\.p5z\shell\MaxPack.2Details`:   "Details",
	} {
		if got := value(classes+path, "MUIVerb"); got != want {
			t.Errorf("%s: %q", path, got)
		}
		if got := value(classes+path, "MultiSelectModel"); got != "Player" {
			t.Errorf("%s: shown for %q", path, got)
		}
	}
	if got := value(classes+`\SystemFileAssociations\.p5z\shell\MaxPack.1Verify\command`, ""); got != `"C:\Tools\MaxPack\MaxPack.exe" --section verify "%1"` {
		t.Errorf("command: %s", got)
	}
	// one of its keys gone: it is not all there, so it is to be added again
	if err := deleteTree(classes + `\SystemFileAssociations\.exfat\shell\MaxPack.Compress`); err != nil {
		t.Fatal(err)
	}
	if got := Read(exe).Menu; got != Moved {
		t.Fatalf("in part: %q", got)
	}
	if err := Add(Menu, exe, texts); err != nil {
		t.Fatal(err)
	}

	other := MenuTexts{Compress: "Comprimir con MaxPack", Verify: "Verificar con MaxPack", Details: "Detalles"}
	if err := Relabel(`D:\Elsewhere\MaxPack.exe`, other); err != nil {
		t.Fatal(err)
	}
	if got := value(classes+`\SystemFileAssociations\.p5z\shell\MaxPack.2Details`, "MUIVerb"); got != "Details" {
		t.Errorf("written again for another copy: %q", got)
	}
	if err := Relabel(exe, other); err != nil {
		t.Fatal(err)
	}
	if got := value(classes+`\SystemFileAssociations\.p5z\shell\MaxPack.2Details`, "MUIVerb"); got != "Detalles" {
		t.Errorf("not written again: %q", got)
	}

	if err := Remove(Menu); err != nil {
		t.Fatal(err)
	}
	if got := Read(exe).Menu; got != Missing {
		t.Fatalf("removed: %q", got)
	}
	if exists(t, classes+`\SystemFileAssociations`) {
		t.Error("SystemFileAssociations stays, empty")
	}
	if !exists(t, classes+`\Directory\shell\Other`) {
		t.Error("the entry of another program went")
	}
	if exists(t, classes+`\Directory\shell\MaxPack.Compress`) {
		t.Error("the entry on folders stays")
	}
	// removing what is not there is no error
	if err := Remove(Menu); err != nil {
		t.Fatal(err)
	}
	if err := Relabel(exe, other); err != nil || exists(t, classes+`\SystemFileAssociations`) {
		t.Fatalf("written again while it was not there: %v", err)
	}
}

// With .p5z free, MaxPack opens it with a double click (--open); removed, all of it goes.
func TestFilesWhenFree(t *testing.T) {
	software, _ := testKeys(t)
	classes := software + `\Classes`
	exe := `C:\Tools\MaxPack\MaxPack.exe`
	if err := writeString(software+`\RegisteredApplications`, "Other", `Software\Other\Capabilities`); err != nil {
		t.Fatal(err)
	}
	if err := Add(Files, exe, MenuTexts{}); err != nil {
		t.Fatal(err)
	}
	state := Read(exe)
	if state.Files != Here || state.FilesOther {
		t.Fatalf("added: %+v", state)
	}
	if got := value(classes+`\.p5z`, ""); got != ProgID {
		t.Errorf(".p5z opens with %q", got)
	}
	if got := value(classes+`\`+ProgID+`\shell\open\command`, ""); got != `"C:\Tools\MaxPack\MaxPack.exe" --open "%1"` {
		t.Errorf("command: %s", got)
	}
	if got := value(classes+`\`+ProgID+`\DefaultIcon`, ""); got != `"C:\Tools\MaxPack\MaxPack.exe",0` {
		t.Errorf("icon: %s", got)
	}
	if !exists(t, classes+`\.p5z\OpenWithProgids`) || value(software+`\RegisteredApplications`, "MaxPack") != software+`\MaxPack\Capabilities` {
		t.Error("not in Open with or in Default apps")
	}
	if got := value(software+`\MaxPack\Capabilities\FileAssociations`, ".p5z"); got != ProgID {
		t.Errorf("Default apps: %q", got)
	}
	if got := Read(`D:\Elsewhere\MaxPack.exe`).Files; got != Moved {
		t.Errorf("for another copy: %q", got)
	}

	if err := Remove(Files); err != nil {
		t.Fatal(err)
	}
	if got := Read(exe); got.Files != Missing || got.FilesOther {
		t.Fatalf("removed: %+v", got)
	}
	for _, path := range []string{classes + `\` + ProgID, classes + `\.p5z`, software + `\MaxPack`} {
		if exists(t, path) {
			t.Errorf("%s stays", path)
		}
	}
	if value(software+`\RegisteredApplications`, "Other") == "" {
		t.Error("another program left Default apps")
	}
	if err := Remove(Files); err != nil {
		t.Fatal(err)
	}
}

// A .p5z another program opens is left to it, as Windows wants: MaxPack is only in Open with, and
// says the user has to choose it; once chosen with Open with › Always, it is MaxPack's. Removed,
// the other program still opens it.
func TestFilesOfAnotherProgram(t *testing.T) {
	software, _ := testKeys(t)
	classes := software + `\Classes`
	exe := `C:\Tools\MaxPack\MaxPack.exe`
	if err := writeString(classes+`\.p5z`, "", "Other.File"); err != nil {
		t.Fatal(err)
	}
	if err := Add(Files, exe, MenuTexts{}); err != nil {
		t.Fatal(err)
	}
	if got := value(classes+`\.p5z`, ""); got != "Other.File" {
		t.Fatalf("the program of .p5z was taken: %q", got)
	}
	if state := Read(exe); state.Files != Here || !state.FilesOther {
		t.Fatalf("added: %+v", state)
	}
	// chosen with Open with › Always (Windows 11 24H2 keeps it in UserChoiceLatest)
	if err := writeString(software+`\FileExts\.p5z\UserChoiceLatest\ProgId`, "ProgId", ProgID); err != nil {
		t.Fatal(err)
	}
	if state := Read(exe); state.FilesOther {
		t.Fatalf("chosen: %+v", state)
	}
	if err := writeString(software+`\FileExts\.p5z\UserChoiceLatest\ProgId`, "ProgId", "Another.File"); err != nil {
		t.Fatal(err)
	}
	if state := Read(exe); !state.FilesOther {
		t.Fatalf("another chosen: %+v", state)
	}
	if err := Remove(Files); err != nil {
		t.Fatal(err)
	}
	if got := value(classes+`\.p5z`, ""); got != "Other.File" {
		t.Errorf("the program of .p5z went: %q", got)
	}
	if exists(t, classes+`\.p5z\OpenWithProgids`) {
		t.Error("MaxPack stays in Open with")
	}
}

// "Send to" holds a shortcut to MaxPack, for this copy; removed, it goes.
func TestSendToAddedAndRemoved(t *testing.T) {
	_, folder := testKeys(t)
	exe := filepath.Join(t.TempDir(), "MaxPack.exe")
	os.WriteFile(exe, []byte("MZ"), 0o755)
	if got := Read(exe).SendTo; got != Missing {
		t.Fatalf("before: %q", got)
	}
	if err := Add(SendTo, exe, MenuTexts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, "MaxPack.lnk")); err != nil {
		t.Fatal(err)
	}
	if got := Read(exe).SendTo; got != Here {
		t.Fatalf("added: %q", got)
	}
	if got := Read(filepath.Join(t.TempDir(), "MaxPack.exe")).SendTo; got != Moved {
		t.Fatalf("for another copy: %q", got)
	}
	if err := Remove(SendTo); err != nil {
		t.Fatal(err)
	}
	if got := Read(exe).SendTo; got != Missing {
		t.Fatalf("removed: %q", got)
	}
	if err := Remove(SendTo); err != nil {
		t.Fatal(err)
	}
	if err := Add("other", exe, MenuTexts{}); err == nil {
		t.Error("an unknown place")
	}
	if err := Remove("other"); err == nil {
		t.Error("an unknown place removed")
	}
}

// The shortcut is made in any folder: one named O’Brien, one with letters out of the code page of
// the system, and one named to run code if it went through a script, which runs nothing. It is read
// back, to tell whether it points to this program.
func TestShortcutInAnyFolder(t *testing.T) {
	base := t.TempDir()
	// what a name made to run code would leave, in the working folder
	ran := "maxpack-test-ran"
	for _, name := range []string{"O’Brien", "it's ‘here’", "日本語", "x’); New-Item " + ran + "; (’", "x'); New-Item " + ran + "; ('"} {
		folder := filepath.Join(base, name)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(folder, "MaxPack.exe")
		os.WriteFile(target, []byte("MZ"), 0o755)
		shortcut := filepath.Join(folder, "MaxPack.lnk")
		if err := makeShortcut(shortcut, target); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !shortcutTo(shortcut, target) || shortcutTo(shortcut, filepath.Join(base, "other.exe")) {
			t.Fatalf("%s: to this program, and not to another", name)
		}
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a folder name ran code")
	}
	// a file that is not a shortcut is taken as it is
	other := filepath.Join(base, "other.lnk")
	os.WriteFile(other, []byte("not a shortcut"), 0o644)
	if _, err := shortcutTarget(other); err == nil || !shortcutTo(other, "x") {
		t.Fatal("a file that is not a shortcut")
	}
}

// shortcutFile is a shortcut with only a LinkInfo, its local path in the code page of the system
// or, with wide, in UTF-16 too.
func shortcutFile(path string, wide bool) []byte {
	header := 0x1C
	inCodePage := path
	if wide {
		header, inCodePage = 0x24, "?"
	}
	info := make([]byte, header)
	binary.LittleEndian.PutUint32(info[4:], uint32(header))
	binary.LittleEndian.PutUint32(info[8:], 1)
	binary.LittleEndian.PutUint32(info[0x10:], uint32(len(info)))
	info = append(append(info, inCodePage...), 0)
	binary.LittleEndian.PutUint32(info[0x18:], uint32(len(info)))
	info = append(info, 0)
	if wide {
		binary.LittleEndian.PutUint32(info[0x1C:], uint32(len(info)))
		for _, unit := range utf16.Encode([]rune(path)) {
			info = binary.LittleEndian.AppendUint16(info, unit)
		}
		info = append(info, 0, 0)
		binary.LittleEndian.PutUint32(info[0x20:], uint32(len(info)))
		info = append(info, 0, 0)
	}
	binary.LittleEndian.PutUint32(info, uint32(len(info)))
	data := make([]byte, 0x4C)
	binary.LittleEndian.PutUint32(data, 0x4C)
	binary.LittleEndian.PutUint32(data[0x14:], 1<<1)
	return append(data, info...)
}

// The local path of a shortcut is read in both forms it is stored in, and a damaged one is not.
func TestShortcutTargetForms(t *testing.T) {
	folder := t.TempDir()
	for path, wide := range map[string]bool{`C:\Games\MaxPack.exe`: false, `C:\ゲーム\MaxPack.exe`: true} {
		shortcut := filepath.Join(folder, "test.lnk")
		os.WriteFile(shortcut, shortcutFile(path, wide), 0o644)
		if got, err := shortcutTarget(shortcut); err != nil || got != path {
			t.Errorf("%s: %q %v", path, got, err)
		}
	}
	whole := shortcutFile(`C:\Games\MaxPack.exe`, true)
	for cut := 0; cut < len(whole); cut += 7 {
		shortcut := filepath.Join(folder, "cut.lnk")
		os.WriteFile(shortcut, whole[:cut], 0o644)
		shortcutTarget(shortcut) // never panics
	}
}

// MP_TEST_EXPLORER=1 go test ./internal/explorer/ adds the three places for real, for this user
// (HKEY_CURRENT_USER\Software\Classes and the Send to folder), checks that the Explorer opens a
// .p5z with MaxPack when no program had it, and removes them. It does nothing when MaxPack is
// there already, so it never takes away what the user chose.
func TestRealPlaces(t *testing.T) {
	if os.Getenv("MP_TEST_EXPLORER") != "1" {
		t.Skip("MP_TEST_EXPLORER=1 adds MaxPack to this user's Explorer for a moment, and removes it")
	}
	exe := filepath.Join(t.TempDir(), "MaxPack.exe")
	os.WriteFile(exe, []byte("MZ"), 0o755)
	before := Read(exe)
	if before.Menu != Missing || before.SendTo != Missing || before.Files != Missing {
		t.Skipf("MaxPack is in the Explorer already: %+v", before)
	}
	free := readString(registry.CLASSES_ROOT, ".p5z", "") == ""
	t.Cleanup(func() {
		for _, place := range Places {
			if err := Remove(place); err != nil {
				t.Errorf("%s: %v", place, err)
			}
		}
		if after := Read(exe); after.Menu != Missing || after.SendTo != Missing || after.Files != Missing {
			t.Errorf("left behind: %+v", after)
		}
		for _, path := range []string{`Software\Classes\` + ProgID, `Software\MaxPack`, `Software\Classes\SystemFileAssociations\.p5z\shell\MaxPack.1Verify`} {
			if exists(t, path) {
				t.Errorf("%s stays", path)
			}
		}
	})
	texts := MenuTexts{Compress: "Compress with MaxPack", Verify: "Verify with MaxPack", Details: "Details"}
	for _, place := range Places {
		if err := Add(place, exe, texts); err != nil {
			t.Fatalf("%s: %v", place, err)
		}
	}
	state := Read(exe)
	if state.Menu != Here || state.SendTo != Here || state.Files != Here {
		t.Fatalf("added: %+v", state)
	}
	if free {
		if state.FilesOther {
			t.Error(".p5z was free, and opens with another program")
		}
		if got, err := assocCommand(".p5z", "open"); err != nil || got != Command(exe, "--open") {
			t.Errorf("the Explorer opens a .p5z with %q (%v)", got, err)
		}
	}
	if got, err := assocCommand(".p5z", "MaxPack.1Verify"); err != nil || got != Command(exe, "--section", "verify") {
		t.Errorf("the Explorer verifies a .p5z with %q (%v)", got, err)
	}
}

// assocCommand is the command the Explorer runs for a verb of a type, as it sees the classes of
// the computer and of the user together (AssocQueryString).
func assocCommand(extension, verb string) (string, error) {
	const command = 1 // ASSOCSTR_COMMAND
	ext, _ := windows.UTF16PtrFromString(extension)
	name, _ := windows.UTF16PtrFromString(verb)
	buffer := make([]uint16, 2048)
	size := uint32(len(buffer))
	result, _, _ := windows.NewLazySystemDLL("shlwapi.dll").NewProc("AssocQueryStringW").Call(0, command,
		uintptr(unsafe.Pointer(ext)), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if int32(result) < 0 {
		return "", windows.Errno(result)
	}
	return windows.UTF16ToString(buffer), nil
}
