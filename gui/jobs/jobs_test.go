// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
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
		t.Skip("MAXPACK_CORE and MP_FIXTURES are missing: the engine and the test images are needed")
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
			t.Errorf("%q: %q, expected %q", c.title, got, c.want)
		}
	}
	long := P5zName(item(strings.Repeat("é", 200), "PPSA00001", 1))
	if len(long) > 200 || !strings.HasSuffix(long, "[PPSA00001].p5z") {
		t.Errorf("long name: %d bytes, %q", len(long), long)
	}
	empty := item("", "PPSA00001", 1)
	empty.Path = filepath.Join("x", "Mi Juego")
	if got := P5zName(empty); !strings.HasPrefix(got, "Mi Juego") {
		t.Errorf("without a title: %q", got)
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
		t.Errorf("root: %q, wanted %q", p.Out, want)
	}
	p = PlanCompress([]Item{it}, Options{Mode: "folder", Dest: other}, f)[0]
	if want := filepath.Join(other, "TEST00001"); !same(p.Out, want) {
		t.Errorf("folder: %q, wanted %q", p.Out, want)
	}
	p = PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: other}, f)[0]
	if want := filepath.Join(other, "Test [TEST00001].p5z"); !same(p.Out, want) {
		t.Errorf("p5z: %q, expected %q", p.Out, want)
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
		t.Errorf("console root: %q", p.Out)
	}
	p = PlanDecompress([]Item{it}, Options{Mode: "console", Dest: other}, f)[0]
	if want := filepath.Join(other, "TEST00001"); !same(p.Out, want) {
		t.Errorf("console folder: %q", p.Out)
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
		t.Errorf("inside the source: Block = %q", p.Block)
	}
	// source inside the destination, which would be replaced
	it.Path = "E:/homebrew/PPSA12544/sub"
	p = PlanCompress([]Item{it}, Options{Mode: "folder", Dest: "E:/"}, f)[0]
	if p.Block != "same_as_source" {
		t.Errorf("source inside the destination: Block = %q", p.Block)
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
		t.Errorf("without continuing: warnings %v, Block %q", p.Warnings, p.Block)
	}
	p = PlanCompress([]Item{big}, Options{Mode: "p5z", Dest: dest, ContinueFat32: true}, f)[0]
	if !hasWarning(p, "fat32") || p.Block != "" {
		t.Errorf("continuing: warnings %v, Block %q", p.Warnings, p.Block)
	}
	if !hasArg(p.Args, "--max-file-size") || !hasArg(p.Args, "4294967295") {
		t.Errorf("the cap arguments are missing: %v", p.Args)
	}
	small := item("Pequeño", "TEST00002", 3*gib)
	p = PlanCompress([]Item{small}, Options{Mode: "p5z", Dest: dest}, f)[0]
	if len(p.Warnings) != 0 || p.Block != "" || hasArg(p.Args, "--max-file-size") {
		t.Errorf("3 GiB: warnings %v, Block %q, args %v", p.Warnings, p.Block, p.Args)
	}
	p = PlanCompress([]Item{big}, Options{Mode: "folder", Dest: dest}, f)[0]
	if hasWarning(p, "fat32") || p.Block != "" {
		t.Errorf("folder on FAT32: warnings %v, Block %q", p.Warnings, p.Block)
	}
}

func TestNoSpace(t *testing.T) {
	dest := t.TempDir()
	f := fakeFS{free: 2 * gib, typ: "NTFS"}
	it := item("Juego", "TEST00001", 3*gib)
	p := PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: dest}, f)[0]
	if !hasWarning(p, "may_not_fit") || p.Block != "" {
		t.Errorf("warnings %v, Block %q", p.Warnings, p.Block)
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
		t.Errorf("replacement with room: %+v", p)
	}
}

func TestNeedsRuntime(t *testing.T) {
	dest := t.TempDir()
	it := item("Juego", "TEST00001", 10)
	it.HasZstdRuntime = false
	p := PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: dest}, newFS())[0]
	if !hasWarning(p, "needs_runtime") || p.Block != "" || hasArg(p.Args, "--runtime") {
		t.Errorf("without runtime: %v %q %v", p.Warnings, p.Block, p.Args)
	}
	p = PlanCompress([]Item{it}, Options{Mode: "p5z", Dest: dest, Runtime: "ampr.sprx"}, newFS())[0]
	if hasWarning(p, "needs_runtime") || !hasArg(p.Args, "--runtime") || !hasArg(p.Args, "ampr.sprx") {
		t.Errorf("with runtime: %v %v", p.Warnings, p.Args)
	}
}

