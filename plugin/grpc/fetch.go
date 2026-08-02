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

	// PluginDigestTimeout bounds the check of an installed plugin against its
	// latest release. Two API calls reading a release and a 108-byte digest,
	// on the path to every start — short, because a slow or unreachable forge
	// must not hold up a plugin that is already on disk.
	PluginDigestTimeout = 15 * time.Second

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

// PluginInstallPath is the directory a fetched plugin is unpacked into:
// ~/.qntx/plugins/<name>/. Fetched plugins land in one known place regardless
// of search paths, so what arrived over the network is always in the same
// directory to inspect.
//
// A directory rather than a file because an archive may carry more than the
// binary — a plugin that cannot statically link everything ships its libraries
// in lib/ beside it. Unpacking to a directory per plugin keeps one plugin's
// libraries from colliding with another's.
func PluginInstallPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve home directory for plugin %s install path", name)
	}
	return filepath.Join(home, ".qntx", "plugins", name), nil
}

// LegacyPluginInstallPath is where fetched plugins were installed before they
// were unpacked as trees: a bare file in the plugins directory.
//
// It is still QNTX's own install location, so a file there is not somebody's
// hand-placed build and may be superseded. It also shadows the tree — discovery
// tries qntx-<name>-plugin before <name> — so it has to be removed when one is
// installed, or the superseded file wins on the next start forever.
func LegacyPluginInstallPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve home directory for plugin %s install path", name)
	}
	return filepath.Join(home, ".qntx", "plugins", PluginBinaryName(name)), nil
}

// removeLegacyInstall deletes the pre-tree install of name, if there is one.
func removeLegacyInstall(name string, logger *zap.SugaredLogger) error {
	path, err := LegacyPluginInstallPath(name)
	if err != nil {
		return err
	}

	info, err := os.Lstat(path)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		return nil
	}

	if err := os.Remove(path); err != nil {
		return errors.Wrapf(err, "failed to remove the superseded plugin binary at %s", path)
	}

	logger.Infow("Removed the superseded plugin binary",
		"plugin", name, "path", path)

	return nil
}

// installedDigestFile names the record of which archive a plugin directory was
// unpacked from. The box keeps the same record beside the qntx binary
// (apply.sh, $BIN.installed.sha256) and for the same reason: comparing it
// against a cheap published digest is what makes an install reconcilable
// rather than permanent.
const installedDigestFile = ".installed.sha256"

// recordInstalledDigest notes the archive digest dir was unpacked from.
func recordInstalledDigest(dir, digest string) error {
	path := filepath.Join(dir, installedDigestFile)
	if err := os.WriteFile(path, []byte(digest), 0o644); err != nil {
		return errors.Wrapf(err, "failed to record the installed digest at %s", path)
	}
	return nil
}

// installedDigest reads the digest recorded when dir was unpacked. Absent for a
// directory QNTX did not install, and for one installed before this was kept.
func installedDigest(dir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, installedDigestFile))
	if err != nil {
		return "", false
	}

	digest := strings.TrimSpace(string(data))
	if len(digest) != sha256.Size*2 {
		return "", false
	}

	return digest, true
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

	ref, err := config.PluginAccessToken(u.Host)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read the access token configured for %s", u.Host)
	}
	if ref == "" {
		return "", nil
	}

	token, err := secretref.Resolve(ctx, ref)
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve the access token configured for host %q", u.Host)
	}
	return token, nil
}

// publishedRelease is what a repo's latest release offers this platform: the
// asset, the digest published alongside it, and the token that read them.
type publishedRelease struct {
	tag      string
	asset    releaseAsset
	checksum releaseAsset
	token    string
}

