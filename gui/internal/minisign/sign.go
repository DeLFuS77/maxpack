// SPDX-License-Identifier: GPL-3.0-or-later

package minisign

// Signing, for tools/releasesign only: the window never signs. Ported from NXPress, by the same author, whose
// releases are signed with the same keys and the same tool.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Sign signs data as minisign does ("ED": Ed25519 over the BLAKE2b-512 of data), with a trusted comment
// signed too (the global signature).
func Sign(private ed25519.PrivateKey, id [8]byte, data []byte, comment string) []byte {
	sum := Blake2b512(data)
	signature := ed25519.Sign(private, sum[:])
	global := ed25519.Sign(private, append(append([]byte{}, signature...), comment...))
	head := append(append([]byte("ED"), id[:]...), signature...)
	var out bytes.Buffer
	fmt.Fprintf(&out, "untrusted comment: signature from the release key of MaxPack\n%s\ntrusted comment: %s\n%s\n",
		base64.StdEncoding.EncodeToString(head), comment, base64.StdEncoding.EncodeToString(global))
	return out.Bytes()
}

// The private release key, as the author keeps it in a password manager and gives it to tools/releasesign on
// their own computer: its ID and the seed of its Ed25519 key, in base64. It never goes on GitHub, into a file
// of MaxPack, nor into a log.

// ParsePrivateKey reads the private release key.
func ParsePrivateKey(text string) (ed25519.PrivateKey, [8]byte, error) {
	var id [8]byte
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(data) != 8+ed25519.SeedSize {
		return nil, id, errors.New("not a release key of MaxPack")
	}
	copy(id[:], data[:8])
	return ed25519.NewKeyFromSeed(data[8:]), id, nil
}

// PrivateKeyText is the private release key as the author keeps it.
func PrivateKeyText(private ed25519.PrivateKey, id [8]byte) string {
	return base64.StdEncoding.EncodeToString(append(append([]byte{}, id[:]...), private.Seed()...))
}
