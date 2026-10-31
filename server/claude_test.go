package server

import (
	"context"
	"crypto/ed25519"
	"github.com/teranos/QNTX/plugin"
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
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
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
// how it was run, under ran/<n>, and prints the stream. A session it starts it
// keeps where Claude Code keeps one.
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
		// Signing in, as Claude Code does it headless: the URL, then the code
		// read on stdin and kept in the config dir. Status reads what is kept.
		"if [ \"$1\" = auth ]; then\n" +
		"  if [ \"$2\" = login ]; then\n" +
		"    echo 'Opening browser to sign in…'\n" +
		"    echo 'If the browser didn'\"'\"'t open, visit: " + standInSignInURL + "'\n" +
		"    printf 'Paste code here if prompted > '\n" +
		"    read code\n" +
		"    if [ \"$code\" = bad-code ]; then echo 'Invalid code' >&2; exit 1; fi\n" +
		"    printf '%s\\n' \"$code\" > \"$CLAUDE_CONFIG_DIR/.credentials.json\"\n" +
		"    exit 0\n" +
		"  fi\n" +
		// Signed out, Claude Code 2.1.289 says so and exits 1.
		"  if [ -f \"$CLAUDE_CONFIG_DIR/.credentials.json\" ]; then\n" +
		"    echo '{\"loggedIn\":true,\"authMethod\":\"claude.ai\",\"apiProvider\":\"firstParty\"}'\n" +
		"    exit 0\n" +
		"  fi\n" +
		"  echo '{\"loggedIn\":false,\"authMethod\":\"none\",\"apiProvider\":\"firstParty\"}'\n" +
		"  exit 1\n" +
		"fi\n" +
		"prev=''\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$prev\" = '--session-id' ] && [ -n \"$CLAUDE_CONFIG_DIR\" ]; then\n" +
		"    mkdir -p \"$CLAUDE_CONFIG_DIR/projects/work\" && : > \"$CLAUDE_CONFIG_DIR/projects/work/$arg.jsonl\"\n" +
		"  fi\n" +
		"  prev=\"$arg\"\n" +
		"done\n" +
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

// underGround stands the agent in home under ground: stand-ins for its ground binaries, and the settings that run them.
func underGround(t *testing.T, home string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(comet.AgentGround(home)), 0o755))
	for _, built := range []string{comet.AgentGround(home), comet.AgentHook(home), comet.AgentSky(home)} {
		require.NoError(t, os.WriteFile(built, []byte("#!/bin/sh\n"), 0o755))
	}
	require.NoError(t, comet.AgentSettings(home))
}

