//go:build libacp

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/agy"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
)

// "correct it for libACP client use for agy"
// What is said to the Node Agent in agy is answered by `agy acp`, through
// libACP's client: in the agent's own home, signed in with the Gemini key
// am.toml names, the model set as its session's option, with the agent's
// ground on every event agy sends.
func TestWhatIsSaidToAgyIsAnsweredByAgy(t *testing.T) {
	s, ran := runningAgyToo(t)

	answer, refused := sayingToAgy(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())
	assert.Equal(t, "gemini-3.8-flash", answer.GetModel())
	assert.Equal(t, "conv-7", answer.GetConversation())
	assert.Equal(t, agy.PinnedVersion, answer.GetAgyVersion())
	assert.EqualValues(t, 14, answer.GetContextUsed())
	assert.EqualValues(t, 1000000, answer.GetContextSize())
	holds(t, s.nodeAgentSignum(), "say", &protocol.NodeAgentSaid{Agy: answer})

	args := ranWith(t, ran, "0", "args")
	assert.Equal(t, "acp", args[0])
	assert.Contains(t, args, "--sandbox")
	input := strings.Join(ranWith(t, ran, "0", "input"), "\n")
	assert.Contains(t, input, `"method":"authenticate","params":{"methodId":"gemini-api-key"}`)
	assert.Contains(t, input, `"configId":"model","value":"gemini-3.8-flash"`)
	assert.Contains(t, input, `"method":"session/new"`, "a first turn starts agy's session")

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

// agy names its session on the first turn; the next resumes that one, and both
// are written down in the one session.
func TestAgyResumesItsOwnConversationInTheOneSession(t *testing.T) {
	s, ran := runningAgyToo(t)

	first, refused := sayingToAgy(s, sigil.Sent{"says": "remember 7"})
	require.Nil(t, refused)
	second, refused := sayingToAgy(s, sigil.Sent{"says": "what did I say?"})
	require.Nil(t, refused)
	assert.Equal(t, first.GetSession(), second.GetSession())
	assert.NotEqual(t, "conv-7", first.GetSession(), "the session is the node's, the conversation agy's")
	input := strings.Join(ranWith(t, ran, "1", "input"), "\n")
	assert.Contains(t, input, `"method":"session/resume","params":{"sessionId":"conv-7"`, "the second turn resumes agy's own session")
	assert.NotContains(t, input, `"method":"session/new"`)
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
