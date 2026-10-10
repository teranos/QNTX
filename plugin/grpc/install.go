package grpc

// Installing a plugin build: a .tar.gz verified against its .sha256 and
// unpacked into ~/.qntx/plugins/<name>/. The build comes from the Actions
// runner on this box (ADR-043); nothing is downloaded from GitHub.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/teranos/QNTX/internal/sqlclose"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

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

	// Nothing there is nothing to remove. Anything else is not knowing whether
	// there is, and reporting that as removed leaves a superseded binary on
	// disk that the next load may find first.
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrapf(err, "could not tell whether a superseded plugin binary is at %s", path)
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
// unpacked from. Comparing it against a cheap published digest is what makes an
// install reconcilable rather than permanent.
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

// sha256Of hashes the downloaded bytes.
func sha256Of(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
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
func extractArchive(archive []byte, dir, binaryName string) (_ string, _ int, err error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", 0, errors.Wrap(err, "asset is not gzip data")
	}
	defer func() { err = sqlclose.With(err, gz.Close(), "the archive gzip reader") }()

	tr := tar.NewReader(gz)
	var seen []string
	var binary string
	files := 0

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
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
			err = errors.Wrapf(err, "failed to write %s", target)
			err = sqlclose.With(err, out.Close(), target)
			return "", 0, err
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
func install(archive []byte, dir, binaryName string) (_ string, _ int, err error) {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", 0, errors.Wrapf(err, "failed to create plugin directory %s", parent)
	}

	staging, err := os.MkdirTemp(parent, filepath.Base(dir)+".partial-*")
	if err != nil {
		return "", 0, errors.Wrapf(err, "failed to create temp directory in %s", parent)
	}
	// A .partial-* directory left behind sits in the plugins directory for
	// every later listing to trip on, so failing to remove it is said.
	defer func() {
		if rmErr := os.RemoveAll(staging); rmErr != nil {
			err = sqlclose.With(err, rmErr, "the staging directory "+staging)
		}
	}()

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
