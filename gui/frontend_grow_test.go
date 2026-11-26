// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/settings"
)

// growChecks runs in node over the folder of the page, as dlcChecks does, and prints one line for each
// thing the page does not do with a .p5z that grows as it should. argv: the folder.
const growChecks = `
"use strict";
const fs = require("fs"), path = require("path"), vm = require("vm");
const dir = process.argv[1];
const element = () => ({ replaceChildren() {}, append() {}, classList: { toggle() {}, add() {}, remove() {} }, style: {},
  dataset: {}, content: { firstElementChild: { cloneNode: () => element() } }, querySelector: () => element() });
const context = { navigator: {}, console, Promise, setTimeout: () => 0, clearTimeout() {} };
context.window = context;
context.document = { readyState: "loading", addEventListener() {}, getElementById: () => element(), querySelectorAll: () => [], querySelector: () => element(), createElement: () => element() };
vm.createContext(context);
let source = "";
for (const name of ["i18n/base.js", "i18n/en.js", "app.js"]) source += fs.readFileSync(path.join(dir, name), "utf8") + "\n;\n";
vm.runInContext(source, context);
console.log(vm.runInContext(` + "`" + `(() => {
  const problems = [];
  const say = text => problems.push(text);
  const p5z = { Name: "Copper Vale", TitleID: "PPSA00000", Kind: "p5z", OriginalSize: 1000, Grown: 2, UnusedBytes: 4096 };
  const row = { path: "D:/g.p5z", section: "details", game: p5z, warnings: [], state: "", detail: "", error: "", grow: ["D:/dlc/a.pkg", "D:/dlc/b"], growKind: "add_dlc", force: false };
  // what goes to the window: the DLC (or the new dump) and whether to go on when most of it changed
  const item = itemOf(row);
  if (JSON.stringify(item.Grow) !== JSON.stringify(["D:/dlc/a.pkg", "D:/dlc/b"]) || item.Force !== false || item.Icon !== "") say("the item: " + JSON.stringify(item));
  row.force = true;
  if (itemOf(row).Force !== true) say("force is not sent");
  const plain = { path: "D:/x", section: "compress", game: { Kind: "folder" }, warnings: [] };
  if (itemOf(plain).Grow.length || itemOf(plain).Force) say("a game of Compress grows: " + JSON.stringify(itemOf(plain)));
  // its steps: added to, verified, and its repair data made again when it had some of its own
  if (stepsOf({ mode: "add_dlc", repair: 0 }).join() !== "growing,verifying") say("steps: " + stepsOf({ mode: "add_dlc", repair: 0 }));
  if (stepsOf({ mode: "update", repair: 5 }).join() !== "growing,verifying,protecting") say("steps with repair data: " + stepsOf({ mode: "update", repair: 5 }));
  if (!working.includes("growing")) say("growing is not a state at work");
  // the bar of the step at work goes by the bytes of its phases, not by the size of the game
  const at = { step: "growing", offset: 0, done: 50, total: 100, game: p5z };
  if (stepShare(at) !== 0.5) say("the share of growing: " + stepShare(at));
  // the facts of Details: how many times it grew, and the room it does not use
  const facts = growFacts(p5z).map(f => f.join(": ")).join(" | ");
  if (facts !== t("factGrown") + ": " + t("grownTimes")(2) + " | " + t("factUnused") + ": " + t("unusedFact")(size(4096))) say("the facts: " + facts);
  if (growFacts({ Kind: "p5z", Grown: 0 }).length || growFacts({ Kind: "folder", Grown: 3 }).length) say("facts of one that did not grow");
  // more than half of it changed: the update can go on, when the user says so; nothing else can
  Object.assign(row, { growKind: "update", state: "error", detail: "mostly_changed", force: false, grow: ["D:/v2"] });
  if (!canGrowAnyway(row)) say("an update of most of the game cannot go on");
  for (const [k, v] of [["state", "ok"], ["detail", "other_game"], ["growKind", "add_dlc"], ["section", "verify"]]) {
    const other = Object.assign({}, row, { [k]: v });
    if (canGrowAnyway(other)) say("it can go on with " + k + " = " + v);
  }
  // what the engine and the window say, in the page's words
  for (const code of ["grown", "nothing_to_do", "repair_redone", "busy", "grow_no_space", "grow_too_big", "too_many_volumes", "mostly_changed",
    "no_packs_left", "other_game", "same_version", "no_title_id", "not_a_game", "changed_meanwhile", "dump_packed", "nothing_given"]) {
    if (detailText(code) !== t("detail_" + code)) say("the detail " + code + ": " + detailText(code));
  }
  if (detailText("grown; repair_redone") !== t("detail_grown") + "; " + t("detail_repair_redone")) say("two details: " + detailText("grown; repair_redone"));
  // a grow that ended well says so as good news
  if (!goodNote({ state: "ok", detail: "grown; repair_redone" }) || goodNote({ state: "ok", detail: "grown; no_par2" }) || goodNote({ state: "error", detail: "grown" })) say("good news");
  // the end card says where to play it
  if (nextStep({ mode: "update", rows: [] }) !== t("nextGrow")) say("next step: " + nextStep({ mode: "update", rows: [] }));
  // Details starts nothing on its own: its card does
  if (queues.includes("details")) say("Details has a queue of its own");
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestPageGrow: the card of a .p5z in Details adds DLC to it or updates it with a new dump, as a queue
// of its own: what it sends to the window, its steps and bar, the facts of one that grew, going on with
// an update of most of the game when the user says so, and what is said of it in the page's words.
func TestPageGrow(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", growChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}

// TestPageBusySaysWho: a .p5z another program holds (Add DLC, Update, Send) says what may hold it: another
// MaxPack, the Explorer making its cover or an antivirus, and to try again in a moment; in every language.
func TestPageBusySaysWho(t *testing.T) {
	runPage(t, `
  if (!/another MaxPack/.test(t("detail_busy")) || !/Explorer/.test(t("detail_busy")) || !/antivirus/.test(t("detail_busy")) || !/moment/.test(t("detail_busy")))
    say("detail_busy: " + t("detail_busy"));
`)
	for _, lang := range settings.Languages {
		found := regexp.MustCompile(`(?m)^  detail_busy: "(.*)",\r?$`).FindStringSubmatch(readPage(t, "i18n/"+lang+".js"))
		if found == nil || !strings.Contains(found[1], "MaxPack") || !strings.Contains(found[1], ".p5z") {
			t.Errorf("%s: detail_busy %q", lang, found)
		}
	}
}

// The choice of what to add to a .p5z (Add DLC, Update) warns of nothing: it is not drawn as a warning.
func TestPageGrowChoiceIsNoWarning(t *testing.T) {
	found := regexp.MustCompile(`<dialog id="grow-dialog"([^>]*)>`).FindStringSubmatch(readPage(t, "index.html"))
	if found == nil || strings.Contains(found[1], "warn-dialog") || !strings.Contains(found[1], "tool-dialog") {
		t.Errorf("the grow dialog: %q", found)
	}
}
