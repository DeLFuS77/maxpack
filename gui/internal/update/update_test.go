// SPDX-License-Identifier: GPL-3.0-or-later

package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/minisign/minisigntest"
)

// testSeed makes the key of these tests only: it signs nothing but what the tests make up. The key of the
// releases is never in the tests.
var testSeed = []byte("maxpack update test key, not real")[:32]

func testKey() (ed25519.PrivateKey, [8]byte, string) {
	private, id := ed25519.NewKeyFromSeed(testSeed), [8]byte{'u', 'p', 'd', 't', 'e', 's', 't', '1'}
	return private, id, minisigntest.PublicKey(private.Public().(ed25519.PublicKey), id)
}

const installer = "maxpack-windows-x64-setup.exe"

// release is a made-up GitHub: the latest release, its assets, and every request it was asked.
type release struct {
	mutex    sync.Mutex
	asked    []string
	tag      string
	assets   map[string][]byte
	redirect string // a download goes there instead (a Location)
}

func (r *release) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mutex.Lock()
	r.asked = append(r.asked, req.URL.Path)
	r.mutex.Unlock()
	switch {
	case req.URL.Path == "/repos/DeLFuS77/maxpack/releases/latest":
		if req.Header.Get("Accept") != "application/vnd.github+json" || !strings.HasPrefix(req.Header.Get("User-Agent"), "MaxPack") {
			http.Error(w, "headers", http.StatusBadRequest)
			return
		}
		type asset struct {
			Name string `json:"name"`
			Size int    `json:"size"`
		}
		answer := struct {
			Tag    string  `json:"tag_name"`
			Assets []asset `json:"assets"`
		}{Tag: r.tag}
		for name, data := range r.assets {
			answer.Assets = append(answer.Assets, asset{name, len(data)})
		}
		json.NewEncoder(w).Encode(answer)
	case strings.HasPrefix(req.URL.Path, "/DeLFuS77/maxpack/releases/download/"+r.tag+"/"):
		name := strings.TrimPrefix(req.URL.Path, "/DeLFuS77/maxpack/releases/download/"+r.tag+"/")
		if r.redirect != "" {
			http.Redirect(w, req, r.redirect+name, http.StatusFound)
			return
		}
		r.serve(w, name)
	case strings.HasPrefix(req.URL.Path, "/assets/"):
		r.serve(w, strings.TrimPrefix(req.URL.Path, "/assets/"))
	default:
		http.NotFound(w, req)
	}
}

func (r *release) serve(w http.ResponseWriter, name string) {
	data, ok := r.assets[name]
	if !ok {
		http.NotFound(w, nil)
		return
	}
	w.Write(data)
}

func (r *release) requests() string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return strings.Join(r.asked, " ")
}

