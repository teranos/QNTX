package grpc

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	got, digest, err := ArtifactBuild("pyre", zipped)
	require.NoError(t, err)
	assert.Equal(t, archive, got)
	sum := sha256.Sum256(archive)
	assert.Equal(t, hex.EncodeToString(sum[:]), digest)
}

// An artifact is checked against its .sha256 as a build under the runner is.
func TestAnArtifactThatDisagreesWithItsDigestIsRefused(t *testing.T) {
	zipped, _ := builtArtifact(t, "pyre", "1.0.0", []byte("\x7fELF pyre"), "0000000000000000000000000000000000000000000000000000000000000000")

	_, _, err := ArtifactBuild("pyre", zipped)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest")
}

// A build with no .sha256 beside it is not one QNTX can check, and is refused.
func TestAnArtifactWithoutADigestIsRefused(t *testing.T) {
	archive := tarGz(t, map[string][]byte{PluginBinaryName("pyre"): []byte("\x7fELF pyre")})
	file := buildArchive("pyre", "1.0.0")

	_, _, err := ArtifactBuild("pyre", zipOf(t, map[string][]byte{file: archive}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), file+digestSuffix)
}

// An artifact holding another plugin's build holds none of this one.
func TestAnArtifactOfAnotherPluginIsRefused(t *testing.T) {
	zipped, _ := builtArtifact(t, "duif", "1.0.0", []byte("\x7fELF duif"), "")

	_, _, err := ArtifactBuild("pyre", zipped)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pyre")
}

// What GitHub served that is no zip is said, not unpacked.
func TestAnArtifactThatIsNoZipIsRefused(t *testing.T) {
	_, _, err := ArtifactBuild("pyre", []byte("not a zip"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zip")
}
