package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "「[Opus 5.5] I OPEN MY OBSIDIAN, I SEE LIVE STATE OF AGENT MEMORY」"
// "「[Opus 5.5] I CAN EDIT IT, IT GOES TO QNTX, IT GOES TO AGENT MEMORY」"
// "「[Opus 5.5] AND AGENT MEMORIES COME BACK VIA QNTX TO THE VAULT AS WELL」"

// 「[Opus 5.5] memoryOfAnAgent is a node whose vault backs memory at Minds, and an agent it」
// 「[Opus 5.5] runs in home, whose Claude Code keeps memory for its work directory.」
func memoryOfAnAgent(t *testing.T) (s *QNTXServer, harness, vault, held string) {
	t.Helper()
	s, kept := memoryServer(t)
	home := t.TempDir()
	s.underGround.add(home)
	project := claudeProjectOf(filepath.Join(home, "work"))
	harness = filepath.Join(home, "claude", "projects", project, "memory")
	require.NoError(t, os.MkdirAll(harness, 0o755))
	store := s.calledInMemory(home)
	return s, harness, filepath.Join(kept.Path, "Minds", store), filepath.Join(home, heldMemoryDir, project)
}

func noteSays(t *testing.T, dir, note string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(dir, note))
	require.NoError(t, err)
	return string(text)
}

func gone(t *testing.T, dir, note string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, note))
	assert.True(t, os.IsNotExist(err), "%s is still in %s", note, dir)
}

