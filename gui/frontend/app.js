// SPDX-License-Identifier: GPL-3.0-or-later
"use strict";

// The page of MaxPack, as the one of NXPress: the sections on the left, the list of games, the
// options of the section on the right and the progress of the queue at the bottom. The data of
// the games goes into the page only as text (textContent and attributes), in elements cloned
// from the fixed templates of index.html.

// ---- MaxPack: every call of the page to the window goes through here -----------------------

// go is the window as Wails gives it to the page: the methods of App (window.go.main.App) and
// the events of window.runtime. Without the window (the page opened in a browser, or with a
// stand-in for it) the calls fail, the events never come, and the page says so instead of
// breaking.
const go = {
  app: () => window.go && window.go.main && window.go.main.App,
  has() { return !!this.app(); },
  call(name, ...args) {
    const app = this.app();
    if (!app || typeof app[name] !== "function") return Promise.reject(new Error(t("noWindow")));
    try { return Promise.resolve(app[name](...args)); } catch (error) { return Promise.reject(error); }
  },
  on(event, handler) {
    if (window.runtime && typeof window.runtime.EventsOn === "function") window.runtime.EventsOn(event, handler);
  },
  // files dropped anywhere on the window (Wails gives their paths)
  onDrop(handler) {
    if (window.runtime && typeof window.runtime.OnFileDrop === "function") window.runtime.OnFileDrop(handler, false);
  },
};

// ---- what the page knows ---------------------------------------------------------------------

// the sections that run a queue, and the ones of these that write games
const queues = ["compress", "decompress", "verify"];
const writes = ["compress", "decompress"];
const ended = ["ok", "error", "skipped", "cancelled"];
const working = ["packing", "verifying", "unpacking", "protecting", "repairing"];
// the states of a game of a queue that did not start: there from the start, or added while it runs
const notStarted = ["waiting", "queued"];
// the shortcuts of the level, as in NXPress: the level each one sets, and the ones it covers
const presets = [[3, 1, 6], [12, 7, 14], [19, 15, 22]];
// what each section takes, as the empty list says it: [type, its text or none]
const takes = {
  compress: [["", "typeDump"], [".exfat", ""]],
  decompress: [[".p5z", ""]],
  verify: [[".p5z", ""], ["", "typePacked"]],
  details: [["", "typeDump"], [".exfat", ""], [".p5z", ""]],
  rename: [["", "typeDump"], [".exfat", ""], [".p5z", ""]],
};
const defaultTemplate = "{title} [{id}]";
const defaults = { Lang: "", Theme: "system", Runtime: "", LastDest: "", Mode: "p5z", Level: 19, DeleteOriginals: false, DeletePermanently: false, LowPriority: false, NameTemplate: defaultTemplate, Threads: 0,
  RepairData: false, RepairPercent: 5 };
// the shares of repair data the window offers (a PAR2 set next to each .p5z)
const percents = [1, 5, 10];

const $ = id => document.getElementById(id);
const windows = /win/i.test(navigator.platform || navigator.userAgent || "");

const state = {
  section: "compress",
  // the language of the window: the one chosen in Settings (settings.Lang), else the one of the
  // system, else English; see chooseLanguage
  lang: systemLanguage(navigator),
  // the settings as GetSettings gave them, with the changes made here: SetSettings takes them whole
  settings: Object.assign({}, defaults),
  setup: null,
  unpackTo: load("unpackTo", "original") === "console" ? "console" : "original",
  lists: { compress: [], decompress: [], verify: [], details: [], rename: [] },
  // whether this run of Compress deletes the originals once checked: what Settings says, until
  // it is changed next to Compress
  deleteOriginals: false,
  // the queue running, or the last one: its section, mode and games in the order of the queue
  job: null,
  busy: false,
  cancelling: false,
  // the runtime of ampr-emu-ng is asked once: after "Go on without it" (runtimeWithout) the window does not insist
  askedRuntime: false,
  // Compare the levels or Measure this PC, at work or the last one: see compareLevels and measurePC
  tool: null,
  // MaxPack in the Explorer of Windows, as ExplorerPlaces tells it: each place "" (not there),
  // "here" (for this copy) or "moved" (for another copy), and whether .p5z opens with another program
  explorer: { available: false, menu: "", sendTo: "", files: "", filesOther: false },
  // a place of the Explorer is being added or removed: its switches wait
  explorerBusy: false,
};

// whether a queue is running: the window took it, and has not said its end yet
const running = () => !!(state.busy && state.job && state.job.running && !state.job.summary);

// the ampr-emu-ng that comes with MaxPack, as Setup tells it: its path, "" when the window could not
// put it in its folder; and the runtime a game that lacks one is given: the one chosen in Settings,
// else that one (the window puts it in, see plans in page.go)
const builtInRuntime = () => (state.setup && state.setup.builtInRuntime) || "";
const runtimeInUse = () => state.settings.Runtime || builtInRuntime();
// maxpack-par2, which makes and uses the repair data, as Setup tells it: without it (a build of the
// window alone) the page offers no repair data, and says why
const hasPar2 = () => !!(state.setup && state.setup.par2);
// the share of repair data Compress gives each .p5z, 0 for none
const repairOf = () => (state.settings.RepairData && hasPar2() && percents.includes(Number(state.settings.RepairPercent)) ? Number(state.settings.RepairPercent) : 0);

// a text of the language chosen, else the English one
const t = key => (texts[state.lang] && texts[state.lang][key] !== undefined ? texts[state.lang] : texts.en)[key];

function load(name, fallback) {
  try { const value = localStorage.getItem("maxpack." + name); return value === null ? fallback : JSON.parse(value); } catch { return fallback; }
}
function save(name, value) {
  try { localStorage.setItem("maxpack." + name, JSON.stringify(value)); } catch { }
}

// a number with its unit inside text written from right to left keeps its own order (300.2 GB, not
// GB 300.2): the sizes and times are isolated as left to right there (a Go notice does the same)
const isolate = text => languageInfo(state.lang)[3] ? "\u2066" + text + "\u2069" : text;
// the languages that write the decimal mark as a comma (8,4 GB in Spanish); the notice at the end
// of a queue writes sizes the same way (sizeText in notify.go)
const decimalComma = ["ru", "de", "es", "fr", "pt", "it"];
// a size as NXPress writes it: steps of 1024, no decimals for bytes and one from KB on, rounded
// half away from zero, no thousands separator
function size(bytes) {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = Number(bytes) || 0, unit = 0;
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit++; }
  let text = unit ? (Math.round(value * 10) / 10).toFixed(1) : String(Math.round(value));
  if (decimalComma.includes(state.lang)) text = text.replace(".", ",");
  return isolate(text + " " + units[unit]);
}
function time(seconds) {
  if (seconds < 60) return isolate(seconds + " s");
  const m = Math.floor(seconds / 60), s = seconds % 60;
  return isolate(m < 60 ? `${m} min ${s} s` : `${Math.floor(m / 60)} h ${m % 60} min`);
}
// the language of the window: the one chosen if it is one the window has, else the one of the system
const chooseLanguage = chosen => isLanguage(chosen) ? chosen : systemLanguage(navigator);
const message = error => String((error && error.message) || error || "");
const baseName = path => String(path).split(/[\\/]/).filter(Boolean).pop() || String(path);
// a path in a field cut at its start (direction: rtl) keeps its order with a mark of left to
// right at both ends, as in NXPress: else "E:\" shows as "\:E"
const ltr = path => "\u200E" + path + "\u200E";
// paths are the same on Windows whatever their case
const pathKey = path => (windows ? String(path).toLowerCase() : String(path));

// ---- the rows of the list ----------------------------------------------------------------------

function newRow(path, section) {
  const tr = $("row-template").content.firstElementChild.cloneNode(true);
  const row = { path, section, game: null, error: "", state: "", detail: "", step: "", phase: "", offset: 0, done: 0, total: 0, out: "", outSize: 0, homebrew: false, warnings: [], renameTo: "", renameBlock: "",
    repair: null, repairSize: 0, tr };
  tr.querySelector(".remove button").addEventListener("click", () => removeRow(row));
  tr.querySelector(".report").addEventListener("click", () => copyReport([row], row.section));
  tr.querySelector(".compare").addEventListener("click", () => compareLevels(row));
  tr.querySelector(".to-verify").addEventListener("click", () => takeTo(row, "verify"));
  tr.querySelector(".to-decompress").addEventListener("click", () => takeTo(row, "decompress"));
  tr.querySelector(".repair").addEventListener("click", () => startQueue("repair", [row]));
  // the entrance of a row and the pop of what it saved play once: a row put back in the list
  // (every render puts them all back) does not play them again
  tr.addEventListener("animationend", event => {
    if (event.animationName === "row-in") tr.classList.remove("enter");
    if (event.animationName === "pop") event.target.classList.remove("pop");
  });
  return row;
}

// the engine is asked of a few games at a time: each is two short runs of maxpack-core
const describeAtOnce = 3;
const toDescribe = [];
let describing = 0;
function describe(row) {
  toDescribe.push(row);
  nextDescribe();
}
function nextDescribe() {
  while (describing < describeAtOnce && toDescribe.length) {
    const row = toDescribe.shift();
    if (!state.lists[row.section].includes(row)) continue;
    describing++;
    go.call(row.section === "details" ? "Details" : "Describe", row.path)
      .then(game => { row.game = game; if (row.section === "details") row.repair = game.Repair || null; }, error => { row.error = message(error); })
      .then(() => (row.section === "verify" && row.game && row.game.Kind === "p5z" ? readRepair(row) : null))
      .finally(() => {
        describing--;
        paintRow(row);
        paintSummary();
        paintStart();
        // a game added to the section whose queue runs joins it (not a repair, nor repair data being
        // added: a game is not given those unasked); one of Rename shows its new name
        if (row.game && running() && state.job.section === row.section && !state.job.rows.includes(row) && !["repair", "protect"].includes(state.job.mode)) join(row);
        if (row.section === "rename") planRenames();
        nextDescribe();
      });
  }
}

// readRepair asks the window what repair data a .p5z of the list has next to it
async function readRepair(row) {
  row.repair = await go.call("RepairInfo", row.path).catch(() => null);
  paintRow(row);
  if (state.section === row.section) paintOptions();
}

// addPaths adds games to the list of a section, each once
function addPaths(section, paths) {
  const list = state.lists[section];
  const have = new Set(list.map(row => pathKey(row.path)));
  let added = 0;
  for (const path of paths || []) {
    if (typeof path !== "string" || have.has(pathKey(path))) continue;
    have.add(pathKey(path));
    const row = newRow(path, section);
    row.tr.classList.add("enter");
    list.push(row);
    paintRow(row);
    describe(row);
    added++;
  }
  if (added) render();
  return added;
}

