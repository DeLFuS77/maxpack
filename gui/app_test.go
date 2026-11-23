// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreMissingReported(t *testing.T) {
	t.Setenv("MAXPACK_CORE", filepath.Join(t.TempDir(), "no-existe.exe"))
	setup := (&App{}).Setup()
	if setup.CoreError == "" {
		t.Fatal("falta el motor y no se dice")
	}
	if setup.Version == "" || setup.Lang == "" || setup.Theme != "system" {
		t.Fatalf("setup incompleto: %+v", setup)
	}
}

func TestForwardArgs(t *testing.T) {
	core := os.Getenv("MAXPACK_CORE")
	if core == "" {
		t.Skip("falta MAXPACK_CORE: se necesita el motor")
	}
	exe := filepath.Join(t.TempDir(), "MaxPack.exe")
	build := exec.Command("go", "build", "-tags", "desktop,production", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("no compila: %v\n%s", err, out)
	}
	want, err := exec.Command(core, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command(exe, "--version").Output()
	if err != nil || strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("--version: %q, %v (esperado %q)", got, err, want)
	}
	err = exec.Command(exe, "pack").Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("pack sin argumentos debería salir con 2: %v", err)
	}
}
