// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/jobs"
)

// consoleChecks runs in node over the folder of the page, as sendChecks does, and prints one line for each thing
// the Console section does not decide or say as it should: what each .p5z of the PC is against the console's
// games, what can be sent, the header's state, the covers, and what a send left on the console. argv: the folder.
const consoleChecks = `
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
  const games = [
    { titleId: "PPSA01234", version: "01.000.000", status: "" },
    { titleId: "PPSA01234", version: "01.001.000", status: "duplicate" },
    { titleId: "PPSA01235", version: "01.000.000", status: "" }];
  // the same title ID and version is on the console; the same title ID, another version; nothing of it; a .p5z
  // whose addition stopped halfway, whatever the console has
  for (const [item, want] of [
    [{ titleId: "PPSA01234", version: "01.001.000" }, "on_console"], [{ titleId: "PPSA01235", version: "02.000.000" }, "other_version"],
    [{ titleId: "PPSA01236", version: "01.000.000" }, "absent"], [{ titleId: "PPSA01236", version: "01.000.000", interrupted: true }, "needs_repair"],
    [{ titleId: "PPSA01234", version: "01.000.000", interrupted: true }, "needs_repair"], [{ titleId: "PPSA01234", version: "01.000.000" }, "on_console"]])
    if (consoleMark(item, games) !== want) say("the mark of " + JSON.stringify(item) + ": " + consoleMark(item, games) + ", not " + want);
  if (consoleMark({ titleId: "PPSA01234", version: "01.000.000" }, []) !== "absent") say("a console with no games has it");
  // only what is not there, or is there in another version, is sent; never one that could not be read
  if (canSend({ titleId: "PPSA01234", version: "01.000.000" }, games) || !canSend({ titleId: "PPSA01236", version: "1" }, games)) say("canSend");
  if (canSend({ titleId: "PPSA01236", version: "1", error: "x" }, games)) say("a .p5z with an error can be sent");
  if (!canSend({ titleId: "PPSA01235", version: "02.000.000" }, games)) say("another version cannot be sent");
  if (canSend({ titleId: "PPSA01236", version: "1", interrupted: true }, games)) say("one that needs repair can be sent");
  // a console game that is also in the PC's column: same title ID and version
  if (!alsoOnPc(games[2], [{ titleId: "PPSA01235", version: "01.000.000" }]) || alsoOnPc(games[0], [{ titleId: "PPSA01234", version: "01.001.000" }])) say("alsoOnPc");
  if (alsoOnPc(games[2], [{ titleId: "PPSA01235", version: "01.000.000", error: "x" }]) || alsoOnPc(games[2], [])) say("alsoOnPc of what could not be read");
  // the header's state, as the window tells it, and before it answers
  for (const [view, key] of [[{ state: "connected" }, "consoleConnected"], [{ state: "receive_off" }, "consoleReceiveOff"],
    [{ state: "unreachable" }, "consoleUnreachable"], [{ state: "no_console" }, "consoleNone"], [null, "consoleLooking"]]) {
    if (consoleStateKey(view) !== key) say("the state of " + JSON.stringify(view) + ": " + consoleStateKey(view));
    if (typeof t(key) !== "string" || !t(key)) say("no text " + key);
  }
  // the cover: the console's icon, else the one of the PC's .p5z of that title ID, else none
  if (consoleCover(games[0], { PPSA01234: "data:a" }, []) !== "data:a") say("the console's icon");
  if (consoleCover(games[2], {}, [{ titleId: "PPSA01235", icon: "data:b" }]) !== "data:b") say("the PC's icon");
  if (consoleCover(games[2], {}, []) !== "" || consoleCover(games[2], { PPSA01235: "" }, [{ titleId: "PPSA01235", icon: "" }]) !== "") say("no icon");
  // what a send that did not end left on the console is deleted from the row of the Console section
  const left = (d, section) => ({ section: section || "console", game: { Kind: "p5z" }, detail: d });
  if (!leftOnConsole(left("send_kept")) || !leftOnConsole(left("send_stalled"))) say("a row of the Console section with send_kept");
  if (leftOnConsole(left("send_kept", "details")) || leftOnConsole(left("sent"))) say("a row of Details, or one sent");
  // the end card of Compress sends what it made, as before
  const job = { section: "compress", mode: "p5z", rows: [{ state: "ok", out: "D:/a.p5z" }, { state: "error", out: "" }, { state: "ok", out: "D:/b.p5z", unqueued: true }] };
  if (madeP5z(job).join() !== "D:/a.p5z" || madeP5z(Object.assign({}, job, { mode: "folder" })).length) say("the .p5z made: " + madeP5z(job));
  // a send left on the console: its drive from where it is there, and its name without .maxpack-recv
  if (leftDrive("/data/homebrew/g.p5z.maxpack-recv") !== "internal" || leftDrive("/mnt/usb1/homebrew/g.p5z.maxpack-recv") !== "usb1") say("leftDrive");
  if (leftDrive("/mnt/ext0/homebrew/g.p5z") !== load("sendDrive", "")) say("leftDrive elsewhere: " + leftDrive("/mnt/ext0/homebrew/g.p5z"));
  if (leftName({ path: "/data/homebrew/G.p5z.maxpack-recv", name: "G.p5z.maxpack-recv" }) !== "G.p5z" || leftName({ path: "/mnt/usb0/homebrew/H.p5z", name: "" }) !== "H.p5z") say("leftName");
  // the console's drives in the page's words, by their id and by the label the console gives them
  if (driveName("internal") !== t("driveInternal") || driveName("usb0") !== t("driveUsb")(1)) say("driveName");
  if (diskText({ label: "Internal", free: 5, total: 10 }) !== t("driveLine")(t("driveInternal"), size(5), size(10))) say("diskText internal: " + diskText({ label: "Internal", free: 5, total: 10 }));
  if (diskText({ label: "USB 1", free: 5, total: 10 }) !== t("driveLine")(t("driveUsb")(1), size(5), size(10))) say("diskText usb");
  if (diskText({ label: "Extended 1", free: 5, total: 10 }) !== t("driveLine")(t("driveExtended")(1), size(5), size(10))) say("diskText extended: " + diskText({ label: "Extended 1", free: 5, total: 10 }));
  if (driveName("ext0") !== t("driveExtended")(1)) say("driveName ext0: " + driveName("ext0"));
  // a console game says where it is by the mount of its drive (0-based, as the console mounts them): the same names
  // as its disks, and anything else as it is
  for (const [mount, want] of [["/data", t("driveInternal")], ["/mnt/usb0", t("driveUsb")(1)], ["/mnt/usb1", t("driveUsb")(2)],
    ["/mnt/ext0", t("driveExtended")(1)], ["/mnt/ext1", t("driveExtended")(2)], ["/mnt/sandbox", "/mnt/sandbox"], ["", ""]])
    if (mountName(mount) !== want) say("mountName(" + mount + "): " + mountName(mount) + ", not " + want);
  if (typeof t("driveExtended") !== "function" || !t("driveExtended")(2).includes("2")) say("driveExtended");
  // what a console game is, and its verify
  if (consoleKindText("p5z") !== ".p5z" || consoleKindText("pkg") !== ".pkg" || consoleKindText("folder") !== t("consoleKindFolder")) say("consoleKindText");
  // the sections that take a queue: the Console section sends, and only that
  if (queues.includes("console")) say("the Console section has a queue of its own");
  if (!state.lists.console || takes.console.map(([type]) => type).join() !== ".p5z") say("the Console section's list");
  return problems.join("\\n");
})()` + "`" + `, context));
`

