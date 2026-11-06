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

// "i want inside of my obsidian:"
// "QNTX/AgentInstructions/QNTX/AGENTS.md"
// "i want single file bindings that follow that format"
func TestANoteIsBoundToItsOwnPlace(t *testing.T) {
	s, vault := vaultBindingServer(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "QNTX/AgentInstructions/QNTX/AGENTS.md", "repo": "github.com/abcd-nl/clean", "path": "docs/adr/ADR-001.md"})
	require.Nil(t, refused, "%v", refused)

	vaults, err := s.nodeRecords().Vaults()
	require.NoError(t, err)
	assert.Equal(t, []string{"github.com/abcd-nl/clean@main:docs/adr/ADR-001.md=QNTX/AgentInstructions/QNTX/AGENTS.md"}, vaults[0].Folders)
	assert.Equal(t, mainNotes["docs/adr/ADR-001.md"], readNote(t, vault, "QNTX/AgentInstructions/QNTX/AGENTS.md"))
	// Only the one note: its folder's others stay in the repository.
	entries, err := os.ReadDir(filepath.Join(vault.Path, "QNTX", "AgentInstructions", "QNTX"))
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	got, _ := askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	assert.Equal(t, vaultActive, got.(*protocol.VaultStates).GetFolders()[0].State)
}

// A file at the repository's top can be bound. A folder still cannot be the top.
func TestANoteAtTheRepositorysTopIsBound(t *testing.T) {
	s, _ := vaultBindingServer(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "README.md", "repo": "github.com/abcd-nl/clean", "path": "README.md"})
	require.Nil(t, refused, "%v", refused)
	got, _ := askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	state := got.(*protocol.VaultStates).GetFolders()[0]
	assert.Equal(t, "github.com/abcd-nl/clean@main:README.md=README.md", state.Folder)
	assert.Equal(t, vaultActive, state.State, state.Why)
}

// A note is bound to a note's place, and a folder to a folder's.
func TestANoteIsNeverBoundToAFolder(t *testing.T) {
	s, _ := vaultBindingServer(t)
	for place, path := range map[string]string{
		"Course Material":       "docs/adr/ADR-001.md",
		"Course Material/x.txt": "docs/adr/ADR-001.md",
	} {
		_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": place, "repo": "github.com/abcd-nl/clean", "path": path})
		require.NotNil(t, refused, place)
		assert.Equal(t, "place", refused.GetParam(), place)
	}
	for _, folders := range []string{
		"github.com/abcd-nl/clean@main:docs/adr=Notes/ADR.md",
		"github.com/abcd-nl/clean@main:docs/adr/ADR-001.md=Notes",
		"github.com/abcd-nl/clean@main:docs/adr=Notes github.com/abcd-nl/clean@main:docs/adr/ADR-001.md=Notes/ADR-001.md",
	} {
		_, refused := askVault(t, s, "set", sigil.Sent{"name": "abcd", "path": "/vault", "folders": folders})
		require.NotNil(t, refused, folders)
		assert.Equal(t, "folders", refused.GetParam(), folders)
	}
}

// Two notes of one folder keep apart: one gone from main is gone from the
// vault, and the other stays.
func TestTwoNotesOfOneFolderKeepApart(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{
		"github.com/abcd-nl/clean@main:docs/adr/ADR-001.md=Notes/One.md",
		"github.com/abcd-nl/clean@main:docs/adr/old/ADR-000.md=Notes/Zero.md",
	}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))
	s.fillVault(t.Context(), "abcd", everyFolder)
	assert.Equal(t, mainNotes["docs/adr/old/ADR-000.md"], readNote(t, vault, "Notes/Zero.md"))

	delete(mainNotes, "docs/adr/old/ADR-000.md")
	filled := s.fillVault(t.Context(), "abcd", everyFolder)
	require.Len(t, filled, 2)
	require.NoError(t, filled[0].Refused)
	require.NoError(t, filled[1].Refused)
	assert.Empty(t, filled[0].Removed)
	assert.Equal(t, []string{"Notes/Zero.md"}, filled[1].Removed)
	assert.NoFileExists(t, filepath.Join(vault.Path, "Notes", "Zero.md"))
	assert.Equal(t, mainNotes["docs/adr/ADR-001.md"], readNote(t, vault, "Notes/One.md"))
}

