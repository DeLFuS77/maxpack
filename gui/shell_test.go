// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/explorer"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// fakeShell stands in for the Explorer: it keeps what was added and removed, and never writes the
// registry of the user.
type fakeShell struct {
	calls []string
	state explorer.State
	fail  error
}

func useShell(t *testing.T, portableCopy bool) *fakeShell {
	t.Helper()
	fake := &fakeShell{state: explorer.State{Available: true}}
	before, beforePortable := shell, isPortable
	shell = places{
		available:  func() bool { return true },
		executable: func() (string, error) { return `C:\Programs\MaxPack\MaxPack.exe`, nil },
		read:       func(exe string) explorer.State { return fake.state },
		add: func(place, exe string, texts explorer.MenuTexts) error {
			fake.calls = append(fake.calls, "add "+place+" "+exe+" "+texts.Compress+"|"+texts.Verify+"|"+texts.Details)
			return fake.fail
		},
		remove: func(place string) error {
			fake.calls = append(fake.calls, "remove "+place)
			return fake.fail
		},
		relabel: func(exe string, texts explorer.MenuTexts) error {
			fake.calls = append(fake.calls, "relabel "+texts.Details)
			return fake.fail
		},
	}
	isPortable = func() bool { return portableCopy }
	t.Cleanup(func() { shell, isPortable = before, beforePortable })
	return fake
}

var english = explorer.MenuTexts{Compress: "Compress with MaxPack", Verify: "Verify with MaxPack", Details: "Details"}

// Settings › Windows Explorer adds and removes each place for this copy of MaxPack, with the texts
// of the page, and tells what is there after.
func TestSetExplorerPlace(t *testing.T) {
	fake := useShell(t, false)
	a := &App{}
	fake.state.Menu = explorer.Here
	state, err := a.SetExplorerPlace("menu", true, english)
	if err != nil || state.Menu != explorer.Here {
		t.Fatalf("%+v %v", state, err)
	}
	if _, err := a.SetExplorerPlace("files", false, explorer.MenuTexts{}); err != nil {
		t.Fatal(err)
	}
	a.RelabelExplorerMenu(explorer.MenuTexts{Compress: "a", Verify: "b", Details: "Detalles"})
	want := []string{`add menu C:\Programs\MaxPack\MaxPack.exe Compress with MaxPack|Verify with MaxPack|Details`, "remove files", "relabel Detalles"}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("%q", fake.calls)
	}
	fake.fail = errors.New("denied")
	if _, err := a.SetExplorerPlace("sendto", true, english); err == nil {
		t.Fatal("a failure is not told")
	}
	if got := a.ExplorerPlaces(); !got.Available || got.Menu != explorer.Here {
		t.Fatalf("%+v", got)
	}
}

// A portable MaxPack writes nothing in Windows: not from Settings, not from the installer. Settings
// knows it from Setup.
func TestPortableWritesNothing(t *testing.T) {
	fake := useShell(t, true)
	a := &App{}
	if _, err := a.SetExplorerPlace("menu", true, english); !errors.Is(err, errPortable) {
		t.Fatalf("added in a portable copy: %v", err)
	}
	if _, err := a.SetExplorerPlace("menu", false, english); !errors.Is(err, errPortable) {
		t.Fatalf("removed in a portable copy: %v", err)
	}
	a.RelabelExplorerMenu(english)
	var out, errs bytes.Buffer
	if code := runInstaller([]string{"add", "menu"}, &out, &errs); code == 0 {
		t.Fatalf("the installer added in a portable copy: %s", out.String())
	}
	if code := runInstaller([]string{"remove"}, &out, &errs); code == 0 {
		t.Fatal("the installer removed in a portable copy")
	}
	if len(fake.calls) != 0 {
		t.Fatalf("written: %q", fake.calls)
	}
	useSettings(t, t.TempDir())
	if setup := a.Setup(); !setup.Portable || !setup.Explorer {
		t.Fatalf("setup: %+v", setup)
	}
}