// signed is a release of tag with its installer, its SHA256SUMS and their signature by the test key.
func signed(tag string) *release {
	private, id, _ := testKey()
	files := map[string][]byte{installer: []byte("the installer of " + tag), "maxpack-linux-x64.tar.gz": []byte("linux " + tag)}
	var sums strings.Builder
	for _, name := range []string{installer, "maxpack-linux-x64.tar.gz"} {
		sum := sha256.Sum256(files[name])
		sums.WriteString(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	}
	files["SHA256SUMS"] = []byte(sums.String())
	files["SHA256SUMS.minisig"] = minisigntest.Sign(private, id, files["SHA256SUMS"], true, "timestamp:1764000000\tfile:SHA256SUMS\thashed")
	return &release{tag: tag, assets: files}
}

// config checks with the made-up GitHub of server, as the window would on Windows (the installer downloaded).
func config(t *testing.T, server *httptest.Server, current string) Config {
	_, _, key := testKey()
	host := strings.TrimPrefix(server.URL, "https://")
	return Config{Current: current, PublicKey: key, Asset: installer, Download: true, Folder: t.TempDir(),
		API: server.URL, Downloads: server.URL, Hosts: []string{host}, Client: server.Client()}
}

func codeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// A newer release, signed: its list is checked against the key, then its installer against the list, and it is
// offered with the installer on disk.
func TestNewerSignedRelease(t *testing.T) {
	r := signed("v1.1.0")
	server := httptest.NewTLSServer(r)
	defer server.Close()
	c := config(t, server, "1.0.7")
	offer, err := Check(context.Background(), c)
	if err != nil || offer.Version != "1.1.0" || offer.Page != "https://github.com/DeLFuS77/maxpack/releases/tag/v1.1.0" {
		t.Fatalf("%+v, %v", offer, err)
	}
	data, err := os.ReadFile(offer.File)
	if err != nil || string(data) != "the installer of v1.1.0" || filepath.Dir(offer.File) != c.Folder || filepath.Base(offer.File) != installer {
		t.Fatalf("the installer: %q %v at %s", data, err, offer.File)
	}
	if err := VerifyFile(offer.File, offer.SHA256); err != nil {
		t.Fatal(err)
	}
	want := "/repos/DeLFuS77/maxpack/releases/latest /DeLFuS77/maxpack/releases/download/v1.1.0/SHA256SUMS " +
		"/DeLFuS77/maxpack/releases/download/v1.1.0/SHA256SUMS.minisig /DeLFuS77/maxpack/releases/download/v1.1.0/" + installer
	if got := r.requests(); got != want {
		t.Fatalf("asked %s", got)
	}
	// changed on disk afterwards, it is not run
	os.WriteFile(offer.File, []byte("something else"), 0o644)
	if err := VerifyFile(offer.File, offer.SHA256); err == nil {
		t.Fatal("a changed installer verifies")
	}
}

// macOS, Linux and the portable version download nothing but the signed list: Update opens the page.
func TestOnlyTheListWithoutDownload(t *testing.T) {
	r := signed("v1.1.0")
	server := httptest.NewTLSServer(r)
	defer server.Close()
	c := config(t, server, "1.0.7")
	c.Asset, c.Download = "maxpack-linux-x64.tar.gz", false
	offer, err := Check(context.Background(), c)
	if err != nil || offer.Version != "1.1.0" || offer.File != "" {
		t.Fatalf("%+v, %v", offer, err)
	}
	if strings.Contains(r.requests(), "tar.gz") {
		t.Fatalf("downloaded: %s", r.requests())
	}
}

// The same version, an older one, a tag that is not a release's or a build of no release (dev) offer nothing,
// and download nothing.
func TestNothingNewer(t *testing.T) {
	for _, c := range []struct{ tag, current string }{{"v1.0.7", "1.0.7"}, {"v1.0.6", "1.0.7"}, {"v1.1.0-beta.1", "1.0.7"}, {"latest", "1.0.7"}, {"v1.1.0", "dev-1a2b3c4"}} {
		r := signed(c.tag)
		server := httptest.NewTLSServer(r)
		offer, err := Check(context.Background(), config(t, server, c.current))
		server.Close()
		if err != nil || offer.Version != "" {
			t.Errorf("%s over %s: %+v, %v", c.tag, c.current, offer, err)
		}
		if got := r.requests(); strings.Contains(got, "download") || (strings.HasPrefix(c.current, "dev") && got != "") {
			t.Errorf("%s over %s asked %s", c.tag, c.current, got)
		}
	}
	if !newer("1.10.0", "1.9.9") || newer("1.9.9", "1.10.0") || newer("2.0.0", "2.0.0") {
		t.Error("versions are compared as numbers")
	}
}

// Nothing is downloaded past the list unless the signature is right: a list changed after it was signed, one
// signed by another key, or a release with no signature, are not offered.
func TestBadSignatureDownloadsNothing(t *testing.T) {
	for name, change := range map[string]func(r *release){
		"changed list": func(r *release) { r.assets["SHA256SUMS"] = append(r.assets["SHA256SUMS"], "0000  evil.exe\n"...) },
		"another key": func(r *release) {
			other := ed25519.NewKeyFromSeed([]byte("another test key, not the right1"))
			r.assets["SHA256SUMS.minisig"] = minisigntest.Sign(other, [8]byte{'u', 'p', 'd', 't', 'e', 's', 't', '1'}, r.assets["SHA256SUMS"], true, "x")
		},
		"another key ID": func(r *release) {
			private, _, _ := testKey()
			r.assets["SHA256SUMS.minisig"] = minisigntest.Sign(private, [8]byte{1}, r.assets["SHA256SUMS"], true, "x")
		},
		"not a signature": func(r *release) { r.assets["SHA256SUMS.minisig"] = []byte("hello") },
		"unsigned":        func(r *release) { delete(r.assets, "SHA256SUMS.minisig") },
		"no list":         func(r *release) { delete(r.assets, "SHA256SUMS") },
	} {
		r := signed("v1.1.0")
		change(r)
		server := httptest.NewTLSServer(r)
		c := config(t, server, "1.0.7")
		offer, err := Check(context.Background(), c)
		server.Close()
		want := "bad_signature"
		if name == "unsigned" || name == "no list" {
			want = "unsigned"
		}
		if codeOf(err) != want || offer.Version != "" {
			t.Errorf("%s: %+v, %v", name, offer, err)
		}
		if strings.Contains(r.requests(), installer) {
			t.Errorf("%s: the installer was downloaded: %s", name, r.requests())
		}
		if entries, _ := os.ReadDir(c.Folder); len(entries) != 0 {
			t.Errorf("%s: left %v", name, entries)
		}
	}
}

// The installer must be on the signed list, and be what the list says, or it is taken away and not offered.
func TestInstallerMustMatchTheList(t *testing.T) {
	r := signed("v1.1.0")
	r.assets[installer] = []byte("an installer that is not the one signed")
	server := httptest.NewTLSServer(r)
	c := config(t, server, "1.0.7")
	offer, err := Check(context.Background(), c)
	if codeOf(err) != "bad_download" || offer.Version != "" {
		t.Errorf("a different installer: %+v, %v", offer, err)
	}
	if entries, _ := os.ReadDir(c.Folder); len(entries) != 0 {
		t.Errorf("left %v", entries)
	}
	c.Asset = "maxpack-windows-arm64-setup.exe"
	if _, err := Check(context.Background(), c); codeOf(err) != "not_listed" {
		t.Errorf("an asset the list does not have: %v", err)
	}
	server.Close()
}

// Only HTTPS, and only the hosts of GitHub (here, the made-up one): a download sent elsewhere, or to plain
// HTTP, is refused; one sent to an allowed host is followed.
func TestOnlyGitHubOverHTTPS(t *testing.T) {
	r := signed("v1.1.0")
	server := httptest.NewTLSServer(r)
	defer server.Close()
	r.redirect = server.URL + "/assets/"
	if offer, err := Check(context.Background(), config(t, server, "1.0.7")); err != nil || offer.Version != "1.1.0" {
		t.Fatalf("a redirect within the allowed hosts: %+v, %v", offer, err)
	}
	u, _ := url.Parse(server.URL)
	for _, elsewhere := range []string{"http://" + u.Host + "/assets/", "https://localhost:" + u.Port() + "/assets/"} {
		r.redirect = elsewhere
		before := strings.Count(r.requests(), "/assets/")
		if _, err := Check(context.Background(), config(t, server, "1.0.7")); codeOf(err) != "network" {
			t.Errorf("sent to %s: %v", elsewhere, err)
		}
		if strings.Count(r.requests(), "/assets/") != before {
			t.Errorf("followed to %s", elsewhere)
		}
	}
	c := config(t, server, "1.0.7")
	c.API = strings.Replace(server.URL, "https://", "http://", 1)
	if _, err := Check(context.Background(), c); codeOf(err) != "network" {
		t.Errorf("plain HTTP asked: %v", err)
	}
	if Defaults().API != "https://api.github.com" || Defaults().Downloads != "https://github.com" {
		t.Errorf("the defaults: %+v", Defaults())
	}
	for _, host := range Defaults().Hosts {
		if host != "api.github.com" && host != "github.com" && !strings.HasSuffix(host, ".githubusercontent.com") {
			t.Errorf("a host that is not GitHub's: %s", host)
		}
	}
}

// Without a key nothing is asked at all.
func TestNoKeyAsksNothing(t *testing.T) {
	r := signed("v1.1.0")
	server := httptest.NewTLSServer(r)
	defer server.Close()
	c := config(t, server, "1.0.7")
	c.PublicKey = ""
	if _, err := Check(context.Background(), c); !errors.Is(err, ErrNoKey) || r.requests() != "" {
		t.Fatalf("%v, asked %s", err, r.requests())
	}
}

// Assets is the one of each system: the installer on Windows (downloaded), the archive elsewhere and for the
// portable version (not downloaded).
func TestAssetOfEachSystem(t *testing.T) {
	for _, c := range []struct {
		goos, arch string
		portable   bool
		name       string
		download   bool
	}{
		{"windows", "amd64", false, installer, true},
		{"windows", "amd64", true, "maxpack-windows-x64-portable.zip", false},
		{"darwin", "arm64", false, "maxpack-macos-apple-silicon.dmg", false},
		{"darwin", "amd64", false, "maxpack-macos-intel.dmg", false},
		{"linux", "amd64", false, "maxpack-linux-x64.tar.gz", false},
		{"linux", "arm64", false, "", false},
	} {
		name, download := AssetFor(c.goos, c.arch, c.portable)
		if name != c.name || download != c.download {
			t.Errorf("%s/%s portable %v: %q %v", c.goos, c.arch, c.portable, name, download)
		}
	}
}
