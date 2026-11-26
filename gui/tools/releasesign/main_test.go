// SPDX-License-Identifier: GPL-3.0-or-later

package main

// Ported from NXPress, by the same author. Nothing here goes on the network nor uses a real key: gh and git
// are stand-ins, and each key is made for the test.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/minisign"
	"github.com/DeLFuS77/maxpack/gui/internal/update"
)

const (
	testCommit = "1111111111111111111111111111111111111111"
	testTagObj = "2222222222222222222222222222222222222222"
)

// the files of a release of MaxPack, as the build workflow drafts it
var programs = []string{"maxpack-windows-x64-setup.exe", "maxpack-windows-x64-portable.zip", "maxpack-macos-apple-silicon.dmg",
	"maxpack-macos-apple-silicon.tar.gz", "maxpack-macos-intel.dmg", "maxpack-macos-intel.tar.gz", "maxpack-linux-x64.tar.gz"}

// fakeRelease is a draft on GitHub as the workflow leaves it, and the gh and git that see it.
type fakeRelease struct {
	files      map[string][]byte
	draft      bool
	localTag   string
	remoteTag  string
	unattested string
	attested   [][]string
	uploaded   []byte
	published  bool
	keyAsked   bool
	asked      []string
}

func newFakeRelease() *fakeRelease {
	f := &fakeRelease{draft: true, localTag: testCommit, remoteTag: testCommit, files: map[string][]byte{}}
	sums := ""
	for _, name := range programs {
		f.files[name] = []byte("the file " + name)
		sum := sha256.Sum256(f.files[name])
		sums += hex.EncodeToString(sum[:]) + "  " + name + "\n"
	}
	f.files[sumsName] = []byte(sums)
	return f
}

func (f *fakeRelease) tools(secret string, yes bool, out *bytes.Buffer) tools {
	return tools{
		out: out,
		git: func(args ...string) ([]byte, error) {
			f.asked = append(f.asked, "git "+strings.Join(args, " "))
			switch args[0] {
			case "rev-parse":
				if f.localTag == "" || args[len(args)-1] != "refs/tags/v1.2.3^{commit}" {
					return nil, errors.New("no tag")
				}
				return []byte(f.localTag + "\n"), nil
			case "ls-remote":
				return []byte(fmt.Sprintf("%s\trefs/tags/v1.2.3\n%s\trefs/tags/v1.2.3^{}\n", testTagObj, f.remoteTag)), nil
			}
			return nil, fmt.Errorf("git %v", args)
		},
		gh: func(args ...string) ([]byte, error) {
			f.asked = append(f.asked, "gh "+strings.Join(args, " "))
			switch strings.Join(args[:2], " ") {
			case "release view":
				if args[2] != "v1.2.3" {
					return nil, errors.New("no such release")
				}
				r := map[string]any{"isDraft": f.draft}
				assets := []map[string]string{}
				for name := range f.files {
					assets = append(assets, map[string]string{"name": name})
				}
				r["assets"] = assets
				return json.Marshal(r)
			case "release download":
				folder := args[slices.Index(args, "-D")+1]
				for name, data := range f.files {
					os.WriteFile(filepath.Join(folder, name), data, 0o644)
				}
				return nil, nil
			case "attestation verify":
				f.attested = append(f.attested, args)
				if filepath.Base(args[2]) == f.unattested {
					return nil, errors.New("no attestation")
				}
				return nil, nil
			case "release upload":
				data, err := os.ReadFile(args[3])
				f.uploaded = data
				return nil, err
			case "release edit":
				f.published = true
				return nil, nil
			}
			return nil, fmt.Errorf("gh %v", args)
		},
		readKey: func(string) (string, error) {
			f.keyAsked = true
			return secret, nil
		},
		confirm: func(string) bool { return yes },
	}
}

// testKey is a key made for one test: its private text, as the author keeps the real one, and its public key.
func testKey() (string, minisign.PublicKey) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	var id [8]byte
	rand.Read(id[:])
	return minisign.PrivateKeyText(private, id), minisign.PublicKey{ID: id, Key: public}
}

var draft = options{repo: defaultRepo, workflow: defaultWorkflow, version: "1.2.3"}

