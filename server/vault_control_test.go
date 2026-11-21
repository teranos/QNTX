package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

const control = "scope {\n  event: \"PreToolUse\"\n\n  control {\n    name: \"no-docker\"\n    cmd: \"docker\"\n    msg: \"No docker here.\"\n  }\n}\n"

// "so i can author them the same i would .feature"
// "i want to see those .pbt in addition to anything else that is for human authorship."
func TestAControlIsANoteInTheVault(t *testing.T) {
	s, vault := vaultBindingServer(t)
	mainNotes["docs/adr/no-docker.pbt"] = control
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "github.com/abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)

	assert.Equal(t, control, readNote(t, vault, "Course Material/no-docker.pbt.md"))
	assert.NoFileExists(t, filepath.Join(vault.Path, "Course Material", "no-docker.pbt"))
}

func TestAControlAuthoredInTheVaultReachesTheBranchAsTheControl(t *testing.T) {
	s, vault := vaultBindingServer(t)
	mainNotes["docs/adr/no-docker.pbt"] = control
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "github.com/abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)
	_, refused = askVault(t, s, "send", sigil.Sent{"name": "abcd", "place": "Course Material", "branch": "obsidian-abcd"})
	require.Nil(t, refused, "%v", refused)

	authored := control + "# authored in the vault\n"
	at := filepath.Join(vault.Path, "Course Material")
	require.NoError(t, os.WriteFile(filepath.Join(at, "no-docker.pbt.md"), []byte(authored), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(at, "no-podman.pbt.md"), []byte("scope {\n}\n"), 0o644))

	sent := s.sendVault(t.Context(), "abcd", everyPlace)
	require.Len(t, sent, 1)
	require.NoError(t, sent[0].Refused)
	assert.Equal(t, []string{"docs/adr/no-docker.pbt", "docs/adr/no-podman.pbt"}, sent[0].Committed)
	branch := stand.branches["obsidian-abcd"]
	assert.Equal(t, authored, branch["docs/adr/no-docker.pbt"])
	assert.Equal(t, "scope {\n}\n", branch["docs/adr/no-podman.pbt"])
	assert.NotContains(t, branch, "docs/adr/no-docker.pbt.md")
	assert.NotContains(t, branch, "docs/adr/no-podman.pbt.md")
}