// A push to main reaches a bound note when it changed that note, and not when
// it changed another of its folder.
func TestAPushReachesTheNoteItChanged(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"github.com/abcd-nl/clean@main:docs/adr/ADR-001.md=Notes/One.md"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))
	s.fillVault(t.Context(), "abcd", everyFolder)
	mainNotes["docs/adr/ADR-001.md"] = "# ADR-001\n\nPushed to main.\n"

	pushed := func(changed string) forge.Push {
		return forge.Push{Ref: "refs/heads/main", Repo: onGitHubTest("abcd-nl/clean"), Changed: []string{changed}}
	}
	assert.Empty(t, s.vaultsMovedBy(pushed("docs/adr/old/ADR-000.md")))
	assert.Equal(t, []string{"abcd:Notes/One.md"}, s.vaultsMovedBy(pushed("docs/adr/ADR-001.md")))
	assert.Equal(t, "# ADR-001\n\nPushed to main.\n", readNote(t, vault, "Notes/One.md"))
}

// A bound note that sends commits to its own file on the branch, and nothing
// else of its folder.
func TestABoundNoteReachesItsBranch(t *testing.T) {
	s, vault := vaultBindingServer(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Notes/One.md", "repo": "github.com/abcd-nl/clean", "path": "docs/adr/ADR-001.md"})
	require.Nil(t, refused, "%v", refused)
	_, refused = askVault(t, s, "send", sigil.Sent{"name": "abcd", "place": "Notes/One.md", "branch": "obsidian-abcd"})
	require.Nil(t, refused, "%v", refused)
	assert.Empty(t, stand.commits)

	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, "Notes", "One.md"), []byte("# ADR-001\n\nAs the phone has it.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, "Notes", "Two.md"), []byte("not bound\n"), 0o644))

	sent := s.sendVault(t.Context(), "abcd", everyPlace)
	require.Len(t, sent, 1)
	require.NoError(t, sent[0].Refused)
	assert.Equal(t, []string{"docs/adr/ADR-001.md"}, sent[0].Committed)
	assert.Empty(t, sent[0].Removed)
	branch := stand.branches["obsidian-abcd"]
	assert.Equal(t, "# ADR-001\n\nAs the phone has it.\n", branch["docs/adr/ADR-001.md"])
	assert.Equal(t, "# ADR-000\n", branch["docs/adr/old/ADR-000.md"])
	assert.NotContains(t, branch, "docs/adr/Two.md")
}

// A folder bound again as one of its notes keeps what main last agreed on:
// the vault's edit stays, and nothing fails.
func TestAFolderBoundAgainAsANoteKeepsItsBase(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"github.com/abcd-nl/clean@main:docs/adr=Course Material"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))
	s.fillVault(t.Context(), "abcd", everyFolder)
	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, "Course Material", "ADR-001.md"), []byte("# ADR-001\n\nWritten in the vault.\n"), 0o644))

	vault.Folders = []string{"github.com/abcd-nl/clean@main:docs/adr/ADR-001.md=Course Material/ADR-001.md"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))
	filled := s.fillVault(t.Context(), "abcd", everyFolder)
	require.Len(t, filled, 1)
	require.NoError(t, filled[0].Refused)
	assert.Equal(t, []string{"Course Material/ADR-001.md"}, filled[0].Kept)
	assert.Equal(t, "# ADR-001\n\nWritten in the vault.\n", readNote(t, vault, "Course Material/ADR-001.md"))
}

// A bound note whose folder in the vault is a link sends nothing of what it points at.
func TestABoundNoteInALinkedFolderSendsNothing(t *testing.T) {
	s, vault := vaultBindingServer(t)
	elsewhere := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "One.md"), []byte("not the vault's\n"), 0o644))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(vault.Path, "Linked")))
	vault.Folders = []string{"github.com/abcd-nl/clean@main:docs/adr/ADR-001.md=Linked/One.md"}
	vault.Sends = map[string]string{"Linked/One.md": "obsidian-abcd"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))

	assert.Empty(t, s.sendVault(t.Context(), "abcd", everyPlace))
	assert.Empty(t, stand.commits)
}
