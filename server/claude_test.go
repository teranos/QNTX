package server

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
	"go.uber.org/zap/zaptest"
)

// The stream a stand-in for Claude Code prints: a run that reached for one
// tool and answered.
const claudeAnswered = `{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5","claude_code_version":"2.1.289"}
{"type":"assistant","session_id":"s-1","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"uptime"}}]}}
{"type":"user","session_id":"s-1","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"up 3 days"}]}}
{"type":"result","subtype":"success","session_id":"s-1","is_error":false,"result":"Up 3 days.","total_cost_usd":0.05,"duration_ms":2592,"permission_denials":[{"tool_name":"Write"}]}
`

// claudeStandIn is a program standing in for Claude Code: each run writes down
// how it was run, under ran/<n>, and prints the stream.
func claudeStandIn(t *testing.T, stream string) (binary, ran string) {
	t.Helper()
	dir := t.TempDir()
	ran = filepath.Join(dir, "ran")
	require.NoError(t, os.MkdirAll(ran, 0o755))
	canned := filepath.Join(dir, "stream")
	require.NoError(t, os.WriteFile(canned, []byte(stream), 0o644))
	script := "#!/bin/sh\n" +
		"n=$(ls '" + ran + "' | wc -l | tr -d ' ')\n" +
		"mkdir '" + ran + "'/$n\n" +
		"for arg in \"$@\"; do printf '%s\\n' \"$arg\"; done > '" + ran + "'/$n/args\n" +
		"env > '" + ran + "'/$n/env\n" +
		"cat '" + canned + "'\n"
	binary = filepath.Join(dir, "claude")
	require.NoError(t, os.WriteFile(binary, []byte(script), 0o755))
	return binary, ran
}

func ranWith(t *testing.T, ran, run, what string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ran, run, what))
	require.NoError(t, err)
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

// after is the value an argument was given: what follows it.
func after(args []string, name string) string {
	at := slices.Index(args, name)
	if at < 0 || at+1 >= len(args) {
		return ""
	}
	return args[at+1]
}

// handed keeps each row a store was handed, as it was handed: the test store
// underneath keeps no signature (qntxtest.CreateTestStore).
type handed struct {
	ats.AttestationStore
	rows []*types.As
}

func (h *handed) CreateAttestation(as *types.As) error {
	h.rows = append(h.rows, as)
	return h.AttestationStore.CreateAttestation(as)
}

// runningTheRootAgent is a node whose am.toml names the ROOT agent, holding a
// stand-in for Claude Code.
func runningTheRootAgent(t *testing.T, named appcfg.RootAgentConfig) (s *QNTXServer, ran string) {
	t.Helper()
	under, db := createTestStore(t)
	store := &handed{AttestationStore: under}
	binary, ran := claudeStandIn(t, claudeAnswered)
	fetched := make(chan struct{})
	close(fetched)

	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 7
	agent, err := theRootAgent(ed25519.NewKeyFromSeed(seed), t.TempDir())
	require.NoError(t, err)

	t.Setenv("QNTX_TEST_PLAN_TOKEN", "the-plan-token")
	named.TokenRef = "env:QNTX_TEST_PLAN_TOKEN"
	s = &QNTXServer{
		held:       servingOne(db, store),
		logger:     zaptest.NewLogger(t).Sugar(),
		deps:       &serverDependencies{cfg: &appcfg.Config{Agent: appcfg.AgentConfig{Root: named}}},
		claudeCode: &claudeCodeHeld{fetched: fetched, path: binary},
		rootAgent:  agent,
		ctx:        context.Background(),
		ownURL:     "http://127.0.0.1:8770",
	}
	return s, ran
}

