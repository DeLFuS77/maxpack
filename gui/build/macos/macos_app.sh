#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Puts the window, the engine and maxpack-par2 (the repair data of a .p5z) in a macOS app,
# MaxPack.app, to open it from the Finder; the command line is MaxPack.app/Contents/MacOS/maxpack.
# The app is signed "ad hoc", as macOS requires on Apple chips: it is not notarized, so the first
# time it is opened with right-click, Open. The icon (.icns) comes from the PNG with sips and
# iconutil.
#   macos_app.sh <window> <maxpack-core> <version> <app folder> <appicon.png> <maxpack-par2>
set -euo pipefail

program="$1"
core="$2"
version="$3"
app="$4/MaxPack.app"
png="$5"
par2="$6"

rm -rf "${app}"
mkdir -p "${app}/Contents/MacOS" "${app}/Contents/Resources"
cp "${program}" "${app}/Contents/MacOS/maxpack"
cp "${core}" "${app}/Contents/MacOS/maxpack-core"
cp "${par2}" "${app}/Contents/MacOS/maxpack-par2"
chmod 755 "${app}/Contents/MacOS/maxpack" "${app}/Contents/MacOS/maxpack-core" "${app}/Contents/MacOS/maxpack-par2"

iconset="$(mktemp -d)/icon.iconset"
mkdir -p "${iconset}"
for size in 16 32 128 256 512; do
  sips -z "${size}" "${size}" "${png}" --out "${iconset}/icon_${size}x${size}.png" > /dev/null
  sips -z "$((size * 2))" "$((size * 2))" "${png}" --out "${iconset}/icon_${size}x${size}@2x.png" > /dev/null
done
iconutil -c icns "${iconset}" -o "${app}/Contents/Resources/icon.icns"

# a tag v1.2.3 (or 1.2.3) gives version 1.2.3 and numbers 1.2.3; otherwise, 0.0.0
version="${version#v}"
numbers="0.0.0"
if [[ "${version}" =~ ^([0-9]+\.[0-9]+\.[0-9]+)(-[0-9A-Za-z.-]+)?$ ]]; then
  numbers="${BASH_REMATCH[1]}"
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
			<key>CFBundleTypeName</key><string>Folder</string>
			<key>CFBundleTypeRole</key><string>Viewer</string>
			<key>LSHandlerRank</key><string>Alternate</string>
			<key>LSItemContentTypes</key><array><string>public.folder</string></array>
		</dict>
		<dict>
			<key>CFBundleTypeName</key><string>PS5 game</string>
			<key>CFBundleTypeRole</key><string>Viewer</string>
			<key>LSHandlerRank</key><string>Alternate</string>
			<key>CFBundleTypeExtensions</key><array><string>p5z</string><string>exfat</string><string>ffpkg</string><string>ffpfs</string><string>ffpfsc</string></array>
		</dict>
	</array>
</dict>
</plist>
PLIST
plutil -lint "${app}/Contents/Info.plist"
codesign --force --deep --sign - "${app}"
codesign --verify "${app}"