// TestConsolePage: what the Console section decides from what the window gives it: the mark of each .p5z of the
// PC against the console's games, which can be sent, the header's state, the covers, and what a send left there.
func TestConsolePage(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's script cannot be run")
	}
	out, err := exec.Command(node, "-e", consoleChecks, "frontend").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, problem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if problem != "" {
			t.Error(problem)
		}
	}
}

// consoleFixture is the window as the checks of the Console section give it: a library folder with a .p5z the
// console has, one it has in another version, one whose addition stopped halfway, one that could not be read, and
// one from elsewhere; a console that has two of them, and one drive.
const consoleFixture = `
  const item = (path, name, titleId, version, more) => Object.assign({ path, name, titleId, version, size: 3000, icon: "", interrupted: false, other: false, error: "" }, more || {});
  const library = { folder: "D:/lib", error: "", items: [
    item("D:/lib/A.p5z", "Copper Vale", "PPSA01234", "01.000.000"),
    item("D:/lib/B.p5z", "Iron Bay", "PPSA01235", "01.001.000", { size: 5000 }),
    item("D:/lib/C.p5z", "Glass Tide", "PPSA01236", "01.000.000", { interrupted: true }),
    item("D:/lib/D.p5z", "D.p5z", "", "", { error: "not a .p5z" }),
    item("E:/x/E.p5z", "Ember Reach", "PPSA01237", "01.000.000", { other: true })] };
  const consoleView = { state: "connected", address: "192.168.1.20", receiving: false, images: [{ path: "/data/homebrew/x.exfat", kind: "exfat", titleId: "PPSA01238" }],
    disks: [{ label: "Internal", kind: "internal", total: 2e12, free: 1e12 }], left: [],
    games: [{ titleId: "PPSA01234", name: "Copper Vale", version: "01.000.000", kind: "p5z", drive: "/mnt/usb0", size: 3000, status: "", verified: { at: 1700000000, ok: true } },
      { titleId: "PPSA01235", name: "Iron Bay", version: "01.000.000", kind: "p5z", drive: "/mnt/usb0", size: 4000, status: "", verified: null }] };
  window.answers = { Library: library, ConsoleState: consoleView, ConsoleIcon: "", ConsoleDrives: { drives: [{ id: "usb0", free: 1e12, total: 2e12, fs: "exfat" }], error: "", message: "" } };
  const intervals = [], cleared = [];
  window.setInterval = (fn, ms) => { intervals.push(ms); return 7; };
  window.clearInterval = id => { cleared.push(id); };
  const toasts = [];
  toast = text => toasts.push(text);
  render = () => {};
  state.settings.ConsoleIP = "192.168.1.20";
  state.settings.ConsoleCode = "482913";
  const rowOf = p => state.lists.console.find(row => row.path === p);
`

