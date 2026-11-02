package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// "wiring it into node and namespace config as an actual harness"

var agyGarden = sigil.Sent{"namespace": "garden", "harness": "agy", "model": "gemini-3.8-flash-low"}

func agyNamespaceNode(t *testing.T) (*QNTXServer, gardens) {
	s, _, in := runningNamespaceAgents(t)
	home, err := s.agentHome("garden", "gemini-3.8-flash-low")
	require.NoError(t, err)
	underGround(t, home)
	s.underGround.add(home)
	return s, in
}

// agyStandIn prints stream as agy would, after keeping its arguments and the
// key it was handed in its working directory.
func agyStandIn(t *testing.T, s *QNTXServer, stream string) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "agy")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' \"$@\" >> calls\nprintf '%s\\n' \"$GEMINI_API_KEY\" > key\ncat <<'JSON'\n"+stream+"JSON\n"), 0o700))
	arrived := make(chan struct{})
	close(arrived)
	s.holdHarness(agentLineAgy, &harnessHeld{name: "agy", path: binary, fetched: arrived})
}

func gardenGeminiKey(t *testing.T, s *QNTXServer) {
	t.Helper()
	_, refused := keysAs(s, person(gardener, auth.LevelUser, "garden"), "set", sigil.Sent{"namespace": "garden", "name": "gemini", "value": "the-gardens-gemini-key"})
	require.Nil(t, refused)
}

func TestAgyNamespaceConfiguration(t *testing.T) {
	s, in := agyNamespaceNode(t)
	am, refused := askingAm(s, gardener, auth.LevelToken, []string{"garden"}, "set", agyGarden)
	require.Nil(t, refused)
	require.Equal(t, "gemini-3.8-flash-low", am.Agy.Model)
	require.Contains(t, am.Agy.NotReady, "gemini", "it has no key to run on yet")
	require.Equal(t, []string{"agy"}, in.store["garden"].rows[0].Predicates)
	holds(t, s.agentsSignum(), "set", am)
	opted, err := s.optedIntoAgent("garden")
	require.NoError(t, err)
	assert.True(t, opted)
	_, refused = askingAm(s, visitor, auth.LevelToken, []string{"orchard"}, "am", agyGarden)
	require.NotNil(t, refused)
	require.Equal(t, sigil.NotFound, refused.Why)
	_, refused = askingAm(s, visitor, auth.LevelToken, []string{"garden"}, "set", agyGarden)
	require.NotNil(t, refused)
	require.Equal(t, sigil.NotAllowed, refused.Why)
}

// "Handing the node their key": the agent makes its model calls on the
// namespace's gemini key, and without one nothing is said to it.
func TestAgyNamespaceRequiresItsGeminiKeyBeforeTurn(t *testing.T) {
	s, in := agyNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", agyGarden)
	require.Nil(t, refused)
	agyStandIn(t, s, agyAnswered)
	_, refused = answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "agy", "says": "hello"})
	require.NotNil(t, refused)
	require.Contains(t, refused.Says, "gemini")
	require.Len(t, in.store["garden"].rows, 1)
}

func TestAgyNamespaceTurnResumesAndSigns(t *testing.T) {
	s, in := agyNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", agyGarden)
	require.Nil(t, refused)
	gardenGeminiKey(t, s)
	agent, ok := s.namespaceAgents.of(agentPurpose("garden", "gemini-3.8-flash-low"))
	require.True(t, ok)
	agyStandIn(t, s, agyAnswered)
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
	calls, err := os.ReadFile(filepath.Join(agent.home, "work", "calls"))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(calls), "--conversation\nconv-7\n"), "the second turn resumes agy's own conversation")
	key, err := os.ReadFile(filepath.Join(agent.home, "work", "key"))
	require.NoError(t, err)
	require.Equal(t, "the-gardens-gemini-key\n", string(key))
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

func TestAgyNamespaceRefusesWithoutGround(t *testing.T) {
	s, _ := agyNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", agyGarden)
	require.Nil(t, refused)
	gardenGeminiKey(t, s)
	agyStandIn(t, s, agyAnswered)
	agent, _ := s.namespaceAgents.of(agentPurpose("garden", "gemini-3.8-flash-low"))
	require.NoError(t, os.Remove(filepath.Join(agent.home, "bin", "ground")))
	_, refused = answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "agy", "says": "hello"})
	require.NotNil(t, refused)
	require.Contains(t, refused.Says, "ground")
}

// A gemini key is asked of Gemini, as the other providers' are.
func TestAGeminiKeyIsAskedOfGemini(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	s.providersAt = map[string]string{"gemini": providerIssuing(t, "AIza-issued")}
	owner := person(gardener, auth.LevelUser, "garden")
	set, refused := keysAs(s, owner, "set", sigil.Sent{"namespace": "garden", "name": "gemini", "value": "AIza-issued"})
	require.Nil(t, refused)
	key := set.(*protocol.NamespaceKeys).GetKeys()[0]
	assert.True(t, key.GetAsked())
	assert.True(t, key.GetAccepted(), key.GetWhy())
}
