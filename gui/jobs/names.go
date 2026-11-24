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

// baseName is the name of the folder or image of a game, without the extension of an image.
func baseName(path string) string {
	name := filepath.Base(filepath.Clean(path))
	switch strings.ToLower(filepath.Ext(name)) {
	case ".exfat", ".p5z", ".ffpkg":
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
}

// gameName is "<Title> [<TITLE_ID>]" with no characters that Windows refuses and at most room
// bytes; without a title, the name of the folder or the image.
func gameName(it Item, room int) string {
	title := cleanTitle(it.Name)
	if title == "" {
		title = cleanTitle(baseName(it.Path))
	}
	suffix := ""
	id := cleanTitle(it.TitleID)
	if id != "" && !strings.Contains(title, id) {
		suffix = " [" + id + "]"
	}
	title = cut(title, room-len(suffix))
	if title == "" {
		title = "game"
	}
	return title + suffix
}

// P5zName is the name of the .p5z of a game: "<Title> [<TITLE_ID>].p5z".
func P5zName(it Item) string { return gameName(it, maxName-len(".p5z")) + ".p5z" }

// folderName is the name of the folder of an unpacked original: "<Title> [<TITLE_ID>]".
func folderName(it Item) string { return gameName(it, maxName) }

// idName is the name of the folder of a game made for the console: its TITLE_ID.
func idName(it Item) string {
	if id := cleanTitle(it.TitleID); id != "" {
		return cut(id, maxName)
	}
	return cut(cleanTitle(baseName(it.Path)), maxName)
}
