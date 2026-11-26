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
const working = ["packing", "verifying", "unpacking", "protecting", "repairing", "growing", "sending"];
// what the card of a .p5z in Details does to it: DLC added, or a new dump of its game (see growRow)
const grows = ["add_dlc", "update"];
// the states of a game of a queue that did not start: there from the start, or added while it runs
const notStarted = ["waiting", "queued"];
// the shortcuts of the level, as in NXPress: the level each one sets, and the ones it covers
const presets = [[3, 1, 6], [12, 7, 14], [19, 15, 22]];
// the images of a game ShadowMountPlus mounts and the PS5 packages, which the window takes as it
// takes a dump folder (jobs.ImageExts): by their extension until the engine says what each one is
const images = [".exfat", ".ffpkg", ".ffpfs", ".ffpfsc", ".pkg"].map(type => [type, ""]);
// what each section takes, as the empty list says it: [type, its text or none]
const takes = {
  compress: [["", "typeDump"], ...images],
  decompress: [[".p5z", ""]],
  verify: [[".p5z", ""], ["", "typePacked"]],
  details: [["", "typeDump"], ...images, [".p5z", ""]],
  rename: [["", "typeDump"], ...images, [".p5z", ""]],
};
const defaultTemplate = "{title} [{id}]";
const defaults = { Lang: "", Theme: "system", Runtime: "", LastDest: "", Mode: "p5z", Level: 19, DeleteOriginals: false, DeletePermanently: false, LowPriority: false, NameTemplate: defaultTemplate, Threads: 0,
  RepairData: false, RepairPercent: 5, Passcode: "", KeepLeftovers: false, ConsoleIP: "", ConsoleCode: "",
  WatchOn: false, WatchFolder: "", WatchSend: false, QuietEnd: false, UpdateCheck: false };
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
  // the queue running is paused (job:paused), and a pause or a resume was asked and not answered yet
  paused: false,
  pausing: false,
  // the runtime of ampr-emu-ng is asked once: after "Go on without it" (runtimeWithout) the window does not insist
  askedRuntime: false,
  // Compare the levels or Measure this PC, at work or the last one: see compareLevels and measurePC
  tool: null,
  // MaxPack in the Explorer of Windows, as ExplorerPlaces tells it: each place "" (not there),
  // "here" (for this copy) or "moved" (for another copy), and whether .p5z opens with another program
  explorer: { available: false, menu: "", sendTo: "", files: "", filesOther: false },
  // a place of the Explorer is being added or removed: its switches wait
  explorerBusy: false,
  // the watched folder as the window tells it (StartWatch, watch:status): the folder, and why it cannot
  // be looked at ("gone", "unreadable"; "" when it can)
  watch: { folder: "", error: "", existing: 0 },
  // the look for a new version (Settings › Internet), as the window answered it (null while it looks), and
  // whether it was asked and the toast said, in this page
  update: null,
  updateAsked: false,
  updateTold: false,
};

// whether a queue is running: the window took it, and has not said its end yet
const running = () => !!(state.busy && state.job && state.job.running && !state.job.summary);
// whether the queue running can pause: one of Compress, Decompress or Verify (what Details does cannot:
// a send would make the console stop the receive)
const canPause = () => running() && queues.includes(state.job.section) && !state.cancelling;

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
  tr.querySelector(".add-dlc").addEventListener("click", () => addDlcTo(row));
  tr.querySelector(".to-verify").addEventListener("click", () => takeTo(row, "verify"));
  tr.querySelector(".to-decompress").addEventListener("click", () => takeTo(row, "decompress"));
  tr.querySelector(".repair").addEventListener("click", () => startQueue("repair", [row]));
  tr.querySelector(".to-add-dlc").addEventListener("click", () => growRow(row, "add_dlc"));
  tr.querySelector(".to-update").addEventListener("click", () => growRow(row, "update"));
  tr.querySelector(".grow-anyway").addEventListener("click", () => growAnyway(row));
  tr.querySelector(".to-send").addEventListener("click", () => sendRows([row]));
  tr.querySelector(".send-discard").addEventListener("click", () => discardOnConsole(row));
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
      .then(game => {
        row.game = game;
        if (row.section === "details") row.repair = game.Repair || null;
        // the engine found a change of it that had stopped halfway (a power cut) and undid it
        if (game && game.Recovered) toast(t("recoveredToast")(game.Name || baseName(row.path)));
      }, error => { row.error = message(error); })
      .then(() => (row.section === "verify" && row.game && row.game.Kind === "p5z" ? readRepair(row) : null))
      .finally(() => {
        describing--;
        paintRow(row);
        // in Compress a DLC goes inside the games of its title ID: what each row says may change
        if (row.section === "compress") for (const other of state.lists.compress) if (other !== row) paintRow(other);
        paintSummary();
        paintStart();
        // a game added to the section whose queue runs joins it (not a repair, nor repair data being
        // added: a game is not given those unasked); one of Rename shows its new name
        if (row.game && running() && state.job.section === row.section && !state.job.rows.includes(row) && !["repair", "protect"].includes(state.job.mode) && !row.watched) join(row);
        if (row.watched) watchSoon();
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
  if (grows.includes(mode)) return job.repair ? ["growing", "verifying", "protecting"] : ["growing", "verifying"];
  // a .p5z sent: its bytes go to the console, which then verifies them
  if (mode === "send") return ["sending", "verifying"];
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
    const weight = stepsOf(job).length * Math.max(1, row.game ? (job.mode === "send" ? sendSize(row) : row.game.OriginalSize) : 1);
    whole += weight;
    done += rowShare(row, job) * weight;
  }
  job.shown = Math.max(job.shown || 0, whole ? done / whole : 0);
  return { share: job.shown, left: job.rate > 0 ? (whole - done) / job.rate : 0 };
}

// ---- what a game says ----------------------------------------------------------------------------

// what a game is, by the kind the engine gives it: a folder (a dump, or compressed), a .p5z, or an
// image, which the engine tells by its contents whatever its extension says
function kindName(game) {
  let name;
  switch (game.Kind) {
    case "p5z": return ".p5z";
    case "folder": case "": case undefined: name = game.Packed ? t("kindPacked") : t("kindDump"); break;
    default: name = imageName(game.Kind);
  }
  // a DLC, with files of its own or a license only
  return isDlc(game) ? name + " · " + t(game.DLCData ? "kindDlc" : "kindDlcLicense") : name;
}
// an image by its kind: exFAT, UFS2, PFS, a PFS container that holds one of them, or a PS5 package
function imageName(kind) {
  switch (kind) {
    case "exfat": return t("kindImage");
    case "ufs2": return t("kindUfs2");
    case "pfs": return t("kindPfs");
    case "pfsc": return t("kindPfsc");
    case "fpkg": return t("kindFpkg");
  }
  return t("kindOther");
}
const imageKinds = { exfat: ".exfat", ufs2: ".ffpkg", pfs: ".ffpfs", pfsc: ".ffpfsc", fpkg: ".pkg" };
// what a game took before it was compressed, as the end card and what it saved count it (Item.SizeBefore
// in jobs/plan.go): an image's file on disk, what the disk gets back, never more; a folder, the game.
// 0 for an image whose file is not known: nothing is claimed for it
function sizeBefore(game) {
  return Number(imageKinds[game.Kind] ? game.ImageSize : game.OriginalSize) || 0;
}
// the same for a game of a queue, with the DLC that went inside it, which what was made holds too (as
// the window counts what it saved); 0 when the game's is not known
function beforeOf(row) {
  const before = sizeBefore(row.game);
  return before ? before + (Number(row.dlcSentSize) || 0) : 0;
}
// a compressed image (a PFS container, or a PFS image whose files are PFSC-compressed) takes less
// than the game in it, so what a level saves against the game is more than against its file; a PS5
// package's file is not the game's size either (compressed, or with its own padding)
const compressedImage = game => (game.Kind === "pfsc" || game.Kind === "pfs" && !!game.Compressed || game.Kind === "fpkg") && Number(game.ImageSize) > 0;
// a 32-character passcode a PS5 package can have, as the engine takes it (or "", the default)
const validPasscode = p => p === "" || /^[\x21-\x7e]{32}$/.test(p);
// what the page says of an error: a package that needs another passcode says where to give it
const errorText = raw => (/--passcode/.test(raw) ? t("detail_passcode") : raw);
// the extension of an image as the user has it, else the one of its kind
const imageExt = game => ((/\.[^.\\/]+$/.exec(game.Path || "") || [""])[0] || imageKinds[game.Kind] || "").toLowerCase();
// what something made of a compressed image of made bytes takes against its file ("vs your .ffpfsc:
// −8%", or "+3%" when it is bigger); "" for anything else, where the saving is already against what it takes
function vsImage(game, made) {
  if (!compressedImage(game)) return "";
  const image = Number(game.ImageSize), change = Math.round(Math.abs(1 - made / image) * 100);
  return t("vsImage")(imageExt(game), made <= image ? t("smallerBy")(change) : t("largerBy")(change));
}

