#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Mete la ventana y el motor en una app de macOS, MaxPack.app, para abrirla desde el Finder; la
# línea de comandos es MaxPack.app/Contents/MacOS/maxpack. La app va firmada "ad hoc", como pide
# macOS en los chips de Apple: no está notarizada, así que la primera vez se abre con clic
# derecho, Abrir. El icono (.icns) sale del PNG con sips e iconutil.
#   macos_app.sh <ventana> <maxpack-core> <versión> <carpeta de la app> <appicon.png>
set -euo pipefail

program="$1"
core="$2"
version="$3"
app="$4/MaxPack.app"
png="$5"

rm -rf "${app}"
mkdir -p "${app}/Contents/MacOS" "${app}/Contents/Resources"
cp "${program}" "${app}/Contents/MacOS/maxpack"
cp "${core}" "${app}/Contents/MacOS/maxpack-core"
chmod 755 "${app}/Contents/MacOS/maxpack" "${app}/Contents/MacOS/maxpack-core"

iconset="$(mktemp -d)/icon.iconset"
mkdir -p "${iconset}"
for size in 16 32 128 256 512; do
  sips -z "${size}" "${size}" "${png}" --out "${iconset}/icon_${size}x${size}.png" > /dev/null
  sips -z "$((size * 2))" "$((size * 2))" "${png}" --out "${iconset}/icon_${size}x${size}@2x.png" > /dev/null
done
iconutil -c icns "${iconset}" -o "${app}/Contents/Resources/icon.icns"

numbers="0.0.0"
if [[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  numbers="${version}"
fi
cat > "${app}/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>MaxPack</string>
	<key>CFBundleDisplayName</key><string>MaxPack</string>
	<key>CFBundleIdentifier</key><string>io.github.delfus77.maxpack</string>
	<key>CFBundleExecutable</key><string>maxpack</string>
	<key>CFBundleIconFile</key><string>icon</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleVersion</key><string>${numbers}</string>
	<key>CFBundleShortVersionString</key><string>${version}</string>
	<key>LSMinimumSystemVersion</key><string>13.0</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>CFBundleDocumentTypes</key>
	<array>
		<dict>
			<key>CFBundleTypeName</key><string>Carpeta</string>
			<key>CFBundleTypeRole</key><string>Viewer</string>
			<key>LSHandlerRank</key><string>Alternate</string>
			<key>LSItemContentTypes</key><array><string>public.folder</string></array>
		</dict>
		<dict>
			<key>CFBundleTypeName</key><string>Juego de PS5</string>
			<key>CFBundleTypeRole</key><string>Viewer</string>
			<key>LSHandlerRank</key><string>Alternate</string>
			<key>CFBundleTypeExtensions</key><array><string>p5z</string><string>exfat</string></array>
		</dict>
	</array>
</dict>
</plist>
PLIST
plutil -lint "${app}/Contents/Info.plist"
codesign --force --deep --sign - "${app}"
codesign --verify "${app}"
