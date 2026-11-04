//go:build libacp

package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// "wiring it into node and namespace config as an actual harness"
// A namespace's agent in agy runs over ACP as its namespace at its owner's Google
// Cloud, and resumes its own session.
func TestAgyNamespaceTurnResumesAndSigns(t *testing.T) {
	s, in := agyNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", agyGarden)
	require.Nil(t, refused)
	gardenConnected(t, s)
	agent, ok := s.namespaceAgents.of(agentPurpose("garden", "gemini-3.8-flash"))
	require.True(t, ok)
	ran := agyStandIn(t, s, agyAnswered)
	// agy needs neither the Claude executable nor the Node Agent.
	s.harnessBinaries.mu.Lock()
	delete(s.harnessBinaries.held, "claude")
	s.harnessBinaries.mu.Unlock()
	s.nodeAgent = nil
	before := len(in.store["garden"].rows)
	var session string
	for range 2 {
		answer, refused := answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "agy", "says": "hello"})
		require.Nil(t, refused)
		said := answer.(*protocol.NamespaceAgentSaid).Agy
		require.Equal(t, "Up 3 days.", said.Answer)
		require.Equal(t, "conv-7", said.Conversation)
		if session != "" {
			require.Equal(t, session, said.Session)
		}
		session = said.Session
		holds(t, s.agentsSignum(), "say", answer)
	}
	require.NotEqual(t, "conv-7", session)
	require.Contains(t, strings.Join(ranWith(t, ran, "1", "input"), "\n"), `"method":"session/resume","params":{"sessionId":"conv-7"`,
		"the second turn resumes agy's own session")
	require.Contains(t, strings.Join(ranWith(t, ran, "1", "input"), "\n"), `"methodId":"agent-platform"`, "it signs in as its namespace at Google")
	for _, set := range ranWith(t, ran, "1", "env") {
		require.False(t, strings.HasPrefix(set, "GEMINI_API_KEY="), "no key: %s", set)
	}
	for _, row := range in.store["garden"].rows[before:] {
		require.Equal(t, []string{agent.did}, row.Actors)
		require.Equal(t, []string{"session:" + session}, row.Contexts)
		require.NoError(t, signing.Verify(row))
	}
	transcript, refused := answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "session", agyGarden)
	require.Nil(t, refused)
	require.NotNil(t, transcript.(*protocol.SessionTranscript).Transcript)
	am, refused := askingAm(s, gardener, auth.LevelToken, []string{"garden"}, "am", agyGarden)
	require.Nil(t, refused)
	require.Equal(t, session, am.Agy.Session)
	require.Empty(t, am.Agy.NotReady)
	holds(t, s.agentsSignum(), "am", am)
}