// A draft whose files match SHA256SUMS and were built from the commit of the tag is signed with the version in
// its trusted comment ("MaxPack 1.2.3", what the window requires), by the main key or the spare one, and
// published once told so; the key is never shown.
func TestSignADraft(t *testing.T) {
	secret, key := testKey()
	spare, spareKey := testKey()
	keys := []minisign.PublicKey{key, spareKey}
	for _, c := range []struct {
		secret string
		yes    bool
	}{{secret, true}, {secret, false}, {spare, true}} {
		f := newFakeRelease()
		var out bytes.Buffer
		if err := run(f.tools(c.secret, c.yes, &out), draft, keys); err != nil {
			t.Fatal(err)
		}
		comment, err := minisign.VerifyAny(keys, f.files[sumsName], f.uploaded)
		if err != nil || comment != "MaxPack 1.2.3" {
			t.Fatalf("signature: %q %v", comment, err)
		}
		if f.published != c.yes {
			t.Errorf("published %v, told %v", f.published, c.yes)
		}
		if len(f.attested) != len(programs) {
			t.Fatalf("%d files checked by their provenance", len(f.attested))
		}
		for _, want := range []string{"--signer-workflow", "DeLFuS77/maxpack/.github/workflows/build.yml", "--source-ref", "refs/tags/v1.2.3",
			"--source-digest", testCommit, "--deny-self-hosted-runners", "-R", "DeLFuS77/maxpack"} {
			if !slices.Contains(f.attested[0], want) {
				t.Errorf("the provenance is not checked with %s: %v", want, f.attested[0])
			}
		}
		if !slices.Contains(f.asked, "git ls-remote --tags https://github.com/DeLFuS77/maxpack.git refs/tags/v1.2.3 refs/tags/v1.2.3^{}") {
			t.Errorf("the tag on GitHub is not compared: %v", f.asked)
		}
		if strings.Contains(out.String(), c.secret) {
			t.Error("the key was printed")
		}
	}
}

// What the tool signs is what the window takes: a list signed for 1.2.3 by a key of release.pub's format.
func TestTheWindowTakesIt(t *testing.T) {
	secret, key := testKey()
	f := newFakeRelease()
	var out bytes.Buffer
	if err := run(f.tools(secret, false, &out), draft, []minisign.PublicKey{key}); err != nil {
		t.Fatal(err)
	}
	if trusted, err := minisign.VerifyAny([]minisign.PublicKey{key}, f.files[sumsName], f.uploaded); err != nil || trusted != "MaxPack 1.2.3" {
		t.Fatalf("%q %v", trusted, err)
	}
	// the version may be given with its v, as the tag
	f = newFakeRelease()
	o := draft
	o.version = "v1.2.3"
	if err := run(f.tools(secret, false, &out), o, []minisign.PublicKey{key}); err != nil {
		t.Fatal(err)
	}
}

// Anything that does not hold stops before the key is asked, or before the signature goes in.
func TestSignRefuses(t *testing.T) {
	secret, key := testKey()
	other, _ := testKey()
	keys := []minisign.PublicKey{key}
	type change func(f *fakeRelease, o *options, keys *[]minisign.PublicKey, secret *string)
	for what, c := range map[string]struct {
		change   change
		want     string
		keyAsked bool
	}{
		"a file changed after its checksum": {func(f *fakeRelease, _ *options, _ *[]minisign.PublicKey, _ *string) {
			f.files["maxpack-linux-x64.tar.gz"] = []byte("something else")
		}, "does not match its checksum", false},
		"a file not in the checksums": {func(f *fakeRelease, _ *options, _ *[]minisign.PublicKey, _ *string) {
			f.files["maxpack-linux-arm64.tar.gz"] = []byte("added")
		}, "not in SHA256SUMS", false},
		"a checksum of a file the release lacks": {func(f *fakeRelease, _ *options, _ *[]minisign.PublicKey, _ *string) {
			delete(f.files, "maxpack-macos-intel.dmg")
		}, "which the release does not have", false},
		"a file not built by the workflow from the tag": {func(f *fakeRelease, _ *options, _ *[]minisign.PublicKey, _ *string) {
			f.unattested = "maxpack-windows-x64-setup.exe"
		}, "was not built by", false},
		"no tag here": {func(f *fakeRelease, _ *options, _ *[]minisign.PublicKey, _ *string) {
			f.localTag = ""
		}, "is not in this repository", false},
		"another tag on GitHub": {func(f *fakeRelease, _ *options, _ *[]minisign.PublicKey, _ *string) {
			f.remoteTag = "3333333333333333333333333333333333333333"
		}, "is not the one of this repository", false},
		"a release published already": {func(f *fakeRelease, _ *options, _ *[]minisign.PublicKey, _ *string) {
			f.draft = false
		}, "is published already", false},
		"a release signed already": {func(f *fakeRelease, _ *options, _ *[]minisign.PublicKey, _ *string) {
			f.files[signatureName] = []byte("old")
		}, "has a signature already", false},
		"not a version": {func(_ *fakeRelease, o *options, _ *[]minisign.PublicKey, _ *string) {
			o.version = "latest"
		}, "is not a version", false},
		"a MaxPack without keys": {func(_ *fakeRelease, _ *options, k *[]minisign.PublicKey, _ *string) {
			*k = nil
		}, "has no release key", false},
		"not a key": {func(_ *fakeRelease, _ *options, _ *[]minisign.PublicKey, s *string) {
			*s = "hello"
		}, "not a release key", true},
		"a key MaxPack is not built with": {func(_ *fakeRelease, _ *options, _ *[]minisign.PublicKey, s *string) {
			*s = other
		}, "not one MaxPack is built with", true},
	} {
		f := newFakeRelease()
		o, k, s := draft, keys, secret
		c.change(f, &o, &k, &s)
		var out bytes.Buffer
		err := run(f.tools(s, true, &out), o, k)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", what, err)
		}
		if f.uploaded != nil || f.published {
			t.Errorf("%s: signed or published", what)
		}
		if f.keyAsked != c.keyAsked {
			t.Errorf("%s: key asked %v", what, f.keyAsked)
		}
		if strings.Contains(out.String(), s) || err != nil && strings.Contains(err.Error(), s) {
			t.Errorf("%s: the key was printed", what)
		}
	}
}

