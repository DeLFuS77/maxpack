// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
)

const gib = uint64(1) << 30

// fakeFS is a FS the tests control: what is free and the type of every path, and where the
// roots of the drives are.
type fakeFS struct {
	free  uint64
	typ   string
	roots []string
}

func (f fakeFS) Free(string) (uint64, error) { return f.free, nil }
func (f fakeFS) Type(string) (string, error) { return f.typ, nil }
func (f fakeFS) Root(path string) string {
	best := "/fake-root"
	length := -1
	for _, root := range f.roots {
		if same(path, root) || inside(path, root) {
			if len(root) > length {
				best, length = root, len(root)
			}
		}
	}
	return best
}

func newFS() fakeFS { return fakeFS{free: 1000 * gib, typ: "NTFS"} }

func needCore(t *testing.T) (engine, fixtures string) {
	t.Helper()
	engine, fixtures = os.Getenv("MAXPACK_CORE"), os.Getenv("MP_FIXTURES")
	if engine == "" || fixtures == "" {
		t.Skip("faltan MAXPACK_CORE y MP_FIXTURES: se necesita el motor y las imágenes de prueba")
	}
	return engine, fixtures
}

func item(title, id string, size uint64) Item {
	return Item{Path: filepath.Join(os.TempDir(), "origen-"+id), TitleID: id, Name: title, Kind: "folder", OriginalSize: size, HasZstdRuntime: true}
}

func TestP5zNameSanitized(t *testing.T) {
	cases := []struct{ title, want string }{
		{"Ratchet: Rift? <Apart>", "Ratchet Rift Apart [PPSA00001].p5z"},
		{"Fin del juego. ", "Fin del juego [PPSA00001].p5z"},
		{"Dos\x01 controles\x7f", "Dos controles [PPSA00001].p5z"},
	}
	for _, c := range cases {
		it := item(c.title, "PPSA00001", 1)
		if got := P5zName(it); got != c.want {
			t.Errorf("%q: %q, esperaba %q", c.title, got, c.want)
		}
	}
	long := P5zName(item(strings.Repeat("é", 200), "PPSA00001", 1))
	if len(long) > 200 || !strings.HasSuffix(long, "[PPSA00001].p5z") {
		t.Errorf("nombre largo: %d bytes, %q", len(long), long)
	}
	empty := item("", "PPSA00001", 1)
	empty.Path = filepath.Join("x", "Mi Juego")
	if got := P5zName(empty); !strings.HasPrefix(got, "Mi Juego") {
		t.Errorf("sin título: %q", got)
	}
}

func TestDestRules(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(root, "sub")
	os.MkdirAll(other, 0o755)
	f := fakeFS{free: 1000 * gib, typ: "NTFS", roots: []string{root}}
	it := item("Test", "TEST00001", 100)

	p := PlanCompress([]Item{it}, Options{Mode: "folder", Dest: root}, f)[0]
	if want := filepath.Join(root, "homebrew", "TEST00001"); !same(p.Out, want) {
		t.Errorf("raíz: %q, esperaba %q", p.Out, want)
	}
	p = PlanCompress([]Item{it}, Options{Mode: "folder", Dest: other}, f)[0]
	if want := filepath.Join(other, "TEST00001"); !same(p.Out, want) {
		t.Errorf("carpeta: %q, esperaba %q", p.Out, want)
	}
	p = PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: other}, f)[0]
	if want := filepath.Join(other, "Test [TEST00001].p5z"); !same(p.Out, want) {
		t.Errorf("p5z: %q, esperaba %q", p.Out, want)
	}
}

func TestDecompressDestRules(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(root, "sub")
	f := fakeFS{free: 1000 * gib, typ: "NTFS", roots: []string{root}}
	it := item("Mi: Juego", "TEST00001", 100)
	it.Kind = "p5z"
	p := PlanDecompress([]Item{it}, Options{Mode: "original", Dest: other}, f)[0]
	if want := filepath.Join(other, "Mi Juego [TEST00001]"); !same(p.Out, want) {
		t.Errorf("original: %q", p.Out)
	}
	p = PlanDecompress([]Item{it}, Options{Mode: "console", Dest: root}, f)[0]
	if want := filepath.Join(root, "homebrew", "TEST00001"); !same(p.Out, want) {
		t.Errorf("consola raíz: %q", p.Out)
	}
	p = PlanDecompress([]Item{it}, Options{Mode: "console", Dest: other}, f)[0]
	if want := filepath.Join(other, "TEST00001"); !same(p.Out, want) {
		t.Errorf("consola carpeta: %q", p.Out)
	}
}

