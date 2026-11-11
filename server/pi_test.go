package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/signing"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/pi"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/harness/piharness"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

// runningPiToo is the Node Agent of runningTheNodeAgent, with Pi named and a
// stand-in for it held.
func runningPiToo(t *testing.T) (s *QNTXServer, claudeRan, piRan string) {
	t.Helper()
	named := opusLow
	named.Pi = sonnetThroughTheGateway
	s, claudeRan = runningTheNodeAgent(t, named)
	binary, piRan := claudeStandIn(t, piAnswered)
	piPluginRuns(t, s, binary)
	return s, claudeRan, piRan
}

// piPlugin is a plugin named pi that runs Pi: it declared harness_provider,
// and what it serves is reached over gRPC, as a plugin's is (ADR-059).
type piPlugin struct {
	fakePlugin
	served a2apb.A2AServiceClient
}

func (p *piPlugin) IsHarnessProvider() bool                  { return true }
func (p *piPlugin) A2AServiceClient() a2apb.A2AServiceClient { return p.served }

// piPluginRuns has a pi plugin running on s, whose Pi is the stand-in at
// binary: a2a-go's gRPC binding of piharness, served and dialed on loopback.
func piPluginRuns(t *testing.T, s *QNTXServer, binary string) {
	t.Helper()
	listening, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serving := grpc.NewServer()
	a2agrpc.NewHandler(piharness.Handler(func(context.Context) (string, error) { return binary, nil })).RegisterWith(serving)
	go func() { _ = serving.Serve(listening) }()
	t.Cleanup(serving.Stop)
	conn, err := grpc.NewClient(listening.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	s.pluginRegistry = plugin.NewRegistry("0.1.0", zaptest.NewLogger(t).Sugar())
	require.NoError(t, s.pluginRegistry.Register(&piPlugin{fakePlugin: fakePlugin{name: agentLinePi}, served: a2apb.NewA2AServiceClient(conn)}))
	s.pluginRegistry.MarkReady(agentLinePi)
}

func sayingToPi(s *QNTXServer, sent sigil.Sent) (*protocol.PiSaid, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/agent/say", nil)
	answer, refused := s.harnessSay(sigil.WithCaller(context.Background(), asked), s.piHarness(), sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(*protocol.PiSaid), nil
}

// What is said in the Pi element is answered by Pi, which reaches the node's
// MCP with the agent's own token and makes its model calls through the
// gateway plugin, presenting that same token.
func TestWhatIsSaidToPiIsAnsweredByPi(t *testing.T) {
	s, _, ran := runningPiToo(t)

	answer, refused := sayingToPi(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())
	assert.False(t, answer.GetIsError())
	assert.Equal(t, "anthropic/claude-sonnet-4.6", answer.GetModel())
	holds(t, s.nodeAgentSignum(), "say", &protocol.NodeAgentSaid{Pi: answer})

	args := ranWith(t, ran, "0", "args")
	assert.Equal(t, "json", after(args, "--mode"))
	assert.Equal(t, "qntx/anthropic/claude-sonnet-4.6", after(args, "--model"))
	assert.Equal(t, "low", after(args, "--thinking"))
	assert.Equal(t, answer.GetSession(), after(args, "--session-id"))
	assert.Contains(t, after(args, "--append-system-prompt"), s.nodeAgent.did)
	assert.NotContains(t, args, "--no-builtin-tools", "the Node Agent runs Pi's own tools")

	env := ranWith(t, ran, "0", "env")
	for _, line := range env {
		assert.False(t, strings.HasPrefix(line, "QNTX_MCP_BEARER_"), "the Node Agent was handed a bearer: %s", line)
	}
	// Pi names a key for its provider, and is handed the agent's DID: no secret.
	assert.Contains(t, env, "QNTX_PI_PROVIDER_KEY="+s.nodeAgent.did)

	var models struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
		} `json:"providers"`
	}
	raw, err := os.ReadFile(filepath.Join(s.nodeAgent.home, "pi", "models.json"))
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
	assert.NotEqual(t, inClaude.GetSession(), inPi.GetSession())

	system, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	for _, row := range system.(*handed).rows {
		require.NoError(t, signing.Verify(row))
		assert.Equal(t, s.nodeAgent.did, row.SignerDID)
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

// "there should be one NODE agent at a time": Pi does not answer while
// Claude Code is in a turn.
func TestPiDoesNotAnswerWhileClaudeCodeIsInATurn(t *testing.T) {
	s, _, _ := runningPiToo(t)
	s.nodeAgent.in(s.claudeHarness()).turn <- struct{}{}
	defer func() { <-s.nodeAgent.in(s.claudeHarness()).turn }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	asked := httptest.NewRequest(http.MethodPost, "/api/agent/say", nil)
	_, refused := s.harnessSay(sigil.WithCaller(ctx, asked), s.piHarness(), sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "still answering")
}

// piSessionSaid is the agent's session in Pi as pi session reads it.
func piSessionSaid(t *testing.T, s *QNTXServer) [][2]string {
	t.Helper()
	answer, refused := s.harnessSession(s.piHarness())
	require.Nil(t, refused)
	holds(t, s.nodeAgentSignum(), "session", answer)
	var said [][2]string
	for _, turn := range answer.(*protocol.SessionTranscript).GetTranscript().GetTurns() {
		said = append(said, [2]string{turn.GetSpeaker(), turn.GetText()})
	}
	return said
}

// A node whose am.toml names no Pi has none to speak to, and says where to
// name one.
func TestANodeThatNamesNoPiHasNoneToSpeakTo(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	_, refused := sayingToPi(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "[agent.root.pi]")
}

// Pi says who answers in it and how it is run, which the Pi element draws.
func TestPiSaysWhoItIs(t *testing.T) {
	s, _, _ := runningPiToo(t)
	is, refused := s.harnessAm(s.piHarness())
	require.Nil(t, refused)
	am := is.(*protocol.PiAm)
	assert.Equal(t, s.nodeAgent.did, am.GetDid())
	assert.Equal(t, "anthropic/claude-sonnet-4.6", am.GetModel())
	assert.Equal(t, "openrouter-qntx", am.GetGateway())
	assert.Equal(t, pi.PinnedVersion, am.GetPiVersion())
	holds(t, s.nodeAgentSignum(), "am", &protocol.NodeAgentAm{Pi: am})
	require.NoError(t, s.nodeAgentSignum().Check())
}

// "We shouldn't be displaying unavailable things to people"
//
// 「[Opus 5.5] am asked in no harness answers in every harness am.toml names the
// Node Agent in, and in none other.」
func TestAmInNoHarnessIsEveryHarnessItIsNamedIn(t *testing.T) {
	alone, _ := runningTheNodeAgent(t, opusLow)
	answer, refused := alone.nodeAgentSignum().Answers["am"](context.Background(), sigil.Sent{})
	require.Nil(t, refused)
	am := answer.(*protocol.NodeAgentAm)
	assert.NotNil(t, am.GetClaude())
	assert.Nil(t, am.GetPi(), "this node's am.toml names no Pi")
	assert.Nil(t, am.GetCodex())
	assert.Nil(t, am.GetAgy())
	holds(t, alone.nodeAgentSignum(), "am", am)

	withPi, _, _ := runningPiToo(t)
	answer, refused = withPi.nodeAgentSignum().Answers["am"](context.Background(), sigil.Sent{})
	require.Nil(t, refused)
	am = answer.(*protocol.NodeAgentAm)
	assert.NotNil(t, am.GetClaude())
	assert.Equal(t, "anthropic/claude-sonnet-4.6", am.GetPi().GetModel())
	holds(t, withPi.nodeAgentSignum(), "am", am)
	require.NoError(t, withPi.nodeAgentSignum().Check())
}

// 「[Opus 5.5] A harness whose binary has not arrived, or failed to, is not in am
// asked in no harness, and one still arriving is said to be; asked in it by
// name, am still says why.」
func TestAmInNoHarnessLeavesOutAHarnessNotReady(t *testing.T) {
	for name, each := range map[string]struct {
		held     *harnessHeld
		arriving []string
	}{
		"still arriving": {&harnessHeld{name: "Pi", fetched: make(chan struct{})}, []string{"pi"}},
		"failed to arrive": {func() *harnessHeld {
			h := &harnessHeld{name: "Pi", fetched: make(chan struct{}), err: errors.New("the download was cut off")}
			close(h.fetched)
			return h
		}(), nil},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := runningPiToo(t)
			s.holdHarness("pi", each.held)
			answer, refused := s.nodeAgentSignum().Answers["am"](context.Background(), sigil.Sent{})
			require.Nil(t, refused)
			am := answer.(*protocol.NodeAgentAm)
			assert.NotNil(t, am.GetClaude())
			assert.Nil(t, am.GetPi())
			assert.Equal(t, each.arriving, am.GetArriving())
			holds(t, s.nodeAgentSignum(), "am", am)

			named, refused := s.harnessAm(s.piHarness())
			require.Nil(t, refused)
			assert.NotEmpty(t, named.(*protocol.PiAm).GetNotReady())
		})
	}
}