// TestConsolePageSend: opening the section reads the library and the console (then the console every 3 s while it
// is open, and its drives once); the rows are the library's, with what each is on the console; what is marked goes
// to the one queue, which it waits for while something else runs, as rows of the Console section.
func TestConsolePageSend(t *testing.T) {
	runPage(t, consoleFixture+`
  await setSection("console");
  if (!calls.includes("Library [[]]") || !calls.includes("ConsoleState")) say("opening it does not read the library and the console: " + calls);
  if (calls.filter(call => call === "ConsoleDrives").length !== 1) say("the drives are not asked once: " + calls);
  if (intervals.join() !== "3000") say("the console is not asked every 3 s: " + intervals);
  if (state.lists.console.map(row => row.path).join() !== "D:/lib/A.p5z,D:/lib/B.p5z,D:/lib/C.p5z,D:/lib/D.p5z,E:/x/E.p5z") say("the rows: " + state.lists.console.map(row => row.path));
  if (state.lists.console.some(row => row.section !== "console")) say("a row is not of the Console section");
  if (rowOf("D:/lib/D.p5z").error !== "not a .p5z" || rowOf("D:/lib/D.p5z").game) say("the one that could not be read: " + JSON.stringify(rowOf("D:/lib/D.p5z").error));
  const game = rowOf("D:/lib/B.p5z").game;
  if (!game || game.Kind !== "p5z" || game.Name !== "Iron Bay" || game.TitleID !== "PPSA01235" || game.Version !== "01.001.000" || sendSize(rowOf("D:/lib/B.p5z")) !== 5000) say("the game of a row: " + JSON.stringify(game));
  // the right column and the header know the console now; the PC's marks follow it
  if (consoleStateKey(state.console) !== "consoleConnected") say("the state: " + JSON.stringify(state.console));
  const marks = state.lists.console.map(row => consoleMark(row.item, state.console.games)).join();
  if (marks !== "on_console,other_version,needs_repair,absent,absent") say("the marks: " + marks);
  // a re-read asks the library again, with the files from elsewhere
  calls.length = 0;
  state.others = ["E:/x/E.p5z"];
  await loadLibrary();
  if (calls.filter(call => call.startsWith("Library")).join() !== 'Library [["E:/x/E.p5z"]]') say("Read again: " + calls);
  // marked while a compression runs: they wait, said, and nothing starts
  for (const p of ["D:/lib/A.p5z", "D:/lib/B.p5z", "D:/lib/C.p5z", "D:/lib/D.p5z", "E:/x/E.p5z"]) rowOf(p).picked = true;
  if (pickedRows().map(row => row.path).join() !== "D:/lib/B.p5z,E:/x/E.p5z") say("what is marked to send: " + pickedRows().map(row => row.path));
  state.busy = true;
  state.job = { section: "compress", mode: "p5z", rows: [], running: true };
  calls.length = 0;
  sendMarked();
  if (rowOf("D:/lib/B.p5z").state !== "waiting" || rowOf("E:/x/E.p5z").state !== "waiting" || rowOf("D:/lib/A.p5z").state) say("not waiting: " + state.lists.console.map(row => row.state));
  if (calls.some(call => /^(Plan|Start) /.test(call))) say("a send started while something runs: " + calls);
  if (!toasts.includes(t("sendQueuedWait"))) say("the wait is not said: " + toasts);
  // the compression ends: they go, to the drive chosen, as rows of the Console section
  window.answers.Plan = [{ Item: { Name: "Iron Bay" }, Block: "", Warnings: [], Replace: null }, { Item: { Name: "Ember Reach" }, Block: "", Warnings: [], Replace: null }];
  window.answers.Space = [];
  window.answers.Start = null;
  onDone({ Done: 0, Failed: 0 });
  for (let n = 0; n < 50; n++) await null;
  const start = calls.find(call => call.startsWith("Start "));
  if (!start || !start.startsWith('Start ["console",') || !start.includes('"Mode":"send"') || !start.includes('"Drive":"usb0"') || !start.includes("Iron Bay") || !start.includes("Ember Reach")) say("the send: " + start);
  if (!state.job || state.job.section !== "console" || state.job.mode !== "send" || state.job.rows.map(row => row.path).join() !== "D:/lib/B.p5z,E:/x/E.p5z") say("the queue: " + JSON.stringify(state.job && state.job.section));
  if (rowOf("D:/lib/B.p5z").sentDrive !== "usb0") say("the drive it went to: " + rowOf("D:/lib/B.p5z").sentDrive);
  onItem({ i: 1, state: "sending", detail: "" });
  if (rowOf("E:/x/E.p5z").state !== "sending" || stepsOf(state.job).join() !== "sending,verifying") say("its bar: " + rowOf("E:/x/E.p5z").state);
  // what is sending stays in the list, whatever the folder says meanwhile
  window.answers.Library = { folder: "D:/lib", error: "", items: [] };
  await loadLibrary();
  if (state.lists.console.map(row => row.path).join() !== "D:/lib/B.p5z,E:/x/E.p5z") say("a re-read during the send: " + state.lists.console.map(row => row.path));
  // left: the console is asked no more
  setSection("details");
  if (cleared.join() !== "7") say("the console is still asked: " + cleared);
`)
}

