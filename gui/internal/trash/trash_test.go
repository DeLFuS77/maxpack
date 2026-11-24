// SPDX-License-Identifier: GPL-3.0-or-later

package trash

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// What is not there cannot go: the error says so, and is not one of a drive without a bin.
func TestMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for name, call := range map[string]func(string) error{"Move": Move, "Check": Check} {
		err := call(missing)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: %v", name, err)
		}
		if _, ok := Why(err); ok {
			t.Errorf("%s: a missing path is told as a drive without a bin: %v", name, err)
		}
	}
}

// Check of a small file answers either that it can go, or why its drive cannot take it; it
// never moves it.
func TestCheckMovesNothing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.bin")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(file); err != nil {
		if _, ok := Why(err); !ok {
			t.Errorf("Check: %v", err)
		}
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("Check moved it: %v", err)
	}
}

// Moving for real goes to the bin of the user, so it is only done when asked:
// MP_TEST_TRASH=1 go test ./internal/trash/
func TestMoveForReal(t *testing.T) {
	if os.Getenv("MP_TEST_TRASH") != "1" {
		t.Skip("MP_TEST_TRASH=1 moves a small file and a small folder to the recycle bin of this user")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "maxpack-trash-test.bin")
	folder := filepath.Join(dir, "maxpack-trash-test")
	os.WriteFile(file, []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(folder, "sce_sys"), 0o755)
	os.WriteFile(filepath.Join(folder, "sce_sys", "param.json"), []byte("{}"), 0o644)
	for _, path := range []string{file, folder} {
		if err := Move(path); err != nil {
			if why, ok := Why(err); ok {
				t.Skipf("%s: %s (%s)", path, why.Reason, why.Drive)
			}
			t.Fatalf("%s: %v", path, err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is still there: %v", path, err)
		}
	}
}