async function expandAndAdd(paths, section) {
  if (!paths || !paths.length) return 0;
  const found = await go.call("Expand", paths, section).catch(error => { toast(message(error)); return []; });
  return addPaths(section, found);
}

// the section open shows what came; when it took nothing and another did, that one opens; the one
// the Explorer asked for (its menu, a double click on a .p5z) opens whatever was open, with its game
function showAdded(took, asked, paths) {
  const count = Object.values(took).reduce((sum, n) => sum + n, 0);
  if (asked && state.lists[asked]) {
    if (state.section !== asked) setSection(asked);
    const wanted = new Set((paths || []).map(pathKey));
    const row = state.lists[asked].find(row => wanted.has(pathKey(row.path)));
    if (row && row.tr.scrollIntoView) row.tr.scrollIntoView({ block: "nearest" });
    return;
  }
  if (took[state.section]) return;
  const section = ["compress", "decompress", "verify", "details"].find(name => took[name]);
  if (section) setSection(section);
  if (count) toast(t("added")(count));
}

// the spaces the files given to MaxPack go to (see routing.go)
const given = ["compress", "verify", "details"];

// files that reach the window: given to MaxPack (dropped on it, sent to it, from the menu of the
// Explorer or a .p5z opened with a double click), or to another MaxPack while this one was open
async function addSent(files) {
  if (!files) return;
  const took = {};
  for (const section of given) took[section] = await expandAndAdd(files[section], section);
  showAdded(took, files.show, files.show ? files[files.show] : null);
}

// the Verify and Decompress of the card of a .p5z in Details: the game goes to that section, which
// opens with it
function takeTo(row, section) {
  addPaths(section, [row.path]);
  setSection(section);
  const there = state.lists[section].find(other => pathKey(other.path) === pathKey(row.path));
  if (there && there.tr.scrollIntoView) there.tr.scrollIntoView({ block: "nearest" });
}

// files dropped on the window: to the section open in Verify and Details; else to
// Compress or Decompress by what they are (the files given to MaxPack from outside the window go
// to Compress or Details instead, see addSent)
async function dropped(paths) {
  showDrop(false);
  if (state.section === "verify" || state.section === "details" || state.section === "rename") {
    if (!await expandAndAdd(paths, state.section)) toast(t("dropNothing"));
    return;
  }
  const compress = await expandAndAdd(paths, "compress");
  const decompress = await expandAndAdd(paths, "decompress");
  if (!compress && !decompress) toast(t("dropNothing"));
  else showAdded({ compress, decompress });
}

function removeRow(row) {
  const job = state.job;
  if (running() && job.rows.includes(row)) {
    if (!notStarted.includes(row.state)) return;
    // not started yet: out of the queue, then out of the list
    go.call("Unqueue", job.section, job.rows.indexOf(row)).then(out => {
      if (!out) return;
      row.unqueued = true;
      dropRow(row);
    }, error => toast(message(error)));
    return;
  }
  dropRow(row);
}
function dropRow(row) {
  const list = state.lists[row.section];
  const at = list.indexOf(row);
  if (at >= 0) list.splice(at, 1);
  render();
}

function clearList() {
  if (state.busy) return;
  state.lists[state.section] = [];
  if (state.job && state.job.section === state.section && !running()) state.job = null;
  render();
}

// ---- how far a game is -------------------------------------------------------------------------

// the steps of a game in a queue, each a part of its bar (stepsOf in progress.go): what is made is
// verified before it takes its name, but the original dump, which the engine checks as it writes
// it; a .p5z with repair data gets it once in its place; a .p5z is verified before it gets repair
// data, and after it is repaired
function stepsOf(job) {
  const mode = job.mode;
  if (mode === "p5z") return job.repair ? ["packing", "verifying", "protecting"] : ["packing", "verifying"];
  if (mode === "folder" || mode === "console") return [mode === "console" ? "unpacking" : "packing", "verifying"];
  if (mode === "verify") return ["verifying"];
  if (mode === "protect") return ["verifying", "protecting"];
  if (mode === "repair") return ["repairing", "verifying"];
  return ["unpacking"];
}
// how far the step at work is, from 0 to 1: the phases of a step follow one another (copying the
// loose files, then packing the rest) and their bytes add up to the size of the game; the other
// steps go through the bytes of the file they read
function stepShare(row) {
  if (!row.total) return 0;
  const bySize = (row.step === "packing" || row.step === "unpacking") && row.game;
  const whole = Math.max(row.offset + row.total, bySize ? row.game.OriginalSize || 0 : 0);
  return Math.min(1, (row.offset + row.done) / whole);
}
function rowShare(row, job) {
  if (ended.includes(row.state)) return 1;
  const steps = stepsOf(job), index = steps.indexOf(row.step);
  return index < 0 ? 0 : (index + stepShare(row)) / steps.length;
}
// the games of a queue, without the ones taken out of it before their turn
const inQueue = job => job.rows.filter(row => row && !row.unqueued);
// how far the queue is (never going back), and the seconds left at the speed of the engine
function progressOf(job) {
  let whole = 0, done = 0;
  for (const row of inQueue(job)) {
    const weight = stepsOf(job).length * Math.max(1, row.game ? row.game.OriginalSize : 1);
    whole += weight;
    done += rowShare(row, job) * weight;
  }
  job.shown = Math.max(job.shown || 0, whole ? done / whole : 0);
  return { share: job.shown, left: job.rate > 0 ? (whole - done) / job.rate : 0 };
}

// ---- what a game says ----------------------------------------------------------------------------

function kindName(game) {
  if (game.Kind === "p5z") return ".p5z";
  if (game.Kind === "exfat") return t("kindImage");
  return game.Packed ? t("kindPacked") : t("kindDump");
}

// the warnings of a game in its section: the ones of the list, and the ones of its plan
function rowWarnings(row) {
  const game = row.game, list = [];
  if (!game) return list;
  if (row.section === "compress" && game.Packed) list.push(t("detail_already_packed"));
  if (row.section === "verify" && game.Kind === "folder" && !game.Packed) list.push(t("detail_not_packed"));
  // only a folder needs ampr-emu-ng in its fakelib/: MaxPack for the console puts its own in when it
  // plays a .p5z. A folder that lacks one is given the one in use, when there is one
  const lacks = row.section === "compress" && modeOf("compress") === "folder" && !game.Packed && !game.HasZstdRuntime && !runtimeInUse();
  // made without a runtime that reads zstd: its note says it already
  const told = row.state === "ok" && /(^|; )runtime_without_zstd($|; )/.test(row.detail);
  if ((lacks || row.warnings.includes("needs_runtime")) && !told) list.push(t("warn_needs_runtime"));
  for (const warning of row.warnings) if (warning !== "needs_runtime") list.push(t("warn_" + warning));
  return list;
}

// detailText says a detail of the window in the language: codes ("exists"), codes with what
// they are about ("tmp_not_deleted: <path>: <why>"), several joined by "; ", and the message of
// the engine when it is not a code
function detailText(detail) {
  return String(detail || "").split("; ").filter(Boolean).map(partText).join("; ");
}
function partText(part) {
  let found = /^files_bad:(\d+)$/.exec(part);
  if (found) return t("detail_files_bad")(+found[1]);
  found = /^repair_not_possible: (\d+)$/.exec(part);
  if (found) return t("detail_repair_not_possible")(+found[1]);
  found = /^exit_(-?\d+)$/.exec(part);
  if (found) return t("detail_exit")(+found[1]);
  found = /^([a-z_]+)(?:: ([\s\S]*))?$/.exec(part);
  if (found && typeof texts.en["detail_" + found[1]] === "string") {
    const rest = found[2] || "";
    if (found[1] === "verify_failed") return t("detail_verify_failed") + (rest ? ": " + partText(rest) : "");
    return t("detail_" + found[1]) + (rest ? ": " + rest : "");
  }
  return t("detail_other") + ": " + part;
}

function fact(list, name, value) {
  const dt = document.createElement("dt"), dd = document.createElement("dd");
  dt.textContent = name;
  dd.textContent = value;
  list.append(dt, dd);
}

function paintFacts(row) {
  const facts = row.tr.querySelector(".facts"), game = row.game;
  facts.hidden = row.section !== "details" || !game;
  if (facts.hidden) return;
  facts.replaceChildren();
  fact(facts, t("factKind"), kindName(game));
  fact(facts, t("factId"), game.TitleID || "—");
  fact(facts, t("factVersion"), game.Version || "—");
  fact(facts, t("factFiles"), String(game.Files || 0));
  fact(facts, t("factOriginal"), size(game.OriginalSize));
  fact(facts, t("factInstalled"), size(game.InstallSize));
  if (game.P5zSize) fact(facts, ".p5z", size(game.P5zSize));
  // what it saves: the .p5z, or the compressed folder installed, against the original
  const made = game.P5zSize || (game.Packed ? game.InstallSize : 0);
  if (made && game.OriginalSize > made) fact(facts, t("factSaved"), t("smallerBy")(Math.round((1 - made / game.OriginalSize) * 100)) + " · " + size(game.OriginalSize - made));
  fact(facts, t("factPacked"), game.Packed ? t("yes") : t("no"));
  if (game.Packed) fact(facts, t("factPacks"), t("packsOf")(game.PackedFiles || 0, game.Volumes || 0));
  fact(facts, "ampr-emu-ng", game.HasZstdRuntime ? t("runtimeZstd") : game.HasRuntime ? t("runtimeOld") : t("runtimeNone"));
  // the repair data of a .p5z (its card, as a double click opens it): how much, or none
  if (game.Kind === "p5z") fact(facts, t("factRepair"), repairFactText(row.repair));
  const where = document.createElement("dd");
  where.className = "path-fact";
  where.textContent = game.Path;
  const label = document.createElement("dt");
  label.textContent = t("factPath");
  facts.append(label, where);
}

// what the repair data next to a .p5z is, for its card in Details
function repairFactText(repair) {
  if (!repair || !repair.state || repair.state === "none") return t("repairNone");
  if (repair.state === "match") return t("repairFact")(repair.percent, repair.blocks, size(repair.blockSize));
  return repair.state === "other" ? t("repairOtherShort") : t("repairUnreadableLine");
}
// the line of a .p5z of Verify with repair data: what it can repair; and the set of another file
function repairLineText(row) {
  const repair = row.repair;
  if (row.section !== "verify" || !repair) return "";
  if (repair.state === "match") return t("repairLine")(repair.percent, repair.blocks, size(repair.blockSize), size(repair.capacity));
  if (repair.state === "other") return t("repairOtherLine");
  return repair.state === "unreadable" ? t("repairUnreadableLine") : "";
}
// a .p5z of Verify that the engine found damaged and that has repair data of its own: it can be
// repaired (not again once its repair data was found to be too little)
const canRepair = row => row.section === "verify" && !!row.game && row.game.Kind === "p5z" && row.state === "error" && !!row.repair && row.repair.state === "match"
  && !/^repair_not_possible/.test(row.detail);

