// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func routed(t *testing.T) (dir string, compress, decompress []string) {
	t.Helper()
	dir = t.TempDir()
	folder := filepath.Join(dir, "Juego")
	os.Mkdir(folder, 0o755)
	for _, name := range []string{"a.p5z", "b.exfat", "notas.txt", "C.P5Z"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	return dir, []string{filepath.Join(dir, "b.exfat"), folder}, []string{filepath.Join(dir, "a.p5z"), filepath.Join(dir, "C.P5Z")}
}

func TestSecondInstanceRoutes(t *testing.T) {
	dir, wantC, wantD := routed(t)
	paths := []string{
		filepath.Join(dir, "a.p5z"), filepath.Join(dir, "b.exfat"), filepath.Join(dir, "Juego"),
		filepath.Join(dir, "notas.txt"), filepath.Join(dir, "no-existe.exfat"), filepath.Join(dir, "C.P5Z"),
	}
	compress, decompress := routeFiles(paths)
	if !reflect.DeepEqual(compress, wantC) || !reflect.DeepEqual(decompress, wantD) {
		t.Fatalf("comprimir %v, descomprimir %v", compress, decompress)
	}
}

func TestFilesNotLostWithoutPage(t *testing.T) {
	dir, wantC, wantD := routed(t)
	var sent []any
	a := &App{emit: func(event string, data any) { sent = append(sent, data) }}
	// no page yet: kept, nothing emitted
	a.receive([]string{filepath.Join(dir, "a.p5z"), filepath.Join(dir, "Juego")})
	if len(sent) != 0 {
		t.Fatalf("emitted without a page: %v", sent)
	}
	// the page asks on load: gets them once
	got := a.PendingFiles()
	if !reflect.DeepEqual(got.Compress, wantC[1:]) || !reflect.DeepEqual(got.Decompress, wantD[:1]) {
		t.Fatalf("pendientes: %+v", got)
	}
	if again := a.PendingFiles(); len(again.Compress)+len(again.Decompress) != 0 {
		t.Fatalf("they were not emptied: %+v", again)
	}
	// with the page ready, they are emitted and not kept
	a.receive([]string{filepath.Join(dir, "b.exfat")})
	if len(sent) != 1 {
		t.Fatalf("eventos: %v", sent)
	}
	if p := sent[0].(FilesAdd); !reflect.DeepEqual(p.Compress, wantC[:1]) {
		t.Fatalf("evento: %+v", p)
	}
	if left := a.PendingFiles(); len(left.Compress) != 0 {
		t.Fatalf("what was emitted was left pending: %+v", left)
	}
}

func TestStartupFilesPending(t *testing.T) {
	dir, _, _ := routed(t)
	a := &App{files: []string{filepath.Join(dir, "a.p5z")}}
	a.startup(nil)
	if got := a.PendingFiles(); len(got.Decompress) != 1 {
		t.Fatalf("%+v", got)
	}
}
