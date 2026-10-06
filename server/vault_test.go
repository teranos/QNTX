package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "name both ends"

// A repository's folder, read as a build's input is, and where in the vault it is.
func TestAVaultHoldsFoldersNamedAtBothEnds(t *testing.T) {
	folders, err := vaultFolders("abcd-nl/clean@main:cdr=ABCD/clean/cdr  abcd-nl/clean-business@main:WEEK_41.md=ABCD/weeks/WEEK_41.md")
	require.NoError(t, err)
	assert.Equal(t, []string{"abcd-nl/clean@main:cdr=ABCD/clean/cdr", "abcd-nl/clean-business@main:WEEK_41.md=ABCD/weeks/WEEK_41.md"}, folders)

	none, err := vaultFolders("")
	require.NoError(t, err)
	assert.Equal(t, []string{}, none)

	// A place is kept as it reads, without what says nothing.
	tidy, err := vaultFolders("abcd-nl/clean@main:cdr=ABCD//clean/./cdr/")
	require.NoError(t, err)
	assert.Equal(t, []string{"abcd-nl/clean@main:cdr=ABCD/clean/cdr"}, tidy)
}

// A folder missing either end is refused, and with it the whole vault.
func TestAVaultRefusesAFolderThatDoesNotRead(t *testing.T) {
	for _, said := range []string{
		"abcd-nl/clean@main:cdr",
		"abcd-nl/clean@main:cdr=",
		"abcd-nl/clean@main=ABCD/clean",
		"abcd-nl/clean:cdr=ABCD/clean",
		"clean@main:cdr=ABCD/clean",
		"abcd-nl/clean@main:cdr=ABCD/clean abcd-nl/clean@main:docs",
	} {
		_, err := vaultFolders(said)
		assert.Error(t, err, said)
	}
}

// A place is inside the vault.
func TestAVaultRefusesAPlaceOutsideIt(t *testing.T) {
	for _, said := range []string{
		"abcd-nl/clean@main:cdr=/etc/cdr",
		"abcd-nl/clean@main:cdr=../cdr",
		"abcd-nl/clean@main:cdr=ABCD/../../cdr",
	} {
		_, err := vaultFolders(said)
		assert.Error(t, err, said)
	}
}

// A note is one folder's: no two places are one, and none holds another.
func TestAVaultRefusesTwoFoldersAtOnePlace(t *testing.T) {
	for _, said := range []string{
		"abcd-nl/clean@main:cdr=ABCD/docs abcd-nl/q@main:docs=ABCD/docs",
		"abcd-nl/clean@main:cdr=ABCD abcd-nl/q@main:docs=ABCD/docs",
		"abcd-nl/clean@main:cdr=ABCD/docs/cdr abcd-nl/q@main:docs=ABCD/docs",
	} {
		_, err := vaultFolders(said)
		assert.Error(t, err, said)
	}
	// Two places that only share a beginning are two places.
	_, err := vaultFolders("abcd-nl/clean@main:cdr=ABCD/doc abcd-nl/q@main:docs=ABCD/docs")
	assert.NoError(t, err)
}

func TestTheVaultSignumSaysWhatItHolds(t *testing.T) {
	s := &QNTXServer{}
	require.NoError(t, s.vaultSignum().Check())
}
