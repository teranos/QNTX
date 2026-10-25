package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/codex"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// The Node Agent in codex, a third harness (ADR-048).
// "Codex Agent runtime as another first class citizen in QNTX besides Claude"

const (
	agentLineCodex   = "codex"
	codexSessionFile = "codex-session"
	// codexThreadFile is codex's own thread, kept apart from the node's
	// session: codex names its thread on a first turn and resumes only that.
	codexThreadFile = "codex-thread"
)

// codexHarness is the Node Agent in codex.
func (s *QNTXServer) codexHarness() *harness {
	named := func() appcfg.CodexConfig { return s.deps.cfg.Agent.Root.Codex }
	return &harness{
		name: "codex", called: "Codex", file: codexSessionFile,
		named:    func() bool { return named().Named() },
		absent:   s.thereIsNoCodex,
		fetching: "codex is still being fetched",
		spec:     func() agentSpec { return agentSpec{} },
		part: func(_ context.Context, _ sigil.Sent, agent *rootAgent, _ agentSpec) (aTurn, *protocol.Refusal) {
			return s.codexPart(named(), agent)
		},
		am: func(is agentIn, _ *rootAgent, _ agentSpec) proto.Message {
			pin := named()
			return &protocol.CodexAm{
				Did: is.did, Model: pin.Model, Gateway: pin.Gateway, Sandbox: pin.Sandbox,
				Session: is.session, Answering: is.answering, Codex: is.path, CodexVersion: codex.PinnedVersion, NotReady: is.notReady,
			}
		},
	}
}

// holdCodex fetches the codex Parity pins into dir, off the boot's own time, as
// holdClaudeCode does Claude Code.
func holdCodex(ctx context.Context, dir string, spawn func(string, func()), logger *zap.SugaredLogger) *harnessHeld {
	held := &harnessHeld{name: "Codex", fetched: make(chan struct{})}
	spawn("agent.codex", func() {
		defer close(held.fetched)
		pin, err := codex.Pinned()
		if err != nil {
			held.err = errors.Wrap(err, "the codex this build pins did not read")
		} else {
			held.path, held.err = pin.Ensure(ctx, dir)
		}
		if held.err != nil {
			logger.Errorw("codex was not fetched, so the Node Agent does not run in it", "dir", dir, "error", held.err)
			return
		}
		logger.Infow("codex is on this node", "version", pin.Version, "path", held.path)
	})
	return held
}

// thereIsNoCodex is why the Node Agent cannot be spoken to in codex on this node.
func (s *QNTXServer) thereIsNoCodex() *protocol.Refusal {
	if s.nodeAgent == nil {
		return s.thereIsNoNodeAgent()
	}
	if !s.deps.cfg.Agent.Root.Codex.Named() {
		return &protocol.Refusal{Why: sigil.NotFound, Says: "this node's am.toml names no codex for the Node Agent ([agent.root.codex]), so it does not run in codex"}
	}
	return &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no codex, so the Node Agent does not run in it"}
}

