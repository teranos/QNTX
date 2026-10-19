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

// Writing and checking go through the same lookup: a repository on a forge the
// node does not reach is refused naming it, and nothing is asked of GitHub.
func TestAWriteOrACheckOnAForgeTheNodeDoesNotReachIsRefusedNamingIt(t *testing.T) {
	s, _ := vaultBindingServer(t)
	gitlab, err := forge.ParseRepo("gitlab.com/group/sub/project")
	require.NoError(t, err)
	source := buildSource{Repo: gitlab, Branch: "main", Path: "docs", PathNamed: true}

	err = s.branchHeld(t.Context(), source, "obsidian-abcd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")

	_, _, err = s.sendState(t.Context(), source, "obsidian-abcd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")

	_, err = s.pullHeld(t.Context(), source, "obsidian-abcd", "abcd", "Course Material", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")

	_, err = s.commitChecks(t.Context(), gitlab, "c0ffee")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")
}

// A forge asked to act as nobody is refused, not given an identity.
func TestAForgeIsNotActedOnAsNobody(t *testing.T) {
	s, _ := vaultBindingServer(t)
	repo, err := forge.ParseRepo("github.com/abcd-nl/clean")
	require.NoError(t, err)
	host, err := s.forges().For(repo)
	require.NoError(t, err)

	var nobody forge.Acting
	_, err = host.Branch(t.Context(), nobody, repo, "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "as nobody")
	err = host.AddFile(t.Context(), nobody, repo, "main", "a.md", nil, "m")
	require.Error(t, err)
	_, err = host.Checks(t.Context(), nobody, repo, "main", forge.EveryCheck)
	require.Error(t, err)
}