function paintRow(row) {
  const tr = row.tr, game = row.game;
  const icon = tr.querySelector("img.icon"), cover = tr.querySelector(".cover");
  icon.hidden = !(game && game.Icon);
  if (!icon.hidden) icon.src = game.Icon;
  cover.hidden = !icon.hidden;
  cover.classList.toggle("loading", !game && !row.error);
  tr.title = row.path;
  const title = tr.querySelector(".title");
  title.textContent = game ? game.Name : baseName(row.path);
  // its ID, version and what it is; until read, that it is being read, or where it is
  const file = tr.querySelector(".file");
  if (game) file.textContent = [game.TitleID, game.Version && t("versionOf")(game.Version), kindName(game)].filter(Boolean).join(" · ");
  else file.textContent = row.error ? row.path : t("reading");
  tr.querySelector(".size").textContent = game ? size(game.OriginalSize) : "";

  // once made: the size before and after (compressed), and where it is
  const sizes = tr.querySelector(".sizes"), where = tr.querySelector(".where");
  sizes.hidden = !(row.section === "compress" && game && row.outSize && row.state === "ok");
  if (!sizes.hidden) sizes.textContent = size(game.OriginalSize) + " → " + size(row.outSize) + (row.repairSize ? " · " + t("withRepair")(size(row.repairSize)) : "");
  const repairLine = tr.querySelector(".repair-line"), repairText = repairLineText(row);
  repairLine.hidden = !repairText;
  repairLine.textContent = repairText;
  repairLine.classList.toggle("other", !!row.repair && row.repair.state !== "match");
  const repairButton = tr.querySelector(".repair");
  repairButton.hidden = !canRepair(row) || !hasPar2();
  repairButton.textContent = t("repairButton");
  repairButton.disabled = state.busy;
  where.hidden = !row.out;
  if (row.out) where.textContent = t("madeAt")(row.out);
  if (row.section === "rename") {
    const to = row.renameTo && !row.renameBlock && row.state !== "ok" ? baseName(row.renameTo) : "";
    const now = row.state === "ok" ? baseName(row.path) : "";
    where.hidden = !to && !now;
    where.textContent = to ? t("renameTo")(to) : now ? t("renamedAs")(now) : "";
  }
  const report = tr.querySelector(".report");
  report.hidden = row.section !== "details" || !game;
  report.textContent = t("copyReport");
  // the card of a .p5z in Details (as a double click on one opens it): Verify and Decompress
  const card = tr.querySelector(".card-actions");
  card.hidden = !(row.section === "details" && game && game.Kind === "p5z");
  card.querySelector(".to-verify").textContent = t("verify");
  card.querySelector(".to-decompress").textContent = t("decompress");
  const compare = tr.querySelector(".compare");
  compare.hidden = !canCompare(row) || (running() && state.job.rows.includes(row));
  compare.textContent = t("compareRow");
  compare.disabled = state.busy;

  const warnings = rowWarnings(row), warn = tr.querySelector(".list-warn");
  warn.hidden = !warnings.length;
  warn.textContent = warnings.join(" · ");

  const note = tr.querySelector(".note");
  const said = row.error || (row.detail && row.detail !== "cancelled" ? detailText(row.detail) : row.section === "rename" && row.renameBlock && row.state !== "ok" ? detailText(row.renameBlock) : "");
  note.hidden = !said;
  note.textContent = said;
  // a repair that ended well says so as good news, not as a warning
  const good = row.state === "ok" && /^(repaired|repair_not_needed)$/.test(row.detail);
  note.className = "note " + (row.error || row.state === "error" ? "bad" : good ? "good" : "warn");
  paintFacts(row);
  paintState(row);
}

function paintState(row) {
  const tr = row.tr, job = state.job;
  const badge = tr.querySelector(".badge");
  badge.hidden = !row.state;
  badge.className = "badge " + (ended.includes(row.state) ? row.state : working.includes(row.state) ? "work" : "waiting");
  if (row.section === "rename") badge.hidden = !ended.includes(row.state);
  badge.textContent = row.state ? t("state_" + row.state) : "";
  // the bar: a part for each step, the ones done full, the one at work filling up
  const segs = tr.querySelector(".segs"), step = tr.querySelector(".step");
  const active = running() && job.rows.includes(row) && working.includes(row.state);
  segs.hidden = !active;
  step.textContent = "";
  if (active) {
    const steps = stepsOf(job), index = steps.indexOf(row.step), share = stepShare(row);
    if (segs.children.length !== steps.length) segs.replaceChildren(...steps.map(() => $("seg-template").content.firstElementChild.cloneNode(true)));
    [...segs.children].forEach((seg, n) => {
      seg.className = "seg " + (steps[n] === "verifying" || steps[n] === "protecting" ? "tone-check" : "tone-work");
      seg.firstElementChild.style.transform = "scaleX(" + (n < index ? 1 : n === index ? share : 0) + ")";
    });
    const phase = row.phase ? t("phase_" + row.phase) : "";
    step.textContent = [phase, t("percent")(Math.floor(rowShare(row, job) * 100))].filter(Boolean).join(" · ");
  }
  // what it saved, once made
  const saved = tr.querySelector(".saved"), game = row.game;
  const smaller = row.section === "compress" && row.state === "ok" && game && row.outSize && row.outSize < game.OriginalSize;
  const savedText = smaller ? t("smallerBy")(Math.round((1 - row.outSize / game.OriginalSize) * 100)) : "";
  if (savedText && !saved.textContent) saved.classList.add("pop");
  saved.textContent = savedText;
  tr.querySelector(".remove button").disabled = running() && job.rows.includes(row) && !notStarted.includes(row.state);
}

// ---- the page ----------------------------------------------------------------------------------

function setSection(section) {
  state.section = section;
  render();
  if (section === "settings") previewNames(state.settings.NameTemplate || defaultTemplate);
}

function paintTexts() {
  // the document says its language and its direction: Arabic is written from right to left
  const [, , tag, rtl] = languageInfo(state.lang);
  document.documentElement.lang = tag || state.lang;
  document.documentElement.dir = rtl ? "rtl" : "ltr";
  document.querySelectorAll("[data-t]").forEach(element => { element.textContent = t(element.dataset.t); });
  document.querySelectorAll("[data-t-title]").forEach(element => { element.title = t(element.dataset.tTitle); });
  document.querySelectorAll("[data-t-label]").forEach(element => { element.setAttribute("aria-label", t(element.dataset.tLabel)); });
}

// the highlight of the open section slides to it: moved, never animated in size
function placeNavHighlight() {
  const on = document.querySelector("nav button.on"), highlight = $("nav-hl");
  if (!on || !on.offsetHeight) return;
  highlight.style.height = on.offsetHeight + "px";
  highlight.style.transform = "translateY(" + on.offsetTop + "px)";
  highlight.classList.add("on");
}

function paintNav() {
  for (const button of document.querySelectorAll("nav button[data-section]")) {
    const on = button.dataset.section === state.section;
    button.classList.toggle("on", on);
    if (on) button.setAttribute("aria-current", "page"); else button.removeAttribute("aria-current");
  }
  placeNavHighlight();
  const setup = state.setup, core = $("side-core");
  core.classList.toggle("bad", !setup || !!setup.coreError);
  core.textContent = !setup ? t("noWindowShort") : setup.coreError ? (setup.coreOtherVersion ? t("coreOtherVersionShort") : t("coreMissingShort")) : "maxpack-core " + (setup.coreVersion.split(" ")[1] || "");
  core.title = setup ? coreText(setup) || setup.coreVersion : "";
}

function paintHead() {
  const section = state.section;
  const button = document.querySelector(`nav button[data-section="${section}"]`);
  const tile = $("title-tile");
  tile.className = "tile " + [...button.classList].filter(name => name.startsWith("tone-")).join(" ");
  tile.replaceChildren(button.querySelector("svg").cloneNode(true));
  $("title").textContent = t(section);
  $("about").textContent = t("about_" + section);
  const tags = takes[section].map(([type, text]) => {
    const tag = document.createElement("i");
    tag.textContent = text ? t(text) : type;
    return tag;
  });
  $("takes").replaceChildren(...tags);
}

function paintList() {
  const list = state.lists[state.section] || [];
  $("empty").hidden = list.length > 0;
  $("files").hidden = list.length === 0;
  $("drop").classList.toggle("filled", list.length > 0);
  $("files").classList.toggle("no-state", !list.some(row => row.state));
  for (const row of list) paintRow(row);
  $("rows").replaceChildren(...list.map(row => row.tr));
}

function paintOptions() {
  const section = state.section, s = state.settings;
  document.querySelectorAll("#options [data-for]").forEach(block => { block.hidden = !block.dataset.for.split(" ").includes(section); });
  $("options").hidden = section === "details" || section === "settings";
  $("delete-originals").checked = state.deleteOriginals;
  $("delete-hint").textContent = state.deleteOriginals ? t(s.DeletePermanently ? "deleteForGood" : "deleteToBin") : "";
  $("rename-template").textContent = s.NameTemplate || defaultTemplate;
  const mode = s.Mode === "folder" ? "folder" : "p5z";
  for (const input of document.querySelectorAll('input[name="made-for"]')) input.checked = input.value === mode;
  for (const input of document.querySelectorAll('input[name="unpack-to"]')) input.checked = input.value === state.unpackTo;
  paintLevel(s.Level);
  $("dest").textContent = s.LastDest ? ltr(s.LastDest) : t("destNone");
  $("dest").classList.toggle("empty", !s.LastDest);
  $("dest").title = s.LastDest || "";
  $("dest-hint").textContent = section === "decompress"
    ? t(state.unpackTo === "console" ? "destFolderHint" : "destOriginalHint")
    : t(mode === "folder" ? "destFolderHint" : "destP5zHint");
  // the runtime in use: the one chosen, else the built-in one; with neither, only a folder is at a loss
  const runtime = $("runtime-state"), builtIn = !s.Runtime && builtInRuntime();
  runtime.classList.toggle("ok", !!runtimeInUse());
  runtime.classList.toggle("bad", !runtimeInUse() && mode === "folder");
  runtime.textContent = s.Runtime || (builtIn ? t("runtimeBuiltIn")(state.setup.builtInRuntimeVersion) : t("runtimeUnset"));
  runtime.title = runtimeInUse();
  $("choose-runtime").textContent = s.Runtime ? t("runtimeChange") : builtIn ? t("runtimeChooseOther") : t("runtimeChoose");
  for (const element of document.querySelectorAll("#options input, #options button")) element.disabled = state.busy;
  paintRepairOptions();
}

