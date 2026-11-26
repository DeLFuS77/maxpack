// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// pageHarness loads the page's script in node with a stand-in for the document whose elements keep what
// is written to them (by id), and a window whose App answers as calls says. The checks are an async
// function's body that returns the problems, one per line. argv: the folder.
const pageHarness = `
"use strict";
const fs = require("fs"), path = require("path"), vm = require("vm");
const dir = process.argv[1];
const classes = () => { const on = new Set(); return { toggle(name, yes) { if (yes === undefined ? !on.has(name) : yes) on.add(name); else on.delete(name); }, add(name) { on.add(name); }, remove(name) { on.delete(name); }, contains: name => on.has(name) }; };
const element = () => { const e = { textContent: "", hidden: false, disabled: false, title: "", open: false, value: "", children: [], attributes: {}, replaceChildren() {}, append() {},
  setAttribute(name, value) { this.attributes[name] = String(value); }, getAttribute(name) { return this.attributes[name]; }, removeAttribute(name) { delete this.attributes[name]; },
  classList: classes(), style: {}, dataset: {}, content: { firstElementChild: { cloneNode: () => element() } }, querySelector: () => element(), querySelectorAll: () => [],
  addEventListener() {}, showModal() { this.open = true; }, close() { this.open = false; }, focus() {}, click() { this.clicked = (this.clicked || 0) + 1; } };
  e.firstElementChild = { style: {} }; return e; };
const elements = {};
const calls = [];
const context = { navigator: { platform: "Win32" }, console, Promise, setTimeout: () => 0, clearTimeout() {}, setInterval: () => 0, calls, elements };
context.window = context;
context.document = { readyState: "loading", addEventListener() {}, getElementById: id => elements[id] || (elements[id] = element()), querySelectorAll: () => [], querySelector: () => null,
  createElement: () => element(), documentElement: element(), body: element(), activeElement: null, hasFocus: () => true };
context.window.go = { main: { App: new Proxy({}, { get: (_, name) => async (...args) => { calls.push(name + (args.length ? " " + JSON.stringify(args) : "")); return context.answers && name in context.answers ? context.answers[name] : null; } }) } };
vm.createContext(context);
let source = "";
for (const name of ["i18n/base.js", "i18n/en.js", "app.js"]) source += fs.readFileSync(path.join(dir, name), "utf8") + "\n;\n";
vm.runInContext(source, context);
const newline = String.fromCharCode(10);
vm.runInContext("(async () => { const problems = []; const say = text => problems.push(text);" + newline + process.argv[2] + newline +
  "return problems.join(String.fromCharCode(10)); })()", context)
  .then(out => console.log(out), error => console.log("the checks broke: " + (error && error.stack || error)));
`

// runPage runs checks (see pageHarness) and reports each problem they say.
func runPage(t *testing.T, checks string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", pageHarness, "frontend", checks).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}

// TestPagePause: Pause and Resume, for the queues of Compress, Decompress and Verify: the button, what the
// window is asked, the bar and what the status says while paused, and the question when the window closes.
func TestPagePause(t *testing.T) {
	runPage(t, `
  for (const key of ["pause", "resume", "pausedStatus", "stepPaused", "closeTitlePaused", "closeTextPaused"])
    if (typeof t(key) !== "string" || !t(key)) say("no text " + key);
  state.busy = true;
  for (const section of ["compress", "decompress", "verify", "details", "rename"]) {
    state.job = { section, mode: "p5z", rows: [], running: true };
    if (canPause() !== ["compress", "decompress", "verify"].includes(section)) say("canPause in " + section + ": " + canPause());
  }
  state.job = { section: "compress", mode: "p5z", rows: [], running: true };
  paintStart();
  if ($("pause").hidden || $("pause").textContent !== t("pause") || $("pause").disabled) say("the button before: " + JSON.stringify($("pause")));
  await togglePause();
  if (calls.join() !== "Pause" || !state.paused) say("paused: " + calls + " " + state.paused);
  paintSummary();
  if ($("pause").textContent !== t("resume")) say("the button paused: " + $("pause").textContent);
  if ($("status").textContent !== t("pausedStatus")) say("the status paused: " + $("status").textContent);
  if (!$("total-bar").classList.contains("paused")) say("the bar does not look paused");
  askClose();
  if ($("close-text").textContent !== t("closeTextPaused") || $("close-title-text").textContent !== t("closeTitlePaused")) say("the question as it closes paused: " + $("close-text").textContent);
  await togglePause();
  paintSummary();
  if (calls.join() !== "Pause,Resume" || state.paused) say("resumed: " + calls + " " + state.paused);
  if ($("pause").textContent !== t("pause") || $("status").textContent === t("pausedStatus")) say("the button and status resumed");
  if ($("total-bar").classList.contains("paused")) say("the bar still looks paused");
  $("close-dialog").open = false;
  askClose();
  if ($("close-text").textContent !== t("closeText")) say("the question as it closes: " + $("close-text").textContent);
  // the window says it (job:paused); a queue that ended is not paused
  onPaused({ paused: true });
  if (!state.paused) say("job:paused was not heard");
  state.cancelling = true;
  if (canPause()) say("it can pause while it cancels");
  state.cancelling = false;
  state.job.summary = {};
  onPaused({ paused: true });
  if (state.paused) say("a queue that ended is paused");
  // Details cannot pause: no button
  state.job = { section: "details", mode: "send", rows: [], running: true };
  paintStart();
  if (!$("pause").hidden) say("Details shows Pause");
`)
}
