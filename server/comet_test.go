package server

// "the feature i am imagining is called comet"

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCometIsNamedByItsRepository(t *testing.T) {
	owner, repo, err := cometOf("/comet/abcd-nl/grove")
	require.NoError(t, err)
	assert.Equal(t, "abcd-nl", owner)
	assert.Equal(t, "grove", repo)

	for _, path := range []string{"/comet/", "/comet/abcd-nl", "/comet/abcd-nl/", "/comet/abcd-nl/grove/more", "/comet//grove", "/comet/../grove"} {
		_, _, err := cometOf(path)
		assert.Error(t, err, path)
	}
}

func TestAmGroundNamesEachCometTheNodeKeeps(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "abcd-nl", "grove", "ground")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o755))
	require.NoError(t, keepDigest(kept))
	// A build still landing is not a comet yet.
	require.NoError(t, os.WriteFile(kept+".new", []byte("half"), 0o755))

	comets, err := heldComets(dir)
	require.NoError(t, err)
	require.Len(t, comets, 1)
	assert.Equal(t, "abcd-nl/grove", comets[0]["repository"])
	assert.Equal(t, int64(7), comets[0]["size"])
	sum := sha256.Sum256([]byte("a comet"))
	assert.Equal(t, hex.EncodeToString(sum[:]), comets[0]["digest"])

	// A node that has built none keeps none, and says so as an empty list.
	none, err := heldComets(filepath.Join(dir, "nothing"))
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestACometIsServedWithItsDigestAndAskedForByIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	kept := filepath.Join(home, ".qntx", "comets", "abcd-nl", "grove", "ground")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o755))
	require.NoError(t, keepDigest(kept))
	sum := sha256.Sum256([]byte("a comet"))
	s := &QNTXServer{}

	got := httptest.NewRecorder()
	s.HandleComet(got, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/grove", nil))
	assert.Equal(t, http.StatusOK, got.Code)
	assert.Equal(t, hex.EncodeToString(sum[:]), got.Header().Get("X-Comet-Digest"))
	assert.Equal(t, "a comet", got.Body.String())

	// Asking whether it moved costs a header, not the binary.
	head := httptest.NewRecorder()
	s.HandleComet(head, httptest.NewRequest(http.MethodHead, "/comet/abcd-nl/grove", nil))
	assert.Equal(t, http.StatusOK, head.Code)
	assert.Equal(t, hex.EncodeToString(sum[:]), head.Header().Get("X-Comet-Digest"))
	assert.Empty(t, head.Body.String())

	none := httptest.NewRecorder()
	s.HandleComet(none, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/other", nil))
	assert.Equal(t, http.StatusNotFound, none.Code)

	// The digest alone, as text: what ground reads every few seconds.
	digest := httptest.NewRecorder()
	s.HandleComet(digest, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/grove?digest", nil))
	assert.Equal(t, http.StatusOK, digest.Code)
	assert.JSONEq(t, `{"digest":"`+hex.EncodeToString(sum[:])+`"}`, digest.Body.String())
}

func TestACometsDigestIsWrittenOnceBesideIt(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "ground")
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o755))
	sum := sha256.Sum256([]byte("a comet"))

	require.NoError(t, keepDigest(kept))
	written, err := os.ReadFile(kept + ".digest")
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(sum[:]), string(written))

	// Read from beside it, not hashed again.
	require.NoError(t, os.WriteFile(kept+".digest", []byte("said"), 0o644))
	said, err := cometDigest(kept)
	require.NoError(t, err)
	assert.Equal(t, "said", said)
}

func TestCometSandIsGroundsControlsAndTheProjectsAlone(t *testing.T) {
	// One project block naming the checkout: wind folds that project's controls and nothing of anyone's.
	assert.Equal(t, "project {\n  path: \"/b/grove\"\n}\n", cometDeclaration("/b/grove"))
}