// the buttons of the shares of repair data in a group, the one of Settings on, each choosing it
function paintPercents(group, enabled) {
  const chosen = Number(state.settings.RepairPercent) || 5;
  if (group.children.length !== percents.length) {
    group.replaceChildren(...percents.map(percent => {
      const button = document.createElement("button");
      button.setAttribute("role", "radio");
      button.dataset.percent = String(percent);
      button.addEventListener("click", () => { keep({ RepairPercent: percent }); render(); });
      return button;
    }));
  }
  for (const button of group.children) {
    const on = Number(button.dataset.percent) === chosen;
    button.textContent = t("percent")(Number(button.dataset.percent));
    button.classList.toggle("on", on);
    button.setAttribute("aria-checked", on);
    button.disabled = !enabled;
  }
}

// why a section offers no repair data: a folder, Decompress, or no maxpack-par2; "" when it does
function repairWhyNot(section) {
  if (!hasPar2()) return t("repairNoPar2");
  if (section === "decompress") return t("repairNotHere");
  if (section === "compress" && modeOf("compress") === "folder") return t("repairOnlyP5z");
  return "";
}

// the repair data in Compress (the box and its share, off until it is ticked, as Settings keeps
// it) and in Verify (add it to the .p5z of the list, or repair the damaged ones with theirs)
function paintRepairOptions() {
  const s = state.settings, why = repairWhyNot(state.section), offered = !why && !state.busy;
  $("repair-data").checked = !!s.RepairData && !why;
  $("repair-data").disabled = !offered;
  paintPercents($("repair-percent"), offered && !!s.RepairData);
  $("repair-hint").textContent = why || t("repairDataHint");
  const list = state.lists.verify, verifyWhy = hasPar2() ? "" : t("repairNoPar2");
  paintPercents($("verify-percent"), !verifyWhy && !state.busy);
  $("add-repair").disabled = !!verifyWhy || state.busy || !list.some(row => row.game && row.game.Kind === "p5z");
  $("repair-damaged").disabled = !!verifyWhy || state.busy || !list.some(canRepair);
  $("verify-repair-why").hidden = !verifyWhy;
  $("verify-repair-why").textContent = verifyWhy;
}

function paintLevel(level) {
  level = Math.min(22, Math.max(1, Number(level) || 19));
  $("level").value = level;
  $("level-value").textContent = level;
  for (const button of document.querySelectorAll("button[data-level]")) {
    const [, low, high] = presets.find(([at]) => String(at) === button.dataset.level);
    button.classList.toggle("on", level >= low && level <= high);
  }
  $("level-hint").textContent = t("levelHint")(level);
}

function paintSettings() {
  const s = state.settings, setup = state.setup;
  // "" is "System": the language is the one of the system until one is chosen
  for (const button of document.querySelectorAll("button[data-lang]")) {
    const on = button.dataset.lang === (s.Lang || "");
    button.classList.toggle("on", on);
    button.setAttribute("aria-checked", on);
  }
  for (const button of document.querySelectorAll("button[data-theme]")) {
    button.classList.toggle("on", button.dataset.theme === s.Theme);
    button.setAttribute("aria-checked", button.dataset.theme === s.Theme);
  }
  // the built-in ampr-emu-ng, or the one chosen instead of it, which can be dropped to go back to it
  const builtIn = builtInRuntime();
  $("pref-runtime").textContent = s.Runtime || (builtIn ? t("runtimeBuiltIn")(state.setup.builtInRuntimeVersion) : t("runtimeUnset"));
  $("pref-runtime").classList.toggle("bad", !runtimeInUse());
  $("pref-runtime").title = runtimeInUse();
  $("pref-runtime-choose").textContent = runtimeInUse() ? t("runtimeChooseOther") : t("choose");
  $("pref-runtime-clear").textContent = builtIn ? t("runtimeUseBuiltIn") : t("forget");
  $("pref-runtime-clear").hidden = !s.Runtime;
  $("pref-low").checked = !!s.LowPriority;
  // the threads Compress gives the engine: all of them, or the ones a measure found
  const cpu = cpuThreads();
  $("pref-threads").textContent = s.Threads > 0 ? t("threadsSome")(s.Threads, cpu) : t("threadsAll")(cpu);
  $("pref-threads-all").hidden = !(s.Threads > 0);
  $("pref-threads-all").disabled = state.busy;
  $("pref-measure").disabled = state.busy || !go.has() || !!(setup && setup.coreError);
  $("pref-delete").checked = !!s.DeleteOriginals;
  $("pref-forgood").checked = !!s.DeletePermanently;
  $("pref-forgood").disabled = !s.DeleteOriginals;
  $("pref-forgood-row").classList.toggle("off", !s.DeleteOriginals);
  $("pref-repair").checked = !!s.RepairData && hasPar2();
  $("pref-repair").disabled = !hasPar2();
  paintPercents($("pref-repair-percent"), hasPar2());
  $("pref-repair-why").textContent = hasPar2() ? "" : t("repairNoPar2");
  $("pref-log").textContent = setup && setup.logFile ? t("logAt")(setup.logFile) : "";
  const templates = (setup && setup.templates) || [defaultTemplate];
  if ($("templates").children.length !== templates.length) {
    $("templates").replaceChildren(...templates.map(template => {
      const button = document.createElement("button");
      button.setAttribute("role", "radio");
      button.dataset.template = template;
      button.textContent = template;
      button.addEventListener("click", () => setTemplate(template));
      return button;
    }));
  }
  const template = s.NameTemplate || defaultTemplate;
  for (const button of $("templates").children) {
    button.classList.toggle("on", button.dataset.template === template);
    button.setAttribute("aria-checked", button.dataset.template === template);
  }
  if (document.activeElement !== $("template")) $("template").value = template;
  $("about-version").textContent = setup ? setup.version : "—";
  $("about-core").textContent = !setup ? "—" : coreText(setup) || setup.coreVersion;
  $("about-core").classList.toggle("bad", !!(setup && setup.coreError));
  const tile = $("settings-tile");
  if (!tile.firstChild) tile.append(document.querySelector('nav button[data-section="settings"] svg').cloneNode(true));
  paintExplorer();
}

// ---- MaxPack in the Explorer of Windows (Settings › Windows Explorer) -------------------------------

// the switches of the places, the names the window gives them, and where ExplorerPlaces tells each
const explorerPlaces = [["menu", "menu", "menu"], ["sendto", "sendto", "sendTo"], ["files", "files", "files"]];

// each switch is on while its place is there for this copy of MaxPack; one there for another copy
// (or for this one before it was moved) says so, and turning it on writes it for this one. The
// portable version writes nothing in Windows: its switches stay off, and it says why
function paintExplorer() {
  const setup = state.setup, places = state.explorer, portable = !!(setup && setup.portable);
  $("pref-explorer").hidden = !(setup && setup.explorer);
  $("explorer-portable").hidden = !portable;
  $("explorer-for").hidden = portable;
  for (const [id, , field] of explorerPlaces) {
    const where = places[field] || "";
    $("pref-" + id).checked = where === "here";
    $("pref-" + id).disabled = portable || !go.has() || !!state.explorerBusy;
    $(id + "-status").textContent = where === "moved" ? t("explorerMoved") : id === "files" && where === "here" && places.filesOther ? t("explorerOther") : "";
  }
}
// the entries of the menu of the Explorer, in the language of the page
const menuTexts = () => ({ compress: t("menuCompress"), verify: t("menuVerify"), details: t("menuDetails") });
async function setExplorer(place, on) {
  state.explorerBusy = true;
  paintExplorer();
  try {
    state.explorer = await go.call("SetExplorerPlace", place, on, menuTexts());
  } catch (error) {
    toast(message(error));
    state.explorer = await go.call("ExplorerPlaces").catch(() => state.explorer);
  } finally {
    state.explorerBusy = false;
  }
  render();
}
// the menu speaks the language of the page: the installer wrote it in its own, and the language
// may change
function relabelMenu() {
  if (state.explorer.menu === "here") go.call("RelabelExplorerMenu", menuTexts()).catch(() => { });
}
async function loadExplorer() {
  if (!state.setup || !state.setup.explorer) return;
  state.explorer = await go.call("ExplorerPlaces").catch(() => state.explorer);
  relabelMenu();
  render();
}

function paintStart() {
  const section = state.section, list = state.lists[section] || [];
  const start = $("start"), acts = queues.includes(section) || section === "rename";
  start.hidden = !acts || (running() && section !== "rename");
  start.textContent = acts ? t(section) : "";
  start.disabled = state.busy || !go.has() || !!(state.setup && state.setup.coreError) || !list.some(row => row.game)
    || (section === "rename" && (running() || !list.some(row => row.renameTo && !row.renameBlock && row.state !== "ok")));
  $("cancel").hidden = !running();
  $("cancel").disabled = state.cancelling;
  for (const id of ["add-files", "add-folder", "empty-files", "empty-folder"]) $(id).disabled = !go.has();
  $("clear").disabled = state.busy || list.length === 0;
}

function paintSummary() {
  const job = state.job, bar = $("total-bar");
  if (running()) {
    const { share, left } = progressOf(job);
    const rows = inQueue(job), finished = rows.filter(row => ended.includes(row.state)).length;
    $("summary-text").textContent = t("working")(finished, rows.length, Math.floor(share * 100));
    bar.hidden = false;
    bar.firstElementChild.style.transform = "scaleX(" + share + ")";
    $("status").textContent = state.cancelling ? t("cancelling")
      : [job.rate > 0 ? t("speed")(size(job.rate)) : "", left >= 1 ? t("timeLeft")(time(Math.round(left))) : ""].filter(Boolean).join(" · ");
    return;
  }
  const list = state.lists[state.section] || [];
  const bytes = list.reduce((sum, row) => sum + (row.game ? row.game.OriginalSize : 0), 0);
  $("summary-text").textContent = list.length ? t("listed")(list.length, bytes) : t("nothing");
  bar.hidden = true;
  // how the last queue of this section ended, in the language of now
  const last = job && job.summary && job.section === state.section;
  $("status").textContent = last ? summaryLine(job.summary) : "";
}

function render() {
  const section = state.section, settings = section === "settings";
  paintTexts();
  paintNav();
  $("work").hidden = settings;
  $("settings").hidden = !settings;
  document.body.classList.toggle("busy", state.busy);
  if (!settings) {
    paintHead();
    paintList();
    $("toolbar").hidden = !(state.lists[section] || []).length;
  }
  paintOptions();
  paintSettings();
  paintStart();
  paintSummary();
}

// what is wrong with maxpack-core, in the language; "" when nothing is
function coreText(setup) {
  if (!setup || !setup.coreError) return "";
  return setup.coreOtherVersion ? t("coreOtherVersion") + " (" + setup.coreVersion + ")" : setup.coreError;
}

function banner(text) {
  $("banner").hidden = !text;
  $("banner-text").textContent = text || "";
}

let toastTimer = 0;
function toast(text) {
  if (!text) return;
  clearTimeout(toastTimer);
  $("toast").textContent = text;
  $("toast").hidden = false;
  toastTimer = setTimeout(() => { $("toast").hidden = true; }, 4000);
}

