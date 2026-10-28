package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/codex"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
)

// "Codex Agent runtime as another first class citizen in QNTX besides Claude"

// The stream a stand-in for codex exec --json prints: a turn that ran one
// command and answered, in the shapes codex-rs/exec/src/exec_events.rs gives.
const codexAnswered = `{"type":"thread.started","thread_id":"thread-7"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"uptime","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"uptime","aggregated_output":"up 3 days","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Up 3 days."}}
{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":4}}
`

var sonnetInCodex = appcfg.CodexConfig{Model: "anthropic/claude-sonnet-4.5", Gateway: "openrouter-qntx", Sandbox: "workspace-write"}

// runningCodexToo is the Node Agent of runningTheNodeAgent, with codex named
// and a stand-in for it held.
func runningCodexToo(t *testing.T) (s *QNTXServer, ran string) {
	t.Helper()
	named := opusLow
	named.Codex = sonnetInCodex
	s, _ = runningTheNodeAgent(t, named)
	binary, ran := claudeStandIn(t, codexAnswered)
	fetched := make(chan struct{})
	close(fetched)
	s.holdHarness("codex", &harnessHeld{name: "Codex", fetched: fetched, path: binary})
	return s, ran
}

func sayingToCodex(s *QNTXServer, sent sigil.Sent) (*protocol.CodexSaid, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/agent/say", nil)
	answer, refused := s.harnessSay(sigil.WithCaller(context.Background(), asked), s.codexHarness(), sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(*protocol.CodexSaid), nil
}

// What is said to the Node Agent in codex is answered by codex: run headless
// with its own CODEX_HOME, which reaches the node's gateway with the agent's
// own token and runs the agent's ground on every event codex sends.
func TestWhatIsSaidToCodexIsAnsweredByCodex(t *testing.T) {
	s, ran := runningCodexToo(t)

	answer, refused := sayingToCodex(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())
	assert.Equal(t, "anthropic/claude-sonnet-4.5", answer.GetModel())
	assert.Equal(t, codex.PinnedVersion, answer.GetCodexVersion())
	holds(t, s.nodeAgentSignum(), "say", &protocol.NodeAgentSaid{Codex: answer})

	args := ranWith(t, ran, "0", "args")
	assert.Equal(t, "exec", args[0])
	assert.Equal(t, "anthropic/claude-sonnet-4.5", after(args, "--model"))
	assert.Equal(t, "workspace-write", after(args, "--sandbox"))
	assert.Contains(t, args, "--dangerously-bypass-hook-trust", "its hooks are the node's, written by the node")
	assert.NotContains(t, args, "resume", "a first turn starts codex's thread")

	home := s.nodeAgent.home
	env := ranWith(t, ran, "0", "env")
	assert.Contains(t, env, "CODEX_HOME="+filepath.Join(home, "codex"))
	assert.Contains(t, env, "CODEX_API_KEY="+s.nodeAgent.token)

	config, err := os.ReadFile(filepath.Join(home, "codex", "config.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(config), `base_url = "http://127.0.0.1:8770/api/openrouter-qntx/v1"`)
	assert.Contains(t, string(config), `env_key = "CODEX_API_KEY"`)
	assert.Contains(t, string(config), `wire_api = "responses"`)
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "Stop"} {
		assert.Contains(t, string(config), "[[hooks."+event+"]]")
	}
	assert.Contains(t, string(config), `command = "`+comet.AgentHook(home)+`"`)
}

// "keep Codex's thread ID distinct from QNTX's current preallocated session ID"
// codex names its thread on the first turn; the next resumes that thread, and
// both are written down in the one session.
func TestCodexResumesItsOwnThreadInTheOneSession(t *testing.T) {
	s, ran := runningCodexToo(t)

	first, refused := sayingToCodex(s, sigil.Sent{"says": "remember 7"})
	require.Nil(t, refused)
	second, refused := sayingToCodex(s, sigil.Sent{"says": "what did I say?"})
	require.Nil(t, refused)
	assert.Equal(t, first.GetSession(), second.GetSession())
	assert.NotEqual(t, "thread-7", first.GetSession(), "the session is the node's, the thread codex's")

	args := ranWith(t, ran, "1", "args")
	assert.Equal(t, "thread-7", after(args, "--"), "the second turn resumes codex's own thread")
	assert.Contains(t, args, "resume")
}

// The session reads as the other harnesses' do.
func TestCodexSessionReadsAsTheOthers(t *testing.T) {
	s, _ := runningCodexToo(t)
	_, refused := sayingToCodex(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)

	answer, refused := s.harnessSession(s.codexHarness())
	require.Nil(t, refused)
	holds(t, s.nodeAgentSignum(), "session", answer)
	var said [][2]string
	for _, turn := range answer.(*protocol.SessionTranscript).GetTranscript().GetTurns() {
		said = append(said, [2]string{turn.GetSpeaker(), turn.GetText()})
	}
	assert.Equal(t, [][2]string{
		{"human", "how long has the box been up?"},
		{"session", "Start codex"},
		{"tool", "uptime"},
		{"assistant", "Up 3 days."},
	}, said)
}

// A node whose am.toml names no codex has none to speak to, and says where.
func TestANodeThatNamesNoCodexHasNoneToSpeakTo(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	_, refused := sayingToCodex(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "[agent.root.codex]")
}

// codex says who answers in it and how it is run.
func TestCodexSaysWhoItIs(t *testing.T) {
	s, _ := runningCodexToo(t)
	is, refused := s.harnessAm(s.codexHarness())
	require.Nil(t, refused)
	am := is.(*protocol.CodexAm)
	assert.Equal(t, s.nodeAgent.did, am.GetDid())
	assert.Equal(t, "anthropic/claude-sonnet-4.5", am.GetModel())
	assert.Equal(t, "openrouter-qntx", am.GetGateway())
	assert.Equal(t, codex.PinnedVersion, am.GetCodexVersion())
	holds(t, s.nodeAgentSignum(), "am", &protocol.NodeAgentAm{Codex: am})
	require.NoError(t, s.nodeAgentSignum().Check())
}
