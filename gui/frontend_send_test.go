// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// sendChecks runs in node over the folder of the page, as growChecks does, and prints one line for each
// thing the page does not do with a .p5z sent to the console as it should. argv: the folder.
const sendChecks = `
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
  // its steps: the bytes go, then the console verifies them; its bar goes by the size of the .p5z
  if (stepsOf({ mode: "send" }).join() !== "sending,verifying") say("steps: " + stepsOf({ mode: "send" }));
  if (!working.includes("sending")) say("sending is not a state at work");
  const row = { path: "D:/g.p5z", section: "details", game: { Name: "Copper Vale", Kind: "p5z", OriginalSize: 9000, P5zSize: 3000 }, state: "", warnings: [] };
  if (sendSize(row) !== 3000) say("the size sent: " + sendSize(row));
  // the console of Settings
  for (const ip of ["192.168.1.20", "10.0.0.5:8095", "0.0.0.0"]) if (!validConsoleIp(ip)) say("refused: " + ip);
  for (const ip of ["", "192.168.1", "256.1.1.1", "console.local", "1.2.3.4:0", "1.2.3.4:70000", "1.2.3.4/24", " 1.2.3.4", "010.0.0.5", "10.0.0.5:08095"]) if (validConsoleIp(ip)) say("taken: " + ip);
  if (!validConsoleCode("004213") || validConsoleCode("12345") || validConsoleCode("12a456") || validConsoleCode("1234567")) say("the code");
  // its drives, with their free space, and whether the .p5z fit with the console's margin
  const internal = { id: "internal", free: 600e9, total: 825e9, fs: "ufs" }, usb = { id: "usb1", free: 3000, total: 64e9, fs: "exfat" };
  if (driveText(internal) !== t("driveLine")(t("driveInternal"), size(600e9), size(825e9))) say("internal: " + driveText(internal));
  if (driveText(usb) !== t("driveLine")(t("driveUsb")(2), size(3000), size(64e9))) say("usb: " + driveText(usb));
  if (!driveFits(internal, [row]) || driveFits(usb, [row]) || driveFits({ free: 3000 + 256 * 1024 * 1024 - 1 }, [row])) say("what fits");
  // what the console and the engine say, in the page's words
  for (const code of ["unreachable", "receive_off", "bad_code", "locked", "exists", "busy", "no_space", "fat32", "bad_name", "no_drive",
    "verify_failed", "console_cancelled", "console_error", "invalid"]) {
    if (detailText("send_" + code) !== t("detail_send_" + code)) say("the detail send_" + code + ": " + detailText("send_" + code));
    if (sendErrorText(code) !== t("detail_send_" + code)) say("the error " + code + ": " + sendErrorText(code));
  }
  if (sendErrorText("no_console") !== t("detail_no_console") || detailText("no_console") !== t("detail_no_console")) say("no console");
  if (sendErrorText("what", "the words") !== t("detail_other") + ": the words") say("an unknown error: " + sendErrorText("what", "the words"));
  if (detailText("send_verify_failed: ampr_zstd-001.pak") !== t("detail_send_verify_failed") + ": ampr_zstd-001.pak") say("what arrived damaged");
  if (detailText("sent") !== t("detail_sent") || !goodNote({ state: "ok", detail: "sent" })) say("sent is not good news");
  if (nextStep({ mode: "send", rows: [] }) !== t("nextSend")) say("next step: " + nextStep({ mode: "send", rows: [] }));
  // the end card of Compress sends the .p5z it made, and nothing else
  const job = { section: "compress", mode: "p5z", rows: [{ state: "ok", out: "D:/a.p5z" }, { state: "error", out: "" }, { state: "ok", out: "D:/b.p5z", unqueued: true }] };
  if (madeP5z(job).join() !== "D:/a.p5z") say("the .p5z made: " + madeP5z(job));
  if (madeP5z(Object.assign({}, job, { mode: "folder" })).length || madeP5z(Object.assign({}, job, { section: "details" })).length) say("folders or Details are sent");
  // what a send that did not end leaves on the console can be deleted there, and only that
  const left = d => ({ section: "details", game: { Kind: "p5z" }, detail: d });
  for (const d of ["send_kept", "send_console_stopped", "send_stalled", "send_write_error", "send_unreachable", "send_verify_failed: ampr_zstd-001.pak"])
    if (!leftOnConsole(left(d))) say("not left on the console: " + d);
  for (const d of ["sent", "send_exists", "send_bad_code", "send_discarded", "", "send_no_space"])
    if (leftOnConsole(left(d))) say("left on the console: " + d);
  if (leftOnConsole(Object.assign(left("send_kept"), { section: "compress" }))) say("a row of Compress");
  for (const code of ["kept", "console_stopped", "stalled", "write_error", "discarded"])
    if (detailText("send_" + code) !== t("detail_send_" + code)) say("the detail send_" + code);
  if (!goodNote({ state: "ok", detail: "send_discarded" }) && goodNote({ state: "cancelled", detail: "send_kept" })) say("good news");
  if (typeof t("discardText")("x") !== "string" || t("discardText")("Copper Vale").indexOf("Copper Vale") < 0) say("discardText");
  // the code is never in what the page sends of a game, nor in a report
  if (JSON.stringify(itemOf(row)).includes("ConsoleCode")) say("the item carries the code");
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestPageSend: Send to the console, from the card of a .p5z in Details and from the end card of Compress:
// the console of Settings, its drives and their free space, the steps and bar of the queue, and what the
// console and the engine say, in the page's words.
func TestPageSend(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", sendChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}
