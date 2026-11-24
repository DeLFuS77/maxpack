#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Genera THIRD_PARTY_NOTICES: zstd y zlib (de los submódulos) y los módulos de Go de gui/go.mod.

  python tools/gen_third_party.py            # reescribe THIRD_PARTY_NOTICES
  python tools/gen_third_party.py --check    # falla si no coincide con lo que saldría ahora

Hace falta Go y los módulos descargados (`go mod download` en gui/). De cada módulo de go.mod sale su
versión y el texto de su licencia, leído de su carpeta; los que el programa enlaza de verdad (los
paquetes que compilan en Windows, Linux o macOS, con las etiquetas de producción) se marcan.
"""
import json
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
GUI = os.path.join(ROOT, "gui")
OUT = os.path.join(ROOT, "THIRD_PARTY_NOTICES")

# Licencia de cada módulo (SPDX), comprobada a mano contra el texto de su LICENSE; el generador
# falla si aparece un módulo nuevo sin entrada, para que alguien la mire.
LICENSES = {
    "git.sr.ht/~jackmordaunt/go-toast/v2": "Unlicense OR MIT",
    "github.com/bep/debounce": "MIT",
    "github.com/gabriel-vasile/mimetype": "MIT",
    "github.com/go-ole/go-ole": "MIT",
    "github.com/godbus/dbus/v5": "BSD-2-Clause",
    "github.com/google/uuid": "BSD-3-Clause",
    "github.com/gorilla/websocket": "BSD-2-Clause",
    "github.com/jchv/go-winloader": "ISC",
    "github.com/labstack/echo/v4": "MIT",
    "github.com/labstack/gommon": "MIT",
    "github.com/leaanthony/go-ansi-parser": "MIT",
    "github.com/leaanthony/gosod": "MIT",
    "github.com/leaanthony/slicer": "MIT",
    "github.com/leaanthony/u": "MIT",
    "github.com/mattn/go-colorable": "MIT",
    "github.com/mattn/go-isatty": "MIT",
    "github.com/pkg/browser": "BSD-2-Clause",
    "github.com/pkg/errors": "BSD-2-Clause",
    "github.com/rivo/uniseg": "MIT",
    "github.com/samber/lo": "MIT",
    "github.com/tkrajina/go-reflector": "Apache-2.0",
    "github.com/valyala/bytebufferpool": "MIT",
    "github.com/valyala/fasttemplate": "MIT",
    "github.com/wailsapp/go-webview2": "MIT",
    "github.com/wailsapp/wails/v2": "MIT",
    "golang.org/x/crypto": "BSD-3-Clause",
    "golang.org/x/net": "BSD-3-Clause",
    "golang.org/x/sys": "BSD-3-Clause",
    "golang.org/x/text": "BSD-3-Clause",
}
LICENSE_FILES = ("LICENSE", "LICENSE.md", "LICENSE.txt", "LICENCE", "COPYING", "License", "license")


def run(cmd, cwd, env=None):
    e = dict(os.environ)
    e.update(env or {})
    return subprocess.run(cmd, cwd=cwd, env=e, check=True, capture_output=True, text=True).stdout


def read(path):
    with open(path, encoding="utf-8", errors="replace", newline="") as f:
        return f.read().replace("\r\n", "\n").strip("\n")


def modules():
    """Los módulos de go.mod: path -> (versión, carpeta)."""
    out = {}
    required = {r["Path"] for r in json.loads(run(["go", "mod", "edit", "-json"], GUI))["Require"]}
    text = run(["go", "list", "-m", "-json", "all"], GUI)
    dec = json.JSONDecoder()
    i = 0
    while i < len(text):
        while i < len(text) and text[i].isspace():
            i += 1
        if i >= len(text):
            break
        m, i = dec.raw_decode(text, i)
        if m.get("Main") or not m.get("Dir") or m["Path"] not in required:
            continue
        out[m["Path"]] = (m["Version"], m["Dir"])
    return out


def linked():
    """Los módulos de los que sale algún paquete del programa, en algún sistema."""
    seen = set()
    for goos in ("windows", "linux", "darwin"):
        for goarch in ("amd64", "arm64"):
            text = run(["go", "list", "-deps", "-tags", "desktop,production,webkit2_41", "-f",
                        "{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}", "./..."], GUI,
                       {"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "1"})
            seen.update(t for t in text.split() if t)
    return seen


def license_text(folder):
    for name in LICENSE_FILES:
        p = os.path.join(folder, name)
        if os.path.isfile(p):
            return read(p)
    raise SystemExit(f"sin licencia en {folder}")


def build():
    mods = modules()
    used = linked()
    required = set(mods)
    missing = sorted(set(mods) - set(LICENSES))
    if missing:
        raise SystemExit("módulos sin licencia anotada en gen_third_party.py: " + ", ".join(missing))
    goroot = run(["go", "env", "GOROOT"], GUI).strip()
    goversion = run(["go", "env", "GOVERSION"], GUI).strip()

    o = []
    w = o.append
    w("MaxPack - avisos de software de terceros")
    w("=" * 40)
    w("")
    w("MaxPack es software libre bajo GPL-3.0-or-later (ver LICENSE). Los paquetes de MaxPack (el")
    w("instalador y el zip de Windows, el .dmg y el .tar.gz de macOS y el .tar.gz de Linux) incluyen")
    w("el código de terceros de abajo, cada uno con su licencia. Este archivo se genera con")
    w("tools/gen_third_party.py a partir de los submódulos y de gui/go.mod; no lo edites a mano.")
    w("")
    w("El código fuente correspondiente a cada binario de MaxPack es este repositorio, en la etiqueta de")
    w("su versión: https://github.com/DeLFuS77/maxpack")
    w("")
    w("Runtime de la ventana: MaxPack usa WebView2 (Windows), WebKitGTK (Linux) y WebKit (macOS) del")
    w("sistema; no se distribuyen con MaxPack.")
    w("")
    w("")
    w("1. Motor (maxpack / maxpack-core)")
    w("-" * 34)
    w("")
    w("zstd 1.5.6 - https://github.com/facebook/zstd (third_party/zstd)")
    w("  Licencia: BSD-3-Clause (zstd se ofrece a elección bajo BSD-3-Clause o GPL-2.0; MaxPack lo usa")
    w("  bajo BSD-3-Clause).")
    w("")
    w(read(os.path.join(ROOT, "third_party", "zstd", "LICENSE")))
    w("")
    w("zlib 1.3.1 - https://github.com/madler/zlib (third_party/zlib)")
    w("  Licencia: zlib. El texto está en la cabecera de zlib.h:")
    w("")
    zh = read(os.path.join(ROOT, "third_party", "zlib", "zlib.h"))
    a = zh.index("Copyright (C)")
    b = zh.index("*/", a)
    w("\n".join(ln.strip() for ln in zh[a:b].splitlines()).strip("\n"))
    w("")
    w("")
    w("2. Ventana del PC (MaxPack.exe / maxpack)")
    w("-" * 41)
    w("")
    w(f"Go ({goversion}) - https://go.dev  Licencia: BSD-3-Clause. La biblioteca estándar de Go va dentro")
    w("de la ventana.")
    w("")
    w(read(os.path.join(goroot, "LICENSE")))
    w("")
    w("go-winres 0.3.3 - https://github.com/tc-hib/go-winres. Solo se usa al compilar, para el icono y la")
    w("versión de MaxPack.exe; su código no va en el programa.")
    w("")
    w("Los módulos de Go de gui/go.mod (versión, licencia). Los marcados con * los enlaza el programa en")
    w("al menos un sistema; los demás son dependencias de go.mod que solo usan las herramientas de")
    w("desarrollo de Wails y no van en los paquetes.")
    w("")
    for path in sorted(required):
        ver, _ = mods[path]
        w(f"  {'*' if path in used else ' '} {path} {ver} - {LICENSES[path]}")
    w("")
    for path in sorted(required):
        ver, folder = mods[path]
        w("")
        w("-" * 72)
        w(f"{path} {ver}  ({LICENSES[path]})" + ("" if path in used else "  [no se enlaza]"))
        w("-" * 72)
        w("")
        w(license_text(folder))
    w("")
    return "\n".join(o)


def main():
    text = build()
    if "--check" in sys.argv:
        cur = read(OUT) if os.path.exists(OUT) else ""
        if cur.strip() != text.strip():
            raise SystemExit("THIRD_PARTY_NOTICES no coincide: ejecuta tools/gen_third_party.py")
        print("ok")
        return
    with open(OUT, "w", encoding="utf-8", newline="\n") as f:
        f.write(text)
    print(f"escrito {OUT} ({len(text)} bytes)")


main()
