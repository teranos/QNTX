package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// wholeRepository is abcd-nl/clean on trunk, its default branch: notes at its
// top and in folders deep in it, a story, a folder starting with a dot, and
// what is no note.
func wholeRepository(t *testing.T) (*QNTXServer, Vault) {
	t.Helper()
	s, vault := vaultBindingServer(t)
	stand.defaultBranch = "trunk"
	stand.branches["trunk"] = map[string]string{
		"CLAUDE.md":                        "# CLAUDE\n",
		"docs/README.md":                   "# docs\n",
		"docs/adr/ADR-001.md":              "# ADR-001\n",
		"stories/10-a.feature":             "Feature: a\n",
		".github/PULL_REQUEST_TEMPLATE.md": "# template\n",
		"source/app.d":                     "void main() {}\n",
	}
	return s, vault
}

// "- the repository/git binding"
// "- named as one git repository under one name, like teranos/ground currently is."
func TestARepositoryIsBoundWholeUnderOneName(t *testing.T) {
	s, vault := wholeRepository(t)
	_, refused := askVault(t, s, "repository", sigil.Sent{"name": "abcd", "repo": "github.com/abcd-nl/clean"})
	require.Nil(t, refused, "%v", refused)

	vaults, err := s.nodeRecords().Vaults()
	require.NoError(t, err)
	assert.Equal(t, []string{"github.com/abcd-nl/clean@trunk:.=abcd-nl/clean"}, vaults[0].Folders)

	// "we are missing some bindings like the ground CLAUDE.md, its other markdown document's"
	assert.Equal(t, "# CLAUDE\n", readNote(t, vault, "abcd-nl/clean/CLAUDE.md"))
	assert.Equal(t, "# docs\n", readNote(t, vault, "abcd-nl/clean/docs/README.md"))
	assert.Equal(t, "# ADR-001\n", readNote(t, vault, "abcd-nl/clean/docs/adr/ADR-001.md"))
	assert.Equal(t, "Feature: a\n", readNote(t, vault, "abcd-nl/clean/stories/10-a.feature.md"))
	assert.NoFileExists(t, filepath.Join(vault.Path, "abcd-nl", "clean", ".github", "PULL_REQUEST_TEMPLATE.md"))
	assert.NoFileExists(t, filepath.Join(vault.Path, "abcd-nl", "clean", "source", "app.d"))

	got, _ := askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	state := got.(*protocol.VaultStates).GetFolders()[0]
	assert.Equal(t, vaultActive, state.State, state.Why)
}

// The one name may be said; it is a folder's place, never a note's.
func TestARepositoryIsBoundUnderTheNameSaid(t *testing.T) {
	s, vault := wholeRepository(t)
	_, refused := askVault(t, s, "repository", sigil.Sent{"name": "abcd", "repo": "github.com/abcd-nl/clean", "place": "Course Material"})
	require.Nil(t, refused, "%v", refused)
	assert.Equal(t, "# CLAUDE\n", readNote(t, vault, "Course Material/CLAUDE.md"))

	_, refused = askVault(t, s, "repository", sigil.Sent{"name": "abcd", "repo": "github.com/abcd-nl/clean", "place": "Notes/CLAUDE.md"})
	require.NotNil(t, refused)
	assert.Equal(t, "place", refused.GetParam())
}

// A push changing a note anywhere in the repository reaches the vault; one
// changing what is no note, or a note under a dot, does not.
func TestAPushAnywhereInTheRepositoryReachesIt(t *testing.T) {
	s, vault := wholeRepository(t)
	_, refused := askVault(t, s, "repository", sigil.Sent{"name": "abcd", "repo": "github.com/abcd-nl/clean"})
	require.Nil(t, refused, "%v", refused)

	pushed := func(changed string) forge.Push {
		return forge.Push{Ref: "refs/heads/trunk", Repo: onGitHubTest("abcd-nl/clean"), Changed: []string{changed}}
	}
	assert.Empty(t, s.vaultsMovedBy(pushed("source/app.d")))
	assert.Empty(t, s.vaultsMovedBy(pushed(".github/PULL_REQUEST_TEMPLATE.md")))

	stand.branches["trunk"]["docs/adr/ADR-001.md"] = "# ADR-001\n\nPushed.\n"
	assert.Equal(t, []string{"abcd:abcd-nl/clean"}, s.vaultsMovedBy(pushed("docs/adr/ADR-001.md")))
	assert.Equal(t, "# ADR-001\n\nPushed.\n", readNote(t, vault, "abcd-nl/clean/docs/adr/ADR-001.md"))
}

// A note gone from the repository, deep in it, is gone from the vault.
func TestANoteGoneFromTheRepositoryIsGoneFromTheVault(t *testing.T) {
	s, vault := wholeRepository(t)
	_, refused := askVault(t, s, "repository", sigil.Sent{"name": "abcd", "repo": "github.com/abcd-nl/clean"})
	require.Nil(t, refused, "%v", refused)

	delete(stand.branches["trunk"], "docs/adr/ADR-001.md")
	filled := s.fillVault(t.Context(), "abcd", everyFolder)
	require.Len(t, filled, 1)
	require.NoError(t, filled[0].Refused)
	assert.Equal(t, []string{"abcd-nl/clean/docs/adr/ADR-001.md"}, filled[0].Removed)
	assert.NoFileExists(t, filepath.Join(vault.Path, "abcd-nl", "clean", "docs", "adr", "ADR-001.md"))
}

// What the vault changes, in any folder of the bound repository, reaches its
// branch where the repository has it.
func TestARepositoryBindingSendsEveryNote(t *testing.T) {
	s, vault := wholeRepository(t)
	_, refused := askVault(t, s, "repository", sigil.Sent{"name": "abcd", "repo": "github.com/abcd-nl/clean"})
	require.Nil(t, refused, "%v", refused)
	_, refused = askVault(t, s, "send", sigil.Sent{"name": "abcd", "place": "abcd-nl/clean", "branch": "obsidian-abcd"})
	require.Nil(t, refused, "%v", refused)
	assert.Empty(t, stand.commits)

	root := filepath.Join(vault.Path, "abcd-nl", "clean")
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# CLAUDE\n\nFrom the phone.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "adr", "ADR-002.md"), []byte("# ADR-002\n"), 0o644))

	sent := s.sendVault(t.Context(), "abcd", everyPlace)
	require.Len(t, sent, 1)
	require.NoError(t, sent[0].Refused)
	assert.ElementsMatch(t, []string{"CLAUDE.md", "docs/adr/ADR-002.md"}, sent[0].Committed)
	branch := stand.branches["obsidian-abcd"]
	assert.Equal(t, "# CLAUDE\n\nFrom the phone.\n", branch["CLAUDE.md"])
	assert.Equal(t, "# ADR-002\n", branch["docs/adr/ADR-002.md"])
	assert.Equal(t, "# template\n", branch[".github/PULL_REQUEST_TEMPLATE.md"])
}

// "oh, require it to be a dir in the repo , not in its root"
// A folder binding still is never the repository's top: the top is the
// repository binding's alone.
func TestTheTopIsTheRepositoryBindingsAlone(t *testing.T) {
	s, _ := wholeRepository(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "github.com/abcd-nl/clean", "path": "."})
	require.NotNil(t, refused)
	assert.Equal(t, "path", refused.GetParam())
}