func TestSameAsSource(t *testing.T) {
	f := fakeFS{free: 1000 * gib, typ: "NTFS", roots: []string{"E:/"}}
	it := item("X", "PPSA12544", 100)
	it.Path = "E:/homebrew/PPSA12544"
	p := PlanCompress([]Item{it}, Options{Mode: "folder", Dest: "E:/"}, f)[0]
	if p.Block != "same_as_source" {
		t.Errorf("Block = %q (Out %q)", p.Block, p.Out)
	}
	// destination inside the source
	it.Path = "E:/juegos/G"
	p = PlanCompress([]Item{it}, Options{Mode: "folder", Dest: "E:/juegos/G/salida"}, f)[0]
	if p.Block != "same_as_source" {
		t.Errorf("dentro del origen: Block = %q", p.Block)
	}
	// source inside the destination, which would be replaced
	it.Path = "E:/homebrew/PPSA12544/sub"
	p = PlanCompress([]Item{it}, Options{Mode: "folder", Dest: "E:/"}, f)[0]
	if p.Block != "same_as_source" {
		t.Errorf("origen dentro del destino: Block = %q", p.Block)
	}
}

func hasWarning(p Plan, w string) bool {
	for _, x := range p.Warnings {
		if x == w {
			return true
		}
	}
	return false
}

func hasArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

func TestFat32Rules(t *testing.T) {
	dest := t.TempDir()
	f := fakeFS{free: 1000 * gib, typ: "FAT32"}
	big := item("Grande", "TEST00001", 5*gib)
	p := PlanCompress([]Item{big}, Options{Mode: "p5z", Dest: dest}, f)[0]
	if !hasWarning(p, "fat32") || p.Block != "fat32_too_big" {
		t.Errorf("sin seguir: avisos %v, Block %q", p.Warnings, p.Block)
	}
	p = PlanCompress([]Item{big}, Options{Mode: "p5z", Dest: dest, ContinueFat32: true}, f)[0]
	if !hasWarning(p, "fat32") || p.Block != "" {
		t.Errorf("seguir: avisos %v, Block %q", p.Warnings, p.Block)
	}
	if !hasArg(p.Args, "--max-file-size") || !hasArg(p.Args, "4294967295") {
		t.Errorf("faltan los argumentos de tope: %v", p.Args)
	}
	small := item("Pequeño", "TEST00002", 3*gib)
	p = PlanCompress([]Item{small}, Options{Mode: "p5z", Dest: dest}, f)[0]
	if len(p.Warnings) != 0 || p.Block != "" || hasArg(p.Args, "--max-file-size") {
		t.Errorf("3 GiB: avisos %v, Block %q, args %v", p.Warnings, p.Block, p.Args)
	}
	p = PlanCompress([]Item{big}, Options{Mode: "folder", Dest: dest}, f)[0]
	if hasWarning(p, "fat32") || p.Block != "" {
		t.Errorf("carpeta en FAT32: avisos %v, Block %q", p.Warnings, p.Block)
	}
}

func TestNoSpace(t *testing.T) {
	dest := t.TempDir()
	f := fakeFS{free: 2 * gib, typ: "NTFS"}
	it := item("Juego", "TEST00001", 3*gib)
	p := PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: dest}, f)[0]
	if !hasWarning(p, "may_not_fit") || p.Block != "" {
		t.Errorf("avisos %v, Block %q", p.Warnings, p.Block)
	}
	// replacing something that is already there: both must fit
	old := filepath.Join(dest, P5zName(it))
	os.WriteFile(old, []byte("viejo"), 0o644)
	p = PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: dest}, f)[0]
	if len(p.Replace) == 0 || p.Tmp != p.Out+".maxpack-tmp" || p.Block != "no_space" {
		t.Errorf("reemplazo: %+v", p)
	}
	f.free = 10 * gib
	p = PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: dest}, f)[0]
	if len(p.Replace) == 0 || p.Block != "" || hasWarning(p, "may_not_fit") {
		t.Errorf("reemplazo con sitio: %+v", p)
	}
}

func TestNeedsRuntime(t *testing.T) {
	dest := t.TempDir()
	it := item("Juego", "TEST00001", 10)
	it.HasZstdRuntime = false
	p := PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: dest}, newFS())[0]
	if !hasWarning(p, "needs_runtime") || p.Block != "" || hasArg(p.Args, "--runtime") {
		t.Errorf("sin runtime: %v %q %v", p.Warnings, p.Block, p.Args)
	}
	p = PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: dest, Runtime: "ampr.sprx"}, newFS())[0]
	if hasWarning(p, "needs_runtime") || !hasArg(p.Args, "--runtime") || !hasArg(p.Args, "ampr.sprx") {
		t.Errorf("con runtime: %v %v", p.Warnings, p.Args)
	}
}

