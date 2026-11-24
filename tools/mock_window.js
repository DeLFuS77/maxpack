// SPDX-License-Identifier: GPL-3.0-or-later
// Development only: serves the page of the window (gui/frontend) with a stand-in for what the Go
// side gives it, to look at the page in a browser without building the window.
//   node tools/mock_window.js [port]          then open http://127.0.0.1:8765/
// Query parameters of the page:
//   ?system=pt-BR,en    the languages the "system" has (navigator.languages); default the browser's
//   ?lang=ja            a language already chosen in Settings (the Lang setting); default none
//   ?empty=1            no games in the list
//   ?mode=folder        "Play from the USB" chosen (the Mode setting); default the installer (.p5z)
//   ?runtime=<path>     a libSceAmpr.sprx chosen in Settings (the Runtime setting); default none
//   ?nobuiltin=1        the window could not write the ampr-emu-ng that comes with MaxPack
//   ?delete=1           Settings deletes the originals once checked (?delete=good: for good)
//   ?nobin=1            the originals cannot go to the recycle bin (too big for it)
//   ?space=1            the queue does not fit in its destination
//   ?leftovers=1        a run cut short left two copies apart, offered as the page opens
//   ?low=1              low priority on
//   ?template=<t>       the template of the names (the NameTemplate setting)
//   ?section=rename     the section open at the start (compress, rename, details, settings...)
// Start runs a queue made up here: each game packs and verifies in a few seconds, the games added
// meanwhile join it, and the card of the end shows. Settings are kept in memory (they are lost
// when the page is reloaded). Nothing here is part of MaxPack.
"use strict";
const fs = require("fs"), http = require("http"), path = require("path");

const root = path.join(__dirname, "..", "gui", "frontend");
const port = Number(process.argv[2]) || 8765;
const types = { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".png": "image/png", ".svg": "image/svg+xml" };