// resolveRelease finds this platform's asset on repo's latest release.
//
// Split out from the download so the published digest can be read on its own.
// That digest is 108 bytes against an asset of tens of megabytes, which is what
// makes it affordable to ask "is what I installed still what is published?" on
// every start rather than only when nothing is installed.
func resolveRelease(ctx context.Context, name, repo string) (publishedRelease, error) {
	owner, repoName, err := githubRepoPath(repo)
	if err != nil {
		return publishedRelease{}, err
	}

	token, err := pluginAccessToken(ctx, repo)
	if err != nil {
		return publishedRelease{}, err
	}

	rel, err := latestRelease(ctx, owner, repoName, token)
	if err != nil {
		return publishedRelease{}, err
	}

	suffix := pluginAssetSuffix()
	asset, ok := findAsset(rel, suffix)
	if !ok {
		err := errors.Newf("release %s of %s/%s publishes no asset ending in %s (has: %s)",
			rel.TagName, owner, repoName, suffix, strings.Join(assetNames(rel), ", "))
		return publishedRelease{}, errors.WithHintf(err, "the release must ship an asset named for this platform, e.g. %s%s", PluginBinaryName(name), suffix)
	}

	checksumAsset, ok := findAssetNamed(rel, asset.Name+".sha256")
	if !ok {
		err := errors.Newf("release %s of %s/%s publishes %s without %s.sha256",
			rel.TagName, owner, repoName, asset.Name, asset.Name)
		return publishedRelease{}, errors.WithHint(err, "every asset must ship a .sha256 alongside it; an unverifiable binary is not installed")
	}

	return publishedRelease{tag: rel.TagName, asset: asset, checksum: checksumAsset, token: token}, nil
}

// publishedDigest reads the digest the latest release publishes for this
// platform's asset, without downloading the asset.
func publishedDigest(ctx context.Context, name, repo string) (string, error) {
	rel, err := resolveRelease(ctx, name, repo)
	if err != nil {
		return "", err
	}

	digest, err := publishedChecksum(ctx, rel.checksum, rel.token)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read %s from release %s", rel.checksum.Name, rel.tag)
	}

	return digest, nil
}

// fetchPlugin downloads name's binary from repo's latest release, verifies it
// against the published .sha256, and installs it. Returns the installed path.
func fetchPlugin(ctx context.Context, name, repo string, logger *zap.SugaredLogger) (string, error) {
	rel, err := resolveRelease(ctx, name, repo)
	if err != nil {
		return "", err
	}

	logger.Infow("Fetching plugin release asset",
		"plugin", name, "repo", repo, "release", rel.tag,
		"asset", rel.asset.Name, "bytes", rel.asset.Size)

	archive, err := downloadAsset(ctx, rel.asset, rel.token)
	if err != nil {
		return "", errors.Wrapf(err, "failed to download %s from release %s", rel.asset.Name, rel.tag)
	}

	want, err := publishedChecksum(ctx, rel.checksum, rel.token)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read %s from release %s", rel.checksum.Name, rel.tag)
	}

	got := hex.EncodeToString(sha256Of(archive))
	if got != want {
		err := errors.Newf("checksum mismatch for %s from release %s: published %s, downloaded %s",
			rel.asset.Name, rel.tag, want, got)
		return "", errors.WithHint(err, "the release asset and its .sha256 disagree; nothing was installed")
	}

	dir, err := PluginInstallPath(name)
	if err != nil {
		return "", err
	}

	binary, files, err := install(archive, dir, PluginBinaryName(name))
	if err != nil {
		return "", errors.Wrapf(err, "failed to install plugin %s to %s from %s", name, dir, rel.asset.Name)
	}

	// Recorded so a later start can tell what is installed from what is
	// published. Without it an install is permanent: a plugin on disk is
	// accepted whatever it is, so a broken build stays broken and a new
	// release never arrives.
	if err := recordInstalledDigest(dir, got); err != nil {
		return "", err
	}

	if err := removeLegacyInstall(name, logger); err != nil {
		return "", err
	}

	logger.Infow("Installed plugin",
		"plugin", name, "repo", repo, "release", rel.tag,
		"binary", binary, "files", files, "sha256", got)

	return binary, nil
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

