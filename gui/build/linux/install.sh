#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
# Installs MaxPack for this user: the window, the engine and maxpack-par2 in ~/.local/lib/maxpack
# (together, because the window looks for maxpack-core and maxpack-par2 next to it), with a link in
# ~/.local/bin, and its icon and its entry in the applications menu. With --uninstall it removes
# everything.
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
	echo "MaxPack is uninstalled."
	exit 0
fi

mkdir -p "$bin" "$lib" "$(dirname "$icon")" "$(dirname "$entry")"
install -m 755 "$here/maxpack" "$lib/maxpack"
install -m 755 "$here/maxpack-core" "$lib/maxpack-core"
install -m 755 "$here/maxpack-par2" "$lib/maxpack-par2"
install -m 644 "$here/maxpack.png" "$icon"
ln -sf "$lib/maxpack" "$bin/maxpack"
# the entry opens the program where it ended up, also if ~/.local/bin is not in the PATH
sed "s|^Exec=maxpack|Exec=$lib/maxpack|" "$here/maxpack.desktop" > "$entry"
chmod 644 "$entry"
refresh
echo "MaxPack is installed: it shows up in the applications menu, and $bin/maxpack on the command line."
case ":$PATH:" in
	*":$bin:"*) ;;
	*) echo "Add $bin to the PATH to call maxpack by its name." ;;
esac
