// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// dlcChecks runs in node over the folder of the page, as packageChecks does, and prints one line for
// each thing the page does not do with DLC as it should. argv: the folder.
const dlcChecks = `
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
  const row = (path, game) => ({ path, section: "compress", game: game && Object.assign({ Name: "A", TitleID: "PPSA00000", OriginalSize: 10, Kind: "folder" }, game),
    warnings: [], state: "", detail: "", error: "" });
  const A = "UP0000-PPSA00000_00-AAAAAAAAAAAAAAAA", B = "UP0000-PPSA00000_00-BBBBBBBBBBBBBBBB";
  const dlcA = row("D:/dlc/a.pkg", { Name: "Bonus", Kind: "fpkg", Content: "dlc", DLCData: true, ContentID: A, OriginalSize: 100 });
  const game1 = row("D:/games/one", { Name: "Copper Vale" });
  const dlcB = row("D:/dlc/b", { Name: "License", Content: "dlc", DLCData: false, ContentID: B, OriginalSize: 5 });
  const twin = row("D:/dlc/a-again", { Content: "dlc", DLCData: true, ContentID: A, OriginalSize: 100 });
  const other = row("D:/dlc/o.pkg", { Content: "dlc", ContentID: "UP0000-PPSA09999_00-CCCCCCCCCCCCCCCC", TitleID: "PPSA09999" });
  const game2 = row("D:/games/one-v2", { Name: "Copper Vale v2" });
  const game3 = row("D:/games/three", { Name: "Third", TitleID: "PPSA01234" });
  // a game whose param.json gives no titleId goes with the DLC of its content ID's title
  const game4 = row("D:/games/four", { Name: "Fourth", TitleID: "", ContentID: "UP0000-PPSA09999_00-MAXPACKTEST00000" });
  state.lists.compress = [dlcA, game1, dlcB, twin, other, game2, game3, game4];
  const paths = list => list.map(r => r.path).join(" ");
  // a DLC is never a game of the queue; a game is
  if (paths(readyRows("compress")) !== paths([game1, game2, game3, game4])) say("ready: " + paths(readyRows("compress")));
  if (canCompare(dlcA)) say("the levels of a DLC can be compared");
  // each game gets the DLC of its title, in the order of the list, one per content ID; every game of that
  // title does; what they take is what goes inside (a license adds no files)
  for (const g of [game1, game2]) {
    const item = itemOf(g);
    if (JSON.stringify(item.AddDLC) !== JSON.stringify(["D:/dlc/a.pkg", "D:/dlc/b"]) || item.AddDLCSize !== 100) say("the DLC of " + g.path + ": " + JSON.stringify(item.AddDLC) + " " + item.AddDLCSize);
  }
  // what a game made with its DLC took before is the game and them, as what was made holds both; an
  // image of unknown size claims nothing
  const items = itemsOf([game1]);
  if (game1.dlcSentSize !== 100 || beforeOf(game1) !== 110 || items[0].AddDLCSize !== 100) say("before: " + game1.dlcSentSize + " " + beforeOf(game1));
  const lost = row("D:/g.exfat", { Kind: "exfat", ImageSize: 0 });
  lost.dlcSentSize = 100;
  if (beforeOf(lost) !== 0) say("an image of unknown size: " + beforeOf(lost));
  game1.dlcSent = undefined;
  game1.dlcSentSize = undefined;
  if (itemOf(game3).AddDLC.length !== 0) say("a game of another title gets DLC: " + itemOf(game3).AddDLC);
  if (JSON.stringify(itemOf(game4).AddDLC) !== JSON.stringify(["D:/dlc/o.pkg"])) say("by its content ID: " + itemOf(game4).AddDLC);
  if (itemOf(dlcA).AddDLC.length !== 0) say("a DLC gets DLC");
  // the rows say it: a game how many go inside it, a DLC which game it goes in, or why it goes in none
  if (dlcCountOf(game1) !== 2 || dlcCountOf(game3) !== 0) say("counts: " + dlcCountOf(game1) + " " + dlcCountOf(game3));
  if (dlcLineText(dlcA) !== t("dlcGoesWith")("Copper Vale, Copper Vale v2")) say("dlcA says " + dlcLineText(dlcA));
  if (!rowWarnings(twin).includes(t("dlcTwin")) || dlcLineText(twin) !== "") say("the twin: " + rowWarnings(twin) + " / " + dlcLineText(twin));
  state.lists.compress = [dlcA, game1, dlcB, other, game2, game3];
  if (!rowWarnings(other).includes(t("dlcNoGame")("PPSA09999"))) say("another game's: " + rowWarnings(other));
  if (rowWarnings(dlcA).length) say("dlcA warns: " + rowWarnings(dlcA));
  // a DLC still being read goes with nothing until it is read, and then with its game, wherever it is
  const reading = row("D:/dlc/c", null);
  state.lists.compress = [game1, reading];
  if (itemOf(game1).AddDLC.length !== 0) say("a DLC being read goes");
  reading.game = Object.assign({ Name: "C", TitleID: "PPSA00000", OriginalSize: 1 }, { Content: "dlc", ContentID: "UP0000-PPSA00000_00-DDDDDDDDDDDDDDDD" });
  if (JSON.stringify(itemOf(game1).AddDLC) !== JSON.stringify(["D:/dlc/c"])) say("read after its game: " + itemOf(game1).AddDLC);
  // its game taken out of the list: the DLC says so, and goes nowhere
  state.lists.compress = [dlcA, dlcB];
  if (!rowWarnings(dlcA).includes(t("dlcNoGame")("PPSA00000")) || readyRows("compress").length) say("without its game: " + rowWarnings(dlcA));
  // a game made with them: the DLC says where it went
  state.lists.compress = [dlcA, game1];
  Object.assign(game1, { state: "ok", dlcSent: ["D:/dlc/a.pkg"] });
  if (dlcLineText(dlcA) !== t("dlcPackedInto")("Copper Vale")) say("once made: " + dlcLineText(dlcA));
  // a DLC added once its game is made did not go inside it, and says so; the game counts what went
  const after = row("D:/dlc/e", { Content: "dlc", DLCData: true, ContentID: "UP0000-PPSA00000_00-EEEEEEEEEEEEEEEE" });
  state.lists.compress = [dlcA, game1, after];
  if (dlcLineText(after) !== "" || !rowWarnings(after).includes(t("dlcLate")("Copper Vale"))) say("after its game was made: " + dlcLineText(after) + " / " + rowWarnings(after));
  if (dlcCountOf(game1) !== 1) say("the game made counts " + dlcCountOf(game1));
  // a game that failed takes it the next time
  game1.state = "error";
  if (dlcLineText(after) !== t("dlcGoesWith")("Copper Vale") || rowWarnings(after).length) say("after its game failed: " + dlcLineText(after) + " / " + rowWarnings(after));
  Object.assign(game1, { state: "", dlcSent: undefined });
  // the report counts them, never their content IDs
  state.lists.compress = [dlcA, game1, dlcB];
  const report = gameReport(game1);
  if (report.DLC !== 2 || report.DLCData !== 1 || JSON.stringify(report).includes("AAAA")) say("the report of a game with DLC: " + JSON.stringify(report));
  const p5z = { Kind: "p5z", Name: "X", DLC: [{ ContentID: A, Data: true, Folder: "addcont0", Size: 2048 }, { ContentID: B, Data: false, Folder: "", Size: 0 }] };
  const carried = gameReport({ section: "details", game: p5z, state: "", detail: "", path: "D:/x.p5z" });
  if (carried.DLC !== 2 || carried.DLCData !== 1 || JSON.stringify(carried).includes("AAAA")) say("the report of a .p5z with DLC: " + JSON.stringify(carried));
  // once the game is made, its report counts the DLC that went inside it, not the rows the list has now
  state.lists.compress = [dlcA, game1, dlcB];
  itemsOf([game1]);
  game1.state = "ok";
  const later = row("D:/dlc/later", { Content: "dlc", DLCData: true, ContentID: "UP0000-PPSA00000_00-LATERLATERLATER1" });
  state.lists.compress = [dlcA, game1, dlcB, later];
  let made = gameReport(game1);
  if (made.DLC !== 2 || made.DLCData !== 1) say("the report of a game made, with a DLC added after it: " + JSON.stringify(made));
  state.lists.compress = [game1];
  made = gameReport(game1);
  if (made.DLC !== 2 || made.DLCData !== 1) say("the report of a game made, its DLC rows taken away: " + JSON.stringify(made));
  Object.assign(game1, { state: "", dlcSent: undefined, dlcSentSize: undefined, dlcSentData: undefined });
  // Details lists what a .p5z carries, and says what a DLC is
  const facts = dlcFacts(p5z).map(f => f.join(": ")).join(" | ");
  if (facts !== [t("factDlc") + ": " + t("dlcCarried")(1, 1), "DLC 1: " + A + " · " + t("dlcFactData")(size(2048), "addcont0"),
    "DLC 2: " + B + " · " + t("dlcFactLicense")].join(" | ")) say("the facts: " + facts);
  if (dlcFacts({ Kind: "folder" }).length) say("a game without DLC has facts of them");
  if (kindName(dlcA.game) !== t("kindFpkg") + " · " + t("kindDlc") || kindName(dlcB.game) !== t("kindDump") + " · " + t("kindDlcLicense")) say("kindName of a DLC: " + kindName(dlcA.game) + " / " + kindName(dlcB.game));
  // what the engine says when a DLC stops a game, in the page's words
  for (const code of ["dlc_has_ini", "dlc_too_many", "dlc_refused"]) if (detailText(code) !== t("detail_" + code)) say("the detail " + code + ": " + detailText(code));
  // a DLC row the engine would refuse is an error on its own row and goes inside no game, so it cannot
  // stop one: no content ID dlc_emu takes (two of them are not "the same DLC"), or the game's own
  game1.game.ContentID = "UP0000-PPSA00000_00-MAXPACKTEST00000";
  const noId = row("D:/dlc/noid", { Content: "dlc", DLCData: true, ContentID: "" });
  const noId2 = row("D:/dlc/noid2", { Content: "dlc", DLCData: true, ContentID: "" });
  const shortId = row("D:/dlc/short", { Content: "dlc", DLCData: true, ContentID: "UP0000-PPSA00000_00-SHORTLABEL00001" });
  const itself = row("D:/games/one-copy", { Content: "dlc", DLCData: true, ContentID: "UP0000-PPSA00000_00-MAXPACKTEST00000" });
  state.lists.compress = [noId, game1, noId2, shortId, itself, dlcA];
  if (JSON.stringify(itemOf(game1).AddDLC) !== JSON.stringify(["D:/dlc/a.pkg"])) say("the DLC sent with bad ones in the list: " + itemOf(game1).AddDLC);
  for (const bad of [noId, noId2, shortId]) {
    if (dlcErrorOf(bad) !== t("dlcBadId") || rowWarnings(bad).length || dlcLineText(bad) !== "") say(bad.path + ": " + dlcErrorOf(bad) + " / " + rowWarnings(bad) + " / " + dlcLineText(bad));
  }
  if (dlcErrorOf(itself) !== t("dlcIsGame")("Copper Vale") || rowWarnings(itself).length || dlcLineText(itself) !== "") say("the game itself: " + dlcErrorOf(itself) + " / " + rowWarnings(itself));
  if (dlcErrorOf(dlcA) !== "" || dlcErrorOf(game1) !== "") say("errors where there are none: " + dlcErrorOf(dlcA) + " / " + dlcErrorOf(game1));
  if (dlcCountOf(game1) !== 1) say("the game counts " + dlcCountOf(game1));
  game1.game.ContentID = undefined;
  // the foot of the list counts the DLC apart from the games: they go inside them
  const counted = [game1, dlcA, dlcB, game3];
  const bytes = counted.reduce((sum, r) => sum + r.game.OriginalSize, 0);
  if (listedText(counted) !== t("listedDlc")(2, 2, bytes)) say("the foot of the list: " + listedText(counted));
  if (listedText([game1, game3]) !== t("listed")(2, 20)) say("the foot of a list without DLC: " + listedText([game1, game3]));
  if (listedText([]) !== t("nothing")) say("the foot of an empty list: " + listedText([]));
  // Compress waits for what is still being read: a DLC of the game may be among it, and must not be
  // left out; the reason is said
  const pending = row("D:/dlc/pending", null);
  state.lists.compress = [game1, pending];
  state.section = "compress";
  state.settings.LastDest = "E:\\\\";
  if (readingRows("compress").length !== 1 || readingRows("verify").length !== 0) say("being read: " + readingRows("compress").length);
  startQueue();
  if (state.busy) say("Compress started while a row was being read");
  state.busy = false;
  if (!String(t("readingWait")(2)).includes("2")) say("the reason: " + t("readingWait")(2));
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestPageDLC: in Compress a DLC is not a game of the queue but goes inside every game of its title
// ID, in the order of the list and once per content ID, read before or after its game; each row says
// how many go inside it, or which game a DLC goes in, or why it goes in none; a report counts them
// without naming them; Details lists the DLC of a .p5z and says what a DLC is; and what the engine says
// when a DLC stops a game is said in the page's words.
func TestPageDLC(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", dlcChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}