// extractArchive unpacks a .tar.gz into dir, preserving its layout, and returns
// the path to binaryName within it along with the number of files written.
//
// The layout is preserved because it is load-bearing: a binary that ships its
// libraries in lib/ finds them through an RPATH relative to itself, so lib/ has
// to land beside the binary and nowhere else.
//
// Entry paths come from a downloaded archive, so they are not trusted. Anything
// absolute or climbing out of dir is refused rather than sanitised — a release
// that tries it is not one to install a corrected version of.
func extractArchive(archive []byte, dir, binaryName string) (string, int, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", 0, errors.Wrap(err, "asset is not gzip data")
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var seen []string
	var binary string
	files := 0

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", 0, errors.Wrap(err, "failed to read tar entry")
		}

		// Symlinks and devices are skipped, as they were before trees were
		// unpacked at all: a link is a way to name a file outside dir.
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
			continue
		}

		rel, err := safeArchivePath(header.Name)
		if err != nil {
			return "", 0, err
		}
		if rel == "" {
			continue
		}

		target := filepath.Join(dir, rel)

		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", 0, errors.Wrapf(err, "failed to create %s", target)
			}
			continue
		}

		seen = append(seen, rel)

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", 0, errors.Wrapf(err, "failed to create directory for %s", target)
		}

		// The archive's mode decides only whether a file is executable. A
		// release cannot make anything group- or world-writable here.
		mode := os.FileMode(0o644)
		if header.Mode&0o111 != 0 {
			mode = 0o755
		}

		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return "", 0, errors.Wrapf(err, "failed to create %s", target)
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return "", 0, errors.Wrapf(err, "failed to write %s", target)
		}
		if err := out.Close(); err != nil {
			return "", 0, errors.Wrapf(err, "failed to close %s", target)
		}

		files++

		if filepath.Base(rel) == binaryName {
			binary = target
			// The binary must be executable whatever the archive claimed —
			// tar modes survive some packaging pipelines and not others.
			if err := os.Chmod(target, 0o755); err != nil {
				return "", 0, errors.Wrapf(err, "failed to make %s executable", target)
			}
		}
	}

	if binary == "" {
		err := errors.Newf("archive contains no file named %s (has: %s)", binaryName, strings.Join(seen, ", "))
		return "", 0, errors.WithHintf(err, "the release asset must contain the plugin binary named %s", binaryName)
	}

	return binary, files, nil
}

// safeArchivePath rejects an archive entry that would write outside the
// directory it is being unpacked into. Returns the cleaned relative path, or
// empty for an entry that names the directory itself.
func safeArchivePath(name string) (string, error) {
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", errors.Newf("archive entry %q is an absolute path", name)
	}

	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == string(filepath.Separator) {
		return "", nil
	}

	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.Newf("archive entry %q escapes the plugin directory", name)
	}

	return clean, nil
}

// install unpacks archive into dir, replacing whatever was there, and returns
// the path to the plugin binary and the number of files written.
//
// Unpacked into a sibling temp directory and renamed, so a crash or a truncated
// download never leaves a half-tree where a plugin is expected. A partial tree
// is worse than a partial file: the binary can be complete while the library it
// needs is missing, which fails at exec with nothing to point at.
func install(archive []byte, dir, binaryName string) (string, int, error) {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", 0, errors.Wrapf(err, "failed to create plugin directory %s", parent)
	}

	staging, err := os.MkdirTemp(parent, filepath.Base(dir)+".partial-*")
	if err != nil {
		return "", 0, errors.Wrapf(err, "failed to create temp directory in %s", parent)
	}
	defer os.RemoveAll(staging)

	binary, files, err := extractArchive(archive, staging, binaryName)
	if err != nil {
		return "", 0, err
	}

	// Remove first: renaming over a populated directory fails, and a running
	// binary underneath may be read-only from a previous install.
	if err := os.RemoveAll(dir); err != nil {
		return "", 0, errors.Wrapf(err, "failed to remove existing %s", dir)
	}

	if err := os.Rename(staging, dir); err != nil {
		return "", 0, errors.Wrapf(err, "failed to move %s into place at %s", staging, dir)
	}

	// The binary's path was inside staging, which no longer exists.
	rel, err := filepath.Rel(staging, binary)
	if err != nil {
		return "", 0, errors.Wrapf(err, "failed to locate %s within %s", binaryName, staging)
	}

	return filepath.Join(dir, rel), files, nil
}
