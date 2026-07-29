package grpc

// Fetching a plugin binary from its repo's latest release.
//
// This layer knows exactly two states: the binary is on disk, or it is not.
// There is no version pinning, no update check, and no rollback — adding a repo
// URL to [plugin] enabled says "I want this plugin", not "I want this build".
// `qntx-<name>-plugin --version` remains the only statement of what is running.
//
// A fetch failure is never fatal: the caller falls into the same
// warn-and-mark-failed path as a plugin that was simply missing.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// githubAPIBase is the only forge QNTX fetches from. Other hosts are rejected
// by name rather than guessed at — release APIs are not portable.
// A var so tests can drive the whole fetch path against a local server.
var githubAPIBase = "https://api.github.com"

const (
	// githubHost is the host a plugin repo URL must have.
	githubHost = "github.com"

	// PluginFetchTimeout bounds a single plugin fetch. Plugin binaries run to
	// tens of megabytes, so this is sized for a download, not an API call.
	PluginFetchTimeout = 10 * time.Minute

	// maxChecksumBytes caps the .sha256 read — it holds one hex digest.
	maxChecksumBytes = 4096
)

// releaseAsset is one file published on a release.
type releaseAsset struct {
	Name string `json:"name"`
	// URL is the API asset URL. Unlike browser_download_url it carries
	// authorization, so the same code path serves public and private repos.
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// release is the subset of the GitHub release payload this needs.
type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

// PluginBinaryName is the file name a fetched plugin is installed as, and the
// first name plugin discovery looks for on disk.
func PluginBinaryName(name string) string {
	return "qntx-" + name + "-plugin"
}

// pluginAssetSuffix is the release asset this platform needs, e.g.
// "-darwin-arm64.tar.gz". Publishers name assets by GOOS-GOARCH.
func pluginAssetSuffix() string {
	return "-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
}

// PluginInstallPath is where a fetched binary is placed: ~/.qntx/plugins/.
// Fetched plugins land in one known directory regardless of search paths, so
// what arrived over the network is always in the same place to inspect.
func PluginInstallPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve home directory for plugin %s install path", name)
	}
	return filepath.Join(home, ".qntx", "plugins", PluginBinaryName(name)), nil
}

// githubRepoPath splits a plugin repo URL into owner and repo.
func githubRepoPath(repo string) (string, string, error) {
	u, err := url.Parse(repo)
	if err != nil {
		return "", "", errors.Wrapf(err, "failed to parse plugin source URL %s", repo)
	}

	if u.Host != githubHost {
		err := errors.Newf("unsupported plugin source host %q in %s", u.Host, repo)
		return "", "", errors.WithHintf(err, "plugin binaries are fetched from %s only; install this plugin to ~/.qntx/plugins/ and enable it by bare name instead", githubHost)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		err := errors.Newf("plugin source URL %s is not an owner/repo path", repo)
		return "", "", errors.WithHint(err, "use the repo root, e.g. https://github.com/sbvh-nl/duif")
	}

	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

// pluginAccessToken resolves the credential configured for a repo's host.
// Public repos need none, so an unset host is not an error.
func pluginAccessToken(ctx context.Context, repo string) (string, error) {
	u, err := url.Parse(repo)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse plugin source URL %s", repo)
	}

	ref := config.PluginAccessToken(u.Host)
	if ref == "" {
		return "", nil
	}

	token, err := secretref.Resolve(ctx, ref)
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve plugin.access_token.%q", u.Host)
	}
	return token, nil
}

// fetchPlugin downloads name's binary from repo's latest release, verifies it
// against the published .sha256, and installs it. Returns the installed path.
func fetchPlugin(ctx context.Context, name, repo string, logger *zap.SugaredLogger) (string, error) {
	owner, repoName, err := githubRepoPath(repo)
	if err != nil {
		return "", err
	}

	token, err := pluginAccessToken(ctx, repo)
	if err != nil {
		return "", err
	}

	rel, err := latestRelease(ctx, owner, repoName, token)
	if err != nil {
		return "", err
	}

	suffix := pluginAssetSuffix()
	asset, ok := findAsset(rel, suffix)
	if !ok {
		err := errors.Newf("release %s of %s/%s publishes no asset ending in %s (has: %s)",
			rel.TagName, owner, repoName, suffix, strings.Join(assetNames(rel), ", "))
		return "", errors.WithHintf(err, "the release must ship an asset named for this platform, e.g. %s%s", PluginBinaryName(name), suffix)
	}

	checksumAsset, ok := findAssetNamed(rel, asset.Name+".sha256")
	if !ok {
		err := errors.Newf("release %s of %s/%s publishes %s without %s.sha256",
			rel.TagName, owner, repoName, asset.Name, asset.Name)
		return "", errors.WithHint(err, "every asset must ship a .sha256 alongside it; an unverifiable binary is not installed")
	}

	logger.Infow("Fetching plugin release asset",
		"plugin", name, "repo", repo, "release", rel.TagName,
		"asset", asset.Name, "bytes", asset.Size)

	archive, err := downloadAsset(ctx, asset, token)
	if err != nil {
		return "", errors.Wrapf(err, "failed to download %s from release %s of %s/%s", asset.Name, rel.TagName, owner, repoName)
	}

	want, err := publishedChecksum(ctx, checksumAsset, token)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read %s from release %s of %s/%s", checksumAsset.Name, rel.TagName, owner, repoName)
	}

	got := hex.EncodeToString(sha256Of(archive))
	if got != want {
		err := errors.Newf("checksum mismatch for %s from release %s of %s/%s: published %s, downloaded %s",
			asset.Name, rel.TagName, owner, repoName, want, got)
		return "", errors.WithHint(err, "the release asset and its .sha256 disagree; nothing was installed")
	}

	binary, err := extractBinary(archive, PluginBinaryName(name))
	if err != nil {
		return "", errors.Wrapf(err, "failed to extract %s from %s", PluginBinaryName(name), asset.Name)
	}

	dest, err := PluginInstallPath(name)
	if err != nil {
		return "", err
	}

	if err := install(binary, dest); err != nil {
		return "", errors.Wrapf(err, "failed to install plugin %s to %s", name, dest)
	}

	logger.Infow("Installed plugin binary",
		"plugin", name, "repo", repo, "release", rel.TagName,
		"path", dest, "bytes", len(binary), "sha256", got)

	return dest, nil
}

