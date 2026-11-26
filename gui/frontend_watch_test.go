// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// watchChecks runs in node over the folder of the page, as growChecks does, and prints one line for each
// thing the page does not do with what the watched folder finds as it should. argv: the folder.
const watchChecks = `
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
  const game = (more) => Object.assign({ Name: "Copper Vale", Kind: "folder", OriginalSize: 9000, ContentID: "UP0000-PPSA10001_00-MAXPACKTEST00000" }, more || {});
  const row = (path, g, more) => Object.assign({ path, section: "compress", game: g, error: "", state: "", detail: "", warnings: [], watched: true }, more || {});
  const whole = row("D:/w/Game", game());
  const lacking = row("D:/w/Half", game({ Completeness: { Checkable: true, Source: "playgo", Missing: 3 } }));
  const damaged = row("D:/w/Copy", game({ Completeness: { Checkable: true, Source: "dump_index", Missing: 0, SizeMismatch: 2 } }));
  const dlc = row("D:/w/dlc.pkg", game({ Content: "dlc", Kind: "fpkg" }));
  const packed = row("D:/w/Packed", game({ Packed: true }));
  const unread = row("D:/w/Reading", null);
  const done = row("D:/w/Done", game(), { state: "ok" });
  const ready = watchReady([whole, lacking, damaged, dlc, packed, unread, done]);
  if (ready.map(r => r.path).join() !== "D:/w/Game") say("ready: " + ready.map(r => r.path).join());
  // what lacks files of its list stays in the list with its warning, held: it is not compressed
  for (const r of [lacking, damaged]) if (!r.watchHeld || r.detail !== "watch_incomplete") say("not held: " + r.path + " " + r.detail);
  if (watchReady([lacking]).length) say("a held one goes");
  if (detailText("watch_incomplete") !== t("detail_watch_incomplete") || detailText("watch_exists") !== t("detail_watch_exists")) say("the details");
  // only the watched rows that are not done count
  state.lists.compress = [whole, done, row("D:/x", game(), { watched: false })];
  if (watchedRows().map(r => r.path).join() !== "D:/w/Game") say("watched rows: " + watchedRows().map(r => r.path).join());
  // what arrives goes to Compress, watched, and said (the rows as addPaths makes them, without the DOM)
  describe = () => {};
  toast = () => {};
  addPaths = (section, paths) => { for (const p of paths) if (!state.lists[section].some(r => r.path === p)) state.lists[section].push(row(p, null, { watched: false })); };
  state.lists.compress = [];
  onWatchFound({ paths: ["D:/w/New", "D:/w/new.pkg"] });
  if (state.lists.compress.map(r => r.path + ":" + !!r.watched).join() !== "D:/w/New:true,D:/w/new.pkg:true") say("found: " + state.lists.compress.map(r => r.path + ":" + !!r.watched).join());
  // one that failed and came back changed goes again
  state.lists.compress[0].state = "error";
  state.lists.compress[0].detail = "verify_failed";
  onWatchFound({ paths: ["D:/w/New"] });
  if (state.lists.compress.length !== 2 || state.lists.compress[0].state !== "" || state.lists.compress[0].detail !== "") say("changed: " + JSON.stringify(state.lists.compress[0].state));
  if (t("watchFound")(1) === t("watchFound")(3)) say("watchFound does not count");
  // the window is told which rows the watched folder brought, and how each ended
  if (itemOf(row("D:/w/G", game())).Watched !== true || itemOf(Object.assign(row("D:/w/G", game()), { section: "details" })).Watched !== false
    || itemOf(row("D:/w/G", game(), { watched: false })).Watched !== false) say("Watched in the item");
  for (const [st, detail, final] of [["ok", "", true], ["error", "verify_failed", true], ["skipped", "watch_exists", true],
    ["cancelled", "", false], ["skipped", "fat32_too_big", false]])
    if (watchFinal({ state: st, detail }) !== final) say("final: " + st + " " + detail);
  if (typeof t("watchExisting")(3) !== "string" || t("watchExisting")(3).indexOf("3") < 0) say("watchExisting");
  for (const code of ["original_kept_changed", "original_kept_unchecked"]) if (detailText(code) !== t("detail_" + code)) say("the detail " + code);
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestPageWatch: what arrives in the watched folder goes to Compress as if dropped there; the games read and
// whole go, a DLC with its game, what lacks files of its list stays held with its warning, and one that failed
// goes again once it changed.
func TestPageWatch(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", watchChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}
