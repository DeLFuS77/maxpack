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
  const report = gameReport(row("compress", { Kind: "pfsc", Inner: "ufs2", Compressed: true, Path: "D:/Iron Harbor.ffpfsc" }));
  if (report.Kind !== "pfsc" || report.Inner !== "ufs2" || report.Compressed !== true) say("the report of a container: " + JSON.stringify(report));
  if (JSON.stringify(report).includes("Iron Harbor") || JSON.stringify(report).includes("PPSA")) say("the report has a name: " + JSON.stringify(report));
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestPageImages: the page says what each image is by the kind the engine gives (an .ffpkg is a UFS2
// image, an .ffpfsc a PFS container), takes the four kinds where it takes an .exfat, sends them to
// Compress and not to Verify, and a report says the kind of each one without its name.
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
