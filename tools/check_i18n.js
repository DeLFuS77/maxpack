// SPDX-License-Identifier: GPL-3.0-or-later
// Checks that every language file of the window (gui/frontend/i18n/<code>.js) has every text of
// en.js, and none that en.js does not have. For translators, who can run it alone:
//   node tools/check_i18n.js
// The Go test of the window (gui/frontend_test.go) checks much more (arguments, names that are
// never translated, the scripts of the page); this is the quick one.
"use strict";
const fs = require("fs"), path = require("path"), vm = require("vm");

const dir = path.join(__dirname, "..", "gui", "frontend", "i18n");
const context = { size: bytes => bytes + " B", time: seconds => seconds + " s", navigator: {} };
vm.createContext(context);
const files = fs.readdirSync(dir).filter(name => name.endsWith(".js") && name !== "base.js").sort();
let source = fs.readFileSync(path.join(dir, "base.js"), "utf8") + "\n;\n";
for (const name of files) source += fs.readFileSync(path.join(dir, name), "utf8") + "\n;\n";
vm.runInContext(source + "this.texts = texts; this.languages = languages;", context);
const { texts, languages } = context;

const problems = [];
const en = Object.keys(texts.en || {});
if (!en.length) problems.push("en.js has no texts");
for (const [code] of languages) {
  const table = texts[code];
  if (!table) { problems.push(code + ": no file with its texts"); continue; }
  for (const key of en) if (!(key in table)) problems.push(code + ": missing " + key);
  for (const key of Object.keys(table)) if (!en.includes(key)) problems.push(code + ": " + key + " is not in en.js");
}
for (const name of files) {
  const code = name.slice(0, -3);
  if (!languages.some(([known]) => known === code)) problems.push(name + " is not in the list of languages in base.js");
}
if (problems.length) {
  console.error(problems.join("\n"));
  process.exit(1);
}
console.log("ok: " + languages.length + " languages, " + en.length + " texts each");
