// SPDX-License-Identifier: GPL-3.0-or-later

// Package jobs plans and runs the work of MaxPack: compressing, decompressing and verifying
// games, one at a time, with the rules that protect what the user already has.
package jobs

import (
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxName is the most bytes a name made here may have.
const maxName = 200

// norm makes a path comparable: clean, with forward slashes, no trailing one and, on Windows,
// in lower case.
func norm(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	if path == "/" {
		path = ""
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// same tells whether two paths are the same place.
func same(a, b string) bool { return norm(a) == norm(b) }

// inside tells whether child is below parent (not the same place).
func inside(child, parent string) bool {
	return strings.HasPrefix(norm(child), norm(parent)+"/")
}

// cleanTitle takes out what a file name cannot have on Windows, and the spaces and dots that
// would end it.
func cleanTitle(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case strings.ContainsRune(`<>:"/\|?*`, r), unicode.IsControl(r), r == utf8.RuneError:
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimRight(strings.Join(strings.Fields(b.String()), " "), ". ")
}

// cut keeps the start of s that fits in n bytes, whole characters only.
func cut(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return strings.TrimRight(s[:n], ". ")
}

// The images of a game the engine reads, the ones ShadowMountPlus mounts: by the kind `info --json`
// gives them, the extension they go by. The extension is only the quick hint the lists, the
// dialogs, the command line and the Explorer go by; what a file is, the engine tells by its
// contents, and that is what counts once it has read it.
var imageExt = map[string]string{"exfat": ".exfat", "ufs2": ".ffpkg", "pfs": ".ffpfs", "pfsc": ".ffpfsc"}

// ImageExts are the extensions of the images, in the order the window names them.
var ImageExts = []string{".exfat", ".ffpkg", ".ffpfs", ".ffpfsc"}

// IsImageName tells whether a name has the extension of an image, in any case.
func IsImageName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, image := range ImageExts {
		if ext == image {
			return true
		}
	}
	return false
}

// IsImage tells whether a kind the engine gives is one of an image (not a folder nor a .p5z).
func IsImage(kind string) bool { return imageExt[kind] != "" }

// kindExt is the extension of a kind: the one of its image, or .p5z; "" for a folder or a kind
// this window does not know.
func kindExt(kind string) string {
	if kind == "p5z" {
		return ".p5z"
	}
	return imageExt[kind]
}

// baseName is the name of the folder or image of a game, without the extension of an image.
func baseName(path string) string {
	name := filepath.Base(filepath.Clean(path))
	if IsImageName(name) || strings.EqualFold(filepath.Ext(name), ".p5z") {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
}

// P5zName is the name of the .p5z of a game by the default template: "<Title> [<TITLE_ID>].p5z".
func P5zName(it Item) string { return P5zNameAs(it, DefaultTemplate) }

// P5zNameAs is the name of the .p5z of a game by a template.
func P5zNameAs(it Item, template string) string {
	return nameIn(it, template, maxName-len(".p5z")) + ".p5z"
}

// folderName is the name of the folder of an unpacked original by a template.
func folderName(it Item, template string) string { return nameIn(it, template, maxName) }

// idName is the name of the folder of a game made for the console: its TITLE_ID.
func idName(it Item) string {
	if id := cleanTitle(it.TitleID); id != "" {
		return cut(id, maxName)
	}
	return cut(cleanTitle(baseName(it.Path)), maxName)
}
