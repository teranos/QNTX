package grpc

// A build QNTX made itself: the binary packaged as a plugin archive and
// installed the way a build the runner delivers is.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"io"
	"os"
	"time"

	"github.com/teranos/QNTX/internal/sqlclose"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// PackageBuild is a .tar.gz holding binary as name's plugin binary. Every
// timestamp is the epoch, so the same binary is always the same archive and a
// rebuild that changed nothing is not installed again.
func PackageBuild(name, binary string) ([]byte, error) {
	data, err := os.ReadFile(binary)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the built binary %s", binary)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	header := &tar.Header{
		Name:     PluginBinaryName(name),
		Mode:     0o755,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
		ModTime:  time.Unix(0, 0),
	}
	if err := tw.WriteHeader(header); err != nil {
		return nil, errors.Wrapf(err, "failed to package %s", binary)
	}
	if written, err := tw.Write(data); err != nil {
		return nil, errors.Wrapf(err, "failed to package %s: %d of its %d bytes went in", binary, written, len(data))
	}
	if err := tw.Close(); err != nil {
		return nil, errors.Wrapf(err, "failed to package %s", binary)
	}
	if err := gz.Close(); err != nil {
		return nil, errors.Wrapf(err, "failed to package %s", binary)
	}
	return buf.Bytes(), nil
}

// InstallBuild installs archive as name's build, unless it is the build already
// installed. It returns whether the plugin now has a different build, and the
// archive's digest.
func InstallBuild(name string, archive []byte, logger *zap.SugaredLogger) (bool, string, error) {
	digest := hex.EncodeToString(sha256Of(archive))
	return installBuild(name, digest, func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(archive)), nil }, logger)
}

// InstallArtifactBuild is InstallBuild of a build inside an artifact, checked
// as digest by ArtifactBuild: it is read out of the artifact as it is
// unpacked, and never held whole.
func InstallArtifactBuild(name string, archive ArtifactArchive, digest string, logger *zap.SugaredLogger) (bool, string, error) {
	return installBuild(name, digest, archive.Open, logger)
}

// installBuild installs the archive open reads, of digest, as name's build,
// unless it is the build already installed.
func installBuild(name, digest string, open func() (io.ReadCloser, error), logger *zap.SugaredLogger) (_ bool, _ string, err error) {
	dir, err := PluginInstallPath(name)
	if err != nil {
		return false, digest, err
	}
	if installed, ok := installedDigest(dir); ok && installed == digest {
		return false, digest, nil
	}
	archive, err := open()
	if err != nil {
		return false, digest, err
	}
	defer func() { err = sqlclose.With(err, archive.Close(), "the build of plugin "+name) }()
	binary, files, err := installFrom(archive, digest, dir, PluginBinaryName(name))
	if err != nil {
		return false, digest, errors.Wrapf(err, "failed to install the build of plugin %s to %s", name, dir)
	}
	if err := recordInstalledDigest(dir, digest); err != nil {
		return false, digest, err
	}
	if err := removeLegacyInstall(name, logger); err != nil {
		return false, digest, err
	}
	logger.Infow("Installed a plugin build", "plugin", name, "binary", binary, "files", files, "sha256", digest)
	return true, digest, nil
}
