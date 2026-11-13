package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// "I EDIT"
// "ITS IN MEMORY"

// memoryServer is a node keeping a vault that backs memory at Minds.
func memoryServer(t *testing.T) (*QNTXServer, Vault) {
	t.Helper()
	s, vault := vaultBindingServer(t)
	got, refused := askMemory(t, s, asRoot(), "keep", sigil.Sent{"name": vault.Name, "place": "Minds"})
	require.Nil(t, refused, refused.GetSays())
	assert.Equal(t, "Minds", got.(*protocol.MemoryStores).GetPlace())
	return s, vault
}

func askMemory(t *testing.T, s *QNTXServer, ctx context.Context, name string, sent sigil.Sent) (any, *protocol.Refusal) {
	t.Helper()
	got, refused := s.memorySignum().Answers[name](ctx, sent)
	if refused == nil {
		holds(t, s.memorySignum(), name, got)
	}
	return got, refused
}

// asBirch is a context the agent that named itself Birch asks in.
func asBirch(t *testing.T, s *QNTXServer) context.Context {
	t.Helper()
	birch := auth.Admitted(auth.LevelRoot, "garden")
	birch.Identity, birch.ClientDID, birch.TokenDID = rootAccount, "did:key:z6Mkbirch", "did:key:z6Mkbirch"
	ctx := auth.WithAdmission(context.Background(), birch)
	_, refusal := s.iName(ctx, sigil.Sent{"name": "Birch"})
	require.Nil(t, refusal, refusal.GetSays())
	return ctx
}

func TestTheMemorySignumChecks(t *testing.T) {
	s, _ := memoryServer(t)
	require.NoError(t, s.memorySignum().Check())
}