// the settings are kept whole: SetSettings writes every field
function keep(change) {
  state.settings = Object.assign({}, state.settings, change);
  go.call("SetSettings", state.settings).catch(error => toast(message(error)));
}

function applyTheme() {
  const theme = state.settings.Theme;
  if (theme === "light" || theme === "dark") document.documentElement.dataset.theme = theme;
  else document.documentElement.removeAttribute("data-theme");
}

// the template of the names: kept when it is one, and what it gives shown with a game of the lists
function setTemplate(template) {
  keep({ NameTemplate: template });
  $("template").value = template;
  render();
  previewNames(template);
  planRenames();
}
// the game of the preview: the first read of the section open, else of any list, else an invented one
function previewGame() {
  const lists = [state.lists[state.section] || [], ...Object.values(state.lists)];
  for (const list of lists) for (const row of list) if (row.game) return itemOf(row);
  return null;
}
let previewAsked = 0;
async function previewNames(template) {
  const asked = ++previewAsked;
  const preview = await go.call("NamePreview", template, previewGame()).catch(() => null);
  if (!preview || asked !== previewAsked) return;
  $("preview-p5z").textContent = preview.P5z;
  $("preview-original").textContent = preview.Original;
  $("preview-folder").textContent = preview.Folder;
  $("template-bad").hidden = !preview.Error;
  return preview;
}

async function chooseDest() {
  const dest = await go.call("ChooseDest").catch(error => { toast(message(error)); return ""; });
  if (dest) { keep({ LastDest: dest }); render(); }
  return dest || "";
}

async function chooseRuntime() {
  const path = await go.call("ChooseRuntime").catch(error => {
    const text = message(error);
    toast(text.startsWith("runtime_not_zstd") ? t("runtimeNotZstd") : text);
    return "";
  });
  if (path) { keep({ Runtime: path }); render(); }
  return path || "";
}

// ---- dialogs -------------------------------------------------------------------------------------

// ask opens a dialog and gives the value of the button pressed: "cancel" with Escape
function ask(dialog) {
  return new Promise(resolve => {
    dialog.returnValue = "";
    dialog.addEventListener("close", () => resolve(dialog.returnValue || "cancel"), { once: true });
    dialog.showModal();
  });
}

// a line of a list of a dialog: a game and what to know of it
function line(name, text) {
  const item = $("line-template").content.firstElementChild.cloneNode(true);
  item.querySelector("b").textContent = name;
  item.querySelector("span").textContent = text;
  return item;
}

// ---- a queue -------------------------------------------------------------------------------------

// what is sent to the window of a game: no icon, which it does not need
const itemOf = row => Object.assign({}, row.game, { Icon: "" });

function modeOf(section) {
  if (section === "compress") return state.settings.Mode === "folder" ? "folder" : "p5z";
  if (section === "decompress") return state.unpackTo;
  return "verify";
}

// the games of the section that can go: read, not done yet, and of what the section works on;
// repair data goes to every .p5z of Verify, and a repair to the damaged ones that have theirs
function readyRows(section, kind) {
  if (kind === "protect") return state.lists.verify.filter(row => row.game && row.game.Kind === "p5z");
  if (kind === "repair") return state.lists.verify.filter(canRepair);
  return state.lists[section].filter(row => {
    if (!row.game || row.state === "ok") return false;
    if (section === "compress" && row.game.Packed) return false;
    if (section === "verify" && row.game.Kind === "folder" && !row.game.Packed) return false;
    return true;
  });
}

// plan asks the window what the queue would do, and the user what needs a choice: FAT32, the
// runtime of ampr-emu-ng, games that cannot go to the destination. It gives the plans, or null
// when the user stops it.
async function plan(section, rows, opts) {
  for (;;) {
    const plans = await go.call("Plan", section, rows.map(itemOf), opts);
    const names = n => plans[n].Item.Name;
    // FAT32: a .p5z of more than 4 GiB does not fit; the folder does
    const big = plans.map((p, n) => n).filter(n => plans[n].Block === "fat32_too_big");
    if (big.length) {
      $("fat32-list").replaceChildren(...big.map(n => line(names(n), size(plans[n].Item.OriginalSize))));
      const answer = await ask($("fat32-dialog"));
      if (answer === "folder") { keep({ Mode: "folder" }); opts.Mode = "folder"; render(); continue; }
      if (answer === "anyway") { opts.ContinueFat32 = true; continue; }
      return null;
    }
    // ampr-emu-ng, once, only for a folder (a .p5z is given it by MaxPack for the console when it
    // plays) and only without the built-in one: chosen and kept in the settings, or not given, and
    // then said clearly
    const lacking = plans.map((p, n) => n).filter(n => (plans[n].Warnings || []).includes("needs_runtime"));
    if (section === "compress" && opts.Mode === "folder" && lacking.length && !opts.Runtime && !builtInRuntime() && !state.askedRuntime) {
      $("runtime-list").replaceChildren(...lacking.map(n => line(names(n), plans[n].Item.TitleID || "")));
      const answer = await ask($("runtime-dialog"));
      if (answer === "choose") {
        const path = await chooseRuntime();
        if (path) opts.Runtime = path;
        continue;
      }
      if (answer !== "without") return null;
      state.askedRuntime = true;
    }
    // what cannot go to this destination (or be repaired, or get repair data)
    const blocked = plans.map((p, n) => n).filter(n => plans[n].Block);
    if (blocked.length) {
      $("block-list").replaceChildren(...blocked.map(n => line(names(n), detailText(plans[n].Block))));
      $("block-skip").hidden = blocked.length === plans.length;
      $("block-dest").hidden = !writes.includes(section);
      const answer = await ask($("block-dialog"));
      if (answer === "dest") {
        const dest = await chooseDest();
        if (!dest) return null;
        opts.Dest = dest;
        continue;
      }
      if (answer !== "skip") return null;
    }
    return plans;
  }
}

// startQueue starts the queue of the section open; kind "protect" adds repair data to the .p5z of
// Verify, and "repair" repairs the damaged ones with theirs (the ones given, or all that can be)
async function startQueue(kind, only) {
  const section = kind ? "verify" : state.section;
  if (state.busy || !queues.includes(section)) return;
  const rows = only || readyRows(section, kind);
  if (!rows.length) { toast(kind === "repair" ? t("repairNothing") : kind === "protect" ? t("protectNothing") : t("nothingToDo")); return; }
  const mode = kind || modeOf(section);
  const repair = mode === "p5z" ? repairOf() : mode === "protect" ? Number(state.settings.RepairPercent) || 5 : 0;
  const opts = { Mode: mode, Dest: "", Level: Number(state.settings.Level) || 19, Runtime: state.settings.Runtime || "", ContinueFat32: false, Replace: [],
    DeleteOriginals: section === "compress" && state.deleteOriginals, DeleteNoBin: false, Repair: repair };
  if (writes.includes(section)) {
    opts.Dest = state.settings.LastDest || await chooseDest();
    if (!opts.Dest) return;
  }
  // nothing else starts while the questions are asked; once the window takes the queue, the page
  // stays busy until job:done (which may even come before Start answers)
  state.busy = true;
  render();
  let job = null, started = false, again = "";
  try {
    const plans = await plan(section, rows, opts);
    if (!plans) return;
    // what is in the way: replaced (said what goes and how much it takes) or skipped
    const going = [], skipped = [];
    const inTheWay = plans.filter(p => !p.Block && (p.Replace || []).length).length;
    let asked = 0, all = "";
    for (const [n, p] of plans.entries()) {
      const row = rows[n];
      if (p.Block) { skipped.push([row, p.Block]); continue; }
      if ((p.Replace || []).length) {
        let answer = all;
        if (!answer) {
          asked++;
          $("replace-text").textContent = t("replaceText")(p.Item.Name);
          $("replace-path").textContent = p.Out;
          $("replace-size").textContent = t("replaceSize")(size(p.ReplaceSize));
          $("replace-all").checked = false;
          $("replace-all-row").hidden = inTheWay - asked < 1;
          $("replace-all-text").textContent = t("replaceAll")(inTheWay - asked);
          answer = await ask($("replace-dialog"));
          if ($("replace-all").checked) all = answer;
        }
        if (answer === "cancel") return;
        if (answer === "skip") { skipped.push([row, "skipped_by_user"]); continue; }
        opts.Replace.push(p.Out);
      }
      going.push(n);
    }
    if (!going.length) { toast(t("nothingToDo")); return; }
    // the originals that go once checked: said with what each takes and where it goes
    if (opts.DeleteOriginals) {
      const answer = await askOriginals(going.map(n => plans[n]), opts);
      if (answer === "cancel") return;
      if (answer === "keep") opts.DeleteOriginals = false;
    }
    // the space of the whole queue, by drive, before anything starts
    const short = await go.call("Space", section, going.map(n => rows[n]).map(itemOf), opts).catch(() => []);
    if (short && short.length) {
      $("space-list").replaceChildren(...short.map(drive => line(ltr(drive.Root), t("spaceLine")(size(drive.Need), size(drive.Free)))));
      // a repair writes a whole copy next to the .p5z: without room for it, it does not start
      $("space-text").textContent = mode === "repair" ? t("spaceRepairText") : t("spaceText");
      $("space-dest").hidden = !writes.includes(section);
      $("space-go").hidden = mode === "repair";
      const answer = await ask($("space-dialog"));
      if (answer === "dest") { again = await chooseDest(); return; }
      if (answer !== "go") return;
    }
    // what to know before it starts: two games to one place, no ID (the space was told above)
    const told = w => w === "duplicate_target" || w === "no_title_id";
    const warned = going.filter(n => (plans[n].Warnings || []).some(told));
    if (warned.length) {
      $("warn-list").replaceChildren(...warned.map(n => line(plans[n].Item.Name, plans[n].Warnings.filter(told).map(w => t("warn_" + w)).join(" · "))));
      if (await ask($("warn-dialog")) !== "go") return;
    }
    // the games left out are said in the list; the others wait for their turn
    for (const [row, detail] of skipped) Object.assign(row, { state: "skipped", detail, out: "", outSize: 0, warnings: [] });
    job = { section, mode: opts.Mode, repair: opts.Repair, dest: opts.Dest, runtime: opts.Runtime || builtInRuntime(), rows: going.map(n => rows[n]), shown: 0, rate: 0,
      opts: Object.assign({}, opts, { Replace: [] }), deletes: opts.DeleteOriginals, early: {} };
    for (const n of going) {
      Object.assign(rows[n], { state: "waiting", detail: "", step: "", phase: "", offset: 0, done: 0, total: 0, out: "", outSize: 0, repairSize: 0,
        warnings: plans[n].Warnings || [] });
    }
    job.running = true;
    state.job = job;
    state.cancelling = false;
    await go.call("Start", section, job.rows.map(itemOf), opts);
    started = true;
  } catch (error) {
    toast(message(error));
  } finally {
    if (!started) {
      state.busy = false;
      if (job) for (const row of job.rows) if (row.state === "waiting") row.state = "";
    }
    render();
  }
  // another destination was chosen for the space: the queue is planned again with it
  if (again && !started) startQueue(kind, only);
}

