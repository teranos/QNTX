package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/acp/acptest"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// "wiring it into node and namespace config as an actual harness"

var agyGarden = sigil.Sent{"namespace": "garden", "harness": "agy", "model": "gemini-3.8-flash"}

func agyNamespaceNode(t *testing.T) (*QNTXServer, gardens) {
	s, _, in := runningNamespaceAgents(t)
	home, err := s.agentHome("garden", "gemini-3.8-flash")
	require.NoError(t, err)
	underGround(t, home)
	s.underGround.add(home)
	return s, in
}

// agyStandIn holds a stand-in for `agy acp` as the node's agy, and answers
// where its runs are kept.
func agyStandIn(t *testing.T, s *QNTXServer, script acptest.Script) string {
	t.Helper()
	binary, ran := acptest.StandIn(t, script)
	arrived := make(chan struct{})
	close(arrived)
	s.holdHarness(agentLineAgy, &harnessHeld{name: "agy", path: binary, fetched: arrived})
	return ran
}

func TestAgyNamespaceConfiguration(t *testing.T) {
	s, in := agyNamespaceNode(t)
	am, refused := askingAm(s, gardener, auth.LevelToken, []string{"garden"}, "set", agyGarden)
	require.Nil(t, refused)
	require.Equal(t, "gemini-3.8-flash", am.Agy.Model)
	require.Contains(t, am.Agy.NotReady, "Google Cloud", "its owner has not connected it yet")
	require.Equal(t, "/auth/google/cloud?namespace=garden&thing=agy", am.Agy.GoogleConnect)
	require.Equal(t, []string{"agy"}, in.store["garden"].rows[0].Predicates)
	holds(t, s.agentsSignum(), "set", am)
	// An agy agent takes no Task over A2A, so it is not served there.
	opted, err := s.optedIntoAgent("garden")
	require.NoError(t, err)
	assert.False(t, opted)
	_, refused = askingAm(s, visitor, auth.LevelToken, []string{"orchard"}, "am", agyGarden)
	require.NotNil(t, refused)
	require.Equal(t, sigil.NotFound, refused.Why)
	_, refused = askingAm(s, visitor, auth.LevelToken, []string{"garden"}, "set", agyGarden)
	require.NotNil(t, refused)
	require.Equal(t, sigil.NotAllowed, refused.Why)
}

// The agent runs as its namespace at its owner's Google Cloud, and until its owner
// connected it nothing is said to it.
func TestAgyNamespaceRequiresItsOwnersGoogleBeforeTurn(t *testing.T) {
	s, in := agyNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", agyGarden)
	require.Nil(t, refused)
	agyStandIn(t, s, agyAnswered)
	_, refused = answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "agy", "says": "hello"})
	require.NotNil(t, refused)
	require.Contains(t, refused.Says, "/auth/google/cloud?namespace=garden&thing=agy")
	require.Len(t, in.store["garden"].rows, 1)
}

func TestAgyNamespaceRefusesWithoutGround(t *testing.T) {
	s, _ := agyNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", agyGarden)
	require.Nil(t, refused)
	gardenConnected(t, s)
	agyStandIn(t, s, agyAnswered)
	agent, _ := s.namespaceAgents.of(agentPurpose("garden", "gemini-3.8-flash"))
	require.NoError(t, os.Remove(filepath.Join(agent.home, "bin", "ground")))
	_, refused = answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "agy", "says": "hello"})
	require.NotNil(t, refused)
	require.Contains(t, refused.Says, "ground")
}
