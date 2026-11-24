// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func routed(t *testing.T) (dir string, compress, details []string) {
	t.Helper()
	dir = t.TempDir()
	folder := filepath.Join(dir, "Juego")
	os.Mkdir(folder, 0o755)
	for _, name := range []string{"a.p5z", "b.exfat", "notas.txt", "C.P5Z"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	return dir, []string{filepath.Join(dir, "b.exfat"), folder}, []string{filepath.Join(dir, "a.p5z"), filepath.Join(dir, "C.P5Z")}
}

// The space a path goes to: the one asked for (by the menu of the Explorer, or Details for a double
// click) when it takes the path; else Compress for a folder or an .exfat image and Details for a
// .p5z, whose card offers Verify and Decompress; nothing for any other file.
func TestSpaceOf(t *testing.T) {
	for _, c := range []struct {
		name   string
		folder bool
		asked  string
		want   string
	}{
		{"Game", true, "", "compress"},
		{"image.exfat", false, "", "compress"},
		{"IMAGE.EXFAT", false, "", "compress"},
		{"game.p5z", false, "", "details"},
		{"GAME.P5Z", false, "", "details"},
		{"notes.txt", false, "", ""},
		{"game.p5z.maxpack-tmp", false, "", ""},
		// a folder named like a file is a folder
		{"Odd.p5z", true, "", "compress"},
		{"game.p5z", false, "verify", "verify"},
		{"Game", true, "verify", "verify"},
		{"image.exfat", false, "verify", "compress"},
		{"game.p5z", false, "details", "details"},
		{"Game", true, "details", "details"},
		{"image.exfat", false, "details", "details"},
		{"Game", true, "compress", "compress"},
		{"image.exfat", false, "compress", "compress"},
		{"game.p5z", false, "compress", "details"},
		{"notes.txt", false, "details", ""},
		{"game.p5z", false, "nowhere", "details"},
	} {
		if got := spaceOf(c.name, c.folder, c.asked); got != c.want {
			t.Errorf("%s (folder %v, asked %q): %q, want %q", c.name, c.folder, c.asked, got, c.want)
		}
	}
}

func TestRouteFiles(t *testing.T) {
	dir, wantC, wantD := routed(t)
	paths := []string{
		filepath.Join(dir, "a.p5z"), filepath.Join(dir, "b.exfat"), filepath.Join(dir, "Juego"),
		filepath.Join(dir, "notas.txt"), filepath.Join(dir, "no-existe.exfat"), filepath.Join(dir, "C.P5Z"),
	}
	got := routeFiles(launch{files: paths})
	if want := (FilesAdd{Compress: wantC, Details: wantD}); !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v, want %+v", got, want)
	}
	got = routeFiles(launch{files: paths, section: "verify"})
	if want := (FilesAdd{Compress: wantC[:1], Verify: []string{paths[0], wantC[1], wantD[1]}, Show: "verify"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("verify: %+v, want %+v", got, want)
	}
	// the space asked for shows only when it took something
	if got := routeFiles(launch{files: []string{filepath.Join(dir, "b.exfat")}, section: "verify"}); got.Show != "" || len(got.Compress) != 1 {
		t.Fatalf("an image for Verify: %+v", got)
	}
	if got := routeFiles(launch{files: []string{filepath.Join(dir, "gone.p5z")}, section: "details"}); !got.empty() {
		t.Fatalf("nothing there: %+v", got)
	}
}

func TestFilesNotLostWithoutPage(t *testing.T) {
	dir, wantC, wantD := routed(t)
	var sent []any
	a := &App{emit: func(event string, data any) { sent = append(sent, data) }}
	// no page yet: kept, nothing emitted; the space of the last launch that asked for one shows
	a.receive(launch{files: []string{filepath.Join(dir, "a.p5z"), filepath.Join(dir, "Juego")}})
	a.receive(launch{files: []string{filepath.Join(dir, "C.P5Z")}, section: "verify"})
	if len(sent) != 0 {
		t.Fatalf("emitted without a page: %v", sent)
	}
	// the page asks on load: gets them once
	got := a.PendingFiles()
	want := FilesAdd{Compress: wantC[1:], Verify: wantD[1:], Details: wantD[:1], Show: "verify"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pendientes: %+v, want %+v", got, want)
	}
	if again := a.PendingFiles(); !again.empty() || again.Show != "" || again.Compress == nil || again.Verify == nil || again.Details == nil {
		t.Fatalf("they were not emptied, or are null for the page: %+v", again)
	}
	// with the page ready, they are emitted and not kept
	a.receive(launch{files: []string{filepath.Join(dir, "b.exfat")}})
	if len(sent) != 1 {
		t.Fatalf("eventos: %v", sent)
	}
	if p := sent[0].(FilesAdd); !reflect.DeepEqual(p.Compress, wantC[:1]) {
		t.Fatalf("evento: %+v", p)
	}
	if left := a.PendingFiles(); !left.empty() {
		t.Fatalf("what was emitted was left pending: %+v", left)
	}
}

func TestStartupFilesPending(t *testing.T) {
	dir, _, _ := routed(t)
	a := &App{start: launch{files: []string{filepath.Join(dir, "a.p5z")}, section: "details"}}
	a.startup(nil)
	if got := a.PendingFiles(); len(got.Details) != 1 || got.Show != "details" {
		t.Fatalf("%+v", got)
	}
}