// askOriginals says which originals go once checked, and asks: "delete", "keep" or "cancel". It
// asks when one of them is a folder, or cannot go to the recycle bin (the user says whether those
// are deleted for good: opts.DeleteNoBin); an image that goes to the bin is not asked about.
async function askOriginals(plans, opts) {
  const forGood = !!state.settings.DeletePermanently;
  const noBin = plans.filter(p => p.NoBin);
  if (!plans.some(p => p.Item.Kind === "folder") && !noBin.length) return "delete";
  $("delete-text").textContent = t(forGood ? "deleteTextGood" : "deleteTextBin");
  $("delete-list").replaceChildren(...plans.map(p => line(p.Item.Name, size(p.SourceSize || p.Item.OriginalSize) + " · " + (p.NoBin ? t("noBin_" + p.NoBin) : t(forGood ? "goesForGood" : "goesToBin")))));
  $("delete-nobin-row").hidden = !noBin.length;
  $("delete-nobin").checked = false;
  $("delete-nobin-text").textContent = t("deleteNoBin")(noBin.length);
  const answer = await ask($("delete-dialog"));
  if (answer === "delete" && opts) opts.DeleteNoBin = $("delete-nobin").checked;
  return answer;
}

// The games added to the section whose queue runs join it, a few at a time: with its options, as
// queued, and counted in its totals, its time left and its summary. When the queue takes the
// originals away, the user is asked about theirs; the space of the queue is told again.
const toJoin = [];
let joinTimer = 0;
function join(row) {
  if (!toJoin.includes(row)) toJoin.push(row);
  clearTimeout(joinTimer);
  joinTimer = setTimeout(joinQueue, 250);
}
async function joinQueue() {
  const job = state.job;
  if (!running() || job.joining) return;
  const rows = toJoin.splice(0).filter(row => row.game && state.lists[job.section].includes(row) && !job.rows.includes(row)
    && row.state !== "ok" && !(job.section === "compress" && row.game.Packed) && !(job.section === "verify" && row.game.Kind === "folder" && !row.game.Packed));
  if (!rows.length) return;
  job.joining = true;
  try {
    let keepOriginals = false;
    if (job.deletes) {
      const plans = await go.call("Plan", job.section, rows.map(itemOf), Object.assign({}, job.opts, { DeleteOriginals: true }));
      const opts = { DeleteNoBin: false };
      const answer = await askOriginals(plans, opts);
      if (answer === "cancel" || !running()) return;
      keepOriginals = answer === "keep" || plans.some(p => p.NoBin && !opts.DeleteNoBin);
    }
    const first = await go.call("Append", job.section, rows.map(itemOf), [], keepOriginals);
    rows.forEach((row, n) => {
      job.rows[first + n] = row;
      Object.assign(row, { state: "queued", detail: "", step: "", phase: "", offset: 0, done: 0, total: 0, out: "", outSize: 0, warnings: [], unqueued: false });
      // what the window told of it before the page knew its place
      for (const event of job.early[first + n] || []) onItem(event);
    });
    toast(t("queuedAdded")(rows.length));
    const short = await go.call("Space", job.section, [], job.opts).catch(() => []);
    if (short && short.length) toast(t("spaceQueued")(ltr(short[0].Root), size(short[0].Need), size(short[0].Free)));
  } catch (error) {
    // the queue ended meanwhile: they stay in the list, to start with the button
  } finally {
    job.joining = false;
    render();
    if (toJoin.length) joinQueue();
  }
}

// ---- what the window tells of a queue -----------------------------------------------------------

function onItem(event) {
  const job = state.job, row = job && job.rows[event.i];
  if (job && !row && job.running && !job.summary) {
    (job.early[event.i] = job.early[event.i] || []).push(event);
    return;
  }
  if (!row) return;
  if (working.includes(event.state) && event.state !== row.step) Object.assign(row, { step: event.state, phase: "", offset: 0, done: 0, total: 0 });
  row.state = event.state;
  row.detail = event.detail || "";
  if (event.out) Object.assign(row, { out: event.out, outSize: event.size || 0, homebrew: !!event.homebrew });
  row.repairSize = event.repair || 0;
  paintRow(row);
  paintSummary();
  // a .p5z repaired, or given repair data: what it has next to it now
  if (ended.includes(event.state) && (job.mode === "repair" || job.mode === "protect")) readRepair(row);
}

function onProgress(event) {
  const job = state.job, row = job && job.rows[event.i];
  if (!row) return;
  // a new phase of the same step: the bytes of the one before count as done
  if (row.phase && event.phase !== row.phase) row.offset += row.total;
  Object.assign(row, { phase: event.phase, done: event.done, total: event.total });
  job.rate = event.rate || 0;
  paintState(row);
  paintSummary();
}

function onDone(summary) {
  const job = state.job;
  state.busy = false;
  state.cancelling = false;
  if (!job) { render(); return; }
  job.summary = summary;
  render();
  if (writes.includes(job.section) && summary.Done + summary.Failed > 0) showDone(job, summary);
  if (job.mode === "repair") toast(t("repairedCount")(summary.Repaired || 0));
  if (job.mode === "protect") toast(t("protectedCount")(summary.Protected || 0));
}

// the games of a queue, in the words of the notice of the system (noticeText in notify.go)
function summaryLine(summary) {
  const parts = [t("gamesDone")(summary.Done)];
  if (summary.Failed) parts.push(t("gamesFailed")(summary.Failed));
  if (summary.Cancelled) parts.push(t("gamesCancelled")(summary.Cancelled));
  if (summary.Skipped) parts.push(t("gamesSkipped")(summary.Skipped));
  if (summary.Saved) parts.push(t("saved")(summary.Saved));
  return parts.join(", ");
}

function showDone(job, summary) {
  $("done-saved").hidden = !summary.Saved;
  $("done-saved").textContent = summary.Saved ? t("saved")(summary.Saved) : "";
  const seconds = Math.round((summary.Duration || 0) / 1e9);
  $("done-stats").textContent = summaryLine(summary);
  // the time it took, the games done, and how fast it went through them on average
  const through = inQueue(job).filter(row => row.state === "ok" && row.game).reduce((sum, row) => sum + row.game.OriginalSize, 0);
  $("done-tile-time").textContent = time(seconds);
  $("done-tile-games").textContent = String(summary.Done || 0);
  $("done-tile-speed").textContent = seconds > 0 && through ? t("speed")(size(through / seconds)) : "—";
  const originals = [];
  if (summary.Trashed) originals.push(t("doneInBin")(summary.TrashedSize));
  if (summary.Deleted) originals.push(t("doneDeleted")(summary.DeletedSize));
  if (summary.Kept) originals.push(t("doneKept")(summary.Kept));
  $("done-originals").hidden = !originals.length;
  $("done-originals").textContent = originals.join(" ");
  // the repair data made with the games, and the games that did not get the one asked for
  const repair = [];
  if (summary.Protected) repair.push(t("doneRepair")(summary.Protected, summary.ProtectedSize || 0));
  if (summary.Unprotected) repair.push(t("doneNoRepair")(summary.Unprotected));
  $("done-repair").hidden = !repair.length;
  $("done-repair").textContent = repair.join(" ");
  const top = (summary.Top || []).map(game => {
    const item = document.createElement("li");
    const name = document.createElement("span"), saved = document.createElement("b");
    name.textContent = game.Name + (game.TitleID ? " (" + game.TitleID + ")" : "");
    saved.textContent = size(game.Saved);
    item.append(name, saved);
    return item;
  });
  $("done-top").replaceChildren(...top);
  $("done-top-block").hidden = !top.length;
  $("done-next-block").hidden = !summary.Done;
  $("done-next").textContent = nextStep(job);
  // the folders made without a runtime that reads zstd are said again, at the end: none was given
  // (the dump has none, Settings neither and the built-in one was missing), or the one of Settings
  // does not read zstd; a .p5z is never one
  const lacking = job.section === "compress" ? summary.NoZstdRuntime || 0 : 0;
  $("done-runtime").hidden = !lacking;
  $("done-runtime").textContent = lacking ? (job.runtime ? t("doneRuntimeWrong")(lacking) : t("doneNoRuntime")(lacking)) : "";
  const dialog = $("done-dialog");
  dialog.classList.toggle("failed", !summary.Done);
  const sizes = summary.Saved > 0 ? doneSizes(job) : null;
  $("done-squeeze").hidden = !sizes;
  if (!dialog.open) dialog.showModal();
  playDone(dialog, sizes, summary.Saved || 0);
}

// what the games made well took before and take now, when the queue compressed and every one of
// them says its size; null otherwise (unpacking, nothing made, a size missing)
function doneSizes(job) {
  if (job.section !== "compress") return null;
  let before = 0, after = 0;
  for (const row of job.rows) {
    if (row.state !== "ok") continue;
    if (!row.game || !row.outSize) return null;
    before += row.game.OriginalSize;
    after += row.outSize;
  }
  return before > 0 ? { before, after } : null;
}

