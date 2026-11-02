package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/agy"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// The Node Agent in agy, a fourth harness (ADR-048).
// "wiring it into node and namespace config as an actual harness"

const (
	agentLineAgy   = "agy"
	agySessionFile = "agy-session"
	// agyConversationFile is agy's own conversation, kept apart from the
	// node's session: agy names it on a first turn and resumes only that.
	agyConversationFile = "agy-conversation"
)

// agyHarness is the Node Agent in agy.
func (s *QNTXServer) agyHarness() *harness {
	named := func() appcfg.AgyConfig { return s.deps.cfg.Agent.Root.Agy }
	return &harness{
		name: agentLineAgy, called: "agy", file: agySessionFile,
		named:    func() bool { return named().Named() },
		absent:   s.thereIsNoAgy,
		fetching: "agy is still being fetched",
		spec:     func() agentSpec { return agentSpec{} },
		part: func(_ context.Context, _ sigil.Sent, agent *rootAgent, _ agentSpec) (aTurn, *protocol.Refusal) {
			return s.agyPart(named(), agent)
		},
		am: func(is agentIn, _ *rootAgent, _ agentSpec) proto.Message {
			return &protocol.AgyAm{
				Did: is.did, Model: named().Model,
				Session: is.session, Answering: is.answering, Agy: is.path, AgyVersion: agy.PinnedVersion, NotReady: is.notReady,
			}
		},
	}
}

// holdAgy fetches the agy Parity pins into dir, off the boot's own time, as
// holdCodex does codex.
func holdAgy(ctx context.Context, dir string, spawn func(string, func()), logger *zap.SugaredLogger) *harnessHeld {
	held := &harnessHeld{name: "agy", fetched: make(chan struct{})}
	spawn("agent.agy", func() {
		defer close(held.fetched)
		pin, err := agy.Pinned()
		if err != nil {
			held.err = errors.Wrap(err, "the agy this build pins did not read")
		} else {
			held.path, held.err = pin.Ensure(ctx, dir)
		}
		if held.err != nil {
			logger.Errorw("agy was not fetched, so the Node Agent does not run in it", "dir", dir, "error", held.err)
			return
		}
		logger.Infow("agy is on this node", "version", pin.Version, "path", held.path)
	})
	return held
}

// thereIsNoAgy is why the Node Agent cannot be spoken to in agy on this node.
func (s *QNTXServer) thereIsNoAgy() *protocol.Refusal {
	if s.nodeAgent == nil {
		return s.thereIsNoNodeAgent()
	}
	if !s.deps.cfg.Agent.Root.Agy.Named() {
		return &protocol.Refusal{Why: sigil.NotFound, Says: "this node's am.toml names no agy for the Node Agent ([agent.root.agy]), so it does not run in agy"}
	}
	return &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no agy, so the Node Agent does not run in it"}
}

// agyPart is agy's part of one turn of the Node Agent: the Gemini key am.toml
// names, the agent's ground on every event agy sends, and its own conversation.
func (s *QNTXServer) agyPart(named appcfg.AgyConfig, agent *rootAgent) (aTurn, *protocol.Refusal) {
	var binary, key string
	return aTurn{
		ready: func(ctx context.Context, _ string, kept bool) (bool, *protocol.Refusal) {
			var err error
			if key, err = secretref.Resolve(ctx, named.TokenRef); err != nil || key == "" {
				why := "it resolved to nothing"
				if err != nil {
					why = err.Error()
				}
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "the Gemini API key am.toml names for agy did not resolve: " + why}
			}
			if binary, err = s.harnessHeldBy(agentLineAgy).Path(ctx); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no agy to run: " + err.Error()}
			}
			if refused := agyUnderGround(agent); refused != nil {
				return false, refused
			}
			return kept, nil
		},
		run: func(t turnRun) (proto.Message, error) {
			conversation := ""
			if t.resumes {
				conversation = agyConversationOf(agent.home)
			}
			return s.agyTurn(t, agent, agy.Said{Binary: binary, Home: agent.home, Session: conversation, Says: t.says, Model: named.Model, APIKey: key, Env: t.env})
		},
	}, nil
}

// agyUnderGround is whether agent runs under ground in agy: its ground built,
// and agy's hooks running it.
func agyUnderGround(agent *rootAgent) *protocol.Refusal {
	if err := comet.Under(agent.home); err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: agent.called + " is said nothing until it runs under ground: " + err.Error()}
	}
	if err := comet.AgentAgyHooks(agent.home); err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return nil
}

