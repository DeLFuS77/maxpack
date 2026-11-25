// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// completeChecks runs in node over the folder of the page, as imageChecks does: it loads base.js,
// en.js and app.js without a window and prints one line for each thing the page does not say as it
// should of a game against the list of the original package's files. argv: the folder.
const completeChecks = `
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
  const complete = { Checkable: true, Reason: "", Listed: 1234, Present: 1234, Missing: 0, ExtraCount: 0, ExtraBytes: 0, LeftoverCount: 0, LeftoverBytes: 0 };
  const missing = Object.assign({}, complete, { Present: 1231, Missing: 3, ExtraCount: 2, ExtraBytes: 3000000000, LeftoverCount: 1, LeftoverBytes: 2000000000 });
  const one = Object.assign({}, complete, { Present: 1233, Missing: 1 });
  const none = { Checkable: false, Reason: "no_table", Listed: 0, Present: 0, Missing: 0, ExtraCount: 0, ExtraBytes: 0, LeftoverCount: 4, LeftoverBytes: 1000000000 };
  const row = (section, game) => ({ section, game: Object.assign({ Name: "A", TitleID: "PPSA00000", Kind: "folder", OriginalSize: 10, Path: "D:/Iron Harbor" }, game),
    warnings: [], state: "", detail: "", path: "D:/Iron Harbor" });
  // Compress warns of the files that are missing, before anything is packed; nothing for a complete
  // game, one that cannot be checked or an engine that says nothing
  const warns = game => rowWarnings(row("compress", game));
  if (!warns({ Completeness: missing }).includes("3 files of the original game are missing")) say("compress: " + warns({ Completeness: missing }).join(" · "));
  if (!warns({ Completeness: one }).includes("1 file of the original game is missing")) say("compress, one: " + warns({ Completeness: one }).join(" · "));
  for (const c of [complete, none, null, undefined]) {
    if (warns({ Completeness: c }).length) say("compress warns of " + JSON.stringify(c) + ": " + warns({ Completeness: c }).join(" · "));
  }
  // Verify says it in a line under the game, when it could be checked
  const line = (section, c) => completeLineText(row(section, { Kind: "p5z", Packed: true, Completeness: c }));
  if (line("verify", complete) !== "Complete: all 1234 files of the original package") say("verify, complete: " + line("verify", complete));
  if (line("verify", missing) !== "Incomplete: 3 of the 1234 files of the original package are missing") say("verify, missing: " + line("verify", missing));
  if (line("verify", none) !== "" || line("verify", null) !== "") say("verify says a line of a game that cannot be checked");
  if (line("details", missing) !== "" || line("compress", missing) !== "") say("the line is not only for Verify");
  // Details: whether it is complete, what is not in the package and what dump tools left
  const facts = game => completenessFacts(Object.assign({ Kind: "folder" }, game)).map(([name, value]) => name + ": " + value);
  const said = facts({ Completeness: missing }).join(" | ");
  for (const want of ["Complete: No: 3 of the 1234 files of the original package are missing", "Not in the original package: 2 files · 2.8 GB",
    "Left by dump tools: 1.9 GB · 1 file · left out when compressing"]) {
    if (!said.includes(want)) say("details does not say " + want + ": " + said);
  }
  if (facts({ Completeness: complete }).join(" | ") !== "Complete: Yes: all 1234 files of the original package") say("details, complete: " + facts({ Completeness: complete }).join(" | "));
  const unproven = facts({ Completeness: none }).join(" | ");
  if (!unproven.includes("Complete: Cannot tell: it has no list of the original package's files") || !unproven.includes("kept: nothing proves")) say("details, no table: " + unproven);
  for (const [reason, words] of [["unknown_format", "format MaxPack does not know"], ["damaged", "damaged"], ["unreadable", "could not be read"], ["later", "Cannot tell"]]) {
    const text = facts({ Completeness: { Checkable: false, Reason: reason } }).join(" | ");
    if (!text.includes(words)) say("details, " + reason + ": " + text);
  }
  if (facts({}).length || facts({ Completeness: null }).length) say("details says a completeness the engine did not");
  // with Settings keeping them, the leftovers stay; in a .p5z they are only what is there
  state.settings.KeepLeftovers = true;
  if (!facts({ Completeness: missing }).join(" | ").includes("kept (Settings)")) say("details, kept: " + facts({ Completeness: missing }).join(" | "));
  state.settings.KeepLeftovers = false;
  const inP5z = facts({ Kind: "p5z", Completeness: missing }).join(" | ");
  if (inP5z.includes("left out") || inP5z.includes("kept")) say("details of a .p5z: " + inP5z);
  // a report has the counts, never the names nor the paths of the files
  const report = gameReport(row("details", { Completeness: Object.assign({ extra_paths: ["decrypted/eboot.bin"] }, missing) }));
  const c = report.Completeness || {};
  if (c.Missing !== 3 || c.Listed !== 1234 || c.LeftoverBytes !== 2000000000 || c.Checkable !== true) say("the report: " + JSON.stringify(report));
  if (JSON.stringify(report).includes("decrypted") || JSON.stringify(report).includes("Iron Harbor")) say("the report has a name: " + JSON.stringify(report));
  if (gameReport(row("details", {})).Completeness !== null) say("the report of a game the engine said nothing of: " + JSON.stringify(gameReport(row("details", {}))));
  // the setting starts on: the leftovers go
  if (defaults.KeepLeftovers !== false) say("the default keeps the leftovers");
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestPageCompleteness: Compress warns of the files of the original game a dump lacks before it is
// packed, Verify says whether what it checked was complete, and Details says it all: whether it is
// complete (or why that cannot be told), what is not in the original package and what dump tools
// left, which Compress leaves out unless Settings keeps it. A report says the counts, never a name.
func TestPageCompleteness(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", completeChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}