// the warnings of a game in its section: the ones of the list, and the ones of its plan
function rowWarnings(row) {
  const game = row.game, list = [];
  if (!game) return list;
  // a DLC of Compress: why it goes inside no game, or not inside the one that started without it
  if (row.section === "compress" && isDlc(game)) {
    if (dlcErrorOf(row)) return list;   // said as the error of its row
    const { games, late } = dlcStatus(row);
    if (!firstOfItsId(row)) list.push(t("dlcTwin"));
    else if (!games.length) list.push(t("dlcNoGame")(titleOf(game)));
    if (late.length) list.push(t("dlcLate")(namesOf(late)));
    return list;
  }
  if (row.section === "compress" && game.Packed) list.push(t("detail_already_packed"));
  // a dump that lacks files of the original game makes a backup that lacks them too: said before
  // it is packed, which it still is
  if (row.section === "compress" && !game.Packed) {
    const c = game.Completeness;
    if (fromDumpTool(c)) list.push(...copyProblems(c));
    else if (missingOf(game)) list.push(t("warn_missing_files")(missingOf(game)));
  }
  // only a .p5z or a compressed folder can be verified: what the engine says is neither (a dump, or
  // an image named as a .p5z) has nothing to verify
  if (row.section === "verify" && !game.Packed) list.push(t("detail_not_packed"));
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
  found = /^([a-z][a-z0-9_]*)(?:: ([\s\S]*))?$/.exec(part);
  if (found && typeof texts.en["detail_" + found[1]] === "string") {
    const rest = found[2] || "";
    if (found[1] === "verify_failed") return t("detail_verify_failed") + (rest ? ": " + partText(rest) : "");
    return t("detail_" + found[1]) + (rest ? ": " + rest : "");
  }
  return t("detail_other") + ": " + part;
}

// ---- whether a game has every file of the original package ---------------------------------------

// the files of the original package a game lacks, as the engine counts them against the list the
// game carries (sce_sys/playgo-hash-table.dat); 0 when it could not be checked, or was not said
function missingOf(game) {
  const c = game && game.Completeness;
  return c && c.Checkable ? Number(c.Missing) || 0 : 0;
}
// whether it was checked against the dump tool's list (ampr_emu.index), not the original package's:
// that list proves only that the copy has every file the dump tool wrote, with its size, never that
// the dump is complete
const fromDumpTool = c => !!(c && c.Checkable && c.Source === "dump_index");
// what a copy lacks against the dump tool's list: the files missing, and the ones of another size
function copyProblems(c) {
  const list = [];
  if (Number(c.Missing) > 0) list.push(t("copyMissing")(Number(c.Missing)));
  if (Number(c.SizeMismatch) > 0) list.push(t("copyOtherSize")(Number(c.SizeMismatch)));
  return list;
}
// whether it is complete, for its card in Details: yes, how many are missing, or why it cannot be told;
// against the dump tool's list, whether the copy has every file it wrote
function completenessText(c) {
  if (fromDumpTool(c)) {
    const problems = copyProblems(c);
    return problems.length ? t("damagedCopy") + ": " + problems.join(", ") : t("dumpListAll")(c.Listed) + " · " + t("dumpListHint");
  }
  if (c.Checkable) return c.Missing ? t("completeNo")(c.Missing, c.Listed) : t("completeYes")(c.Listed);
  const why = { no_table: "completeNoTable", unknown_format: "completeUnknownFormat", damaged: "completeDamaged", unreadable: "completeUnreadable",
    dump_index_damaged: "completeDumpIndexDamaged", dump_index_mismatch: "completeDumpIndexMismatch", dump_index_packed: "completeDumpIndexPacked" }[c.Reason];
  return t(why || "completeOther");
}
// the facts of Details of a game against its list, as [name, value]: whether it is complete, what of
// it is not in the original package and what dump tools left, with what Compress does with that: it
// leaves it out when the list proves it is not the game's, unless Settings keeps it. A .p5z only
// says what it has. None when the engine said nothing.
function completenessFacts(game) {
  const c = game && game.Completeness;
  if (!c) return [];
  const dumpTool = fromDumpTool(c);
  const facts = [[t(dumpTool ? "factDumpList" : "factComplete"), completenessText(c)]];
  if (c.Checkable && c.ExtraCount) facts.push([t(dumpTool ? "factNotInDumpList" : "factExtra"), t("extraOf")(c.ExtraCount, size(c.ExtraBytes))]);
  if (c.LeftoverCount) {
    const what = t("leftoversOf")(size(c.LeftoverBytes), c.LeftoverCount);
    // only the original package's list proves they are not the game's
    const proven = c.Checkable && !dumpTool;
    const fate = game.Kind === "p5z" || game.Packed ? "" : !proven ? t("leftoversUnproven") : state.settings.KeepLeftovers ? t("leftoversKept") : t("leftoversOut");
    facts.push([t("factLeftovers"), fate ? what + " · " + fate : what]);
  }
  return facts;
}
// the line of a game of Verify: whether what it checked is complete, when that could be told
function completeLineText(row) {
  const c = row.game && row.game.Completeness;
  if (row.section !== "verify" || !c || !c.Checkable) return "";
  if (fromDumpTool(c)) {
    const problems = copyProblems(c);
    return problems.length ? t("damagedCopy") + ": " + problems.join(", ") : t("dumpListAll")(c.Listed);
  }
  return c.Missing ? t("incompleteLine")(c.Missing, c.Listed) : t("completeLine")(c.Listed);
}

// ---- DLC: they go inside their game ----------------------------------------------------------------

// a DLC (a package of additional content, or a dumped DLC folder), as the engine tells it: it is never
// compressed alone, but inside each game of its title ID in Compress
const isDlc = game => !!game && game.Content === "dlc";
// the title ID a game or a DLC goes by: its own, else its content ID's ("PPSA00000" of
// "UP0000-PPSA00000_00-..."), as the engine matches them
const titleOf = game => (game && (game.TitleID || String(game.ContentID || "").slice(7, 16))) || "";
const readRow = row => !!row.game && !row.error;
// a content ID dlc_emu takes, as the engine checks it (mp_dlc_content_id_ok): 36 characters,
// XXXXXX-TITLEID00_00-LABEL of 16, letters and digits
const validContentId = id => /^[A-Za-z0-9]{6}-[A-Za-z0-9]{9}_[A-Za-z0-9]{2}-[A-Za-z0-9]{16}$/.test(String(id || ""));
// the games of Compress a "DLC" row has the content ID of: it is that game, not a DLC of it
const gamesItIs = row => state.lists.compress.filter(other => readRow(other) && !isDlc(other.game) && !!other.game.ContentID && other.game.ContentID === row.game.ContentID);
// why a DLC row of Compress goes inside no game, as an error of its own row ("" when it can go): the
// engine would refuse it, and so stop the whole game it went with
function dlcErrorOf(row) {
  if (row.section !== "compress" || !readRow(row) || !isDlc(row.game)) return "";
  if (!validContentId(row.game.ContentID)) return t("dlcBadId");
  const games = gamesItIs(row);
  return games.length ? t("dlcIsGame")(namesOf(games)) : "";
}
// a DLC row that can go inside a game
const goodDlc = row => readRow(row) && isDlc(row.game) && !dlcErrorOf(row);
// whether a DLC row of Compress is the first of the list with its content ID: another one with it is
// the same DLC, which goes once
const firstOfItsId = row => state.lists.compress.find(other => goodDlc(other) && other.game.ContentID === row.game.ContentID) === row;
// the games of Compress a DLC row goes inside: read, of its title, not compressed already
function gameRowsOf(row) {
  const id = titleOf(row.game);
  if (!id || !goodDlc(row) || !firstOfItsId(row)) return [];
  return state.lists.compress.filter(other => readRow(other) && !isDlc(other.game) && !other.game.Packed && titleOf(other.game) === id);
}
// the DLC rows that go inside a game of Compress, in the order of the list, once each
function dlcRowsOf(row) {
  if (row.section !== "compress" || !readRow(row) || isDlc(row.game) || row.game.Packed) return [];
  const id = titleOf(row.game);
  return id ? state.lists.compress.filter(other => goodDlc(other) && titleOf(other.game) === id && firstOfItsId(other)) : [];
}
// the rows of a section still being read: Compress waits for them, as a DLC of a game may be among them
const readingRows = section => (state.lists[section] || []).filter(row => !row.game && !row.error);
// whether what a game of Compress is made with is set: it is made, or in the queue that runs (its DLC
// went with it when it joined)
const setRow = row => row.state === "ok" || (running() && state.job.rows.includes(row));
// how many DLC go inside a game of Compress: the ones that went, once that is set
const dlcCountOf = row => (row.dlcSent && setRow(row) ? row.dlcSent.length : dlcRowsOf(row).length);
// what becomes of a DLC row of Compress: the games of its title (games), the ones that were set
// without it (late), the ones it goes inside (going) and, of those, the ones made (made)
function dlcStatus(row) {
  const games = gameRowsOf(row);
  const late = games.filter(other => setRow(other) && !(other.dlcSent || []).includes(row.path));
  const going = games.filter(other => !late.includes(other));
  return { games, late, going, made: going.filter(other => other.state === "ok") };
}
const namesOf = rows => rows.map(other => other.game.Name).join(", ");
// the line of a DLC of Compress: the games it goes inside, or went inside once they are made; "" for
// one that goes in none (its warning says why)
function dlcLineText(row) {
  if (row.section !== "compress" || !readRow(row) || !isDlc(row.game)) return "";
  const { going, made } = dlcStatus(row);
  if (!going.length) return "";
  return made.length === going.length ? t("dlcPackedInto")(namesOf(made)) : t("dlcGoesWith")(namesOf(going));
}
// the facts of Details of the DLC a game carries (a .p5z made with them), as [name, value]: how many,
// and each one with its content ID and what it has
function dlcFacts(game) {
  const list = (game && game.DLC) || [];
  if (!list.length) return [];
  const data = list.filter(d => d.Data).length;
  return [[t("factDlc"), t("dlcCarried")(data, list.length - data)],
    ...list.map((d, n) => ["DLC " + (n + 1), d.ContentID + " · " + (d.Data ? t("dlcFactData")(size(d.Size), d.Folder) : t("dlcFactLicense"))])];
}
// ---- a .p5z of Details that grows: DLC added to it, or a new dump of its game ---------------------

