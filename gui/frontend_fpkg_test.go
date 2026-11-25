// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// packageChecks runs in node over the folder of the page, as imageChecks does, and prints one line
// for each thing the page does not do with PS5 packages as it should. argv: the folder.
const packageChecks = `
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
console.log(vm.runInContext(` + "`" + `(() => {
  const problems = [];
  const say = text => problems.push(text);
  if (kindName({ Kind: "fpkg" }) !== "PS5 package (FPKG)") say("kindName of a package: " + kindName({ Kind: "fpkg" }));
  for (const section of ["compress", "details", "rename"]) {
    if (!takes[section].some(([type]) => type === ".pkg")) say(section + " does not take a .pkg");
  }
  for (const section of ["decompress", "verify"]) {
    if (takes[section].some(([type]) => type === ".pkg")) say(section + " takes a .pkg");
  }
  const row = (section, game) => ({ section, game: Object.assign({ Name: "A", TitleID: "PPSA00000", OriginalSize: 10 }, game), warnings: [], state: "", detail: "" });
  const pkg = { Kind: "fpkg", Compressed: true, OriginalSize: 100, ImageSize: 80, Path: "D:/pkgs/Copper Vale.PKG" };
  state.lists.compress = [row("compress", pkg)];
  if (readyRows("compress").length !== 1) say("a package does not go in Compress");
  if (rowWarnings(state.lists.compress[0]).length) say("a package is warned of in Compress");
  state.lists.verify = [row("verify", pkg)];
  if (readyRows("verify").length !== 0) say("a package goes in a queue of Verify");
  // saved is against the package's file, and Compare levels says what each level gives against it
  // (stored or compressed: its file is not the game's size)
  if (sizeBefore(pkg) !== 80) say("sizeBefore of a package: " + sizeBefore(pkg));
  if (vsImage(pkg, 60) !== "vs your .pkg: " + t("smallerBy")(25)) say("vsImage of a package: " + vsImage(pkg, 60));
  if (vsImage(Object.assign({}, pkg, { Compressed: false, ImageSize: 110 }), 60) !== "vs your .pkg: " + t("smallerBy")(45)) say("vsImage of a stored package");
  if (vsImage(Object.assign({}, pkg, { ImageSize: 0 }), 60) !== "") say("vsImage of a package of unknown size");
  const report = gameReport(row("compress", pkg));
  if (report.Kind !== "fpkg" || report.Compressed !== true || report.ImageSize !== 80 || JSON.stringify(report).includes("Copper")) say("the report of a package: " + JSON.stringify(report));
  // the passcode: what the engine says of a package made with another one is said in the page's
  // words, with where to give it; a job that failed for it too
  const said = errorText("info of D:/x.pkg: exit status 1: D:/x.pkg: this package was made with another passcode; give it with --passcode");
  if (said !== t("detail_passcode") || !said.includes("Settings")) say("the passcode error: " + said);
  if (errorText("info of D:/x.pkg: exit status 1: D:/x.pkg: a retail package") !== "info of D:/x.pkg: exit status 1: D:/x.pkg: a retail package") say("another error is changed");
  if (detailText("passcode") !== t("detail_passcode")) say("the detail of a job: " + detailText("passcode"));
  for (const [p, ok] of [["", true], ["0".repeat(32), true], ["MaxPackTestPasscode-0123456789ab", true], ["0".repeat(31), false],
    ["0".repeat(33), false], ["MaxPackTestPasscode 0123456789ab", false], ["MaxPackTestPasscode-0123456789añ", false]]) {
    if (validPasscode(p) !== ok) say("validPasscode(" + JSON.stringify(p) + ") is " + validPasscode(p));
  }
  if (defaults.Passcode !== "") say("the default passcode: " + defaults.Passcode);
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestPagePackages: the page names a PS5 package by its kind, takes it where it takes an image, sends
// it to Compress and not to Verify, counts what it saved against the package's file (and Compare
// levels against it too), reports its kind without its name, and says in its own words, with where
// to give it, that a package needs another passcode; Settings keeps only a passcode the engine takes.
func TestPagePackages(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", packageChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}