// TestConsolePageSendWhileSending: while a send of the Console section runs, Send stays beside Cancel, and what is
// marked then says Waiting and goes once that send ends, in the one queue.
func TestConsolePageSendWhileSending(t *testing.T) {
	runPage(t, consoleFixture+`
  await setSection("console");
  const plan = name => ({ Item: { Name: name }, Block: "", Warnings: [], Replace: null });
  window.answers.Plan = [plan("Iron Bay")];
  window.answers.Space = [];
  window.answers.Start = null;
  rowOf("D:/lib/B.p5z").picked = true;
  await sendMarked();
  if (!running() || state.job.section !== "console" || state.job.rows.map(row => row.path).join() !== "D:/lib/B.p5z") say("the first send did not start");
  paintStart();
  if ($("start").hidden || $("cancel").hidden) say("Send is not beside Cancel while the send runs: start hidden " + $("start").hidden + ", cancel hidden " + $("cancel").hidden);
  rowOf("E:/x/E.p5z").picked = true;
  paintStart();
  if ($("start").disabled) say("Send cannot be pressed while the send runs");
  calls.length = 0;
  window.answers.Plan = [plan("Ember Reach")];
  await sendMarked();
  if (rowOf("E:/x/E.p5z").state !== "waiting") say("what is marked during the send does not wait: " + rowOf("E:/x/E.p5z").state);
  if (calls.some(call => /^(Plan|Start) /.test(call))) say("a second queue started: " + calls);
  if (state.job.rows.includes(rowOf("E:/x/E.p5z"))) say("it joined the send at work");
  // the first send ends: the one that waited goes
  onDone({ Done: 1, Failed: 0 });
  for (let n = 0; n < 50; n++) await null;
  const start = calls.find(call => call.startsWith("Start "));
  if (!start || !start.startsWith('Start ["console",') || !start.includes("Ember Reach") || start.includes("Iron Bay")) say("after the first send: " + start);
`)
}

