package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

const story = "Feature: USER connects Obsidian\n\n  Scenario: USER opens Obsidian from Connectors\n    Given I am in Connectors\n"

// "i see, but i want those .feature files to be authored in Obsidian"
func TestAStoryIsANoteInTheVault(t *testing.T) {
	s, vault := vaultBindingServer(t)
	mainNotes["docs/adr/13-user-connects-obsidian.feature"] = story
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "github.com/abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)

	assert.Equal(t, story, readNote(t, vault, "Course Material/13-user-connects-obsidian.feature.md"))
	assert.NoFileExists(t, filepath.Join(vault.Path, "Course Material", "13-user-connects-obsidian.feature"))
}

// "yes, because i would love to author these things myself."
func TestAStoryAuthoredInTheVaultReachesTheBranchAsTheStory(t *testing.T) {
	s, vault := vaultBindingServer(t)
	mainNotes["docs/adr/13-user-connects-obsidian.feature"] = story
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "github.com/abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)
	_, refused = askVault(t, s, "send", sigil.Sent{"name": "abcd", "place": "Course Material", "branch": "obsidian-abcd"})
	require.Nil(t, refused, "%v", refused)

	authored := story + "    When I press Obsidian\n"
	at := filepath.Join(vault.Path, "Course Material")
	require.NoError(t, os.WriteFile(filepath.Join(at, "13-user-connects-obsidian.feature.md"), []byte(authored), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(at, "14-user-sets-an-openrouter-key.feature.md"), []byte("Feature: USER sets an OpenRouter key\n"), 0o644))

	sent := s.sendVault(t.Context(), "abcd", everyPlace)
	require.Len(t, sent, 1)
	require.NoError(t, sent[0].Refused)
	assert.Equal(t, []string{"docs/adr/13-user-connects-obsidian.feature", "docs/adr/14-user-sets-an-openrouter-key.feature"}, sent[0].Committed)
	assert.Empty(t, sent[0].Removed)
	branch := stand.branches["obsidian-abcd"]
	assert.Equal(t, authored, branch["docs/adr/13-user-connects-obsidian.feature"])
	assert.Equal(t, "Feature: USER sets an OpenRouter key\n", branch["docs/adr/14-user-sets-an-openrouter-key.feature"])
	assert.NotContains(t, branch, "docs/adr/13-user-connects-obsidian.feature.md")
	assert.NotContains(t, branch, "docs/adr/14-user-sets-an-openrouter-key.feature.md")
}

// A story and a note named as its note would be one note in the vault: the folder is not filled.
func TestAStoryAndItsNoteNameAreNotBothFilled(t *testing.T) {
	s, vault := vaultBindingServer(t)
	mainNotes["docs/adr/13-user-connects-obsidian.feature"] = story
	mainNotes["docs/adr/13-user-connects-obsidian.feature.md"] = "# a note\n"
	vault.Folders = []string{"github.com/abcd-nl/clean@main:docs/adr=Course Material"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))

	filled := s.fillVault(t.Context(), "abcd", everyFolder)
	require.Len(t, filled, 1)
	require.Error(t, filled[0].Refused)
	assert.Contains(t, filled[0].Refused.Error(), "are both 13-user-connects-obsidian.feature.md in the vault")
	assert.NoFileExists(t, filepath.Join(vault.Path, "Course Material", "13-user-connects-obsidian.feature.md"))
}