// Add DLC… and Update with a new dump… of the card of a .p5z: what to add is chosen (DLC packages or
// folders; the new dump as a folder, or as an image or package), and then it grows in its place, a
// queue of its own with its bar, its cancel and its end card
async function growRow(row, kind) {
  if (state.busy || !row.game || row.game.Kind !== "p5z") return;
  const dlc = kind === "add_dlc";
  $("grow-title-text").textContent = t(dlc ? "growDlcTitle" : "growDumpTitle");
  $("grow-text").textContent = t(dlc ? "growDlcText" : "growDumpText");
  $("grow-files").textContent = t(dlc ? "growPackages" : "growFile");
  $("grow-folder").textContent = t("growFolder");
  const answer = await ask($("grow-dialog"));
  if (answer !== "files" && answer !== "folder") return;
  const chosen = answer === "folder" ? go.call("ChooseFolder", "compress") : dlc ? go.call("ChooseDLC") : go.call("ChooseFiles", "compress");
  const paths = await chosen.catch(error => { toast(message(error)); return []; });
  if (!paths || !paths.length) return;
  // the new dump is one: the first chosen
  Object.assign(row, { grow: dlc ? paths : [paths[0]], growKind: kind, force: false });
  startQueue(kind, [row]);
}
// an update that stopped because more than half of the game changed can go on, when the user says so
const canGrowAnyway = row => row.section === "details" && row.state === "error" && row.growKind === "update" && !!row.grow
  && /(^|; )mostly_changed($|; )/.test(row.detail);
function growAnyway(row) {
  if (!canGrowAnyway(row) || state.busy) return;
  row.force = true;
  startQueue("update", [row]);
}
// the facts of Details of a .p5z that grew: how many times, and the room it does not use
function growFacts(game) {
  if (!game || game.Kind !== "p5z" || !game.Grown) return [];
  return [[t("factGrown"), t("grownTimes")(game.Grown)], [t("factUnused"), t("unusedFact")(size(game.UnusedBytes || 0))]];
}
// a row's note that is good news: a repair, or a .p5z grown with nothing else to say (its repair data
// made again counts as good)
function goodNote(row) {
  if (row.state !== "ok") return false;
  if (/^(repaired|repair_not_needed|sent|send_discarded)$/.test(row.detail)) return true;
  return /^(grown|nothing_to_do)(; repair_redone)?$/.test(row.detail);
}

// the row button: DLC packages chosen go to Compress, and each one inside the game of its title ID
async function addDlcTo() {
  const paths = await go.call("ChooseDLC").catch(error => { toast(message(error)); return []; });
  if (paths && paths.length) addPaths("compress", paths);
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
  // a container: the image that holds the game
  if (game.Kind === "pfsc" && game.Inner) fact(facts, t("factInside"), imageName(game.Inner));
  fact(facts, t("factId"), game.TitleID || "—");
  fact(facts, t("factVersion"), game.Version || "—");
  fact(facts, t("factFiles"), String(game.Files || 0));
  for (const [name, value] of completenessFacts(game)) fact(facts, name, value);
  for (const [name, value] of dlcFacts(game)) fact(facts, name, value);
  for (const [name, value] of growFacts(game)) fact(facts, name, value);
  fact(facts, t("factOriginal"), size(game.OriginalSize));
  // an image: what its file takes, less than the game when it is PFSC-compressed; a package, its
  // file, Kraken-compressed or not
  if (game.ImageSize) {
    const fpkg = game.Kind === "fpkg";
    fact(facts, t(fpkg ? "factPackageFile" : "factImageFile"), size(game.ImageSize) + (game.Compressed ? " · " + t(fpkg ? "krakenCompressed" : "imageCompressed") : ""));
  }
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
  const file = tr.querySelector(".file"), dlc = dlcCountOf(row);
  if (game) file.textContent = [game.TitleID, game.Version && t("versionOf")(game.Version), kindName(game), dlc && t("withDlc")(dlc)].filter(Boolean).join(" · ");
  else file.textContent = row.error ? row.path : t("reading");
  tr.querySelector(".size").textContent = game ? size(game.OriginalSize) : "";

  // once made: the size before and after (compressed), and where it is
  const sizes = tr.querySelector(".sizes"), where = tr.querySelector(".where");
  // what it took before (an image's file) and takes now
  sizes.hidden = !(row.section === "compress" && game && row.outSize && row.state === "ok" && beforeOf(row));
  if (!sizes.hidden) sizes.textContent = size(beforeOf(row)) + " → " + size(row.outSize) + (row.repairSize ? " · " + t("withRepair")(size(row.repairSize)) : "");
  const completeLine = tr.querySelector(".complete-line"), completeText = completeLineText(row);
  completeLine.hidden = !completeText;
  completeLine.textContent = completeText;
  completeLine.classList.toggle("incomplete", !!completeText && (missingOf(game) > 0 || Number(game.Completeness.SizeMismatch) > 0));
  const repairLine = tr.querySelector(".repair-line"), repairText = repairLineText(row);
  repairLine.hidden = !repairText;
  repairLine.textContent = repairText;
  repairLine.classList.toggle("other", !!row.repair && row.repair.state !== "match");
  const repairButton = tr.querySelector(".repair");
  repairButton.hidden = !canRepair(row) || !hasPar2();
  repairButton.textContent = t("repairButton");
  repairButton.disabled = state.busy;
  // a DLC of Compress: the games it goes inside
  const goesWith = dlcLineText(row);
  where.hidden = !row.out && !goesWith;
  if (row.out) where.textContent = t("madeAt")(row.out);
  else if (goesWith) where.textContent = goesWith;
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
  // and what makes it grow: DLC added to it, or a new dump of its game
  for (const [cls, key] of [[".to-add-dlc", "addDlcCard"], [".to-update", "updateCard"], [".to-send", "sendCard"]]) {
    const button = card.querySelector(cls);
    button.textContent = t(key);
    button.disabled = state.busy;
  }
  // what a send that did not end left on the console can be deleted from there, once asked
  const discard = tr.querySelector(".send-discard");
  discard.hidden = !leftOnConsole(row);
  discard.textContent = t("sendDiscard");
  discard.disabled = state.busy;
  const anyway = tr.querySelector(".grow-anyway");
  anyway.hidden = !canGrowAnyway(row);
  anyway.textContent = t("growAnyway");
  anyway.disabled = state.busy;
  const compare = tr.querySelector(".compare");
  compare.hidden = !canCompare(row) || (running() && state.job.rows.includes(row));
  compare.textContent = t("compareRow");
  compare.disabled = state.busy;
  // DLC packages for a game of Compress (a dumped DLC folder comes with Add folder, or dropped)
  const addDlc = tr.querySelector(".add-dlc");
  addDlc.hidden = !(row.section === "compress" && readRow(row) && !isDlc(game) && !game.Packed && row.state !== "ok") || (running() && state.job.rows.includes(row));
  addDlc.textContent = t("addDlc");

  const warnings = rowWarnings(row), warn = tr.querySelector(".list-warn");
  warn.hidden = !warnings.length;
  warn.textContent = warnings.join(" · ");

  const note = tr.querySelector(".note"), dlcError = dlcErrorOf(row);
  const said = (row.error && errorText(row.error)) || dlcError || (row.detail && row.detail !== "cancelled" ? detailText(row.detail) : row.section === "rename" && row.renameBlock && row.state !== "ok" ? detailText(row.renameBlock) : "");
  note.hidden = !said;
  note.textContent = said;
  // a repair, or a .p5z grown, that ended well says so as good news, not as a warning
  note.className = "note " + (row.error || dlcError || row.state === "error" ? "bad" : goodNote(row) ? "good" : "warn");
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
  segs.classList.toggle("paused", active && state.paused);
  badge.classList.toggle("paused", active && state.paused);
  step.textContent = "";
  if (active) {
    const steps = stepsOf(job), index = steps.indexOf(row.step), share = stepShare(row);
    if (segs.children.length !== steps.length) segs.replaceChildren(...steps.map(() => $("seg-template").content.firstElementChild.cloneNode(true)));
    [...segs.children].forEach((seg, n) => {
      seg.className = "seg " + (steps[n] === "verifying" || steps[n] === "protecting" ? "tone-check" : "tone-work");
      seg.firstElementChild.style.transform = "scaleX(" + (n < index ? 1 : n === index ? share : 0) + ")";
    });
    tellBar(segs, rowShare(row, job), t("progressGame")((row.game && row.game.Name) || baseName(row.path)));
    const phase = row.phase ? t("phase_" + row.phase) : "";
    step.textContent = [phase, t("percent")(Math.floor(rowShare(row, job) * 100)), state.paused ? t("stepPaused") : ""].filter(Boolean).join(" · ");
  }
  // what it saved, once made
  const saved = tr.querySelector(".saved"), game = row.game;
  const before = game ? beforeOf(row) : 0;
  const smaller = row.section === "compress" && row.state === "ok" && game && row.outSize && row.outSize < before;
  const savedText = smaller ? t("smallerBy")(Math.round((1 - row.outSize / before) * 100)) : "";
  if (savedText && !saved.textContent) saved.classList.add("pop");
  saved.textContent = savedText;
  tr.querySelector(".remove button").disabled = running() && job.rows.includes(row) && !notStarted.includes(row.state);
  paintRemove(row);
}

