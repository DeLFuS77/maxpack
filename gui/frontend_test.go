// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/jobs"

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
			t.Errorf("%s: el texto %s está dos veces", lang, found[1])
		}
		keys[found[1]] = true
	}
	if len(keys) == 0 {
		t.Fatalf("%s: sin textos", lang)
	}
	return keys
}

// pageCodes are the names of the texts of what the window tells the page: the states of a game,
// the phases of its progress, the warnings of a plan and the codes of a detail (why a game
// failed or was skipped), with the ones the page gives itself.
func pageCodes() []string {
	states := []string{jobs.StateWaiting, jobs.StatePacking, jobs.StateVerifying, jobs.StateUnpacking,
		jobs.StateOK, jobs.StateError, jobs.StateSkipped, jobs.StateCancelled}
	warnings := []string{"fat32", "needs_runtime", "may_not_fit", "duplicate_target"}
	details := []string{
		// blocks of a plan, which the Runner gives as the detail of a skip
		"same_as_source", "no_space", "fat32_too_big", "exists",
		// the Runner
		"dest_missing", "tmp_not_deleted", "partial_not_deleted", "old_not_deleted", "swap_failed",
		"verify_failed", "files_bad", "exit", "cancelled", "io",
		// the engine
		"bad_input", "too_big",
		// the page: skipped by the user, a game that cannot go, and anything else
		"skipped_by_user", "already_packed", "not_packed", "other",
	}
	// the phases of the progress of the engine (phase_code in src/cli/main.c)
	phases := []string{"copying", "packing", "verifying", "unpacking", "working"}
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
// prints one line for each problem it finds.
const textChecks = `
"use strict";
const fs = require("fs"), path = require("path"), vm = require("vm");
const dir = process.argv[1];
const problems = [];
const say = text => problems.push(text);
const context = { size: bytes => bytes + " B", time: seconds => seconds + " s", navigator: {} };
vm.createContext(context);
let source = "";
for (const name of ["base", "es", "en"]) source += fs.readFileSync(path.join(dir, "i18n", name + ".js"), "utf8") + "\n;\n";
vm.runInContext(source + "this.texts = texts; this.languages = languages;", context);
const { texts, languages } = context;
if (languages.map(language => language[0]).join() !== "es,en") say("los idiomas son " + languages.map(language => language[0]).join());
const es = texts.es, en = texts.en;
const keys = JSON.parse(process.argv[2]);
for (const [code, table] of [["es", es], ["en", en]]) {
  const unread = Object.keys(table).filter(key => !keys[code].includes(key));
  if (unread.length) say(code + ": textos que no van en una línea propia: " + unread.join(", "));
}
const braces = text => (text.match(/\{[A-Za-z_]+\}/g) || []).sort().join(" ");
for (const key of Object.keys(es)) {
  if (!(key in en)) continue;
  if (typeof es[key] !== typeof en[key]) say(key + ": en es " + typeof es[key] + ", en en " + typeof en[key]);
  else if (typeof es[key] === "function" && es[key].length !== en[key].length) say(key + ": en es toma " + es[key].length + " argumentos, en en " + en[key].length);
}
for (const [code, table] of [["es", es], ["en", en]]) {
  for (const [key, value] of Object.entries(table)) {
    if (typeof value === "string") { if (!value.trim()) say(code + "." + key + " está vacío"); continue; }
    if (typeof value !== "function") { say(code + "." + key + " no es un texto"); continue; }
    for (const args of [[1, 1, 1, 1], [2, 0, 3, 1], [0, 5, 0, 0], [21, 4, 21, 4]]) {
      let text;
      try { text = String(value(...args)); } catch (error) { say(code + "." + key + "(" + args.join(", ") + ") falla: " + error.message); continue; }
      if (/undefined|NaN|\[object/.test(text)) say(code + "." + key + "(" + args.join(", ") + ") da " + text);
    }
  }
}
console.log(problems.join("\n"));
`

// TestPageTexts: es.js and en.js have the same texts; every text the page asks for by name
// (t("…") in app.js, data-t in index.html) is in both, and so is the one of every state,
// warning and detail the window can tell the page. With node, every script of the page reads,
// and the texts of both languages are of the same kind and take the same arguments.
func TestPageTexts(t *testing.T) {
	es, en := textKeys(t, "es"), textKeys(t, "en")
	for key := range es {
		if !en[key] {
			t.Errorf("en.js no tiene %s", key)
		}
	}
	for key := range en {
		if !es[key] {
			t.Errorf("es.js no tiene %s", key)
		}
	}
	page := readPage(t, "app.js") + readPage(t, "index.html")
	asked := regexp.MustCompile(`\bt\("([A-Za-z0-9_]+)"\)|data-t(?:-title|-label|-placeholder)?="([A-Za-z0-9_]+)"`)
	count := 0
	for _, found := range asked.FindAllStringSubmatch(page, -1) {
		key := found[1] + found[2]
		count++
		if !es[key] || !en[key] {
			t.Errorf("la página pide el texto %s, que no está en los dos idiomas", key)
		}
	}
	if count < 20 {
		t.Errorf("la página pide solo %d textos por su nombre", count)
	}
	for _, key := range pageCodes() {
		if !es[key] || !en[key] {
			t.Errorf("falta el texto %s en algún idioma", key)
		}
	}
	// the end of a queue for the console says where its games are: in homebrew/, where
	// ShadowMountPlus sees them, or in the folder they have to be moved from to homebrew/, by
	// what the window tells of each game made (homebrew in job:item)
	for _, lang := range []string{"es", "en"} {
		source := readPage(t, "i18n/"+lang+".js")
		for _, key := range []string{"nextFolder", "nextMove"} {
			found := regexp.MustCompile(`(?m)^  ` + key + `: (.*)$`).FindStringSubmatch(source)
			if found == nil || !strings.Contains(found[1], "homebrew/") {
				t.Errorf("%s.%s no dice homebrew/", lang, key)
			}
		}
		if !regexp.MustCompile(`(?m)^  nextMove: folder => .*\+ folder \+`).MatchString(source) {
			t.Errorf("%s.nextMove no dice dónde están los juegos", lang)
		}
	}
	if app := readPage(t, "app.js"); !strings.Contains(app, `t("nextMove")`) || !strings.Contains(app, "row.homebrew") {
		t.Error("el final de una cola para la consola no mira dónde quedó cada juego")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("sin node: no se comprueba que los scripts se lean ni los argumentos de los textos")
		return
	}
	scripts, _ := filepath.Glob(filepath.Join("frontend", "*.js"))
	more, _ := filepath.Glob(filepath.Join("frontend", "i18n", "*.js"))
	for _, script := range append(scripts, more...) {
		if out, err := exec.Command(node, "--check", script).CombinedOutput(); err != nil {
			t.Errorf("%s no se lee: %s", script, out)
		}
	}
	sorted := func(keys map[string]bool) []string {
		list := make([]string, 0, len(keys))
		for key := range keys {
			list = append(list, key)
		}
		sort.Strings(list)
		return list
	}
	keys := `{"es":["` + strings.Join(sorted(es), `","`) + `"],"en":["` + strings.Join(sorted(en), `","`) + `"]}`
	out, err := exec.Command(node, "-e", textChecks, "frontend", keys).CombinedOutput()
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

// TestPageSafe: the page never writes data as HTML. innerHTML takes only fixed templates (a
// string literal, with nothing put in it), as NXPress does; nothing else that parses HTML is
// used; names and paths go in with textContent. And it loads nothing from the network.
func TestPageSafe(t *testing.T) {
	scripts, _ := filepath.Glob(filepath.Join("frontend", "*.js"))
	more, _ := filepath.Glob(filepath.Join("frontend", "i18n", "*.js"))
	scripts = append(scripts, more...)
	if len(scripts) < 4 {
		t.Fatalf("faltan scripts de la página: %v", scripts)
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
				t.Errorf("%s:%d: innerHTML recibe algo que no es una plantilla fija", name, line)
				continue
			}
			end, fixed := literalEnd(rest)
			after := strings.TrimLeft(rest[end:], " \t")
			if !fixed || after == "" || !strings.ContainsRune(";\n,)", rune(after[0])) {
				t.Errorf("%s:%d: innerHTML recibe una plantilla con datos", name, line)
			}
		}
		for _, found := range banned.FindAllStringIndex(source, -1) {
			t.Errorf("%s:%d: usa %s", name, strings.Count(source[:found[0]], "\n")+1, source[found[0]:found[1]])
		}
	}
	app := readPage(t, "app.js")
	for _, field := range []string{"Name", "Path", "TitleID"} {
		if !regexp.MustCompile(`\.textContent\s*=[^;\n]*\.` + field + `\b`).MatchString(app) {
			t.Errorf("app.js no escribe %s con textContent", field)
		}
	}
	network := regexp.MustCompile(`(?i)\b(?:https?:)?//[a-z0-9.-]+\.[a-z]{2,}|@import|url\(\s*["']?(?:https?:|//)`)
	for _, name := range []string{"index.html", "style.css", "app.js", "i18n/base.js", "i18n/es.js", "i18n/en.js"} {
		source := readPage(t, name)
		for _, found := range network.FindAllString(source, -1) {
			t.Errorf("%s carga algo de fuera: %s", name, found)
		}
	}
	page := readPage(t, "index.html")
	if !strings.Contains(page, `http-equiv="Content-Security-Policy"`) || !strings.Contains(page, "default-src 'self'") {
		t.Error("index.html no limita lo que carga (Content-Security-Policy)")
	}
	if strings.Contains(page, " style=") || strings.Contains(page, "<style") || strings.Contains(app, "<style") {
		t.Error("estilos sueltos en la página: la política de seguridad los bloquea")
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
			t.Errorf("%s no tiene la cabecera SPDX", name)
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
// settings; «Hecho para» with the installer (p5z) chosen by default and the folder as the other
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
				t.Errorf("el id %s está dos veces", id)
			}
			ids[id] = node
		}
		if name, _ := attr(node, "name"); node.Data == "input" && name == "made-for" {
			if kind, _ := attr(node, "type"); kind != "radio" {
				t.Errorf("Hecho para no es una elección: %s", kind)
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
				t.Errorf("el diálogo %s no cuelga del body", id)
			}
		}
	})
	for _, section := range []string{"compress", "decompress", "verify", "details", "settings"} {
		if !sections[section] {
			t.Errorf("falta la sección %s", section)
		}
	}
	if len(madeFor) != 2 || !madeFor["p5z"] {
		t.Errorf("Hecho para: %v (p5z marcado por defecto, y folder)", madeFor)
	}
	if checked, ok := madeFor["folder"]; !ok || checked {
		t.Errorf("Hecho para sin la carpeta, o marcada: %v", madeFor)
	}
	if cancel := ids["cancel"]; cancel == nil || cancel.Data != "button" {
		t.Error("falta el botón cancel")
	}
	for _, id := range []string{"settings", "options", "files", "done-dialog", "replace-dialog", "fat32-dialog", "runtime-dialog", "block-dialog", "warn-dialog", "close-dialog"} {
		if ids[id] == nil {
			t.Errorf("falta #%s", id)
		}
	}
	if dialogs < 6 {
		t.Errorf("solo %d diálogos", dialogs)
	}
	if strings.Join(scripts, " ") != "i18n/base.js i18n/es.js i18n/en.js app.js" {
		t.Errorf("scripts: %v", scripts)
	}
}
