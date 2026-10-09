package server

import (
	"crypto/ed25519"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// gardens is the node's namespaces as a test keeps them: each with its own
// store and the owner its definition names.
type gardens struct {
	store map[string]*handed
	owner map[string]string
}

func (g gardens) List() ([]storage.Namespace, error) {
	out := make([]storage.Namespace, 0, len(g.store))
	for name := range g.store {
		out = append(out, storage.Namespace{Name: name, Definition: &storage.NamespaceDefinition{Owner: g.owner[name], Enabled: true}})
	}
	return out, nil
}
func (gardens) Create(string, storage.NamespaceDefinition) error { return nil }
func (gardens) SetEnabled(string, bool) error                    { return nil }
func (gardens) Delete(string) error                              { return nil }
func (gardens) Nuke() error                                      { return nil }
func (g gardens) OpenNamespace(name string) (*namespaces.Universe, error) {
	if s, ok := g.store[name]; ok {
		return oneNamespace(name, s), nil
	}
	return nil, fmt.Errorf("no namespace %q served in test", name)
}

const (
	gardener = "https://example.org/gardener"
	visitor  = "https://example.org/visitor"
)

// runningNamespaceAgents is a node running the ROOT agent that also holds the
// namespaces garden and orchard, garden owned by the gardener.
func runningNamespaceAgents(t *testing.T) (s *QNTXServer, ran string, in gardens) {
	t.Helper()
	s, ran = runningTheRootAgent(t, opusLow)
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 9
	s.nodeDID = &nodedid.Handler{PrivateKey: ed25519.NewKeyFromSeed(seed)}
	s.agentsDir = t.TempDir()
	in = gardens{store: map[string]*handed{}, owner: map[string]string{"garden": gardener}}
	for _, name := range []string{"garden", "orchard"} {
		store, _ := createTestStore(t)
		in.store[name] = &handed{AttestationStore: store}
	}
	s.held.SetKnown(in)
	s.held.SetOpener(in)
	return s, ran, in
}

// asking is a call on the agents signum by somebody admitted at level, acting
// in the namespaces named.
func asking(s *QNTXServer, who string, level auth.Level, acting []string, name string, sent sigil.Sent) (map[string]any, *protocol.Refusal) {
	admitted := auth.Admitted(level, acting...)
	admitted.Identity = who
	asked := httptest.NewRequest(http.MethodPost, "/api/agents/"+sent["namespace"], nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	answer, refused := s.agentsSignum().Answers[name](sigil.WithCaller(asked.Context(), asked), sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(map[string]any), nil
}

func rootAsks(s *QNTXServer, name string, sent sigil.Sent) (map[string]any, *protocol.Refusal) {
	return asking(s, "https://example.org/root", auth.LevelRoot, nil, name, sent)
}

var sonnetGarden = sigil.Sent{"namespace": "garden", "model": "claude-sonnet-5-5", "effort": "medium", "permission_mode": "dontAsk", "allow": "Read, mcp__qntx"}

// "The Namespace agent is a common Agent of the Namespace it's opted into."
// Opting in is a line in the namespace's own store, and the agent it names is
// the model named: its own DID, which is not ROOT's.
func TestANamespaceOptsIntoAnAgentOfItsOwn(t *testing.T) {
	s, _, in := runningNamespaceAgents(t)

	am, refused := rootAsks(s, "set", sonnetGarden)
	require.Nil(t, refused)
	assert.Equal(t, "garden", am["namespace"])
	assert.Equal(t, "https://example.org/root", am["set_by"])
	assert.Equal(t, "claude-sonnet-5-5", am["model"])
	assert.Equal(t, "medium", am["effort"])
	assert.Equal(t, "dontAsk", am["permission_mode"])
	assert.Equal(t, []string{"Read", "mcp__qntx"}, am["allow"])
	assert.Equal(t, false, am["signed_in"])
	assert.NotEqual(t, s.rootAgent.did, am["did"], "the namespace agent is not the ROOT agent")
	holds(t, s.agentsSignum(), "set", am)

	require.Len(t, in.store["garden"].rows, 1, "the AGENT line is in the namespace's own store")
	line := in.store["garden"].rows[0]
	assert.Equal(t, []string{"AGENT"}, line.Subjects)
	assert.Equal(t, []string{"claude"}, line.Predicates)
	assert.Equal(t, []string{"https://example.org/root"}, line.Actors)
	system, _ := s.held.WriteWhatTheNodeKnowsOfItself()
	assert.Empty(t, system.(*handed).rows, "nothing of the namespace's agent is written in system")

	again, refused := rootAsks(s, "am", sigil.Sent{"namespace": "garden"})
	require.Nil(t, refused)
	assert.Equal(t, am["did"], again["did"], "one agent per namespace and model, wherever it is asked for")
	holds(t, s.agentsSignum(), "am", again)
}

// Two namespaces are two agents, and so is one namespace naming a new model.
func TestEachNamespaceAndModelIsItsOwnAgent(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	garden, refused := rootAsks(s, "set", sonnetGarden)
	require.Nil(t, refused)
	orchard, refused := rootAsks(s, "set", sigil.Sent{"namespace": "orchard", "model": "claude-sonnet-5-5", "effort": "medium", "permission_mode": "dontAsk"})
	require.Nil(t, refused)
	assert.NotEqual(t, garden["did"], orchard["did"])

	opus, refused := rootAsks(s, "set", sigil.Sent{"namespace": "garden", "model": "claude-opus-5-5", "effort": "high", "permission_mode": "dontAsk"})
	require.Nil(t, refused)
	assert.NotEqual(t, garden["did"], opus["did"], "a different model is a different agent")
	assert.Equal(t, "claude-opus-5-5", opus["model"])
	am, refused := rootAsks(s, "am", sigil.Sent{"namespace": "garden"})
	require.Nil(t, refused)
	assert.Equal(t, opus["did"], am["did"], "the newest line holds")
}

// It is signed in through claude login, in its own home, and what is said to
// it is run as the line says and written in the namespace, signed by its own
// DID: nothing of it lands in system (ADR-026, nothing crosses).
func TestWhatIsSaidToANamespaceAgentIsWrittenInTheNamespace(t *testing.T) {
	s, ran, in := runningNamespaceAgents(t)
	am, refused := rootAsks(s, "set", sonnetGarden)
	require.Nil(t, refused)

	begun, refused := rootAsks(s, "login", sigil.Sent{"namespace": "garden"})
	require.Nil(t, refused)
	assert.Equal(t, standInSignInURL, begun["url"])
	holds(t, s.agentsSignum(), "login", begun)
	home := filepath.Join(s.agentsDir, "garden", "claude-sonnet-5-5")
	assert.Contains(t, ranWith(t, ran, "0", "env"), "CLAUDE_CONFIG_DIR="+filepath.Join(home, "claude"))
	finished, refused := rootAsks(s, "login", sigil.Sent{"namespace": "garden", "code": "the-code-shown"})
	require.Nil(t, refused)
	assert.Equal(t, true, finished["signed_in"])

	answer, refused := asking(s, visitor, auth.LevelUser, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer["answer"])
	holds(t, s.agentsSignum(), "say", answer)
	// Run as the AGENT line says, on its own sign-in, with its own token.
	run := theRunSaying(t, ran)
	args := ranWith(t, ran, run, "args")
	assert.Equal(t, "claude-sonnet-5-5", after(args, "--model"))
	assert.Equal(t, "medium", after(args, "--effort"))
	assert.Equal(t, "Read,mcp__qntx", after(args, "--allowedTools"))
	assert.Contains(t, after(args, "--append-system-prompt"), "garden")
	assert.Contains(t, after(args, "--append-system-prompt"), am["did"])
	env := ranWith(t, ran, run, "env")
	assert.NotContains(t, env, "CLAUDE_CODE_OAUTH_TOKEN=the-plan-token", "ROOT's plan token is not the namespace agent's")
	assert.NotContains(t, env, "QNTX_MCP_BEARER_QNTX="+s.rootAgent.token, "ROOT's token is not the namespace agent's")
	for _, line := range env {
		assert.False(t, len(line) > 13 && line[:13] == "GIT_CONFIG_GL", "the namespace agent holds no git: %s", line)
	}

	// Written in garden by the agent, signed as itself; not in system, not in orchard.
	var session []string
	for _, row := range in.store["garden"].rows[1:] {
		assert.Equal(t, am["did"], row.SignerDID)
		require.NoError(t, signing.Verify(row))
		session = append(session, row.Predicates[0])
	}
	assert.Len(t, session, 4, "told, session start, tool, answer")
	system, _ := s.held.WriteWhatTheNodeKnowsOfItself()
	assert.Empty(t, system.(*handed).rows)
	assert.Empty(t, in.store["orchard"].rows)

	read, refused := asking(s, visitor, auth.LevelUser, []string{"garden"}, "session", sigil.Sent{"namespace": "garden"})
	require.Nil(t, refused)
	holds(t, s.agentsSignum(), "session", read)
	transcript := read["transcript"].(transcript)
	require.NotEmpty(t, transcript.Turns)
	assert.Equal(t, "how long has the box been up?", transcript.Turns[0].Text)
}

// "shared amongst anyone who has REACH on it": who acts in the namespace
// speaks to it; who does not finds no such namespace.
func TestANamespaceAgentIsReachedByWhoActsInTheNamespace(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	_, refused := rootAsks(s, "set", sonnetGarden)
	require.Nil(t, refused)

	_, refused = asking(s, visitor, auth.LevelUser, []string{"orchard"}, "am", sigil.Sent{"namespace": "garden"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.Why)

	_, refused = asking(s, visitor, auth.LevelUser, []string{"orchard"}, "say", sigil.Sent{"namespace": "garden", "says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.Why)

	_, refused = asking(s, visitor, auth.LevelUser, []string{"orchard"}, "am", sigil.Sent{"namespace": "orchard"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.Why, "orchard opted into no agent")
	assert.Contains(t, refused.Says, "opted into no agent")

	_, refused = rootAsks(s, "am", sigil.Sent{"namespace": "system"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotAllowed, refused.Why, "system has no agent of its own")
}

// "The one who set's it up": the namespace's owner sets its agent, as do ROOT
// and SUPER; somebody who merely acts there does not.
func TestWhoSetsANamespacesAgent(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)

	_, refused := asking(s, visitor, auth.LevelUser, []string{"garden"}, "set", sonnetGarden)
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotAllowed, refused.Why)

	am, refused := asking(s, gardener, auth.LevelUser, []string{"garden"}, "set", sonnetGarden)
	require.Nil(t, refused)
	assert.Equal(t, gardener, am["set_by"])

	super, refused := asking(s, "https://example.org/super", auth.LevelSuper, nil, "set", sigil.Sent{"namespace": "orchard", "model": "claude-sonnet-5-5", "effort": "low", "permission_mode": "dontAsk"})
	require.Nil(t, refused)
	assert.Equal(t, "https://example.org/super", super["set_by"])

	_, refused = asking(s, gardener, auth.LevelUser, []string{"garden"}, "set", sigil.Sent{"namespace": "garden", "effort": "low", "permission_mode": "dontAsk"})
	require.NotNil(t, refused)
	assert.Equal(t, "model", refused.Param, "the agent is the model named, and nothing stands in for it")
}

// The agents paths let in whoever acts in a namespace: ROOT, SUPER and USER.
// Which namespace is the handler's to ask. A token reaches /mcp: the namespace
// agent's own way to the node's sigils.
func TestTheTableLetsTheNamespacesPeopleReachItsAgent(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	s, _, _ := runningNamespaceAgents(t)
	for _, held := range s.agentsSignum().GetSigils() {
		assert.Equal(t, []string{"ROOT", "SUPER", "USER"}, compiled[held.GetHttp().GetPath()], held.GetHttp().GetPath())
	}
	assert.Equal(t, []string{"ROOT", "SUPER", "TOKEN"}, compiled["/mcp"])
	require.NoError(t, s.agentsSignum().Check())
}

// theRunSaying is the one run of the stand-in that was a turn: the sign-in and
// status runs before it are Claude Code's own, and how many there were is not
// the test's to count.
func theRunSaying(t *testing.T, ran string) string {
	t.Helper()
	runs, err := os.ReadDir(ran)
	require.NoError(t, err)
	for _, run := range runs {
		if slices.Contains(ranWith(t, ran, run.Name(), "args"), "-p") {
			return run.Name()
		}
	}
	require.FailNow(t, "no run of the stand-in was a turn")
	return ""
}

var _ ats.AttestationStore = (*handed)(nil)