// what runs before the scripts of the page: the system languages asked for, and the Go bindings
const mock = `
(() => {
  const query = new URLSearchParams(location.search);
  if (query.get("system")) {
    const languages = query.get("system").split(",");
    Object.defineProperty(navigator, "languages", { get: () => languages });
    Object.defineProperty(navigator, "language", { get: () => languages[0] });
  }
  const templates = ["{title} [{id}]", "{title} [{id}] [v{version}]", "{title} [{id}] [{region}] [v{version}]"];
  const settings = { Lang: query.get("lang") || "", Theme: "system", Runtime: query.get("runtime") || "", LastDest: "E:\\\\", Mode: query.get("mode") === "folder" ? "folder" : "p5z", Level: 19,
    DeleteOriginals: !!query.get("delete"), DeletePermanently: query.get("delete") === "good", LowPriority: !!query.get("low"), NameTemplate: query.get("template") || templates[0] };
  const builtIn = query.get("nobuiltin") ? {} : { builtInRuntime: "C:\\\\Users\\\\user\\\\AppData\\\\Roaming\\\\MaxPack\\\\ampr-emu-ng\\\\libSceAmpr.sprx", builtInRuntimeVersion: "v0.5.1-maxpack.1" };
  const games = {
    "D:\\\\dumps\\\\Starfall.Odyssey-PPSA10001": { TitleID: "PPSA10001", Name: "Starfall Odyssey", Version: "01.000.010", ContentID: "EP0001-PPSA10001_00-STARFALL00000000", Kind: "folder", OriginalSize: 8390000000, HasZstdRuntime: false, Icon: "", Packed: false },
    "D:\\\\dumps\\\\Iron.Harbor-PPSA10002.exfat": { TitleID: "PPSA10002", Name: "Iron Harbor", Version: "01.000.021", ContentID: "", Kind: "exfat", OriginalSize: 180400000000, HasZstdRuntime: true, Icon: "", Packed: false },
    "D:\\\\dumps\\\\Neon Meridian-PPSA10003": { TitleID: "PPSA10003", Name: "Neon Meridian", Version: "01.000.000", ContentID: "UP0003-PPSA10003_00-NEONMERIDIAN0000", Kind: "folder", OriginalSize: 133590000000, HasZstdRuntime: false, Icon: "", Packed: false },
    "D:\\\\more\\\\Lost Signal-PPSA10004": { TitleID: "PPSA10004", Name: "Lost Signal", Version: "01.000.002", ContentID: "JP0004-PPSA10004_00-LOSTSIGNAL000000", Kind: "folder", OriginalSize: 21400000000, HasZstdRuntime: true, Icon: "", Packed: false },
  };
  const first = Object.keys(games).slice(0, 3);
  // the events of the window, and a queue made up: each game packs, verifies and ends well
  const handlers = {};
  const emit = (event, data) => (handlers[event] || []).forEach(handler => handler(data));
  window.runtime = { EventsOn: (event, handler) => { (handlers[event] = handlers[event] || []).push(handler); }, OnFileDrop: () => {} };
  let queue = null;
  const region = item => ({ UP: "USA", EP: "EUR", JP: "JPN", HP: "ASIA", KP: "KOR" })[(item.ContentID || "").slice(0, 2)] || "";
  const nameOf = (item, template) => {
    const values = { title: item.Name, id: item.TitleID, version: item.Version, region: region(item) };
    template = template.replace(/\\s*\\[[^\\[\\]]*\\]/g, group => (group.match(/\\{(\\w+)\\}/g) || []).some(p => values[p.slice(1, -1)]) || !/\\{/.test(group) ? group : "");
    return template.replace(/\\{(\\w+)\\}/g, (all, name) => values[name] || "").replace(/[<>:"/\\\\|?*]/g, "").trim();
  };
  const validTemplate = t => !!t.trim() && /\\{(title|id)\\}/.test(t) && !(t.match(/\\{[^{}]*\\}/g) || []).some(p => !["{title}", "{id}", "{version}", "{region}"].includes(p));
  const plan = (item, opts) => {
    const out = opts.Mode === "folder" ? "E:\\\\homebrew\\\\" + item.TitleID : "E:\\\\" + nameOf(item, settings.NameTemplate) + ".p5z";
    const del = opts.DeleteOriginals;
    return { Item: item, Out: out, Tmp: out + ".maxpack-tmp", Replace: null, Warnings: [], Block: "", Args: [], ReplaceSize: 0,
      SourceSize: del ? item.OriginalSize : 0, NoBin: del && query.get("nobin") && !settings.DeletePermanently ? "too_big" : "" };
  };
  function runQueue() {
    const q = queue;
    const step = () => {
      if (q.cancelled) { q.items.forEach((item, i) => { if (!q.removed[i] && i >= q.at) emit("job:item", { i, state: "cancelled", detail: "" }); }); return finish(); }
      while (q.at < q.items.length && q.removed[q.at]) q.at++;
      if (q.at >= q.items.length) return finish();
      const i = q.at++, item = q.items[i], total = item.OriginalSize;
      let done = 0, phase = "packing";
      emit("job:item", { i, state: "packing", detail: "" });
      const tick = setInterval(() => {
        done = Math.min(total, done + total / 12);
        emit("job:progress", { i, phase, done, total, rate: 310 * 1048576 });
        if (done < total) return;
        if (phase === "packing") { phase = "verifying"; done = 0; emit("job:item", { i, state: "verifying", detail: "" }); return; }
        clearInterval(tick);
        const out = plan(item, q.opts).Out, made = Math.round(total * 0.42);
        const detail = q.opts.DeleteOriginals && !q.keep[i] ? (query.get("nobin") && !settings.DeletePermanently ? "original_kept_too_big" : settings.DeletePermanently ? "original_deleted" : "original_trashed") : "";
        q.sum.Done++; q.sum.Saved += total - made;
        q.sum.Top.push({ Name: item.Name, TitleID: item.TitleID, Saved: total - made });
        if (detail === "original_trashed") { q.sum.Trashed++; q.sum.TrashedSize += total; }
        if (detail === "original_deleted") { q.sum.Deleted++; q.sum.DeletedSize += total; }
        if (detail.startsWith("original_kept")) q.sum.Kept++;
        emit("job:item", { i, state: "ok", detail, out, size: made, homebrew: q.opts.Mode === "folder" });
        step();
      }, 260);
    };
    const finish = () => {
      q.sum.Top.sort((a, b) => b.Saved - a.Saved); q.sum.Top = q.sum.Top.slice(0, 3);
      q.sum.Duration = (Date.now() - q.started) * 1e6;
      queue = null;
      emit("job:done", q.sum);
    };
    q.items.forEach((item, i) => emit("job:item", { i, state: "waiting", detail: "" }));
    setTimeout(step, 300);
  }
  const App = {
    Setup: async () => Object.assign({ version: "1.0.2-mock", coreVersion: "maxpack 1.0.2", lang: settings.Lang || "en", theme: "system", coreError: "", builtInRuntime: "", builtInRuntimeVersion: "",
      logFile: "C:\\\\Users\\\\user\\\\AppData\\\\Roaming\\\\MaxPack\\\\maxpack.log", templates }, builtIn),
    GetSettings: async () => Object.assign({}, settings),
    SetSettings: async next => { Object.assign(settings, next); if (!validTemplate(settings.NameTemplate)) settings.NameTemplate = templates[0]; },
    PendingFiles: async () => (query.get("empty") ? null : { compress: first, decompress: [] }),
    Expand: async paths => paths,
    Describe: async file => Object.assign({ Path: file }, games[file] || { TitleID: "PPSA00000", Name: file.split(/[\\\\/]/).pop(), Version: "01.000.000", Kind: "folder", OriginalSize: 1e9, Packed: false }),
    Details: async file => Object.assign({ Path: file, InstallSize: 7900000000, P5zSize: 2400000000, Files: 2165, PackedFiles: 2119, Volumes: 1, HasRuntime: true }, games[file] || {}),
    ChooseFiles: async () => Object.keys(games).slice(3), ChooseFolder: async () => Object.keys(games), ChooseDest: async () => "E:\\\\", ChooseRuntime: async () => "",
    CloseTexts: async () => {}, TellLanguage: async () => {}, ConfirmClose: async () => {},
    Plan: async (section, items, opts) => items.map(item => plan(item, opts)),
    Space: async (section, items, opts) => (query.get("space") ? [{ Root: "E:\\\\", Need: 322390000000, Free: 118200000000 }] : []),
    Start: async (section, items, opts) => {
      queue = { section, items: items.slice(), opts, at: 0, removed: {}, keep: {}, started: Date.now(), sum: { Done: 0, Failed: 0, Skipped: 0, Cancelled: 0, NoZstdRuntime: 0, Saved: 0, Top: [], Duration: 0, Trashed: 0, Deleted: 0, Kept: 0, TrashedSize: 0, DeletedSize: 0 } };
      runQueue();
    },
    Append: async (section, items, replace, keepOriginals) => {
      if (!queue || queue.section !== section) throw new Error("no queue of this section is running");
      const at = queue.items.length;
      items.forEach((item, n) => { queue.items.push(item); queue.keep[at + n] = keepOriginals; setTimeout(() => emit("job:item", { i: at + n, state: "queued", detail: "" }), 0); });
      return at;
    },
    Unqueue: async (section, i) => { if (!queue || i < queue.at) return false; queue.removed[i] = true; return true; },
    Cancel: async () => { if (queue) queue.cancelled = true; },
    Leftovers: async () => (query.get("leftovers") ? [{ Path: "E:\\\\Starfall Odyssey [PPSA10001].p5z.maxpack-tmp", Size: 3123456789 }, { Path: "E:\\\\homebrew\\\\PPSA10003.maxpack-tmp", Size: 41234567890 }] : []),
    CleanLeftovers: async () => [],
    CopyReport: async request => "MaxPack report\\n" + JSON.stringify(request),
    OpenLink: async name => { console.log("open link", name); },
    NamePreview: async (template, item) => {
      const it = item || { Name: "Starfall Odyssey", TitleID: "PPSA10001", Version: "01.000.021", ContentID: "EP0001-PPSA10001_00-X" };
      const error = !validTemplate(template), use = error ? templates[0] : template;
      return { P5z: nameOf(it, use) + ".p5z", Original: nameOf(it, use), Folder: it.TitleID, Error: error };
    },
    RenamePlan: async items => items.map(item => {
      const ext = item.Kind === "exfat" ? ".exfat" : item.Kind === "p5z" ? ".p5z" : "";
      const to = item.Path.replace(/[^\\\\/]+$/, "") + nameOf(item, settings.NameTemplate) + ext;
      return { Item: item, From: item.Path, To: to, Block: to === item.Path ? "same_name" : item.TitleID === "PPSA10004" ? "name_taken" : "" };
    }),
    Rename: async items => (await App.RenamePlan(items)).map(p => ({ From: p.From, To: p.To, State: p.Block ? "skipped" : "ok", Detail: p.Block })),
  };
  window.go = { main: { App } };
  if (query.get("section")) document.addEventListener("DOMContentLoaded", () => setTimeout(() => { const b = document.querySelector('nav button[data-section="' + query.get("section") + '"]'); if (b) b.click(); }, 50));
})();
`;

http.createServer((request, response) => {
  const url = new URL(request.url, "http://localhost");
  if (url.pathname === "/__mock.js") { response.writeHead(200, { "content-type": types[".js"] }); response.end(mock); return; }
  const name = url.pathname === "/" ? "index.html" : decodeURIComponent(url.pathname.slice(1));
  const file = path.join(root, name);
  if (!file.startsWith(root + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) { response.writeHead(404); response.end("not found"); return; }
  let body = fs.readFileSync(file);
  if (name === "index.html") body = Buffer.from(body.toString("utf8").replace('<script src="i18n/base.js">', '<script src="__mock.js"></script>\n<script src="i18n/base.js">'));
  response.writeHead(200, { "content-type": types[path.extname(file)] || "application/octet-stream", "cache-control": "no-store" });
  response.end(body);
}).listen(port, "127.0.0.1", () => console.log("http://127.0.0.1:" + port + "/"));