// "AS AN A2A AGENT, IT ALSO NEEDS TO SHACKLE ITSELF UNDER GROUND"
func TestTheNodeAgentIsSaidNothingUntilItRunsUnderGround(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	require.NoError(t, os.Remove(comet.AgentGround(s.nodeAgent.home)))
	_, refused := saying(s, sigil.Sent{"says": "how long has the box been up?"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.Says, "is said nothing until it runs under ground")
	assert.Contains(t, refused.Says, "its ground is not built")
}

// runningTheNodeAgent is a node whose am.toml names the Node Agent, holding a
// stand-in for Claude Code.
func runningTheNodeAgent(t *testing.T, named appcfg.NodeAgentConfig) (s *QNTXServer, ran string) {
	t.Helper()
	under, db := createTestStore(t)
	store := &handed{AttestationStore: under}
	binary, ran := claudeStandIn(t, claudeAnswered)
	fetched := make(chan struct{})
	close(fetched)

	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 7
	agent, err := theNodeAgent(ed25519.NewKeyFromSeed(seed), t.TempDir())
	require.NoError(t, err)
	underGround(t, agent.home)

	t.Setenv("QNTX_TEST_PLAN_TOKEN", "the-plan-token")
	named.TokenRef = "env:QNTX_TEST_PLAN_TOKEN"
	held := servingOne(db, store)
	system, _ := createTestStore(t)
	held.SetSystem(oneNamespace("system", &handed{AttestationStore: system}))
	s = &QNTXServer{
		pluginRegistry: plugin.GetDefaultRegistry(),
		held:           held,
		logger:         zaptest.NewLogger(t).Sugar(),
		deps:           &serverDependencies{cfg: &appcfg.Config{Agent: appcfg.AgentConfig{Root: named}}},
		nodeAgent:      agent,
		ctx:            context.Background(),
		ownURL:         "http://127.0.0.1:8770",
	}
	s.holdHarness("claude", &harnessHeld{fetched: fetched, path: binary})
	return s, ran
}

func saying(s *QNTXServer, sent sigil.Sent) (*protocol.ClaudeSaid, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/agent/say", nil)
	answer, refused := s.harnessSay(sigil.WithCaller(context.Background(), asked), s.claudeHarness(), sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(*protocol.ClaudeSaid), nil
}

var opusLow = appcfg.NodeAgentConfig{
	Model: "claude-opus-5-5", Effort: "low", Mode: "dontAsk", Allow: []string{"Bash", "Read", "mcp__qntx"},
}

// What is said to the node is said to Claude Code, run as am.toml names it,
// and what it answered is given back (ADR-048).
func TestWhatIsSaidToTheNodeAgentIsAnsweredByClaudeCode(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)

	answer, refused := saying(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())
	assert.False(t, answer.GetIsError())
	assert.Equal(t, "claude-opus-5-5", answer.GetModel())
	assert.Equal(t, "dontAsk", answer.GetPermissionMode())
	assert.Equal(t, []string{"Write"}, answer.GetDenied())
	holds(t, s.nodeAgentSignum(), "say", &protocol.NodeAgentSaid{Claude: answer})

	args := ranWith(t, ran, "0", "args")
	assert.Equal(t, "how long has the box been up?", after(args, "-p"))
	assert.Equal(t, "claude-opus-5-5", after(args, "--model"))
	assert.Equal(t, "low", after(args, "--effort"))
	assert.Equal(t, "dontAsk", after(args, "--permission-mode"))
	assert.Equal(t, "Bash,Read,mcp__qntx", after(args, "--allowedTools"))
	assert.Equal(t, answer.GetSession(), after(args, "--session-id"))
	assert.Contains(t, after(args, "--append-system-prompt"), s.nodeAgent.did)

	env := ranWith(t, ran, "0", "env")
	assert.Contains(t, env, "CLAUDE_CODE_OAUTH_TOKEN=the-plan-token")
	// It reaches the node's own MCP with its own token.
	assert.Contains(t, env, "QNTX_MCP_BEARER_QNTX="+s.nodeAgent.token)
	written, err := os.ReadFile(after(args, "--mcp-config"))
	require.NoError(t, err)
	assert.Contains(t, string(written), "http://127.0.0.1:8770/mcp")
}

// It is one session: what is said next is said to the same one.
func TestTheNodeAgentContinuesItsOneSession(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)

	first, refused := saying(s, sigil.Sent{"says": "remember 7"})
	require.Nil(t, refused)
	second, refused := saying(s, sigil.Sent{"says": "what did I ask you to remember?"})
	require.Nil(t, refused)

	assert.Equal(t, first.GetSession(), second.GetSession())
	again := ranWith(t, ran, "1", "args")
	assert.Equal(t, first.GetSession(), after(again, "--resume"))
	assert.NotContains(t, again, "--session-id")
}

// Its session is its own to write down: every row is signed by its DID, in
// system, which is where the node keeps its own record.
func TestTheNodeAgentsSessionReadsAsATranscriptItSigned(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	_, refused := saying(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)

	system, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	wrote, kept := system.(*handed)
	require.True(t, kept, "the system store is not the one this test handed it")
	require.Len(t, wrote.rows, 4)
	for _, row := range wrote.rows {
		assert.Equal(t, s.nodeAgent.did, row.SignerDID, "%s is not signed by the agent", row.Predicates[0])
		assert.NotEmpty(t, row.Signature)
		assert.Equal(t, []string{s.nodeAgent.did}, row.Actors)
		require.NoError(t, signing.Verify(row))
	}
	served, _ := s.held.Served().(*handed)
	assert.Empty(t, served.rows, "a row of the session was written where its speaker stands")

	assert.Equal(t, [][2]string{
		{"human", "how long has the box been up?"},
		{"session", "Start startup"},
		{"tool", "uptime"},
		{"assistant", "Up 3 days."},
	}, sessionSaid(t, s))
}

