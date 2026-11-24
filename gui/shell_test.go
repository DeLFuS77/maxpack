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
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the installer does not have: %s", want)
		}
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
			want = `DeleteRegValue HKCU "` + op.Key + `" "` + op.Name + `"`
			if !strings.Contains(script, `ReadRegStr $1 HKCU "`+op.Key+`" "`+op.Name+`"`) || op.Data != "MaxPack.p5z" {
				t.Errorf("the uninstaller does not check %s before removing it", op.Key)
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
	for _, name := range []string{"ExplorerTitle", "ExplorerSub", "OptMenu", "OptMenuHint", "OptSendTo", "OptSendToHint", "OptFiles", "OptFilesHint", "ExplorerFailed"} {
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
