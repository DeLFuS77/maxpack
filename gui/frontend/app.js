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
const working = ["packing", "verifying", "unpacking"];
// the shortcuts of the level, as in NXPress: the level each one sets, and the ones it covers
const presets = [[3, 1, 6], [12, 7, 14], [19, 15, 22]];
// what each section takes, as the empty list says it: [type, its text or none]
const takes = {
  compress: [["", "typeDump"], [".exfat", ""]],
  decompress: [[".p5z", ""]],
  verify: [[".p5z", ""], ["", "typePacked"]],
  details: [["", "typeDump"], [".exfat", ""], [".p5z", ""]],
};
const defaults = { Lang: "es", Theme: "system", Runtime: "", LastDest: "", Mode: "p5z", Level: 19 };

const $ = id => document.getElementById(id);
const windows = /win/i.test(navigator.platform || navigator.userAgent || "");

const state = {
  section: "compress",
  lang: /^es\b/i.test(navigator.language || "") ? "es" : "en",
  // the settings as GetSettings gave them, with the changes made here: SetSettings takes them whole
  settings: Object.assign({}, defaults),
  setup: null,
  unpackTo: load("unpackTo", "original") === "console" ? "console" : "original",
  lists: { compress: [], decompress: [], verify: [], details: [] },
  // the queue running, or the last one: its section, mode and games in the order of the queue
  job: null,
  busy: false,
  cancelling: false,
  // the runtime of ampr-emu-ng is asked once: after "Seguir sin él" the window does not insist
  askedRuntime: false,
};

// whether a queue is running: the window took it, and has not said its end yet
const running = () => !!(state.busy && state.job && state.job.running && !state.job.summary);

// a text of the language chosen, else the English one
const t = key => (texts[state.lang] && texts[state.lang][key] !== undefined ? texts[state.lang] : texts.en)[key];

function load(name, fallback) {
  try { const value = localStorage.getItem("maxpack." + name); return value === null ? fallback : JSON.parse(value); } catch { return fallback; }
}
function save(name, value) {
  try { localStorage.setItem("maxpack." + name, JSON.stringify(value)); } catch { }
}

// a size as NXPress writes it, with the decimal mark of the language (8,4 GB in Spanish); the
// notice at the end of a queue writes it the same way (sizeText in notify.go)
const sizeFormats = {};
function size(bytes) {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = Number(bytes) || 0, unit = 0;
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit++; }
  const digits = unit ? 1 : 0, key = state.lang + digits;
  sizeFormats[key] = sizeFormats[key] || new Intl.NumberFormat(state.lang, { minimumFractionDigits: digits, maximumFractionDigits: digits, useGrouping: false });
  return sizeFormats[key].format(value) + " " + units[unit];
}
function time(seconds) {
  if (seconds < 60) return seconds + " s";
  const m = Math.floor(seconds / 60), s = seconds % 60;
  return m < 60 ? `${m} min ${s} s` : `${Math.floor(m / 60)} h ${m % 60} min`;
}
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
  const row = { path, section, game: null, error: "", state: "", detail: "", step: "", phase: "", offset: 0, done: 0, total: 0, out: "", outSize: 0, homebrew: false, warnings: [], replacing: false, tr };
  tr.querySelector(".remove button").addEventListener("click", () => removeRow(row));
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
      .then(game => { row.game = game; }, error => { row.error = message(error); })
      .finally(() => { describing--; paintRow(row); paintSummary(); paintStart(); nextDescribe(); });
  }
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

// the section open shows what came; when it took nothing and another did, that one opens
function showAdded(compress, decompress) {
  const took = { compress, decompress };
  if (took[state.section]) return;
  if (compress) setSection("compress");
  else if (decompress) setSection("decompress");
  if (compress + decompress) toast(t("added")(compress + decompress));
}

// files that reach the window: given to MaxPack, or to another MaxPack while this one was open
async function addSent(files) {
  if (!files) return;
  const compress = await expandAndAdd(files.compress, "compress");
  const decompress = await expandAndAdd(files.decompress, "decompress");
  showAdded(compress, decompress);
}

// files dropped on the window: to the section open in Verificar and Detalles; else to
// Comprimir or Descomprimir by what they are, as the files given to MaxPack
async function dropped(paths) {
  showDrop(false);
  if (state.section === "verify" || state.section === "details") {
    if (!await expandAndAdd(paths, state.section)) toast(t("dropNothing"));
    return;
  }
  const compress = await expandAndAdd(paths, "compress");
  const decompress = await expandAndAdd(paths, "decompress");
  if (!compress && !decompress) toast(t("dropNothing"));
  else showAdded(compress, decompress);
}

