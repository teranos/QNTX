package server

// "wiring it into node and namespace config as an actual harness"

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/teranos/QNTX/internal/agy"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
)

// agyKey is the namespace's key a namespace agent in agy makes its model calls
// on: its Gemini API key, by the provider's name (ADR-051).
const agyKey = "gemini"

type agyLine struct{ model, setBy string }
type anAgyAgentHeld struct {
	agent *rootAgent
	line  agyLine
}

func namespaceAgyHarness() *harness {
	return &harness{name: agentLineAgy, called: "agy", file: agySessionFile}
}

func (s *QNTXServer) namespaceAgyAgent(ctx context.Context, sent sigil.Sent) (anAgyAgentHeld, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	if refused != nil {
		return anAgyAgentHeld{}, refused
	}
	row, setBy, set, err := s.newestAgentLine(asked.namespace, agentLineAgy)
	if err != nil {
		return anAgyAgentHeld{}, agyRefusal(err)
	}
	if !set {
		return anAgyAgentHeld{}, &protocol.Refusal{Why: sigil.NotFound, Says: asked.namespace + " opted into no agy agent: set one with agents set, harness agy"}
	}
	model, err := oneText(row.Attributes, "model", agentLineSubject, row.ID, asked.namespace)
	if err != nil {
		return anAgyAgentHeld{}, agyRefusal(err)
	}
	if strings.TrimSpace(model) == "" {
		return anAgyAgentHeld{}, agyRefusal(errors.Newf("the agy agent of %s names no model in %s", asked.namespace, row.ID))
	}
	agent, refused := s.holdNamespaceAgent(asked.namespace, model, setBy)
	return anAgyAgentHeld{agent: agent, line: agyLine{model: model, setBy: setBy}}, refused
}

func agyRefusal(err error) *protocol.Refusal {
	return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
}

func (s *QNTXServer) setAgyAgent(admitted auth.Admission, namespace, model string) (any, *protocol.Refusal) {
	agent, refused := s.holdNamespaceAgent(namespace, model, admitted.Identity)
	if refused != nil {
		return nil, refused
	}
	if err := s.setAgentLine(admitted, namespace, agentLineAgy, map[string]any{"model": model}); err != nil {
		return nil, agyRefusal(err)
	}
	s.logger.Infow("a namespace opted into an agent", "namespace", namespace, "harness", agentLineAgy, "model", model, "by", admitted.Identity)
	return s.agyAmOf(anAgyAgentHeld{agent: agent, line: agyLine{model: model, setBy: admitted.Identity}})
}

// agyKeyOf is the namespace's Gemini API key, for the agy process the node
// starts for it and nothing else.
func (s *QNTXServer) agyKeyOf(namespace string) (string, error) {
	key, err := s.keyValue(namespace, agyKey)
	if err != nil {
		return "", errors.Wrapf(err, "the agent of %s in agy makes its model calls on the namespace's %s key", namespace, agyKey)
	}
	return key, nil
}

func (s *QNTXServer) agyAm(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceAgyAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	return s.agyAmOf(held)
}

func (s *QNTXServer) agyAmOf(held anAgyAgentHeld) (any, *protocol.Refusal) {
	agent, line := held.agent, held.line
	am := &protocol.AgyAm{Did: agent.did, Model: line.model, AgyVersion: agy.PinnedVersion}
	session, kept, err := agent.sessionIn(agySessionFile)
	if err != nil {
		am.NotReady = err.Error()
	} else if kept {
		am.Session = session
	}
	if going := agent.in(namespaceAgyHarness()).answering.Load(); going != nil {
		am.Answering = true
		am.Session = going.session
	}
	if binary := s.harnessHeldBy(agentLineAgy); binary != nil {
		path, arrived, err := binary.Now()
		if err != nil {
			am.NotReady = err.Error()
		} else if !arrived {
			am.NotReady = "agy is still being fetched"
		} else {
			am.Agy = path
		}
	}
	if err := comet.Under(agent.home); err != nil {
		am.NotReady = err.Error()
	}
	if _, err := s.agyKeyOf(agent.namespace); err != nil {
		am.NotReady = err.Error()
	}
	return &protocol.NamespaceAgentAm{Namespace: agent.namespace, SetBy: line.setBy, Agy: am}, nil
}

// namespaceAgyBinary fetches agy on the first turn a namespace asks of it; one
// binary is shared by every namespace.
func (s *QNTXServer) namespaceAgyBinary() (*harnessHeld, error) {
	s.harnessBinaries.mu.Lock()
	defer s.harnessBinaries.mu.Unlock()
	if held := s.harnessBinaries.held[agentLineAgy]; held != nil {
		return held, nil
	}
	if s.agentsDir == "" {
		return nil, errors.New("agy has no agents directory to be fetched into")
	}
	if s.harnessBinaries.held == nil {
		s.harnessBinaries.held = map[string]*harnessHeld{}
	}
	held := holdAgy(s.ctx, filepath.Join(s.agentsDir, ".agy-runtime"), s.wg.Go, s.logger)
	s.harnessBinaries.held[agentLineAgy] = held
	return held, nil
}

func (s *QNTXServer) agySay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceAgyAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	agent, line := held.agent, held.line
	h := namespaceAgyHarness()
	h.part = func(_ context.Context, _ sigil.Sent, _ *rootAgent, _ agentSpec) (aTurn, *protocol.Refusal) {
		var binary, key, conversation string
		return aTurn{
			withoutClaude: true, keepBeforeRun: true,
			ready: func(ctx context.Context, _ string, _ bool) (bool, *protocol.Refusal) {
				var err error
				if key, err = s.agyKeyOf(agent.namespace); err != nil {
					return false, agyRefusal(err)
				}
				if refused := agyUnderGround(agent); refused != nil {
					return false, refused
				}
				runtime, err := s.namespaceAgyBinary()
				if err != nil {
					return false, agyRefusal(err)
				}
				if binary, err = runtime.Path(ctx); err != nil {
					return false, agyRefusal(err)
				}
				conversation = agyConversationOf(agent.home)
				return conversation != "", nil
			},
			run: func(t turnRun) (proto.Message, error) {
				return s.agyTurn(t, agent, agy.Said{Binary: binary, Home: agent.home, Session: conversation, Says: t.says, Model: line.model, APIKey: key, Env: t.env})
			},
		}, nil
	}
	answer, refused := s.sayTo(ctx, h, agent, agentSpec{}, sent)
	if refused != nil {
		return nil, refused
	}
	return &protocol.NamespaceAgentSaid{Agy: answer.(*protocol.AgySaid)}, nil
}
