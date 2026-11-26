// SPDX-License-Identifier: GPL-3.0-or-later

package minisign_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/minisign"
	"github.com/DeLFuS77/maxpack/gui/internal/minisign/minisigntest"
)

// BLAKE2b-512 against RFC 7693 (Appendix A, "abc") and the empty message, and across the edges of its
// 128-byte blocks (the same as hashing it in two halves cannot be checked here: the vectors below, which
// minisign made, check the prehashed signatures over it).
func TestBlake2b512(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", "786a02f742015903c6c6fd852552d272912f4740e15847618a86e217f71f5419d25e1031afee585313896444934eb04b903a685b1448b755d56f701afe9be2ce"},
		{"abc", "ba80a53f981c4d0d6a2797b69f12f6e94c212f14685ac4b74b12bb6fdbffa2d17d87c5392aab792dc252d5de4533cc9518d38aa8dbf1925ab92386edd4009923"},
	} {
		sum := minisign.Blake2b512([]byte(c.in))
		if got := hex.EncodeToString(sum[:]); got != c.want {
			t.Errorf("BLAKE2b-512(%q) = %s", c.in, got)
		}
	}
	seen := map[[64]byte]int{}
	for n := 0; n <= 300; n++ {
		sum := minisign.Blake2b512(bytes.Repeat([]byte{0x5a}, n))
		if other, ok := seen[sum]; ok {
			t.Fatalf("%d and %d bytes give the same sum", other, n)
		}
		seen[sum] = n
	}
}

// Test vectors made with the minisign command, from the tests of jedisct1/go-minisign: a public key of
// those tests (its secret key is not known here), and the message "test" signed with it, legacy ("Ed") and
// prehashed ("ED").
const (
	vectorKey    = "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3"
	vectorLegacy = "untrusted comment: signature from minisign secret key\nRWQf6LRCGA9i59SLOFxz6NxvASXDJeRtuZykwQepbDEGt87ig1BNpWaVWuNrm73YiIiJbq71Wi+dP9eKL8OC351vwIasSSbXxwA=\ntrusted comment: timestamp:1635442742\tfile:test\n0YteLgV960ia80vnA/fHbvkyjl/IoP/HNOCaZfrF0CdhAlp7ok+Tpkya+VpWPX5C/Is3q8a/kEDSY7fBmmgJCg==\n"
	vectorHashed = "untrusted comment: signature from minisign secret key\nRUQf6LRCGA9i559r3g7V1qNyJDApGip8MfqcadIgT9CuhV3EMhHoN1mGTkUidF/z7SrlQgXdy8ofjb7bNJJylDOocrCo8KLzZwo=\ntrusted comment: timestamp:1635443258\tfile:test\thashed\n/cj37GK60vryibFn+ftOgbCvW9NKhKYgjVpFFQUcWPAnjO23wrvVDTt7cloNC06maoBli9q6qwZDXXoaxweICQ==\n"
)

// What the minisign command signs is taken, legacy and prehashed, with its trusted comment; anything changed
// in the file, the signature or the trusted comment is refused, and so is a signature of another key.
func TestMinisignVectors(t *testing.T) {
	key, err := minisign.ParsePublicKey(vectorKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, sig, trusted string }{
		{"legacy", vectorLegacy, "timestamp:1635442742\tfile:test"},
		{"prehashed", vectorHashed, "timestamp:1635443258\tfile:test\thashed"},
	} {
		trusted, err := minisign.Verify(key, []byte("test"), []byte(c.sig))
		if err != nil || trusted != c.trusted {
			t.Errorf("%s: %q, %v", c.name, trusted, err)
		}
		// Windows line ends, as a file saved there may have
		if _, err := minisign.Verify(key, []byte("test"), []byte(strings.ReplaceAll(c.sig, "\n", "\r\n"))); err != nil {
			t.Errorf("%s with CRLF: %v", c.name, err)
		}
		if _, err := minisign.Verify(key, []byte("tesT"), []byte(c.sig)); !errors.Is(err, minisign.ErrSignature) {
			t.Errorf("%s: another file: %v", c.name, err)
		}
		forged := strings.Replace(c.sig, "file:test", "file:tost", 1)
		if _, err := minisign.Verify(key, []byte("test"), []byte(forged)); !errors.Is(err, minisign.ErrTrustedComment) {
			t.Errorf("%s: a trusted comment changed: %v", c.name, err)
		}
	}
	other := key
	other.ID[0] ^= 1
	if _, err := minisign.Verify(other, []byte("test"), []byte(vectorHashed)); !errors.Is(err, minisign.ErrOtherKey) {
		t.Errorf("a signature of another key: %v", err)
	}
	other = key
	other.Key = append(ed25519.PublicKey{}, key.Key...)
	other.Key[3] ^= 1
	if _, err := minisign.Verify(other, []byte("test"), []byte(vectorHashed)); !errors.Is(err, minisign.ErrSignature) {
		t.Errorf("another key with the same ID: %v", err)
	}
}