// TestConsolePageRouting: Send to the console of Details and the end card of Compress open the Console section
// with those .p5z marked (also from outside the library), and what the watched folder made goes to its queue there.
func TestConsolePageRouting(t *testing.T) {
	runPage(t, consoleFixture+`
  await openConsoleWith(["E:/x/E.p5z", "D:/lib/B.p5z"]);
  if (state.section !== "console") say("the section is not open: " + state.section);
  if (state.others.join() !== "E:/x/E.p5z,D:/lib/B.p5z") say("the files from elsewhere: " + state.others);
  if (!calls.includes('Library [["E:/x/E.p5z","D:/lib/B.p5z"]]')) say("the library is not read with them: " + calls);
  for (const p of ["E:/x/E.p5z", "D:/lib/B.p5z"]) if (!rowOf(p).picked) say("not marked: " + p);
  if (rowOf("D:/lib/A.p5z").picked) say("marked: A");
  // once more: each is there once
  await openConsoleWith(["E:/x/E.p5z"]);
  if (state.others.join() !== "E:/x/E.p5z,D:/lib/B.p5z") say("the files from elsewhere, again: " + state.others);
  // the end card of Compress
  state.section = "compress";
  await sendMade({ section: "compress", mode: "p5z", rows: [{ state: "ok", out: "D:/lib/C.p5z" }] });
  if (state.section !== "console" || !state.others.includes("D:/lib/C.p5z")) say("the end card: " + state.section + " " + state.others);
  // one that needs repair is not marked, and opens in Details
  if (rowOf("D:/lib/C.p5z").picked) say("one that needs repair is marked");
  window.answers.Details = { Path: "D:/lib/C.p5z", Name: "Glass Tide", Kind: "p5z", TitleID: "PPSA01236", Version: "01.000.000" };
  repairInDetails(rowOf("D:/lib/C.p5z"));
  if (state.section !== "details" || !state.lists.details.some(row => row.path === "D:/lib/C.p5z")) say("needs repair does not open Details");
  // the watched folder: what it made goes to the Console section's queue, to the drive chosen last
  state.section = "compress";
  state.lists.console.forEach(row => { row.picked = false; });
  state.busy = true;
  state.job = { section: "compress", mode: "p5z", rows: [], running: true };
  calls.length = 0;
  await sendWatched({ section: "compress", mode: "p5z", watched: true, rows: [{ path: "D:/w/G", watched: true, state: "ok", out: "D:/lib/A.p5z", detail: "" }] });
  if (state.section !== "compress") say("the watched folder opened the section");
  if (rowOf("D:/lib/A.p5z").state !== "waiting" || !rowOf("D:/lib/A.p5z").sendWait || !rowOf("D:/lib/A.p5z").sendWait.auto) say("the watched send does not wait in the Console section: " + rowOf("D:/lib/A.p5z").state);
  if (calls.some(call => call.startsWith("Plan "))) say("a send started while something runs");
  // Delete what arrived, and the console letting go of it, in the Console section's rows
  const sent = rowOf("D:/lib/A.p5z");
  Object.assign(sent, { state: "cancelled", detail: "send_kept", sendWait: null });
  onDiscardWaiting({ path: "D:/lib/A.p5z", seconds: 30 });
  if (sent.detail !== "send_letting_go: 30") say("letting go: " + sent.detail);
`)
}