// codexPart is codex's part of one turn of the Node Agent: its own CODEX_HOME,
// naming the node's gateway and the agent's ground, and its own thread.
func (s *QNTXServer) codexPart(named appcfg.CodexConfig, agent *rootAgent) (aTurn, *protocol.Refusal) {
	if s.ownURL == "" {
		return aTurn{}, &protocol.Refusal{Why: sigil.Failed, Says: "this node knows no URL of its own, where codex reaches its gateway"}
	}
	var binary string
	threads := filepath.Join(agent.home, codexThreadFile)
	return aTurn{
		ready: func(ctx context.Context, _ string, kept bool) (bool, *protocol.Refusal) {
			var err error
			if binary, err = s.harnessHeldBy("codex").Path(ctx); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no codex to run: " + err.Error()}
			}
			if err := comet.Under(agent.home); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: agent.called + " is said nothing until it runs under ground: " + err.Error()}
			}
			if err := comet.AgentCodexConfig(agent.home, codexGateway(s.ownURL, named.Gateway)); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
			}
			return kept, nil
		},
		run: func(t turnRun) (proto.Message, error) {
			thread := ""
			if t.resumes {
				if held, err := os.ReadFile(threads); err == nil {
					thread = strings.TrimSpace(string(held))
				}
			}
			last := ""
			said := codex.Said{
				Binary: binary, Home: agent.home, Session: thread, Says: t.says,
				Model: named.Model, Sandbox: named.Sandbox, APIKey: agent.token, TrustHooks: true, Env: t.env,
			}
			answer, err := said.Run(s.ctx, func(e codex.Event) { t.write(t.writes.rowsOfCodex(e, &last, t.now())) })
			if err != nil {
				t.write(t.writes.codexFailed(err.Error(), t.now()))
				return nil, err
			}
			if answer.Session != thread {
				if err := os.WriteFile(threads, []byte(answer.Session+"\n"), 0o600); err != nil {
					return nil, errors.Wrapf(err, "codex answered in thread %s, which was not kept at %s", answer.Session, threads)
				}
			}
			return &protocol.CodexSaid{
				Answer: answer.Text, Session: t.session, Model: named.Model, CodexVersion: codex.PinnedVersion,
				InputTokens: answer.Usage.InputTokens, OutputTokens: answer.Usage.OutputTokens,
				TookMs: float64(answer.Took.Milliseconds()), Unwritten: t.unwritten(),
			}, nil
		},
	}, nil
}

// codexGateway is where codex makes its model calls: a gateway named by its
// URL is that URL, and one named as a plugin is that plugin on this node.
func codexGateway(own, gateway string) string {
	if strings.Contains(gateway, "://") {
		return gateway
	}
	return own + "/api/" + gateway + "/v1"
}

// rowsOfCodex is one record of codex's stream as the hook events Claude Code's
// are written as: its thread starting, each tool it reached for, the turn's end.
// last keeps what it said last, which the turn's end carries.
func (w sessionWriter) rowsOfCodex(e codex.Event, last *string, at time.Time) ([]*types.As, error) {
	switch e.Type {
	case "thread.started":
		row, err := w.row("SessionStart", at, map[string]any{"source": "codex"})
		return []*types.As{row}, err
	case "item.started", "item.completed":
		var item struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Text    string `json:"text"`
			Command string `json:"command"`
			Server  string `json:"server"`
			Tool    string `json:"tool"`
			Changes []struct {
				Path string `json:"path"`
			} `json:"changes"`
		}
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return nil, errors.Wrapf(err, "the item of a codex %s did not read", e.Type)
		}
		if e.Type == "item.completed" {
			if item.Type == "agent_message" {
				*last = item.Text
			}
			return nil, nil
		}
		attrs := map[string]any{"tool_use_id": item.ID}
		switch item.Type {
		case "command_execution":
			attrs["tool_name"], attrs["command"] = "Bash", item.Command
		case "file_change":
			attrs["tool_name"] = "apply_patch"
			if len(item.Changes) > 0 {
				attrs["file_path"] = item.Changes[0].Path
			}
		case "mcp_tool_call":
			attrs["tool_name"] = "mcp__" + item.Server + "__" + item.Tool
		default:
			return nil, nil
		}
		row, err := w.row("PreToolUse", at, attrs)
		return []*types.As{row}, err
	case "turn.completed":
		attrs := map[string]any{"last_assistant_message": *last}
		if w.effort != "" {
			attrs["effort"] = map[string]any{"level": w.effort}
		}
		row, err := w.row("Stop", at, attrs)
		return []*types.As{row}, err
	}
	return nil, nil
}

// codexFailed is the row of a turn codex did not answer.
func (w sessionWriter) codexFailed(why string, at time.Time) ([]*types.As, error) {
	row, err := w.row("StopFailure", at, map[string]any{"error": "codex", "last_assistant_message": why})
	return []*types.As{row}, err
}