// testSeed makes the key of these tests only: it signs nothing but what the tests make up.
var testSeed = []byte("maxpack test key, never a real one")[:32]

func testKey() (ed25519.PrivateKey, [8]byte) {
	return ed25519.NewKeyFromSeed(testSeed), [8]byte{'t', 'e', 's', 't', 'k', 'e', 'y', '1'}
}

// The test key signs as the minisign command does, legacy and prehashed; a whole minisign.pub is taken too.
func TestTestKeyRoundTrip(t *testing.T) {
	private, id := testKey()
	line := minisigntest.PublicKey(private.Public().(ed25519.PublicKey), id)
	for _, text := range []string{line, "untrusted comment: minisign public key 316579656B747365\n" + line + "\n", " " + line + "\r\n"} {
		key, err := minisign.ParsePublicKey(text)
		if err != nil || key.ID != id {
			t.Fatalf("%q: %v", text, err)
		}
		message := []byte("3f2a…  maxpack-windows-x64-setup.exe\n")
		for _, prehashed := range []bool{false, true} {
			if _, err := minisign.Verify(key, message, minisigntest.Sign(private, id, message, prehashed, "timestamp:1\tfile:SHA256SUMS")); err != nil {
				t.Errorf("prehashed %v: %v", prehashed, err)
			}
		}
	}
}

// Keys and signatures that are not minisign's are refused before anything is checked.
func TestMalformed(t *testing.T) {
	for _, text := range []string{"", "not base64!", "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7", "UlFm" + vectorKey[4:],
		"untrusted comment: only the comment\n"} {
		if _, err := minisign.ParsePublicKey(text); !errors.Is(err, minisign.ErrFormat) {
			t.Errorf("key %q: %v", text, err)
		}
	}
	key, _ := minisign.ParsePublicKey(vectorKey)
	lines := strings.Split(vectorHashed, "\n")
	for name, sig := range map[string]string{
		"empty":                     "",
		"three lines":               strings.Join(lines[:3], "\n"),
		"no untrusted comment":      strings.Join(append([]string{"comment: x"}, lines[1:]...), "\n"),
		"no trusted comment":        strings.Join([]string{lines[0], lines[1], "comment: x", lines[3]}, "\n"),
		"signature cut":             strings.Join([]string{lines[0], lines[1][:40], lines[2], lines[3]}, "\n"),
		"global signature cut":      strings.Join([]string{lines[0], lines[1], lines[2], lines[3][:40]}, "\n"),
		"another algorithm":         strings.Join([]string{lines[0], "RXQ" + lines[1][3:], lines[2], lines[3]}, "\n"),
		"something after":           vectorHashed + "more\n",
		"a line too long":           strings.Repeat("x", 9000),
		"not base64 in a signature": strings.Join([]string{lines[0], "!!" + lines[1][2:], lines[2], lines[3]}, "\n"),
	} {
		if _, err := minisign.Verify(key, []byte("test"), []byte(sig)); !errors.Is(err, minisign.ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
