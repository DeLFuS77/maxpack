// SPDX-License-Identifier: GPL-3.0-or-later

package main

import "testing"

// watchRedoChecks: what the watched folder does with a game that is in the destination already. The .p5z it
// made itself of a game that grew since is made again in its place, without asking (WatchRedo), asked for apart
// (Redo, never Replace); anything else is left out with the window's reason (WatchSkip): another version newer or
// older, only dates changed, made again too often, or simply there. The same when the game joins a queue at work.
const watchRedoChecks = `
  render = () => {};
  paintRow = () => {};
  paintSummary = () => {};
  toast = () => {};
  const game = (name, version) => ({ Name: name, Kind: "folder", TitleID: "PPSA10001", Version: version, OriginalSize: 9000 });
  const row = (name, version) => ({ path: "D:/w/" + name, section: "compress", game: game(name, version), error: "", state: "", detail: "", warnings: [], watched: true });
  const view = (name, more) => Object.assign({ Item: { Name: name }, Out: "D:/d/" + name + ".p5z", Replace: ["D:/d/" + name + ".p5z"], Warnings: [], Block: "" }, more);
  const views = [view("A", { WatchRedo: true }), view("B", { WatchSkip: "watch_other_version: 01.000.000 01.002.000" }), view("C", { WatchSkip: "watch_older_version: 01.002.000 01.000.000" })];
  state.settings.LastDest = "D:/d";
  // a queue started by the watched folder
  let rows = [row("A", "01.000.000"), row("B", "01.002.000"), row("C", "01.000.000")];
  state.lists.compress = rows;
  window.answers = { Plan: views, Space: [], Start: null };
  await startQueue("", rows, undefined, true);
  const start = calls.find(call => call.startsWith("Start "));
  if (!start || !start.includes('"Redo":["D:/d/A.p5z"]') || !start.includes('"Replace":[]')) say("the .p5z it made is not asked to be made again apart: " + start);
  if (rows[1].state !== "skipped" || rows[1].detail !== "watch_other_version: 01.000.000 01.002.000") say("another version: " + rows[1].state + " " + rows[1].detail);
  if (rows[2].state !== "skipped" || rows[2].detail !== "watch_older_version: 01.002.000 01.000.000") say("an older one: " + rows[2].state + " " + rows[2].detail);
  if (detailText("watch_older_version: 01.002.000 01.000.000") !== t("detail_watch_older_version")("01.002.000", "01.000.000")
    || t("detail_watch_older_version")("01.002.000", "01.000.000").includes(t("updateCard"))) say("the older one: " + detailText("watch_older_version: 01.002.000 01.000.000"));
  for (const code of ["watch_dates_only", "watch_redo_limit", "watch_redo_refused"]) {
    if (detailText(code) !== t("detail_" + code)) say("the detail " + code);
    if (!watchFinal({ state: "skipped", detail: code })) say("not final: " + code);
  }
  // a redo whose game changed again before its turn comes again; the old .p5z went to the bin, or was kept beside
  if (detailText("watch_redo_changed") !== t("detail_watch_redo_changed") || watchFinal({ state: "skipped", detail: "watch_redo_changed" })) say("watch_redo_changed");
  if (detailText("watch_redone; watch_old_in_bin") !== t("detail_watch_redone") + "; " + t("detail_watch_old_in_bin")) say("in the bin");
  if (detailText("watch_old_kept: D:/d/A.p5z.maxpack-old-20261126-101500") !== t("detail_watch_old_kept") + ": D:/d/A.p5z.maxpack-old-20261126-101500") say("kept beside");
  // a swap a program held too long is not final: the game comes again in the next run
  if (watchFinal({ state: "error", detail: "swap_failed: in use" }) || !watchFinal({ state: "error", detail: "verify_failed" })) say("swap_failed is final");
  for (const name of ["B", "C"]) if (!calls.some(call => call.startsWith("WatchEnded ") && call.includes("D:/w/" + name) && call.includes("true"))) say("not final: " + name);
  if (detailText("watch_other_version: 01.000.000 01.002.000") !== t("detail_watch_other_version")("01.000.000", "01.002.000")) say("the versions: " + detailText("watch_other_version: 01.000.000 01.002.000"));
  if (!t("detail_watch_other_version")("01.000.000", "01.002.000").includes(t("updateCard"))) say("it does not say how to update it");
  if (!watchFinal({ state: "skipped", detail: "watch_other_version: 01.000.000 01.002.000" })) say("another version comes again as it is");
  if (detailText("watch_redone; original_kept_watched") !== t("detail_watch_redone") + "; " + t("detail_original_kept_watched")) say("made again: " + detailText("watch_redone; original_kept_watched"));
  // the .p5z made of a game that ended well goes to the window with it
  watchEnded({ path: "D:/w/A", watched: true, state: "ok", out: "D:/d/A.p5z", detail: "watch_redone; original_kept_watched", game: game("A", "01.000.000") });
  if (!calls.some(call => call === 'WatchEnded ["D:/w/A",true,"D:/d/A.p5z","PPSA10001","01.000.000",true]')) say("what was made is not told: " + calls.filter(c => c.startsWith("WatchEnded")).join(" | "));
  // the same when they join a queue at work
  rows = [row("A", "01.000.000"), row("B", "01.002.000"), row("C", "01.000.000")];
  state.lists.compress = rows;
  state.busy = true;
  state.job = { section: "compress", mode: "p5z", rows: [], running: true, opts: { Mode: "p5z", Dest: "D:/d" }, early: {}, deletes: false };
  window.answers = { Plan: views, Append: 0, Space: [] };
  calls.length = 0;
  for (const r of rows) toJoin.push(r);
  await joinQueue();
  const append = calls.find(call => call.startsWith("Append "));
  if (!append || !append.includes('"Name":"A"') || append.includes('"Name":"B"') || append.includes('"Name":"C"') || !append.includes('],[],["D:/d/A.p5z"],false]')) say("joined: " + append);
  if (rows[1].detail !== "watch_other_version: 01.000.000 01.002.000" || rows[2].detail !== "watch_older_version: 01.002.000 01.000.000") say("joined, left out: " + rows[1].detail + " " + rows[2].detail);
  // sent to the console too, from the Console section: a console that has the older copy says so
  state.job = null;
  state.busy = false;
  state.settings.ConsoleIP = "192.168.1.20";
  state.settings.ConsoleCode = "482913";
  window.answers = { Plan: [], Space: [], Library: { folder: "D:/d", error: "", items: [{ path: "D:/d/A.p5z", name: "A", titleId: "PPSA10001", version: "01.000.000", size: 10, icon: "", interrupted: false, other: false, error: "" }] } };
  await sendWatched({ section: "compress", mode: "p5z", watched: true, rows: [{ path: "D:/w/A", watched: true, state: "ok", out: "D:/d/A.p5z", detail: "watch_redone; original_kept_watched" }] });
  const sentRow = state.lists.console.find(r => r.path === "D:/d/A.p5z");
  if (!sentRow || sentRow.section !== "console") say("the .p5z made is not a row of the Console section");
  else if (!sentRow.redone) say("the .p5z made again is not marked");
  state.job = { section: "console", mode: "send", rows: [sentRow], running: true, early: {} };
  onItem({ i: 0, state: "error", detail: "send_exists" });
  if (sentRow.detail !== "send_exists_older") say("the console's older copy: " + sentRow.detail);
  if (detailText("send_exists_older") !== t("detail_send_exists_older")) say("the text of the older copy");
  // a queue the user starts with watched rows in it: theirs are kept, so they are not listed as going away
  state.job = null;
  state.busy = false;
  state.deleteOriginals = true;
  let asked = null;
  askOriginals = async plans => { asked = plans.map(p => p.Item.Name); return "keep"; };
  const mine = Object.assign(row("Mine", "01.000.000"), { watched: false });
  rows = [row("Watched", "01.000.000"), mine];
  state.lists.compress = rows;
  window.answers = { Plan: [view("Watched", { Replace: [] }), view("Mine", { Replace: [] })], Space: [], Start: null };
  await startQueue("", rows);
  if (!asked || asked.join() !== "Mine") say("the originals asked about: " + asked);
  asked = null;
  rows = [row("Watched", "01.000.000")];
  state.lists.compress = rows;
  state.busy = false;
  window.answers = { Plan: [view("Watched", { Replace: [] })], Space: [], Start: null };
  await startQueue("", rows);
  if (asked) say("asked about the original of a watched game alone: " + asked);
`

// TestPageWatchRedo: see watchRedoChecks.
func TestPageWatchRedo(t *testing.T) { runPage(t, watchRedoChecks) }

// TestPageWatchWriting: the watched folder's status says which games wait for a program that keeps writing into
// them (Settings › Watched folder), and nothing once none does.
func TestPageWatchWriting(t *testing.T) {
	runPage(t, `
  if (typeof t("watchWriting")("Copper Vale") !== "string" || !t("watchWriting")("Copper Vale").includes("Copper Vale")) say("watchWriting");
  state.settings = Object.assign(state.settings, { WatchOn: true, WatchFolder: "D:/w" });
  state.watch = { folder: "D:/w", error: "", existing: 0, writing: "Copper Vale" };
  paintWatchStatus(state.settings);
  if ($("pref-watch-status").hidden || $("pref-watch-status").textContent !== t("watchWriting")("Copper Vale")) say("not said: " + $("pref-watch-status").textContent);
  state.watch.writing = "";
  paintWatchStatus(state.settings);
  if (!$("pref-watch-status").hidden) say("still said");
  state.watch.error = "gone";
  paintWatchStatus(state.settings);
  if ($("pref-watch-status").hidden || $("pref-watch-status").textContent !== t("watchGone")) say("gone");
`)
}
