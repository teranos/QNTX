package server

// "the feature i am imagining is called comet"

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestCometSandIsGroundsControlsAndTheProjectsAlone(t *testing.T) {
	// One project block naming the checkout: wind folds that project's controls and nothing of anyone's.
	assert.Equal(t, "project {\n  path: \"/b/grove\"\n}\n", cometDeclaration("/b/grove"))
}