// The end of a queue that saved space: the size before is squeezed into the size now, the bar
// with it, and what it saved counts up from 0 with a small burst. With less motion asked for, the
// end at once. Without sizes the dialog is the plain one.
let counting = 0;
function playDone(dialog, sizes, saved) {
  dialog.classList.remove("go");
  counting++;
  if (!sizes) return;
  $("done-before").textContent = size(sizes.before);
  $("done-after").textContent = size(sizes.after);
  $("done-before-text").textContent = t("doneBefore")(size(sizes.before));
  $("done-after-text").textContent = t("doneAfter")(size(sizes.after));
  dialog.style.setProperty("--squeeze", String(Math.max(0.02, Math.min(1, sizes.after / sizes.before))));
  burst();
  if (window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    dialog.classList.add("go");
    return;
  }
  requestAnimationFrame(() => requestAnimationFrame(() => dialog.classList.add("go")));
  const run = counting, start = performance.now() + 1400, text = $("done-saved");
  text.textContent = t("saved")(0);
  const step = now => {
    if (run !== counting) return;
    const share = Math.max(0, Math.min(1, (now - start) / 900));
    text.textContent = t("saved")(saved * (1 - Math.pow(1 - share, 3)));
    if (share < 1) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}

// the dots of the burst, made once: where each flies to is its own --dx and --dy
function burst() {
  const box = $("done-burst");
  if (box.children.length) return;
  for (let n = 0; n < 18; n++) {
    const dot = document.createElement("i"), angle = n / 18 * Math.PI * 2, reach = 70 + (n % 3) * 34;
    dot.style.setProperty("--dx", Math.round(Math.cos(angle) * reach + 120) + "px");
    dot.style.setProperty("--dy", Math.round(Math.sin(angle) * reach * 0.6) + "px");
    box.append(dot);
  }
}

// what to do now with the games made, by where they are: a folder for the console is seen by
// ShadowMountPlus only in homebrew/ at the root of its drive, which the window tells of each one
// (homebrew in job:item); one made elsewhere has to be moved there
function nextStep(job) {
  if (job.mode === "p5z") return t("nextP5z");
  if (job.mode === "original") return t("nextOriginal")(job.dest);
  const made = job.rows.filter(row => row.state === "ok" && row.out);
  const away = made.filter(row => !row.homebrew);
  if (!away.length) return t("nextFolder");
  const folders = [...new Set(away.map(row => row.out.replace(/[\\/]+[^\\/]+[\\/]*$/, "")))];
  return t("nextMove")(folders.length === 1 ? folders[0] : job.dest);
}

// a game as a report tells it: what it is, its sizes and how it ended, never its name, ID or path
// (the window leaves them out); a game of Details says how it ended in another list
function gameReport(row) {
  const game = row.game || {};
  let ran = row;
  if (!row.state) {
    for (const section of queues) {
      const other = state.lists[section].find(r => r.state && pathKey(r.path) === pathKey(row.path));
      if (other) { ran = other; break; }
    }
  }
  return { Kind: game.Kind || "", Packed: !!game.Packed, OriginalSize: game.OriginalSize || 0, InstallSize: game.InstallSize || 0,
    P5zSize: game.P5zSize || 0, OutSize: ran.outSize || 0, Volumes: game.Volumes || 0, Files: game.Files || 0,
    HasZstdRuntime: !!game.HasZstdRuntime, HasRuntime: !!game.HasRuntime, State: ran.state || "", Detail: ran.detail || "", Out: ran.out || "" };
}
function copyReport(rows, section, mode) {
  go.call("CopyReport", { Section: section || "", Mode: mode || "", Games: rows.map(gameReport) })
    .then(() => toast(t("reportCopied")), error => toast(message(error)));
}

// ---- Compare the levels on this PC, and Measure this PC --------------------------------------------

// the threads of this PC, as the window says them (the page alone knows the browser's guess)
const cpuThreads = () => (state.setup && state.setup.cpuThreads) || navigator.hardwareConcurrency || 1;
// a game of Compress whose levels can be compared: read, and not compressed already
const canCompare = row => row.section === "compress" && !!row.game && !row.game.Packed && !row.error;
// what the window says of a comparison or a measure that did not end well
function toolError(error) {
  if (error === "cancelled") return t("state_cancelled");
  if (/^busy/.test(error)) return t("toolBusy");
  return detailText(error);
}

// toolDialog opens the card of a comparison or a measure; closing it (Close, or Escape) while it
// works cancels it
function toolDialog(dialog) {
  if (!dialog.open) dialog.showModal();
}
function cancelTool() {
  const tool = state.tool;
  if (!tool || !tool.running || tool.cancelling) return;
  tool.cancelling = true;
  paintTool();
  go.call("CancelTool").catch(error => toast(message(error)));
}

// compareLevels packs a sample of the game at levels 3, 9, 15 and 19 (CompareLevels), and shows each
// level as the engine tells it: the size of what Compress makes of it, what that saves and how long
// it takes on this PC. A level of the table is chosen with its button, once all are measured.
async function compareLevels(row) {
  if (!row || state.busy || !canCompare(row)) return;
  state.tool = { kind: "levels", row, rows: [], done: 0, total: 0, running: true, cancelling: false, result: null, error: "" };
  state.busy = true;
  render();
  toolDialog($("levels-dialog"));
  paintTool();
  try {
    const result = await go.call("CompareLevels", itemOf(row));
    Object.assign(state.tool, { result, rows: result.Rows || [] });
  } catch (error) {
    state.tool.error = message(error);
  } finally {
    state.tool.running = false;
    state.busy = false;
    paintTool();
    render();
  }
}

// the level chosen in the table: kept, as the slider keeps it
function useLevel(level) {
  keep({ Level: level });
  paintLevel(level);
  $("levels-dialog").close();
  toast(t("levelChosen")(level));
}

// measurePC packs generated data at a quarter, half, three quarters and all the threads of the PC
// and measures the disk of the destination (MeasurePC); the window keeps the threads it recommends,
// which Compress then uses
async function measurePC() {
  if (state.busy) return;
  const dest = state.settings.LastDest || "";
  state.tool = { kind: "measure", dest, trials: [], done: 0, total: 0, running: true, cancelling: false, result: null, error: "" };
  state.busy = true;
  render();
  toolDialog($("measure-dialog"));
  paintTool();
  try {
    const result = await go.call("MeasurePC", dest);
    Object.assign(state.tool, { result, trials: result.Trials || [] });
    // what the window kept: the next settings the page writes keep it too
    state.settings = Object.assign({}, state.settings, { Threads: result.Threads || 0 });
  } catch (error) {
    state.tool.error = message(error);
  } finally {
    state.tool.running = false;
    state.busy = false;
    paintTool();
    render();
  }
}

function paintTool() {
  const tool = state.tool;
  if (!tool) return;
  const share = tool.total ? Math.min(1, tool.done / tool.total) : 0;
  const status = tool.running ? (tool.cancelling ? t("cancelling") : t(tool.kind === "levels" ? "levelsWorking" : "measureWorking")(Math.floor(share * 100)))
    : tool.error ? toolError(tool.error) : "";
  const prefix = tool.kind === "levels" ? "levels" : "measure";
  $(prefix + "-bar").hidden = !tool.running;
  $(prefix + "-bar").firstElementChild.style.transform = "scaleX(" + share + ")";
  $(prefix + "-status").textContent = status;
  $(prefix + "-status").hidden = !status;
  $(prefix + "-cancel").hidden = !tool.running;
  $(prefix + "-cancel").disabled = tool.cancelling;
  $(prefix + "-close").hidden = tool.running;
  if (tool.kind === "levels") paintLevels(tool);
  else paintMeasure(tool);
}

function paintLevels(tool) {
  const game = tool.row.game, folder = modeOf("compress") === "folder";
  const original = (tool.result && tool.result.OriginalSize) || game.OriginalSize || 0;
  $("levels-game").textContent = [game.Name, game.TitleID, size(game.OriginalSize)].filter(Boolean).join(" · ");
  $("levels-for").textContent = t("madeFor") + ": " + t(folder ? "forUsb" : "forInstaller");
  $("levels-size-head").textContent = folder ? t("levelsFolder") : ltr(".p5z");
  const rows = tool.rows.slice().sort((a, b) => a.Level - b.Level).map(level => {
    const tr = $("level-row-template").content.firstElementChild.cloneNode(true);
    const made = folder ? level.FolderSize : level.P5zSize, seconds = folder ? level.FolderSeconds : level.P5zSeconds;
    const saved = Math.max(0, original - made);
    tr.querySelector(".level").textContent = String(level.Level);
    tr.querySelector(".size-cell").textContent = size(made);
    tr.querySelector(".saved-cell").textContent = original ? size(saved) + " · " + isolate(t("smallerBy")(Math.round(saved / original * 100))) : "—";
    tr.querySelector(".time-cell").textContent = time(seconds || 0);
    tr.classList.toggle("current", Number(state.settings.Level) === level.Level);
    const use = tr.querySelector(".use");
    use.textContent = t("levelsUse");
    use.hidden = tool.running || !!tool.error;
    use.addEventListener("click", () => useLevel(level.Level));
    return tr;
  });
  $("levels-rows").replaceChildren(...rows);
  const result = tool.result;
  $("levels-about").textContent = result ? t("levelsAbout")(size(result.SampleBytes), Math.round((result.SampleRate || 0) * 100), result.Threads || cpuThreads()) : t("compareHint");
}

function paintMeasure(tool) {
  const result = tool.result, s = state.settings;
  $("measure-intro").textContent = t("measureIntro")(cpuThreads(), Number(s.Level) || 19) + " " + (tool.dest ? t("measureDiskOf")(ltr(tool.dest)) : t("measureNoDest"));
  const fastest = Math.max(1, ...tool.trials.map(trial => trial.Rate || 0));
  const rows = tool.trials.map(trial => {
    const tr = $("trial-row-template").content.firstElementChild.cloneNode(true);
    const used = !!result && trial.Threads === result.Recommended;
    tr.classList.toggle("best", used);
    tr.querySelector(".threads").textContent = String(trial.Threads);
    tr.querySelector(".speed span").textContent = t("speed")(size(trial.Rate || 0));
    tr.querySelector(".speed .bar div").style.transform = "scaleX(" + ((trial.Rate || 0) / fastest) + ")";
    tr.querySelector(".used").textContent = used ? t("measureUsed") : "";
    return tr;
  });
  $("measure-rows").replaceChildren(...rows);
  let disk = "";
  if (result && result.DiskMeasured) disk = t("measureDisk")(ltr(tool.dest), t("speed")(size(result.WriteRate)), t("speed")(size(result.ReadRate)));
  else if (result && result.DiskError === "dest_missing") disk = t("detail_dest_missing");
  else if (result && result.DiskError) disk = t("measureDiskFailed")(result.DiskError);
  $("measure-disk").textContent = disk;
  $("measure-disk").hidden = !disk;
  let said = "";
  if (result) {
    if (!result.Threads) said = t("measureResultAll")(result.CPUThreads || cpuThreads());
    else if (result.Threads === result.Fastest) said = t("measureResultFastest")(result.Threads);
    else said = t("measureResultFewer")(result.Threads, result.Fastest);
  } else if (tool.error && !tool.running) {
    said = t("measureNotKept");
  }
  $("measure-result").textContent = said;
  $("measure-result").hidden = !said;
}

// what the window tells while a comparison or a measure works
function onToolProgress(event) {
  const tool = state.tool;
  if (!tool || !tool.running) return;
  tool.done = Math.max(tool.done, event.done || 0);
  tool.total = event.total || tool.total;
  paintTool();
}
function onToolLevel(row) {
  const tool = state.tool;
  if (!tool || !tool.running || tool.kind !== "levels") return;
  tool.rows.push(row);
  paintTool();
}
function onToolTrial(trial) {
  const tool = state.tool;
  if (!tool || !tool.running || tool.kind !== "measure") return;
  tool.trials.push(trial);
  paintTool();
}

// ---- Rename ----------------------------------------------------------------------------------------

// planRenames asks the window the new name of each game of Rename, by the template of Settings
let renamesAsked = 0;
async function planRenames() {
  const rows = state.lists.rename.filter(row => row.game);
  const asked = ++renamesAsked;
  if (!rows.length) return;
  const plans = await go.call("RenamePlan", rows.map(itemOf)).catch(() => null);
  if (!plans || asked !== renamesAsked) return;
  rows.forEach((row, n) => {
    if (row.state === "ok" && plans[n].Block === "same_name") return;
    Object.assign(row, { renameTo: plans[n].To || "", renameBlock: plans[n].Block || "", state: "", detail: "" });
  });
  if (state.section === "rename") render();
}

// renameAll names the games of Rename as shown; a name taken, a file in use or a game that cannot
// be read is skipped and said
async function renameAll() {
  const rows = state.lists.rename.filter(row => row.game && row.renameTo && !row.renameBlock);
  if (!rows.length || running()) { toast(t("renameNone")); return; }
  state.busy = true;
  render();
  try {
    const results = await go.call("Rename", rows.map(itemOf));
    let renamed = 0;
    results.forEach((result, n) => {
      const row = rows[n];
      if (result.State === "ok") {
        renamed++;
        // the game is at its new name, in every list that has it
        for (const list of Object.values(state.lists)) {
          for (const other of list) {
            if (pathKey(other.path) !== pathKey(result.From)) continue;
            other.path = result.To;
            if (other.game) other.game = Object.assign({}, other.game, { Path: result.To });
          }
        }
      }
      Object.assign(row, { state: result.State, detail: result.State === "ok" ? "" : result.Detail });
    });
    toast(t("renamed")(renamed));
  } catch (error) {
    toast(message(error));
  } finally {
    state.busy = false;
    render();
  }
}

// closing the window while a queue runs, asked by the window: the game at work is cancelled and
// the window closes, or it goes on
function askClose() {
  const dialog = $("close-dialog");
  if (dialog.open) return;
  ask(dialog).then(answer => { if (answer === "close") go.call("ConfirmClose").catch(error => toast(message(error))); });
}
// the words of the question the window asks with the dialog of the system when the page cannot
// ask, in the language of the page
function tellCloseTexts() {
  go.call("TellLanguage", state.lang).catch(() => { });
  go.call("CloseTexts", t("closeQuestion"), t("closeYes"), t("closeNo")).catch(() => { });
}

// ---- dropping files ----------------------------------------------------------------------------

// the layer over the window while files are dragged over it, as in NXPress: it goes with the
// drop, also when the system takes the drop and the page gets neither a drop nor a leave, and
// with the mouse moving on it, as no drag goes on then. Nothing dragged over the window is
// taken by its browser (it would open the file).
let dragging = 0;
function showDrop(show) {
  if (!show) dragging = 0;
  $("drop-overlay").hidden = !show;
}
function watchDrops() {
  const keepFromBrowser = event => event.preventDefault();
  window.addEventListener("dragover", keepFromBrowser, true);
  window.addEventListener("drop", keepFromBrowser, true);
  window.addEventListener("dragenter", event => {
    if (!event.dataTransfer || ![...event.dataTransfer.types].includes("Files")) return;
    dragging++;
    showDrop(true);
  });
  window.addEventListener("dragleave", () => { dragging = Math.max(0, dragging - 1); if (!dragging) showDrop(false); });
  window.addEventListener("drop", () => showDrop(false));
  $("drop-overlay").addEventListener("mousemove", () => showDrop(false));
}

// ---- start -----------------------------------------------------------------------------------------

function wire() {
  for (const button of document.querySelectorAll("nav button[data-section]")) button.addEventListener("click", () => setSection(button.dataset.section));
  const chooseFiles = () => go.call("ChooseFiles", state.section).then(paths => addPaths(state.section, paths), error => toast(message(error)));
  const chooseFolder = () => go.call("ChooseFolder", state.section).then(paths => addPaths(state.section, paths), error => toast(message(error)));
  $("add-files").addEventListener("click", chooseFiles);
  $("empty-files").addEventListener("click", chooseFiles);
  $("add-folder").addEventListener("click", chooseFolder);
  $("empty-folder").addEventListener("click", chooseFolder);
  $("clear").addEventListener("click", clearList);
  $("start").addEventListener("click", () => (state.section === "rename" ? renameAll() : startQueue()));
  // the repair data: Compress gives it to each .p5z when ticked (5 % the first time), as Settings
  // keeps it; Verify adds it to the .p5z of the list, or repairs the damaged ones with theirs
  $("repair-data").addEventListener("change", () => { keep({ RepairData: $("repair-data").checked }); render(); });
  $("pref-repair").addEventListener("change", () => { keep({ RepairData: $("pref-repair").checked }); render(); });
  $("add-repair").addEventListener("click", () => startQueue("protect"));
  $("repair-damaged").addEventListener("click", () => startQueue("repair"));
  $("delete-originals").addEventListener("change", () => { state.deleteOriginals = $("delete-originals").checked; render(); });
  $("pref-low").addEventListener("change", () => keep({ LowPriority: $("pref-low").checked }));
  $("pref-delete").addEventListener("change", () => { keep({ DeleteOriginals: $("pref-delete").checked }); state.deleteOriginals = $("pref-delete").checked; render(); });
  $("pref-forgood").addEventListener("change", () => { keep({ DeletePermanently: $("pref-forgood").checked }); render(); });
  // a template of one's own is kept once it is one; meanwhile the preview says what it gives
  $("template").addEventListener("input", () => previewNames($("template").value));
  $("template").addEventListener("change", async () => {
    const template = $("template").value.trim();
    const preview = await previewNames(template);
    if (preview && !preview.Error) setTemplate(template);
  });
  $("rename-settings").addEventListener("click", () => setSection("settings"));
  // Compare the levels: the first game of Compress that can be, or the one whose link was clicked
  $("compare-levels").addEventListener("click", () => {
    const row = state.lists.compress.find(canCompare);
    if (row) compareLevels(row); else toast(t("compareNeedsGame"));
  });
  $("pref-measure").addEventListener("click", measurePC);
  $("pref-threads-all").addEventListener("click", () => { keep({ Threads: 0 }); render(); });
  for (const [id, place] of explorerPlaces) $("pref-" + id).addEventListener("change", () => setExplorer(place, $("pref-" + id).checked));
  for (const name of ["levels", "measure"]) {
    $(name + "-cancel").addEventListener("click", cancelTool);
    $(name + "-dialog").addEventListener("close", cancelTool);
  }
  $("pref-report").addEventListener("click", () => copyReport([], ""));
  $("done-report").addEventListener("click", () => { const job = state.job; if (job) copyReport(inQueue(job), job.section, job.mode); });
  $("link-bug").addEventListener("click", () => go.call("OpenLink", "bug").catch(error => toast(message(error))));
  $("link-game").addEventListener("click", () => go.call("OpenLink", "game").catch(error => toast(message(error))));
  $("cancel").addEventListener("click", () => {
    state.cancelling = true;
    paintStart();
    paintSummary();
    go.call("Cancel").catch(error => toast(message(error)));
  });
  for (const input of document.querySelectorAll('input[name="made-for"]')) input.addEventListener("change", () => { keep({ Mode: input.value }); render(); });
  for (const input of document.querySelectorAll('input[name="unpack-to"]')) input.addEventListener("change", () => { state.unpackTo = input.value; save("unpackTo", input.value); render(); });
  $("level").addEventListener("input", () => paintLevel($("level").value));
  $("level").addEventListener("change", () => { keep({ Level: Number($("level").value) }); paintLevel($("level").value); });
  for (const button of document.querySelectorAll("button[data-level]")) button.addEventListener("click", () => { keep({ Level: Number(button.dataset.level) }); paintLevel(button.dataset.level); });
  $("choose-dest").addEventListener("click", chooseDest);
  $("choose-runtime").addEventListener("click", chooseRuntime);
  $("pref-runtime-choose").addEventListener("click", chooseRuntime);
  $("pref-runtime-clear").addEventListener("click", () => { keep({ Runtime: "" }); state.askedRuntime = false; render(); });
  // "System" (the language is the one of the system again) and the languages of i18n/, each by its
  // own name, whatever the language of the window is
  $("languages").replaceChildren(...[["", ""], ...languages].map(([code, name]) => {
    const button = document.createElement("button");
    button.setAttribute("role", "radio");
    button.dataset.lang = code;
    if (code) button.textContent = name; else button.dataset.t = "themeSystem";
    if (code) button.lang = languageInfo(code)[2] || code;
    button.addEventListener("click", () => { keep({ Lang: code }); state.lang = chooseLanguage(code); tellCloseTexts(); relabelMenu(); render(); });
    return button;
  }));
  for (const button of document.querySelectorAll("button[data-theme]")) button.addEventListener("click", () => { keep({ Theme: button.dataset.theme }); applyTheme(); render(); });
  watchDrops();
  window.addEventListener("resize", placeNavHighlight);
}

async function main() {
  wire();
  render();
  if (!go.has()) { banner(t("noWindow")); return; }
  // the files of the window come by "files:add" once it was asked for what came before: the
  // event is listened to first, so none is lost between the two
  go.on("files:add", addSent);
  go.on("job:item", onItem);
  go.on("job:progress", onProgress);
  go.on("job:done", onDone);
  go.on("close:ask", askClose);
  go.on("tool:progress", onToolProgress);
  go.on("tool:level", onToolLevel);
  go.on("tool:trial", onToolTrial);
  go.onDrop((x, y, paths) => dropped(paths));
  try {
    state.setup = await go.call("Setup");
    state.settings = Object.assign({}, defaults, await go.call("GetSettings"));
  } catch (error) {
    banner(message(error));
  }
  state.lang = chooseLanguage(state.settings.Lang);
  state.deleteOriginals = !!state.settings.DeleteOriginals;
  tellCloseTexts();
  applyTheme();
  const setup = state.setup;
  banner(setup && setup.coreError ? (setup.coreOtherVersion ? coreText(setup) : t("coreMissing") + " " + setup.coreError) : "");
  render();
  addSent(await go.call("PendingFiles").catch(() => null));
  loadExplorer();
  offerLeftovers();
}

// what a run cut short (a crash, a power cut) left apart is offered for deletion as MaxPack opens
async function offerLeftovers() {
  const left = await go.call("Leftovers").catch(() => null);
  if (!left || !left.length) return;
  // a repair cut short puts its .p5z back (the one that verified, else the damaged one as it was);
  // a folder that may have a copy of a .p5z that is in its place stays
  const about = item => [size(item.Size), item.Restore ? t("leftoverRestore")(ltr(item.Restore)) : "", item.Keep ? t("leftoverKeep") : ""].filter(Boolean).join(" · ");
  $("leftover-list").replaceChildren(...left.map(item => line(ltr(item.Path), about(item))));
  $("leftover-repair").hidden = !left.some(item => item.Restore || item.Keep);
  if (await ask($("leftover-dialog")) !== "delete") return;
  const failed = await go.call("CleanLeftovers").catch(error => [message(error)]);
  toast(failed && failed.length ? t("leftoverFailed")(failed.length) + " " + failed.join("; ") : t("leftoverDone"));
}

if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", main);
else main();