func TestDuplicateTargetsWarned(t *testing.T) {
	dest := t.TempDir()
	a, b := item("A", "TEST00001", 10), item("B", "TEST00001", 10)
	b.Path += "2"
	plans := PlanCompress([]Item{a, b}, Options{Mode: "folder", Dest: dest}, newFS())
	if !hasWarning(plans[0], "duplicate_target") || !hasWarning(plans[1], "duplicate_target") {
		t.Errorf("warnings: %v %v", plans[0].Warnings, plans[1].Warnings)
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
		t.Fatalf("%v: code %d, %+v, %v", args, code, result.Raw, err)
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
				t.Errorf("the .p5z is not byte-for-byte equal (%d and %d bytes)", len(a), len(b))
			}
		} else {
			a, b := tree(t, plans[0].Out), tree(t, want)
			if len(a) == 0 || len(a) != len(b) {
				t.Errorf("different folders: %d and %d files", len(a), len(b))
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
		t.Fatal("the plan does not replace")
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
		t.Errorf("the old one changed: %q", data)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 {
		t.Errorf("extra files: %v", entries)
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
		t.Fatalf("the new one is not there: %v", err)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 {
		t.Errorf("extra files: %v", entries)
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
		t.Errorf("states of the second: %v", s.list[1])
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("the destination appeared again")
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
		t.Fatalf("%d and %d files", len(a), len(b))
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
		t.Errorf("a damaged .p5z gave %+v", sum)
	}
}

func TestSamePathCase(t *testing.T) {
	if runtime.GOOS == "windows" && !same(`E:\Juegos`, `e:/juegos/`) {
		t.Error("on Windows paths are case-insensitive")
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

// failingRemove makes the removal of paths that exist fail, as an antivirus holding a file does.
func failingRemove(t *testing.T, match string) {
	t.Helper()
	old := removeAll
	removeAll = func(path string) error {
		if _, err := os.Stat(path); err == nil && strings.HasSuffix(path, match) {
			return os.ErrPermission
		}
		return old(path)
	}
	t.Cleanup(func() { removeAll = old })
}

func lastDetail() (func(int, string, string), *string, *string) {
	var state, detail string
	return func(_ int, s, d string) { state, detail = s, d }, &state, &detail
}

func TestStaleTmpNotDeleted(t *testing.T) {
	engine, fixtures := needCore(t)
	it := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	dest := t.TempDir()
	o := Options{Mode: "p5z", Dest: dest}
	out := filepath.Join(dest, P5zName(it))
	os.WriteFile(out, []byte("viejo"), 0o644)
	plans := PlanCompress([]Item{it}, o, newFS())
	os.WriteFile(plans[0].Tmp, []byte("resto"), 0o644)
	failingRemove(t, ".maxpack-tmp")
	s := &states{list: map[int][]string{}}
	on, state, detail := lastDetail()
	r := &Runner{Core: engine, OnItem: func(i int, st, d string) { s.on(i, st, d); on(i, st, d) }}
	sum := r.Run(context.Background(), plans, o)
	if sum.Failed != 1 || *state != "error" || !strings.Contains(*detail, "tmp_not_deleted") || !strings.Contains(*detail, plans[0].Tmp) {
		t.Errorf("%+v %s %q", sum, *state, *detail)
	}
	if hasArg(s.list[0], "packing") {
		t.Errorf("the engine got to run: %v", s.list[0])
	}
	if data, _ := os.ReadFile(out); string(data) != "viejo" {
		t.Error("the old one changed")
	}
}

func TestDiscardFailureReported(t *testing.T) {
	engine, fixtures := needCore(t)
	it := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	dest := t.TempDir()
	o := Options{Mode: "p5z", Dest: dest}
	out := filepath.Join(dest, P5zName(it))
	os.WriteFile(out, []byte("viejo"), 0o644)
	plans := PlanCompress([]Item{it}, o, newFS())
	failingRemove(t, ".maxpack-tmp")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	on, state, detail := lastDetail()
	r := &Runner{Core: engine, OnItem: func(i int, st, d string) {
		on(i, st, d)
		if st == "verifying" {
			cancel()
		}
	}}
	r.Run(ctx, plans, o)
	if *state != "cancelled" || !strings.Contains(*detail, "cancelled") || !strings.Contains(*detail, "tmp_not_deleted") {
		t.Errorf("%s %q", *state, *detail)
	}
	if data, _ := os.ReadFile(out); string(data) != "viejo" {
		t.Error("the old one changed")
	}
}

func TestConsoleReplaceVerifies(t *testing.T) {
	engine, fixtures := needCore(t)
	src := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	p5z := filepath.Join(t.TempDir(), "juego.p5z")
	engineRun(t, engine, "pack", src.Path, "-o", p5z, "--p5z")
	it := describe(t, engine, p5z)
	for _, cancelAtVerify := range []bool{false, true} {
		dest := t.TempDir()
		o := Options{Mode: "console", Dest: dest}
		old := filepath.Join(dest, "TEST00001")
		os.MkdirAll(old, 0o755)
		os.WriteFile(filepath.Join(old, "viejo.txt"), []byte("viejo"), 0o644)
		plans := PlanDecompress([]Item{it}, o, fakeFS{free: 1000 * gib, typ: "NTFS"})
		if len(plans[0].Replace) == 0 {
			t.Fatal("does not replace")
		}
		ctx, cancel := context.WithCancel(context.Background())
		s := &states{list: map[int][]string{}}
		r := &Runner{Core: engine, OnItem: func(i int, st, d string) {
			s.on(i, st, d)
			if st == "verifying" && cancelAtVerify {
				cancel()
			}
		}}
		sum := r.Run(ctx, plans, o)
		cancel()
		got := strings.Join(s.list[0], ",")
		if !strings.Contains(got, "unpacking,verifying") {
			t.Errorf("not verified before the change: %s", got)
		}
		_, oldErr := os.Stat(filepath.Join(old, "viejo.txt"))
		entries, _ := os.ReadDir(dest)
		if cancelAtVerify {
			if sum.Done != 0 || oldErr != nil || len(entries) != 1 {
				t.Errorf("cancelado: %+v, viejo %v, %v", sum, oldErr, entries)
			}
		} else if sum.Done != 1 || oldErr == nil || len(entries) != 1 {
			t.Errorf("%+v, viejo %v, %v", sum, oldErr, entries)
		}
	}
}

// shortGrace makes Run kill an engine that does not stop soon after asking it to cancel.
func shortGrace(t *testing.T) {
	t.Helper()
	old := core.CancelGrace
	core.CancelGrace = 200 * time.Millisecond
	t.Cleanup(func() { core.CancelGrace = old })
}

// runCancelled runs the plans with the engine of the test binary and cancels at its first
// progress; it gives the summary and the last state and detail of the first game.
func runCancelled(t *testing.T, plans []Plan, o Options) (Summary, string, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	on, state, detail := lastDetail()
	r := &Runner{Core: os.Args[0], OnItem: on, OnEvent: func(int, core.Event) { cancel() }}
	sum := r.Run(ctx, plans, o)
	return sum, *state, *detail
}

// An engine that is killed after a cancel (it did not stop in time) leaves what it wrote; the
// Runner takes the output back to what it was: gone if it was not there, empty if it was an
// empty folder.
func TestKilledEngineLeavesNothing(t *testing.T) {
	t.Setenv(fakecore.Env, "writes-stubborn")
	shortGrace(t)
	for _, c := range []struct {
		mode  Mode
		empty bool
	}{{ModeP5z, false}, {ModeFolder, false}, {ModeFolder, true}} {
		dest := t.TempDir()
		o := Options{Mode: c.mode, Dest: dest}
		plans := PlanCompress([]Item{item("Juego", "TEST00001", 10)}, o, newFS())
		if c.empty {
			os.MkdirAll(plans[0].Out, 0o755)
		}
		sum, state, detail := runCancelled(t, plans, o)
		if sum.Cancelled != 1 || state != StateCancelled || detail != "cancelled" {
			t.Errorf("%s empty=%v: %+v %s %q", c.mode, c.empty, sum, state, detail)
		}
		entries, err := os.ReadDir(plans[0].Out)
		switch {
		case c.empty && (err != nil || len(entries) != 0):
			t.Errorf("%s: the empty folder was not left empty: %v %v", c.mode, entries, err)
		case !c.empty:
			if _, err := os.Stat(plans[0].Out); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s: the half-written output was left: %v", c.mode, err)
			}
		}
	}
}

// When what a killed engine wrote cannot be taken away, the detail says where it is.
func TestPartialNotDeletedReported(t *testing.T) {
	t.Setenv(fakecore.Env, "writes-stubborn")
	shortGrace(t)
	for _, c := range []struct {
		mode  Mode
		empty bool
		match string
	}{{ModeP5z, false, ".p5z"}, {ModeFolder, true, "eboot.bin"}} {
		dest := t.TempDir()
		o := Options{Mode: c.mode, Dest: dest}
		plans := PlanCompress([]Item{item("Juego", "TEST00001", 10)}, o, newFS())
		if c.empty {
			os.MkdirAll(plans[0].Out, 0o755)
		}
		failingRemove(t, c.match)
		_, state, detail := runCancelled(t, plans, o)
		if state != StateCancelled || !strings.Contains(detail, "partial_not_deleted: "+plans[0].Out) {
			t.Errorf("%s: %s %q", c.mode, state, detail)
		}
	}
}

// With Replace the engine writes in Tmp: a killed one leaves the old one as it was and no Tmp.
func TestKilledEngineKeepsOld(t *testing.T) {
	t.Setenv(fakecore.Env, "writes-stubborn")
	shortGrace(t)
	dest := t.TempDir()
	o := Options{Mode: ModeP5z, Dest: dest}
	it := item("Juego", "TEST00001", 10)
	out := filepath.Join(dest, P5zName(it))
	os.WriteFile(out, []byte("viejo"), 0o644)
	plans := PlanCompress([]Item{it}, o, newFS())
	if plans[0].Tmp == "" {
		t.Fatal("the plan does not replace")
	}
	_, state, detail := runCancelled(t, plans, o)
	if data, _ := os.ReadFile(out); string(data) != "viejo" || state != StateCancelled {
		t.Errorf("the old one: %q, %s %q", data, state, detail)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 1 {
		t.Errorf("extra files: %v", entries)
	}
}

// What is at the output with something in it when the game starts (it came after the plan) is
// not the Runner's: the engine refuses it, and it stays as it was.
func TestOutWithContentNeverTouched(t *testing.T) {
	engine, fixtures := needCore(t)
	it := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	for _, mode := range []Mode{ModeP5z, ModeFolder} {
		dest := t.TempDir()
		o := Options{Mode: mode, Dest: dest}
		plans := PlanCompress([]Item{it}, o, newFS())
		mine := plans[0].Out
		if mode == ModeFolder {
			os.MkdirAll(mine, 0o755)
			mine = filepath.Join(mine, "mio.txt")
		}
		os.WriteFile(mine, []byte("ajeno"), 0o644)
		on, state, detail := lastDetail()
		(&Runner{Core: engine, OnItem: on}).Run(context.Background(), plans, o)
		if data, _ := os.ReadFile(mine); string(data) != "ajeno" || *state != StateError {
			t.Errorf("%s: %q, %s %q", mode, data, *state, *detail)
		}
	}
}

// A game packed with a runtime that does not read zstd (none, or a wrong one) is made, but says
// that it will not start; the summary counts it. With the runtime of ampr-emu-ng it says nothing.
func TestRuntimeWithoutZstdWarned(t *testing.T) {
	engine, fixtures := needCore(t)
	it := describe(t, engine, filepath.Join(fixtures, "basic.exfat"))
	if it.HasZstdRuntime {
		t.Fatal("the test dump has a runtime")
	}
	good := filepath.Join(t.TempDir(), "bueno.sprx")
	os.WriteFile(good, append(bytes.Repeat([]byte{7}, 100), []byte("AMPRZPK1 resto")...), 0o644)
	wrong := filepath.Join(t.TempDir(), "viejo.sprx")
	os.WriteFile(wrong, bytes.Repeat([]byte{7}, 200), 0o644)
	for _, c := range []struct {
		runtime string
		warned  bool
	}{{"", true}, {wrong, true}, {good, false}} {
		o := Options{Mode: ModeP5z, Dest: t.TempDir(), Level: 1, Runtime: c.runtime}
		on, state, detail := lastDetail()
		sum := (&Runner{Core: engine, OnItem: on}).Run(context.Background(), PlanCompress([]Item{it}, o, newFS()), o)
		warned := strings.Contains(*detail, "runtime_without_zstd")
		if sum.Done != 1 || *state != StateOK || warned != c.warned || (sum.NoZstdRuntime == 1) != c.warned {
			t.Errorf("runtime %q: %+v, %s %q", c.runtime, sum, *state, *detail)
		}
	}
}

// A .p5z needs the TITLE_ID: the installer refuses one without it. A dump whose param.json does
// not give it is warned of before it starts; a folder is named after its folder all the same.
func TestNoTitleIdWarned(t *testing.T) {
	dest := t.TempDir()
	it := item("Sin ID", "", 10)
	p := PlanCompress([]Item{it}, Options{Mode: ModeP5z, Dest: dest}, newFS())[0]
	if !hasWarning(p, "no_title_id") || p.Block != "" {
		t.Errorf("p5z: %v %q", p.Warnings, p.Block)
	}
	p = PlanCompress([]Item{item("Con ID", "TEST00001", 10)}, Options{Mode: ModeP5z, Dest: dest}, newFS())[0]
	if hasWarning(p, "no_title_id") {
		t.Errorf("with ID: %v", p.Warnings)
	}
}

// When the engine refuses with "exists", what is at the output is not ours whatever the Runner
// saw before: it is not removed, nor emptied. With Replace only Tmp goes.
func TestExistsRefusalNeverCleansOut(t *testing.T) {
	t.Setenv(fakecore.Env, "exists")
	for _, c := range []struct {
		mode  Mode
		empty bool
	}{{ModeP5z, false}, {ModeFolder, false}, {ModeFolder, true}} {
		dest := t.TempDir()
		o := Options{Mode: c.mode, Dest: dest}
		plans := PlanCompress([]Item{item("Juego", "TEST00001", 10)}, o, newFS())
		if c.empty {
			os.MkdirAll(plans[0].Out, 0o755)
		}
		on, state, detail := lastDetail()
		(&Runner{Core: os.Args[0], OnItem: on}).Run(context.Background(), plans, o)
		if *state != StateError || *detail != "exists" {
			t.Errorf("%s empty=%v: %s %q", c.mode, c.empty, *state, *detail)
		}
		if _, err := os.Stat(plans[0].Out); err != nil {
			t.Errorf("%s empty=%v: the foreign output disappeared: %v", c.mode, c.empty, err)
		}
		if c.mode == ModeFolder {
			if _, err := os.Stat(filepath.Join(plans[0].Out, "eboot.bin")); err != nil {
				t.Errorf("%s: the foreign file disappeared: %v", c.mode, err)
			}
		}
	}
}

func TestExistsRefusalReplaceCleansOnlyTmp(t *testing.T) {
	t.Setenv(fakecore.Env, "exists")
	dest := t.TempDir()
	o := Options{Mode: ModeP5z, Dest: dest}
	it := item("Juego", "TEST00001", 10)
	out := filepath.Join(dest, P5zName(it))
	os.WriteFile(out, []byte("viejo"), 0o644)
	plans := PlanCompress([]Item{it}, o, newFS())
	if plans[0].Tmp == "" {
		t.Fatal("the plan does not replace")
	}
	(&Runner{Core: os.Args[0]}).Run(context.Background(), plans, o)
	if data, _ := os.ReadFile(out); string(data) != "viejo" {
		t.Errorf("the old one: %q", data)
	}
	if _, err := os.Stat(plans[0].Tmp); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Tmp was left: %v", err)
	}
}

// Only "does not exist" is "nothing there": any other failure to look at the output (access, I/O,
// sharing) counts as full, so it is never a candidate for deletion.
func TestOutStateUnreadableIsFull(t *testing.T) {
	oldL, oldR := lstat, readDir
	t.Cleanup(func() { lstat, readDir = oldL, oldR })
	dir := t.TempDir()
	if got := outState(filepath.Join(dir, "no")); got != outMissing {
		t.Errorf("inexistente: %d", got)
	}
	lstat = func(string) (fs.FileInfo, error) { return nil, fs.ErrPermission }
	if got := outState(dir); got != outFull {
		t.Errorf("Lstat falla: %d", got)
	}
	lstat = oldL
	readDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("EIO") }
	if got := outState(dir); got != outFull {
		t.Errorf("ReadDir falla: %d", got)
	}
}

// With an output that cannot be looked at, a failed game takes nothing away.
func TestUnreadableOutNotCleaned(t *testing.T) {
	t.Setenv(fakecore.Env, "writes-stubborn")
	shortGrace(t)
	oldL := lstat
	t.Cleanup(func() { lstat = oldL })
	dest := t.TempDir()
	o := Options{Mode: ModeP5z, Dest: dest}
	plans := PlanCompress([]Item{item("Juego", "TEST00001", 10)}, o, newFS())
	lstat = func(p string) (fs.FileInfo, error) {
		if p == plans[0].Out {
			return nil, fs.ErrPermission
		}
		return oldL(p)
	}
	runCancelled(t, plans, o)
	if _, err := os.Stat(plans[0].Out); err != nil {
		t.Errorf("what could not be read was deleted: %v", err)
	}
}
