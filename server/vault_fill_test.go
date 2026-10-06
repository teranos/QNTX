package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// A note is named as git names it, so the vault's copy is compared without
// asking GitHub for its content.
func TestANoteIsNamedAsGitNamesIt(t *testing.T) {
	// printf 'hello\n' | git hash-object --stdin
	assert.Equal(t, "ce013625030ba8dba906f756967f9e9ca394464a", gitBlobSHA([]byte("hello\n")))
}

func readNote(t *testing.T, vault Vault, place string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(vault.Path, filepath.FromSlash(place)))
	require.NoError(t, err)
	return string(content)
}

// Binding a folder writes its repository folder's notes into it, folders and all.
func TestBindingAFolderFillsItFromMain(t *testing.T) {
	s, vault := vaultBindingServer(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)

	assert.Equal(t, mainNotes["docs/adr/ADR-001.md"], readNote(t, vault, "Course Material/ADR-001.md"))
	assert.Equal(t, mainNotes["docs/adr/old/ADR-000.md"], readNote(t, vault, "Course Material/old/ADR-000.md"))
	// Only notes: what is not one stays in the repository.
	assert.NoFileExists(t, filepath.Join(vault.Path, "Course Material", "diagram.png"))
}

// "main wins"
// A note changed in the vault is main's again, and a note only the vault has
// is left as it is.
func TestMainWins(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"abcd-nl/clean@main:docs/adr=Course Material"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))
	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, "Course Material", "ADR-001.md"), []byte("# ADR-001\n\nAs the vault has it.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, "Course Material", "only here.md"), []byte("mine\n"), 0o644))

	filled := s.fillVault(t.Context(), "abcd", everyFolder)
	require.Len(t, filled, 1)
	require.NoError(t, filled[0].Refused)
	assert.ElementsMatch(t, []string{"Course Material/ADR-001.md", "Course Material/old/ADR-000.md"}, filled[0].Wrote)
	assert.Equal(t, mainNotes["docs/adr/ADR-001.md"], readNote(t, vault, "Course Material/ADR-001.md"))
	assert.Equal(t, "mine\n", readNote(t, vault, "Course Material/only here.md"))

	// What the vault already holds as main has it is not written again.
	again := s.fillVault(t.Context(), "abcd", everyFolder)
	require.Len(t, again, 1)
	assert.Empty(t, again[0].Wrote)
	assert.Equal(t, 2, again[0].Same)
}

// A push to main that changes a note of a bound folder writes it into the vault.
func TestAPushToMainReachesTheVault(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"abcd-nl/clean@main:docs/adr=Course Material"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))
	s.fillVault(t.Context(), "abcd", everyFolder)

	mainNotes["docs/adr/ADR-001.md"] = "# ADR-001\n\nPushed to main.\n"
	push := gitHubPush{Ref: "refs/heads/main"}
	push.Repository.FullName = "abcd-nl/clean"
	push.Commits = append(push.Commits, struct {
		Added    []string `json:"added"`
		Removed  []string `json:"removed"`
		Modified []string `json:"modified"`
	}{Modified: []string{"docs/adr/ADR-001.md"}})

	assert.Equal(t, []string{"abcd:Course Material"}, s.vaultsMovedBy(push))
	assert.Equal(t, "# ADR-001\n\nPushed to main.\n", readNote(t, vault, "Course Material/ADR-001.md"))
}

// A push elsewhere, or to a disabled folder, writes nothing.
func TestAPushElsewhereLeavesTheVault(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"abcd-nl/clean@main:docs/adr=Course Material"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))
	s.fillVault(t.Context(), "abcd", everyFolder)
	mainNotes["docs/adr/ADR-001.md"] = "# ADR-001\n\nPushed to main.\n"

	pushed := func(repo, ref, changed string) gitHubPush {
		push := gitHubPush{Ref: ref}
		push.Repository.FullName = repo
		push.Commits = append(push.Commits, struct {
			Added    []string `json:"added"`
			Removed  []string `json:"removed"`
			Modified []string `json:"modified"`
		}{Modified: []string{changed}})
		return push
	}
	for name, push := range map[string]gitHubPush{
		"another branch":     pushed("abcd-nl/clean", "refs/heads/obsidian-abcd", "docs/adr/ADR-001.md"),
		"another repository": pushed("abcd-nl/other", "refs/heads/main", "docs/adr/ADR-001.md"),
		"another folder":     pushed("abcd-nl/clean", "refs/heads/main", "cdr/CDR-001.md"),
		"a folder's sibling": pushed("abcd-nl/clean", "refs/heads/main", "docs/adr-old/x.md"),
	} {
		assert.Empty(t, s.vaultsMovedBy(push), name)
	}

	_, refused := askVault(t, s, "disable", sigil.Sent{"name": "abcd", "place": "Course Material"})
	require.Nil(t, refused, "%v", refused)
	assert.Empty(t, s.vaultsMovedBy(pushed("abcd-nl/clean", "refs/heads/main", "docs/adr/ADR-001.md")), "disabled")
	assert.Equal(t, "# ADR-001\n\nAs main has it.\n", readNote(t, vault, "Course Material/ADR-001.md"))
}

// A folder that could not be filled says why, and writes nothing.
func TestAFolderNotFilledSaysWhy(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"abcd-nl/clean@main:gone=Course Material"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))

	filled := s.fillVault(t.Context(), "abcd", everyFolder)
	require.Len(t, filled, 1)
	require.Error(t, filled[0].Refused)
	assert.Contains(t, filled[0].Refused.Error(), "404")
	assert.Empty(t, filled[0].Wrote)
}
