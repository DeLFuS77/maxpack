// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"

	"golang.org/x/net/html"
)

// The page is checked from its files, as NXPress checks its own: its texts, that it writes data
// only as text, and that it has the sections and choices the window needs.

func readPage(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("frontend", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// a text of a language file is one line that starts with two spaces, its name and a colon
var textLine = regexp.MustCompile(`(?m)^  ([A-Za-z][A-Za-z0-9_]*):`)

func textKeys(t *testing.T, lang string) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, found := range textLine.FindAllStringSubmatch(readPage(t, "i18n/"+lang+".js"), -1) {
		if keys[found[1]] {
			t.Errorf("%s: the text %s appears twice", lang, found[1])
		}
		keys[found[1]] = true
	}
	if len(keys) == 0 {
		t.Fatalf("%s: no texts", lang)
	}
	return keys
}

// pageCodes are the names of the texts of what the window tells the page: the states of a game,
// the phases of its progress, the warnings of a plan and the codes of a detail (why a game
// failed or was skipped), with the ones the page gives itself.
func pageCodes() []string {
	states := []string{jobs.StateWaiting, jobs.StateQueued, jobs.StatePacking, jobs.StateVerifying, jobs.StateUnpacking,
		jobs.StateProtecting, jobs.StateRepairing, jobs.StateOK, jobs.StateError, jobs.StateSkipped, jobs.StateCancelled}
	warnings := []string{"fat32", "needs_runtime", "may_not_fit", "duplicate_target", "no_title_id"}
	details := []string{
		// blocks of a plan, which the Runner gives as the detail of a skip
		"same_as_source", "no_space", "fat32_too_big", "exists",
		// the Runner
		"dest_missing", "tmp_not_deleted", "partial_not_deleted", "old_not_deleted", "swap_failed",
		"runtime_without_zstd",
		"verify_failed", "files_bad", "exit", "cancelled", "io",
		// what became of an original once checked
		"original_trashed", "original_deleted", "original_kept_no_bin", "original_kept_too_big",
		"original_kept_too_long", "original_not_removed",
		// Rename
		"same_name", "name_taken", "duplicate_target", "cannot_read", "in_use", "gone",
		// the engine
		"bad_input", "too_big",
		// the repair data of a .p5z: made, added, and used to repair it (jobs/repair.go)
		"repair_data_failed", "repair_data_cancelled", "repair_data_other", "repair_data_unreadable", "repair_data_same",
		"no_par2", "not_p5z", "no_repair_data", "repair_folder_exists", "repair_no_space", "repair_failed",
		"repair_verify_failed", "repair_place_failed", "repair_restore_failed", "repair_backup_not_deleted", "repaired",
		"repair_not_needed", "old_repair_data_not_deleted", "repair_not_possible",
		// the page: skipped by the user, a game that cannot go, and anything else
		"skipped_by_user", "already_packed", "not_packed", "other",
	}
	// the phases of the progress of the engine (phase_code in src/cli/main.c), and of par2
	phases := []string{"copying", "packing", "verifying", "unpacking", "working", "protecting", "repairing"}
	var keys []string
	for _, s := range states {
		keys = append(keys, "state_"+s)
	}
	for _, p := range phases {
		keys = append(keys, "phase_"+p)
	}
	for _, w := range warnings {
		keys = append(keys, "warn_"+w)
	}
	for _, d := range details {
		keys = append(keys, "detail_"+d)
	}
	return keys
}

// textChecks runs in node over the folder of the page: it loads the texts as the page does, and
// prints one line for each problem it finds. argv: the folder, the codes of the languages (the ones
// of the window, en first) and a file with the names of the texts each language file has on a line
// of its own.
const textChecks = `
"use strict";
const fs = require("fs"), path = require("path"), vm = require("vm");
const dir = process.argv[1];
const codes = JSON.parse(process.argv[2]);
const keys = JSON.parse(fs.readFileSync(process.argv[3], "utf8"));
const problems = [];
const say = text => problems.push(text);
const context = { size: bytes => bytes + " B", time: seconds => seconds + " s", navigator: {} };
vm.createContext(context);
let source = "";
for (const name of ["base", ...codes]) source += fs.readFileSync(path.join(dir, "i18n", name + ".js"), "utf8") + "\n;\n";
vm.runInContext(source + "this.texts = texts; this.languages = languages; this.systemLanguage = systemLanguage; this.languageInfo = languageInfo;", context);
const { texts, languages, systemLanguage, languageInfo } = context;
if (languages.map(language => language[0]).join() !== codes.join()) say("the languages are " + languages.map(language => language[0]).join() + ", not " + codes.join());
for (const language of languages) {
  if (!language[1] || !language[1].trim()) say("the language " + language[0] + " has no name of its own");
}
if (languages.filter(language => language[3]).map(language => language[0]).join() !== "ar") say("the languages written from right to left are not just ar");
const en = texts.en;
for (const code of codes) {
  const table = texts[code];
  if (!table) { say(code + ": no texts"); continue; }
  const unread = Object.keys(table).filter(key => !keys[code].includes(key));
  if (unread.length) say(code + ": texts that are not on a line of their own: " + unread.join(", "));
  const missing = Object.keys(en).filter(key => !(key in table)), extra = Object.keys(table).filter(key => !(key in en));
  if (missing.length) say(code + ": missing texts: " + missing.join(", "));
  if (extra.length) say(code + ": texts that en does not have: " + extra.join(", "));
  for (const key of Object.keys(en)) {
    if (!(key in table)) continue;
    if (typeof en[key] !== typeof table[key]) say(key + ": in en " + typeof en[key] + ", in " + code + " " + typeof table[key]);
    else if (typeof en[key] === "function" && en[key].length !== table[key].length) say(key + ": in en it takes " + en[key].length + " arguments, in " + code + " " + table[key].length);
  }
  for (const [key, value] of Object.entries(table)) {
    if (typeof value === "string") { if (!value.trim()) say(code + "." + key + " is empty"); continue; }
    if (typeof value !== "function") { say(code + "." + key + " is not a text"); continue; }
    for (const args of [[1, 1, 1, 1], [2, 0, 3, 1], [0, 5, 0, 0], [21, 4, 21, 4], [11, 12, 100, 22]]) {
      let text;
      try { text = String(value(...args)); } catch (error) { say(code + "." + key + "(" + args.join(", ") + ") fails: " + error.message); continue; }
      if (/undefined|NaN|\[object/.test(text)) say(code + "." + key + "(" + args.join(", ") + ") gives " + text);
    }
  }
  // the placeholders of a text with a fixed form are the same as in English: ${…} is not left in a
  // string (the ones of the template of the names, {title} {id} {version} {region}, are its words)
  for (const [key, value] of Object.entries(table)) {
    if (typeof value === "string" && /\$\{|\{(?!(?:title|id|version|region)\})[A-Za-z_]+\}/.test(value)) say(code + "." + key + " has a placeholder left in it");
  }
  // the texts that name the placeholders of the template say all four, as they are
  for (const key of ["namesFields"]) {
    for (const name of ["{title}", "{id}", "{version}", "{region}"]) if (!String(table[key]).includes(name)) say(code + "." + key + " lost " + name);
  }
  // names that are never translated stay as they are in the texts that say them
  for (const key of Object.keys(en)) {
    if (typeof en[key] !== "string" || !(key in table)) continue;
    for (const name of ["MaxPack", ".p5z", "PPSA", "ampr-emu-ng", "ShadowMountPlus", "Kraken Mount", "libSceAmpr.sprx", "maxpack-core", "SHA-256", "zstd", "FAT32", "homebrew/", "fakelib/", "TITLE_ID", "param.json"]) {
      if (en[key].includes(name) && !table[key].includes(name)) say(code + "." + key + " lost " + name);
    }
  }
}
// the language at the start: the first of the system's the window has, by its base subtag, else English
const detect = (languagesOfSystem, language) => systemLanguage({ languages: languagesOfSystem, language });
for (const [given, want] of [
  [["pt-BR", "en-US"], "pt"], [["zh-Hant-TW"], "zh"], [["zh-CN"], "zh"], [["xx", "ar-EG"], "ar"], [["fil", "ja"], "ja"],
  [["ko_KR"], "ko"], [["DE-at"], "de"], [["fil-PH"], "en"], [[], "en"], [["C"], "en"],
]) {
  if (detect(given, "") !== want) say("systemLanguage(" + given.join() + ") is " + detect(given, "") + ", expected " + want);
}
if (systemLanguage({ language: "it-IT" }) !== "it") say("systemLanguage falls back to navigator.language");
if (systemLanguage({}) !== "en" || systemLanguage(null) !== "en") say("systemLanguage without a system language is not English");
if (languageInfo("ar")[3] !== true || languageInfo("en")[3]) say("only Arabic is written from right to left");
console.log(problems.join("\n"));
`

// TestPageTexts: every language file has the same texts as en.js; every text the page asks for by
// name (t("…") in app.js, data-t in index.html) is in all of them, and so is the one of every
// state, warning and detail the window can tell the page. With node, every script of the page
// reads, and the texts of every language are of the same kind and take the same arguments.
func TestPageTexts(t *testing.T) {
	langs := map[string]map[string]bool{}
	for _, lang := range settings.Languages {
		langs[lang] = textKeys(t, lang)
	}
	en := langs["en"]
	for lang, keys := range langs {
		for key := range en {
			if !keys[key] {
				t.Errorf("%s.js does not have %s", lang, key)
			}
		}
		for key := range keys {
			if !en[key] {
				t.Errorf("en.js does not have %s, which %s.js has", key, lang)
			}
		}
	}
	page := readPage(t, "app.js") + readPage(t, "index.html")
	asked := regexp.MustCompile(`\bt\("([A-Za-z0-9_]+)"\)|data-t(?:-title|-label|-placeholder)?="([A-Za-z0-9_]+)"`)
	count := 0
	for _, found := range asked.FindAllStringSubmatch(page, -1) {
		key := found[1] + found[2]
		count++
		for lang, keys := range langs {
			if !keys[key] {
				t.Errorf("the page asks for the text %s, which is not in %s", key, lang)
			}
		}
	}
	if count < 20 {
		t.Errorf("the page asks for only %d texts by name", count)
	}
	for _, key := range pageCodes() {
		for lang, keys := range langs {
			if !keys[key] {
				t.Errorf("the text %s is missing in %s", key, lang)
			}
		}
	}
	// the end of a queue for the console says where its games are: in homebrew/, where
	// ShadowMountPlus sees them, or in the folder they have to be moved from to homebrew/, by
	// what the window tells of each game made (homebrew in job:item)
	for _, lang := range settings.Languages {
		source := readPage(t, "i18n/"+lang+".js")
		for _, key := range []string{"nextFolder", "nextMove"} {
			found := regexp.MustCompile(`(?m)^  ` + key + `: (.*)$`).FindStringSubmatch(source)
			if found == nil || !strings.Contains(found[1], "homebrew/") {
				t.Errorf("%s.%s does not say homebrew/", lang, key)
			}
		}
		if !regexp.MustCompile(`(?m)^  nextMove: folder => .*\+ folder \+`).MatchString(source) {
			t.Errorf("%s.nextMove does not say where the games are", lang)
		}
	}
	if app := readPage(t, "app.js"); !strings.Contains(app, `t("nextMove")`) || !strings.Contains(app, "row.homebrew") {
		t.Error("the end of a queue for the console does not look at where each game ended up")
	}
	// the choice of a language is remembered, and none means the system's: the line that used to
	// start everyone in Spanish is not there, and the settings take what the window has
	if app := readPage(t, "app.js"); strings.Contains(app, `=== "en" ? "en" : "es"`) || !strings.Contains(app, "chooseLanguage(state.settings.Lang)") {
		t.Error("the language at the start is not the chosen one, else the system's")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("no node: not checking that the scripts read or the texts' arguments")
		return
	}
	scripts, _ := filepath.Glob(filepath.Join("frontend", "*.js"))
	more, _ := filepath.Glob(filepath.Join("frontend", "i18n", "*.js"))
	if len(more) != len(settings.Languages)+1 {
		t.Errorf("%d files in i18n/ for %d languages and base.js: %v", len(more), len(settings.Languages), more)
	}
	for _, script := range append(scripts, more...) {
		if out, err := exec.Command(node, "--check", script).CombinedOutput(); err != nil {
			t.Errorf("%s does not read: %s", script, out)
		}
	}
	keys := "{"
	for i, lang := range settings.Languages {
		list := make([]string, 0, len(langs[lang]))
		for key := range langs[lang] {
			list = append(list, key)
		}
		sort.Strings(list)
		if i > 0 {
			keys += ","
		}
		keys += `"` + lang + `":["` + strings.Join(list, `","`) + `"]`
	}
	keys += "}"
	codes := `["` + strings.Join(settings.Languages, `","`) + `"]`
	keysFile := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(keysFile, []byte(keys), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, "-e", textChecks, "frontend", codes, keysFile).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}

// literalEnd gives where the string literal that starts at s[0] ends (the index after its
// closing quote), and whether it is a fixed one: no ${…} in a template literal.
func literalEnd(s string) (int, bool) {
	quote := s[0]
	for i := 1; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case quote == '`' && s[i] == '$' && i+1 < len(s) && s[i+1] == '{':
			return i, false
		case s[i] == quote:
			return i + 1, true
		}
	}
	return len(s), false
}

