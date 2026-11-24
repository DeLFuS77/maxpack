// SPDX-License-Identifier: GPL-3.0-or-later
// Development only: serves the page of the window (gui/frontend) with a stand-in for what the Go
// side gives it, to look at the page in a browser without building the window.
//   node tools/mock_window.js [port]          then open http://127.0.0.1:8765/
// Query parameters of the page:
//   ?system=pt-BR,en    the languages the "system" has (navigator.languages); default the browser's
//   ?lang=ja            a language already chosen in Settings (the Lang setting); default none
//   ?empty=1            no games in the list
// Settings are kept in memory (they are lost when the page is reloaded), so a choice made in
// Settings is looked at in the same page. Nothing here is part of MaxPack.
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
  const settings = { Lang: query.get("lang") || "", Theme: "system", Runtime: "", LastDest: "", Mode: "p5z", Level: 19 };
  const games = {
    "D:\\\\dumps\\\\Hollow.Knight.Silksong-PPSA12544": { TitleID: "PPSA12544", Name: "Hollow Knight: Silksong", Version: "01.000.010", Kind: "folder", OriginalSize: 8390000000, HasZstdRuntime: false, Icon: "", Packed: false },
    "D:\\\\dumps\\\\Black.Myth.Wukong-PPSA23226.exfat": { TitleID: "PPSA23226", Name: "Black Myth: Wukong", Version: "01.000.021", Kind: "image", OriginalSize: 180400000000, HasZstdRuntime: true, Icon: "", Packed: false },
    "D:\\\\dumps\\\\RESIDENT EVIL requiem-PPSA31246": { TitleID: "PPSA31246", Name: "RESIDENT EVIL requiem", Version: "01.000.000", Kind: "folder", OriginalSize: 133590000000, HasZstdRuntime: false, Icon: "", Packed: false },
  };
  const App = {
    Setup: async () => ({ version: "1.0.0-mock", coreVersion: "maxpack 1.0.0", lang: settings.Lang || "en", theme: "system", coreError: "" }),
    GetSettings: async () => Object.assign({}, settings),
    SetSettings: async next => { Object.assign(settings, next); },
    PendingFiles: async () => (query.get("empty") ? null : { compress: Object.keys(games), decompress: [] }),
    Expand: async paths => paths,
    Describe: async file => Object.assign({ Path: file }, games[file] || { TitleID: "PPSA00000", Name: file.split(/[\\\\/]/).pop(), Version: "01.000.000", Kind: "folder", OriginalSize: 1e9, Packed: false }),
    Details: async file => Object.assign({ Path: file, InstallSize: 7900000000, P5zSize: 2400000000, Files: 2165, PackedFiles: 2119, Volumes: 1, HasRuntime: true }, games[file] || {}),
    ChooseFiles: async () => Object.keys(games), ChooseFolder: async () => [], ChooseDest: async () => "E:\\\\", ChooseRuntime: async () => "",
    CloseTexts: async () => {}, TellLanguage: async () => {}, Cancel: async () => {}, ConfirmClose: async () => {},
  };
  window.go = { main: { App } };
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