// the button that takes a game out of the list is only a sign: it says which game to screen readers
function paintRemove(row) {
  const button = row.tr.querySelector(".remove button"), name = t("removeRow")((row.game && row.game.Name) || baseName(row.path));
  button.setAttribute("aria-label", name);
  button.title = name;
}

// a bar says how far it is to screen readers: a percentage, and whether it is paused
function tellBar(bar, share, label) {
  const percent = Math.floor(Math.min(1, Math.max(0, share)) * 100);
  bar.setAttribute("aria-valuenow", String(percent));
  bar.setAttribute("aria-valuetext", [t("percent")(percent), state.paused && running() ? t("stepPaused") : ""].filter(Boolean).join(" · "));
  if (label) bar.setAttribute("aria-label", label);
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
  document.querySelectorAll("[data-t-placeholder]").forEach(element => { element.placeholder = t(element.dataset.tPlaceholder); });
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
  $("pref-end-notice").checked = !s.QuietEnd;
  paintKeys();
  // the passcode of PS5 packages: empty is the default; one being typed stays as it is
  if (document.activeElement !== $("pref-passcode")) {
    $("pref-passcode").value = s.Passcode || "";
    $("passcode-bad").hidden = true;
  }
  $("pref-leftovers").checked = !s.KeepLeftovers;
  // the watched folder: on only with a folder; what it makes may go to the console too
  $("pref-watch").checked = !!s.WatchOn && !!s.WatchFolder;
  $("pref-watch").disabled = !s.WatchFolder;
  $("pref-watch-folder").textContent = s.WatchFolder || t("watchNoFolder");
  $("pref-watch-status").hidden = !(s.WatchOn && state.watch.error);
  $("pref-watch-status").textContent = state.watch.error === "gone" ? t("watchGone") : t("watchUnreadable");
  $("pref-watch-send").checked = !!s.WatchSend;
  $("pref-watch-send").disabled = !s.WatchOn;
  $("pref-watch-send-row").classList.toggle("off", !s.WatchOn);
  const existing = s.WatchOn ? Number(state.watch.existing) || 0 : 0;
  $("pref-watch-existing-row").hidden = !existing;
  $("pref-watch-existing").textContent = t("watchExisting")(existing);
  // the console a .p5z is sent to: one being typed stays as it is
  if (document.activeElement !== $("pref-console-ip")) {
    $("pref-console-ip").value = s.ConsoleIP || "";
    $("console-ip-bad").hidden = true;
  }
  if (document.activeElement !== $("pref-console-code")) {
    $("pref-console-code").value = s.ConsoleCode || "";
    $("console-code-bad").hidden = true;
  }
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
  paintUpdate();
}

// ---- new versions (Settings › Internet) ----------------------------------------------------------------

// what the look for a new version says: nothing while it is off; in a build without the key, that it cannot
const updateAvailable = () => !!(state.setup && state.setup.updateKey);
function updateText(view) {
  if (!updateAvailable() || (view && view.state === "unavailable")) return t("updateUnavailable");
  if (!state.settings.UpdateCheck) return "";
  if (!view) return t("updateChecking");
  switch (view.state) {
    case "none": return t("updateNone");
    case "found": return t(view.install ? "updateFoundInstall" : "updateFoundPage")(view.version);
    case "error": return typeof texts.en["updateError_" + view.error] === "string" ? t("updateError_" + view.error) : t("updateError_network");
  }
  return "";
}
function paintUpdate() {
  const view = state.update, found = !!view && view.state === "found" && !!state.settings.UpdateCheck;
  $("pref-update").checked = updateAvailable() && !!state.settings.UpdateCheck;
  $("pref-update").disabled = !updateAvailable() || !go.has();
  $("update-status").textContent = state.updateAsked || !updateAvailable() ? updateText(view) : "";
  $("update-status").classList.toggle("warning", !!view && view.state === "error" || !updateAvailable());
  $("update-go").hidden = !found;
  $("update-go").textContent = found ? t(view.install ? "updateInstall" : "updateOpenPage") : "";
}
// looks for a new version once Settings says so: the window asks GitHub once each time MaxPack starts
async function checkUpdate() {
  if (!updateAvailable() || !state.settings.UpdateCheck || state.updateAsked) return;
  state.updateAsked = true;
  state.update = null;
  paintUpdate();
  const view = await go.call("CheckUpdate").catch(() => ({ state: "error", error: "network" }));
  // turned off meanwhile: it is asked again when it is turned on
  if (view && view.state === "off") {
    state.updateAsked = false;
    state.update = null;
  } else {
    state.update = view || { state: "error", error: "network" };
  }
  if (state.update && state.update.state === "found" && !state.updateTold) {
    state.updateTold = true;
    toast(t("updateFoundToast")(state.update.version));
  }
  paintUpdate();
}
// Update: on Windows the installer, checked again, runs and MaxPack closes (not while a queue runs); elsewhere
// the release's page opens
async function runUpdate() {
  const view = state.update;
  if (!view || view.state !== "found") return;
  if (view.install && state.busy) { toast(t("updateWait")); return; }
  await go.call("Update").catch(error => toast(/^busy/.test(message(error)) ? t("updateWait") : message(error)));
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
// what is there for this copy follows it: the menu speaks the language of the page (the installer
// wrote it in its own, and the language may change), and .p5z files get the thumbnails that a
// MaxPack before this one did not add
function relabelMenu() {
  if (state.explorer.menu === "here" || state.explorer.files === "here") go.call("RelabelExplorerMenu", menuTexts()).catch(() => { });
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
  // Compress waits for every row to be read: a DLC still being read must not be left out of its game
  start.disabled = state.busy || !go.has() || !!(state.setup && state.setup.coreError) || !list.some(row => row.game && !(section === "compress" && isDlc(row.game)))
    || (section === "compress" && readingRows(section).length > 0)
    || (section === "rename" && (running() || !list.some(row => row.renameTo && !row.renameBlock && row.state !== "ok")));
  $("cancel").hidden = !running();
  $("cancel").disabled = state.cancelling;
  // Pause and Resume, for the queues that can
  $("pause").hidden = !running() || !queues.includes(state.job.section);
  $("pause").textContent = t(state.paused ? "resume" : "pause");
  $("pause").disabled = !canPause() || state.pausing;
  for (const id of ["add-files", "add-folder", "empty-files", "empty-folder"]) $(id).disabled = !go.has();
  $("clear").disabled = state.busy || list.length === 0;
}

// what a list holds, at its foot: its games and what they take, the DLC (which go inside them) apart
function listedText(list) {
  if (!list.length) return t("nothing");
  const bytes = list.reduce((sum, row) => sum + (row.game ? row.game.OriginalSize : 0), 0);
  const dlc = list.filter(row => isDlc(row.game)).length;
  return dlc ? t("listedDlc")(list.length - dlc, dlc, bytes) : t("listed")(list.length, bytes);
}

function paintSummary() {
  const job = state.job, bar = $("total-bar");
  if (running()) {
    const { share, left } = progressOf(job);
    const rows = inQueue(job), finished = rows.filter(row => ended.includes(row.state)).length;
    $("summary-text").textContent = t("working")(finished, rows.length, Math.floor(share * 100));
    bar.hidden = false;
    bar.firstElementChild.style.transform = "scaleX(" + share + ")";
    bar.classList.toggle("paused", state.paused);
    tellBar(bar, share);
    $("status").textContent = state.cancelling ? t("cancelling") : state.paused ? t("pausedStatus")
      : [job.rate > 0 ? t("speed")(size(job.rate)) : "", left >= 1 ? t("timeLeft")(time(Math.round(left))) : ""].filter(Boolean).join(" · ");
    return;
  }
  $("summary-text").textContent = listedText(state.lists[state.section] || []);
  bar.hidden = true;
  // why Compress waits, else how the last queue of this section ended, in the language of now
  const last = job && job.summary && job.section === state.section;
  const reading = state.section === "compress" ? readingRows("compress").length : 0;
  $("status").textContent = reading ? t("readingWait")(reading) : last ? summaryLine(job.summary) : "";
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

// the settings are kept whole: SetSettings writes every field (the promise ends once they are kept)
function keep(change) {
  state.settings = Object.assign({}, state.settings, change);
  return go.call("SetSettings", state.settings).catch(error => toast(message(error)));
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
  if (dest) { keep({ LastDest: dest }); render(); watchSoon(); }
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

// what is sent to the window of a game: no icon, which it does not need; for a game of Compress, the
// DLC that go inside it (their paths, in the order of the list) and what they take (itemWith: with those
// DLC rows, found by the caller)
function itemOf(row) {
  return itemWith(row, dlcRowsOf(row));
}
function itemWith(row, dlc) {
  // a license adds no files: only the ones with files of their own take room
  return Object.assign({}, row.game, { Icon: "", AddDLC: dlc.map(other => other.path),
    AddDLCSize: dlc.filter(other => other.game.DLCData).reduce((sum, other) => sum + (Number(other.game.OriginalSize) || 0), 0),
    // a .p5z of Details that grows: the DLC to add, or the new dump, and whether to go on with most of it changed
    Grow: row.section === "details" && row.grow ? row.grow.slice() : [], Force: row.section === "details" && !!row.force,
    // the watched folder brought it: its original follows Settings, to the recycle bin at most, once checked again
    Watched: row.section === "compress" && !!row.watched });
}
// the items of the games a queue starts with or takes, each game told which DLC went inside it, what
// they took and how many of them had files of their own
function itemsOf(rows) {
  const dlc = rows.map(row => dlcRowsOf(row));
  const items = rows.map((row, n) => itemWith(row, dlc[n]));
  rows.forEach((row, n) => Object.assign(row, { dlcSent: items[n].AddDLC, dlcSentSize: items[n].AddDLCSize,
    dlcSentData: dlc[n].filter(other => other.game.DLCData).length }));
  return items;
}

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
    // a DLC goes inside its game, never alone
    if (section === "compress" && (row.game.Packed || isDlc(row.game))) return false;
    if (section === "verify" && !row.game.Packed) return false;
    return true;
  });
}