// "In my mental model, the ROOT agent is the same session"
// Spoken to from two namespaces, it is one session, read whole by either.
func TestTheNodeAgentIsOneSessionWhereverItsSpeakersStand(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	from := func(namespace, says string) {
		root := auth.Admitted(auth.LevelRoot, namespace)
		root.Identity = "https://example.org/" + namespace
		asked := httptest.NewRequest(http.MethodPost, "/api/agent/say", nil)
		asked = asked.WithContext(auth.WithAdmission(asked.Context(), root))
		_, refused := s.harnessSay(sigil.WithCaller(context.Background(), asked), s.claudeHarness(), sigil.Sent{"says": says})
		require.Nil(t, refused)
	}
	from("default", "from the browser")
	from("elsewhere", "from the phone")

	var told []string
	for _, turn := range sessionSaid(t, s) {
		if turn[0] == "human" {
			told = append(told, turn[1])
		}
	}
	assert.Equal(t, []string{"from the browser", "from the phone"}, told)
}

// sessionSaid is the agent's session as claude session reads it: each turn's
// speaker and text.
func sessionSaid(t *testing.T, s *QNTXServer) [][2]string {
	t.Helper()
	answer, refused := s.harnessSession(s.claudeHarness())
	require.Nil(t, refused)
	holds(t, s.nodeAgentSignum(), "session", answer)
	var said [][2]string
	for _, turn := range answer.(*protocol.SessionTranscript).GetTranscript().GetTurns() {
		said = append(said, [2]string{turn.GetSpeaker(), turn.GetText()})
	}
	return said
}

// "make sure --permission-mode is configurable in the Claude Element"
func TestThePermissionModeIsNamedByWhoeverSpeaks(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)

	answer, refused := saying(s, sigil.Sent{"says": "plan it", "permission_mode": "plan"})
	require.Nil(t, refused)
	assert.Equal(t, "plan", answer.GetPermissionMode())
	assert.Equal(t, "plan", after(ranWith(t, ran, "0", "args"), "--permission-mode"))

	// am.toml that gives none leaves it to the speaker, and nothing is assumed.
	bare, _ := runningTheNodeAgent(t, appcfg.NodeAgentConfig{Model: "claude-opus-5-5", Effort: "low"})
	_, refused = saying(bare, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Missing, refused.GetWhy())
	assert.Equal(t, "permission_mode", refused.GetParam())
}

// Who said it is written down with what was said.
func TestWhoSpokeToTheNodeAgentIsWrittenDown(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	root := auth.Admitted(auth.LevelRoot)
	root.Identity = "https://github.com/tim"
	asked := httptest.NewRequest(http.MethodPost, "/api/agent/say", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), root))

	answer, refused := s.harnessSay(sigil.WithCaller(context.Background(), asked), s.claudeHarness(), sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	said, held := answer.(*protocol.ClaudeSaid)
	require.True(t, held)
	session := said.GetSession()

	told, err := s.held.TheNodesOwnRecords().GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Contexts: []string{"session:" + session}, Predicates: []string{"UserPromptSubmit"}, Limit: 5})
	require.NoError(t, err)
	require.Len(t, told, 1)
	assert.Equal(t, "https://github.com/tim", told[0].Attributes["said_by"])
}

// Its token is ROOT's kind, so the gate would let it speak to itself, from
// inside the turn it would then wait on. It is told no.
func TestTheNodeAgentDoesNotSpeakToItself(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)
	itself := auth.Admitted(auth.LevelRoot)
	itself.Grant = &auth.Grant{DID: s.nodeAgent.did}
	asked := httptest.NewRequest(http.MethodPost, "/api/agent/say", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), itself))

	_, refused := s.harnessSay(sigil.WithCaller(context.Background(), asked), s.claudeHarness(), sigil.Sent{"says": "hello me"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotAllowed, refused.GetWhy())
	runs, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Empty(t, runs, "Claude Code was run for what the agent said to itself")
}