// 「[Opus 5.5] An element asked am in no harness while no harness is ready is
// answered in none, the one arriving named, and no agent of it is held. When
// none is ready or arriving, it is refused, and still no agent is held.」
func TestElementAmInNoHarnessReadyHoldsNoAgent(t *testing.T) {
	s, _ := runningElementAgents(t)
	s.holdHarness("claude", &harnessHeld{name: "Claude Code", fetched: make(chan struct{})})
	answer, refused := s.elementsSignum().Answers["am"](context.Background(), sigil.Sent{"element": "never-seen"})
	require.Nil(t, refused)
	am := answer.(*protocol.NodeAgentAm)
	assert.Nil(t, am.GetClaude())
	assert.Equal(t, []string{"claude"}, am.GetArriving())
	_, held := s.elementAgents.of(elementPurpose("never-seen"))
	assert.False(t, held)
	_, refused = s.elementsSignum().Answers["am"](context.Background(), sigil.Sent{"element": "../never-seen"})
	require.NotNil(t, refused, "an element named by a path is refused, arriving or not")
	assert.Equal(t, sigil.Invalid, refused.GetWhy())

	failed := &harnessHeld{name: "Claude Code", fetched: make(chan struct{}), err: errors.New("the download was cut off")}
	close(failed.fetched)
	s.holdHarness("claude", failed)
	_, refused = s.elementsSignum().Answers["am"](context.Background(), sigil.Sent{"element": "never-seen"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "the download was cut off")
	_, held = s.elementAgents.of(elementPurpose("never-seen"))
	assert.False(t, held)
}

// 「[Opus 5.5] An element's agent, asked am in no harness, answers in the ones it can be spoken to in.」
func TestElementAmInNoHarnessIsEveryHarnessItIsNamedIn(t *testing.T) {
	s, _ := runningElementAgents(t)
	answer, refused := s.elementsSignum().Answers["am"](context.Background(), sigil.Sent{"element": "ground-element"})
	require.Nil(t, refused)
	am := answer.(*protocol.NodeAgentAm)
	assert.NotNil(t, am.GetClaude())
	assert.Nil(t, am.GetPi(), "this node's am.toml names no Pi")
	holds(t, s.elementsSignum(), "am", am)
}

// 「[Opus 5.5] The harnesses an agent's sigils take are the harnesses it runs in, said once.」
func TestTheHarnessesTakenAreTheHarnessesItRunsIn(t *testing.T) {
	s, _, _ := runningPiToo(t)
	takes := func(signum *protocol.Signum, name string) []string {
		for _, each := range signum.GetSigils() {
			if each.GetName() == name {
				for _, param := range each.GetTakes() {
					if param.GetName() == "harness" {
						return param.GetOneOf()
					}
				}
			}
		}
		return nil
	}
	assert.Equal(t, []string{"claude", "pi", "codex", "agy"}, takes(s.nodeAgentSignum().Signum, "say"))
	assert.Equal(t, []string{"claude", "pi", "codex", "agy"}, takes(s.nodeAgentSignum().Signum, "am"))
	assert.Equal(t, []string{"claude", "pi"}, takes(s.elementsSignum().Signum, "am"))
	_, refused := s.harnessNamed(sigil.Sent{"harness": "gemini"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "claude, pi, codex, agy")
}

// Only ROOT talks to it, in Pi as in Claude Code.
func TestOnlyRootTalksToItInPi(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	s, _, _ := runningPiToo(t)
	for _, held := range s.nodeAgentSignum().GetSigils() {
		assert.Equal(t, []string{"ROOT"}, compiled[held.GetHttp().GetPath()], held.GetHttp().GetPath())
	}
}

// The same in Pi: the node's environment is the node's.
func TestATurnInPiCarriesOnlyItsOwnCredential(t *testing.T) {
	t.Setenv("QNTX_NODE_SECRET", "the node's")
	s, _, ran := runningPiToo(t)
	_, refused := sayingToPi(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)

	env := ranWith(t, ran, "0", "env")
	assert.Contains(t, env, "QNTX_PI_PROVIDER_KEY="+s.nodeAgent.did)
	assert.NotContains(t, env, "QNTX_TEST_PLAN_TOKEN=the-plan-token")
	assert.NotContains(t, env, "QNTX_NODE_SECRET=the node's")
}

// "Harness is a plugin that speaks grpc A2A": Pi is the pi plugin's to run. A
// node that names Pi and runs no such plugin has no Pi, and nothing answers in
// its place; a plugin still loading runs nothing yet.
func TestPiIsRunByThePiPluginAndNothingElse(t *testing.T) {
	named := opusLow
	named.Pi = sonnetThroughTheGateway
	s, _ := runningTheNodeAgent(t, named)
	s.pluginRegistry = plugin.NewRegistry("0.1.0", zaptest.NewLogger(t).Sugar())

	_, refused := sayingToPi(s, sigil.Sent{"says": "how long has the box been up?"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "no plugin on this node runs Pi")

	// A binary the node itself holds is not Pi's way in any more.
	binary, ran := claudeStandIn(t, piAnswered)
	fetched := make(chan struct{})
	close(fetched)
	s.holdHarness("pi", &harnessHeld{name: "Pi", fetched: fetched, path: binary})
	_, refused = sayingToPi(s, sigil.Sent{"says": "how long has the box been up?"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "no plugin on this node runs Pi")
	entries, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Empty(t, entries, "Pi was run by the node itself")

	// Loading is not running.
	s.holdHarness("pi", nil)
	s.pluginRegistry.PreRegister(agentLinePi)
	require.NoError(t, s.pluginRegistry.Register(&piPlugin{fakePlugin: fakePlugin{name: agentLinePi}}))
	_, runs := s.harnessBackend(agentLinePi)
	assert.False(t, runs)
	s.pluginRegistry.MarkReady(agentLinePi)
	_, runs = s.harnessBackend(agentLinePi)
	assert.True(t, runs)
}

// A run Pi does not answer is the turn failing in Pi's own words, carried back
// from the plugin as the Task failed.
func TestARunThePiPluginCannotAnswerSaysWhy(t *testing.T) {
	named := opusLow
	named.Pi = sonnetThroughTheGateway
	s, _ := runningTheNodeAgent(t, named)
	piPluginRuns(t, s, filepath.Join(t.TempDir(), "no-pi-here"))

	_, refused := sayingToPi(s, sigil.Sent{"says": "how long has the box been up?"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "Pi did not answer")
	assert.Contains(t, refused.GetSays(), "no-pi-here")
}