func TestDuplicateTargetsWarned(t *testing.T) {
	dest := t.TempDir()
	a, b := item("A", "TEST00001", 10), item("B", "TEST00001", 10)
	b.Path += "2"
	plans := PlanCompress([]Item{a, b}, Options{Mode: "folder", Dest: dest}, newFS())
	if !hasWarning(plans[0], "duplicate_target") || !hasWarning(plans[1], "duplicate_target") {
		t.Errorf("avisos: %v %v", plans[0].Warnings, plans[1].Warnings)
	}
}

type states struct {
	mu   sync.Mutex
	list map[int][]string
}

func (s *states) on(i int, state, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list[i] = append(s.list[i], state)
}

func describe(t *testing.T, engine, path string) Item {
	t.Helper()
	it, err := Describe(engine, path)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func tree(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func engineRun(t *testing.T, engine string, args ...string) {
	t.Helper()
	result, code, err := core.Run(context.Background(), engine, args, nil)
	if err != nil || code != 0 || result.Raw["ok"] != true {
		t.Fatalf("%v: código %d, %+v, %v", args, code, result.Raw, err)
	}
}

func TestRunCompressBoth(t *testing.T) {
	engine, fixtures := needCore(t)
	it := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	if it.TitleID != "TEST00001" || it.OriginalSize == 0 {
		t.Fatalf("Describe: %+v", it)
	}
	for _, mode := range []Mode{"p5z", "folder"} {
		dest := t.TempDir()
		o := Options{Mode: mode, Dest: dest, Level: 19}
		plans := PlanCompress([]Item{it}, o, newFS())
		if plans[0].Block != "" {
			t.Fatalf("%s: bloqueado: %q", mode, plans[0].Block)
		}
		s := &states{list: map[int][]string{}}
		r := &Runner{Core: engine, OnItem: s.on}
		sum := r.Run(context.Background(), plans, o)
		if sum.Done != 1 || sum.Failed != 0 || sum.Saved == 0 {
			t.Fatalf("%s: %+v, estados %v", mode, sum, s.list)
		}
		got := strings.Join(s.list[0], ",")
		if !strings.Contains(got, "packing,verifying,ok") {
			t.Errorf("%s: estados %s", mode, got)
		}
		want := filepath.Join(t.TempDir(), "ref")
		args := []string{"pack", it.Path, "-o", want, "--level", "19"}
		if mode == "p5z" {
			args = append(args, "--p5z")
		} else {
			args = append(args, "--folder")
		}
		engineRun(t, engine, args...)
		if mode == "p5z" {
			a, _ := os.ReadFile(plans[0].Out)
			b, _ := os.ReadFile(want)
			if len(a) == 0 || !bytes.Equal(a, b) {
				t.Errorf("el .p5z no es igual byte a byte (%d y %d bytes)", len(a), len(b))
			}
		} else {
			a, b := tree(t, plans[0].Out), tree(t, want)
			if len(a) == 0 || len(a) != len(b) {
				t.Errorf("carpetas distintas: %d y %d archivos", len(a), len(b))
			}
			for k, v := range b {
				if a[k] != v {
					t.Errorf("difiere %s", k)
				}
			}
		}
	}
}

func TestCancelKeepsOld(t *testing.T) {
	engine, fixtures := needCore(t)
	it := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	dest := t.TempDir()
	o := Options{Mode: "p5z", Dest: dest}
	plans := PlanCompress([]Item{it}, o, newFS())
	os.WriteFile(plans[0].Out, []byte("viejo"), 0o644)
	plans = PlanCompress([]Item{it}, o, newFS())
	if len(plans[0].Replace) == 0 {
		t.Fatal("el plan no reemplaza")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &states{list: map[int][]string{}}
	r := &Runner{Core: engine, OnItem: s.on, OnEvent: func(int, core.Event) { cancel() }}
	sum := r.Run(ctx, plans, o)
	if sum.Done != 0 {
		t.Errorf("%+v", sum)
	}
	if data, _ := os.ReadFile(plans[0].Out); string(data) != "viejo" {
		t.Errorf("lo viejo cambió: %q", data)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 {
		t.Errorf("sobran archivos: %v", entries)
	}
	if last := s.list[0][len(s.list[0])-1]; last != "cancelled" {
		t.Errorf("estados %v", s.list[0])
	}
}

func TestReplaceSwaps(t *testing.T) {
	engine, fixtures := needCore(t)
	it := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	dest := t.TempDir()
	o := Options{Mode: "p5z", Dest: dest}
	out := filepath.Join(dest, P5zName(it))
	os.WriteFile(out, []byte("viejo"), 0o644)
	plans := PlanCompress([]Item{it}, o, newFS())
	sum := (&Runner{Core: engine}).Run(context.Background(), plans, o)
	if sum.Done != 1 {
		t.Fatalf("%+v", sum)
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() < 1000 {
		t.Fatalf("no quedó lo nuevo: %v", err)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 {
		t.Errorf("sobran archivos: %v", entries)
	}
}

func TestDestVanishesMarksError(t *testing.T) {
	engine, fixtures := needCore(t)
	it := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	it2 := it
	it2.TitleID = "TEST00002"
	parent := t.TempDir()
	dest := filepath.Join(parent, "usb")
	os.MkdirAll(dest, 0o755)
	o := Options{Mode: "folder", Dest: dest}
	plans := PlanCompress([]Item{it, it2}, o, newFS())
	s := &states{list: map[int][]string{}}
	r := &Runner{Core: engine, OnItem: func(i int, state, detail string) {
		s.on(i, state, detail)
		if i == 0 && state == "ok" {
			os.RemoveAll(dest)
		}
	}}
	sum := r.Run(context.Background(), plans, o)
	if sum.Done != 1 || sum.Failed != 1 {
		t.Fatalf("%+v, estados %v", sum, s.list)
	}
	if last := s.list[1][len(s.list[1])-1]; last != "error" {
		t.Errorf("estados del segundo: %v", s.list[1])
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("el destino volvió a aparecer")
	}
}

func TestRunDecompressVerify(t *testing.T) {
	engine, fixtures := needCore(t)
	src := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	dest := t.TempDir()
	p5z := filepath.Join(dest, "juego.p5z")
	engineRun(t, engine, "pack", src.Path, "-o", p5z, "--p5z")
	it := describe(t, engine, p5z)
	if it.Kind != "p5z" {
		t.Fatalf("%+v", it)
	}
	outDir := t.TempDir()
	o := Options{Mode: "original", Dest: outDir}
	plans := PlanDecompress([]Item{it}, o, newFS())
	s := &states{list: map[int][]string{}}
	sum := (&Runner{Core: engine, OnItem: s.on}).Run(context.Background(), plans, o)
	if sum.Done != 1 || !strings.Contains(strings.Join(s.list[0], ","), "unpacking,ok") {
		t.Fatalf("%+v %v", sum, s.list)
	}
	want := filepath.Join(t.TempDir(), "ref")
	engineRun(t, engine, "unpack", p5z, "--original", "-o", want)
	a, b := tree(t, plans[0].Out), tree(t, want)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("%d y %d archivos", len(a), len(b))
	}
	for k, v := range b {
		if a[k] != v {
			t.Errorf("difiere %s", k)
		}
	}

	vo := Options{Mode: "verify"}
	vplans := PlanVerify([]Item{it})
	s = &states{list: map[int][]string{}}
	sum = (&Runner{Core: engine, OnItem: s.on}).Run(context.Background(), vplans, vo)
	if sum.Done != 1 || !strings.Contains(strings.Join(s.list[0], ","), "verifying,ok") {
		t.Fatalf("verificar: %+v %v", sum, s.list)
	}
	// a damaged .p5z is not ok
	data, _ := os.ReadFile(p5z)
	data[len(data)/2] ^= 0xFF
	bad := filepath.Join(dest, "malo.p5z")
	os.WriteFile(bad, data, 0o644)
	badItem := it
	badItem.Path = bad
	sum = (&Runner{Core: engine}).Run(context.Background(), PlanVerify([]Item{badItem}), vo)
	if sum.Failed != 1 {
		t.Errorf("un .p5z dañado dio %+v", sum)
	}
}

func TestSamePathCase(t *testing.T) {
	if runtime.GOOS == "windows" && !same(`E:\Juegos`, `e:/juegos/`) {
		t.Error("en Windows las rutas no distinguen mayúsculas")
	}
	if !inside("E:/a/b/c", "E:/a/b") || inside("E:/a/bc", "E:/a/b") {
		t.Error("inside")
	}
}

func TestSystemFS(t *testing.T) {
	dir := t.TempDir()
	f := SystemFS()
	if free, err := f.Free(filepath.Join(dir, "no", "existe")); err != nil || free == 0 {
		t.Errorf("Free: %d, %v", free, err)
	}
	if root := f.Root(dir); root == "" || !(same(root, dir) || inside(dir, root)) {
		t.Errorf("Root(%q) = %q", dir, root)
	}
	if runtime.GOOS == "windows" {
		if kind, err := f.Type(dir); err != nil || kind == "" {
			t.Errorf("Type: %q, %v", kind, err)
		}
	}
}
