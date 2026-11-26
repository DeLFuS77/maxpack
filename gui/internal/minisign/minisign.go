// SPDX-License-Identifier: GPL-3.0-or-later

// Package minisign checks signatures made with minisign (https://jedisct1.github.io/minisign/), as the
// releases of MaxPack sign their SHA256SUMS, with Ed25519 from Go's standard library. A signature file
// is four lines:
//
//	untrusted comment: <anything>
//	base64 of "Ed" or "ED" (prehashed with BLAKE2b-512), the key ID (8 bytes) and the Ed25519 signature
//	trusted comment: <text>
//	base64 of the Ed25519 signature of the signature above followed by the trusted comment's text
//
// Both signatures must be valid for the key, and the key ID must be the key's.
package minisign

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrFormat: what was given is not a minisign public key or signature.
	ErrFormat = errors.New("not in the format of minisign")
	// ErrOtherKey: the signature was made with another key.
	ErrOtherKey = errors.New("signed with another key")
	// ErrSignature: the signature is not the key's for this file.
	ErrSignature = errors.New("the signature does not match the file")
	// ErrTrustedComment: the trusted comment is not the one that was signed.
	ErrTrustedComment = errors.New("the trusted comment was changed")
)

// maxLine is the longest line a key or signature file may have (a trusted comment is short).
const maxLine = 8192

// PublicKey is a minisign public key: its ID and its Ed25519 key.
type PublicKey struct {
	ID  [8]byte
	Key ed25519.PublicKey
}

// ParsePublicKey reads a public key: the base64 line of minisign.pub, or the whole file (its untrusted
// comment first).
func ParsePublicKey(text string) (PublicKey, error) {
	lines := splitLines(text)
	if len(lines) > 0 && strings.HasPrefix(lines[0], "untrusted comment:") {
		lines = lines[1:]
	}
	var key PublicKey
	if len(lines) == 0 {
		return key, fmt.Errorf("public key: %w", ErrFormat)
	}
	raw, err := base64.StdEncoding.DecodeString(lines[0])
	if err != nil || len(raw) != 2+8+ed25519.PublicKeySize || string(raw[:2]) != "Ed" {
		return key, fmt.Errorf("public key: %w", ErrFormat)
	}
	for _, rest := range lines[1:] {
		if rest != "" {
			return key, fmt.Errorf("public key: %w", ErrFormat)
		}
	}
	copy(key.ID[:], raw[2:10])
	key.Key = ed25519.PublicKey(raw[10:])
	return key, nil
}

// Verify checks that sig (the content of a .minisig file) is key's signature of message, and that its
// trusted comment is the one signed; it gives the trusted comment.
func Verify(key PublicKey, message, sig []byte) (trusted string, err error) {
	lines := splitLines(string(sig))
	for len(lines) > 4 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "untrusted comment: ") || !strings.HasPrefix(lines[2], "trusted comment: ") {
		return "", fmt.Errorf("signature: %w", ErrFormat)
	}
	for _, line := range lines {
		if len(line) > maxLine {
			return "", fmt.Errorf("signature: %w", ErrFormat)
		}
	}
	raw, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil || len(raw) != 2+8+ed25519.SignatureSize {
		return "", fmt.Errorf("signature: %w", ErrFormat)
	}
	algorithm, id, signature := string(raw[:2]), raw[2:10], raw[10:]
	if algorithm != "Ed" && algorithm != "ED" {
		return "", fmt.Errorf("signature: %w", ErrFormat)
	}
	global, err := base64.StdEncoding.DecodeString(lines[3])
	if err != nil || len(global) != ed25519.SignatureSize {
		return "", fmt.Errorf("signature: %w", ErrFormat)
	}
	if !bytes.Equal(id, key.ID[:]) || len(key.Key) != ed25519.PublicKeySize {
		return "", ErrOtherKey
	}
	signed := message
	if algorithm == "ED" {
		sum := Blake2b512(message)
		signed = sum[:]
	}
	if !ed25519.Verify(key.Key, signed, signature) {
		return "", ErrSignature
	}
	trusted = strings.TrimPrefix(lines[2], "trusted comment: ")
	if !ed25519.Verify(key.Key, append(append([]byte{}, signature...), trusted...), global) {
		return "", ErrTrustedComment
	}
	return trusted, nil
}

// splitLines splits text into its lines, without their \r (a file written on Windows) and without the
// spaces around the whole.
func splitLines(text string) []string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, "\r")
	}
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}