// The installer adds the places chosen, the menu in the language of the installer; it removes all
// of MaxPack's, whatever copy they are for; anything else is a usage error.
func TestInstaller(t *testing.T) {
	fake := useShell(t, false)
	var out, errs bytes.Buffer
	if code := runInstaller([]string{"add", "files", "menu", "sendto", "--language", "es"}, &out, &errs); code != 0 {
		t.Fatalf("%d: %s", code, errs.String())
	}
	es := menuTexts("es")
	want := []string{
		`add files C:\Programs\MaxPack\MaxPack.exe ` + es.Compress + "|" + es.Verify + "|" + es.Details,
		`add menu C:\Programs\MaxPack\MaxPack.exe ` + es.Compress + "|" + es.Verify + "|" + es.Details,
		`add sendto C:\Programs\MaxPack\MaxPack.exe ` + es.Compress + "|" + es.Verify + "|" + es.Details,
	}
	if !reflect.DeepEqual(fake.calls, want) || strings.TrimSpace(out.String()) != "done" {
		t.Fatalf("%q, %q", fake.calls, out.String())
	}
	fake.calls = nil
	if code := runInstaller([]string{"remove"}, &out, &errs); code != 0 || !reflect.DeepEqual(fake.calls, []string{"remove menu", "remove sendto", "remove files"}) {
		t.Fatalf("%d %q", code, fake.calls)
	}
	fake.calls = nil
	for _, args := range [][]string{nil, {"add", "everything"}, {"remove", "menu"}, {"add", "--language"}, {"other"}} {
		errs.Reset()
		if code := runInstaller(args, &out, &errs); code == 0 || errs.Len() == 0 {
			t.Errorf("%q: %d", args, code)
		}
	}
	if len(fake.calls) != 0 {
		t.Fatalf("a usage error did something: %q", fake.calls)
	}
	// nothing chosen is nothing to do; a language the window has not is English
	if code := runInstaller([]string{"add", "--language", "xx"}, &out, &errs); code != 0 || len(fake.calls) != 0 {
		t.Fatalf("%d %q", code, fake.calls)
	}
	if code := runInstaller([]string{"add", "menu", "--language", "xx"}, &out, &errs); code != 0 || !strings.HasSuffix(fake.calls[0], english.Compress+"|"+english.Verify+"|"+english.Details) {
		t.Fatalf("%d %q", code, fake.calls)
	}
	fake.calls, fake.fail = nil, errors.New("denied")
	if code := runInstaller([]string{"add", "menu"}, &out, &errs); code == 0 {
		t.Fatal("a failure is not told")
	}
}

// The menu the installer writes speaks the language of the installer, in the words of the page,
// which writes it again in its own language (RelabelExplorerMenu).
func TestMenuTexts(t *testing.T) {
	if got := menuTexts("en"); got != english {
		t.Fatalf("English: %+v", got)
	}
	line := func(lang, key string) string {
		found := regexp.MustCompile(`(?m)^  ` + key + `: "(.*)",$`).FindStringSubmatch(readPage(t, "i18n/"+lang+".js"))
		if found == nil {
			t.Fatalf("%s.js has no %s", lang, key)
		}
		return found[1]
	}
	for _, lang := range settings.Languages {
		got := menuTexts(lang)
		if got.Compress != line(lang, "menuCompress") || got.Verify != line(lang, "menuVerify") || got.Details != line(lang, "menuDetails") {
			t.Errorf("%s: %+v", lang, got)
		}
		if !strings.Contains(got.Compress, "MaxPack") || !strings.Contains(got.Verify, "MaxPack") {
			t.Errorf("%s: the menu does not say MaxPack: %+v", lang, got)
		}
	}
	if got := menuTexts("xx"); got != english {
		t.Fatalf("unknown: %+v", got)
	}
}

