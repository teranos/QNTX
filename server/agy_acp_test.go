//go:build libacp

package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/acp/acptest"
	"github.com/teranos/QNTX/internal/agy"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
)

// "correct it for libACP client use for agy"
// 「[Opus 5.5] What is said to the Node Agent in agy is answered by `agy acp`, through」
// 「[Opus 5.5] libACP's client: in the agent's own home, signed in as the node at Google with」
// 「[Opus 5.5] no key anywhere, the model set as its session's option, with the agent's ground」
// 「[Opus 5.5] on every event agy sends.」
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
	assert.Contains(t, input, `"method":"authenticate","params":{"methodId":"agent-platform"}`)
	assert.Contains(t, input, `"configId":"model","value":"gemini-3.8-flash"`)
	assert.Contains(t, input, `"method":"session/new"`, "a first turn starts agy's session")

	home := s.nodeAgent.home
	env := ranWith(t, ran, "0", "env")
	assert.Contains(t, env, "HOME="+home)
	credentials := filepath.Join(home, ".qntx-google", "external-account.json")
	assert.Contains(t, env, "GOOGLE_APPLICATION_CREDENTIALS="+credentials)
	assert.Contains(t, env, "GOOGLE_CLOUD_PROJECT=the-project")
	assert.Contains(t, env, "GOOGLE_GENAI_USE_VERTEXAI=true")
	for _, each := range env {
		assert.False(t, strings.HasPrefix(each, "GEMINI_API_KEY="), "no key: %s", each)
	}
	written, err := os.ReadFile(credentials)
	require.NoError(t, err)
	var file map[string]any
	require.NoError(t, json.Unmarshal(written, &file))
	assert.Equal(t, "external_account", file["type"])
	assert.Equal(t, googleAudience("123"), file["audience"])
	assert.Contains(t, file["service_account_impersonation_url"], theAccount)
	token := file["credential_source"].(map[string]any)["file"].(string)
	info, err := os.Stat(token)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Contains(t, env, "AGY_CLI_DISABLE_AUTO_UPDATE=true")

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

// 「[Opus 5.5] agy names its session on the first turn; the next resumes that one, and both」
// 「[Opus 5.5] are written down in the one session.」
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

// 「[Opus 5.5] The session reads as the other harnesses' do.」
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

// 「[Opus 5.5] "per thing basis": connecting the node asks for nothing that spends. agy is its」
// 「[Opus 5.5] own yes, and a node without it says where to give it.」
func TestAgyIsItsOwnYesAtGoogle(t *testing.T) {
	s, g := connectedToGoogle(t)
	s.deps.cfg.Agent.Root.Agy = flashInAgy
	binary, _ := acptest.StandIn(t, agyAnswered)
	fetched := make(chan struct{})
	close(fetched)
	s.holdHarness("agy", &harnessHeld{name: "agy", fetched: fetched, path: binary})

	assert.NotContains(t, g.enabled, googleModelService)
	am := rootAsksGoogle(t, s)
	assert.False(t, am.GetAgyWired())
	assert.Contains(t, am.GetAgyConnect(), "project=the-project&thing=agy")
	_, refused := sayingToAgy(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "Connect agy")

	said, err := s.connectGoogleAgy(context.Background(), "root-token", "google:root-sub")
	require.NoError(t, err)
	assert.Contains(t, said, "the-project")
	assert.Contains(t, g.enabled, googleModelService)
	assert.Equal(t, []any{map[string]any{"role": googleRunsModels, "members": []any{"serviceAccount:" + theAccount}}}, g.projectBindings)
	assert.True(t, rootAsksGoogle(t, s).GetAgyWired())
	answer, refused := sayingToAgy(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused, "%v", refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())
}

// "everything that costs anything needs to be halted until the budget frees up"
func TestAgyIsHaltedAt80PercentAndResumesWhenTheBudgetFreesUp(t *testing.T) {
	s, b, _ := wiredForCosts(t)
	s.deps.cfg.Agent.Root.Agy = flashInAgy
	binary, _ := acptest.StandIn(t, agyAnswered)
	fetched := make(chan struct{})
	close(fetched)
	s.holdHarness("agy", &harnessHeld{name: "agy", fetched: fetched, path: binary})

	require.Nil(t, s.keepGoogleCostLine(googleAgyLineSubject, "wired", "google:root-sub", map[string]any{"role": googleRunsModels}))

	b.says("b1", "a1", "2026-11-20T00:00:00Z", "monthly", 16, 20, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	_, refused := sayingToAgy(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Failed, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "halted")

	b.says("b1", "a2", "2026-12-01T00:00:00Z", "monthly", 1, 20, "2026-12-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	answer, refused := sayingToAgy(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused, "%v", refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())
}
