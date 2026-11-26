// SPDX-License-Identifier: GPL-3.0-or-later

package main

import "testing"

// watchRedoChecks: what the watched folder does with a game that is in the destination already. The .p5z it
// made itself of a game that changed since is made again in its place, without asking (WatchRedo); the same game
// in another version is left out, with both versions and the way to update it; anything else is left out, as
// before. The same when the game joins a queue at work.
const watchRedoChecks = `
  render = () => {};
  paintRow = () => {};
  paintSummary = () => {};
  toast = () => {};
  const game = (name, version) => ({ Name: name, Kind: "folder", TitleID: "PPSA10001", Version: version, OriginalSize: 9000 });
  const row = (name, version) => ({ path: "D:/w/" + name, section: "compress", game: game(name, version), error: "", state: "", detail: "", warnings: [], watched: true });
  const view = (name, more) => Object.assign({ Item: { Name: name }, Out: "D:/d/" + name + ".p5z", Replace: ["D:/d/" + name + ".p5z"], Warnings: [], Block: "" }, more);
  const views = [view("A", { WatchRedo: true }), view("B", { OtherVersion: "01.000.000" }), view("C", {})];
  state.settings.LastDest = "D:/d";
  // a queue started by the watched folder
  let rows = [row("A", "01.000.000"), row("B", "01.002.000"), row("C", "01.000.000")];
  state.lists.compress = rows;
  window.answers = { Plan: views, Space: [], Start: null };
  await startQueue("", rows, undefined, true);
  const start = calls.find(call => call.startsWith("Start "));
  if (!start || !start.includes('"Replace":["D:/d/A.p5z"]')) say("the .p5z it made is not replaced: " + start);
  if (rows[1].state !== "skipped" || rows[1].detail !== "watch_other_version: 01.000.000 01.002.000") say("another version: " + rows[1].state + " " + rows[1].detail);
  if (rows[2].state !== "skipped" || rows[2].detail !== "watch_exists") say("in the way: " + rows[2].state + " " + rows[2].detail);
  for (const name of ["B", "C"]) if (!calls.some(call => call.startsWith("WatchEnded ") && call.includes("D:/w/" + name) && call.includes("true"))) say("not final: " + name);
  if (detailText("watch_other_version: 01.000.000 01.002.000") !== t("detail_watch_other_version")("01.000.000", "01.002.000")) say("the versions: " + detailText("watch_other_version: 01.000.000 01.002.000"));
  if (!t("detail_watch_other_version")("01.000.000", "01.002.000").includes(t("updateCard"))) say("it does not say how to update it");
  if (!watchFinal({ state: "skipped", detail: "watch_other_version: 01.000.000 01.002.000" })) say("another version comes again as it is");
  if (detailText("watch_redone; original_kept_watched") !== t("detail_watch_redone") + "; " + t("detail_original_kept_watched")) say("made again: " + detailText("watch_redone; original_kept_watched"));
  // the .p5z made of a game that ended well goes to the window with it
  watchEnded({ path: "D:/w/A", watched: true, state: "ok", out: "D:/d/A.p5z", detail: "" });
  if (!calls.some(call => call === 'WatchEnded ["D:/w/A",true,"D:/d/A.p5z"]')) say("what was made is not told: " + calls.filter(c => c.startsWith("WatchEnded")).join(" | "));
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
  if (!append || !append.includes('"Name":"A"') || append.includes('"Name":"B"') || append.includes('"Name":"C"') || !append.includes('["D:/d/A.p5z"]')) say("joined: " + append);
  if (rows[1].detail !== "watch_other_version: 01.000.000 01.002.000" || rows[2].detail !== "watch_exists") say("joined, left out: " + rows[1].detail + " " + rows[2].detail);
  // sent to the console too: a console that has the older copy says so
  state.job = null;
  state.busy = false;
  const sentRow = { path: "D:/d/A.p5z", section: "details", game: { Name: "A", Kind: "p5z", P5zSize: 10 }, error: "", state: "", detail: "", warnings: [] };
  state.lists.details = [sentRow];
  addPaths = () => {};
  state.settings.ConsoleIP = "192.168.1.20";
  state.settings.ConsoleCode = "482913";
  window.answers = { Plan: [], Space: [] };
  await sendWatched({ section: "compress", mode: "p5z", watched: true, rows: [{ path: "D:/w/A", watched: true, state: "ok", out: "D:/d/A.p5z", detail: "watch_redone; original_kept_watched" }] });
  if (!sentRow.redone) say("the .p5z made again is not marked");
  state.job = { section: "details", mode: "send", rows: [sentRow], running: true, early: {} };
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