function removeRow(row) {
  if (running() && state.job.rows.includes(row)) return;
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

// the steps of a game in a queue of a mode, each a part of its bar
function stepsOf(row, mode) {
  if (mode === "p5z" || mode === "folder") return ["packing", "verifying"];
  if (mode === "verify") return ["verifying"];
  if (mode === "console" && row.replacing) return ["unpacking", "verifying"];
  return ["unpacking"];
}
// how far the step at work is, from 0 to 1: the phases of a step follow one another (copying the
// loose files, then packing the rest) and their bytes add up to the size of the game
function stepShare(row) {
  if (!row.total) return 0;
  const whole = Math.max(row.offset + row.total, row.step === "verifying" || !row.game ? 0 : row.game.OriginalSize || 0);
  return Math.min(1, (row.offset + row.done) / whole);
}
function rowShare(row, mode) {
  if (ended.includes(row.state)) return 1;
  const steps = stepsOf(row, mode), index = steps.indexOf(row.step);
  return index < 0 ? 0 : (index + stepShare(row)) / steps.length;
}
// how far the queue is (never going back), and the seconds left at the speed of the engine
function progressOf(job) {
  let whole = 0, done = 0;
  for (const row of job.rows) {
    const weight = stepsOf(row, job.mode).length * Math.max(1, row.game ? row.game.OriginalSize : 1);
    whole += weight;
    done += rowShare(row, job.mode) * weight;
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
  const lacks = row.section === "compress" && !game.Packed && !game.HasZstdRuntime && !state.settings.Runtime;
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
  fact(facts, t("factPacked"), game.Packed ? t("yes") : t("no"));
  if (game.Packed) fact(facts, t("factPacks"), t("packsOf")(game.PackedFiles || 0, game.Volumes || 0));
  fact(facts, "ampr-emu-ng", game.HasZstdRuntime ? t("runtimeZstd") : game.HasRuntime ? t("runtimeOld") : t("runtimeNone"));
  const where = document.createElement("dd");
  where.className = "path-fact";
  where.textContent = game.Path;
  const label = document.createElement("dt");
  label.textContent = t("factPath");
  facts.append(label, where);
}

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
  if (!sizes.hidden) sizes.textContent = size(game.OriginalSize) + " → " + size(row.outSize);
  where.hidden = !row.out;
  if (row.out) where.textContent = t("madeAt")(row.out);

  const warnings = rowWarnings(row), warn = tr.querySelector(".list-warn");
  warn.hidden = !warnings.length;
  warn.textContent = warnings.join(" · ");

  const note = tr.querySelector(".note");
  const said = row.error || (row.detail && row.detail !== "cancelled" ? detailText(row.detail) : "");
  note.hidden = !said;
  note.textContent = said;
  note.className = "note " + (row.error || row.state === "error" ? "bad" : "warn");
  paintFacts(row);
  paintState(row);
}

function paintState(row) {
  const tr = row.tr, job = state.job;
  const badge = tr.querySelector(".badge");
  badge.hidden = !row.state;
  badge.className = "badge " + (ended.includes(row.state) ? row.state : working.includes(row.state) ? "work" : "waiting");
  badge.textContent = row.state ? t("state_" + row.state) : "";
  // the bar: a part for each step, the ones done full, the one at work filling up
  const segs = tr.querySelector(".segs"), step = tr.querySelector(".step");
  const active = running() && job.rows.includes(row) && working.includes(row.state);
  segs.hidden = !active;
  step.textContent = "";
  if (active) {
    const steps = stepsOf(row, job.mode), index = steps.indexOf(row.step), share = stepShare(row);
    if (segs.children.length !== steps.length) segs.replaceChildren(...steps.map(() => $("seg-template").content.firstElementChild.cloneNode(true)));
    [...segs.children].forEach((seg, n) => {
      seg.className = "seg " + (steps[n] === "verifying" ? "tone-check" : "tone-work");
      seg.firstElementChild.style.width = (n < index ? 100 : n === index ? share * 100 : 0) + "%";
    });
    const phase = row.phase ? t("phase_" + row.phase) : "";
    step.textContent = [phase, t("percent")(Math.floor(rowShare(row, job.mode) * 100))].filter(Boolean).join(" · ");
  }
  // what it saved, once made
  const saved = tr.querySelector(".saved"), game = row.game;
  const smaller = row.section === "compress" && row.state === "ok" && game && row.outSize && row.outSize < game.OriginalSize;
  saved.textContent = smaller ? t("smallerBy")(Math.round((1 - row.outSize / game.OriginalSize) * 100)) : "";
  tr.querySelector(".remove button").disabled = running() && job.rows.includes(row);
}

// ---- the page ----------------------------------------------------------------------------------

function setSection(section) {
  state.section = section;
  render();
}

function paintTexts() {
  document.documentElement.lang = state.lang;
  document.querySelectorAll("[data-t]").forEach(element => { element.textContent = t(element.dataset.t); });
  document.querySelectorAll("[data-t-title]").forEach(element => { element.title = t(element.dataset.tTitle); });
  document.querySelectorAll("[data-t-label]").forEach(element => { element.setAttribute("aria-label", t(element.dataset.tLabel)); });
}

function paintNav() {
  for (const button of document.querySelectorAll("nav button[data-section]")) {
    const on = button.dataset.section === state.section;
    button.classList.toggle("on", on);
    if (on) button.setAttribute("aria-current", "page"); else button.removeAttribute("aria-current");
  }
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
  const runtime = $("runtime-state");
  runtime.classList.toggle("ok", !!s.Runtime);
  runtime.classList.toggle("bad", !s.Runtime);
  runtime.textContent = s.Runtime ? s.Runtime : t("runtimeUnset");
  runtime.title = s.Runtime || "";
  $("choose-runtime").textContent = s.Runtime ? t("runtimeChange") : t("runtimeChoose");
  for (const element of document.querySelectorAll("#options input, #options button")) element.disabled = state.busy;
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
  for (const button of document.querySelectorAll("button[data-lang]")) {
    button.classList.toggle("on", button.dataset.lang === state.lang);
    button.setAttribute("aria-checked", button.dataset.lang === state.lang);
  }
  for (const button of document.querySelectorAll("button[data-theme]")) {
    button.classList.toggle("on", button.dataset.theme === s.Theme);
    button.setAttribute("aria-checked", button.dataset.theme === s.Theme);
  }
  $("pref-runtime").textContent = s.Runtime || t("runtimeUnset");
  $("pref-runtime").classList.toggle("bad", !s.Runtime);
  $("pref-runtime-clear").hidden = !s.Runtime;
  $("about-version").textContent = setup ? setup.version : "—";
  $("about-core").textContent = !setup ? "—" : coreText(setup) || setup.coreVersion;
  $("about-core").classList.toggle("bad", !!(setup && setup.coreError));
  const tile = $("settings-tile");
  if (!tile.firstChild) tile.append(document.querySelector('nav button[data-section="settings"] svg').cloneNode(true));
}

function paintStart() {
  const section = state.section, list = state.lists[section] || [];
  const start = $("start");
  start.hidden = !queues.includes(section) || running();
  start.textContent = queues.includes(section) ? t(section) : "";
  start.disabled = state.busy || !go.has() || !!(state.setup && state.setup.coreError) || !list.some(row => row.game);
  $("cancel").hidden = !running();
  $("cancel").disabled = state.cancelling;
  for (const id of ["add-files", "add-folder", "empty-files", "empty-folder"]) $(id).disabled = !go.has();
  $("clear").disabled = state.busy || list.length === 0;
}

function paintSummary() {
  const job = state.job, bar = $("total-bar");
  if (running()) {
    const { share, left } = progressOf(job);
    const finished = job.rows.filter(row => ended.includes(row.state)).length;
    $("summary-text").textContent = t("working")(finished, job.rows.length, Math.floor(share * 100));
    bar.hidden = false;
    bar.firstElementChild.style.width = share * 100 + "%";
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

// the games of the section that can go: read, not done yet, and of what the section works on
function readyRows(section) {
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
    // ampr-emu-ng, once: chosen and kept in the settings, or not given, and then said clearly
    const lacking = plans.map((p, n) => n).filter(n => (plans[n].Warnings || []).includes("needs_runtime"));
    if (section === "compress" && lacking.length && !opts.Runtime && !state.askedRuntime) {
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
    // what cannot go to this destination
    const blocked = plans.map((p, n) => n).filter(n => plans[n].Block);
    if (blocked.length) {
      $("block-list").replaceChildren(...blocked.map(n => line(names(n), detailText(plans[n].Block))));
      $("block-skip").hidden = blocked.length === plans.length;
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

async function startQueue() {
  const section = state.section;
  if (state.busy || !queues.includes(section)) return;
  const rows = readyRows(section);
  if (!rows.length) { toast(t("nothingToDo")); return; }
  const opts = { Mode: modeOf(section), Dest: "", Level: Number(state.settings.Level) || 19, Runtime: state.settings.Runtime || "", ContinueFat32: false, Replace: [] };
  if (writes.includes(section)) {
    opts.Dest = state.settings.LastDest || await chooseDest();
    if (!opts.Dest) return;
  }
  // nothing else starts while the questions are asked; once the window takes the queue, the page
  // stays busy until job:done (which may even come before Start answers)
  state.busy = true;
  render();
  let job = null, started = false;
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
    // what to know before it starts: it may not fit, two games to one place
    const told = w => w === "may_not_fit" || w === "duplicate_target" || w === "no_title_id";
    const warned = going.filter(n => (plans[n].Warnings || []).some(told));
    if (warned.length) {
      $("warn-list").replaceChildren(...warned.map(n => line(plans[n].Item.Name, plans[n].Warnings.filter(told).map(w => t("warn_" + w)).join(" · "))));
      if (await ask($("warn-dialog")) !== "go") return;
    }
    // the games left out are said in the list; the others wait for their turn
    for (const [row, detail] of skipped) Object.assign(row, { state: "skipped", detail, out: "", outSize: 0, warnings: [] });
    job = { section, mode: opts.Mode, dest: opts.Dest, runtime: opts.Runtime, rows: going.map(n => rows[n]), shown: 0, rate: 0 };
    for (const n of going) {
      Object.assign(rows[n], { state: "waiting", detail: "", step: "", phase: "", offset: 0, done: 0, total: 0, out: "", outSize: 0,
        warnings: plans[n].Warnings || [], replacing: (plans[n].Replace || []).length > 0 });
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
}

// ---- what the window tells of a queue -----------------------------------------------------------

function onItem(event) {
  const job = state.job, row = job && job.rows[event.i];
  if (!row) return;
  if (working.includes(event.state) && event.state !== row.step) Object.assign(row, { step: event.state, phase: "", offset: 0, done: 0, total: 0 });
  row.state = event.state;
  row.detail = event.detail || "";
  if (event.out) Object.assign(row, { out: event.out, outSize: event.size || 0, homebrew: !!event.homebrew });
  paintRow(row);
  paintSummary();
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
  $("done-stats").textContent = summaryLine(summary) + " · " + time(seconds);
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
  // the games made without a runtime that reads zstd are said again, at the end: none was given
  // (the dump has none and Ajustes neither), or the one of Ajustes does not read zstd
  const lacking = job.section === "compress" ? summary.NoZstdRuntime || 0 : 0;
  $("done-runtime").hidden = !lacking;
  $("done-runtime").textContent = lacking ? (job.runtime ? t("doneRuntimeWrong")(lacking) : t("doneNoRuntime")(lacking)) : "";
  const dialog = $("done-dialog");
  dialog.classList.toggle("failed", !summary.Done);
  if (!dialog.open) dialog.showModal();
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
  $("start").addEventListener("click", startQueue);
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
  // the languages of i18n/, each by its own name
  $("languages").replaceChildren(...languages.map(([code, name]) => {
    const button = document.createElement("button");
    button.setAttribute("role", "radio");
    button.dataset.lang = code;
    button.textContent = name;
    button.addEventListener("click", () => { state.lang = code; keep({ Lang: code }); tellCloseTexts(); render(); });
    return button;
  }));
  for (const button of document.querySelectorAll("button[data-theme]")) button.addEventListener("click", () => { keep({ Theme: button.dataset.theme }); applyTheme(); render(); });
  watchDrops();
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
  go.onDrop((x, y, paths) => dropped(paths));
  try {
    state.setup = await go.call("Setup");
    state.settings = Object.assign({}, defaults, await go.call("GetSettings"));
  } catch (error) {
    banner(message(error));
  }
  state.lang = state.settings.Lang === "en" ? "en" : "es";
  tellCloseTexts();
  applyTheme();
  const setup = state.setup;
  banner(setup && setup.coreError ? (setup.coreOtherVersion ? coreText(setup) : t("coreMissing") + " " + setup.coreError) : "");
  render();
  addSent(await go.call("PendingFiles").catch(() => null));
}

if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", main);
else main();