func saying(s *QNTXServer, sent sigil.Sent) (map[string]any, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/claude/say", nil)
	answer, refused := s.claudeSay(sigil.WithCaller(context.Background(), asked), sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(map[string]any), nil
}

var opusLow = appcfg.RootAgentConfig{
	Model: "claude-opus-5-5", Effort: "low", Mode: "dontAsk", Allow: []string{"Bash", "Read", "mcp__qntx"},
}

// What is said to the node is said to Claude Code, run as am.toml names it,
// and what it answered is given back (ADR-048).
func TestWhatIsSaidToTheRootAgentIsAnsweredByClaudeCode(t *testing.T) {
	s, ran := runningTheRootAgent(t, opusLow)

	answer, refused := saying(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer["answer"])
	assert.Equal(t, false, answer["is_error"])
	assert.Equal(t, "claude-opus-5-5", answer["model"])
	assert.Equal(t, "dontAsk", answer["permission_mode"])
	assert.Equal(t, []string{"Write"}, answer["denied"])
	holds(t, s.claudeSignum(), "say", answer)

	args := ranWith(t, ran, "0", "args")
	assert.Equal(t, "how long has the box been up?", after(args, "-p"))
	assert.Equal(t, "claude-opus-5-5", after(args, "--model"))
	assert.Equal(t, "low", after(args, "--effort"))
	assert.Equal(t, "dontAsk", after(args, "--permission-mode"))
	assert.Equal(t, "Bash,Read,mcp__qntx", after(args, "--allowedTools"))
	assert.Equal(t, answer["session"], after(args, "--session-id"))
	assert.Contains(t, after(args, "--append-system-prompt"), s.rootAgent.did)

	env := ranWith(t, ran, "0", "env")
	assert.Contains(t, env, "CLAUDE_CODE_OAUTH_TOKEN=the-plan-token")
	// It reaches the node's own MCP with its own token.
	assert.Contains(t, env, "QNTX_MCP_BEARER_QNTX="+s.rootAgent.token)
	written, err := os.ReadFile(after(args, "--mcp-config"))
	require.NoError(t, err)
	assert.Contains(t, string(written), "http://127.0.0.1:8770/mcp")
}

// It is one session: what is said next is said to the same one.
func TestTheRootAgentContinuesItsOneSession(t *testing.T) {
	s, ran := runningTheRootAgent(t, opusLow)

	first, refused := saying(s, sigil.Sent{"says": "remember 7"})
	require.Nil(t, refused)
	second, refused := saying(s, sigil.Sent{"says": "what did I ask you to remember?"})
	require.Nil(t, refused)

	assert.Equal(t, first["session"], second["session"])
	again := ranWith(t, ran, "1", "args")
	assert.Equal(t, first["session"], after(again, "--resume"))
	assert.NotContains(t, again, "--session-id")
}

// Its session is its own to write down: every row is signed by its DID, and
// reads back as a transcript where its caller stands.
func TestTheRootAgentsSessionReadsAsATranscriptItSigned(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	answer, refused := saying(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	session, _ := answer["session"].(string)

	wrote, kept := s.held.Served().(*handed)
	require.True(t, kept, "the store the node serves is not the one this test handed it")
	require.Len(t, wrote.rows, 4)
	for _, row := range wrote.rows {
		assert.Equal(t, s.rootAgent.did, row.SignerDID, "%s is not signed by the agent", row.Predicates[0])
		assert.NotEmpty(t, row.Signature)
		assert.Equal(t, []string{s.rootAgent.did}, row.Actors)
		require.NoError(t, signing.Verify(row))
	}

	rows, err := s.held.Served().GetAttestations(ats.AttestationFilter{Contexts: []string{"session:" + session}, Limit: 20})
	require.NoError(t, err)

	var said [][2]string
	for _, turn := range transcriptsOf(rows, 1)[0].Turns {
		said = append(said, [2]string{turn.Speaker, turn.Text})
	}
	assert.Equal(t, [][2]string{
		{"human", "how long has the box been up?"},
		{"session", "Start startup"},
		{"tool", "uptime"},
		{"assistant", "Up 3 days."},
	}, said)
}

// "make sure --permission-mode is configurable in the Claude Element"
func TestThePermissionModeIsNamedByWhoeverSpeaks(t *testing.T) {
	s, ran := runningTheRootAgent(t, opusLow)

	answer, refused := saying(s, sigil.Sent{"says": "plan it", "permission_mode": "plan"})
	require.Nil(t, refused)
	assert.Equal(t, "plan", answer["permission_mode"])
	assert.Equal(t, "plan", after(ranWith(t, ran, "0", "args"), "--permission-mode"))

	// am.toml that gives none leaves it to the speaker, and nothing is assumed.
	bare, _ := runningTheRootAgent(t, appcfg.RootAgentConfig{Model: "claude-opus-5-5", Effort: "low"})
	_, refused = saying(bare, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Missing, refused.GetWhy())
	assert.Equal(t, "permission_mode", refused.GetParam())
}

// Who said it is written down with what was said.
func TestWhoSpokeToTheRootAgentIsWrittenDown(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	root := auth.Admitted(auth.LevelRoot)
	root.Identity = "https://github.com/tim"
	asked := httptest.NewRequest(http.MethodPost, "/api/claude/say", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), root))

	answer, refused := s.claudeSay(sigil.WithCaller(context.Background(), asked), sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	session, _ := answer.(map[string]any)["session"].(string)

	told, err := s.held.Served().GetAttestations(ats.AttestationFilter{
		Contexts: []string{"session:" + session}, Predicates: []string{"UserPromptSubmit"}, Limit: 5})
	require.NoError(t, err)
	require.Len(t, told, 1)
	assert.Equal(t, "https://github.com/tim", told[0].Attributes["said_by"])
}

// Its token is ROOT's kind, so the gate would let it speak to itself, from
// inside the turn it would then wait on. It is told no.
func TestTheRootAgentDoesNotSpeakToItself(t *testing.T) {
	s, ran := runningTheRootAgent(t, opusLow)
	itself := auth.Admitted(auth.LevelRoot)
	itself.Grant = &auth.Grant{DID: s.rootAgent.did}
	asked := httptest.NewRequest(http.MethodPost, "/api/claude/say", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), itself))

	_, refused := s.claudeSay(sigil.WithCaller(context.Background(), asked), sigil.Sent{"says": "hello me"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotAllowed, refused.GetWhy())
	runs, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Empty(t, runs, "Claude Code was run for what the agent said to itself")
}

