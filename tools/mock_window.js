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
//   ?threads=24         the threads a measure found (the Threads setting); default all of them
//   ?nodest=1           no destination chosen yet (Measure this PC then measures no disk)
//   ?places=menu,files  the places of MaxPack in the Explorer already there (menu, sendto, files)
//   ?moved=sendto       places there for another copy of MaxPack
//   ?other=1            another program still opens .p5z files
//   ?portable=1         the portable version: Settings › Windows Explorer says it writes nothing
//   ?explorer=none      a system without the Explorer (macOS, Linux): no Windows Explorer in Settings
//   ?opened=1           MaxPack opened by a double click on a .p5z: its card in Details
//   ?handoff=details    a .p5z handed over by another MaxPack a moment after the page opens, as a
//                       double click while the window is open does (or verify, from the menu)
//   ?repair=1           Compress makes repair data (the RepairData setting), at ?percent=10 (5 by default)
//   ?nopar2=1           a build without maxpack-par2: the page offers no repair data, and says why
//   ?verify=1           three .p5z in Verify: one with repair data that verifies, one damaged with repair
//                       data of its own (Verify finds it damaged, then Repair), one with the set of another file
//   ?images=1           the images of ShadowMountPlus and the PS5 packages in Details (.exfat, .ffpkg,
//                       .ffpfsc, .pkg), to see their cards; one more .pkg needs another passcode until
//                       Settings › FPKG passcode has one
//   ?complete=1         Details with what each game says against the list of the original package's files: a
//                       complete dump, one that lacks three files (Compress warns of it too), one without a
//                       list, one whose list proves its leftovers are not the game's, and a .p5z of each kind;
//                       and, without that list, against the dump tool's (ampr_emu.index): a copy with every
//                       file it wrote, and a damaged one (Compress warns of it too); and a .pkg, complete
//                       against the table its builder made
//   ?dlc=1              Compress with two DLC of Starfall Odyssey (a package with files of its own and a
//                       license-only folder), the same package again and a DLC of a game not in the list;
//                       a game's Add DLC gives two more; Details with a .p5z made with two DLC
//   ?console=1          Settings has a console (an IP address and a pairing code): Send to the console, on
//                       the card of a .p5z and at the end of Compress, asks it for its drives and sends;
//                       ?console=refused: the console refuses the code
//   ?watch=1            Settings watches D:\incoming: two games of the list arrive in it a moment after the page
//                       opens (one whole, one that lacks files of its list) and Compress takes the whole one
//   ?grow=1             Details with a .p5z that grew twice (by DLC and an update) and one that never did:
//                       Add DLC… ends well, and Update with a new dump… stops because most of the game
//                       changed until Update anyway
// Start runs a queue made up here: each game packs and verifies in a few seconds (and gets its repair
// data), the games added meanwhile join it, and the card of the end shows; in Verify the damaged
// .p5z fails, and Repair and Add repair data run their own steps. Compare levels and Measure this PC tell made-up
// rows over a few seconds. Settings are kept in memory (they are lost
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
    DeleteOriginals: !!query.get("delete"), DeletePermanently: query.get("delete") === "good", LowPriority: !!query.get("low"), NameTemplate: query.get("template") || templates[0],
    Threads: Number(query.get("threads")) || 0, RepairData: !!query.get("repair"), RepairPercent: Number(query.get("percent")) || 5, KeepLeftovers: false,
    ConsoleIP: query.get("console") ? "192.168.1.20" : "", ConsoleCode: query.get("console") ? "482913" : "",
    WatchOn: !!query.get("watch"), WatchFolder: query.get("watch") ? "D:\\\\incoming" : "", WatchSend: false };
  if (query.get("nodest")) settings.LastDest = "";
  const builtIn = query.get("nobuiltin") ? {} : { builtInRuntime: "C:\\\\Users\\\\user\\\\AppData\\\\Roaming\\\\MaxPack\\\\ampr-emu-ng\\\\libSceAmpr.sprx", builtInRuntimeVersion: "v0.5.1-maxpack.2" };
  const games = {
    "D:\\\\dumps\\\\Starfall.Odyssey-PPSA10001": { TitleID: "PPSA10001", Name: "Starfall Odyssey", Version: "01.000.010", ContentID: "EP0001-PPSA10001_00-STARFALL00000000", Kind: "folder", OriginalSize: 8390000000, HasZstdRuntime: false, Icon: "", Packed: false },
    "D:\\\\dumps\\\\Iron.Harbor-PPSA10002.exfat": { TitleID: "PPSA10002", Name: "Iron Harbor", Version: "01.000.021", ContentID: "", Kind: "exfat", OriginalSize: 180400000000, HasZstdRuntime: true, Icon: "", Packed: false,
      ImageSize: 185300000000 },
    // a PFS container made by MkPFS, PFSC-compressed, with an exFAT image inside: its file is smaller than the game
    "D:\\\\dumps\\\\Quiet Tide [PPSA10005].ffpfsc": { TitleID: "PPSA10005", Name: "Quiet Tide", Version: "01.000.004", ContentID: "UP0005-PPSA10005_00-QUIETTIDE0000000", Kind: "pfsc", Inner: "exfat", Compressed: true,
      OriginalSize: 52300000000, HasZstdRuntime: false, Icon: "", Packed: false, ImageSize: 37900000000 },
    "D:\\\\dumps\\\\Neon Meridian-PPSA10003": { TitleID: "PPSA10003", Name: "Neon Meridian", Version: "01.000.000", ContentID: "UP0003-PPSA10003_00-NEONMERIDIAN0000", Kind: "folder", OriginalSize: 133590000000, HasZstdRuntime: false, Icon: "", Packed: false },
    "D:\\\\more\\\\Lost Signal-PPSA10004": { TitleID: "PPSA10004", Name: "Lost Signal", Version: "01.000.002", ContentID: "JP0004-PPSA10004_00-LOSTSIGNAL000000", Kind: "folder", OriginalSize: 21400000000, HasZstdRuntime: true, Icon: "", Packed: false },
    // a UFS2 image (ShadowMountPlus's mkufs2.sh)
    "D:\\\\more\\\\Glass Orbit.ffpkg": { TitleID: "PPSA10006", Name: "Glass Orbit", Version: "01.000.000", ContentID: "EP0006-PPSA10006_00-GLASSORBIT000000", Kind: "ufs2", OriginalSize: 14800000000, HasZstdRuntime: false, Icon: "", Packed: false,
      ImageSize: 15400000000 },
    // a PS5 debug package (FPKG), Kraken-compressed: its file is smaller than the game
    "D:\\\\pkgs\\\\Copper Vale-PPSA10007.pkg": { TitleID: "PPSA10007", Name: "Copper Vale", Version: "01.000.003", ContentID: "UP0007-PPSA10007_00-COPPERVALE000000", Kind: "fpkg", Compressed: true,
      OriginalSize: 41200000000, HasZstdRuntime: false, Icon: "", Packed: false, ImageSize: 33800000000 },
  };
  // a package made with another passcode: the engine does not open it until Settings has one
  const locked = "D:\\\\pkgs\\\\Ember Reach-PPSA10008.pkg";
  games[locked] = { TitleID: "PPSA10008", Name: "Ember Reach", Version: "01.000.000", ContentID: "UP0008-PPSA10008_00-EMBERREACH000000", Kind: "fpkg", Compressed: false,
    OriginalSize: 9300000000, HasZstdRuntime: false, Icon: "", Packed: false, ImageSize: 9500000000 };
  const lockedError = () => new Error("info of " + locked + ": exit status 1: " + locked + ": this package was made with another passcode; give it with --passcode");
  // what the engine says of each against the list of the original package's files it carries
  // (sce_sys/playgo-hash-table.dat): complete, three files missing, no list, leftovers it proves
  // are not the game's
  const counts = (listed, missing, extra, extraBytes, left, leftBytes) => ({ Checkable: true, Reason: "", Source: "playgo", Listed: listed, Present: listed - missing, Missing: missing, SizeMismatch: 0,
    ExtraCount: extra, ExtraBytes: extraBytes, LeftoverCount: left, LeftoverBytes: leftBytes });
  const noList = (left, leftBytes) => ({ Checkable: false, Reason: "no_table", Source: "", Listed: 0, Present: 0, Missing: 0, SizeMismatch: 0, ExtraCount: 0, ExtraBytes: 0, LeftoverCount: left, LeftoverBytes: leftBytes });
  // against the dump tool's list: every file it wrote, or a copy that lacks some and has one cut short
  const dumpList = (listed, missing, other, extra, extraBytes) => ({ Checkable: true, Reason: "", Source: "dump_index", Listed: listed, Present: listed - missing - other,
    Missing: missing, SizeMismatch: other, ExtraCount: extra, ExtraBytes: extraBytes, LeftoverCount: 0, LeftoverBytes: 0 });
  games["D:\\\\dumps\\\\Starfall.Odyssey-PPSA10001"].Completeness = counts(2165, 0, 0, 0, 0, 0);
  games["D:\\\\dumps\\\\Iron.Harbor-PPSA10002.exfat"].Completeness = counts(5120, 3, 2, 41000, 0, 0);
  games["D:\\\\dumps\\\\Neon Meridian-PPSA10003"].Completeness = noList(12, 3300000000);
  games["D:\\\\more\\\\Lost Signal-PPSA10004"].Completeness = counts(1880, 0, 15, 2900000000, 14, 2890000000);
  games["D:\\\\dumps\\\\Quiet Tide [PPSA10005].ffpfsc"].Completeness = dumpList(955, 0, 0, 1, 3759);
  games["D:\\\\more\\\\Glass Orbit.ffpkg"].Completeness = dumpList(2023, 2, 1, 0, 0);
  // a package carries the table its builder made of its own files
  games["D:\\\\pkgs\\\\Copper Vale-PPSA10007.pkg"].Completeness = counts(3410, 0, 0, 0, 0, 0);
  const first = Object.keys(games).slice(0, 4);
  const imagesShown = Object.keys(games).filter(file => /\\.(exfat|ffpkg|ffpfs|ffpfsc|pkg)$/i.test(file));
  // the .p5z of Verify (?verify=1): good, damaged, and one with the set of another file next to it
  const good = "E:\\\\Starfall Odyssey [PPSA10001].p5z", damaged = "E:\\\\Iron Harbor [PPSA10002].p5z", foreign = "E:\\\\Neon Meridian [PPSA10003].p5z";
  const repairs = {};
  const ownSet = size => { const bs = Math.ceil(size / 4000 / 4096) * 4096, blocks = Math.round(Math.ceil(size / bs) * (settings.RepairPercent || 5) / 100);
    return { state: "match", percent: settings.RepairPercent || 5, blocks, blockSize: bs, capacity: blocks * bs, bytes: blocks * (bs + 68) + 400000 }; };
  repairs[good] = ownSet(3618000000);
  repairs[damaged] = ownSet(74400000000);
  repairs[foreign] = { state: "other", percent: 0, blocks: 0, blockSize: 0, capacity: 0, bytes: 0 };
  const listOf = { [good]: counts(2165, 0, 0, 0, 0, 0), [damaged]: counts(5120, 3, 2, 41000, 0, 0), [foreign]: noList(0, 0) };
  let broken = true;
  // a .p5z, for Details, Verify and Decompress
  const p5z = "E:\\\\Starfall Odyssey [PPSA10001].p5z";
  games[p5z] = { TitleID: "PPSA10001", Name: "Starfall Odyssey", Version: "01.000.010", ContentID: "EP0001-PPSA10001_00-STARFALL00000000", Kind: "p5z", OriginalSize: 8390000000, HasZstdRuntime: true, Icon: "", Packed: true,
    InstallSize: 412000000, P5zSize: 3618000000, Files: 2165, PackedFiles: 2119, Volumes: 2, HasRuntime: true };
  games[damaged] = { TitleID: "PPSA10002", Name: "Iron Harbor", Version: "01.000.021", ContentID: "", Kind: "p5z", OriginalSize: 180400000000, HasZstdRuntime: true, Icon: "", Packed: true,
    InstallSize: 9100000000, P5zSize: 74400000000, Files: 5120, PackedFiles: 5011, Volumes: 19, HasRuntime: true };
  games[foreign] = { TitleID: "PPSA10003", Name: "Neon Meridian", Version: "01.000.000", ContentID: "UP0003-PPSA10003_00-NEONMERIDIAN0000", Kind: "p5z", OriginalSize: 133590000000, HasZstdRuntime: true, Icon: "", Packed: true,
    InstallSize: 6400000000, P5zSize: 61000000000, Files: 3811, PackedFiles: 3770, Volumes: 15, HasRuntime: true };
  for (const file of [good, damaged, foreign]) games[file].Completeness = listOf[file];
  // DLC (?dlc=1): of Starfall Odyssey, the same package once more, one of a game not in the list, two
  // more of Lost Signal for its Add DLC, and a .p5z made with two
  const dlc = (id, cid, name, data, kind, size) => ({ TitleID: id, Name: name, Version: "01.000.000", ContentID: cid, Kind: kind, OriginalSize: size, HasZstdRuntime: false,
    Icon: "", Packed: false, Content: "dlc", DLCData: data, DLC: [], ImageSize: kind === "fpkg" ? Math.round(size * 0.9) : 0 });
  const dlcShown = ["D:\\\\dlc\\\\Starfall Odyssey - Bonus Pack.pkg", "D:\\\\dlc\\\\Starfall Odyssey - Season Pass", "D:\\\\dlc\\\\copy\\\\Starfall Odyssey - Bonus Pack.pkg",
    "D:\\\\dlc\\\\Harbor Lights Expansion.pkg", "D:\\\\dlc\\\\Starfall Odyssey - Hand Made"];
  games[dlcShown[0]] = dlc("PPSA10001", "EP0001-PPSA10001_00-STARFALLBONUS001", "Starfall Odyssey: Bonus Pack", true, "fpkg", 2140000000);
  games[dlcShown[1]] = dlc("PPSA10001", "EP0001-PPSA10001_00-STARFALLSEASON01", "Starfall Odyssey: Season Pass", false, "folder", 48000);
  games[dlcShown[2]] = Object.assign({}, games[dlcShown[0]]);
  games[dlcShown[3]] = dlc("PPSA10009", "EP0009-PPSA10009_00-HARBORLIGHTS0001", "Harbor Lights Expansion", true, "fpkg", 5300000000);
  // a hand-made DLC folder whose param.json gives no content ID dlc_emu takes: an error of its own row
  games[dlcShown[4]] = dlc("PPSA10001", "", "Starfall Odyssey: Hand Made", true, "folder", 12000000);
  const dlcChosen = ["D:\\\\dlc\\\\Lost Signal - Chapter Two.pkg", "D:\\\\dlc\\\\Lost Signal - Soundtrack.pkg"];
  games[dlcChosen[0]] = dlc("PPSA10004", "JP0004-PPSA10004_00-LOSTSIGNALCHAP02", "Lost Signal: Chapter Two", true, "fpkg", 3900000000);
  games[dlcChosen[1]] = dlc("PPSA10004", "JP0004-PPSA10004_00-LOSTSIGNALMUSIC1", "Lost Signal: Soundtrack", false, "fpkg", 61000);
  const p5zDlc = "E:\\\\Starfall Odyssey [PPSA10001] with DLC.p5z";
  games[p5zDlc] = Object.assign({}, games[p5z], { OriginalSize: 10530000000, P5zSize: 5210000000, Files: 2299,
    DLC: [{ ContentID: "EP0001-PPSA10001_00-STARFALLBONUS001", Data: true, Folder: "addcont0", Size: 2140000000 },
      { ContentID: "EP0001-PPSA10001_00-STARFALLSEASON01", Data: false, Folder: "", Size: 0 }] });
  // a .p5z that grew (?grow=1): DLC added to it, then an update
  const p5zGrown = "E:\\\\Lost Signal [PPSA10004].p5z";
  games[p5zGrown] = { TitleID: "PPSA10004", Name: "Lost Signal", Version: "01.002.000", ContentID: "JP0004-PPSA10004_00-LOSTSIGNAL000000", Kind: "p5z", OriginalSize: 41200000000,
    HasZstdRuntime: true, Icon: "", Packed: true, InstallSize: 19400000000, P5zSize: 20100000000, Files: 3120, PackedFiles: 3075, Volumes: 6, HasRuntime: true,
    Grown: 2, UnusedBytes: 412000000, DLC: [{ ContentID: "JP0004-PPSA10004_00-LOSTSIGNALCHAP02", Data: true, Folder: "addcont0", Size: 3900000000 }] };
  const completeShown = ["D:\\\\dumps\\\\Starfall.Odyssey-PPSA10001", "D:\\\\dumps\\\\Iron.Harbor-PPSA10002.exfat", "D:\\\\dumps\\\\Neon Meridian-PPSA10003",
    "D:\\\\more\\\\Lost Signal-PPSA10004", "D:\\\\dumps\\\\Quiet Tide [PPSA10005].ffpfsc", "D:\\\\more\\\\Glass Orbit.ffpkg",
    "D:\\\\pkgs\\\\Copper Vale-PPSA10007.pkg", good, damaged, foreign];
  // MaxPack in the Explorer: each place "" (not there), "here" or "moved"
  const listed = name => (query.get(name) || "").split(",");
  const explorer = { available: query.get("explorer") !== "none", menu: "", sendTo: "", files: "", filesOther: !!query.get("other") };
  for (const [place, field] of [["menu", "menu"], ["sendto", "sendTo"], ["files", "files"]]) {
    if (listed("places").includes(place)) explorer[field] = "here";
    if (listed("moved").includes(place)) explorer[field] = "moved";
  }
  if (!explorer.available) Object.assign(explorer, { menu: "", sendTo: "", files: "", filesOther: false });
  const handoff = query.get("handoff") === "verify" ? "verify" : "details";
  if (query.get("handoff")) setTimeout(() => emit("files:add", { compress: [], verify: handoff === "verify" ? [p5z] : [], details: handoff === "details" ? [p5z] : [], show: handoff }), 1500);
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
      const i = q.at++, item = q.items[i], total = item.OriginalSize, mode = q.opts.Mode;
      // the steps of the game in this queue, as the window does them
      const grows = mode === "add_dlc" || mode === "update";
      const steps = mode === "send" ? ["sending", "verifying"] : mode === "verify" ? ["verifying"] : mode === "repair" ? ["repairing", "verifying"] : mode === "protect" ? ["verifying", "protecting"]
        : grows ? (q.opts.Repair ? ["growing", "verifying", "protecting"] : ["growing", "verifying"])
        : mode === "p5z" && q.opts.Repair ? ["packing", "verifying", "protecting"] : ["packing", "verifying"];
      // an update of most of the game stops at once, until the user says to go on
      if (mode === "update" && !item.Force) {
        emit("job:item", { i, state: "growing", detail: "" });
        setTimeout(() => { q.sum.Failed++; emit("job:item", { i, state: "error", detail: "mostly_changed" }); step(); }, 900);
        return;
      }
      let done = 0, at = 0;
      emit("job:item", { i, state: steps[0], detail: "" });
      const tick = setInterval(() => {
        done = Math.min(total, done + total / 12);
        emit("job:progress", { i, phase: steps[at] === "growing" ? (done < total / 2 ? "comparing" : "packing") : steps[at], done, total, rate: 310 * 1048576 });
        if (done < total) return;
        if (at + 1 < steps.length) { at++; done = 0; emit("job:item", { i, state: steps[at], detail: "" }); return; }
        clearInterval(tick);
        if (mode === "send") {
          q.sum.Done++;
          emit("job:item", { i, state: "ok", detail: "sent" });
          step();
          return;
        }
        if (grows) {
          games[item.Path] = Object.assign({}, games[item.Path], { Grown: (games[item.Path] && games[item.Path].Grown || 0) + 1, UnusedBytes: 512000000 });
          q.sum.Done++;
          if (q.opts.Repair) { q.sum.Protected++; q.sum.ProtectedSize += 40000000; }
          emit("job:item", { i, state: "ok", detail: q.opts.Repair ? "grown; repair_redone" : "grown" });
          step();
          return;
        }
        if (mode === "verify" || mode === "repair" || mode === "protect") {
          const bad = mode === "verify" && item.Path === damaged && broken;
          if (mode === "repair") { broken = false; q.sum.Repaired++; }
          if (mode === "protect") { repairs[item.Path] = repairs[item.Path] || ownSet(item.P5zSize || total); q.sum.Protected++; q.sum.ProtectedSize += repairs[item.Path].bytes; }
          if (bad) q.sum.Failed++; else q.sum.Done++;
          emit("job:item", { i, state: bad ? "error" : "ok", detail: bad ? "files_bad:3" : mode === "repair" ? "repaired" : "", repair: mode === "protect" ? repairs[item.Path].bytes : 0 });
          step();
          return;
        }
        const out = plan(item, q.opts).Out, made = Math.round(total * 0.42);
        const detail = q.opts.DeleteOriginals && !q.keep[i] ? (query.get("nobin") && !settings.DeletePermanently ? "original_kept_too_big" : settings.DeletePermanently ? "original_deleted" : "original_trashed") : "";
        // what it saved is against what it took before: an image's file, a folder the game
        const before = item.Kind === "folder" ? total : item.ImageSize || 0, saved = Math.max(0, before - made);
        q.sum.Done++; q.sum.Saved += saved;
        if (saved) q.sum.Top.push({ Name: item.Name, TitleID: item.TitleID, Saved: saved });
        if (detail === "original_trashed") { q.sum.Trashed++; q.sum.TrashedSize += total; }
        if (detail === "original_deleted") { q.sum.Deleted++; q.sum.DeletedSize += total; }
        if (detail.startsWith("original_kept")) q.sum.Kept++;
        const repair = q.opts.Mode === "p5z" && q.opts.Repair ? Math.round(made * q.opts.Repair / 100) + 400000 : 0;
        if (repair) { q.sum.Protected++; q.sum.ProtectedSize += repair; }
        emit("job:item", { i, state: "ok", detail, out, size: made, homebrew: q.opts.Mode === "folder", repair });
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
  // Compare the levels and Measure this PC: a few steps told as the engine tells them, then the result;
  // a cancel ends them as cancelled
  let tool = null;
  const toolRun = (steps, each, done) => new Promise((resolve, reject) => {
    const mine = { cancelled: false };
    tool = mine;
    let n = 0;
    const tick = () => {
      if (mine.cancelled) { tool = null; reject(new Error("cancelled")); return; }
      if (n >= steps.length) { tool = null; resolve(done()); return; }
      each(steps[n], n);
      n++;
      setTimeout(tick, 650);
    };
    setTimeout(tick, 300);
  });
  const App = {
    Setup: async () => Object.assign({ version: "1.0.2-mock", coreVersion: "maxpack 1.0.2", lang: settings.Lang || "en", theme: "system", coreError: "", builtInRuntime: "", builtInRuntimeVersion: "",
      logFile: "C:\\\\Users\\\\user\\\\AppData\\\\Roaming\\\\MaxPack\\\\maxpack.log", templates, cpuThreads: 32, explorer: explorer.available, portable: !!query.get("portable"),
      par2: query.get("nopar2") ? "" : "C:\\\\Program Files\\\\MaxPack\\\\maxpack-par2.exe", par2Version: query.get("nopar2") ? "" : "par2cmdline-turbo version 1.5.0",
      par2Error: query.get("nopar2") ? "maxpack-par2 not found: it should be next to the program, at C:\\\\Program Files\\\\MaxPack\\\\maxpack-par2.exe" : "" }, builtIn),
    ExplorerPlaces: async () => Object.assign({}, explorer),
    SetExplorerPlace: async (place, on, texts) => {
      if (query.get("portable")) throw new Error("the portable MaxPack writes nothing in Windows: install MaxPack to add it to the Explorer");
      const field = { menu: "menu", sendto: "sendTo", files: "files" }[place];
      explorer[field] = on ? "here" : "";
      if (place === "menu" && on) console.log("menu", texts);
      return Object.assign({}, explorer);
    },
    RelabelExplorerMenu: async texts => { console.log("menu relabelled", texts); },
    CompareLevels: async item => {
      const original = item.OriginalSize || 1e10, sample = Math.max(1 << 30, original * 0.02);
      const rows = [[3, 0.664, 0.671, 1480], [9, 0.627, 0.634, 610], [15, 0.604, 0.611, 205], [19, 0.588, 0.595, 118]].map(([level, p5z, folder, mb]) => {
        const packs = original / (mb * 1048576);
        return { Level: level, P5zSize: Math.round(original * p5z), FolderSize: Math.round(original * folder), Rate: mb * 1048576,
          P5zSeconds: Math.round(packs + 6), FolderSeconds: Math.round(packs + 1) };
      });
      return toolRun(rows, (row, n) => { emit("tool:progress", { done: n + 1, total: rows.length }); emit("tool:level", row); },
        () => ({ Rows: rows, OriginalSize: original, SampleBytes: sample, SampleRate: sample / original, Threads: settings.Threads || 32 }));
    },
    MeasurePC: async folder => {
      const trials = [[8, 93], [16, 171], [24, 236], [32, 241]].map(([threads, mb]) => ({ Threads: threads, Rate: mb * 1048576 }));
      return toolRun(trials.concat([null]), (trial, n) => { emit("tool:progress", { done: n + 1, total: trials.length + 1 }); if (trial) emit("tool:trial", trial); }, () => {
        settings.Threads = 24;
        return { CPUThreads: 32, Level: settings.Level, Trials: trials, Recommended: 24, Fastest: 32, DiskMeasured: !!folder, WriteRate: 1.9e9, ReadRate: 3.1e9, DiskError: "", Threads: 24 };
      });
    },
    CancelTool: async () => { if (tool) tool.cancelled = true; },
    GetSettings: async () => Object.assign({}, settings),
    SetSettings: async next => { Object.assign(settings, next); if (!validTemplate(settings.NameTemplate)) settings.NameTemplate = templates[0]; },
    PendingFiles: async () => (query.get("opened") ? { compress: [], verify: [], details: [p5z], show: "details" } : query.get("empty") ? null
      : query.get("images") ? { compress: first, verify: [], details: imagesShown, show: "details" }
      : query.get("dlc") ? { compress: [dlcShown[0], ...first, "D:\\\\more\\\\Lost Signal-PPSA10004", ...dlcShown.slice(1)], verify: [], details: [p5zDlc, dlcShown[0]], show: "compress" }
      : query.get("grow") ? { compress: [], verify: [], details: [p5zGrown, p5z], show: "details" }
      : query.get("complete") ? { compress: first.concat(["D:\\\\more\\\\Glass Orbit.ffpkg", "D:\\\\pkgs\\\\Copper Vale-PPSA10007.pkg"]), verify: [good, damaged, foreign], details: completeShown, show: "details" }
      : { compress: first, verify: query.get("verify") ? [good, damaged, foreign] : [], details: query.get("verify") ? [damaged] : [], show: query.get("verify") ? "verify" : "" }),
    RepairInfo: async file => repairs[file] || { state: /\\.p5z$/i.test(file) ? "none" : "", percent: 0, blocks: 0, blockSize: 0, capacity: 0, bytes: 0 },
    Expand: async paths => paths,
    Describe: async file => (file === locked && !settings.Passcode ? Promise.reject(lockedError()) : Object.assign({ Path: file }, games[file] || { TitleID: "PPSA00000", Name: file.split(/[\\\\/]/).pop(), Version: "01.000.000", Kind: "folder", OriginalSize: 1e9, Packed: false })),
    // what is not compressed has no packs; a .p5z or a compressed folder has its own numbers
    Details: async file => {
      if (file === locked && !settings.Passcode) throw lockedError();
      const game = games[file] || {}, packed = game.Kind === "p5z" || !!game.Packed;
      return Object.assign({ Path: file, InstallSize: packed ? 7900000000 : game.OriginalSize || 1e9, P5zSize: game.Kind === "p5z" ? 2400000000 : 0, Files: 2165,
        PackedFiles: packed ? 2119 : 0, Volumes: packed ? 1 : 0, HasRuntime: packed || !!game.HasZstdRuntime }, game, { Repair: repairs[file] || { state: /\\.p5z$/i.test(file) ? "none" : "" } });
    },
    ChooseFiles: async () => Object.keys(games).slice(4), ChooseFolder: async () => Object.keys(games), ChooseDest: async () => "E:\\\\", ChooseRuntime: async () => "",
    ChooseDLC: async () => dlcChosen,
    CloseTexts: async () => {}, TellLanguage: async () => {}, ConfirmClose: async () => {},
    Plan: async (section, items, opts) => items.map(item => Object.assign(plan(item, opts), opts.Mode === "repair" && !(repairs[item.Path] && repairs[item.Path].state === "match") ? { Block: "no_repair_data" } : {})),
    Space: async (section, items, opts) => (query.get("space") ? [{ Root: "E:\\\\", Need: 322390000000, Free: 118200000000 }] : []),
    StartWatch: async () => {
      if (settings.WatchOn) setTimeout(() => emit("watch:found", { paths: ["D:\\\\dumps\\\\Starfall.Odyssey-PPSA10001", "D:\\\\dumps\\\\Iron.Harbor-PPSA10002.exfat"] }), 1500);
      return { folder: settings.WatchOn ? settings.WatchFolder : "", error: "" };
    },
    WatchEnded: async () => {},
    WatchTakeExisting: async () => [],
    ChooseWatchFolder: async () => "D:\\\\incoming",
    CancelConsoleDrives: async () => {},
    DiscardOnConsole: async () => ({ had: 123456789, error: "", message: "" }),
    ConsoleDrives: async () => (query.get("console") === "refused" ? { drives: [], error: "bad_code", message: "" }
      : { drives: [{ id: "internal", free: 412e9, total: 825e9, fs: "ufs" }, { id: "usb0", free: 1.6e12, total: 2e12, fs: "exfat" }, { id: "usb1", free: 2e9, total: 64e9, fs: "exfat" }], error: "", message: "" }),
    Start: async (section, items, opts) => {
      queue = { section, items: items.slice(), opts, at: 0, removed: {}, keep: {}, started: Date.now(), sum: { Done: 0, Failed: 0, Skipped: 0, Cancelled: 0, NoZstdRuntime: 0, Saved: 0, Top: [], Duration: 0, Trashed: 0, Deleted: 0, Kept: 0, TrashedSize: 0, DeletedSize: 0,
        Protected: 0, Unprotected: 0, ProtectedSize: 0, Repaired: 0 } };
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
    Leftovers: async () => (query.get("leftovers") ? [{ Path: "E:\\\\Starfall Odyssey [PPSA10001].p5z.maxpack-tmp", Size: 3123456789 }, { Path: "E:\\\\homebrew\\\\PPSA10003.maxpack-tmp", Size: 41234567890 },
      { Path: "E:\\\\Iron Harbor [PPSA10002].p5z.maxpack-repair", Size: 148800000000, Restore: "E:\\\\Iron Harbor [PPSA10002].p5z", Keep: false }] : []),
    CleanLeftovers: async () => [],
    CopyReport: async request => "MaxPack report\\n" + JSON.stringify(request),
    OpenLink: async name => { console.log("open link", name); },
    NamePreview: async (template, item) => {
      const it = item || { Name: "Starfall Odyssey", TitleID: "PPSA10001", Version: "01.000.021", ContentID: "EP0001-PPSA10001_00-X" };
      const error = !validTemplate(template), use = error ? templates[0] : template;
      return { P5z: nameOf(it, use) + ".p5z", Original: nameOf(it, use), Folder: it.TitleID, Error: error };
    },
    RenamePlan: async items => items.map(item => {
      const ext = { exfat: ".exfat", ufs2: ".ffpkg", pfs: ".ffpfs", pfsc: ".ffpfsc", p5z: ".p5z" }[item.Kind] || "";
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
