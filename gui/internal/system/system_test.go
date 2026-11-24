// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A file or a folder is hidden while MaxPack writes it, and shown again once it is done: on
// Windows by its attribute; elsewhere nothing changes and nothing fails.
func TestSetHidden(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "game.p5z.maxpack-tmp")
	folder := filepath.Join(dir, "PPSA10001.maxpack-tmp")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, folder} {
		if err := SetHidden(path, true); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if runtime.GOOS == "windows" && !IsHidden(path) {
			t.Errorf("%s is not hidden", path)
		}
		if err := SetHidden(path, false); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if IsHidden(path) {
			t.Errorf("%s is still hidden", path)
		}
	}
	if err := SetHidden(filepath.Join(dir, "missing"), true); err == nil && runtime.GOOS == "windows" {
		t.Error("a path that is not there was hidden")
	}
}

// What a report says of the PC: the system and its version, and its memory.
func TestAboutThePC(t *testing.T) {
	version := OSVersion()
	if strings.TrimSpace(version) == "" {
		t.Fatal("no system version")
	}
	if runtime.GOOS == "windows" && (!strings.HasPrefix(version, "Windows ") || !strings.Contains(version, "10.0.")) {
		t.Errorf("Windows says %q", version)
	}
	if memory := TotalMemory(); memory < 256<<20 {
		t.Errorf("memory: %d bytes", memory)
	}
}

// Windows 11 still says 10.0: its builds from 22000 on are 11.
func TestWindowsName(t *testing.T) {
	for _, c := range []struct {
		major, minor, build uint32
		want                string
	}{
		{10, 0, 19045, "Windows 10 (10.0.19045)"},
		{10, 0, 22000, "Windows 11 (10.0.22000)"},
		{10, 0, 26200, "Windows 11 (10.0.26200)"},
		{6, 3, 9600, "Windows 6.3.9600"},
	} {
		if got := windowsName(c.major, c.minor, c.build); got != c.want {
			t.Errorf("%d.%d.%d: %q, expected %q", c.major, c.minor, c.build, got, c.want)
		}
	}
}