// A node whose am.toml names no ROOT agent has none to speak to.
func TestANodeThatNamesNoRootAgentHasNoneToSpeakTo(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	s.rootAgent = nil
	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
}

// One that am.toml names and that did not start says why, in the words the
// start failed in.
func TestARootAgentThatDidNotStartSaysWhy(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	s.rootAgent, s.noRootAgent = nil, errors.New("the ROOT agent's token tok-1 is switched off")

	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Failed, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "tok-1 is switched off")

	asked := httptest.NewRequest(http.MethodGet, "/api/claude", nil)
	_, refused = s.claudeAm(sigil.WithCaller(context.Background(), asked), sigil.Sent{})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "tok-1 is switched off")
}

// The agent says who it is and how it runs, which is what the Claude element
// draws before anything is said.
func TestTheRootAgentSaysWhoItIs(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	asked := httptest.NewRequest(http.MethodGet, "/api/claude", nil)

	before, refused := s.claudeAm(sigil.WithCaller(context.Background(), asked), sigil.Sent{})
	require.Nil(t, refused)
	is := before.(map[string]any)
	assert.Equal(t, s.rootAgent.did, is["did"])
	assert.Equal(t, "claude-opus-5-5", is["model"])
	assert.Equal(t, "low", is["effort"])
	assert.Equal(t, "dontAsk", is["permission_mode"])
	assert.Equal(t, appcfg.PermissionModes, is["permission_modes"])
	assert.Equal(t, "", is["session"])
	holds(t, s.claudeSignum(), "am", is)

	answer, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	after, refused := s.claudeAm(sigil.WithCaller(context.Background(), asked), sigil.Sent{})
	require.Nil(t, refused)
	assert.Equal(t, answer["session"], after.(map[string]any)["session"])
}

// While it is answering, it says so and in which session, so what it is doing
// can be read as it does it, from the first thing ever said to it.
func TestTheRootAgentSaysWhenItIsAnswering(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	asked := httptest.NewRequest(http.MethodGet, "/api/claude", nil)
	am := func() map[string]any {
		is, refused := s.claudeAm(sigil.WithCaller(context.Background(), asked), sigil.Sent{})
		require.Nil(t, refused)
		return is.(map[string]any)
	}
	assert.Equal(t, false, am()["answering"])

	s.rootAgent.answering.Store(&turnInSession{session: "s-first"})
	during := am()
	assert.Equal(t, true, during["answering"])
	assert.Equal(t, "s-first", during["session"])

	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	assert.Equal(t, false, am()["answering"], "a turn that ended is still said to be going")
}

// Its DID is its own: derived from the node's key, and not the node's.
func TestTheRootAgentIsItself(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 7
	node := ed25519.NewKeyFromSeed(seed)
	agent, err := theRootAgent(node, t.TempDir())
	require.NoError(t, err)

	nodePub, _ := node.Public().(ed25519.PublicKey)
	assert.NotEqual(t, access.EncodeDIDKey(nodePub), agent.did)
	_, did, err := access.DeriveToken(node, rootAgentPurpose)
	require.NoError(t, err)
	assert.Equal(t, did, agent.did)
}

func TestTheClaudeSignumSaysWhatItHolds(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	require.NoError(t, s.claudeSignum().Check())
}

// "The only guard is who may talk to it. That is ROOT, and SUPER during
// development, removed before the work merges."
func TestWhoMayTalkToTheRootAgent(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	s, _ := runningTheRootAgent(t, opusLow)
	for _, held := range s.claudeSignum().GetSigils() {
		assert.Equal(t, []string{"ROOT", "SUPER"}, compiled[held.GetHttp().GetPath()], held.GetHttp().GetPath())
	}
}
