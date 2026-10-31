package server

// "make Codex selectable and runnable as a namespace agent"

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/codex"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
)

type codexLine struct{ model, sandbox, setBy string }
type aCodexAgentHeld struct {
	agent *rootAgent
	line  codexLine
}

func namespaceCodexHarness() *harness {
	return &harness{name: agentLineCodex, called: "Codex", file: codexSessionFile}
}

func (s *QNTXServer) namespaceCodexAgent(ctx context.Context, sent sigil.Sent) (aCodexAgentHeld, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	if refused != nil {
		return aCodexAgentHeld{}, refused
	}
	row, setBy, set, err := s.newestAgentLine(asked.namespace, agentLineCodex)
	if err != nil {
		return aCodexAgentHeld{}, codexRefusal(err)
	}
	if !set {
		return aCodexAgentHeld{}, &protocol.Refusal{Why: sigil.NotFound, Says: asked.namespace + " opted into no Codex agent: set one with agents set, harness codex"}
	}
	line := codexLine{setBy: setBy}
	for name, into := range map[string]*string{"model": &line.model, "sandbox": &line.sandbox} {
		if *into, err = oneText(row.Attributes, name, agentLineSubject, row.ID, asked.namespace); err != nil {
			return aCodexAgentHeld{}, codexRefusal(err)
		}
	}
	if strings.TrimSpace(line.model) == "" || !codexSandbox(line.sandbox) {
		return aCodexAgentHeld{}, codexRefusal(errors.Newf("invalid Codex model %q or sandbox %q in %s", line.model, line.sandbox, row.ID))
	}
	agent, refused := s.holdNamespaceAgent(asked.namespace, line.model, setBy)
	return aCodexAgentHeld{agent: agent, line: line}, refused
}

func codexSandbox(value string) bool { return value == "read-only" || value == "workspace-write" }
func codexRefusal(err error) *protocol.Refusal {
	return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
}

func (s *QNTXServer) setCodexAgent(sent sigil.Sent, admitted auth.Admission, namespace, model string) (any, *protocol.Refusal) {
	sandbox := sent["sandbox"]
	if !codexSandbox(sandbox) {
		return nil, &protocol.Refusal{Why: sigil.NotOneOf, Param: "sandbox", Says: "Codex sandbox must be explicitly read-only or workspace-write, got " + sandbox}
	}
	agent, refused := s.holdNamespaceAgent(namespace, model, admitted.Identity)
	if refused != nil {
		return nil, refused
	}
	if err := s.setAgentLine(admitted, namespace, agentLineCodex, map[string]any{"model": model, "sandbox": sandbox}); err != nil {
		return nil, codexRefusal(err)
	}
	return s.codexAmOf(aCodexAgentHeld{agent: agent, line: codexLine{model: model, sandbox: sandbox, setBy: admitted.Identity}})
}

// "Auth would be another PR": no login flow or node credential fallback.
// Codex itself validates any pre-provisioned sign-in in this agent's home.
func codexCredential(home string) error {
	path := filepath.Join(home, "codex", "auth.json")
	info, err := os.Lstat(path)
	if err != nil {
		return errors.Wrapf(err, "Codex has no readable agent-owned credential at %s; authentication setup is not implemented", path)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.Newf("Codex credential %s must be a nonempty regular file", path)
	}
	return nil
}

func (s *QNTXServer) codexAm(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceCodexAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	return s.codexAmOf(held)
}
func (s *QNTXServer) codexAmOf(held aCodexAgentHeld) (any, *protocol.Refusal) {
	pin, err := codex.Pinned()
	if err != nil {
		return nil, codexRefusal(err)
	}
	agent, line := held.agent, held.line
	am := &protocol.CodexAm{Did: agent.did, Model: line.model, Sandbox: line.sandbox, CodexVersion: pin.Version}
	session, kept, err := agent.sessionIn(codexSessionFile)
	if err != nil {
		am.NotReady = err.Error()
	} else if kept {
		am.Session = session
	}
	if going := agent.in(namespaceCodexHarness()).answering.Load(); going != nil {
		am.Answering = true
		am.Session = going.session
	}
	if binary := s.harnessHeldBy(agentLineCodex); binary != nil {
		path, arrived, err := binary.Now()
		if err != nil {
			am.NotReady = err.Error()
		} else if !arrived {
			am.NotReady = "Codex is still being fetched"
		} else {
			am.Codex = path
		}
	}
	if err := comet.Under(agent.home); err != nil {
		am.NotReady = err.Error()
	}
	if err := codexCredential(agent.home); err != nil {
		am.NotReady = err.Error()
	}
	return &protocol.NamespaceAgentAm{Namespace: agent.namespace, SetBy: line.setBy, Codex: am}, nil
}