// A note a person writes in the store's folder is the agent's memory, and
// one they delete is a memory it holds no more.
func TestWhatAPersonWritesInTheVaultIsTheMemory(t *testing.T) {
	s, vault := memoryServer(t)
	folder := filepath.Join(vault.Path, "Minds", "Birch")
	require.NoError(t, os.MkdirAll(folder, 0o755))
	note := filepath.Join(folder, "the pond.md")
	require.NoError(t, os.WriteFile(note, []byte("The pond is drained in March.\n"), 0o644))

	got, refused := askMemory(t, s, asRoot(), "list", sigil.Sent{"store": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	listed := got.(*protocol.Memories).GetMemories()
	require.Len(t, listed, 1)
	assert.Equal(t, "the pond", listed[0].GetName())

	got, refused = askMemory(t, s, asRoot(), "read", sigil.Sent{"store": "Birch", "memory": "the pond"})
	require.Nil(t, refused, refused.GetSays())
	assert.Equal(t, "The pond is drained in March.\n", got.(*protocol.Memory).GetText())

	require.NoError(t, os.WriteFile(note, []byte("The pond is drained in April.\n"), 0o644))
	got, refused = askMemory(t, s, asRoot(), "read", sigil.Sent{"store": "Birch", "memory": "the pond"})
	require.Nil(t, refused, refused.GetSays())
	assert.Equal(t, "The pond is drained in April.\n", got.(*protocol.Memory).GetText(), "what was written last is not what is held")

	require.NoError(t, os.Remove(note))
	_, refused = askMemory(t, s, asRoot(), "read", sigil.Sent{"store": "Birch", "memory": "the pond"})
	require.NotNil(t, refused, "a note the person deleted is still held")
	assert.Equal(t, sigil.NotFound, refused.GetWhy())

	got, refused = askMemory(t, s, asRoot(), "stores", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	stores := got.(*protocol.MemoryStores).GetStores()
	require.Len(t, stores, 1)
	assert.Equal(t, "Birch", stores[0].GetName())
	assert.Equal(t, "Minds/Birch", stores[0].GetPlace())
}

// What an agent writes stands inside 「 」 with its name first, and the node
// sees to it; what a person writes is as they wrote it.
func TestAnAgentsMemoryStandsInItsOwnQuotation(t *testing.T) {
	s, vault := memoryServer(t)
	birch := asBirch(t, s)

	got, refused := askMemory(t, s, birch, "write", sigil.Sent{"store": "Birch", "memory": "frost", "text": "The owner said \"cover the beds\" before the first frost."})
	require.Nil(t, refused, refused.GetSays())
	assert.Equal(t, "「[Birch] The owner said \"cover the beds\" before the first frost.」\n", got.(*protocol.Memory).GetText())
	onDisk, err := os.ReadFile(filepath.Join(vault.Path, "Minds", "Birch", "frost.md"))
	require.NoError(t, err)
	assert.Equal(t, got.(*protocol.Memory).GetText(), string(onDisk), "the note in the vault is not the memory")

	got, refused = askMemory(t, s, birch, "write", sigil.Sent{"store": "Birch", "memory": "frost", "text": "「[Birch] Beds are covered.」"})
	require.Nil(t, refused, refused.GetSays())
	assert.Equal(t, "「[Birch] Beds are covered.」\n", got.(*protocol.Memory).GetText(), "what already stood in its quotation was wrapped again")

	got, refused = askMemory(t, s, asRoot(), "write", sigil.Sent{"store": "Birch", "memory": "rule", "text": "Never prune in autumn."})
	require.Nil(t, refused, refused.GetSays())
	assert.Equal(t, "Never prune in autumn.", got.(*protocol.Memory).GetText(), "a person's words were put in an agent's quotation")
}

// "I WANT TO BE ABLE TO GO FULL LACUNA INC ON YOU"
func TestAMemoryForgottenIsHeldNoMore(t *testing.T) {
	s, vault := memoryServer(t)
	_, refused := askMemory(t, s, asRoot(), "write", sigil.Sent{"store": "Birch", "memory": "rule", "text": "Never prune in autumn."})
	require.Nil(t, refused, refused.GetSays())

	got, refused := askMemory(t, s, asRoot(), "forget", sigil.Sent{"store": "Birch", "memory": "rule"})
	require.Nil(t, refused, refused.GetSays())
	assert.Empty(t, got.(*protocol.Memories).GetMemories())
	_, err := os.Stat(filepath.Join(vault.Path, "Minds", "Birch", "rule.md"))
	assert.True(t, os.IsNotExist(err), "the note is still in the vault")

	_, refused = askMemory(t, s, asRoot(), "forget", sigil.Sent{"store": "Birch", "memory": "rule"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
}

// A store merged into another gives it every memory and is gone; two that
// hold one name merge nothing.
func TestStoresMerge(t *testing.T) {
	s, vault := memoryServer(t)
	for store, memory := range map[string]string{"Birch": "frost", "Hazel": "pond"} {
		_, refused := askMemory(t, s, asRoot(), "write", sigil.Sent{"store": store, "memory": memory, "text": "kept"})
		require.Nil(t, refused, refused.GetSays())
	}
	_, refused := askMemory(t, s, asRoot(), "write", sigil.Sent{"store": "Rowan", "memory": "frost", "text": "another frost"})
	require.Nil(t, refused, refused.GetSays())

	_, refused = askMemory(t, s, asRoot(), "merge", sigil.Sent{"store": "Birch", "from": "Rowan"})
	require.NotNil(t, refused, "two memories by one name were merged")
	assert.Contains(t, refused.GetSays(), "both hold frost")

	got, refused := askMemory(t, s, asRoot(), "merge", sigil.Sent{"store": "Birch", "from": "Hazel"})
	require.Nil(t, refused, refused.GetSays())
	merged := got.(*protocol.Memories).GetMemories()
	require.Len(t, merged, 2)
	assert.Equal(t, "frost", merged[0].GetName())
	assert.Equal(t, "pond", merged[1].GetName())
	_, err := os.Stat(filepath.Join(vault.Path, "Minds", "Hazel"))
	assert.True(t, os.IsNotExist(err), "the store merged in is still there")
}

// A name that leaves the store, or the place memory is kept at, is refused.
func TestAMemoryStaysInItsStore(t *testing.T) {
	s, vault := memoryServer(t)
	for _, sent := range []sigil.Sent{
		{"store": "../abcd", "memory": "x", "text": "y"},
		{"store": "Birch", "memory": "../../x", "text": "y"},
		{"store": ".obsidian", "memory": "x", "text": "y"},
		{"store": "Birch/Deeper", "memory": "x", "text": "y"},
	} {
		_, refused := askMemory(t, s, asRoot(), "write", sent)
		require.NotNil(t, refused, "%v was written", sent)
		assert.Equal(t, sigil.Invalid, refused.GetWhy())
	}
	_, refused := askMemory(t, s, asRoot(), "keep", sigil.Sent{"name": vault.Name, "place": "../outside"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Invalid, refused.GetWhy())
}

// A node whose vault backs no memory says so, and says what does.
func TestNoVaultBacksMemoryUntilOneIsSaid(t *testing.T) {
	s, _ := vaultBindingServer(t)
	_, refused := askMemory(t, s, asRoot(), "stores", sigil.Sent{})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "memory keep")
}