// latestRelease reads the repo's latest release.
func latestRelease(ctx context.Context, owner, repo, token string) (release, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIBase, owner, repo)

	body, err := get(ctx, endpoint, token, "application/vnd.github+json")
	if err != nil {
		return release{}, errors.Wrapf(err, "failed to read latest release of %s/%s", owner, repo)
	}
	defer body.Close()

	var rel release
	if err := json.NewDecoder(body).Decode(&rel); err != nil {
		return release{}, errors.Wrapf(err, "failed to decode latest release of %s/%s", owner, repo)
	}

	if len(rel.Assets) == 0 {
		err := errors.Newf("latest release %s of %s/%s has no assets", rel.TagName, owner, repo)
		return release{}, errors.WithHint(err, "the release must publish built binaries, not just source archives")
	}

	return rel, nil
}

// findAsset returns the first asset whose name ends with suffix.
func findAsset(rel release, suffix string) (releaseAsset, bool) {
	for _, asset := range rel.Assets {
		if strings.HasSuffix(asset.Name, suffix) {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

// findAssetNamed returns the asset with exactly this name.
func findAssetNamed(rel release, name string) (releaseAsset, bool) {
	for _, asset := range rel.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

// assetNames lists what a release actually published, for error messages that
// show the operator why nothing matched.
func assetNames(rel release) []string {
	names := make([]string, 0, len(rel.Assets))
	for _, asset := range rel.Assets {
		names = append(names, asset.Name)
	}
	return names
}

// downloadAsset reads an asset's bytes through the API URL, which carries
// authorization and so works for private repos.
func downloadAsset(ctx context.Context, asset releaseAsset, token string) ([]byte, error) {
	body, err := get(ctx, asset.URL, token, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s (%d bytes expected)", asset.Name, asset.Size)
	}
	return data, nil
}

// publishedChecksum reads the hex digest from a .sha256 asset. The file holds
// either a bare digest or the `<digest>  <filename>` form; both start with it.
func publishedChecksum(ctx context.Context, asset releaseAsset, token string) (string, error) {
	body, err := get(ctx, asset.URL, token, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer body.Close()

	data, err := io.ReadAll(io.LimitReader(body, maxChecksumBytes))
	if err != nil {
		return "", errors.Wrapf(err, "failed to read %s", asset.Name)
	}

	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", errors.Newf("%s is empty", asset.Name)
	}

	digest := strings.ToLower(fields[0])
	if len(digest) != sha256.Size*2 {
		return "", errors.Newf("%s does not start with a sha256 digest: %q", asset.Name, digest)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", errors.Wrapf(err, "%s does not start with a hex digest: %q", asset.Name, digest)
	}

	return digest, nil
}

// sha256Of hashes the downloaded bytes.
func sha256Of(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// get performs an authenticated GET and returns the body on success.
func get(ctx context.Context, endpoint, token, accept string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to build request for %s", endpoint)
	}

	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "request to %s failed", endpoint)
	}

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxChecksumBytes))
		resp.Body.Close()

		err := errors.Newf("%s returned %s: %s", endpoint, resp.Status, strings.TrimSpace(string(detail)))
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
			return nil, errors.WithHint(err, "a private repo needs a credential — set [plugin.access_token] for this host to an ssm:// or env: reference")
		}
		return nil, err
	}

	return resp.Body, nil
}

// extractBinary pulls one named file out of a .tar.gz.
//
// Only the archive entry's base name is compared and the destination is chosen
// by QNTX, so a path in the archive can never steer where bytes land.
func extractBinary(archive []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, errors.Wrap(err, "asset is not gzip data")
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var seen []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.Wrap(err, "failed to read tar entry")
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		seen = append(seen, header.Name)
		if filepath.Base(header.Name) != binaryName {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read %s out of the archive", header.Name)
		}
		return data, nil
	}

	err = errors.Newf("archive contains no file named %s (has: %s)", binaryName, strings.Join(seen, ", "))
	return nil, errors.WithHintf(err, "the release asset must contain the plugin binary named %s", binaryName)
}

// install writes the binary to dest, executable, replacing any previous file.
// Written to a temp file in the same directory and renamed, so a crash mid-write
// never leaves a half-binary where a plugin is expected.
func install(binary []byte, dest string) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.Wrapf(err, "failed to create plugin directory %s", dir)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(dest)+".partial-*")
	if err != nil {
		return errors.Wrapf(err, "failed to create temp file in %s", dir)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return errors.Wrapf(err, "failed to write %d bytes to %s", len(binary), tmpName)
	}
	if err := tmp.Close(); err != nil {
		return errors.Wrapf(err, "failed to close %s", tmpName)
	}

	if err := os.Chmod(tmpName, 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s executable", tmpName)
	}

	// Remove first: renaming over a running binary fails with ETXTBSY on some
	// systems, and the old file may be read-only (0555) from a previous install.
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to remove existing %s", dest)
	}

	if err := os.Rename(tmpName, dest); err != nil {
		return errors.Wrapf(err, "failed to move %s into place at %s", tmpName, dest)
	}

	return nil
}