// The installer of Windows (build/windows/installer/maxpack.nsi), which cannot be built here: it
// offers the three places, none ticked, in the eleven languages; MaxPack adds the ones ticked and
// takes all of them away; and when MaxPack cannot run, the uninstaller takes away the very keys
// and values MaxPack writes, and the default of .p5z only while it is MaxPack's.
func TestInstallerScript(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("build", "windows", "installer", "maxpack.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "${CLASSES}", `Software\Classes`)
	for _, want := range []string{
		`nsExec::ExecToLog '"$INSTDIR\MaxPack.exe" --installer add$0 --language $1'`,
		`nsExec::ExecToLog '"$INSTDIR\MaxPack.exe" --installer remove'`,
		`StrCpy $0 "$0 menu"`, `StrCpy $0 "$0 sendto"`, `StrCpy $0 "$0 files"`,
		`Page custom ExplorerPage ExplorerLeave`,
		`Delete "$SENDTO\MaxPack.lnk"`,
		`${If} $1 == "MaxPack.p5z"`,
		// the thumbnail provider: installed next to MaxPack, set aside when the Explorer has it loaded
		// (deleted then if it can, else by the next update or uninstall), and its keys taken away with
		// the other places
		`File "/oname=maxpack-thumbs.dll" "${THUMBS}"`,
		`Rename "$INSTDIR\maxpack-thumbs.dll" "$INSTDIR\maxpack-thumbs-$0.old"`,
		`Delete "$INSTDIR\maxpack-thumbs-$0.old"`,
		`Delete "$INSTDIR\maxpack-thumbs-*.old"`,
		// at the uninstall, one still loaded goes to the temporary folder, so that the install folder can go
		// (on another drive, it is set aside there); a delete at the restart is left only for that copy,
		// never for the DLL's own path, which a later install would have
		`Rename "$INSTDIR\maxpack-thumbs.dll" "$TEMP\maxpack-thumbs-$0.old"`,
		`Delete /REBOOTOK "$TEMP\maxpack-thumbs-$0.old"`,
		`Delete /REBOOTOK "$INSTDIR\maxpack-thumbs-$0.old"`,
		// and so does a copy an update set aside, which the Explorer may still have loaded too
		`FindFirst $1 $2 "$INSTDIR\maxpack-thumbs-*.old"`,
		`Rename "$INSTDIR\$2" "$TEMP\$2"`,
		`Delete /REBOOTOK "$TEMP\$2"`,
		`DeleteRegKey HKCU "Software\Classes\CLSID\{266D6364-17C0-4B3E-BB33-7EEB90C91837}"`,
		// the finish page never offers to restart the PC
		`!define MUI_FINISHPAGE_NOREBOOTSUPPORT`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the installer does not have: %s", want)
		}
	}
	// An install or an update never asks to restart the PC: a delete left for the restart (/REBOOTOK) raises
	// NSIS's reboot flag, and the finish page would then offer "Reboot now", ticked. Only the uninstaller,
	// which has no finish page, may leave one.
	install := script
	if start := strings.Index(script, `Section "Uninstall"`); start >= 0 {
		if end := strings.Index(script[start:], "SectionEnd"); end >= 0 {
			install = script[:start] + script[start+end:]
		}
	}
	if strings.Contains(install, "/REBOOTOK") || strings.Contains(install, "SetRebootFlag true") {
		t.Error("the install may set the reboot flag")
	}
	// an update while the DLL is held without delete sharing (an antivirus checking it): the set-aside is checked,
	// tried in the temporary folder as the uninstall does, and a DLL nothing could move stays as it is, with no
	// Abort/Retry/Ignore question (SetOverwrite off for that File only)
	thumbs := install[strings.Index(install, `Delete "$INSTDIR\maxpack-thumbs-*.old"`):]
	thumbs = thumbs[:strings.Index(thumbs, `File "/oname=LICENSE.txt"`)]
	for _, want := range []string{"ClearErrors\n    " + `Rename "$INSTDIR\maxpack-thumbs.dll" "$INSTDIR\maxpack-thumbs-$0.old"` + "\n    ${IfNot} ${Errors}",
		`DetailPrint "$(ThumbsKept)"`,
		"SetOverwrite off\n  ${EndIf}\n  " + `File "/oname=maxpack-thumbs.dll" "${THUMBS}"` + "\n  SetOverwrite on"} {
		if !strings.Contains(thumbs, want) {
			t.Errorf("the update of the thumbnail provider does not have: %s", want)
		}
	}
	// a rename needs delete access, so one to the temporary folder fails as the first did: the update does not try it
	if strings.Contains(thumbs, `"$TEMP\maxpack-thumbs-$0.old"`) {
		t.Error("the update tries a set-aside that cannot work")
	}
	if strings.Contains(script, `/REBOOTOK "$INSTDIR\maxpack-thumbs.dll"`) {
		t.Error("a delete at the restart of the thumbnail provider's own path, which a later install would lose")
	}
	if uninstall := script[strings.Index(script, `Section "Uninstall"`):]; !strings.Contains(uninstall,
		`System::Call "kernel32::GetTickCount() i .r0"`+"\n    "+`Rename "$INSTDIR\maxpack-thumbs.dll" "$TEMP\maxpack-thumbs-$0.old"`) {
		t.Error("the uninstaller does not name the copy it sets aside")
	}
	if no, page := strings.Index(script, "!define MUI_FINISHPAGE_NOREBOOTSUPPORT"), strings.Index(script, "!insertmacro MUI_PAGE_FINISH"); no < 0 || no > page {
		t.Error("the finish page has its restart")
	}
	if strings.Contains(script, "MUI_UNPAGE_FINISH") {
		t.Error("the uninstaller has a finish page, which may offer to restart")
	}
	if strings.Contains(script, "NSD_Check") || strings.Count(script, "${BST_UNCHECKED}") != 3 {
		t.Error("a place is ticked by default")
	}
	var removal []explorer.Op
	removal = append(removal, explorer.RemoveMenuOps("Software")...)
	removal = append(removal, explorer.RemoveFilesOps("Software")...)
	for _, op := range removal {
		var want string
		switch op.Do {
		case explorer.DeleteTree:
			want = `DeleteRegKey HKCU "` + op.Key + `"`
		case explorer.DeleteValue:
			want = `DeleteRegValue HKCU "` + op.Key + `" "` + op.Name + `"`
		case explorer.DeleteOwnValue:
			// read, and removed only while it is MaxPack's: the uninstaller's own check, right before it
			want = `DeleteRegValue HKCU "` + op.Key + `" "` + op.Name + `"`
			check := `ReadRegStr $1 HKCU "` + op.Key + `" "` + op.Name + `"` + "\n  ${If} $1 == \"" + op.Data + "\"\n    " + want
			if !strings.Contains(script, check) {
				t.Errorf("the uninstaller does not check %s holds %s before removing it", op.Key, op.Data)
			}
		}
		if !strings.Contains(script, want) {
			t.Errorf("the uninstaller does not have: %s", want)
		}
	}
	nsisLanguages := []string{"ENGLISH", "RUSSIAN", "SIMPCHINESE", "GERMAN", "SPANISH", "FRENCH", "JAPANESE", "PORTUGUESEBR", "ITALIAN", "KOREAN", "ARABIC"}
	if len(nsisLanguages) != len(settings.Languages) {
		t.Fatal("the installer and the window have other languages")
	}
	for _, name := range []string{"ExplorerTitle", "ExplorerSub", "OptMenu", "OptMenuHint", "OptSendTo", "OptSendToHint", "OptFiles", "OptFilesHint", "ExplorerFailed", "ThumbsKept"} {
		if !strings.Contains(script, "$("+name+")") {
			t.Errorf("%s is not used", name)
		}
		for _, lang := range nsisLanguages {
			found := regexp.MustCompile(`(?m)^LangString ` + name + ` \$\{LANG_` + lang + `\} "([^"]+)"$`).FindStringSubmatch(script)
			if found == nil {
				t.Errorf("%s has no %s", name, lang)
			} else if strings.Contains(installerEnglish(name), "MaxPack") && !strings.Contains(found[1], "MaxPack") {
				t.Errorf("%s in %s lost MaxPack", name, lang)
			}
		}
	}
	// the switch of .p5z files is also the one of their covers, and says so, in the installer (with room
	// for three lines) and in Settings
	if !strings.Contains(installerEnglish("OptFilesHint"), "cover") || !strings.Contains(script, `${NSD_CreateLabel} 12u 92u 288u 28u "$(OptFilesHint)"`) {
		t.Errorf("the installer's switch of .p5z files does not say it gives their covers: %q", installerEnglish("OptFilesHint"))
	}
	if hint := regexp.MustCompile(`(?m)^  explorerFilesHint: "(.*)",\r?$`).FindStringSubmatch(readPage(t, "i18n/en.js")); hint == nil || !strings.Contains(hint[1], "cover") {
		t.Errorf("the switch of .p5z files in Settings does not say it gives their covers: %q", hint)
	}
	// the languages of the installer give the codes of the window
	for _, lang := range settings.Languages[1:] {
		if !strings.Contains(script, `StrCpy $1 "`+lang+`"`) {
			t.Errorf("the installer does not give the menu in %s", lang)
		}
	}
}

// installerEnglish is the English text of the installer for a name.
func installerEnglish(name string) string {
	data, _ := os.ReadFile(filepath.Join("build", "windows", "installer", "maxpack.nsi"))
	found := regexp.MustCompile(`(?m)^LangString ` + name + ` \$\{LANG_ENGLISH\} "([^"]+)"\r?$`).FindStringSubmatch(string(data))
	if found == nil {
		return ""
	}
	return found[1]
}
