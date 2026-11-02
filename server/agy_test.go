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
	"github.com/teranos/QNTX/internal/agy"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
)

// "wiring it into node and namespace config as an actual harness"

// The stream a stand-in for agy prints under --output-format stream-json: a
// turn that ran one command and answered, in the shapes agy 1.4.4 gives.
const agyAnswered = `{"event":"init","conversation_id":"conv-7","init":{"cwd":"/w","tools":["run_command"],"permission_mode":"request-review"}}
{"event":"step_update","step_update":{"conversation_id":"conv-7","step_index":0,"state":"DONE","step_type":"user_input"}}
{"event":"step_update","step_update":{"conversation_id":"conv-7","step_index":1,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"uptime"}}}}
{"event":"step_update","step_update":{"conversation_id":"conv-7","step_index":1,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"uptime"},"output":"up 3 days"}}}
{"event":"step_update","step_update":{"conversation_id":"conv-7","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"Up 3 days."}}
{"event":"result","result":{"conversation_id":"conv-7","status":"SUCCESS","response":"Up 3 days.","duration_seconds":2,"num_turns":1,"usage":{"input_tokens":10,"output_tokens":4,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":14}}}
`

var flashInAgy = appcfg.AgyConfig{Model: "gemini-3.8-flash-low", TokenRef: "env:QNTX_TEST_GEMINI_KEY"}

// runningAgyToo is the Node Agent of runningTheNodeAgent, with agy named and a
// stand-in for it held.
func runningAgyToo(t *testing.T) (s *QNTXServer, ran string) {
	t.Helper()
	t.Setenv("QNTX_TEST_GEMINI_KEY", "the-agents-gemini-key")
	named := opusLow
	named.Agy = flashInAgy
	s, _ = runningTheNodeAgent(t, named)
	binary, ran := claudeStandIn(t, agyAnswered)
	fetched := make(chan struct{})
	close(fetched)
	s.holdHarness("agy", &harnessHeld{name: "agy", fetched: fetched, path: binary})
	return s, ran
}

func sayingToAgy(s *QNTXServer, sent sigil.Sent) (*protocol.AgySaid, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/agent/say", nil)
	answer, refused := s.harnessSay(sigil.WithCaller(context.Background(), asked), s.agyHarness(), sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(*protocol.AgySaid), nil
}

// What is said to the Node Agent in agy is answered by agy: run headless in the
// agent's own home, on the Gemini key am.toml names, with the agent's ground on
// every event agy sends.
func TestWhatIsSaidToAgyIsAnsweredByAgy(t *testing.T) {
	s, ran := runningAgyToo(t)

	answer, refused := sayingToAgy(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())
	assert.Equal(t, "gemini-3.8-flash-low", answer.GetModel())
	assert.Equal(t, "conv-7", answer.GetConversation())
	assert.Equal(t, agy.PinnedVersion, answer.GetAgyVersion())
	assert.EqualValues(t, 4, answer.GetOutputTokens())
	holds(t, s.nodeAgentSignum(), "say", &protocol.NodeAgentSaid{Agy: answer})

	args := ranWith(t, ran, "0", "args")
	assert.Equal(t, "gemini-3.8-flash-low", after(args, "--model"))
	assert.NotContains(t, args, "--conversation", "a first turn starts agy's conversation")

	home := s.nodeAgent.home
	env := ranWith(t, ran, "0", "env")
	assert.Contains(t, env, "HOME="+home)
	assert.Contains(t, env, "GEMINI_API_KEY=the-agents-gemini-key")
	assert.Contains(t, env, "AGY_CLI_DISABLE_AUTO_UPDATE=true")
	assert.NotContains(t, env, "GEMINI_API_KEY="+s.nodeAgent.token)

	raw, err := os.ReadFile(filepath.Join(home, ".gemini", "config", "hooks.json"))
	require.NoError(t, err)
	var hooks map[string]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &hooks))
	ground := hooks["ground"]
	for _, event := range []string{"SessionStart", "PreToolUse", "PostToolUse", "PreInvocation", "PostInvocation", "Stop"} {
		require.Contains(t, ground, event)
		assert.Contains(t, string(ground[event]), comet.AgentHook(home)+" agy "+event)
	}
}

// agy names its conversation on the first turn; the next resumes that one,
// and both are written down in the one session.
func TestAgyResumesItsOwnConversationInTheOneSession(t *testing.T) {
	s, ran := runningAgyToo(t)

	first, refused := sayingToAgy(s, sigil.Sent{"says": "remember 7"})
	require.Nil(t, refused)
	second, refused := sayingToAgy(s, sigil.Sent{"says": "what did I say?"})
	require.Nil(t, refused)
	assert.Equal(t, first.GetSession(), second.GetSession())
	assert.NotEqual(t, "conv-7", first.GetSession(), "the session is the node's, the conversation agy's")
	assert.Equal(t, "conv-7", after(ranWith(t, ran, "1", "args"), "--conversation"), "the second turn resumes agy's own conversation")
}

// The session reads as the other harnesses' do.
func TestAgySessionReadsAsTheOthers(t *testing.T) {
	s, _ := runningAgyToo(t)
	_, refused := sayingToAgy(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)

	answer, refused := s.harnessSession(s.agyHarness())
	require.Nil(t, refused)
	holds(t, s.nodeAgentSignum(), "session", answer)
	var said [][2]string
	for _, turn := range answer.(*protocol.SessionTranscript).GetTranscript().GetTurns() {
		said = append(said, [2]string{turn.GetSpeaker(), turn.GetText()})
	}
	assert.Equal(t, [][2]string{
		{"human", "how long has the box been up?"},
		{"session", "Start agy"},
		{"tool", "uptime"},
		{"assistant", "Up 3 days."},
	}, said)
}

// A key that does not resolve says nothing to agy, and says why.
func TestAgyIsSaidNothingWithoutItsKey(t *testing.T) {
	s, ran := runningAgyToo(t)
	t.Setenv("QNTX_TEST_GEMINI_KEY", "")
	_, refused := sayingToAgy(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "Gemini API key")
	_, err := os.Stat(filepath.Join(ran, "0"))
	assert.True(t, os.IsNotExist(err), "agy did not run")
}

// A node whose am.toml names no agy has none to speak to, and says where.
func TestANodeThatNamesNoAgyHasNoneToSpeakTo(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	_, refused := sayingToAgy(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "[agent.root.agy]")
}

// agy says who answers in it and how it is run.
func TestAgySaysWhoItIs(t *testing.T) {
	s, _ := runningAgyToo(t)
	is, refused := s.harnessAm(s.agyHarness())
	require.Nil(t, refused)
	am := is.(*protocol.AgyAm)
	assert.Equal(t, s.nodeAgent.did, am.GetDid())
	assert.Equal(t, "gemini-3.8-flash-low", am.GetModel())
	assert.Equal(t, agy.PinnedVersion, am.GetAgyVersion())
	holds(t, s.nodeAgentSignum(), "am", &protocol.NodeAgentAm{Agy: am})
	require.NoError(t, s.nodeAgentSignum().Check())

	harness, refused := s.harnessNamed(sigil.Sent{"harness": "agy"})
	require.Nil(t, refused)
	assert.Equal(t, "agy", harness.name)
}
