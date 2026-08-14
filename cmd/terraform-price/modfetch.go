package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Public registry modules are open-source: fetch the published tarball and
// expand them exactly like local-path modules. The registry's download
// endpoint (204 + X-Terraform-Get) points at a git source; GitHub sources
// translate to codeload tarballs. Everything degrades to the generic info
// row on failure — a fetch problem must never kill the report.

var (
	registryBase = "https://registry.terraform.io"
	codeloadBase = "https://codeload.github.com"
	fetchClient  = &http.Client{Timeout: 20 * time.Second}
)

// registrySource is a parsed public-registry module source:
// namespace/name/provider[//subdir].
type registrySource struct {
	namespace, name, provider string
	subdir                    string
}

var registrySrcRe = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*)/([a-z0-9][a-z0-9-]*)/([a-z0-9][a-z0-9-]*)(//.*)?$`)

// parseRegistrySource recognizes `<ns>/<name>/<provider>` sources.
// Local paths (./ ../), URLs, and git:: sources return nil.
func parseRegistrySource(src string) *registrySource {
	src = strings.SplitN(src, "?", 2)[0]
	m := registrySrcRe.FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	return &registrySource{namespace: m[1], name: m[2], provider: m[3], subdir: strings.TrimPrefix(m[4], "//")}
}

// resolveVersion picks the version to download: an exact pin as-is, else
// the highest version satisfying a "~> x.y" / "~> x" constraint, else the
// highest published. Pre-releases are skipped.
func resolveVersion(rs *registrySource, pin string) (string, error) {
	if ok, _ := regexp.MatchString(`^\d+\.\d+\.\d+$`, pin); ok {
		return pin, nil
	}
	resp, err := fetchClient.Get(fmt.Sprintf("%s/v1/modules/%s/%s/%s/versions", registryBase, rs.namespace, rs.name, rs.provider))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry versions: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Modules []struct {
			Versions []struct {
				Version string `json:"version"`
			} `json:"versions"`
		} `json:"modules"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	var vers []string
	for _, m := range out.Modules {
		for _, v := range m.Versions {
			if !strings.Contains(v.Version, "-") {
				vers = append(vers, v.Version)
			}
		}
	}
	if len(vers) == 0 {
		return "", fmt.Errorf("no published versions")
	}
	sort.Slice(vers, func(i, j int) bool { return versionLess(vers[j], vers[i]) }) // descending

	if c, ok := strings.CutPrefix(pin, "~> "); ok {
		want := strings.Split(c, ".")
		for _, v := range vers {
			if satisfiesPessimistic(want, strings.Split(v, ".")) {
				return v, nil
			}
		}
	}
	return vers[0], nil
}

// satisfiesPessimistic implements terraform's ~> constraint:
//   - "~> 5"     means >= 5.0.0, < 6.0.0 (major must match)
//   - "~> 5.5"   means >= 5.5.0, < 6.0.0 (minor may increase, major matches)
//   - "~> 5.5.1" means >= 5.5.1, < 5.6.0 (patch may increase, major.minor match)
func satisfiesPessimistic(want, got []string) bool {
	if len(got) < len(want) {
		return false
	}
	for i := range len(want) - 1 {
		if want[i] != got[i] {
			return false
		}
	}
	if len(want) == 1 {
		return got[0] == want[0]
	}
	return numAtLeast(got[len(want)-1], want[len(want)-1])
}

// numAtLeast compares two numeric strings by value (versions are
// numeric components; pad to equal length so "9" < "10" holds).
func numAtLeast(a, b string) bool {
	for len(a) < len(b) {
		a = "0" + a
	}
	for len(b) < len(a) {
		b = "0" + b
	}
	return a >= b
}

// versionLess compares dotted numeric versions.
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(as), len(bs)) {
		if as[i] != bs[i] {
			return !numAtLeast(as[i], bs[i])
		}
	}
	return len(as) < len(bs)
}

// tarballURL turns the registry's X-Terraform-Get location into a
// downloadable tarball URL. GitHub git sources map to codeload; direct
// tarball URLs pass through.
func tarballURL(get string) (string, error) {
	if ref := strings.TrimPrefix(get, "git::https://github.com/"); ref != get {
		repo, sha, ok := strings.Cut(strings.TrimPrefix(ref, ""), "?ref=")
		if !ok {
			return "", fmt.Errorf("github source without ref: %s", get)
		}
		return fmt.Sprintf("%s/%s/tar.gz/%s", codeloadBase, repo, sha), nil
	}
	if strings.HasSuffix(get, ".tar.gz") || strings.HasSuffix(get, ".tgz") {
		return get, nil
	}
	return "", fmt.Errorf("unsupported module source: %s", get)
}

// fetchRegistryModule downloads and extracts a registry module into the
// user cache, returning the directory (root + subdir). ok=false means
// unavailable — the caller degrades to an info row.
func fetchRegistryModule(rs *registrySource, version string) (string, bool) {
	cacheRoot, err := moduleCacheDir()
	if err != nil {
		return "", false
	}
	dir := filepath.Join(cacheRoot, fmt.Sprintf("%s_%s_%s_%s", rs.namespace, rs.name, rs.provider, version))
	if _, err := os.Stat(filepath.Join(dir, ".tfprice-ok")); err == nil {
		return filepath.Join(dir, rs.subdir), true
	}

	resp, err := fetchClient.Get(fmt.Sprintf("%s/v1/modules/%s/%s/%s/%s/download", registryBase, rs.namespace, rs.name, rs.provider, version))
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("X-Terraform-Get")
	if loc == "" {
		return "", false
	}
	tarURL, err := tarballURL(loc)
	if err != nil {
		return "", false
	}
	tr, err := fetchClient.Get(tarURL)
	if err != nil {
		return "", false
	}
	defer tr.Body.Close()
	if tr.StatusCode != http.StatusOK {
		return "", false
	}
	if err := extractTarGz(tr.Body, dir); err != nil {
		os.RemoveAll(dir)
		return "", false
	}
	os.WriteFile(filepath.Join(dir, ".tfprice-ok"), []byte(version), 0o644)
	return filepath.Join(dir, rs.subdir), true
}

// extractTarGz unpacks a tar.gz stream into dst, stripping the leading
// archive directory and refusing path escapes.
func extractTarGz(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		parts := strings.Split(hdr.Name, "/")
		if len(parts) < 2 || parts[0] == "pax_global_header" {
			continue
		}
		rel := filepath.Join(parts[1:]...)
		if rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		target := filepath.Join(dst, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
}

var moduleCacheDir = func() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "terraform-price", "modules")
	return dir, os.MkdirAll(dir, 0o755)
}
