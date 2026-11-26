// SPDX-License-Identifier: GPL-3.0-or-later

// Package update looks for a newer release of MaxPack, and offers it only once its author's signature is
// checked. It asks GitHub's API for the latest release of DeLFuS77/maxpack; when that is newer, it downloads
// the release's SHA256SUMS and SHA256SUMS.minisig and checks the signature with the public key built into
// MaxPack (minisign, Ed25519). The signature's trusted comment names the version ("MaxPack X.Y.Z", signed
// with -t), which must be the release's: a signed list of an older release put under a newer tag is
// refused. Only when all that is right does it download anything else: the file of this system, whose
// SHA-256 must be the one the signed list gives; it is kept, and used again at the next start while it is
// whole. Everything goes over HTTPS, to GitHub's hosts only.
package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/minisign"
)

// Repo is the repository whose releases are looked at.
const Repo = "DeLFuS77/maxpack"

// ReleaseKeys are the public keys the releases are signed with (release.pub): the author's main key and a
// spare one, which signs if the first is lost. A release signed by either is taken.
//
//go:embed release.pub
var ReleaseKeys string

// The most each answer may take: what is bigger is not what it should be.
const (
	maxRelease   = 1 << 20
	maxSums      = 64 << 10
	maxSignature = 4 << 10
	maxAsset     = 1 << 30
)

// ErrNoKey: this build has no public key to check a signature with, so it looks for nothing.
var ErrNoKey = errors.New("no public key in this build")

// Error is why a check found nothing to offer: Code is "network" (GitHub could not be asked, or answered
// otherwise than expected, or only over plain HTTP or another host), "unsigned" (the release has no signed
// list), "bad_signature" (the list is not signed by the key, or its signature names another version),
// "not_listed" (the list has no file of this system) or "bad_download" (the file downloaded is not the one
// listed).
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func fail(code string, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// Config is what a check needs.
type Config struct {
	Current   string // the version of this MaxPack ("1.0.7"); a build of no release ("dev-…") looks for nothing
	PublicKey string // the minisign public keys of the releases, as release.pub keeps them; "" for none
	Asset     string // the file of this system in the release (see AssetFor)
	Download  bool   // whether that file is downloaded and checked (the installer of Windows)
	Folder    string // where it goes
	// where GitHub is, which hosts may answer (redirects included), and the client: Defaults gives GitHub's
	API, Downloads string
	Hosts          []string
	Client         *http.Client
}

// Defaults are GitHub's API, its downloads and the hosts its release files are served from.
func Defaults() Config {
	return Config{API: "https://api.github.com", Downloads: "https://github.com",
		Hosts: []string{"api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}}
}

// AssetFor is the file of a release for a system, and whether it is downloaded: the installer on Windows,
// which Update runs; the portable version and macOS and Linux open the release's page instead, so only the
// signed list is read. "" for a system with no file of its own.
func AssetFor(goos, arch string, portable bool) (name string, download bool) {
	switch {
	case goos == "windows" && arch == "amd64" && portable:
		return "maxpack-windows-x64-portable.zip", false
	case goos == "windows" && arch == "amd64":
		return "maxpack-windows-x64-setup.exe", true
	case goos == "darwin" && arch == "arm64":
		return "maxpack-macos-apple-silicon.dmg", false
	case goos == "darwin" && arch == "amd64":
		return "maxpack-macos-intel.dmg", false
	case goos == "linux" && arch == "amd64":
		return "maxpack-linux-x64.tar.gz", false
	}
	return "", false
}

// Offer is a newer release, signed and checked: its version, its page, and the file of this system with its
// SHA-256 when it was downloaded ("" otherwise).
type Offer struct {
	Version string
	Page    string
	File    string
	SHA256  string
}

var (
	releaseTag = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
	versionOf  = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
)

// newer tells whether version a (X.Y.Z) is newer than b.
func newer(a, b string) bool {
	pa, pb := versionOf.FindStringSubmatch(a), versionOf.FindStringSubmatch(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x > y
		}
	}
	return false
}

// Check looks for a newer release, as the package says. An empty Offer and no error: nothing newer.
func Check(ctx context.Context, c Config) (Offer, error) {
	keys, err := minisign.ParsePublicKeys(c.PublicKey)
	if err != nil {
		return Offer{}, err
	}
	if len(keys) == 0 {
		return Offer{}, ErrNoKey
	}
	if !versionOf.MatchString(c.Current) {
		return Offer{}, nil
	}
	g := getter{c: c, client: client(c)}

	// the latest release: a newer version, with a signed list
	body, err := g.get(ctx, c.API+"/repos/"+Repo+"/releases/latest", maxRelease, map[string]string{"Accept": "application/vnd.github+json"})
	if err != nil {
		return Offer{}, err
	}
	var latest struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &latest); err != nil {
		return Offer{}, fail("network", "the latest release: %v", err)
	}
	found := releaseTag.FindStringSubmatch(latest.Tag)
	if found == nil {
		return Offer{}, nil
	}
	version := strings.TrimPrefix(latest.Tag, "v")
	if !newer(version, c.Current) {
		// nothing newer: what an earlier start downloaded (the update was installed) goes
		if c.Download {
			clean(c.Folder, "")
		}
		return Offer{}, nil
	}
	sizes := map[string]int64{}
	for _, asset := range latest.Assets {
		sizes[asset.Name] = asset.Size
	}
	if _, ok := sizes["SHA256SUMS"]; !ok {
		return Offer{}, fail("unsigned", "MaxPack %s has no SHA256SUMS", version)
	}
	if _, ok := sizes["SHA256SUMS.minisig"]; !ok {
		return Offer{}, fail("unsigned", "MaxPack %s has no SHA256SUMS.minisig", version)
	}
	files := c.Downloads + "/" + Repo + "/releases/download/" + latest.Tag + "/"
	sums, err := g.get(ctx, files+"SHA256SUMS", maxSums, nil)
	if err != nil {
		return Offer{}, err
	}
	signature, err := g.get(ctx, files+"SHA256SUMS.minisig", maxSignature, nil)
	if err != nil {
		return Offer{}, err
	}
	trusted, err := minisign.VerifyAny(keys, sums, signature)
	if err != nil {
		return Offer{}, &Error{Code: "bad_signature", Err: err}
	}
	// the version is signed: the trusted comment is exactly "MaxPack <the tag's version>"
	if trusted != "MaxPack "+version {
		return Offer{}, fail("bad_signature", "the signed list is of %q, not MaxPack %s", trusted, version)
	}

	// signed: the file of this system must be on the list, and be what it says
	want, ok := listed(sums, c.Asset)
	if c.Asset == "" || !ok {
		return Offer{}, fail("not_listed", "the signed list of MaxPack %s has no %q", version, c.Asset)
	}
	offer := Offer{Version: version, Page: "https://github.com/" + Repo + "/releases/tag/" + latest.Tag, SHA256: want}
	if !c.Download {
		return offer, nil
	}
	if size, ok := sizes[c.Asset]; !ok || size <= 0 || size > maxAsset {
		return Offer{}, fail("not_listed", "MaxPack %s has no %s of a size it can have", version, c.Asset)
	}
	if offer.File, err = g.download(ctx, files+c.Asset, c.Folder, c.Asset, want); err != nil {
		return Offer{}, err
	}
	return offer, nil
}

// listed is the SHA-256 the list (as sha256sum writes it) gives a file.
func listed(sums []byte, name string) (string, bool) {
	lines := bufio.NewScanner(bytes.NewReader(sums))
	for lines.Scan() {
		sum, file, ok := strings.Cut(strings.TrimRight(lines.Text(), "\r"), " ")
		file = strings.TrimPrefix(strings.TrimPrefix(file, " "), "*")
		if ok && file == name && len(sum) == 64 {
			if _, err := hex.DecodeString(sum); err == nil {
				return strings.ToLower(sum), true
			}
		}
	}
	return "", false
}

// client is the client of the check: the one given or one of its own, which follows redirects only over
// HTTPS to the hosts allowed.
func client(c Config) *http.Client {
	base := c.Client
	if base == nil {
		base = &http.Client{Timeout: 30 * time.Minute}
	}
	own := *base
	own.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return allowed(c, req.URL)
	}
	return &own
}