// plan asks the window what the queue would do, and the user what needs a choice: FAT32, the
// runtime of ampr-emu-ng, games that cannot go to the destination. It gives the plans, or null
// when the user stops it.
async function plan(section, rows, opts, auto) {
  for (;;) {
    const plans = await go.call("Plan", section, rows.map(itemOf), opts);
    const names = n => plans[n].Item.Name;
    // FAT32: a .p5z of more than 4 GiB does not fit; the folder does
    const big = plans.map((p, n) => n).filter(n => plans[n].Block === "fat32_too_big");
    // the watched folder asks nothing: what cannot go is left out, with why
    if (big.length && !auto) {
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
    if (section === "compress" && opts.Mode === "folder" && lacking.length && !opts.Runtime && !builtInRuntime() && !state.askedRuntime && !auto) {
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
    if (blocked.length && !auto) {
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
async function startQueue(kind, only, extra, auto) {
  const section = grows.includes(kind) || kind === "send" ? "details" : kind ? "verify" : auto ? "compress" : state.section;
  if (state.busy || !(queues.includes(section) || grows.includes(kind) || kind === "send")) return;
  // a DLC still being read would be left out of its game
  if (section === "compress" && readingRows(section).length) {
    if (auto) watchSoon(); else toast(t("readingWait")(readingRows(section).length));
    return;
  }
  const rows = only || readyRows(section, kind);
  if (!rows.length) { toast(kind === "repair" ? t("repairNothing") : kind === "protect" ? t("protectNothing") : t("nothingToDo")); return; }
  const mode = kind || modeOf(section);
  // a .p5z that grows gets its repair data made again when it had some of its own: the bar counts that step
  const own = grows.includes(mode) && rows[0].repair && rows[0].repair.state === "match" ? Number(rows[0].repair.percent) || 0 : 0;
  const repair = mode === "p5z" ? repairOf() : mode === "protect" ? Number(state.settings.RepairPercent) || 5 : own;
  const opts = { Mode: mode, Dest: "", Level: Number(state.settings.Level) || 19, Runtime: state.settings.Runtime || "", ContinueFat32: false, Replace: [],
    // the watched folder never takes an original away (jobs/watched.go)
    DeleteOriginals: section === "compress" && !auto && state.deleteOriginals, DeleteNoBin: false, Repair: repair, Drive: "" };
  Object.assign(opts, extra || {});
  if (writes.includes(section)) {
    // the watched folder asks nothing: without a destination it waits for one
    opts.Dest = state.settings.LastDest || (auto ? "" : await chooseDest());
    if (!opts.Dest) { if (auto && watchSayOnce("nodest")) toast(t("watchNoDest")); return; }
  }
  // nothing else starts while the questions are asked; once the window takes the queue, the page
  // stays busy until job:done (which may even come before Start answers)
  state.busy = true;
  render();
  let job = null, started = false, again = "";
  try {
    const plans = await plan(section, rows, opts, auto);
    if (!plans) return;
    // what is in the way: replaced (said what goes and how much it takes) or skipped
    const going = [], skipped = [];
    const inTheWay = plans.filter(p => !p.Block && (p.Replace || []).length).length;
    let asked = 0, all = "";
    for (const [n, p] of plans.entries()) {
      const row = rows[n];
      if (p.Block) { skipped.push([row, p.Block]); continue; }
      if ((p.Replace || []).length) {
        // the watched folder never replaces: what is in the destination already is not done again
        let answer = auto ? "watch" : all;
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
        if (answer === "watch") { skipped.push([row, "watch_exists"]); continue; }
        opts.Replace.push(p.Out);
      }
      going.push(n);
    }
    if (!going.length) {
      for (const [row, detail] of auto ? skipped : []) { Object.assign(row, { state: "skipped", detail }); watchEnded(row); }
      if (!auto) toast(t("nothingToDo"));
      return;
    }
    // the originals that go once checked: said with what each takes and where it goes (the watched
    // folder does as Settings says, and keeps the ones that cannot go to the recycle bin)
    if (opts.DeleteOriginals && !auto) {
      const answer = await askOriginals(going.map(n => plans[n]), opts);
      if (answer === "cancel") return;
      if (answer === "keep") opts.DeleteOriginals = false;
    }
    // the space of the whole queue, by drive, before anything starts
    const short = await go.call("Space", section, going.map(n => rows[n]).map(itemOf), opts).catch(() => []);
    if (short && short.length && auto) { if (watchSayOnce("nospace")) toast(t("watchNoSpace")); return; }
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
    if (warned.length && !auto) {
      $("warn-list").replaceChildren(...warned.map(n => line(plans[n].Item.Name, plans[n].Warnings.filter(told).map(w => t("warn_" + w)).join(" · "))));
      if (await ask($("warn-dialog")) !== "go") return;
    }
    // the games left out are said in the list; the others wait for their turn
    for (const [row, detail] of skipped) { Object.assign(row, { state: "skipped", detail, out: "", outSize: 0, warnings: [] }); watchEnded(row); }
    job = { section, mode: opts.Mode, repair: opts.Repair, dest: opts.Dest, runtime: opts.Runtime || builtInRuntime(), rows: going.map(n => rows[n]), shown: 0, rate: 0,
      opts: Object.assign({}, opts, { Replace: [] }), deletes: opts.DeleteOriginals, early: {}, watched: !!auto };
    for (const n of going) {
      Object.assign(rows[n], { state: "waiting", detail: "", step: "", phase: "", offset: 0, done: 0, total: 0, out: "", outSize: 0, repairSize: 0,
        warnings: plans[n].Warnings || [] });
    }
    job.running = true;
    state.job = job;
    if (auto) state.watchSaid = "";
    state.cancelling = false;
    await go.call("Start", section, itemsOf(job.rows), opts);
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
  if (again && !started) startQueue(kind, only, extra, auto);
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
    && row.state !== "ok" && !(job.section === "compress" && (row.game.Packed || isDlc(row.game))) && !(job.section === "verify" && !row.game.Packed));
  if (!rows.length) return;
  job.joining = true;
  try {
    let keepOriginals = false;
    // what the watched folder brings is not asked about: its originals follow Settings, whatever this queue
    // does with the others (jobs/watched.go), to the recycle bin at most
    if (job.deletes && !rows.every(row => row.watched)) {
      const plans = await go.call("Plan", job.section, rows.map(itemOf), Object.assign({}, job.opts, { DeleteOriginals: true }));
      const opts = { DeleteNoBin: false };
      const answer = await askOriginals(plans, opts);
      if (answer === "cancel" || !running()) return;
      keepOriginals = answer === "keep" || plans.some(p => p.NoBin && !opts.DeleteNoBin);
    }
    const first = await go.call("Append", job.section, itemsOf(rows), [], keepOriginals);
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

// ---- Send to the console ----------------------------------------------------------------------------

// the console of Settings: an IP address (four numbers of 0 to 255, and maybe ":port") and the pairing
// code it shows (6 digits); the window checks them again (settings.go)
const validConsoleIp = ip => {
  const found = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?::(\d{1,5}))?$/.exec(String(ip));
  // no leading zeros (the engine refuses them)
  return !!found && found.slice(1, 6).every(n => n === undefined || !/^0\d/.test(n)) && found.slice(1, 5).every(n => Number(n) <= 255)
    && (found[5] === undefined || (Number(found[5]) >= 1 && Number(found[5]) <= 65535));
};
const validConsoleCode = code => /^\d{6}$/.test(String(code));
// what a .p5z of Details takes to send
const sendSize = row => Number(row.game && (row.game.P5zSize || row.game.OriginalSize)) || 0;
// the room the console keeps on a drive beyond the .p5z (the contract's margin)
const sendMargin = 256 * 1024 * 1024;
// a drive of the console as the send dialog says it: its name, and its free space of its size
function driveText(drive) {
  const usb = /^usb(\d+)$/.exec(drive.id || "");
  const name = drive.id === "internal" ? t("driveInternal") : usb ? t("driveUsb")(Number(usb[1]) + 1) : String(drive.id || "");
  return t("driveLine")(name, size(Number(drive.free) || 0), size(Number(drive.total) || 0));
}
// whether the .p5z of rows fit on a drive, with the console's margin
const driveFits = (drive, rows) => (Number(drive.free) || 0) >= rows.reduce((sum, row) => sum + sendSize(row), 0) + sendMargin;
// why the console could not be asked or refused, in the page's words
function sendErrorText(code, words) {
  const key = code === "no_console" ? "detail_no_console" : "detail_send_" + code;
  return typeof texts.en[key] === "string" ? t(key) : t("detail_other") + ": " + (words || code || "");
}
// the .p5z a queue of Compress made well, which its end card can send
function madeP5z(job) {
  if (!job || job.section !== "compress" || job.mode !== "p5z") return [];
  return inQueue(job).filter(row => row.state === "ok" && /\.p5z$/i.test(row.out || "")).map(row => row.out);
}

// sendRows sends .p5z of Details to the console of Settings, one after another: its drives are asked for,
// one is chosen in the send dialog (with its free space), and the queue goes with its bar, its cancel and
// its end card
async function sendRows(rows) {
  rows = rows.filter(row => row.game && row.game.Kind === "p5z");
  if (!rows.length) return;
  if (state.busy) { toast(t("sendBusy")); return; }
  if (!validConsoleIp(state.settings.ConsoleIP) || !validConsoleCode(state.settings.ConsoleCode)) {
    toast(t("sendNoConsole"));
    setSection("settings");
    return;
  }
  // the dialog opens at once, asking the console for its drives: Cancel stops the question, and nothing
  // else starts meanwhile
  $("send-list").replaceChildren(...rows.map(row => line(row.game.Name || baseName(row.path), size(sendSize(row)))));
  $("send-drives").replaceChildren();
  $("send-asking").hidden = false;
  $("send-asking").textContent = t("sendAsking");
  $("send-go").disabled = true;
  state.busy = true;
  render();
  const answer = ask($("send-dialog"));
  let view = null;
  try {
    view = await Promise.race([go.call("ConsoleDrives").catch(error => ({ drives: [], error: "io", message: message(error) })), answer.then(() => null)]);
  } finally {
    if (!view) {
      go.call("CancelConsoleDrives").catch(() => {});
      state.busy = false;
      render();
    }
  }
  if (!view) return;
  // busy until a drive is picked: nothing else starts under the dialog
  const done = () => { state.busy = false; render(); };
  if (view.error || !view.drives || !view.drives.length) {
    done();
    if ($("send-dialog").open) $("send-dialog").close("cancel");
    toast(sendErrorText(view.error || "no_drive", view.message));
    return;
  }
  $("send-asking").hidden = true;
  $("send-go").disabled = false;
  const last = load("sendDrive", "");
  const chosen = view.drives.some(drive => drive.id === last) ? last : (view.drives.find(drive => driveFits(drive, rows)) || view.drives[0]).id;
  $("send-drives").replaceChildren(...view.drives.map(drive => {
    const label = document.createElement("label"), input = document.createElement("input"), text = document.createElement("span");
    const name = document.createElement("b"), room = document.createElement("small");
    label.className = "choice" + (driveFits(drive, rows) ? "" : " no-room");
    input.type = "radio";
    input.name = "send-drive";
    input.value = drive.id;
    input.checked = drive.id === chosen;
    name.textContent = driveText(drive);
    room.textContent = driveFits(drive, rows) ? (drive.fs || "") : t("sendNoRoom");
    text.append(name, room);
    label.append(input, text);
    return label;
  }));
  const answered = await answer;
  done();
  if (answered !== "send") return;
  const picked = document.querySelector('input[name="send-drive"]:checked');
  const drive = picked ? picked.value : chosen;
  save("sendDrive", drive);
  for (const row of rows) row.sentDrive = drive;
  startQueue("send", rows, { Drive: drive });
}

// a send that did not end with the game on the console, where what arrived stays (stopped here or on the
// console, cut off, stalled, a write error, or arrived damaged): it can be deleted there
const leftOnConsole = row => row.section === "details" && !!row.game && row.game.Kind === "p5z"
  && /^send_(kept|console_stopped|stalled|write_error|unreachable|verify_failed)(:|$)/.test(row.detail || "");
// Delete what arrived on the console: asked first, as it cannot be undone (the next send starts from nothing)
async function discardOnConsole(row) {
  if (!leftOnConsole(row)) return;
  if (state.busy) { toast(t("sendBusy")); return; }
  $("discard-text").textContent = t("discardText")(row.game.Name || baseName(row.path));
  if (await ask($("discard-dialog")) !== "delete") return;
  state.busy = true;
  render();
  try {
    const view = await go.call("DiscardOnConsole", row.path, row.sentDrive || load("sendDrive", "")).catch(error => ({ error: "io", message: message(error) }));
    if (view.error) { toast(sendErrorText(view.error, view.message)); return; }
    row.detail = "send_discarded";
    toast(t("discardDone")(row.game.Name || baseName(row.path)));
  } finally {
    state.busy = false;
    render();
  }
}

// the end card of Compress: the .p5z it made go to Details, and from there to the console once read
async function sendMade(job) {
  const paths = madeP5z(job);
  if (!paths.length) return;
  addPaths("details", paths);
  setSection("details");
  const wanted = new Set(paths.map(pathKey));
  const rowsOf = () => state.lists.details.filter(row => wanted.has(pathKey(row.path)));
  for (let waited = 0; waited < 120 && rowsOf().some(row => !row.game && !row.error); waited++) await new Promise(done => setTimeout(done, 250));
  sendRows(rowsOf());
}

// ---- the watched folder ------------------------------------------------------------------------------

// what arrived in the watched folder, whole (watch:found): it goes to Compress as if it had been dropped on
// it, and is compressed with its settings once read; one that came back changed goes again
function onWatchFound(event) {
  const paths = (event && event.paths) || [];
  if (!paths.length) return;
  const wanted = new Set(paths.map(pathKey));
  for (const row of state.lists.compress) {
    if (!wanted.has(pathKey(row.path)) || (running() && state.job.rows.includes(row) && !ended.includes(row.state))) continue;
    Object.assign(row, { state: "", detail: "", game: null, error: "", watchHeld: false, watchTold: "" });
    describe(row);
  }
  addPaths("compress", paths);
  for (const row of state.lists.compress) if (wanted.has(pathKey(row.path))) row.watched = true;
  toast(t("watchFound")(paths.length));
  watchSoon();
}
// the watched rows that are not done, and of those the games that can go now: read, whole (one that lacks
// files of its list stays in the list with its warning, held) and not compressed already; a DLC goes inside
// its game, never alone
const watchedRows = () => state.lists.compress.filter(row => row.watched && (!row.state || row.state === "queued"));
const incomplete = game => missingOf(game) > 0 || (fromDumpTool(game && game.Completeness) && copyProblems(game.Completeness).length > 0);
function watchReady(rows) {
  const ready = [];
  for (const row of rows) {
    if (!readRow(row) || row.state || isDlc(row.game) || row.game.Packed) continue;
    if (incomplete(row.game)) {
      if (!row.watchHeld) Object.assign(row, { watchHeld: true, detail: "watch_incomplete" });
      continue;
    }
    if (!row.watchHeld) ready.push(row);
  }
  return ready;
}
// the window is told how a watched game ended: done (compressed, failed, in the destination already) until it
// changes; anything else (cancelled, skipped for another reason) comes again in the next run
const watchFinal = row => row.state === "ok" || row.state === "error" || (row.state === "skipped" && /^(watch_)?exists$/.test(row.detail));
// what keeps the watched folder waiting (no destination, no room) is said once per wait, not on every try
function watchSayOnce(what) {
  if (state.watchSaid === what) return false;
  state.watchSaid = what;
  return true;
}
function watchEnded(row) {
  if (!row.watched || !ended.includes(row.state) || row.watchTold === row.state) return;
  row.watchTold = row.state;
  go.call("WatchEnded", row.path, watchFinal(row)).catch(() => {});
}
// what was in the watched folder when it was turned on: compressed only when asked
async function takeExisting() {
  // they come as what arrives comes (watch:found), once whole
  await go.call("WatchTakeExisting").catch(error => toast(message(error)));
  state.watch.existing = 0;
  render();
}
let watchTimer = 0;
function watchSoon() {
  clearTimeout(watchTimer);
  watchTimer = setTimeout(watchTick, 400);
}
function watchTick() {
  const rows = watchedRows();
  // all of them read first: a DLC must find its game
  if (rows.some(row => !row.game && !row.error)) return;
  const ready = watchReady(rows);
  for (const row of rows) paintRow(row);
  if (!ready.length) return;
  if (running() && state.job.section === "compress") { for (const row of ready) join(row); return; }
  if (state.busy) return; // the end of what runs tries again
  startQueue("", ready, undefined, true);
}
// what the watched folder made, once its queue ended, goes to the console too: to the drive last chosen
async function sendWatched(job) {
  const paths = madeP5z(job).filter(path => job.rows.some(row => row && row.watched && row.out === path));
  if (!paths.length) { watchSoon(); return; }
  if (!validConsoleIp(state.settings.ConsoleIP) || !validConsoleCode(state.settings.ConsoleCode)) { toast(t("sendNoConsole")); watchSoon(); return; }
  addPaths("details", paths);
  const wanted = new Set(paths.map(pathKey));
  const rowsOf = () => state.lists.details.filter(row => wanted.has(pathKey(row.path)));
  for (let waited = 0; waited < 120 && rowsOf().some(row => !row.game && !row.error); waited++) await new Promise(done => setTimeout(done, 250));
  const rows = rowsOf().filter(row => row.game && row.game.Kind === "p5z");
  if (rows.length && !state.busy) startQueue("send", rows, { Drive: load("sendDrive", "") }, true);
  else watchSoon();
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
  watchEnded(row);
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

// pause or resume the queue running (Pause, Ctrl+P); the window says it once done (job:paused)
async function togglePause() {
  if (!canPause() || state.pausing) return;
  const on = !state.paused;
  state.pausing = true;
  paintStart();
  try {
    await go.call(on ? "Pause" : "Resume");
    onPaused({ paused: on });
  } catch (error) {
    toast(message(error));
  } finally {
    state.pausing = false;
    paintStart();
  }
}
// the queue running is paused, or goes on: its rows, its bar and what it says
function onPaused(event) {
  state.paused = !!(event && event.paused) && running();
  if (state.job) for (const row of inQueue(state.job)) paintState(row);
  paintStart();
  paintSummary();
}

function onDone(summary) {
  const job = state.job;
  state.busy = false;
  state.cancelling = false;
  state.paused = false;
  state.pausing = false;
  if (!job) { render(); return; }
  job.summary = summary;
  render();
  if ((writes.includes(job.section) || grows.includes(job.mode) || job.mode === "send") && summary.Done + summary.Failed > 0) showDone(job, summary);
  // a .p5z that grew is read again: its version, its DLC, how many times it grew, its repair data
  if (grows.includes(job.mode)) for (const row of job.rows) if (row && row.state === "ok" && state.lists.details.includes(row)) describe(row);
  if (job.mode === "repair") toast(t("repairedCount")(summary.Repaired || 0));
  if (job.watched && state.settings.WatchSend && madeP5z(job).length) sendWatched(job);
  else watchSoon();
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
  // (a .p5z that grew went through what was added to it, not the whole game: no speed is claimed)
  const through = grows.includes(job.mode) ? 0 : inQueue(job).filter(row => row.state === "ok" && row.game).reduce((sum, row) => sum + (job.mode === "send" ? sendSize(row) : row.game.OriginalSize), 0);
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
  // the .p5z made can go to the console from here
  $("done-send").hidden = !madeP5z(job).length;
  const dialog = $("done-dialog");
  dialog.classList.toggle("failed", !summary.Done);
  const sizes = summary.Saved > 0 ? doneSizes(job) : null;
  $("done-squeeze").hidden = !sizes;
  if (!dialog.open) dialog.showModal();
  playDone(dialog, sizes, summary.Saved || 0);
}

// what the games made well took before and take now, when the queue compressed and every one of
// them says its size; null otherwise (unpacking, nothing made, a size missing). Before is an image's
// file, what the disk gets back, as the summary's saved is (sizeBefore)
function doneSizes(job) {
  if (job.section !== "compress") return null;
  let before = 0, after = 0;
  for (const row of job.rows) {
    if (row.state !== "ok") continue;
    if (!row.game || !row.outSize || !beforeOf(row)) return null;
    before += beforeOf(row);
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
  if (grows.includes(job.mode)) return t("nextGrow");
  if (job.mode === "send") return t("nextSend");
  if (job.mode === "original") return t("nextOriginal")(job.dest);
  const made = job.rows.filter(row => row.state === "ok" && row.out);
  const away = made.filter(row => !row.homebrew);
  if (!away.length) return t("nextFolder");
  const folders = [...new Set(away.map(row => row.out.replace(/[\\/]+[^\\/]+[\\/]*$/, "")))];
  return t("nextMove")(folders.length === 1 ? folders[0] : job.dest);
}

// a game as a report tells it: what it is (for an image, its kind, what a container holds and
// whether it is compressed), its sizes and how it ended, never its name, ID or path (the window
// leaves them out); a game of Details says how it ended in another list
function gameReport(row) {
  const game = row.game || {};
  let ran = row;
  if (!row.state) {
    for (const section of queues) {
      const other = state.lists[section].find(r => r.state && pathKey(r.path) === pathKey(row.path));
      if (other) { ran = other; break; }
    }
  }
  return { Kind: game.Kind || "", Inner: game.Inner || "", Compressed: !!game.Compressed, Packed: !!game.Packed, ImageSize: game.ImageSize || 0,
    OriginalSize: game.OriginalSize || 0, InstallSize: game.InstallSize || 0,
    P5zSize: game.P5zSize || 0, OutSize: ran.outSize || 0, Volumes: game.Volumes || 0, Files: game.Files || 0,
    HasZstdRuntime: !!game.HasZstdRuntime, HasRuntime: !!game.HasRuntime, State: ran.state || "", Detail: ran.detail || "", Out: ran.out || "",
    Completeness: completenessCounts(game.Completeness), ...dlcCounts(row) };
}
// how many DLC a game goes with (in Compress: the ones that went, once it is made or in the queue that
// runs) or carries (a .p5z), and how many of them have files of their own: counts only, never their
// content IDs
function dlcCounts(row) {
  const set = !!row.dlcSent && setRow(row);
  if (set && row.dlcSent.length) return { DLC: row.dlcSent.length, DLCData: Number(row.dlcSentData) || 0 };
  const going = set ? [] : dlcRowsOf(row);
  if (going.length) return { DLC: going.length, DLCData: going.filter(other => other.game.DLCData).length };
  const carried = (row.game && row.game.DLC) || [];
  return { DLC: carried.length, DLCData: carried.filter(d => d.Data).length };
}
// of what the engine said against the list of the original package's files, the counts alone
function completenessCounts(c) {
  if (!c) return null;
  const n = key => Number(c[key]) || 0;
  return { Checkable: !!c.Checkable, Reason: String(c.Reason || ""), Source: String(c.Source || ""), Listed: n("Listed"), Present: n("Present"),
    Missing: n("Missing"), SizeMismatch: n("SizeMismatch"), ExtraCount: n("ExtraCount"), ExtraBytes: n("ExtraBytes"), LeftoverCount: n("LeftoverCount"), LeftoverBytes: n("LeftoverBytes") };
}
function copyReport(rows, section, mode) {
  go.call("CopyReport", { Section: section || "", Mode: mode || "", Games: rows.map(gameReport) })
    .then(() => toast(t("reportCopied")), error => toast(message(error)));
}

// ---- Compare the levels on this PC, and Measure this PC --------------------------------------------

// the threads of this PC, as the window says them (the page alone knows the browser's guess)
const cpuThreads = () => (state.setup && state.setup.cpuThreads) || navigator.hardwareConcurrency || 1;
// a game of Compress whose levels can be compared: read, and not compressed already
const canCompare = row => row.section === "compress" && !!row.game && !row.game.Packed && !row.error && !isDlc(row.game);
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
  tellBar($(prefix + "-bar"), share);
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
    const savedCell = tr.querySelector(".saved-cell");
    savedCell.textContent = original ? size(saved) + " · " + isolate(t("smallerBy")(Math.round(saved / original * 100))) : "—";
    // a compressed image: also against its file, which takes less than the game in it
    const vs = vsImage(game, made);
    if (vs) {
      const line = document.createElement("small");
      line.className = "vs-image";
      line.textContent = isolate(vs);
      savedCell.append(line);
    }
    tr.querySelector(".time-cell").textContent = time(seconds || 0);
    tr.classList.toggle("current", Number(state.settings.Level) === level.Level);
    const use = tr.querySelector(".use");
    use.textContent = t("levelsUse");
    use.hidden = tool.running || !!tool.error;
    use.addEventListener("click", () => useLevel(level.Level));
    return tr;
  });
  $("levels-rows").replaceChildren(...rows);
  // what the savings are against, for a compressed image: the game in it, not its file
  $("levels-image").hidden = !compressedImage(game);
  $("levels-image").textContent = compressedImage(game) ? t("levelsImage")(imageExt(game), size(game.ImageSize), size(original)) : "";
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
  // a paused queue cannot be resumed once MaxPack closes: it says so
  $("close-title-text").textContent = t(state.paused ? "closeTitlePaused" : "closeTitle");
  $("close-text").textContent = t(state.paused ? "closeTextPaused" : "closeText");
  ask(dialog).then(answer => { if (answer === "close") go.call("ConfirmClose").catch(error => toast(message(error))); });
}
// the words of the question the window asks with the dialog of the system when the page cannot
// ask, in the language of the page
function tellCloseTexts() {
  go.call("TellLanguage", state.lang).catch(() => { });
  go.call("CloseTexts", t("closeQuestion"), t("closeYes"), t("closeNo")).catch(() => { });
}

// ---- the keyboard -------------------------------------------------------------------------------------

// the sections Ctrl+1 to Ctrl+5 open, in the order of the menu
const sectionKeys = ["compress", "decompress", "verify", "details", "rename"];
const mac = platform => /mac/i.test(platform || "");

// shortcutOf is what a key pressed does: "addFiles" (Ctrl+O), "addFolder" (Ctrl+Shift+O), "start"
// (Ctrl+Enter), "cancel" (Esc), "pause" (Ctrl+P), "section:<name>" (Ctrl+1 to Ctrl+5), or "" for nothing;
// ⌘ does what Ctrl does (macOS). The digits go by the key's place, whatever the keyboard writes there, and
// so do O and P on a layout without Latin letters
function shortcutOf(event) {
  const ctrl = event.ctrlKey || event.metaKey;
  if (event.key === "Escape") return ctrl || event.shiftKey || event.altKey ? "" : "cancel";
  if (!ctrl || event.altKey) return "";
  // the letter printed on the key (AZERTY, Dvorak); with a layout that writes no Latin letters (Russian,
  // Arabic), the letter of the key's place
  let key = String(event.key || "").toLowerCase();
  if (!/^[a-z]$/.test(key)) key = ((/^Key([A-Z])$/.exec(event.code || "") || [])[1] || key).toLowerCase();
  if (key === "o") return event.shiftKey ? "addFolder" : "addFiles";
  if (event.shiftKey) return "";
  if (event.key === "Enter") return "start";
  if (key === "p") return "pause";
  const digit = /^Digit([1-5])$/.exec(event.code || "") || (!event.code && /^([1-5])$/.exec(key));
  return digit ? "section:" + sectionKeys[Number(digit[1]) - 1] : "";
}

// the shortcuts as Settings › Keyboard lists them: [keys, what they do], with ⌘ on macOS
function keyRows(platform) {
  const m = mac(platform);
  const k = (key, shift) => (m ? (shift ? "⇧" : "") + "⌘" + (key === "Enter" ? "↩" : key) : "Ctrl+" + (shift ? "Shift+" : "") + key);
  return [[k("O"), t("addFiles")], [k("O", true), t("addFolder")], [k("Enter"), t("keyStart")], ["Esc", t("keyCancel")],
    [k("P"), t("keyPause")], [k("1") + " … " + k("5"), t("keySections")]];
}
function paintKeys() {
  const list = $("keys-list");
  list.replaceChildren(...keyRows(navigator.platform).flatMap(([keys, text]) => {
    const term = document.createElement("dt"), kbd = document.createElement("kbd"), what = document.createElement("dd");
    kbd.textContent = keys;
    term.append(kbd);
    what.textContent = text;
    return [term, what];
  }));
}

// a key pressed anywhere: nothing while a dialog is open (it has its own keys), and Esc not while typing
function onKey(event) {
  const action = shortcutOf(event);
  if (!action || document.querySelector("dialog[open]")) return;
  const typing = event.target && /^(INPUT|TEXTAREA|SELECT)$/.test(event.target.tagName || "");
  if (action === "cancel" && (typing || !running())) return;
  const works = state.section !== "settings";
  if (action === "addFiles" || action === "addFolder") {
    if (!works || !go.has()) return;
    event.preventDefault();
    go.call(action === "addFiles" ? "ChooseFiles" : "ChooseFolder", state.section).then(paths => addPaths(state.section, paths), error => toast(message(error)));
    return;
  }
  event.preventDefault();
  if (action === "start") { if (works && !$("start").hidden && !$("start").disabled) $("start").click(); return; }
  if (action === "cancel") { askCancel(); return; }
  if (action === "pause") { togglePause(); return; }
  setSection(action.slice("section:".length));
}

// cancelQueue stops the queue running (Cancel, or Esc once asked)
function cancelQueue() {
  if (!running() || state.cancelling) return;
  state.cancelling = true;
  paintStart();
  paintSummary();
  go.call("Cancel").catch(error => toast(message(error)));
}
// Esc asks first: the game at work stops and what it half made goes
async function askCancel() {
  if (!running() || state.cancelling) return;
  if (await ask($("cancel-dialog")) === "cancel-queue") cancelQueue();
}

// ---- the end of a queue, told while the window is not in front ---------------------------------------

// the short sound of the end, made here (two soft notes) for where the notice of the system makes none:
// the window asks for it (job:chime). The sound can only start once the page was touched (the browser's rule)
let audio = null;
function chime() {
  try {
    const Context = window.AudioContext || window.webkitAudioContext;
    if (!Context) return;
    audio = audio || new Context();
    if (audio.state === "suspended") audio.resume();
    const now = audio.currentTime;
    for (const [at, frequency] of [[0, 880], [0.14, 1318.5]]) {
      const tone = audio.createOscillator(), gain = audio.createGain();
      tone.type = "sine";
      tone.frequency.value = frequency;
      gain.gain.setValueAtTime(0.0001, now + at);
      gain.gain.exponentialRampToValueAtTime(0.18, now + at + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, now + at + 0.4);
      tone.connect(gain).connect(audio.destination);
      tone.start(now + at);
      tone.stop(now + at + 0.45);
    }
  } catch { }
}
// the sound is readied by the first touch of the page, so it can play later with the window behind
function readySound() {
  try {
    const Context = window.AudioContext || window.webkitAudioContext;
    if (Context && !audio) audio = new Context();
  } catch { }
}
// the window is told whether it is in front, for the notice of the end
function tellFront() {
  go.call("TellFront", !!(document.hasFocus && document.hasFocus()) && document.visibilityState !== "hidden").catch(() => { });
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
  $("pref-end-notice").addEventListener("change", () => keep({ QuietEnd: !$("pref-end-notice").checked }));
  // new versions: looked for once it is turned on (and kept), and taken with Update
  $("pref-update").addEventListener("change", async () => {
    const on = $("pref-update").checked;
    await keep({ UpdateCheck: on });
    paintUpdate();
    if (on) checkUpdate();
  });
  $("update-go").addEventListener("click", runUpdate);
  // the passcode of PS5 packages: kept once it is one (or empty, the default); the packages that
  // needed another one are read again with it
  $("pref-passcode").addEventListener("input", () => { $("passcode-bad").hidden = validPasscode($("pref-passcode").value.trim()); });
  $("pref-passcode").addEventListener("change", () => {
    const passcode = $("pref-passcode").value.trim();
    $("passcode-bad").hidden = validPasscode(passcode);
    if (!validPasscode(passcode) || passcode === (state.settings.Passcode || "")) return;
    keep({ Passcode: passcode });
    for (const list of Object.values(state.lists))
      for (const row of list)
        if (row.error && /--passcode/.test(row.error)) {
          row.error = "";
          row.game = null;
          paintRow(row);
          describe(row);
        }
  });
  // the console's address and code: kept once each one is one (or empty, none)
  for (const [id, bad, valid, key] of [["pref-console-ip", "console-ip-bad", validConsoleIp, "ConsoleIP"], ["pref-console-code", "console-code-bad", validConsoleCode, "ConsoleCode"]]) {
    const ok = value => value === "" || valid(value);
    $(id).addEventListener("input", () => { $(bad).hidden = ok($(id).value.trim()); });
    $(id).addEventListener("change", () => {
      const value = $(id).value.trim();
      $(bad).hidden = ok(value);
      if (ok(value) && value !== (state.settings[key] || "")) keep({ [key]: value });
    });
  }
  // the watched folder: chosen (and turned on), turned on and off, and whether what it makes goes to the console
  $("pref-watch-choose").addEventListener("click", () => go.call("ChooseWatchFolder").then(path => {
    if (path) { keep({ WatchFolder: path }); state.watch.error = ""; render(); }
  }, error => toast(message(error))));
  $("pref-watch").addEventListener("change", () => { keep({ WatchOn: $("pref-watch").checked }); state.watch.error = ""; render(); });
  $("pref-watch-send").addEventListener("change", () => keep({ WatchSend: $("pref-watch-send").checked }));
  $("pref-watch-existing").addEventListener("click", takeExisting);
  // the end card of Compress sends what it made
  $("done-send").addEventListener("click", () => { const job = state.job; $("done-dialog").close(); sendMade(job); });
  // what dump tools leave behind goes, unless Settings keeps it; the cards of Details say it
  $("pref-leftovers").addEventListener("change", () => { keep({ KeepLeftovers: !$("pref-leftovers").checked }); render(); });
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
  $("pause").addEventListener("click", togglePause);
  $("cancel").addEventListener("click", cancelQueue);
  document.addEventListener("keydown", onKey);
  for (const kind of ["pointerdown", "keydown"]) window.addEventListener(kind, readySound, { once: true });
  for (const kind of ["focus", "blur"]) window.addEventListener(kind, tellFront);
  document.addEventListener("visibilitychange", tellFront);
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
  go.on("job:paused", onPaused);
  go.on("job:chime", chime);
  go.on("close:ask", askClose);
  go.on("tool:progress", onToolProgress);
  go.on("tool:level", onToolLevel);
  go.on("tool:trial", onToolTrial);
  go.on("watch:found", onWatchFound);
  go.on("watch:skipped", event => { for (const path of (event && event.paths) || []) toast(t("watchSkipped")(baseName(path))); });
  go.on("watch:status", status => {
    const before = Number(state.watch.existing) || 0;
    state.watch = status || { folder: "", error: "", existing: 0 };
    // what was there when it was turned on is said once, and offered in Settings
    if (Number(state.watch.existing) > 0 && !before) toast(t("watchExistingToast")(Number(state.watch.existing)));
    if (state.section === "settings") paintSettings();
  });
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
  tellFront();
  applyTheme();
  const setup = state.setup;
  banner(setup && setup.coreError ? (setup.coreOtherVersion ? coreText(setup) : t("coreMissing") + " " + setup.coreError) : "");
  render();
  addSent(await go.call("PendingFiles").catch(() => null));
  loadExplorer();
  checkUpdate();
  offerLeftovers();
  // the watched folder, now that the page listens; what waits (no destination, no room) is tried again now and then
  go.call("StartWatch").then(status => { state.watch = Object.assign({ existing: 0 }, status || state.watch); }, () => {});
  setInterval(() => { if (watchedRows().some(row => !row.state && !row.watchHeld)) watchSoon(); }, 30000);
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