// TestConsolePageConnect: the connect box checks the address and the code before the window is asked, saves them
// once, says why the console did not take them, and fills the address from the network; Delete what arrived of a
// send left on the console goes to the drive it is on, by its name.
func TestConsolePageConnect(t *testing.T) {
	runPage(t, consoleFixture+`
  state.settings.ConsoleIP = "";
  state.settings.ConsoleCode = "";
  $("connect-ip").value = "192.168.1";
  $("connect-code").value = "482913";
  await saveConsole();
  if (calls.some(call => call.startsWith("SaveConsole")) || $("connect-status").textContent !== t("consoleIpBad")) say("a bad address: " + $("connect-status").textContent);
  $("connect-ip").value = "192.168.1.20";
  $("connect-code").value = "4829";
  await saveConsole();
  if (calls.some(call => call.startsWith("SaveConsole")) || $("connect-status").textContent !== t("consoleCodeBad")) say("a bad code: " + $("connect-status").textContent);
  // Settings could not be written: its own words, and nothing kept here
  $("connect-code").value = "482913";
  window.answers.SaveConsole = { error: "save_failed", message: "access denied" };
  await saveConsole();
  if ($("connect-status").textContent !== t("connectSaveFailed") + ": access denied" || state.settings.ConsoleIP) say("save_failed: " + $("connect-status").textContent);
  // the console refused the code: said, kept as Settings keeps it, and the code is not asked again by itself
  window.answers.SaveConsole = { error: "bad_code", message: "" };
  calls.length = 0;
  await saveConsole();
  if (calls.filter(call => call.startsWith("SaveConsole")).join() !== 'SaveConsole ["192.168.1.20","482913"]') say("saved: " + calls);
  if ($("connect-status").textContent !== sendErrorText("bad_code") || state.settings.ConsoleIP !== "192.168.1.20" || state.settings.ConsoleCode !== "482913") say("bad_code: " + $("connect-status").textContent);
  // the box stays open with what the console said, although Settings has a console now
  if (!state.connect.open || $("console-connect").hidden) say("the box closed on a refused code");
  state.console = consoleView;
  await ensureDrives();
  if (calls.includes("ConsoleDrives")) say("a refused code is tried again");
  // taken: the box closes and says so
  window.answers.SaveConsole = { error: "", message: "" };
  state.connect.open = true;
  await saveConsole();
  if (state.connect.open || !toasts.includes(t("connectOk"))) say("taken: " + state.connect.open + " " + toasts);
  // the network: the first console found fills the address; none found is said
  window.answers.FindConsoles = ["192.168.1.30", "192.168.1.31"];
  await findConsoles();
  if ($("connect-ip").value !== "192.168.1.30" || $("connect-status").textContent !== t("connectFound")("192.168.1.30, 192.168.1.31")) say("found: " + $("connect-ip").value + " " + $("connect-status").textContent);
  window.answers.FindConsoles = [];
  await findConsoles();
  if ($("connect-status").textContent !== t("connectNoneFound")) say("none found: " + $("connect-status").textContent);
  // a send left on the console: asked first, then deleted on its drive by its name
  ask = async () => "delete";
  window.answers.DiscardOnConsole = { deleted: true, had: 5, error: "" };
  calls.length = 0;
  await discardLeft({ path: "/mnt/usb1/homebrew/G.p5z.maxpack-recv", name: "G.p5z.maxpack-recv", size: 5 });
  if (!calls.includes('DiscardOnConsole ["G.p5z","usb1"]')) say("Delete what arrived: " + calls);
  if (!toasts.includes(t("discardDone")("G.p5z"))) say("not said: " + toasts);
  // no library folder: the folder where Compress leaves them is offered, and kept when used
  state.settings.LastDest = "E:/made";
  window.answers.Library = { folder: "", error: "no_folder", items: [] };
  await loadLibrary();
  paintConsole();
  if ($("library-ask").hidden || $("library-use").hidden || !$("library-proposed").textContent.includes("E:/made")) say("no folder: the question is not asked");
  calls.length = 0;
  window.answers.Library = library;
  await useLastDest();
  if (!calls.some(call => call.startsWith("SetSettings ") && call.includes('"LibraryFolder":"E:/made"')) || !calls.some(call => call.startsWith("Library "))) say("Use this folder: " + calls);
`)
}

// TestSectionConsoleOnlySends: a queue of the Console section sends .p5z to the console, and does nothing else.
func TestSectionConsoleOnlySends(t *testing.T) {
	if o, err := sectionOptions("console", jobs.Options{Mode: jobs.ModeSend}); err != nil || o.Mode != jobs.ModeSend {
		t.Errorf("a send of the Console section: %v %v", o.Mode, err)
	}
	for _, mode := range []jobs.Mode{jobs.ModeP5z, jobs.ModeVerify, jobs.ModeAddDLC, ""} {
		if _, err := sectionOptions("console", jobs.Options{Mode: mode}); err == nil {
			t.Errorf("the Console section takes mode %q", mode)
		}
	}
}
