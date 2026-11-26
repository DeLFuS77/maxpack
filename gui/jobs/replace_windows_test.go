// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A swap that meets a program holding the old one without delete sharing (the Explorer making its cover, an
// antivirus) for a moment waits for it, instead of throwing the verified new one away (M4).
func TestSwapWaitsForAHolder(t *testing.T) {
	dir := t.TempDir()
	out, tmp := filepath.Join(dir, "g.p5z"), filepath.Join(dir, "g.p5z"+TmpSuffix)
	os.WriteFile(out, []byte("the old one"), 0o644)
	os.WriteFile(tmp, []byte("the new one"), 0o644)
	name, _ := windows.UTF16PtrFromString(out)
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(700*time.Millisecond, func() { windows.CloseHandle(h) })
	if _, err := swapIf(out, tmp, nil); err != nil {
		t.Fatalf("it did not wait: %v", err)
	}
	if data, _ := os.ReadFile(out); string(data) != "the new one" {
		t.Errorf("%q", data)
	}
}
