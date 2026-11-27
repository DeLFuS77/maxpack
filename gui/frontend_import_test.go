// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// packedChecks runs in node over the folder of the page, as packageChecks does, and prints one line for
// each thing the page does not do with a game packed already as it should. argv: the folder.
const packedChecks = `
"use strict";
const fs = require("fs"), path = require("path"), vm = require("vm");
const dir = process.argv[1];
const element = () => ({ replaceChildren() {}, append() {}, classList: { toggle() {}, add() {}, remove() {} }, style: {},
  dataset: {}, content: { firstElementChild: { cloneNode: () => element() } }, querySelector: () => element() });
const elements = {};
const byId = id => (elements[id] = elements[id] || Object.assign(element(), { id, hidden: false, textContent: "" }));
const radios = [{ value: "again", checked: false }, { value: "keep", checked: false }];
const context = { navigator: {}, console, Promise };
context.window = context;
context.document = { readyState: "loading", addEventListener() {}, getElementById: byId,
  querySelectorAll: sel => (sel === 'input[name="packed-games"]' ? radios : []), querySelector: () => element() };
vm.createContext(context);
let source = "";
for (const name of ["i18n/base.js", "i18n/en.js", "app.js"]) source += fs.readFileSync(path.join(dir, name), "utf8") + "\n;\n";
vm.runInContext(source, context);
console.log(vm.runInContext(` + "`" + `(() => {
  const problems = [];
  const choices = () => document.querySelectorAll('input[name="packed-games"]');
  const say = text => problems.push(text);
  const row = (section, game) => ({ section, game: Object.assign({ Name: "A", TitleID: "PPSA01234", OriginalSize: 10 }, game), warnings: [], state: "", detail: "" });
  const zstd = { Kind: "folder", Packed: true, Packs: "zstd", PackedLevel: 5, OriginalSize: 100, UnpackedSize: 300, HasZstdRuntime: true, Path: "D:/games/Starfall" };
  const image = { Kind: "exfat", Packed: true, Packs: "zstd", PackedLevel: 19, OriginalSize: 100, ImageSize: 120, Path: "D:/games/Starfall.zstd" };
  const lz4 = { Kind: "folder", Packs: "lz4", OriginalSize: 100, Path: "D:/games/Copper" };
  const dump = { Kind: "folder", OriginalSize: 100, Path: "D:/games/Vale" };
  // a game packed with zstd packs goes in Compress, with no warning; one with LZ4 packs does not, and says why
  state.lists.compress = [row("compress", zstd), row("compress", image), row("compress", lz4), row("compress", dump)];
  const ready = readyRows("compress").map(r => r.game.Path);
  if (ready.join() !== [zstd.Path, image.Path, dump.Path].join()) say("the rows that go: " + ready.join());
  if (rowWarnings(state.lists.compress[0]).length) say("a zstd game is warned of: " + rowWarnings(state.lists.compress[0]).join());
  if (rowWarnings(state.lists.compress[2]).join() !== t("detail_packs_lz4")) say("an LZ4 game says: " + rowWarnings(state.lists.compress[2]).join());
  if (!packedZstd(zstd) || !packedZstd(image) || packedZstd(lz4) || packedZstd(dump) || packedZstd({ Kind: "p5z", Packed: true })) say("packedZstd");
  if (!cannotCompress(lz4) || cannotCompress(zstd) || cannotCompress(dump) || !cannotCompress({ Kind: "folder", Packed: true })) say("cannotCompress");
  // its row says it is packed already, with what and at what level
  if (packedNote(zstd) !== "already packed (zstd, level 5)") say("the note of a zstd game: " + packedNote(zstd));
  if (packedNote(dump) !== "" || packedNote(lz4) !== "") say("the note of another game");
  // a level a broken header gives is none: the note says zstd only
  if (packedNote(Object.assign({}, zstd, { PackedLevel: 0 })) !== "already packed (zstd)") say("the note without a level: " + packedNote(Object.assign({}, zstd, { PackedLevel: 0 })));
  // a .zstd (an exFAT image with a game packed already in it) is taken where an image is
  for (const section of ["compress", "details", "rename"]) if (!takes[section].some(([type]) => type === ".zstd")) say(section + " does not take a .zstd");
  for (const section of ["decompress", "verify"]) if (takes[section].some(([type]) => type === ".zstd")) say(section + " takes a .zstd");
  // a packed folder says it once, and an LZ4 one is no dump
  if (kindName(lz4) !== t("kindPacked")) say("kindName of an LZ4 folder: " + kindName(lz4));
  const stub = () => ({ textContent: "", hidden: false, className: "", classList: { toggle() {}, add() {}, remove() {} }, style: {}, dataset: {},
    replaceChildren() {}, append() {}, setAttribute() {}, removeAttribute() {}, querySelector: () => stub() });
  const cells = {}, cell = sel => (cells[sel] = cells[sel] || stub());
  const painted = Object.assign(row("compress", image), { path: image.Path, tr: { querySelector: cell, classList: { toggle() {} } } });
  paintRow(painted);
  if (!String(cells[".file"].textContent).includes("already packed (zstd, level 19)")) say("the line of a packed image: " + cells[".file"].textContent);
  const folderCells = {}, folderCell = sel => (folderCells[sel] = folderCells[sel] || stub());
  paintRow(Object.assign(row("compress", zstd), { path: zstd.Path, tr: { querySelector: folderCell, classList: { toggle() {} } } }));
  const line = String(folderCells[".file"].textContent);
  if (!line.includes("already packed (zstd, level 5)") || line.includes(t("kindPacked"))) say("the line of a packed folder: " + line);
  // its DLC go inside it, as a dump's
  const dlc = row("compress", { Kind: "folder", Content: "dlc", DLCData: true, ContentID: "UP0000-PPSA01234_00-DLCDLCDLCDLCDLC0", TitleID: "PPSA01234", Path: "D:/dlc/A" });
  state.lists.compress = [row("compress", zstd), dlc];
  if (dlcRowsOf(state.lists.compress[0]).length !== 1) say("a DLC does not go inside a zstd game");
  // the choice for the games packed already: compressed again by default, or their packs kept; shown when
  // Compress has one
  if (defaults.KeepPacks !== false) say("the default of KeepPacks: " + defaults.KeepPacks);
  state.section = "compress";
  state.settings = Object.assign({}, defaults);
  state.lists.compress = [row("compress", dump)];
  paintPackedChoice();
  if (!document.getElementById("packed-block").hidden) say("the choice shows with no packed game");
  state.lists.compress = [row("compress", zstd)];
  paintPackedChoice();
  if (document.getElementById("packed-block").hidden) say("the choice does not show with a packed game");
  if (!choices()[0].checked || choices()[1].checked) say("the choice is not Compress again by default");
  state.settings.KeepPacks = true;
  paintPackedChoice();
  if (choices()[0].checked || !choices()[1].checked) say("the choice does not show Keep their packs");
  // once made, its row says what it was made from
  if (detailText("from_packed") !== t("detail_from_packed")) say("from_packed: " + detailText("from_packed"));
  if (detailText("from_packed_kept:5") !== t("detail_from_packed_kept")(5) || !t("detail_from_packed_kept")(5).includes("5")) say("from_packed_kept: " + detailText("from_packed_kept:5"));
  const made = Object.assign(row("compress", zstd), { state: "ok", detail: "from_packed_kept:5" });
  if (!goodNote(made)) say("what it was made from is not good news");
  // and a packed game that is refused says why
  // a DLC packed already is a DLC that goes nowhere yet: it says why, and goes inside no game
  const packedDlc = row("compress", { Kind: "folder", Content: "dlc", DLCData: true, Packed: true, Packs: "zstd", PackedLevel: 19,
    ContentID: "UP0000-PPSA01234_00-DLCDLCDLCDLCDLC0", TitleID: "PPSA01234", Path: "D:/dlc/B" });
  state.lists.compress = [row("compress", zstd), packedDlc];
  if (dlcErrorOf(packedDlc) !== t("detail_dlc_packed")) say("a packed DLC says: " + dlcErrorOf(packedDlc));
  if (dlcRowsOf(state.lists.compress[0]).length !== 0 || readyRows("compress").includes(packedDlc)) say("a packed DLC goes inside a game");
  for (const code of ["packs_damaged", "packed_file_missing", "packs_lz4", "packed_and_loose", "dlc_packed"]) {
    if (detailText(code) !== t("detail_" + code) || /^Something/.test(detailText(code))) say(code + ": " + detailText(code));
  }
  // the report of a game says its packs, never its name
  const report = gameReport(row("compress", zstd));
  if (report.Packs !== "zstd" || report.PackedLevel !== 5 || JSON.stringify(report).includes("Starfall")) say("the report: " + JSON.stringify(report));
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestPagePackedGames: a game packed already with zstd packs is no error in Compress: it goes, its row says it
// is packed already and at what level, its DLC go inside it, the choice of compressing it again or keeping its
// packs shows when the list has one, and its row says, once made, what it was made from; one with LZ4 packs
// does not go, and says why.
func TestPagePackedGames(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", packedChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
	page := readPage(t, "index.html")
	for _, want := range []string{`id="packed-block"`, `name="packed-games" value="again"`, `name="packed-games" value="keep"`, `data-t="packedAgain"`, `data-t="packedKeep"`} {
		if !strings.Contains(page, want) {
			t.Errorf("index.html lacks %s", want)
		}
	}
}