// runtimeChecks runs in node over the folder of the page: it loads base.js, en.js and app.js without
// a window (no DOM: the page does not start), puts stand-ins for the window and the dialogs, and
// prints one line for each case where the list warns of the runtime, or the dialog that asks for it
// opens, when it should not, or the other way round. argv: the folder.
const runtimeChecks = `
"use strict";
const fs = require("fs"), path = require("path"), vm = require("vm");
const dir = process.argv[1];
const element = () => ({ replaceChildren() {}, append() {}, classList: { toggle() {}, add() {}, remove() {} }, style: {},
  dataset: {}, content: { firstElementChild: { cloneNode: () => element() } }, querySelector: () => element() });
const context = { navigator: {}, console, Promise };
context.window = context;
context.document = { readyState: "loading", addEventListener() {}, getElementById: () => element(), querySelectorAll: () => [], querySelector: () => element() };
vm.createContext(context);
let source = "";
for (const name of ["i18n/base.js", "i18n/en.js", "app.js"]) source += fs.readFileSync(path.join(dir, name), "utf8") + "\n;\n";
vm.runInContext(source, context);
vm.runInContext(` + "`" + `(async () => {
  const problems = [];
  const game = { Name: "A", TitleID: "PPSA10001", Kind: "folder", Packed: false, HasZstdRuntime: false, OriginalSize: 10 };
  const row = () => ({ section: "compress", game, warnings: [], state: "", detail: "" });
  let asked = 0;
  ask = () => { asked++; return Promise.resolve("without"); };
  line = () => ({});
  // the plans say needs_runtime whatever the mode: what the page does with them is what is checked
  window.go = { main: { App: { Plan: async () => [{ Item: { Name: "A", TitleID: "PPSA10001" }, Warnings: ["needs_runtime"], Block: "" }] } } };
  // mode, the runtime chosen in Settings, the built-in one, and whether the list warns and the dialog opens
  for (const [mode, chosen, builtIn, want] of [
    ["p5z", "", "", false], ["p5z", "", "B", false], ["p5z", "C", "", false],
    ["folder", "", "", true], ["folder", "", "B", false], ["folder", "C", "", false], ["folder", "C", "B", false],
  ]) {
    state.settings = Object.assign({}, state.settings, { Mode: mode, Runtime: chosen });
    state.setup = { builtInRuntime: builtIn, builtInRuntimeVersion: builtIn ? "v0.5.1-maxpack.1" : "" };
    state.askedRuntime = false;
    const warned = rowWarnings(row()).includes(t("warn_needs_runtime"));
    asked = 0;
    await plan("compress", [row()], { Mode: mode, Runtime: chosen });
    const say = "mode " + mode + ", chosen " + JSON.stringify(chosen) + ", built-in " + JSON.stringify(builtIn) + ": ";
    if (warned !== want) problems.push(say + (warned ? "the list warns of the runtime" : "the list does not warn of the runtime"));
    if ((asked > 0) !== want) problems.push(say + (asked ? "the dialog asks for ampr-emu-ng" : "the dialog does not ask for ampr-emu-ng"));
  }
  return problems.join("\\n");
})()` + "`" + `, context).then(out => console.log(out), error => console.log("fails: " + error.stack));
`

