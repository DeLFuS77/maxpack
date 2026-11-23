#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
# Instala MaxPack para este usuario: la ventana y el motor en ~/.local/lib/maxpack (juntos, porque
# la ventana busca maxpack-core a su lado), con un enlace en ~/.local/bin, y su icono y su entrada
# en el menú de aplicaciones. Con --uninstall lo quita todo.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
data="${XDG_DATA_HOME:-$HOME/.local/share}"
bin="$HOME/.local/bin"
lib="$HOME/.local/lib/maxpack"
icon="$data/icons/hicolor/256x256/apps/maxpack.png"
entry="$data/applications/maxpack.desktop"

refresh() {
	command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$data/applications" >/dev/null 2>&1 || true
	command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q -t "$data/icons/hicolor" >/dev/null 2>&1 || true
}

if [ "${1:-}" = "--uninstall" ]; then
	rm -f "$bin/maxpack" "$icon" "$entry"
	rm -rf "$lib"
	refresh
	echo "MaxPack está desinstalado."
	exit 0
fi

mkdir -p "$bin" "$lib" "$(dirname "$icon")" "$(dirname "$entry")"
install -m 755 "$here/maxpack" "$lib/maxpack"
install -m 755 "$here/maxpack-core" "$lib/maxpack-core"
install -m 644 "$here/maxpack.png" "$icon"
ln -sf "$lib/maxpack" "$bin/maxpack"
# la entrada abre el programa donde quedó, también si ~/.local/bin no está en el PATH
sed "s|^Exec=maxpack|Exec=$lib/maxpack|" "$here/maxpack.desktop" > "$entry"
chmod 644 "$entry"
refresh
echo "MaxPack está instalado: sale en el menú de aplicaciones, y $bin/maxpack en la línea de comandos."
case ":$PATH:" in
	*":$bin:"*) ;;
	*) echo "Añade $bin al PATH para llamar a maxpack por su nombre." ;;
esac
