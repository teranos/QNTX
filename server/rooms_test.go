package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// A read past what a provider's int32 holds is refused, not sent on negative.
func TestARoomReadPastInt32IsRefused(t *testing.T) {
	s := servedForTest(t)
	_, refused := s.roomsRead(t.Context(), sigil.Sent{"room": "node", "limit": "2147483648"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Invalid, refused.GetWhy())
	assert.Equal(t, "limit", refused.GetParam())
}

// With no plugin holding rooms, every room sigil says so.
func TestNoPluginHoldingRoomsIsSaid(t *testing.T) {
	s := servedForTest(t)
	for name, answer := range s.roomsSignum().Answers {
		_, refused := answer(t.Context(), sigil.Sent{"room": "node", "body": "hi", "agents": "lamplighter"})
		require.NotNil(t, refused, name)
		assert.Contains(t, refused.GetSays(), "no plugin holds rooms on this node", name)
	}
}

// "i want all Node Agents to be in the Node Room"
func TestTheNodeRoomIsOfThisNode(t *testing.T) {
	s := &QNTXServer{nodeDID: &nodedid.Handler{DID: "did:key:z6Mknode"}}
	node, other := &protocol.Room{Name: nodeRoomName}, &protocol.Room{Name: "pond"}
	s.roomOfNode(node)
	s.roomOfNode(other)
	assert.Equal(t, "did:key:z6Mknode", node.GetOf().GetNode())
	assert.Nil(t, other.GetOf())
}

// A woken Node Agent's answer is what its turn said, whichever harness ran it.
func TestAWokenNodeAgentAnswersInItsHarnessWords(t *testing.T) {
	assert.Equal(t, "here", saidText(&protocol.NodeAgentSaid{Claude: &protocol.ClaudeSaid{Answer: "here"}}))
	assert.Equal(t, "too", saidText(&protocol.NodeAgentSaid{Pi: &protocol.PiSaid{Answer: "too"}}))
	assert.Equal(t, "", saidText(&protocol.NodeAgentSaid{}))
}

// A room's agent that is no Node Agent is left as the provider holds it.
func TestARoomAgentTheNodeDoesNotRunIsLeftAlone(t *testing.T) {
	s := &QNTXServer{}
	agent := &protocol.RoomAgent{Name: "claude", Harness: "switch"}
	s.roomAgentAs(agent)
	assert.Equal(t, "switch", agent.GetHarness())
	assert.Empty(t, agent.GetModel())
}

// A room's agent held under another DID is not the Node Agent, whatever its name.
func TestANamesakeUnderAnotherDIDIsNotTheNodeAgent(t *testing.T) {
	s := &QNTXServer{nodeAgent: &rootAgent{did: "did:key:z6Mkagent"}}
	agent := &protocol.RoomAgent{Name: "claude", Did: "did:key:z6Mkother", Harness: "switch"}
	s.roomAgentAs(agent)
	assert.Equal(t, "switch", agent.GetHarness())
	assert.Equal(t, "did:key:z6Mkother", agent.GetDid())
}

// A file a woken Node Agent writes for the room is sent with its answer.
func TestAWokenNodeAgentsFileGoesIntoTheRoom(t *testing.T) {
	s := servedForTest(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plot.png"), []byte("\x89PNG"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0o700))
	files := s.answerFiles(dir)
	require.Len(t, files, 1)
	assert.Equal(t, "plot.png", files[0].GetName())
	assert.Equal(t, "image/png", files[0].GetType())
	assert.Equal(t, []byte("\x89PNG"), files[0].GetData())
}
