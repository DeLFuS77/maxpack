// SPDX-License-Identifier: GPL-3.0-or-later

// Package minisigntest signs in the format of minisign, for the tests only: MaxPack itself only checks
// signatures. It holds no key; each test brings its own.
package minisigntest

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"

	"github.com/DeLFuS77/maxpack/gui/internal/minisign"
)

// PublicKey is the public key as minisign writes it in minisign.pub (its second line): "Ed", the key ID
// and the key, in base64.
func PublicKey(public ed25519.PublicKey, id [8]byte) string {
	raw := append(append([]byte("Ed"), id[:]...), public...)
	return base64.StdEncoding.EncodeToString(raw)
}

// Sign is the .minisig of message as `minisign -S` writes it: prehashed (BLAKE2b-512 of the file, "ED", the
// default of minisign since 0.10) or legacy ("Ed", `-l`), with the trusted comment given and its global
// signature.
func Sign(private ed25519.PrivateKey, id [8]byte, message []byte, prehashed bool, trusted string) []byte {
	algorithm, signed := "Ed", message
	if prehashed {
		sum := minisign.Blake2b512(message)
		algorithm, signed = "ED", sum[:]
	}
	signature := ed25519.Sign(private, signed)
	line := append(append([]byte(algorithm), id[:]...), signature...)
	global := ed25519.Sign(private, append(append([]byte{}, signature...), trusted...))
	return []byte(fmt.Sprintf("untrusted comment: signature from minisign secret key\n%s\ntrusted comment: %s\n%s\n",
		base64.StdEncoding.EncodeToString(line), trusted, base64.StdEncoding.EncodeToString(global)))
}