// Fetch lazily on the first authorized turn; one binary is shared by namespaces.
func (s *QNTXServer) namespaceCodexBinary() (*harnessHeld, error) {
	s.harnessBinaries.mu.Lock()
	defer s.harnessBinaries.mu.Unlock()
	if held := s.harnessBinaries.held[agentLineCodex]; held != nil {
		return held, nil
	}
	if s.agentsDir == "" {
		return nil, errors.New("Codex has no agents directory")
	}
	if s.harnessBinaries.held == nil {
		s.harnessBinaries.held = map[string]*harnessHeld{}
	}
	held := holdCodex(s.ctx, filepath.Join(s.agentsDir, ".codex-runtime"), s.wg.Go, s.logger)
	s.harnessBinaries.held[agentLineCodex] = held
	return held, nil
}

func (s *QNTXServer) codexSay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceCodexAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	agent, line := held.agent, held.line
	h := namespaceCodexHarness()
	h.part = func(_ context.Context, _ sigil.Sent, _ *rootAgent, _ agentSpec) (aTurn, *protocol.Refusal) {
		var binary, thread string
		return aTurn{
			withoutClaude: true, keepBeforeRun: true,
			ready: func(ctx context.Context, _ string, _ bool) (bool, *protocol.Refusal) {
				if s.ownURL == "" {
					return false, codexRefusal(errors.New("Codex namespace agent has no node MCP URL"))
				}
				if err := codexCredential(agent.home); err != nil {
					return false, codexRefusal(err)
				}
				if err := comet.Under(agent.home); err != nil {
					return false, codexRefusal(err)
				}
				raw, err := os.ReadFile(filepath.Join(agent.home, codexThreadFile))
				if err != nil && !os.IsNotExist(err) {
					return false, codexRefusal(errors.Wrapf(err, "could not read Codex thread for %s", agent.namespace))
				}
				thread = strings.TrimSpace(string(raw))
				if err == nil && thread == "" {
					return false, codexRefusal(errors.Newf("Codex thread file is empty for %s", agent.namespace))
				}
				runtime, err := s.namespaceCodexBinary()
				if err != nil {
					return false, codexRefusal(err)
				}
				binary, err = runtime.Path(ctx)
				if err != nil {
					return false, codexRefusal(err)
				}
				return thread != "", nil
			},
			run: func(t turnRun) (proto.Message, error) {
				runCtx, cancel := context.WithCancel(s.ctx)
				defer cancel()
				var persistErr error
				var last string
				answer, err := (codex.Said{Binary: binary, Home: agent.home, Session: thread, Says: t.says, Model: line.model, Sandbox: line.sandbox, System: agent.isSaidToBe(), MCPURL: s.ownURL + "/mcp", MCPBearer: agent.token, Ground: comet.AgentGround(agent.home), TrustHooks: true}).Run(runCtx, func(e codex.Event) {
					if persistErr != nil {
						return
					}
					if e.Type == "thread.started" && e.ThreadID != "" && (thread == "" || thread == e.ThreadID) {
						persistErr = keepCodexThread(agent.home, e.ThreadID)
						if persistErr != nil {
							cancel()
							return
						}
					}
					if e.Type != "turn.completed" {
						t.write(t.writes.rowsOfCodex(e, &last, t.now()))
					}
				})
				if persistErr != nil {
					err = errors.WithSecondaryError(persistErr, err)
				}
				if err != nil {
					row, rowErr := t.writes.row("StopFailure", t.now(), map[string]any{"error": err.Error()})
					t.write([]*types.As{row}, rowErr)
					return nil, err
				}
				row, rowErr := t.writes.row("Stop", t.now(), map[string]any{"last_assistant_message": answer.Text})
				t.write([]*types.As{row}, rowErr)
				pin, err := codex.Pinned()
				if err != nil {
					return nil, err
				}
				return &protocol.CodexSaid{Answer: answer.Text, Session: t.session, Thread: answer.Session, Model: line.model, CodexVersion: pin.Version, InputTokens: answer.Usage.InputTokens, OutputTokens: answer.Usage.OutputTokens, TookMs: float64(answer.Took.Milliseconds()), Unwritten: t.unwritten()}, nil
			},
		}, nil
	}
	answer, refused := s.sayTo(ctx, h, agent, agentSpec{}, sent)
	if refused != nil {
		return nil, refused
	}
	return &protocol.NamespaceAgentSaid{Codex: answer.(*protocol.CodexSaid)}, nil
}

func keepCodexThread(home, thread string) error {
	f, err := os.CreateTemp(home, ".codex-thread-*")
	if err != nil {
		return errors.Wrapf(err, "could not keep Codex thread %s in %s", thread, home)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.WriteString(thread + "\n"); err != nil {
		return errors.Wrapf(err, "could not write %s", f.Name())
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "could not close %s", f.Name())
	}
	return errors.Wrapf(os.Rename(f.Name(), filepath.Join(home, codexThreadFile)), "could not persist Codex thread %s in %s", thread, home)
}
