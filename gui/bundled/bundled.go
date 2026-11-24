// SPDX-License-Identifier: GPL-3.0-or-later

// Package bundled is the ampr-emu-ng that comes with MaxPack: the libSceAmpr.sprx of one of its
// releases, kept in ampr-emu-ng/ (VERSION says which, SOURCE where it comes from and SHA256SUMS is
// the release's own) and inside the program. The window writes it to its own folder (Install), so
// the engine can be given its path, and uses it for the games that do not carry one when the user
// did not choose another in Settings.
package bundled

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the name of the runtime, the one the engine puts in fakelib/.
const FileName = "libSceAmpr.sprx"

// SHA256 is the SHA-256 of the libSceAmpr.sprx of the release, as its SHA256SUMS gives it.
const SHA256 = "e24b5533ec8c51db9b08c0d3ef709b328c7fd1571f5a906addbd652dfebad461"

//go:embed ampr-emu-ng/libSceAmpr.sprx
var sprx []byte

//go:embed ampr-emu-ng/VERSION
var version string

// Version is the release of ampr-emu-ng that comes with MaxPack ("v0.5.1-maxpack.1").
func Version() string { return strings.TrimSpace(version) }

// Install puts the runtime in <folder>/ampr-emu-ng/libSceAmpr.sprx and gives its path. It is
// written only when it is not there or is not this one (another release, a damaged copy), and
// whole or not at all: a temporary file, then renamed over the old one.
func Install(folder string) (string, error) {
	if sum := sha256.Sum256(sprx); hex.EncodeToString(sum[:]) != SHA256 {
		return "", fmt.Errorf("the bundled %s is not the one of ampr-emu-ng %s", FileName, Version())
	}
	folder = filepath.Join(folder, "ampr-emu-ng")
	path := filepath.Join(folder, FileName)
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, sprx) {
		return path, nil
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(folder, FileName+".*.tmp")
	if err != nil {
		return "", err
	}
	_, err = temp.Write(sprx)
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp.Name(), path)
	}
	if err != nil {
		os.Remove(temp.Name())
		return "", err
	}
	return path, nil
}
