package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/signing"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/pi"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// "NOT BE ENTIRELY DEPENDENT ON ONE HARNESS"

// The stream a stand-in for Pi prints: a run that reached for one tool and
// answered, in the shapes Pi's JSON mode documents.
const piAnswered = `{"type":"session","version":3,"id":"s-1","timestamp":"2026-10-05T20:00:00.000Z","cwd":"/work"}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"uptime"}}],"provider":"qntx","model":"anthropic/claude-sonnet-4.6","usage":{"cost":{"total":0.01}},"stopReason":"toolUse","timestamp":1791230401000}}
{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"uptime"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Up 3 days."}],"provider":"qntx","model":"anthropic/claude-sonnet-4.6","usage":{"cost":{"total":0.02}},"stopReason":"stop","timestamp":1791230402000}}
{"type":"agent_settled"}
`

var sonnetThroughTheGateway = appcfg.PiConfig{Model: "anthropic/claude-sonnet-4.6", Thinking: "low", Gateway: "openrouter-qntx"}

// runningPiToo is the ROOT agent of runningTheRootAgent, with Pi named and a
// stand-in for it held.
func runningPiToo(t *testing.T) (s *QNTXServer, claudeRan, piRan string) {
	t.Helper()
	named := opusLow
	named.Pi = sonnetThroughTheGateway
	s, claudeRan = runningTheRootAgent(t, named)
	binary, piRan := claudeStandIn(t, piAnswered)
	fetched := make(chan struct{})
	close(fetched)
	s.pi = &harnessHeld{name: "Pi", fetched: fetched, path: binary}
	return s, claudeRan, piRan
}

func sayingToPi(s *QNTXServer, sent sigil.Sent) (map[string]any, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/pi/say", nil)
	answer, refused := s.piSay(sigil.WithCaller(context.Background(), asked), sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(map[string]any), nil
}

// What is said in the Pi element is answered by Pi, which reaches the node's
// MCP with the agent's own token and makes its model calls through the
// gateway plugin, presenting that same token.
func TestWhatIsSaidToPiIsAnsweredByPi(t *testing.T) {
	s, _, ran := runningPiToo(t)

	answer, refused := sayingToPi(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer["answer"])
	assert.Equal(t, false, answer["is_error"])
	assert.Equal(t, "anthropic/claude-sonnet-4.6", answer["model"])
	holds(t, s.piSignum(), "say", answer)

	args := ranWith(t, ran, "0", "args")
	assert.Equal(t, "json", after(args, "--mode"))
	assert.Equal(t, "qntx/anthropic/claude-sonnet-4.6", after(args, "--model"))
	assert.Equal(t, "low", after(args, "--thinking"))
	assert.Equal(t, answer["session"], after(args, "--session-id"))
	assert.Contains(t, after(args, "--append-system-prompt"), s.rootAgent.did)

	env := ranWith(t, ran, "0", "env")
	assert.Contains(t, env, "QNTX_MCP_BEARER_QNTX="+s.rootAgent.token)
	assert.Contains(t, env, "QNTX_PI_PROVIDER_KEY="+s.rootAgent.token)

	var models struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
		} `json:"providers"`
	}
	raw, err := os.ReadFile(filepath.Join(s.rootAgent.home, "pi", "models.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &models))
	assert.Equal(t, "http://127.0.0.1:8770/api/openrouter-qntx/v1", models.Providers["qntx"].BaseURL)
}

// "WHY ISNT PI SEPARATE"
// One agent in two sessions: each harness's is its own, both signed by the
// agent's DID, and each element reads only its own.
func TestClaudeCodeAndPiAreTwoSessionsOfOneAgent(t *testing.T) {
	s, _, _ := runningPiToo(t)

	inClaude, refused := saying(s, sigil.Sent{"says": "remember 7"})
	require.Nil(t, refused)
	inPi, refused := sayingToPi(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.NotEqual(t, inClaude["session"], inPi["session"])

	system, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	for _, row := range system.(*handed).rows {
		require.NoError(t, signing.Verify(row))
		assert.Equal(t, s.rootAgent.did, row.SignerDID)
	}

	claudeSaid := sessionSaid(t, s)
	assert.Contains(t, claudeSaid, [2]string{"human", "remember 7"})
	assert.NotContains(t, claudeSaid, [2]string{"session", "Start pi"})

	piSaid := piSessionSaid(t, s)
	assert.Equal(t, [][2]string{
		{"human", "how long has the box been up?"},
		{"session", "Start pi"},
		{"tool", "uptime"},
		{"assistant", "Up 3 days."},
	}, piSaid, "Pi's bash reads as the command it ran, as Claude Code's Bash does")
}

// Each session hears one thing at a time, and the two answer beside each
// other: Pi answers while Claude Code is in a turn.
func TestPiAnswersWhileClaudeCodeIsInATurn(t *testing.T) {
	s, _, _ := runningPiToo(t)
	s.rootAgent.claude.turn <- struct{}{}
	defer func() { <-s.rootAgent.claude.turn }()

	answer, refused := sayingToPi(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer["answer"])
}

// piSessionSaid is the agent's session in Pi as pi session reads it.
func piSessionSaid(t *testing.T, s *QNTXServer) [][2]string {
	t.Helper()
	asked := httptest.NewRequest(http.MethodGet, "/api/pi/session", nil)
	answer, refused := s.piSession(sigil.WithCaller(context.Background(), asked), sigil.Sent{})
	require.Nil(t, refused)
	holds(t, s.piSignum(), "session", answer)
	read, _ := answer.(map[string]any)["transcript"].(transcript)
	var said [][2]string
	for _, turn := range read.Turns {
		said = append(said, [2]string{turn.Speaker, turn.Text})
	}
	return said
}

// A node whose am.toml names no Pi has none to speak to, and says where to
// name one.
func TestANodeThatNamesNoPiHasNoneToSpeakTo(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	_, refused := sayingToPi(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "[agent.root.pi]")
}

// Pi says who answers in it and how it is run, which the Pi element draws.
func TestPiSaysWhoItIs(t *testing.T) {
	s, _, _ := runningPiToo(t)
	asked := httptest.NewRequest(http.MethodGet, "/api/pi", nil)
	is, refused := s.piAm(sigil.WithCaller(context.Background(), asked), sigil.Sent{})
	require.Nil(t, refused)
	am := is.(map[string]any)
	assert.Equal(t, s.rootAgent.did, am["did"])
	assert.Equal(t, "anthropic/claude-sonnet-4.6", am["model"])
	assert.Equal(t, "openrouter-qntx", am["gateway"])
	assert.Equal(t, pi.PinnedVersion, am["pi_version"])
	holds(t, s.piSignum(), "am", am)
	require.NoError(t, s.piSignum().Check())
}

// Only ROOT talks to it, in Pi as in Claude Code.
func TestOnlyRootTalksToItInPi(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	s, _, _ := runningPiToo(t)
	for _, held := range s.piSignum().GetSigils() {
		assert.Equal(t, []string{"ROOT"}, compiled[held.GetHttp().GetPath()], held.GetHttp().GetPath())
	}
}
