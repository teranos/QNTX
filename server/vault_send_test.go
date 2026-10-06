package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// "5. And the persistent branch is obsidian-[nameofvault] and it always
// contains the abcdsync diff, and people may merge it at times"
func TestTheVaultReachesTheBranchItIsToldTo(t *testing.T) {
	s, vault := vaultBindingServer(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)

	// Told to send, the branch is made from the default branch; the vault holds
	// what main holds, so nothing is committed and nothing is opened.
	_, refused = askVault(t, s, "send", sigil.Sent{"name": "abcd", "place": "Course Material", "branch": "obsidian-abcd"})
	require.Nil(t, refused, "%v", refused)
	require.Contains(t, stand.branches, "obsidian-abcd")
	assert.Empty(t, stand.commits)
	assert.Empty(t, stand.pulls)
	got, _ := askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	state := got.(map[string]any)["folders"].([]vaultFolderState)[0]
	assert.Equal(t, vaultUnchanged, state.State)
	assert.Equal(t, "obsidian-abcd", state.Branch)

	// A note changed, one added and one gone in the vault reach the branch, and a pull request is opened.
	at := filepath.Join(vault.Path, "Course Material")
	require.NoError(t, os.WriteFile(filepath.Join(at, "ADR-001.md"), []byte("# ADR-001\n\nAs the phone has it.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(at, "ADR-002.md"), []byte("# ADR-002\n"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(at, "old", "ADR-000.md")))

	sent := s.sendVault(t.Context(), "abcd", everyPlace)
	require.Len(t, sent, 1)
	require.NoError(t, sent[0].Refused)
	assert.Equal(t, []string{"docs/adr/ADR-001.md", "docs/adr/ADR-002.md"}, sent[0].Committed)
	assert.Equal(t, []string{"docs/adr/old/ADR-000.md"}, sent[0].Removed)
	assert.Equal(t, "https://github.com/abcd-nl/clean/pull/1", sent[0].Pull)
	branch := stand.branches["obsidian-abcd"]
	assert.Equal(t, "# ADR-001\n\nAs the phone has it.\n", branch["docs/adr/ADR-001.md"])
	assert.Equal(t, "# ADR-002\n", branch["docs/adr/ADR-002.md"])
	assert.NotContains(t, branch, "docs/adr/old/ADR-000.md")
	assert.Equal(t, "not a note", branch["docs/adr/diagram.png"], "what is not a note is not the vault's to remove")
	assert.Equal(t, "# ADR-001\n\nAs main has it.\n", mainNotes["docs/adr/ADR-001.md"], "main is changed by merging, never by the vault")

	got, _ = askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	state = got.(map[string]any)["folders"].([]vaultFolderState)[0]
	assert.Equal(t, vaultChanges, state.State)
	assert.Equal(t, "https://github.com/abcd-nl/clean/pull/1", state.Pull)

	// Nothing changed since: nothing is asked of GitHub, and one pull request stays open.
	assert.Empty(t, s.sendVault(t.Context(), "abcd", everyPlace))
	assert.Len(t, stand.commits, 3)
	assert.Len(t, stand.pulls, 1)
}

// Stopping sends nothing more; the branch and its pull request stay.
func TestAFolderStopsSending(t *testing.T) {
	s, vault := vaultBindingServer(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)
	_, refused = askVault(t, s, "send", sigil.Sent{"name": "abcd", "place": "Course Material", "branch": "obsidian-abcd"})
	require.Nil(t, refused, "%v", refused)
	_, refused = askVault(t, s, "send", sigil.Sent{"name": "abcd", "place": "Course Material"})
	require.Nil(t, refused, "%v", refused)

	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, "Course Material", "ADR-002.md"), []byte("# ADR-002\n"), 0o644))
	assert.Empty(t, s.sendVault(t.Context(), "abcd", everyPlace))
	assert.Empty(t, stand.commits)
	got, _ := askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	assert.Equal(t, vaultActive, got.(map[string]any)["folders"].([]vaultFolderState)[0].State)
}

// "i dont want that to be automatically opted in,"
func TestABoundFolderSendsNothingUntilToldTo(t *testing.T) {
	s, vault := vaultBindingServer(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)
	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, "Course Material", "new.md"), []byte("new\n"), 0o644))

	assert.Empty(t, s.sendVault(t.Context(), "abcd", everyPlace))
	assert.Len(t, stand.branches, 1, "a branch was made for a folder that does not send")
	assert.Empty(t, stand.commits)
}

// "and i want to set what the name of the branch would be in the obsidian element in the binding."
func TestABranchToSendToIsOneGitTakesAndNotTheDefault(t *testing.T) {
	s, _ := vaultBindingServer(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)
	for _, branch := range []string{"main", "two words", "a..b", "-x", "x.lock", "a:b"} {
		_, refused := askVault(t, s, "send", sigil.Sent{"name": "abcd", "place": "Course Material", "branch": branch})
		require.NotNil(t, refused, branch)
		assert.Equal(t, "branch", refused.GetParam(), branch)
	}
	_, refused = askVault(t, s, "send", sigil.Sent{"name": "abcd", "place": "ABCD", "branch": "obsidian-abcd"})
	require.NotNil(t, refused)
	assert.Equal(t, "place", refused.GetParam())
}

// "concept of default branch, not main or master"
func TestAFolderIsBoundToItsRepositorysDefaultBranch(t *testing.T) {
	s, _ := vaultBindingServer(t)
	stand.defaultBranch = "trunk"
	stand.branches["trunk"] = stand.branches["main"]

	got, refused := askVault(t, s, "subdirs", sigil.Sent{"repo": "abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)
	assert.Equal(t, "trunk", got.(map[string]any)["branch"])

	_, refused = askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)
	vaults, err := s.nodeRecords().Vaults()
	require.NoError(t, err)
	assert.Equal(t, []string{"abcd-nl/clean@trunk:docs/adr=Course Material"}, vaults[0].Folders)
}

// "it should show red, and have you redo the binding"
func TestABindingWhoseDefaultBranchMovedIsInvalid(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"abcd-nl/clean@main:docs/adr=Course Material"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))
	stand.defaultBranch = "trunk"

	got, refused := askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	require.Nil(t, refused, "%v", refused)
	state := got.(map[string]any)["folders"].([]vaultFolderState)[0]
	assert.Equal(t, vaultInvalid, state.State)
	assert.Equal(t, "the default branch of abcd-nl/clean is now trunk, not main: bind Course Material again", state.Why)
}