// A node whose am.toml names no Node Agent has none to speak to.
func TestANodeThatNamesNoNodeAgentHasNoneToSpeakTo(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	s.nodeAgent = nil
	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
}

// One that am.toml names and that did not start says why, in the words the
// start failed in.
func TestANodeAgentThatDidNotStartSaysWhy(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	s.nodeAgent, s.noNodeAgent = nil, errors.New("the Node Agent's token tok-1 is switched off")

	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Failed, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "tok-1 is switched off")

	_, refused = s.harnessAm(s.claudeHarness())
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "tok-1 is switched off")
}

// The agent says who it is and how it runs, which is what the Claude element
// draws before anything is said.
func TestTheNodeAgentSaysWhoItIs(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)

	before, refused := s.harnessAm(s.claudeHarness())
	require.Nil(t, refused)
	is := before.(*protocol.ClaudeAm)
	assert.Equal(t, s.nodeAgent.did, is.GetDid())
	assert.Equal(t, "claude-opus-5-5", is.GetModel())
	assert.Equal(t, "low", is.GetEffort())
	assert.Equal(t, "dontAsk", is.GetPermissionMode())
	assert.Equal(t, appcfg.PermissionModes, is.GetPermissionModes())
	assert.Equal(t, "", is.GetSession())
	holds(t, s.nodeAgentSignum(), "am", &protocol.NodeAgentAm{Claude: is})

	answer, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	after, refused := s.harnessAm(s.claudeHarness())
	require.Nil(t, refused)
	assert.Equal(t, answer.GetSession(), after.(*protocol.ClaudeAm).GetSession())
}

// While it is answering, it says so and in which session, so what it is doing
// can be read as it does it, from the first thing ever said to it.
func TestTheNodeAgentSaysWhenItIsAnswering(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	am := func() *protocol.ClaudeAm {
		is, refused := s.harnessAm(s.claudeHarness())
		require.Nil(t, refused)
		return is.(*protocol.ClaudeAm)
	}
	assert.False(t, am().GetAnswering())

	s.nodeAgent.in(s.claudeHarness()).answering.Store(&turnInSession{session: "s-first"})
	during := am()
	assert.True(t, during.GetAnswering())
	assert.Equal(t, "s-first", during.GetSession())

	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	assert.False(t, am().GetAnswering(), "a turn that ended is still said to be going")
}

// Its DID is its own: derived from the node's key, and not the node's.
func TestTheNodeAgentIsItself(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 7
	node := ed25519.NewKeyFromSeed(seed)
	agent, err := theNodeAgent(node, t.TempDir())
	require.NoError(t, err)

	nodePub, _ := node.Public().(ed25519.PublicKey)
	assert.NotEqual(t, access.EncodeDIDKey(nodePub), agent.did)
	_, did, err := access.DeriveToken(node, nodeAgentPurpose)
	require.NoError(t, err)
	assert.Equal(t, did, agent.did)
}

func TestTheClaudeSignumSaysWhatItHolds(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	require.NoError(t, s.nodeAgentSignum().Check())
}

// "remove SUPER"
// The only guard is who may talk to it, and that is ROOT.
func TestWhoMayTalkToTheNodeAgent(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	s, _ := runningTheNodeAgent(t, opusLow)
	for _, held := range s.nodeAgentSignum().GetSigils() {
		assert.Equal(t, []string{"ROOT"}, compiled[held.GetHttp().GetPath()], held.GetHttp().GetPath())
	}
}

// "every call it makes carries its own credential": what the node resolves
// from its own environment, the plan token's env: reference here, is handed to
// Claude Code as the agent's credential and is not in its environment beside it.
func TestATurnInClaudeCodeCarriesOnlyItsOwnCredential(t *testing.T) {
	t.Setenv("QNTX_NODE_SECRET", "the node's")
	s, ran := runningTheNodeAgent(t, opusLow)
	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)

	env := ranWith(t, ran, "0", "env")
	assert.Contains(t, env, "CLAUDE_CODE_OAUTH_TOKEN=the-plan-token")
	assert.NotContains(t, env, "QNTX_TEST_PLAN_TOKEN=the-plan-token")
	assert.NotContains(t, env, "QNTX_NODE_SECRET=the node's")
}
