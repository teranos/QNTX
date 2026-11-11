package grpc

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// zipOf is an Actions artifact holding files, as GitHub serves it.
func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// builtArtifact is the artifact a plugin's workflow uploads: the archive its
// package step wrote and the .sha256 beside it, saying digest.
func builtArtifact(t *testing.T, name, version string, binary []byte, digestOverride string) ([]byte, []byte) {
	t.Helper()
	archive := tarGz(t, map[string][]byte{PluginBinaryName(name): binary})
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	if digestOverride != "" {
		digest = digestOverride
	}
	file := buildArchive(name, version)
	return zipOf(t, map[string][]byte{file: archive, file + digestSuffix: []byte(digest + "  " + file + "\n")}), archive
}

// A plugin built on another machine comes as its workflow run's artifact, and
// the archive in it is the build.
func TestABuildIsReadOutOfItsArtifact(t *testing.T) {
	zipped, archive := builtArtifact(t, "pyre", "1.0.0", []byte("\x7fELF pyre"), "")

	got, digest, err := ArtifactBuild("pyre", bytes.NewReader(zipped), int64(len(zipped)))
	require.NoError(t, err)
	assert.Equal(t, archive, readOut(t, got))
	sum := sha256.Sum256(archive)
	assert.Equal(t, hex.EncodeToString(sum[:]), digest)
}

// readOut is what an artifact's build holds, read out of the artifact.
func readOut(t *testing.T, archive ArtifactArchive) []byte {
	t.Helper()
	opened, err := archive.Open()
	require.NoError(t, err)
	held, err := io.ReadAll(opened)
	require.NoError(t, err)
	require.NoError(t, opened.Close())
	return held
}

// A build installed from an artifact is read out of the artifact as it is
// unpacked: the digest it was checked against is the one kept, and the same
// build again changes nothing.
func TestABuildIsInstalledFromItsArtifact(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	zipped, _ := builtArtifact(t, "pyre", "1.0.0", []byte("\x7fELF pyre"), "")
	archive, digest, err := ArtifactBuild("pyre", bytes.NewReader(zipped), int64(len(zipped)))
	require.NoError(t, err)

	changed, installed, err := InstallArtifactBuild("pyre", archive, digest, zap.NewNop().Sugar())
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, digest, installed)
	dir, err := PluginInstallPath("pyre")
	require.NoError(t, err)
	held, err := os.ReadFile(filepath.Join(dir, PluginBinaryName("pyre")))
	require.NoError(t, err)
	assert.Equal(t, "\x7fELF pyre", string(held))
	kept, ok := InstalledDigest("pyre")
	require.True(t, ok)
	assert.Equal(t, digest, kept)

	changed, _, err = InstallArtifactBuild("pyre", archive, digest, zap.NewNop().Sugar())
	require.NoError(t, err)
	assert.False(t, changed)
}

// An artifact is checked against its .sha256 as a build under the runner is.
func TestAnArtifactThatDisagreesWithItsDigestIsRefused(t *testing.T) {
	zipped, _ := builtArtifact(t, "pyre", "1.0.0", []byte("\x7fELF pyre"), "0000000000000000000000000000000000000000000000000000000000000000")

	_, _, err := ArtifactBuild("pyre", bytes.NewReader(zipped), int64(len(zipped)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest")
}

// A build with no .sha256 beside it is not one QNTX can check, and is refused.
func TestAnArtifactWithoutADigestIsRefused(t *testing.T) {
	archive := tarGz(t, map[string][]byte{PluginBinaryName("pyre"): []byte("\x7fELF pyre")})
	file := buildArchive("pyre", "1.0.0")
	only := zipOf(t, map[string][]byte{file: archive})

	_, _, err := ArtifactBuild("pyre", bytes.NewReader(only), int64(len(only)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), file+digestSuffix)
}

// An artifact holding another plugin's build holds none of this one.
func TestAnArtifactOfAnotherPluginIsRefused(t *testing.T) {
	zipped, _ := builtArtifact(t, "duif", "1.0.0", []byte("\x7fELF duif"), "")

	_, _, err := ArtifactBuild("pyre", bytes.NewReader(zipped), int64(len(zipped)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pyre")
}

// What GitHub served that is no zip is said, not unpacked.
func TestAnArtifactThatIsNoZipIsRefused(t *testing.T) {
	_, _, err := ArtifactBuild("pyre", bytes.NewReader([]byte("not a zip")), int64(len("not a zip")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zip")
}