// agyConversationOf is the conversation agy last answered agent in, or empty.
func agyConversationOf(home string) string {
	held, err := os.ReadFile(filepath.Join(home, agyConversationFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(held))
}

// agyTurn runs said, writes its stream down as the session's rows, and keeps
// the conversation agy answered in.
func (s *QNTXServer) agyTurn(t turnRun, agent *rootAgent, said agy.Said) (*protocol.AgySaid, error) {
	tools := map[int]bool{}
	answer, err := said.Run(s.ctx, func(e agy.Event) { t.write(t.writes.rowsOfAgy(e, tools, t.now())) })
	if answer.Session != "" && answer.Session != said.Session {
		if keepErr := keepAgyConversation(agent.home, answer.Session); keepErr != nil {
			if err == nil {
				err = keepErr
			} else {
				err = errors.WithSecondaryError(err, keepErr)
			}
		}
	}
	if err != nil {
		t.write(t.writes.agyFailed(err.Error(), t.now()))
		return nil, err
	}
	return &protocol.AgySaid{
		Answer: answer.Text, Session: t.session, Conversation: answer.Session, Model: said.Model, AgyVersion: agy.PinnedVersion,
		InputTokens: answer.Usage.InputTokens, OutputTokens: answer.Usage.OutputTokens,
		TookMs: float64(answer.Took.Milliseconds()), Unwritten: t.unwritten(),
	}, nil
}

// keepAgyConversation keeps the conversation agy answered in, whole or not at all.
func keepAgyConversation(home, conversation string) error {
	f, err := os.CreateTemp(home, ".agy-conversation-*")
	if err != nil {
		return errors.Wrapf(err, "could not keep agy conversation %s in %s", conversation, home)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.WriteString(conversation + "\n"); err != nil {
		return errors.Wrapf(err, "could not write %s", f.Name())
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "could not close %s", f.Name())
	}
	return errors.Wrapf(os.Rename(f.Name(), filepath.Join(home, agyConversationFile)), "could not keep agy conversation %s in %s", conversation, home)
}

// rowsOfAgy is one event of agy's stream as the hook events Claude Code's are
// written as: its conversation starting, each tool it reached for, the turn's
// end. tools holds the steps already written, which agy updates more than once.
func (w sessionWriter) rowsOfAgy(e agy.Event, tools map[int]bool, at time.Time) ([]*types.As, error) {
	switch e.Event {
	case "init":
		row, err := w.row("SessionStart", at, map[string]any{"source": "agy"})
		return []*types.As{row}, err
	case "step_update":
		step := e.StepUpdate
		if step == nil || step.StepType != "tool" || tools[step.StepIndex] {
			return nil, nil
		}
		tools[step.StepIndex] = true
		attrs := map[string]any{"tool_use_id": e.StepUpdate.ConversationID + ":" + strconv.Itoa(step.StepIndex), "tool_name": step.ToolName}
		if len(step.ToolInfo) > 0 {
			var info struct {
				Parameters struct {
					CommandLine  string `json:"CommandLine"`
					TargetFile   string `json:"TargetFile"`
					AbsolutePath string `json:"AbsolutePath"`
				} `json:"parameters"`
			}
			if err := json.Unmarshal(step.ToolInfo, &info); err != nil {
				return nil, errors.Wrapf(err, "the tool_info of agy step %d did not read", step.StepIndex)
			}
			// A command it runs is written as Claude Code's Bash, as codex's are.
			if step.ToolName == "run_command" {
				attrs["tool_name"], attrs["command"] = "Bash", info.Parameters.CommandLine
			}
			if file := info.Parameters.TargetFile + info.Parameters.AbsolutePath; file != "" {
				attrs["file_path"] = file
			}
		}
		row, err := w.row("PreToolUse", at, attrs)
		return []*types.As{row}, err
	case "result":
		if e.Result == nil || e.Result.Status != "SUCCESS" {
			return nil, nil
		}
		attrs := map[string]any{"last_assistant_message": e.Result.Response}
		if w.effort != "" {
			attrs["effort"] = map[string]any{"level": w.effort}
		}
		row, err := w.row("Stop", at, attrs)
		return []*types.As{row}, err
	}
	return nil, nil
}

// agyFailed is the row of a turn agy did not answer.
func (w sessionWriter) agyFailed(why string, at time.Time) ([]*types.As, error) {
	row, err := w.row("StopFailure", at, map[string]any{"error": "agy", "last_assistant_message": why})
	return []*types.As{row}, err
}