// A release published before it was signed, or one signed again, when told so.
func TestSignAPublishedRelease(t *testing.T) {
	secret, key := testKey()
	f := newFakeRelease()
	f.draft = false
	f.files[signatureName] = []byte("old")
	o := draft
	o.published, o.replace = true, true
	var out bytes.Buffer
	if err := run(f.tools(secret, true, &out), o, []minisign.PublicKey{key}); err != nil {
		t.Fatal(err)
	}
	if _, err := minisign.VerifyAny([]minisign.PublicKey{key}, f.files[sumsName], f.uploaded); err != nil || f.published {
		t.Errorf("signature %v, published again %v", err, f.published)
	}
	if !strings.Contains(strings.Join(f.asked, "|"), "--clobber") {
		t.Errorf("the signature is not put in place of the old one: %v", f.asked)
	}
}

func TestPeeled(t *testing.T) {
	for listing, want := range map[string]string{
		"aaa\trefs/tags/v1.2.3\nbbb\trefs/tags/v1.2.3^{}\n": "bbb",
		"aaa\trefs/tags/v1.2.3\n":                           "aaa",
		"":                                                  "",
		"ccc\trefs/tags/v1.2.30\n":                          "",
	} {
		if got := peeled(listing, "refs/tags/v1.2.3"); got != want {
			t.Errorf("%q: %q, not %q", listing, got, want)
		}
	}
}

// -check tells whether a key kept in a password manager is one of release.pub, without showing it.
func TestCheckKey(t *testing.T) {
	secret, key := testKey()
	other, _ := testKey()
	for given, ok := range map[string]bool{secret: true, other: false, "hello": false} {
		var out bytes.Buffer
		err := checkKey(tools{out: &out, readKey: func(string) (string, error) { return given, nil }}, []minisign.PublicKey{key})
		if (err == nil) != ok {
			t.Errorf("%v: %v", ok, err)
		}
		if strings.Contains(out.String(), given) || err != nil && strings.Contains(err.Error(), given) {
			t.Error("the key was printed")
		}
		if ok && !strings.Contains(out.String(), key.Name()) {
			t.Errorf("it does not say which key: %s", out.String())
		}
	}
	if err := checkKey(tools{readKey: func(string) (string, error) { return secret, nil }}, nil); err == nil {
		t.Error("a MaxPack without keys")
	}
}

// The keys the tool checks with are the ones the window is built with.
func TestTheKeysOfTheWindow(t *testing.T) {
	keys := releaseKeys()
	want, _ := minisign.ParsePublicKeys(update.ReleaseKeys)
	if len(keys) != 2 || len(want) != 2 || keys[0].Name() != want[0].Name() || keys[1].Name() != want[1].Name() {
		t.Fatalf("%v, not %v", keys, want)
	}
}