// 「[Opus 5.5] What the agent's harness holds is in the vault: its live state.」
func TestTheLiveStateOfAgentMemoryIsInTheVault(t *testing.T) {
	s, harness, vault, held := memoryOfAnAgent(t)
	require.NoError(t, os.WriteFile(filepath.Join(harness, "frost.md"), []byte("Cover the beds.\n"), 0o644))

	require.NoError(t, s.memoriesInStep())
	assert.Equal(t, "Cover the beds.\n", noteSays(t, vault, "frost.md"))
	assert.Equal(t, "Cover the beds.\n", noteSays(t, held, "frost.md"), "the node does not hold what it carried")

	// 「[Opus 5.5] The agent writes on: the vault follows.」
	require.NoError(t, os.WriteFile(filepath.Join(harness, "frost.md"), []byte("Cover the beds before November.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(harness, "pond.md"), []byte("Drained in March.\n"), 0o644))
	require.NoError(t, s.memoriesInStep())
	assert.Equal(t, "Cover the beds before November.\n", noteSays(t, vault, "frost.md"))
	assert.Equal(t, "Drained in March.\n", noteSays(t, vault, "pond.md"))

	// 「[Opus 5.5] The agent lets one go: it is gone from the vault.」
	require.NoError(t, os.Remove(filepath.Join(harness, "pond.md")))
	require.NoError(t, s.memoriesInStep())
	gone(t, vault, "pond.md")
	gone(t, held, "pond.md")
}

// 「[Opus 5.5] What the person edits in the vault goes to the node, and to the agent's」
// 「[Opus 5.5] memory in its harness.」
func TestAnEditInTheVaultIsTheAgentsMemory(t *testing.T) {
	s, harness, vault, held := memoryOfAnAgent(t)
	require.NoError(t, os.WriteFile(filepath.Join(harness, "frost.md"), []byte("Cover the beds.\n"), 0o644))
	require.NoError(t, s.memoriesInStep())

	// 「[Opus 5.5] Edited.」
	require.NoError(t, os.WriteFile(filepath.Join(vault, "frost.md"), []byte("Never cover the beds.\n"), 0o644))
	require.NoError(t, s.memoriesInStep())
	assert.Equal(t, "Never cover the beds.\n", noteSays(t, held, "frost.md"), "the edit did not go to the node")
	assert.Equal(t, "Never cover the beds.\n", noteSays(t, harness, "frost.md"), "the edit did not go to the agent's memory")

	// 「[Opus 5.5] Authored: a memory the agent never wrote.」
	require.NoError(t, os.WriteFile(filepath.Join(vault, "rule.md"), []byte("Ask nobody for a key.\n"), 0o644))
	require.NoError(t, s.memoriesInStep())
	assert.Equal(t, "Ask nobody for a key.\n", noteSays(t, harness, "rule.md"))

	// 「[Opus 5.5] Erased: a memory the person deletes is held no more, anywhere.」
	require.NoError(t, os.Remove(filepath.Join(vault, "frost.md")))
	require.NoError(t, s.memoriesInStep())
	gone(t, harness, "frost.md")
	gone(t, held, "frost.md")
}

// 「[Opus 5.5] Written on both sides since the node last held it, the person's stands.」
func TestThePersonsEditStandsOverTheAgents(t *testing.T) {
	s, harness, vault, _ := memoryOfAnAgent(t)
	require.NoError(t, os.WriteFile(filepath.Join(harness, "frost.md"), []byte("Cover the beds.\n"), 0o644))
	require.NoError(t, s.memoriesInStep())

	require.NoError(t, os.WriteFile(filepath.Join(harness, "frost.md"), []byte("The agent's.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vault, "frost.md"), []byte("The person's.\n"), 0o644))
	require.NoError(t, s.memoriesInStep())
	assert.Equal(t, "The person's.\n", noteSays(t, harness, "frost.md"))
	assert.Equal(t, "The person's.\n", noteSays(t, vault, "frost.md"))
}

// 「[Opus 5.5] In step, a second pass moves nothing.」
func TestInStepNothingMoves(t *testing.T) {
	s, harness, vault, _ := memoryOfAnAgent(t)
	require.NoError(t, os.WriteFile(filepath.Join(harness, "frost.md"), []byte("Cover the beds.\n"), 0o644))
	require.NoError(t, s.memoriesInStep())

	stores, err := claudeStoresOf(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(harness)))), "x")
	require.NoError(t, err)
	require.Len(t, stores, 1)
	toHarness, toVault, err := stores[0].inStep(vault)
	require.NoError(t, err)
	assert.Zero(t, toHarness+toVault, "memories moved between two sides that were in step")
}

// 「[Opus 5.5] A vault whose copy is not on the box is no vault with every memory deleted:」
// 「[Opus 5.5] the agent keeps what it holds, and the node says why nothing was done.」
func TestAVaultThatIsNotThereForgetsNothing(t *testing.T) {
	s, harness, vault, _ := memoryOfAnAgent(t)
	require.NoError(t, os.WriteFile(filepath.Join(harness, "frost.md"), []byte("Cover the beds.\n"), 0o644))
	require.NoError(t, s.memoriesInStep())

	// 「[Opus 5.5] The store's folder alone goes: it is written again from what the node holds.」
	require.NoError(t, os.RemoveAll(vault))
	require.NoError(t, s.memoriesInStep())
	assert.Equal(t, "Cover the beds.\n", noteSays(t, harness, "frost.md"), "the agent forgot what a missing folder never asked it to")
	assert.Equal(t, "Cover the beds.\n", noteSays(t, vault, "frost.md"))

	// 「[Opus 5.5] The whole copy goes.」
	kept, _, refused := s.memoryKept()
	require.Nil(t, refused)
	require.NoError(t, os.RemoveAll(kept.Path))
	err := s.memoriesInStep()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing is brought in step")
	assert.Equal(t, "Cover the beds.\n", noteSays(t, harness, "frost.md"))
}

// 「[Opus 5.5] A node whose vault backs no memory keeps nothing in step and writes nowhere.」
func TestNoVaultBackingMemoryMovesNothing(t *testing.T) {
	s, _ := vaultBindingServer(t)
	home := t.TempDir()
	s.underGround.add(home)
	harness := filepath.Join(home, "claude", "projects", claudeProjectOf(filepath.Join(home, "work")), "memory")
	require.NoError(t, os.MkdirAll(harness, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(harness, "frost.md"), []byte("Cover the beds.\n"), 0o644))

	require.NoError(t, s.memoriesInStep())
	_, err := os.Stat(filepath.Join(home, heldMemoryDir))
	assert.True(t, os.IsNotExist(err), "the node held memories with no vault to keep them in step with")
}