func allowed(c Config, u *url.URL) error {
	if u.Scheme != "https" || !slices.Contains(c.Hosts, u.Host) {
		return fmt.Errorf("%s://%s is not one of GitHub's hosts over HTTPS", u.Scheme, u.Host)
	}
	return nil
}

type getter struct {
	c      Config
	client *http.Client
}

func (g getter) request(ctx context.Context, address string, headers map[string]string) (*http.Response, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, fail("network", "%v", err)
	}
	if err := allowed(g.c, u); err != nil {
		return nil, fail("network", "%v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fail("network", "%v", err)
	}
	req.Header.Set("User-Agent", "MaxPack/"+g.c.Current)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fail("network", "%v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fail("network", "%s: %s", address, resp.Status)
	}
	return resp, nil
}

// get is a small answer, whole: at most limit bytes.
func (g getter) get(ctx context.Context, address string, limit int64, headers map[string]string) ([]byte, error) {
	resp, err := g.request(ctx, address, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fail("network", "%s: %v", address, err)
	}
	if int64(len(body)) > limit {
		return nil, fail("network", "%s: more than %d bytes", address, limit)
	}
	return body, nil
}

// clean takes away from folder what checks downloaded there (maxpack-*), but keep.
func clean(folder, keep string) {
	if folder == "" {
		return
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "maxpack-") && entry.Name() != keep {
			os.RemoveAll(filepath.Join(folder, entry.Name()))
		}
	}
}

// download writes a file to folder as name, hashing it on the way; it keeps it only when its SHA-256 is want.
// One there already with that SHA-256 (an earlier start downloaded it) is used as it is; anything else that
// earlier checks left there (maxpack-*) goes first.
func (g getter) download(ctx context.Context, address, folder, name, want string) (string, error) {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", fail("bad_download", "%v", err)
	}
	path := filepath.Join(folder, name)
	if VerifyFile(path, want) == nil {
		clean(folder, name)
		return path, nil
	}
	clean(folder, "")
	resp, err := g.request(ctx, address, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	part := path + ".part"
	out, err := os.Create(part)
	if err != nil {
		return "", fail("bad_download", "%v", err)
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(resp.Body, maxAsset+1))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	switch {
	case err != nil:
		os.Remove(part)
		return "", fail("network", "%s: %v", address, err)
	case n > maxAsset:
		os.Remove(part)
		return "", fail("bad_download", "%s: more than %d bytes", address, maxAsset)
	case hex.EncodeToString(hash.Sum(nil)) != want:
		os.Remove(part)
		return "", fail("bad_download", "%s is not the file the signed list gives", name)
	}
	if err := os.Rename(part, path); err != nil {
		os.Remove(part)
		return "", fail("bad_download", "%v", err)
	}
	return path, nil
}

// VerifyFile tells whether the file at path still has the SHA-256 the signed list gave it: checked again
// right before it is run.
func VerifyFile(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); want == "" || got != want {
		return fmt.Errorf("%s changed since it was checked", filepath.Base(path))
	}
	return nil
}
