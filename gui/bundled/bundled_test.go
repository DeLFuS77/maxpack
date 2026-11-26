// SPDX-License-Identifier: GPL-3.0-or-later

package bundled

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The libSceAmpr.sprx inside the program is the one of the release v0.5.1-maxpack.2 of
// ampr-emu-ng, byte for byte: its SHA-256 and size, as SHA256SUMS and VERSION of the release say,
// and it reads zstd (it carries AMPRZPK1, as the engine and ChooseRuntime look for it).
func TestRuntimeIsTheRelease(t *testing.T) {
	sum := sha256.Sum256(sprx)
	if got := hex.EncodeToString(sum[:]); got != SHA256 || SHA256 != "1146b293cea1653b77895b98c1b1a2056db4d7e041ae9b81ade34fcdf146a893" {
		t.Fatalf("SHA-256 %s, expected %s", got, SHA256)
	}
	if len(sprx) != 720048 {
		t.Errorf("%d bytes, expected 720048", len(sprx))
	}
	if !bytes.Contains(sprx, []byte("AMPRZPK1")) {
		t.Error("it does not read zstd: AMPRZPK1 is missing")
	}
	if Version() != "v0.5.1-maxpack.2" {
		t.Errorf("version %q", Version())
	}
	sums, err := os.ReadFile(filepath.Join("ampr-emu-ng", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, line := range strings.Split(strings.ReplaceAll(string(sums), "\r\n", "\n"), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == FileName {
			listed = fields[0] == SHA256
		}
	}
	if !listed {
		t.Errorf("SHA256SUMS does not give %s for %s:\n%s", SHA256, FileName, sums)
	}
	source, err := os.ReadFile(filepath.Join("ampr-emu-ng", "SOURCE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"https://github.com/DeLFuS77/ampr-emu-ng", "version: " + Version(), "tag " + Version()} {
		if !strings.Contains(string(source), want) {
			t.Errorf("SOURCE does not say %q", want)
		}
	}
}

// Install writes the runtime once to <folder>/ampr-emu-ng/libSceAmpr.sprx: only when it is missing
// or is not this one, whole or not at all, and gives its path.
func TestInstallOnce(t *testing.T) {
	folder := t.TempDir()
	path, err := Install(folder)
	if want := filepath.Join(folder, "ampr-emu-ng", FileName); err != nil || path != want {
		t.Fatalf("%q, %v (expected %q)", path, err, want)
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, sprx) {
		t.Fatalf("what was written is not the runtime (%d bytes)", len(data))
	}
	// the same one is not written again
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if again, err := Install(folder); err != nil || again != path {
		t.Fatalf("again: %q, %v", again, err)
	}
	if info, _ := os.Stat(path); !info.ModTime().Equal(old) {
		t.Errorf("the same one was written again (%v)", info.ModTime())
	}
	// another one (an older release, a damaged copy) is put back
	for _, other := range [][]byte{[]byte("viejo"), append(append([]byte{}, sprx[:len(sprx)-1]...), sprx[len(sprx)-1]^1)} {
		if err := os.WriteFile(path, other, 0o644); err != nil {
			t.Fatal(err)
		}
		if again, err := Install(folder); err != nil || again != path {
			t.Fatalf("over another: %q, %v", again, err)
		}
		if data, _ := os.ReadFile(path); !bytes.Equal(data, sprx) {
			t.Errorf("another one (%d bytes) was not replaced", len(other))
		}
	}
	// nothing else is left in its folder
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("left in the folder: %v", entries)
	}
}

// Where it cannot be written, Install says so and gives no path.
func TestInstallFails(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if path, err := Install(blocked); err == nil || path != "" {
		t.Errorf("inside a file: %q, %v", path, err)
	}
}
