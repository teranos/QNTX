package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A vault's folders are read as a build's inputs are: owner/repo@branch:path.
func TestAVaultHoldsFoldersNamedAsABuildsInputs(t *testing.T) {
	folders, err := vaultFolders("abcd-nl/clean@main:cdr  abcd-nl/clean-business@main:WEEK_41.md")
	require.NoError(t, err)
	assert.Equal(t, []string{"abcd-nl/clean@main:cdr", "abcd-nl/clean-business@main:WEEK_41.md"}, folders)

	none, err := vaultFolders("")
	require.NoError(t, err)
	assert.Equal(t, []string{}, none)
}

// A folder that names no path, branch or repository is refused, and with it
// the whole vault.
func TestAVaultRefusesAFolderThatDoesNotRead(t *testing.T) {
	for _, said := range []string{
		"abcd-nl/clean@main",
		"abcd-nl/clean:cdr",
		"clean@main:cdr",
		"abcd-nl/clean@main:cdr abcd-nl/clean@main",
	} {
		_, err := vaultFolders(said)
		assert.Error(t, err, said)
	}
}

func TestTheVaultSignumSaysWhatItHolds(t *testing.T) {
	s := &QNTXServer{}
	require.NoError(t, s.vaultSignum().Check())
}
