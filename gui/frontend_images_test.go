// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// imageChecks runs in node over the folder of the page, as runtimeChecks does: it loads base.js,
// en.js and app.js without a window and prints one line for each thing the page does not do with
// the images of ShadowMountPlus as it should. argv: the folder.
const imageChecks = `
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
  // what each kind is called: by the kind the engine gives, not by the extension
  for (const [game, want] of [
    [{ Kind: "folder" }, "dump folder"], [{ Kind: "folder", Packed: true }, "compressed folder"], [{ Kind: "p5z", Packed: true }, ".p5z"],
    [{ Kind: "exfat" }, "exFAT image"], [{ Kind: "ufs2" }, "UFS2 image"], [{ Kind: "pfs" }, "PFS image"], [{ Kind: "pfsc", Inner: "exfat" }, "PFS container"],
    [{ Kind: "later" }, "image"],
  ]) {
    if (kindName(game) !== want) say("kindName(" + JSON.stringify(game) + ") is " + kindName(game) + ", not " + want);
  }
  for (const [kind, want] of [["exfat", "exFAT image"], ["ufs2", "UFS2 image"], ["pfs", "PFS image"]]) {
    if (imageName(kind) !== want) say("what a container holds, " + kind + ": " + imageName(kind));
  }
  // the sections that take an .exfat take the other three, and the ones that do not, none
  for (const section of ["compress", "details", "rename"]) {
    const types = takes[section].map(([type]) => type).join(" ");
    if (!types.includes(".exfat .ffpkg .ffpfs .ffpfsc")) say(section + " takes " + types);
  }
  for (const section of ["decompress", "verify"]) {
    if (takes[section].some(([type]) => type.startsWith(".ff") || type === ".exfat")) say(section + " takes an image");
  }
  // an image goes to a queue of Compress; in Verify (an image named as a .p5z) it has nothing to verify
  const row = (section, game) => ({ section, game: Object.assign({ Name: "A", TitleID: "PPSA00000", OriginalSize: 10 }, game), warnings: [], state: "", detail: "" });
  for (const kind of ["exfat", "ufs2", "pfs", "pfsc"]) {
    const image = { Kind: kind, Inner: kind === "pfsc" ? "pfs" : "", Compressed: kind === "pfsc" };
    state.lists.compress = [row("compress", image)];
    if (readyRows("compress").length !== 1) say("an image (" + kind + ") does not go in Compress");
    if (rowWarnings(state.lists.compress[0]).length) say("an image (" + kind + ") is warned of in Compress: " + rowWarnings(state.lists.compress[0]).join(", "));
    state.lists.verify = [row("verify", image)];
    if (readyRows("verify").length !== 0) say("an image (" + kind + ") goes in a queue of Verify");
    if (!rowWarnings(state.lists.verify[0]).includes(t("detail_not_packed"))) say("an image (" + kind + ") in Verify is not said to have nothing to verify");
  }
  state.lists.verify = [row("verify", { Kind: "p5z", Packed: true }), row("verify", { Kind: "folder", Packed: true }), row("verify", { Kind: "folder" })];
  if (readyRows("verify").length !== 2) say("Verify takes " + readyRows("verify").length + " of a .p5z, a compressed folder and a dump");
  // a report says what the image is, never its name
  const report = gameReport(row("compress", { Kind: "pfsc", Inner: "ufs2", Compressed: true, ImageSize: 7, Path: "D:/Iron Harbor.ffpfsc" }));
  if (report.Kind !== "pfsc" || report.Inner !== "ufs2" || report.Compressed !== true || report.ImageSize !== 7) say("the report of a container: " + JSON.stringify(report));
  if (JSON.stringify(report).includes("Iron Harbor") || JSON.stringify(report).includes("PPSA")) say("the report has a name: " + JSON.stringify(report));
  // what a game took before: an image's file on disk, a folder the game; an image whose file is not
  // known, nothing (no saving is claimed)
  for (const [game, want] of [
    [{ Kind: "folder", OriginalSize: 100 }, 100], [{ Kind: "pfsc", OriginalSize: 100, ImageSize: 70 }, 70],
    [{ Kind: "exfat", OriginalSize: 100, ImageSize: 104 }, 104], [{ Kind: "ufs2", OriginalSize: 100 }, 0],
  ]) {
    if (sizeBefore(game) !== want) say("sizeBefore(" + JSON.stringify(game) + ") is " + sizeBefore(game) + ", not " + want);
  }
  // the end card is against what each took before: the image's file for an image
  const made = (game, outSize) => Object.assign(row("compress", game), { state: "ok", outSize });
  const card = doneSizes({ section: "compress", rows: [made({ Kind: "pfsc", OriginalSize: 100, ImageSize: 70 }, 60), made({ Kind: "folder", OriginalSize: 50 }, 20)] });
  if (!card || card.before !== 120 || card.after !== 80) say("the end card: " + JSON.stringify(card));
  if (doneSizes({ section: "compress", rows: [made({ Kind: "pfsc", OriginalSize: 100 }, 60)] }) !== null) say("the end card claims a saving against an image of unknown size");
  // Compare levels: for a compressed image, what a level gives against its file too; nothing more
  // for anything else
  const container = { Kind: "pfsc", Compressed: true, OriginalSize: 100, ImageSize: 80, Path: "D:/x/Tide.FFPFSC" };
  if (vsImage(container, 60) !== "vs your .ffpfsc: " + t("smallerBy")(25)) say("vsImage smaller: " + vsImage(container, 60));
  if (vsImage(container, 88) !== "vs your .ffpfsc: " + t("largerBy")(10)) say("vsImage larger: " + vsImage(container, 88));
  if (vsImage(Object.assign({}, container, { Kind: "pfs" }), 60) !== "vs your .ffpfsc: " + t("smallerBy")(25)) say("a compressed .ffpfs: " + vsImage(Object.assign({}, container, { Kind: "pfs" }), 60));
  for (const game of [{ Kind: "pfs", Compressed: false, ImageSize: 80 }, { Kind: "exfat", ImageSize: 80 }, { Kind: "folder", OriginalSize: 100 }, { Kind: "pfsc", Compressed: true }]) {
    if (vsImage(game, 60) !== "") say("vsImage(" + JSON.stringify(game) + ") says " + vsImage(game, 60));
  }
  if (!t("levelsImage")(".ffpfsc", "3 GB", "4 GB").includes(".ffpfsc")) say("levelsImage does not name the image");
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestPageImages: the page says what each image is by the kind the engine gives (an .ffpkg is a UFS2
// image, an .ffpfsc a PFS container), takes the four kinds where it takes an .exfat, sends them to
// Compress and not to Verify, and a report says the kind of each one without its name. What a game
// took before, on the end card, is an image's file, never more; Compare levels says what each level
// gives against a compressed image's file too.
func TestPageImages(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", imageChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}
