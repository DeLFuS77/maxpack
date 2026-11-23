// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
)

// TestMain lets the test binary act as an engine (see fakecore).
func TestMain(m *testing.M) {
	fakecore.Main()
	os.Exit(m.Run())
}