// TestPageRuntimeWarnings: the list warns that a game lacks ampr-emu-ng, and the dialog asks for it,
// only for a folder ("Play from the USB") with no runtime at all: never for a .p5z, which MaxPack for
// the console gives its own when it plays it, and never while the built-in one or one chosen in
// Settings is there.
func TestPageRuntimeWarnings(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", runtimeChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}

// TestPageSafe: the page never writes data as HTML. innerHTML takes only fixed templates (a
// string literal, with nothing put in it), as NXPress does; nothing else that parses HTML is
// used; names and paths go in with textContent. And it loads nothing from the network.
func TestPageSafe(t *testing.T) {
	scripts, _ := filepath.Glob(filepath.Join("frontend", "*.js"))
	more, _ := filepath.Glob(filepath.Join("frontend", "i18n", "*.js"))
	scripts = append(scripts, more...)
	if len(scripts) < 4 {
		t.Fatalf("the page's scripts are missing: %v", scripts)
	}
	assign := regexp.MustCompile(`\.innerHTML\s*\+?=\s*`)
	banned := regexp.MustCompile(`outerHTML|insertAdjacentHTML|document\.write|\beval\s*\(|new\s+Function\b|createContextualFragment|DOMParser`)
	for _, script := range scripts {
		name, _ := filepath.Rel("frontend", script)
		source := readPage(t, filepath.ToSlash(name))
		for _, at := range assign.FindAllStringIndex(source, -1) {
			line := strings.Count(source[:at[0]], "\n") + 1
			if strings.Contains(source[at[0]:at[1]], "+=") {
				t.Errorf("%s:%d: innerHTML += ", name, line)
				continue
			}
			rest := source[at[1]:]
			if rest == "" || !strings.ContainsRune("\"'`", rune(rest[0])) {
				t.Errorf("%s:%d: innerHTML receives something that is not a fixed template", name, line)
				continue
			}
			end, fixed := literalEnd(rest)
			after := strings.TrimLeft(rest[end:], " \t")
			if !fixed || after == "" || !strings.ContainsRune(";\n,)", rune(after[0])) {
				t.Errorf("%s:%d: innerHTML receives a template with data", name, line)
			}
		}
		for _, found := range banned.FindAllStringIndex(source, -1) {
			t.Errorf("%s:%d: uses %s", name, strings.Count(source[:found[0]], "\n")+1, source[found[0]:found[1]])
		}
	}
	app := readPage(t, "app.js")
	for _, field := range []string{"Name", "Path", "TitleID"} {
		if !regexp.MustCompile(`\.textContent\s*=[^;\n]*\.` + field + `\b`).MatchString(app) {
			t.Errorf("app.js does not write %s with textContent", field)
		}
	}
	network := regexp.MustCompile(`(?i)\b(?:https?:)?//[a-z0-9.-]+\.[a-z]{2,}|@import|url\(\s*["']?(?:https?:|//)`)
	for _, name := range append([]string{"index.html", "style.css", "app.js", "i18n/base.js"}, func() []string {
		var names []string
		for _, lang := range settings.Languages {
			names = append(names, "i18n/"+lang+".js")
		}
		return names
	}()...) {
		source := readPage(t, name)
		for _, found := range network.FindAllString(source, -1) {
			t.Errorf("%s loads something from outside: %s", name, found)
		}
	}
	page := readPage(t, "index.html")
	if !strings.Contains(page, `http-equiv="Content-Security-Policy"`) || !strings.Contains(page, "default-src 'self'") {
		t.Error("index.html does not limit what it loads (Content-Security-Policy)")
	}
	if strings.Contains(page, " style=") || strings.Contains(page, "<style") || strings.Contains(app, "<style") {
		t.Error("loose styles in the page: the security policy blocks them")
	}
	for _, name := range append([]string{"index.html", "style.css"}, func() []string {
		var names []string
		for _, script := range scripts {
			name, _ := filepath.Rel("frontend", script)
			names = append(names, filepath.ToSlash(name))
		}
		return names
	}()...) {
		if !strings.Contains(readPage(t, name), "SPDX-License-Identifier: GPL-3.0-or-later") {
			t.Errorf("%s does not have the SPDX header", name)
		}
	}
}

func attr(node *html.Node, key string) (string, bool) {
	for _, a := range node.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func walk(node *html.Node, visit func(*html.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walk(child, visit)
	}
}

// TestPageSections: the page has the sections compress, decompress, verify, details and
// settings; "Made for" with the installer (p5z) chosen by default and the folder as the other
// choice; and the button cancel. Every dialog hangs from the body (one inside another opens
// hidden in it, as NXPress learnt), and the scripts load in their order.
func TestPageSections(t *testing.T) {
	page, err := html.Parse(strings.NewReader(readPage(t, "index.html")))
	if err != nil {
		t.Fatal(err)
	}
	sections := map[string]bool{}
	ids := map[string]*html.Node{}
	madeFor := map[string]bool{} // value: checked
	var scripts []string
	dialogs := 0
	walk(page, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		if section, ok := attr(node, "data-section"); ok {
			sections[section] = true
		}
		if id, ok := attr(node, "id"); ok {
			if ids[id] != nil {
				t.Errorf("the id %s appears twice", id)
			}
			ids[id] = node
		}
		if name, _ := attr(node, "name"); node.Data == "input" && name == "made-for" {
			if kind, _ := attr(node, "type"); kind != "radio" {
				t.Errorf("Made for is not a choice: %s", kind)
			}
			value, _ := attr(node, "value")
			_, checked := attr(node, "checked")
			madeFor[value] = checked
		}
		if node.Data == "script" {
			src, _ := attr(node, "src")
			scripts = append(scripts, src)
		}
		if node.Data == "dialog" {
			dialogs++
			if node.Parent == nil || node.Parent.Data != "body" {
				id, _ := attr(node, "id")
				t.Errorf("the dialog %s does not hang from the body", id)
			}
		}
	})
	for _, section := range []string{"compress", "decompress", "verify", "details", "rename", "settings"} {
		if !sections[section] {
			t.Errorf("the section %s is missing", section)
		}
	}
	if len(madeFor) != 2 || !madeFor["p5z"] {
		t.Errorf("Made for: %v (p5z checked by default, and folder)", madeFor)
	}
	if checked, ok := madeFor["folder"]; !ok || checked {
		t.Errorf("Made for without the folder, or checked: %v", madeFor)
	}
	if cancel := ids["cancel"]; cancel == nil || cancel.Data != "button" {
		t.Error("the cancel button is missing")
	}
	for _, id := range []string{"settings", "options", "files", "done-dialog", "replace-dialog", "fat32-dialog", "runtime-dialog", "block-dialog", "warn-dialog", "close-dialog",
		"delete-dialog", "space-dialog", "leftover-dialog", "delete-originals", "pref-low", "pref-delete", "pref-forgood", "template", "pref-report", "done-report",
		"repair-data", "repair-percent", "repair-hint", "pref-repair", "pref-repair-percent", "verify-percent", "add-repair", "repair-damaged", "done-repair", "space-dest", "space-go",
		"pref-explorer", "explorer-portable", "explorer-for", "pref-menu", "pref-sendto", "pref-files", "menu-status", "sendto-status", "files-status"} {
		if ids[id] == nil {
			t.Errorf("#%s is missing", id)
		}
	}
	if dialogs < 6 {
		t.Errorf("only %d dialogs", dialogs)
	}
	order := "i18n/base.js"
	for _, lang := range settings.Languages {
		order += " i18n/" + lang + ".js"
	}
	if strings.Join(scripts, " ") != order+" app.js" {
		t.Errorf("scripts: %v", scripts)
	}
	// the document starts in English, left to right; the page sets its language and direction
	walk(page, func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "html" {
			if lang, _ := attr(node, "lang"); lang != "en" {
				t.Errorf("the document starts in %q, not in English", lang)
			}
			if dir, _ := attr(node, "dir"); dir != "ltr" {
				t.Errorf("the document starts with direction %q", dir)
			}
		}
	})
}

// TestPageCallsWindow: every method the page calls (go.call("…") in app.js) is one the window binds,
// a method of App, so none fails only once the window runs.
func TestPageCallsWindow(t *testing.T) {
	app := reflect.TypeOf(&App{})
	calls := regexp.MustCompile(`go\.call\("([A-Za-z]+)"`).FindAllStringSubmatch(readPage(t, "app.js"), -1)
	if len(calls) < 20 {
		t.Fatalf("the page calls only %d methods", len(calls))
	}
	for _, call := range calls {
		if _, ok := app.MethodByName(call[1]); !ok {
			t.Errorf("the page calls %s, which App does not have", call[1])
		}
	}
	for _, name := range []string{"ExplorerPlaces", "SetExplorerPlace", "RelabelExplorerMenu"} {
		if !strings.Contains(readPage(t, "app.js"), `go.call("`+name+`"`) {
			t.Errorf("the page does not call %s", name)
		}
	}
}
