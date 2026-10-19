package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/internal/forge"
)

// A forge the node has no reader for is refused naming it, by everything that
// reads a repository: nothing is asked of GitHub in its place.
func TestARepositoryOnAForgeTheNodeDoesNotReadIsRefusedByEveryReader(t *testing.T) {
	s, _ := vaultBindingServer(t)
	gitlab, err := forge.ParseRepo("gitlab.com/group/sub/project")
	require.NoError(t, err)

	_, err = s.repoDefaultBranch(t.Context(), gitlab)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")

	_, err = s.repoDirs(t.Context(), gitlab, "main", "docs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")

	_, err = s.repoNotes(t.Context(), gitlab, "main", "docs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")

	_, err = s.repoNoteContent(t.Context(), gitlab, "main", "docs/a.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")

	_, err = s.buildRev(t.Context(), buildSource{Repo: gitlab, Branch: "main"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")
}
